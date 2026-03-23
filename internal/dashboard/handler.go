// Package dashboard provides the web UI for Semantix.
package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mreider/semantix/internal/config"
)

// Handler serves the dashboard UI and API endpoints.
type Handler struct {
	manager *config.Manager
	version string
}

// New creates a new dashboard handler.
func New(cfg *config.Config, version string) *Handler {
	return &Handler{
		manager: config.NewManager(cfg),
		version: version,
	}
}

// GetManager returns the config manager for external access (e.g., hot reload).
func (h *Handler) GetManager() *config.Manager {
	return h.manager
}

// RegisterRoutes registers all dashboard routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.serveDashboard)
	mux.HandleFunc("/health", h.serveHealth)
	mux.HandleFunc("/api/topology", h.serveTopology)
	mux.HandleFunc("/api/config", h.serveConfig)

	// Config editing endpoints
	mux.HandleFunc("/api/config/services", h.handleServicesConfig)
	mux.HandleFunc("/api/config/problem_patterns", h.handleProblemPatternsConfig)
	mux.HandleFunc("/api/config/scenarios", h.handleScenariosConfig)
	mux.HandleFunc("/api/config/pending", h.handlePendingChanges)
	mux.HandleFunc("/api/config/save", h.handleSaveChanges)
	mux.HandleFunc("/api/config/reset", h.handleResetConfig)
	mux.HandleFunc("/api/config/changelog", h.handleChangelog)
}

func (h *Handler) serveHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// TopologyData represents the D3-compatible topology structure.
type TopologyData struct {
	Nodes []TopologyNode `json:"nodes"`
	Links []TopologyLink `json:"links"`
	Stats TopologyStats  `json:"stats"`
}

// TopologyNode represents a service node.
type TopologyNode struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Version      string            `json:"version"`
	System       string            `json:"system,omitempty"`
	Endpoints    []EndpointSummary `json:"endpoints"`
	TotalRPM     float64           `json:"totalRpm"`
	AvgLatencyMs float64           `json:"avgLatencyMs"`
	ErrorRate    float64           `json:"errorRate"`
	HasAnomalies bool              `json:"hasAnomalies"`
}

// EndpointSummary provides endpoint details.
type EndpointSummary struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Method       string   `json:"method,omitempty"`
	Route        string   `json:"route,omitempty"`
	Operation    string   `json:"operation,omitempty"`
	Table        string   `json:"table,omitempty"`
	Topic        string   `json:"topic,omitempty"`
	RPM          float64  `json:"rpm"`
	P50Ms        float64  `json:"p50Ms"`
	P95Ms        float64  `json:"p95Ms"`
	P99Ms        float64  `json:"p99Ms"`
	ErrorRate    float64  `json:"errorRate"`
	ErrorCodes   []int    `json:"errorCodes,omitempty"`
	Calls        []string `json:"calls,omitempty"`
	HasAnomalies bool     `json:"hasAnomalies"`
}

// TopologyLink represents a connection between services.
type TopologyLink struct {
	Source   string  `json:"source"`
	Target   string  `json:"target"`
	Type     string  `json:"type"` // "sync", "async"
	Endpoint string  `json:"endpoint"`
	RPM      float64 `json:"rpm"`
}

// TopologyStats provides overall statistics.
type TopologyStats struct {
	TotalServices  int     `json:"totalServices"`
	TotalEndpoints int     `json:"totalEndpoints"`
	TotalRPM       float64 `json:"totalRpm"`
	TotalLinks     int     `json:"totalLinks"`
	AvgErrorRate   float64 `json:"avgErrorRate"`
}

func (h *Handler) serveTopology(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	topology := h.buildTopology()
	json.NewEncoder(w).Encode(topology)
}

func (h *Handler) serveConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	cfg := h.manager.GetCurrentConfig()

	// Return sanitized config (without tokens)
	type SafeConfig struct {
		Version         string                        `json:"version"`
		Simulation      config.SimulationConfig       `json:"simulation"`
		Services        []config.ServiceConfig        `json:"services"`
		Scenarios       []config.ScenarioConfig       `json:"scenarios"`
		ProblemPatterns []config.ProblemPatternConfig `json:"problem_patterns"`
	}

	safeConfig := SafeConfig{
		Version:         cfg.Version,
		Simulation:      cfg.Simulation,
		Services:        cfg.Services,
		Scenarios:       cfg.Scenarios,
		ProblemPatterns: cfg.ProblemPatterns,
	}

	json.NewEncoder(w).Encode(safeConfig)
}

