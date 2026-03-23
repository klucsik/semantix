// Package main is the entry point for the Semantix CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mreider/semantix/internal/config"
	"github.com/mreider/semantix/internal/simulation"
	"github.com/mreider/semantix/internal/telemetry"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	// CLI flags
	configDir := flag.String("config-dir", "./configs", "Directory containing YAML configuration files")
	configFile := flag.String("config", "", "Single YAML configuration file (overrides -config-dir)")
	showVersion := flag.Bool("version", false, "Show version information")
	dryRun := flag.Bool("dry-run", false, "Validate configuration without running simulation")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Semantix - OpenTelemetry Simulation Engine\n\n")
		fmt.Fprintf(os.Stderr, "Usage: semantix [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  DT_ENDPOINT    Dynatrace OTLP endpoint (e.g., https://xxx.live.dynatrace.com/api/v2/otlp)\n")
		fmt.Fprintf(os.Stderr, "  DT_API_TOKEN   Dynatrace API token with ingest permissions\n")
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("Semantix %s\n", version)
		fmt.Printf("  Commit: %s\n", commit)
		fmt.Printf("  Built:  %s\n", buildDate)
		os.Exit(0)
	}

	// Load configuration
	var configs []*config.Config
	var err error

	if *configFile != "" {
		cfg, err := config.LoadConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		configs = []*config.Config{cfg}
		log.Printf("Loaded configuration from %s", *configFile)
	} else {
		configs, err = config.LoadConfigDir(*configDir)
		if err != nil {
			log.Fatalf("Failed to load configs: %v", err)
		}
		log.Printf("Loaded %d configuration(s) from %s", len(configs), *configDir)
	}

	// Print summary of loaded configurations
	for _, cfg := range configs {
		entryPoints := cfg.GetEntryPoints()
		log.Printf("  - %d services, %d entry points", len(cfg.Services), len(entryPoints))
		for _, ep := range entryPoints {
			log.Printf("    → %s.%s (%.1f req/min)", ep.Service.Name, ep.Endpoint.Name, ep.Endpoint.Traffic.RequestsPerMinute)
		}
	}

	if *dryRun {
		log.Println("Dry run complete - configuration is valid")
		os.Exit(0)
	}

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	// Initialize telemetry exporters for each config
	// (each config may have different endpoints)
	engines := make([]*simulation.Engine, 0, len(configs))
	for i, cfg := range configs {
		// Setup telemetry provider
		tp, err := telemetry.NewProvider(ctx, cfg)
		if err != nil {
			log.Fatalf("Failed to initialize telemetry for config %d: %v", i, err)
		}
		defer tp.Shutdown(ctx)

		// Create simulation engine
		engine, err := simulation.NewEngine(cfg, tp)
		if err != nil {
			log.Fatalf("Failed to create simulation engine for config %d: %v", i, err)
		}
		engines = append(engines, engine)
	}

	// Run all simulation engines
	log.Printf("Starting %d simulation engine(s)...", len(engines))
	errCh := make(chan error, len(engines))

	for _, engine := range engines {
		go func(e *simulation.Engine) {
			errCh <- e.Run(ctx)
		}(engine)
	}

	// Start HTTP server for health checks and dashboard
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Placeholder dashboard
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Semantix Dashboard</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; 
               background: #1a1a2e; color: #eee; margin: 0; padding: 40px; }
        .container { max-width: 800px; margin: 0 auto; }
        h1 { color: #00d4ff; }
        .status { background: #16213e; padding: 20px; border-radius: 8px; margin: 20px 0; }
        .status-indicator { display: inline-block; width: 12px; height: 12px; 
                           background: #00ff88; border-radius: 50%%; margin-right: 8px; }
        .services { display: grid; gap: 10px; margin-top: 20px; }
        .service { background: #0f3460; padding: 15px; border-radius: 6px; }
        code { background: #0a0a1a; padding: 2px 6px; border-radius: 4px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Semantix</h1>
        <p>OpenTelemetry Simulation Engine</p>
        <div class="status">
            <span class="status-indicator"></span>
            <strong>Running</strong> — Emitting telemetry to Dynatrace
        </div>
        <div class="services">
            <div class="service">
                <strong>Active Simulations:</strong> %d
            </div>
            <div class="service">
                <strong>Version:</strong> <code>%s</code>
            </div>
        </div>
        <p style="margin-top: 40px; color: #666;">Dashboard coming soon...</p>
    </div>
</body>
</html>`, len(engines), version)
	})

	server := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		log.Printf("HTTP server listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		log.Println("Context cancelled, waiting for engines to stop...")
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			log.Fatalf("Simulation error: %v", err)
		}
	}

	// Shutdown HTTP server
	server.Shutdown(context.Background())

	log.Println("Semantix shutdown complete")
}
