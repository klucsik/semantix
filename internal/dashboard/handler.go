// Package dashboard provides the web UI for Semantix.
package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"

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
	fmt.Fprint(w, dashboardHTML)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Semantix - Telemetry Simulation Dashboard</title>
    <script src="https://d3js.org/d3.v7.min.js"></script>
    <style>
        :root {
            --bg-primary: #0a0a0f;
            --bg-secondary: #12121a;
            --bg-tertiary: #1a1a24;
            --bg-card: #1e1e2a;
            --border-color: #2a2a3a;
            --text-primary: #f0f0f5;
            --text-secondary: #8888a0;
            --text-muted: #5a5a70;
            --accent-blue: #00b4d8;
            --accent-cyan: #00f5d4;
            --accent-purple: #9b5de5;
            --accent-pink: #f15bb5;
            --accent-orange: #ff9500;
            --accent-green: #00c853;
            --accent-red: #ff4757;
            --shadow-glow: 0 0 40px rgba(0, 180, 216, 0.15);
        }

        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: 'SF Pro Display', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            min-height: 100vh;
            overflow-x: hidden;
        }

        /* Header */
        .header {
            background: linear-gradient(180deg, var(--bg-secondary) 0%, transparent 100%);
            padding: 24px 40px;
            display: flex;
            align-items: center;
            justify-content: space-between;
            border-bottom: 1px solid var(--border-color);
            position: relative;
            z-index: 100;
        }

        .logo {
            display: flex;
            align-items: center;
            gap: 16px;
        }

        .logo-icon {
            width: 44px;
            height: 44px;
            background: linear-gradient(135deg, var(--accent-blue), var(--accent-purple));
            border-radius: 12px;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 20px;
            box-shadow: 0 4px 20px rgba(0, 180, 216, 0.3);
        }

        .logo-text {
            font-size: 24px;
            font-weight: 700;
            background: linear-gradient(135deg, var(--text-primary), var(--accent-cyan));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
        }

        .logo-subtitle {
            font-size: 12px;
            color: var(--text-secondary);
            margin-top: 2px;
        }

        .header-stats {
            display: flex;
            gap: 32px;
        }

        .header-stat {
            text-align: center;
        }

        .header-stat-value {
            font-size: 28px;
            font-weight: 700;
            color: var(--accent-cyan);
        }

        .header-stat-label {
            font-size: 11px;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 1px;
        }

        /* Main Layout */
        .main-container {
            display: grid;
            grid-template-columns: 1fr 400px;
            height: calc(100vh - 100px);
        }

        /* Topology View */
        .topology-container {
            position: relative;
            overflow: hidden;
            background: 
                radial-gradient(ellipse at 50% 0%, rgba(0, 180, 216, 0.08) 0%, transparent 50%),
                radial-gradient(ellipse at 80% 80%, rgba(155, 93, 229, 0.05) 0%, transparent 40%);
        }

        #topology {
            width: 100%;
            height: 100%;
        }

        /* Service Nodes */
        .node {
            cursor: pointer;
            transition: transform 0.2s ease;
        }

        .node:hover {
            transform: scale(1.05);
        }

        .node-bg {
            fill: var(--bg-card);
            stroke: var(--border-color);
            stroke-width: 2;
            rx: 16;
            transition: all 0.3s ease;
        }

        .node.selected .node-bg {
            stroke: var(--accent-cyan);
            stroke-width: 3;
            filter: drop-shadow(0 0 20px rgba(0, 245, 212, 0.4));
        }

        .node.http .node-accent { fill: var(--accent-blue); }
        .node.database .node-accent { fill: var(--accent-purple); }
        .node.messaging .node-accent { fill: var(--accent-orange); }

        .node-name {
            fill: var(--text-primary);
            font-size: 14px;
            font-weight: 600;
        }

        .node-type {
            fill: var(--text-muted);
            font-size: 10px;
            text-transform: uppercase;
            letter-spacing: 1px;
        }

        .node-rpm {
            fill: var(--accent-cyan);
            font-size: 12px;
            font-weight: 600;
        }

        .node-anomaly {
            fill: var(--accent-red);
            font-size: 10px;
        }

        /* Links */
        .link {
            fill: none;
            stroke-opacity: 0.6;
            transition: stroke-opacity 0.3s ease;
        }

        .link.sync {
            stroke: var(--accent-blue);
            stroke-width: 2;
        }

        .link.async {
            stroke: var(--accent-orange);
            stroke-width: 2;
            stroke-dasharray: 8, 4;
        }

        .link.highlighted {
            stroke-opacity: 1;
            stroke-width: 3;
        }

        .link.dimmed {
            stroke-opacity: 0.15;
        }

        .link-arrow {
            fill: var(--accent-blue);
        }

        .link.async .link-arrow {
            fill: var(--accent-orange);
        }

        /* Detail Panel */
        .detail-panel {
            background: var(--bg-secondary);
            border-left: 1px solid var(--border-color);
            padding: 24px;
            overflow-y: auto;
        }

        .panel-header {
            margin-bottom: 24px;
        }

        .panel-title {
            font-size: 20px;
            font-weight: 700;
            margin-bottom: 4px;
        }

        .panel-subtitle {
            font-size: 13px;
            color: var(--text-secondary);
        }

        .service-badge {
            display: inline-flex;
            align-items: center;
            gap: 6px;
            padding: 6px 12px;
            border-radius: 20px;
            font-size: 11px;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.5px;
            margin-top: 12px;
        }

        .service-badge.http { background: rgba(0, 180, 216, 0.15); color: var(--accent-blue); }
        .service-badge.database { background: rgba(155, 93, 229, 0.15); color: var(--accent-purple); }
        .service-badge.messaging { background: rgba(255, 149, 0, 0.15); color: var(--accent-orange); }

        /* Stats Grid */
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(2, 1fr);
            gap: 12px;
            margin-bottom: 24px;
        }

        .stat-card {
            background: var(--bg-tertiary);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            padding: 16px;
        }

        .stat-card-value {
            font-size: 24px;
            font-weight: 700;
            margin-bottom: 4px;
        }

        .stat-card-value.rpm { color: var(--accent-cyan); }
        .stat-card-value.latency { color: var(--accent-blue); }
        .stat-card-value.errors { color: var(--accent-red); }
        .stat-card-value.endpoints { color: var(--accent-purple); }

        .stat-card-label {
            font-size: 11px;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }

        /* Endpoints List */
        .endpoints-section {
            margin-top: 24px;
        }

        .section-title {
            font-size: 13px;
            font-weight: 600;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 1px;
            margin-bottom: 16px;
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .section-title::after {
            content: '';
            flex: 1;
            height: 1px;
            background: var(--border-color);
        }

        .endpoint-card {
            background: var(--bg-tertiary);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            padding: 16px;
            margin-bottom: 12px;
            transition: all 0.2s ease;
        }

        .endpoint-card:hover {
            border-color: var(--accent-blue);
            transform: translateX(4px);
        }

        .endpoint-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-bottom: 12px;
        }

        .endpoint-name {
            font-weight: 600;
            font-size: 14px;
        }

        .endpoint-method {
            font-size: 10px;
            font-weight: 700;
            padding: 4px 8px;
            border-radius: 4px;
            text-transform: uppercase;
        }

        .endpoint-method.GET { background: rgba(0, 200, 83, 0.15); color: var(--accent-green); }
        .endpoint-method.POST { background: rgba(0, 180, 216, 0.15); color: var(--accent-blue); }
        .endpoint-method.PUT { background: rgba(255, 149, 0, 0.15); color: var(--accent-orange); }
        .endpoint-method.DELETE { background: rgba(255, 71, 87, 0.15); color: var(--accent-red); }
        .endpoint-method.SELECT { background: rgba(155, 93, 229, 0.15); color: var(--accent-purple); }
        .endpoint-method.INSERT { background: rgba(0, 200, 83, 0.15); color: var(--accent-green); }
        .endpoint-method.UPDATE { background: rgba(255, 149, 0, 0.15); color: var(--accent-orange); }
        .endpoint-method.CONSUMER { background: rgba(255, 149, 0, 0.15); color: var(--accent-orange); }
        .endpoint-method.PRODUCER { background: rgba(0, 180, 216, 0.15); color: var(--accent-blue); }

        .endpoint-route {
            font-size: 12px;
            color: var(--text-secondary);
            font-family: 'SF Mono', Menlo, monospace;
            margin-bottom: 12px;
            word-break: break-all;
        }

        .endpoint-metrics {
            display: grid;
            grid-template-columns: repeat(3, 1fr);
            gap: 8px;
        }

        .endpoint-metric {
            text-align: center;
            padding: 8px;
            background: var(--bg-card);
            border-radius: 8px;
        }

        .endpoint-metric-value {
            font-size: 14px;
            font-weight: 600;
        }

        .endpoint-metric-label {
            font-size: 9px;
            color: var(--text-muted);
            text-transform: uppercase;
            margin-top: 2px;
        }

        .endpoint-calls {
            margin-top: 12px;
            padding-top: 12px;
            border-top: 1px solid var(--border-color);
        }

        .endpoint-calls-label {
            font-size: 10px;
            color: var(--text-muted);
            text-transform: uppercase;
            margin-bottom: 8px;
        }

        .endpoint-call {
            display: inline-flex;
            align-items: center;
            gap: 4px;
            font-size: 11px;
            padding: 4px 8px;
            background: var(--bg-card);
            border-radius: 4px;
            margin-right: 6px;
            margin-bottom: 6px;
            color: var(--text-secondary);
        }

        .endpoint-call::before {
            content: '→';
            color: var(--accent-blue);
        }

        .anomaly-badge {
            display: inline-flex;
            align-items: center;
            gap: 4px;
            font-size: 10px;
            padding: 4px 8px;
            background: rgba(255, 71, 87, 0.15);
            color: var(--accent-red);
            border-radius: 4px;
            margin-left: 8px;
        }

        .anomaly-badge::before {
            content: '⚠';
        }

        /* Empty State */
        .empty-state {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            height: 100%;
            text-align: center;
            padding: 40px;
        }

        .empty-state-icon {
            font-size: 48px;
            margin-bottom: 16px;
            opacity: 0.5;
        }

        .empty-state-title {
            font-size: 18px;
            font-weight: 600;
            margin-bottom: 8px;
        }

        .empty-state-text {
            font-size: 14px;
            color: var(--text-secondary);
        }

        /* Legend */
        .legend {
            position: absolute;
            bottom: 24px;
            left: 24px;
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            padding: 16px;
            display: flex;
            gap: 24px;
        }

        .legend-item {
            display: flex;
            align-items: center;
            gap: 8px;
            font-size: 12px;
            color: var(--text-secondary);
        }

        .legend-color {
            width: 12px;
            height: 12px;
            border-radius: 3px;
        }

        .legend-line {
            width: 24px;
            height: 3px;
            border-radius: 2px;
        }

        .legend-line.dashed {
            background: repeating-linear-gradient(
                90deg,
                var(--accent-orange) 0px,
                var(--accent-orange) 6px,
                transparent 6px,
                transparent 10px
            );
        }

        /* Animations */
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }

        .pulse {
            animation: pulse 2s ease-in-out infinite;
        }

        @keyframes flowAnimation {
            0% { stroke-dashoffset: 24; }
            100% { stroke-dashoffset: 0; }
        }

        .link.async {
            animation: flowAnimation 1s linear infinite;
        }

        /* Scrollbar */
        ::-webkit-scrollbar {
            width: 8px;
        }

        ::-webkit-scrollbar-track {
            background: var(--bg-tertiary);
        }

        ::-webkit-scrollbar-thumb {
            background: var(--border-color);
            border-radius: 4px;
        }

        ::-webkit-scrollbar-thumb:hover {
            background: var(--text-muted);
        }

        /* Volume Bar */
        .volume-bar {
            height: 4px;
            background: var(--bg-card);
            border-radius: 2px;
            margin-top: 8px;
            overflow: hidden;
        }

        .volume-bar-fill {
            height: 100%;
            background: linear-gradient(90deg, var(--accent-blue), var(--accent-cyan));
            border-radius: 2px;
            transition: width 0.3s ease;
        }
    </style>