// handleServicesConfig handles GET/POST for services configuration.
func (h *Handler) handleServicesConfig(w http.ResponseWriter, r *http.Request) {
	h.setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch r.Method {
	case "GET":
		yaml, err := h.manager.GetServicesYAML()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(yaml))

	case "POST":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.manager.AddPendingChange(config.ChangeTypeServices, string(body)); err != nil {
			http.Error(w, "Invalid YAML: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleProblemPatternsConfig handles GET/POST for problem patterns configuration.
func (h *Handler) handleProblemPatternsConfig(w http.ResponseWriter, r *http.Request) {
	h.setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch r.Method {
	case "GET":
		yaml, err := h.manager.GetProblemPatternsYAML()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(yaml))

	case "POST":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.manager.AddPendingChange(config.ChangeTypeProblemPatterns, string(body)); err != nil {
			http.Error(w, "Invalid YAML: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleScenariosConfig handles GET/POST for scenarios configuration.
func (h *Handler) handleScenariosConfig(w http.ResponseWriter, r *http.Request) {
	h.setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch r.Method {
	case "GET":
		yaml, err := h.manager.GetScenariosYAML()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(yaml))

	case "POST":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.manager.AddPendingChange(config.ChangeTypeScenarios, string(body)); err != nil {
			http.Error(w, "Invalid YAML: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePendingChanges returns pending changes.
func (h *Handler) handlePendingChanges(w http.ResponseWriter, r *http.Request) {
	h.setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == "DELETE" {
		h.manager.ClearPendingChanges()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.manager.GetPendingChanges())
}

// handleSaveChanges applies all pending changes.
func (h *Handler) handleSaveChanges(w http.ResponseWriter, r *http.Request) {
	h.setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	newConfig, entry, err := h.manager.ApplyPendingChanges()
	if err != nil {
		http.Error(w, "Failed to apply changes: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "applied",
		"changelog": entry,
		"services":  len(newConfig.Services),
	})
}

// handleResetConfig resets to default configuration.
func (h *Handler) handleResetConfig(w http.ResponseWriter, r *http.Request) {
	h.setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := h.manager.ResetToDefaults()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "reset",
		"services": len(cfg.Services),
	})
}

// handleChangelog returns the changelog history.
func (h *Handler) handleChangelog(w http.ResponseWriter, r *http.Request) {
	h.setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == "DELETE" {
		h.manager.ClearChangelog()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.manager.GetChangelog())
}

func (h *Handler) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func (h *Handler) buildTopology() TopologyData {
	nodes := make([]TopologyNode, 0)
	links := make([]TopologyLink, 0)

	var totalRPM float64
	var totalEndpoints int
	var totalErrorRate float64
	var errorRateCount int

	// Get current config from manager
	cfg := h.manager.GetCurrentConfig()

	for _, svc := range cfg.Services {
		node := TopologyNode{
			ID:      svc.Name,
			Name:    svc.Name,
			Type:    getServiceType(svc),
			Version: svc.Version,
			System:  svc.System,
		}

		var svcRPM float64
		var svcLatency float64
		var svcErrorRate float64
		var latencyCount int
		var errorCount int

		for _, ep := range svc.Endpoints {
			rpm := 0.0
			if ep.Traffic != nil {
				rpm = ep.Traffic.RequestsPerMinute
			}
			svcRPM += rpm
			totalRPM += rpm
			totalEndpoints++

			errRate := 0.0
			var errCodes []int
			if ep.Errors != nil {
				errRate = ep.Errors.Rate
				svcErrorRate += errRate
				errorCount++
				totalErrorRate += errRate
				errorRateCount++
				for _, et := range ep.Errors.Types {
					errCodes = append(errCodes, et.Code)
				}
			}

			svcLatency += ep.Latency.P50Ms
			latencyCount++

			var calls []string
			for _, call := range ep.Calls {
				calls = append(calls, fmt.Sprintf("%s.%s", call.Service, call.Endpoint))

				linkType := "sync"
				if call.Async {
					linkType = "async"
				}

				links = append(links, TopologyLink{
					Source:   svc.Name,
					Target:   call.Service,
					Type:     linkType,
					Endpoint: call.Endpoint,
					RPM:      rpm,
				})
			}

			node.Endpoints = append(node.Endpoints, EndpointSummary{
				Name:         ep.Name,
				Type:         ep.Type,
				Method:       ep.Method,
				Route:        ep.Route,
				Operation:    ep.Operation,
				Table:        ep.Table,
				Topic:        ep.Topic,
				RPM:          rpm,
				P50Ms:        ep.Latency.P50Ms,
				P95Ms:        ep.Latency.P95Ms,
				P99Ms:        ep.Latency.P99Ms,
				ErrorRate:    errRate,
				ErrorCodes:   errCodes,
				Calls:        calls,
				HasAnomalies: len(ep.Anomalies) > 0,
			})

			if len(ep.Anomalies) > 0 {
				node.HasAnomalies = true
			}
		}

		node.TotalRPM = svcRPM
		if latencyCount > 0 {
			node.AvgLatencyMs = svcLatency / float64(latencyCount)
		}
		if errorCount > 0 {
			node.ErrorRate = svcErrorRate / float64(errorCount)
		}

		nodes = append(nodes, node)
	}

	avgErrorRate := 0.0
	if errorRateCount > 0 {
		avgErrorRate = totalErrorRate / float64(errorRateCount)
	}

	return TopologyData{
		Nodes: nodes,
		Links: links,
		Stats: TopologyStats{
			TotalServices:  len(nodes),
			TotalEndpoints: totalEndpoints,
			TotalRPM:       totalRPM,
			TotalLinks:     len(links),
			AvgErrorRate:   avgErrorRate,
		},
	}
}

func getServiceType(svc config.ServiceConfig) string {
	if svc.Type != "" {
		return svc.Type
	}
	if svc.System == "postgresql" || svc.System == "mysql" {
		return "database"
	}
	if svc.System == "kafka" || svc.System == "rabbitmq" {
		return "messaging"
	}
	return "http"
}

func (h *Handler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := strings.Replace(dashboardHTML, "{{VERSION}}", h.version, 1)
	fmt.Fprint(w, html)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Semantix - Telemetry Simulation Dashboard</title>
    <link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>🔭</text></svg>">
    <script src="https://d3js.org/d3.v7.min.js"></script>
    <!-- CodeMirror for YAML syntax highlighting -->
    <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.16/codemirror.min.css">
    <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.16/theme/material-darker.min.css">
    <script src="https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.16/codemirror.min.js"></script>
    <script src="https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.16/mode/yaml/yaml.min.js"></script>
    <style>
        :root {
            --bg-primary: #0a0e14;
            --bg-glass: rgba(22, 27, 34, 0.6);
            --bg-glass-hover: rgba(30, 37, 46, 0.75);
            --border: rgba(48, 54, 61, 0.65);
            --border-subtle: rgba(48, 54, 61, 0.4);
            --text-primary: #e0e0e0;
            --text-secondary: #a5a5a5;
            --text-muted: rgba(165, 165, 165, 0.6);
            --accent-blue: #58a6ff;
            --accent-green: #3fb950;
            --accent-orange: #d29922;
            --accent-red: #f85149;
            --accent-purple: #bc8cff;
            --font-mono: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
            --font-sans: 'Helvetica Neue', 'Helvetica', 'Arial', sans-serif;
        }

        *, *::before, *::after {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        html {
            font-size: 16px;
            -webkit-font-smoothing: antialiased;
        }

        body {
            background: var(--bg-primary);
            color: var(--text-primary);
            font-family: var(--font-sans);
            font-weight: 400;
            line-height: 1.5;
            min-height: 100vh;
        }

        /* Header */
        .header {
            padding: 1rem 2rem;
            display: flex;
            align-items: center;
            justify-content: space-between;
            border-bottom: 1px solid var(--border);
        }

        .logo-text {
            font-size: 1.25rem;
            font-weight: 500;
            color: var(--text-primary);
        }

        .logo-subtitle {
            font-size: 0.75rem;
            color: var(--text-muted);
            margin-top: 2px;
        }

        .header-meta {
            display: flex;
            align-items: center;
            gap: 1rem;
        }

        .version {
            font-size: 0.7rem;
            color: var(--text-muted);
            font-family: var(--font-mono);
        }

        .github-link {
            font-size: 0.75rem;
            color: var(--text-secondary);
            text-decoration: none;
            padding: 0.4rem 0.8rem;
            border: 1px solid var(--border);
            border-radius: 4px;
        }

        .github-link:hover {
            color: var(--text-primary);
            border-color: var(--text-secondary);
        }

        .header-stats {
            display: flex;
            gap: 2rem;
        }

        .header-stat {
            text-align: center;
        }

        .header-stat-value {
            font-size: 1.25rem;
            font-weight: 500;
            color: var(--text-primary);
        }

        .header-stat-label {
            font-size: 0.65rem;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.1em;
        }

        /* Main Layout */
        .main-container {
            display: grid;
            grid-template-columns: 1fr 320px 400px;
            height: calc(100vh - 60px);
        }

        /* Topology View */
        .topology-container {
            position: relative;
            overflow: hidden;
            background: var(--bg-primary);
        }

        #topology {
            width: 100%;
            height: 100%;
        }

        /* Service Nodes */
        .node {
            cursor: pointer;
        }

        .node-bg {
            fill: var(--bg-glass);
            stroke: var(--border);
            stroke-width: 1;
            rx: 6;
        }

        .node.selected .node-bg {
            stroke: var(--accent-blue);
            stroke-width: 2;
        }

        .node.http .node-accent { fill: var(--accent-blue); }
        .node.database .node-accent { fill: var(--accent-purple); }
        .node.messaging .node-accent { fill: var(--accent-orange); }

        .node-name {
            fill: var(--text-primary);
            font-size: 12px;
            font-weight: 500;
            font-family: var(--font-sans);
        }

        .node-type {
            fill: var(--text-muted);
            font-size: 10px;
            font-family: var(--font-mono);
        }

        .node-rpm {
            fill: var(--text-secondary);
            font-size: 10px;
            font-family: var(--font-mono);
        }

        .node-anomaly {
            fill: var(--accent-red);
            font-size: 10px;
        }

        /* Links */
        .link {
            fill: none;
            stroke-opacity: 0.5;
        }

        .link.sync {
            stroke: var(--accent-blue);
            stroke-width: 1;
        }

        .link.async {
            stroke: var(--accent-orange);
            stroke-width: 1;
            stroke-dasharray: 4, 3;
        }

        .link.highlighted {
            stroke-opacity: 0.9;
            stroke-width: 2;
        }

        .link.dimmed {
            stroke-opacity: 0.15;
        }

        /* Detail Panel */
        .detail-panel {
            background: rgba(22, 27, 34, 0.4);
            border-left: 1px solid var(--border);
            padding: 1.5rem;
            overflow-y: auto;
        }

        .panel-header {
            margin-bottom: 1.5rem;
        }

        .panel-title-row {
            display: flex;
            justify-content: space-between;
            align-items: flex-start;
            margin-bottom: 4px;
        }

        .panel-title {
            font-size: 1.1rem;
            font-weight: 500;
            color: var(--text-primary);
        }

        .view-toggle {
            display: flex;
            gap: 2px;
            background: var(--bg-glass);
            border-radius: 4px;
            padding: 2px;
        }

        .view-toggle button {
            background: none;
            border: none;
            color: var(--text-muted);
            font-size: 0.7rem;
            padding: 4px 8px;
            border-radius: 3px;
            cursor: pointer;
            font-family: var(--font-sans);
        }

        .view-toggle button.active {
            background: var(--accent-blue);
            color: var(--bg-primary);
        }

        .jump-to-yaml-btn {
            background: var(--accent-blue);
            color: var(--bg-primary);
            border: none;
            padding: 6px 12px;
            border-radius: 4px;
            font-size: 0.7rem;
            font-weight: 500;
            cursor: pointer;
            font-family: var(--font-sans);
            transition: all 0.2s;
        }

        .jump-to-yaml-btn:hover {
            background: #79b8ff;
        }

        .panel-subtitle {
            font-size: 0.75rem;
            color: var(--text-muted);
            font-family: var(--font-mono);
        }

        .service-badge {
            display: inline-block;
            padding: 4px 8px;
            border-radius: 3px;
            font-size: 0.65rem;
            font-weight: 500;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-top: 0.75rem;
            border: 1px solid var(--border);
            color: var(--text-secondary);
        }

        /* Stats Grid */
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(2, 1fr);
            gap: 0.75rem;
            margin-bottom: 1.5rem;
        }

        .stat-card {
            background: var(--bg-glass);
            border: 1px solid var(--border-subtle);
            border-radius: 6px;
            padding: 1rem;
        }

        .stat-card-value {
            font-size: 1.25rem;
            font-weight: 500;
            color: var(--text-primary);
            margin-bottom: 2px;
        }

        .stat-card-label {
            font-size: 0.65rem;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }

        /* Endpoints List */
        .section-title {
            font-size: 0.7rem;
            font-weight: 500;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.1em;
            margin-bottom: 1rem;
        }

        .endpoint-card {
            background: var(--bg-glass);
            border: 1px solid var(--border-subtle);
            border-radius: 6px;
            padding: 1rem;
            margin-bottom: 0.75rem;
        }

        .endpoint-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-bottom: 0.5rem;
        }

        .endpoint-name {
            font-weight: 500;
            font-size: 0.85rem;
            color: var(--text-primary);
        }

        .endpoint-method {
            font-size: 0.6rem;
            font-weight: 500;
            padding: 2px 6px;
            border-radius: 3px;
            font-family: var(--font-mono);
            background: var(--bg-glass);
            color: var(--text-secondary);
            border: 1px solid var(--border-subtle);
        }

        .endpoint-route {
            font-size: 0.75rem;
            color: var(--text-muted);
            font-family: var(--font-mono);
            margin-bottom: 0.75rem;
        }

        .endpoint-metrics {
            display: grid;
            grid-template-columns: repeat(3, 1fr);
            gap: 0.5rem;
        }

        .endpoint-metric {
            text-align: center;
            padding: 0.5rem;
            background: rgba(0,0,0,0.2);
            border-radius: 4px;
        }

        .endpoint-metric-value {
            font-size: 0.85rem;
            font-weight: 500;
            color: var(--text-primary);
        }

        .endpoint-metric-label {
            font-size: 0.6rem;
            color: var(--text-muted);
            text-transform: uppercase;
        }

        .endpoint-calls {
            margin-top: 0.75rem;
            padding-top: 0.75rem;
            border-top: 1px solid var(--border-subtle);
        }

        .endpoint-calls-label {
            font-size: 0.6rem;
            color: var(--text-muted);
            text-transform: uppercase;
            margin-bottom: 0.5rem;
        }

        .endpoint-call {
            display: inline-block;
            font-size: 0.7rem;
            padding: 2px 6px;
            background: rgba(0,0,0,0.2);
            border-radius: 3px;
            margin-right: 4px;
            margin-bottom: 4px;
            color: var(--text-secondary);
            font-family: var(--font-mono);
        }

        .anomaly-badge {
            font-size: 0.6rem;
            padding: 2px 6px;
            background: rgba(248, 81, 73, 0.15);
            color: var(--accent-red);
            border-radius: 3px;
            margin-left: 6px;
        }

        /* Empty State */
        .empty-state {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            height: 100%;
            text-align: center;
            padding: 2rem;
        }

        .empty-state-title {
            font-size: 1rem;
            font-weight: 500;
            margin-bottom: 0.5rem;
            color: var(--text-primary);
        }

        .empty-state-text {
            font-size: 0.8rem;
            color: var(--text-muted);
            max-width: 240px;
        }

        /* Legend */
        .legend {
            position: absolute;
            bottom: 1rem;
            left: 1rem;
            background: var(--bg-glass);
            border: 1px solid var(--border);
            border-radius: 6px;
            padding: 0.75rem 1rem;
            display: flex;
            gap: 1.25rem;
        }

        .legend-item {
            display: flex;
            align-items: center;
            gap: 6px;
            font-size: 0.7rem;
            color: var(--text-muted);
        }

        .legend-color {
            width: 10px;
            height: 10px;
            border-radius: 2px;
        }

        .legend-line {
            width: 20px;
            height: 2px;
            border-radius: 1px;
        }

        .legend-line.dashed {
            background: repeating-linear-gradient(
                90deg,
                var(--accent-orange) 0px,
                var(--accent-orange) 4px,
                transparent 4px,
                transparent 7px
            );
        }

        /* Volume Bar */
        .volume-bar {
            height: 3px;
            background: rgba(0,0,0,0.3);
            border-radius: 2px;
            margin-top: 6px;
            overflow: hidden;
        }

        .volume-bar-fill {
            height: 100%;
            background: var(--accent-blue);
            border-radius: 2px;
        }

        /* Scrollbar */
        ::-webkit-scrollbar {
            width: 6px;
        }

        ::-webkit-scrollbar-track {
            background: transparent;
        }

        ::-webkit-scrollbar-thumb {
            background: var(--border);
            border-radius: 3px;
        }

        /* Editor Panel */
        .editor-panel {
            background: rgba(22, 27, 34, 0.4);
            border-left: 1px solid var(--border);
            display: flex;
            flex-direction: column;
            overflow: hidden;
            min-height: 0;
        }

        .editor-tabs {
            display: flex;
            border-bottom: 1px solid var(--border);
            background: var(--bg-glass);
        }

        .editor-tab {
            padding: 0.75rem 1rem;
            font-size: 0.75rem;
            color: var(--text-muted);
            cursor: pointer;
            border: none;
            background: none;
            font-family: var(--font-sans);
            border-bottom: 2px solid transparent;
            transition: all 0.2s;
        }

        .editor-tab:hover {
            color: var(--text-secondary);
            background: rgba(255,255,255,0.03);
        }

        .editor-tab.active {
            color: var(--accent-blue);
            border-bottom-color: var(--accent-blue);
        }

        .editor-tab.modified::after {
            content: '*';
            color: var(--accent-orange);
            margin-left: 4px;
        }

        .editor-content {
            flex: 1;
            overflow: hidden;
            display: flex;
            flex-direction: column;
            min-height: 0;
        }

        .editor-textarea {
            flex: 1;
            width: 100%;
            padding: 1rem;
            background: rgba(0,0,0,0.3);
            border: none;
            color: var(--text-secondary);
            font-family: var(--font-mono);
            font-size: 0.75rem;
            line-height: 1.6;
            resize: none;
            outline: none;
        }

        .editor-textarea:focus {
            background: rgba(0,0,0,0.4);
        }

        /* CodeMirror customization */
        .CodeMirror {
            flex: 1;
            height: auto !important;
            background: rgba(0,0,0,0.3);
            font-family: var(--font-mono);
            font-size: 0.75rem;
            line-height: 1.6;
        }

        .CodeMirror-focused {
            background: rgba(0,0,0,0.4);
        }

        .CodeMirror-gutters {
            background: rgba(0,0,0,0.2);
            border-right: 1px solid var(--border);
        }

        .CodeMirror-linenumber {
            color: var(--text-muted);
        }

        .CodeMirror-cursor {
            border-left: 1px solid var(--accent-blue);
        }

        .CodeMirror-selected {
            background: rgba(88, 166, 255, 0.2) !important;
        }

        .cm-s-material-darker .cm-atom { color: var(--accent-orange); }
        .cm-s-material-darker .cm-number { color: var(--accent-orange); }
        .cm-s-material-darker .cm-keyword { color: var(--accent-purple); }
        .cm-s-material-darker .cm-string { color: var(--accent-green); }
        .cm-s-material-darker .cm-comment { color: var(--text-muted); }
        .cm-s-material-darker .cm-meta { color: var(--accent-blue); }

        #editor-container {
            flex: 1;
            overflow: hidden;
            min-height: 0;
        }

        #editor-container .CodeMirror {
            height: 100%;
        }

        .editor-tabs {
            flex-shrink: 0;
        }

        .editor-toolbar {
            display: flex;
            gap: 0.75rem;
            padding: 1rem 1.25rem;
            border-top: 1px solid var(--border);
            background: rgba(22, 27, 34, 0.95);
            align-items: center;
            flex-shrink: 0;
        }

        .editor-btn {
            padding: 0.6rem 1.25rem;
            font-size: 0.8rem;
            font-weight: 500;
            border-radius: 6px;
            cursor: pointer;
            font-family: var(--font-sans);
            transition: all 0.2s;
        }

        .editor-btn-primary {
            background: var(--accent-blue);
            color: var(--bg-primary);
            border: none;
        }

        .editor-btn-primary:hover {
            background: #79b8ff;
        }

        .editor-btn-primary:disabled {
            background: var(--border);
            color: var(--text-muted);
            cursor: not-allowed;
        }

        .editor-btn-secondary {
            background: transparent;
            color: var(--text-secondary);
            border: 1px solid var(--border);
        }

        .editor-btn-secondary:hover {
            color: var(--text-primary);
            border-color: var(--text-secondary);
        }

        .editor-btn-danger {
            background: transparent;
            color: var(--accent-red);
            border: 1px solid var(--accent-red);
        }

        .editor-btn-danger:hover {
            background: rgba(248, 81, 73, 0.15);
        }

        .editor-status {
            flex: 1;
            font-size: 0.7rem;
            color: var(--text-muted);
            text-align: right;
        }

        .editor-status.pending {
            color: var(--accent-orange);
        }

        .editor-status.saved {
            color: var(--accent-green);
        }

        .editor-status.error {
            color: var(--accent-red);
        }

        /* Changelog Panel */
        .changelog-panel {
            padding: 1rem;
            overflow-y: auto;
            flex: 1;
        }

        .changelog-entry {
            background: var(--bg-glass);
            border: 1px solid var(--border-subtle);
            border-radius: 6px;
            padding: 0.75rem;
            margin-bottom: 0.75rem;
        }

        .changelog-time {
            font-size: 0.65rem;
            color: var(--text-muted);
            font-family: var(--font-mono);
        }

        .changelog-summary {
            font-size: 0.8rem;
            color: var(--text-primary);
            margin-top: 0.25rem;
        }

        .changelog-changes {
            margin-top: 0.5rem;
            padding-top: 0.5rem;
            border-top: 1px solid var(--border-subtle);
        }

        .changelog-change {
            font-size: 0.7rem;
            color: var(--text-secondary);
            font-family: var(--font-mono);
        }

        .changelog-empty {
            font-size: 0.8rem;
            color: var(--text-muted);
            text-align: center;
            padding: 2rem;
        }
    </style>
</head>
<body>
    <header class="header">
        <div class="logo">
            <div>
                <div class="logo-text">Semantix</div>
                <div class="logo-subtitle">OpenTelemetry Simulation Engine</div>
            </div>
        </div>
        <div class="header-meta">
            <span class="version">{{VERSION}}</span>
            <a href="https://github.com/mreider/semantix" target="_blank" rel="noopener noreferrer" class="github-link">GitHub</a>
        </div>
        <div class="header-stats">
            <div class="header-stat">
                <div class="header-stat-value" id="total-services">-</div>
                <div class="header-stat-label">Services</div>
            </div>
            <div class="header-stat">
                <div class="header-stat-value" id="total-rpm">-</div>
                <div class="header-stat-label">Req/min</div>
            </div>
            <div class="header-stat">
                <div class="header-stat-value" id="total-links">-</div>
                <div class="header-stat-label">Connections</div>
            </div>
        </div>
    </header>

    <div class="main-container">
        <div class="topology-container">
            <svg id="topology"></svg>
            <div class="legend">
                <div class="legend-item">
                    <div class="legend-color" style="background: var(--accent-blue);"></div>
                    <span>HTTP Service</span>
                </div>
                <div class="legend-item">
                    <div class="legend-color" style="background: var(--accent-purple);"></div>
                    <span>Database</span>
                </div>
                <div class="legend-item">
                    <div class="legend-color" style="background: var(--accent-orange);"></div>
                    <span>Messaging</span>
                </div>
                <div class="legend-item">
                    <div class="legend-line" style="background: var(--accent-blue);"></div>
                    <span>Sync Call</span>
                </div>
                <div class="legend-item">
                    <div class="legend-line dashed"></div>
                    <span>Async Call</span>
                </div>
            </div>
        </div>

        <div class="detail-panel" id="detail-panel">
            <div class="empty-state">
                <div class="empty-state-icon"></div>
                <div class="empty-state-title">Select a Service</div>
                <div class="empty-state-text">Click on any service node to view detailed telemetry information and endpoint configuration.</div>
            </div>
        </div>

        <div class="editor-panel" id="editor-panel">
            <div class="editor-tabs">
                <button class="editor-tab active" data-tab="services" onclick="switchEditorTab('services')">Services</button>
                <button class="editor-tab" data-tab="problem_patterns" onclick="switchEditorTab('problem_patterns')">Problem Patterns</button>
                <button class="editor-tab" data-tab="scenarios" onclick="switchEditorTab('scenarios')">Scenarios</button>
                <button class="editor-tab" data-tab="changelog" onclick="switchEditorTab('changelog')">Changelog</button>
            </div>
            <div class="editor-content" id="editor-content">
                <div id="editor-container"></div>
            </div>
            <div class="editor-toolbar">
                <button class="editor-btn editor-btn-primary" id="save-btn" onclick="saveAllChanges()" disabled>Save All</button>
                <button class="editor-btn editor-btn-secondary" id="discard-btn" onclick="discardChanges()" disabled>Discard</button>
                <button class="editor-btn editor-btn-danger" id="reset-btn" onclick="resetToDefaults()">Reset</button>
                <span class="editor-status" id="editor-status">No pending changes</span>
            </div>
        </div>
    </div>

    <script>
        // Fetch topology data and render
        async function init() {
            try {
                console.log('Initializing dashboard...');
                const response = await fetch('/api/topology');
                const data = await response.json();
                console.log('Topology data:', data);
                
                // Update header stats
                document.getElementById('total-services').textContent = data.stats.totalServices;
                document.getElementById('total-rpm').textContent = formatNumber(data.stats.totalRpm);
                document.getElementById('total-links').textContent = data.stats.totalLinks;
                
                renderTopology(data);
                
                // Auto-select frontend service on initial load
                const frontend = data.nodes.find(n => n.name === 'frontend');
                if (frontend) {
                    showServiceDetail(frontend, data);
                }
            } catch (error) {
                console.error('Failed to fetch topology:', error);
                document.querySelector('.topology-container').innerHTML = '<div style="padding:40px;color:#ff4757;">Error loading topology: ' + error.message + '</div>';
            }
        }

        function formatNumber(num) {
            if (num >= 1000) {
                return (num / 1000).toFixed(1) + 'k';
            }
            return Math.round(num).toString();
        }

        function formatLatency(ms) {
            if (ms >= 1000) {
                return (ms / 1000).toFixed(2) + 's';
            }
            return Math.round(ms) + 'ms';
        }

        function formatPercent(rate) {
            return (rate * 100).toFixed(1) + '%';
        }

        function renderTopology(data) {
            console.log('Rendering topology...');
            const svg = d3.select('#topology');
            const container = document.querySelector('.topology-container');
            const width = container.clientWidth;
            const height = container.clientHeight;
            console.log('Container dimensions:', width, height);

            svg.attr('width', width).attr('height', height);

            // Clear previous content
            svg.selectAll('*').remove();

            // Create definitions for arrows (smaller, fixed size)
            const defs = svg.append('defs');
            
            defs.append('marker')
                .attr('id', 'arrowhead-sync')
                .attr('viewBox', '0 -3 6 6')
                .attr('refX', 5)
                .attr('refY', 0)
                .attr('orient', 'auto')
                .attr('markerWidth', 6)
                .attr('markerHeight', 6)
                .append('path')
                .attr('d', 'M 0,-3 L 6,0 L 0,3')
                .attr('fill', 'var(--accent-blue)');

            defs.append('marker')
                .attr('id', 'arrowhead-async')
                .attr('viewBox', '0 -3 6 6')
                .attr('refX', 5)
                .attr('refY', 0)
                .attr('orient', 'auto')
                .attr('markerWidth', 6)
                .attr('markerHeight', 6)
                .append('path')
                .attr('d', 'M 0,-3 L 6,0 L 0,3')
                .attr('fill', 'var(--accent-orange)');

            const g = svg.append('g');

            // --- Static Layout Algorithm ---
            // Assign layers based on service type and dependencies
            const nodeMap = {};
            data.nodes.forEach(n => { nodeMap[n.id] = n; });
            
            // Build incoming edges map
            const incomingEdges = {};
            const outgoingEdges = {};
            data.nodes.forEach(n => { 
                incomingEdges[n.id] = []; 
                outgoingEdges[n.id] = [];
            });
            data.links.forEach(l => {
                const sourceId = typeof l.source === 'object' ? l.source.id : l.source;
                const targetId = typeof l.target === 'object' ? l.target.id : l.target;
                incomingEdges[targetId].push(sourceId);
                outgoingEdges[sourceId].push(targetId);
            });
            
            // Categorize services into layers
            const layers = { 0: [], 1: [], 2: [], 3: [] };
            data.nodes.forEach(n => {
                if (n.type === 'database') {
                    layers[3].push(n);
                } else if (n.type === 'messaging') {
                    layers[2].push(n);
                } else if (incomingEdges[n.id].length === 0) {
                    // Entry points (no incoming edges)
                    layers[0].push(n);
                } else {
                    layers[1].push(n);
                }
            });
            
            // Calculate positions
            const nodeWidth = 160;
            const nodeHeight = 90;
            const layerGap = 140;
            const nodeGap = 30;
            const padding = 80;
            
            // Position each layer
            let currentY = padding;
            for (let layer = 0; layer <= 3; layer++) {
                const nodesInLayer = layers[layer];
                if (nodesInLayer.length === 0) continue;
                
                const totalWidth = nodesInLayer.length * nodeWidth + (nodesInLayer.length - 1) * nodeGap;
                let startX = (width - totalWidth) / 2;
                
                nodesInLayer.forEach((n, i) => {
                    n.x = startX + i * (nodeWidth + nodeGap) + nodeWidth / 2;
                    n.y = currentY + nodeHeight / 2;
                });
                
                currentY += nodeHeight + layerGap;
            }

            // Create link data with resolved source/target
            const linkData = data.links.map(l => ({
                source: nodeMap[typeof l.source === 'object' ? l.source.id : l.source],
                target: nodeMap[typeof l.target === 'object' ? l.target.id : l.target],
                type: l.type,
                endpoint: l.endpoint,
                rpm: l.rpm
            }));

            // Create nodes FIRST (so links render on top)
            const node = g.append('g')
                .attr('class', 'nodes-layer')
                .selectAll('g')
                .data(data.nodes)
                .join('g')
                .attr('class', d => 'node ' + d.type)
                .attr('transform', d => 'translate(' + d.x + ',' + d.y + ')');

            // Node background
            node.append('rect')
                .attr('class', 'node-bg')
                .attr('width', 140)
                .attr('height', 80)
                .attr('x', -70)
                .attr('y', -40);

            // Accent bar (shortened to avoid overlapping rounded corners)
            node.append('rect')
                .attr('class', 'node-accent')
                .attr('width', 4)
                .attr('height', 68)
                .attr('x', -70)
                .attr('y', -34)
                .attr('rx', 2);

            // Service name
            node.append('text')
                .attr('class', 'node-name')
                .attr('x', -58)
                .attr('y', -15)
                .text(d => d.name);

            // Service type
            node.append('text')
                .attr('class', 'node-type')
                .attr('x', -58)
                .attr('y', 0)
                .text(d => d.system || d.type);

            // RPM badge
            node.append('text')
                .attr('class', 'node-rpm')
                .attr('x', -58)
                .attr('y', 20)
                .text(d => d.totalRpm > 0 ? formatNumber(d.totalRpm) + ' rpm' : '');

            // Anomaly indicator
            node.filter(d => d.hasAnomalies)
                .append('text')
                .attr('class', 'node-anomaly')
                .attr('x', 50)
                .attr('y', -25)
                .text('!');

            // Create links AFTER nodes (curved paths that connect to node edges)
            const link = g.append('g')
                .attr('class', 'links-layer')
                .selectAll('path')
                .data(linkData)
                .join('path')
                .attr('class', d => 'link ' + d.type)
                .attr('marker-end', d => 'url(#arrowhead-' + d.type + ')')
                .attr('d', d => {
                    const sx = d.source.x;
                    const sy = d.source.y + 40;  // Bottom of source node
                    const tx = d.target.x;
                    const ty = d.target.y - 40;  // Top of target node
                    // Curved path
                    const midY = (sy + ty) / 2;
                    return 'M ' + sx + ' ' + sy + ' C ' + sx + ' ' + midY + ', ' + tx + ' ' + midY + ', ' + tx + ' ' + ty;
                });

            // Click handler for nodes
            node.on('click', function(event, d) {
                event.stopPropagation();
                
                // Deselect previous
                d3.selectAll('.node').classed('selected', false);
                d3.selectAll('.link').classed('highlighted', false).classed('dimmed', false);
                
                // Select new
                d3.select(this).classed('selected', true);
                
                // Highlight connected links
                link.classed('highlighted', l => l.source.id === d.id || l.target.id === d.id);
                link.classed('dimmed', l => l.source.id !== d.id && l.target.id !== d.id);
                
                showServiceDetail(d, data);
            });
            
            // Center the diagram
            const bounds = g.node().getBBox();
            const scale = Math.min(
                (width - 40) / bounds.width,
                (height - 40) / bounds.height,
                1
            );
            const translateX = (width - bounds.width * scale) / 2 - bounds.x * scale;
            const translateY = (height - bounds.height * scale) / 2 - bounds.y * scale;
            g.attr('transform', 'translate(' + translateX + ',' + translateY + ') scale(' + scale + ')');
        }

        function showEmptyState() {
            const panel = document.getElementById('detail-panel');
            panel.innerHTML = ` + "`" + `
                <div class="empty-state">
                    <div class="empty-state-icon"></div>
                    <div class="empty-state-title">Select a Service</div>
                    <div class="empty-state-text">Click on any service node to view its configuration. This shows what telemetry will be simulated, not live data.</div>
                </div>
            ` + "`" + `;
        }

        let currentService = null;
        let currentTopology = null;

        function showServiceDetail(service, topology) {
            currentService = service;
            currentTopology = topology;
            renderServiceView();
        }

        function renderServiceView() {
            const service = currentService;
            const topology = currentTopology;
            if (!service) return;

            const panel = document.getElementById('detail-panel');
            const maxRpm = Math.max(...topology.nodes.map(n => n.totalRpm), 1);
            
            let html = ` + "`" + `
                <div class="panel-header">
                    <div class="panel-title-row">
                        <div class="panel-title">${service.name}</div>
                        <button class="jump-to-yaml-btn" onclick="jumpToServiceYaml('${service.name}')">Edit YAML</button>
                    </div>
                    <div class="panel-subtitle">${service.version || 'No version'}</div>
                    <div class="service-badge ${service.type}">
                        ${getServiceIcon(service.type)} ${service.system || service.type}
                    </div>
                </div>
            ` + "`" + `;

            html += ` + "`" + `
                <div class="stats-grid">
                    <div class="stat-card">
                        <div class="stat-card-value rpm">${formatNumber(service.totalRpm)}</div>
                        <div class="stat-card-label">Requests/min</div>
                        <div class="volume-bar">
                            <div class="volume-bar-fill" style="width: ${(service.totalRpm / maxRpm * 100)}%"></div>
                        </div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-card-value latency">${formatLatency(service.avgLatencyMs)}</div>
                        <div class="stat-card-label">Avg Latency</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-card-value errors">${formatPercent(service.errorRate)}</div>
                        <div class="stat-card-label">Error Rate</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-card-value endpoints">${service.endpoints.length}</div>
                        <div class="stat-card-label">Endpoints</div>
                    </div>
                </div>
                
                <div class="endpoints-section">
                    <div class="section-title">Endpoints</div>
            ` + "`" + `;

            for (const ep of service.endpoints) {
                const methodLabel = ep.method || ep.operation || getEndpointTypeLabel(ep.type);
                const routeLabel = ep.route || (ep.table ? ` + "`" + `${ep.operation} ${ep.table}` + "`" + ` : '') || ep.topic || '';
                
                html += ` + "`" + `
                    <div class="endpoint-card">
                        <div class="endpoint-header">
                            <span class="endpoint-name">${ep.name}</span>
                            <span class="endpoint-method ${methodLabel}">${methodLabel}</span>
                            ${ep.hasAnomalies ? '<span class="anomaly-badge">Anomalies</span>' : ''}
                        </div>
                        ${routeLabel ? ` + "`" + `<div class="endpoint-route">${routeLabel}</div>` + "`" + ` : ''}
                        <div class="endpoint-metrics">
                            <div class="endpoint-metric">
                                <div class="endpoint-metric-value" style="color: var(--accent-cyan);">${formatNumber(ep.rpm)}</div>
                                <div class="endpoint-metric-label">RPM</div>
                            </div>
                            <div class="endpoint-metric">
                                <div class="endpoint-metric-value" style="color: var(--accent-blue);">${formatLatency(ep.p50Ms)}</div>
                                <div class="endpoint-metric-label">P50</div>
                            </div>
                            <div class="endpoint-metric">
                                <div class="endpoint-metric-value" style="color: var(--accent-purple);">${formatLatency(ep.p95Ms || ep.p50Ms * 2)}</div>
                                <div class="endpoint-metric-label">P95</div>
                            </div>
                        </div>
                        ${ep.errorRate > 0 ? ` + "`" + `
                            <div class="endpoint-metrics" style="margin-top: 8px;">
                                <div class="endpoint-metric">
                                    <div class="endpoint-metric-value" style="color: var(--accent-red);">${formatPercent(ep.errorRate)}</div>
                                    <div class="endpoint-metric-label">Errors</div>
                                </div>
                                <div class="endpoint-metric">
                                    <div class="endpoint-metric-value" style="color: var(--text-secondary);">${(ep.errorCodes && ep.errorCodes.join(', ')) || '-'}</div>
                                    <div class="endpoint-metric-label">Codes</div>
                                </div>
                                <div class="endpoint-metric">
                                    <div class="endpoint-metric-value" style="color: var(--accent-orange);">${formatLatency(ep.p99Ms || ep.p50Ms * 4)}</div>
                                    <div class="endpoint-metric-label">P99</div>
                                </div>
                            </div>
                        ` + "`" + ` : ''}
                        ${ep.calls && ep.calls.length > 0 ? ` + "`" + `
                            <div class="endpoint-calls">
                                <div class="endpoint-calls-label">Downstream Calls</div>
                                ${ep.calls.map(c => ` + "`" + `<span class="endpoint-call">${c}</span>` + "`" + `).join('')}
                            </div>
                        ` + "`" + ` : ''}
                    </div>
                ` + "`" + `;
            }

            html += '</div>';

            panel.innerHTML = html;
        }

        function jumpToServiceYaml(serviceName) {
            // Switch to Services tab if not already there
            if (currentEditorTab !== 'services') {
                switchEditorTab('services');
            }
            
            // Wait a moment for editor to load if switching tabs
            setTimeout(() => {
                if (!cmEditor) return;
                
                // Search for the service name in the YAML
                const content = cmEditor.getValue();
                const lines = content.split('\n');
                
                // Look for "- name: serviceName" or "name: serviceName"
                for (let i = 0; i < lines.length; i++) {
                    const line = lines[i];
                    if (line.match(new RegExp('[-\\s]*name:\\s*["\']?' + serviceName + '["\']?\\s*$', 'i')) ||
                        line.match(new RegExp('^\\s*-\\s*name:\\s*["\']?' + serviceName + '["\']?', 'i'))) {
                        // Found the service, scroll to it and highlight
                        cmEditor.setCursor(i, 0);
                        cmEditor.scrollIntoView({line: i, ch: 0}, 100);
                        
                        // Select the entire service block for visibility
                        cmEditor.setSelection({line: i, ch: 0}, {line: i, ch: lines[i].length});
                        cmEditor.focus();
                        return;
                    }
                }
                
                // Fallback: just focus the editor
                cmEditor.focus();
            }, currentEditorTab !== 'services' ? 200 : 50);
        }

        function getServiceIcon(type) {
            switch (type) {
                case 'database': return 'DB';
                case 'messaging': return 'MQ';
                default: return 'API';
            }
        }

        function getEndpointTypeLabel(type) {
            if (type && type.includes('consumer')) return 'CONSUMER';
            if (type && type.includes('producer')) return 'PRODUCER';
            return (type && type.toUpperCase()) || 'HTTP';
        }

        // ===== Configuration Editor =====
        let currentEditorTab = 'services';
        let originalContent = {};
        let pendingChanges = {};
        let isChangelogView = false;
        let cmEditor = null;

        async function loadEditorContent(tab) {
            const content = document.getElementById('editor-content');
            
            if (tab === 'changelog') {
                isChangelogView = true;
                if (cmEditor) {
                    cmEditor.toTextArea();
                    cmEditor = null;
                }
                await renderChangelog();
                return;
            }
            
            isChangelogView = false;
            content.innerHTML = '<div id="editor-container"></div>';
            
            try {
                const response = await fetch('/api/config/' + tab);
                const yaml = await response.text();
                originalContent[tab] = yaml;
                
                // Show pending change if exists, otherwise show original
                const initialValue = pendingChanges[tab] !== undefined ? pendingChanges[tab] : yaml;
                
                // Create CodeMirror instance
                const container = document.getElementById('editor-container');
                cmEditor = CodeMirror(container, {
                    value: initialValue,
                    mode: 'yaml',
                    theme: 'material-darker',
                    lineNumbers: true,
                    indentUnit: 2,
                    tabSize: 2,
                    indentWithTabs: false,
                    lineWrapping: true,
                    autofocus: true
                });
                
                // Listen for changes
                cmEditor.on('change', () => onEditorChange(tab));
                
                // Ensure CodeMirror fills the container
                cmEditor.setSize('100%', '100%');
                
            } catch (error) {
                console.error('Failed to load config:', error);
                content.innerHTML = '<div style="padding: 1rem; color: var(--accent-red);"># Error loading configuration: ' + error.message + '</div>';
            }
        }

        function onEditorChange(tab) {
            if (!cmEditor) return;
            const newValue = cmEditor.getValue();
            
            // Check if changed from original
            if (newValue !== originalContent[tab]) {
                pendingChanges[tab] = newValue;
            } else {
                delete pendingChanges[tab];
            }
            
            updateEditorUI();
        }

        function updateEditorUI() {
            const hasPending = Object.keys(pendingChanges).length > 0;
            
            document.getElementById('save-btn').disabled = !hasPending;
            document.getElementById('discard-btn').disabled = !hasPending;
            
            const status = document.getElementById('editor-status');
            if (hasPending) {
                const sections = Object.keys(pendingChanges);
                status.textContent = 'Pending: ' + sections.join(', ');
                status.className = 'editor-status pending';
            } else {
                status.textContent = 'No pending changes';
                status.className = 'editor-status';
            }
            
            // Update tab modified indicators
            document.querySelectorAll('.editor-tab').forEach(tab => {
                const tabName = tab.dataset.tab;
                if (pendingChanges[tabName] !== undefined) {
                    tab.classList.add('modified');
                } else {
                    tab.classList.remove('modified');
                }
            });
        }

        function switchEditorTab(tab) {
            // Save current content if modified
            if (!isChangelogView && cmEditor) {
                const currentValue = cmEditor.getValue();
                if (currentValue !== originalContent[currentEditorTab]) {
                    pendingChanges[currentEditorTab] = currentValue;
                }
            }
            
            // Update tab styles
            document.querySelectorAll('.editor-tab').forEach(t => t.classList.remove('active'));
            document.querySelector('[data-tab="' + tab + '"]').classList.add('active');
            
            currentEditorTab = tab;
            loadEditorContent(tab);
        }

        async function saveAllChanges() {
            const status = document.getElementById('editor-status');
            status.textContent = 'Saving...';
            status.className = 'editor-status pending';
            
            try {
                // Save current editor content first
                if (!isChangelogView && cmEditor) {
                    const currentValue = cmEditor.getValue();
                    if (currentValue !== originalContent[currentEditorTab]) {
                        pendingChanges[currentEditorTab] = currentValue;
                    }
                }
                
                // Submit each pending change
                for (const [section, yaml] of Object.entries(pendingChanges)) {
                    const response = await fetch('/api/config/' + section, {
                        method: 'POST',
                        headers: { 'Content-Type': 'text/plain' },
                        body: yaml
                    });
                    
                    if (!response.ok) {
                        const error = await response.text();
                        throw new Error(section + ': ' + error);
                    }
                }
                
                // Apply all changes
                const saveResponse = await fetch('/api/config/save', { method: 'POST' });
                if (!saveResponse.ok) {
                    throw new Error('Failed to apply changes');
                }
                
                const result = await saveResponse.json();
                
                // Clear pending changes and update originals
                for (const section of Object.keys(pendingChanges)) {
                    originalContent[section] = pendingChanges[section];
                }
                pendingChanges = {};
                
                status.textContent = 'Saved! ' + result.changelog?.summary || '';
                status.className = 'editor-status saved';
                
                // Refresh topology
                init();
                
                setTimeout(() => updateEditorUI(), 2000);
                
            } catch (error) {
                status.textContent = 'Error: ' + error.message;
                status.className = 'editor-status error';
            }
        }

        async function discardChanges() {
            pendingChanges = {};
            loadEditorContent(currentEditorTab);
            updateEditorUI();
            
            const status = document.getElementById('editor-status');
            status.textContent = 'Changes discarded';
            status.className = 'editor-status';
        }

        async function resetToDefaults() {
            if (!confirm('Reset all configuration to defaults? This cannot be undone.')) {
                return;
            }
            
            const status = document.getElementById('editor-status');
            status.textContent = 'Resetting...';
            status.className = 'editor-status pending';
            
            try {
                const response = await fetch('/api/config/reset', { method: 'POST' });
                if (!response.ok) {
                    throw new Error('Reset failed');
                }
                
                pendingChanges = {};
                originalContent = {};
                
                status.textContent = 'Reset to defaults';
                status.className = 'editor-status saved';
                
                // Reload current tab and topology
                loadEditorContent(currentEditorTab);
                init();
                
                setTimeout(() => updateEditorUI(), 2000);
                
            } catch (error) {
                status.textContent = 'Error: ' + error.message;
                status.className = 'editor-status error';
            }
        }

        async function renderChangelog() {
            const content = document.getElementById('editor-content');
            
            try {
                const response = await fetch('/api/config/changelog');
                const changelog = await response.json();
                
                if (!changelog || changelog.length === 0) {
                    content.innerHTML = '<div class="changelog-panel"><div class="changelog-empty">No changes recorded yet.</div></div>';
                    return;
                }
                
                let html = '<div class="changelog-panel">';
                
                // Reverse to show newest first
                for (const entry of changelog.slice().reverse()) {
                    const time = new Date(entry.timestamp).toLocaleString();
                    html += ` + "`" + `
                        <div class="changelog-entry">
                            <div class="changelog-time">${time}</div>
                            <div class="changelog-summary">${entry.summary}</div>
                            <div class="changelog-changes">
                                ${entry.changes.map(c => ` + "`" + `<div class="changelog-change">${c.type}</div>` + "`" + `).join('')}
                            </div>
                        </div>
                    ` + "`" + `;
                }
                
                html += '</div>';
                content.innerHTML = html;
                
            } catch (error) {
                content.innerHTML = '<div class="changelog-panel"><div class="changelog-empty">Error loading changelog: ' + error.message + '</div></div>';
            }
        }

        // Initialize editor on page load
        function initEditor() {
            loadEditorContent('services');
        }

        // Initialize once
        init();
        initEditor();
    </script>
</body>
</html>
`
