// Package dashboard provides the web UI for Semantix.
package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mreider/semantix/internal/config"
)

// Handler serves the dashboard UI and API endpoints.
type Handler struct {
	configs []*config.Config
	version string
}

// New creates a new dashboard handler.
func New(configs []*config.Config, version string) *Handler {
	return &Handler{
		configs: configs,
		version: version,
	}
}

// RegisterRoutes registers all dashboard routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.serveDashboard)
	mux.HandleFunc("/health", h.serveHealth)
	mux.HandleFunc("/api/topology", h.serveTopology)
	mux.HandleFunc("/api/config", h.serveConfig)
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

	// Return sanitized config (without tokens)
	type SafeConfig struct {
		Version    string                  `json:"version"`
		Simulation config.SimulationConfig `json:"simulation"`
		Services   []config.ServiceConfig  `json:"services"`
		Scenarios  []config.ScenarioConfig `json:"scenarios"`
	}

	var safeConfigs []SafeConfig
	for _, cfg := range h.configs {
		safeConfigs = append(safeConfigs, SafeConfig{
			Version:    cfg.Version,
			Simulation: cfg.Simulation,
			Services:   cfg.Services,
			Scenarios:  cfg.Scenarios,
		})
	}

	json.NewEncoder(w).Encode(safeConfigs)
}

func (h *Handler) buildTopology() TopologyData {
	nodes := make([]TopologyNode, 0)
	links := make([]TopologyLink, 0)

	var totalRPM float64
	var totalEndpoints int
	var totalErrorRate float64
	var errorRateCount int

	// Process all configs
	for _, cfg := range h.configs {
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
            grid-template-columns: 1fr 380px;
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

        /* YAML View */
        .yaml-view {
            background: rgba(0,0,0,0.3);
            border: 1px solid var(--border-subtle);
            border-radius: 6px;
            padding: 1rem;
            overflow-x: auto;
        }

        .yaml-view pre {
            margin: 0;
            font-family: var(--font-mono);
            font-size: 0.75rem;
            line-height: 1.6;
            color: var(--text-secondary);
        }

        .yaml-note {
            font-size: 0.7rem;
            color: var(--text-muted);
            margin-bottom: 0.75rem;
            font-style: italic;
        }

        /* YAML syntax highlighting */
        .yaml-key { color: var(--accent-blue); }
        .yaml-string { color: var(--accent-green); }
        .yaml-number { color: var(--accent-orange); }
        .yaml-comment { color: var(--text-muted); font-style: italic; }

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

        let currentView = 'pretty';
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
                        <div class="view-toggle">
                            <button onclick="setView('pretty')" class="${currentView === 'pretty' ? 'active' : ''}">Pretty</button>
                            <button onclick="setView('yaml')" class="${currentView === 'yaml' ? 'active' : ''}">YAML</button>
                        </div>
                    </div>
                    <div class="panel-subtitle">${service.version || 'No version'}</div>
                    <div class="service-badge ${service.type}">
                        ${getServiceIcon(service.type)} ${service.system || service.type}
                    </div>
                </div>
            ` + "`" + `;

            if (currentView === 'yaml') {
                html += ` + "`" + `
                    <div class="yaml-note">Configuration that defines the simulated telemetry:</div>
                    <div class="yaml-view">
                        <pre>${generateYaml(service)}</pre>
                    </div>
                ` + "`" + `;
            } else {
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
            } // end of pretty view

            panel.innerHTML = html;
        }

        function setView(view) {
            currentView = view;
            renderServiceView();
        }

        function generateYaml(service) {
            // Helper functions for syntax highlighting
            const key = (k) => '<span class="yaml-key">' + k + '</span>';
            const str = (s) => '<span class="yaml-string">' + s + '</span>';
            const num = (n) => '<span class="yaml-number">' + n + '</span>';
            const comment = (c) => '<span class="yaml-comment">' + c + '</span>';
            
            let lines = [];
            
            lines.push('- ' + key('name:') + ' ' + str(service.name));
            lines.push('  ' + key('version:') + ' ' + str(service.version || 'v1.0.0'));
            lines.push('  ' + key('type:') + ' ' + str(service.type));
            
            if (service.system) {
                lines.push('  ' + key('system:') + ' ' + str(service.system));
            }
            
            lines.push('  ' + key('endpoints:'));
            
            for (const ep of service.endpoints) {
                lines.push('    - ' + key('name:') + ' ' + str(ep.name));
                lines.push('      ' + key('type:') + ' ' + str(ep.type));
                
                if (ep.method) lines.push('      ' + key('method:') + ' ' + str(ep.method));
                if (ep.route) lines.push('      ' + key('route:') + ' ' + str(ep.route));
                if (ep.operation) lines.push('      ' + key('operation:') + ' ' + str(ep.operation));
                if (ep.table) lines.push('      ' + key('table:') + ' ' + str(ep.table));
                if (ep.topic) lines.push('      ' + key('topic:') + ' ' + str(ep.topic));
                
                lines.push('      ' + key('latency:'));
                lines.push('        ' + key('p50_ms:') + ' ' + num(ep.p50Ms));
                lines.push('        ' + key('p95_ms:') + ' ' + num(ep.p95Ms || ep.p50Ms * 2));
                lines.push('        ' + key('p99_ms:') + ' ' + num(ep.p99Ms || ep.p50Ms * 4));
                
                if (ep.rpm > 0) {
                    lines.push('      ' + key('traffic:'));
                    lines.push('        ' + key('requests_per_minute:') + ' ' + num(ep.rpm));
                }
                
                if (ep.errorRate > 0) {
                    lines.push('      ' + key('errors:'));
                    lines.push('        ' + key('rate:') + ' ' + num(ep.errorRate));
                    if (ep.errorCodes && ep.errorCodes.length > 0) {
                        lines.push('        ' + key('types:'));
                        for (const code of ep.errorCodes) {
                            lines.push('          - ' + key('code:') + ' ' + num(code));
                        }
                    }
                }
                
                if (ep.calls && ep.calls.length > 0) {
                    lines.push('      ' + key('calls:'));
                    for (const call of ep.calls) {
                        const parts = call.split('.');
                        lines.push('        - ' + key('service:') + ' ' + str(parts[0]));
                        lines.push('          ' + key('endpoint:') + ' ' + str(parts[1] || 'default'));
                    }
                }
                
                if (ep.hasAnomalies) {
                    lines.push('      ' + key('anomalies:'));
                    lines.push('        - ' + key('type:') + ' ' + str('latency_spike') + '  ' + comment('# configured'));
                }
            }
            
            return lines.join('\n');
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

        // Initialize once
        init();
    </script>
</body>
</html>
`