</head>
<body>
    <header class="header">
        <div class="logo">
            <div class="logo-icon">S</div>
            <div>
                <div class="logo-text">Semantix</div>
                <div class="logo-subtitle">OpenTelemetry Simulation Engine</div>
            </div>
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
            <div class="header-stat">
                <div class="header-stat-value pulse" style="color: var(--accent-green);">LIVE</div>
                <div class="header-stat-label">Status</div>
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
                <div class="empty-state-icon">🎯</div>
                <div class="empty-state-title">Select a Service</div>
                <div class="empty-state-text">Click on any service node to view detailed telemetry information and endpoint configuration.</div>
            </div>
        </div>
    </div>

    <script>
        // Fetch topology data and render
        async function init() {
            try {
                const response = await fetch('/api/topology');
                const data = await response.json();
                
                // Update header stats
                document.getElementById('total-services').textContent = data.stats.totalServices;
                document.getElementById('total-rpm').textContent = formatNumber(data.stats.totalRpm);
                document.getElementById('total-links').textContent = data.stats.totalLinks;
                
                renderTopology(data);
            } catch (error) {
                console.error('Failed to fetch topology:', error);
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
            const svg = d3.select('#topology');
            const container = document.querySelector('.topology-container');
            const width = container.clientWidth;
            const height = container.clientHeight;

            svg.attr('width', width).attr('height', height);

            // Clear previous content
            svg.selectAll('*').remove();

            // Create definitions for arrows
            const defs = svg.append('defs');
            
            defs.append('marker')
                .attr('id', 'arrowhead-sync')
                .attr('viewBox', '-0 -5 10 10')
                .attr('refX', 20)
                .attr('refY', 0)
                .attr('orient', 'auto')
                .attr('markerWidth', 8)
                .attr('markerHeight', 8)
                .append('path')
                .attr('d', 'M 0,-5 L 10,0 L 0,5')
                .attr('fill', 'var(--accent-blue)');

            defs.append('marker')
                .attr('id', 'arrowhead-async')
                .attr('viewBox', '-0 -5 10 10')
                .attr('refX', 20)
                .attr('refY', 0)
                .attr('orient', 'auto')
                .attr('markerWidth', 8)
                .attr('markerHeight', 8)
                .append('path')
                .attr('d', 'M 0,-5 L 10,0 L 0,5')
                .attr('fill', 'var(--accent-orange)');

            // Create zoom behavior
            const zoom = d3.zoom()
                .scaleExtent([0.3, 3])
                .on('zoom', (event) => {
                    g.attr('transform', event.transform);
                });

            svg.call(zoom);

            const g = svg.append('g');

            // Create force simulation
            const simulation = d3.forceSimulation(data.nodes)
                .force('link', d3.forceLink(data.links).id(d => d.id).distance(200))
                .force('charge', d3.forceManyBody().strength(-1000))
                .force('center', d3.forceCenter(width / 2, height / 2))
                .force('collision', d3.forceCollide().radius(80));

            // Create links
            const link = g.append('g')
                .selectAll('line')
                .data(data.links)
                .join('line')
                .attr('class', d => 'link ' + d.type)
                .attr('marker-end', d => 'url(#arrowhead-' + d.type + ')');

            // Create nodes
            const node = g.append('g')
                .selectAll('g')
                .data(data.nodes)
                .join('g')
                .attr('class', d => 'node ' + d.type)
                .call(d3.drag()
                    .on('start', dragstarted)
                    .on('drag', dragged)
                    .on('end', dragended));

            // Node background
            node.append('rect')
                .attr('class', 'node-bg')
                .attr('width', 140)
                .attr('height', 80)
                .attr('x', -70)
                .attr('y', -40);

            // Accent bar
            node.append('rect')
                .attr('class', 'node-accent')
                .attr('width', 4)
                .attr('height', 80)
                .attr('x', -70)
                .attr('y', -40)
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
                .text('⚠');

            // Click handler
            let selectedNode = null;
            node.on('click', function(event, d) {
                event.stopPropagation();
                
                // Deselect previous
                d3.selectAll('.node').classed('selected', false);
                d3.selectAll('.link').classed('highlighted', false).classed('dimmed', false);
                
                // Select new
                d3.select(this).classed('selected', true);
                selectedNode = d;
                
                // Highlight connected links
                link.classed('highlighted', l => l.source.id === d.id || l.target.id === d.id);
                link.classed('dimmed', l => l.source.id !== d.id && l.target.id !== d.id);
                
                showServiceDetail(d, data);
            });

            // Click outside to deselect
            svg.on('click', () => {
                d3.selectAll('.node').classed('selected', false);
                d3.selectAll('.link').classed('highlighted', false).classed('dimmed', false);
                selectedNode = null;
                showEmptyState();
            });

            // Simulation tick
            simulation.on('tick', () => {
                link
                    .attr('x1', d => d.source.x)
                    .attr('y1', d => d.source.y)
                    .attr('x2', d => d.target.x)
                    .attr('y2', d => d.target.y);

                node.attr('transform', d => 'translate(' + d.x + ',' + d.y + ')');
            });

            function dragstarted(event) {
                if (!event.active) simulation.alphaTarget(0.3).restart();
                event.subject.fx = event.subject.x;
                event.subject.fy = event.subject.y;
            }

            function dragged(event) {
                event.subject.fx = event.x;
                event.subject.fy = event.y;
            }

            function dragended(event) {
                if (!event.active) simulation.alphaTarget(0);
                event.subject.fx = null;
                event.subject.fy = null;
            }
        }

        function showEmptyState() {
            const panel = document.getElementById('detail-panel');
            panel.innerHTML = ` + "`" + `
                <div class="empty-state">
                    <div class="empty-state-icon">🎯</div>
                    <div class="empty-state-title">Select a Service</div>
                    <div class="empty-state-text">Click on any service node to view detailed telemetry information and endpoint configuration.</div>
                </div>
            ` + "`" + `;
        }

        function showServiceDetail(service, topology) {
            const panel = document.getElementById('detail-panel');
            const maxRpm = Math.max(...topology.nodes.map(n => n.totalRpm), 1);
            
            let html = ` + "`" + `
                <div class="panel-header">
                    <div class="panel-title">${service.name}</div>
                    <div class="panel-subtitle">${service.version || 'No version'}</div>
                    <div class="service-badge ${service.type}">
                        ${getServiceIcon(service.type)} ${service.system || service.type}
                    </div>
                </div>
                
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
                const routeLabel = ep.route || (ep.table ? ` + "`" + `${ep.operation} ${ep.table}` + "`" + `) || ep.topic || '';
                
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
                                    <div class="endpoint-metric-value" style="color: var(--text-secondary);">${ep.errorCodes?.join(', ') || '-'}</div>
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

        function getServiceIcon(type) {
            switch (type) {
                case 'database': return '🗄️';
                case 'messaging': return '📨';
                default: return '🌐';
            }
        }

        function getEndpointTypeLabel(type) {
            if (type?.includes('consumer')) return 'CONSUMER';
            if (type?.includes('producer')) return 'PRODUCER';
            return type?.toUpperCase() || 'HTTP';
        }

        // Initialize
        init();

        // Handle resize
        window.addEventListener('resize', () => {
            init();
        });
    </script>
</body>
</html>
`
