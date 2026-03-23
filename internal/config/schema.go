// Package config defines the YAML configuration schema for Semantix.
package config

import "time"

// Config is the root configuration structure.
type Config struct {
	Version        string            `yaml:"version"`
	Simulation     SimulationConfig  `yaml:"simulation"`
	Exporter       ExporterConfig    `yaml:"exporter"`
	GlobalResource map[string]string `yaml:"global_resource_attributes"`
	Services       []ServiceConfig   `yaml:"services"`
	Logs           LogsConfig        `yaml:"logs"`
	Metrics        MetricsConfig     `yaml:"metrics"`
	Scenarios      []ScenarioConfig  `yaml:"scenarios"`
}

// SimulationConfig controls the simulation timing and behavior.
type SimulationConfig struct {
	Duration     string `yaml:"duration"`      // e.g., "24h", "infinite"
	TickInterval string `yaml:"tick_interval"` // e.g., "100ms"
	Seed         int64  `yaml:"seed"`          // For reproducible randomness
}

// ExporterConfig defines the OTLP export settings.
type ExporterConfig struct {
	Endpoint string `yaml:"endpoint"` // Supports ${ENV_VAR} substitution
	Token    string `yaml:"token"`    // Supports ${ENV_VAR} substitution
	Protocol string `yaml:"protocol"` // "http/protobuf" or "grpc"
}

// ServiceConfig defines a virtual service in the topology.
type ServiceConfig struct {
	Name               string            `yaml:"name"`
	Type               string            `yaml:"type,omitempty"`   // "http", "database", "messaging"
	Version            string            `yaml:"version,omitempty"`
	System             string            `yaml:"system,omitempty"` // For db/messaging: "postgresql", "kafka"
	ResourceAttributes map[string]string `yaml:"resource_attributes,omitempty"`
	Connection         *ConnectionConfig `yaml:"connection,omitempty"`
	Endpoints          []EndpointConfig  `yaml:"endpoints,omitempty"`
	Topics             []TopicConfig     `yaml:"topics,omitempty"` // For messaging services
}

// ConnectionConfig for database services.
type ConnectionConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	User     string `yaml:"user,omitempty"`
}

// EndpointConfig defines a service endpoint.
type EndpointConfig struct {
	Name      string          `yaml:"name"`
	Type      string          `yaml:"type"`                // "http.server", "http.client", "db", "messaging.producer", "messaging.consumer"
	Method    string          `yaml:"method,omitempty"`    // HTTP method
	Route     string          `yaml:"route,omitempty"`     // URL path/route
	Operation string          `yaml:"operation,omitempty"` // DB operation: SELECT, INSERT, etc.
	Table     string          `yaml:"table,omitempty"`     // DB table name
	Topic     string          `yaml:"topic,omitempty"`     // Messaging topic
	Traffic   *TrafficConfig  `yaml:"traffic,omitempty"`
	Latency   LatencyConfig   `yaml:"latency"`
	Errors    *ErrorConfig    `yaml:"errors,omitempty"`
	Calls     []CallConfig    `yaml:"calls,omitempty"`
	Anomalies []AnomalyConfig `yaml:"anomalies,omitempty"`
}

// TrafficConfig controls request generation patterns.
type TrafficConfig struct {
	RequestsPerMinute float64 `yaml:"requests_per_minute"`
	BurstProbability  float64 `yaml:"burst_probability"`
	BurstMultiplier   float64 `yaml:"burst_multiplier"`
}

// LatencyConfig defines latency distribution.
type LatencyConfig struct {
	Distribution string  `yaml:"distribution,omitempty"` // "normal", "log_normal", "exponential", "fixed"
	P50Ms        float64 `yaml:"p50_ms"`
	P95Ms        float64 `yaml:"p95_ms,omitempty"`
	P99Ms        float64 `yaml:"p99_ms,omitempty"`
	FixedMs      float64 `yaml:"fixed_ms,omitempty"`
}

// ErrorConfig defines error injection.
type ErrorConfig struct {
	Rate  float64     `yaml:"rate"` // 0.0 to 1.0
	Types []ErrorType `yaml:"types"`
}

// ErrorType defines a specific error response.
type ErrorType struct {
	Code      int     `yaml:"code"`               // HTTP status code
	Message   string  `yaml:"message"`
	Exception string  `yaml:"exception,omitempty"` // Exception class name
	Weight    float64 `yaml:"weight"`              // Relative weight for selection
}

// CallConfig defines a downstream call from an endpoint.
type CallConfig struct {
	Service  string `yaml:"service"`
	Endpoint string `yaml:"endpoint"`
	Type     string `yaml:"type,omitempty"`     // "http", "db", "messaging"
	Parallel bool   `yaml:"parallel,omitempty"` // Execute in parallel with other calls
	Async    bool   `yaml:"async,omitempty"`    // Creates span link instead of parent-child
}

// TopicConfig for messaging services.
type TopicConfig struct {
	Name       string           `yaml:"name"`
	Partitions int              `yaml:"partitions"`
	Producers  []ProducerConfig `yaml:"producers,omitempty"`
	Consumers  []ConsumerConfig `yaml:"consumers,omitempty"`
}

// ProducerConfig defines Kafka producers.
type ProducerConfig struct {
	Service string `yaml:"service"`
}

// ConsumerConfig defines Kafka consumers.
type ConsumerConfig struct {
	Service       string    `yaml:"service"`
	ConsumerGroup string    `yaml:"consumer_group"`
	Lag           LagConfig `yaml:"lag,omitempty"`
}

// LagConfig for consumer lag simulation.
type LagConfig struct {
	MinMs int `yaml:"min_ms"`
	MaxMs int `yaml:"max_ms"`
}

// AnomalyConfig defines chaos/anomaly injection.
type AnomalyConfig struct {
	Type        string   `yaml:"type"`                  // "latency_spike", "error_burst"
	Probability float64  `yaml:"probability"`           // Probability of occurrence
	Multiplier  float64  `yaml:"multiplier,omitempty"`  // For latency
	ErrorRate   float64  `yaml:"error_rate,omitempty"`  // For error burst
	Duration    string   `yaml:"duration"`              // How long the anomaly lasts
	Services    []string `yaml:"services,omitempty"`    // Target services (for scenarios)
}

// LogsConfig controls log generation.
type LogsConfig struct {
	Enabled  bool         `yaml:"enabled"`
	Patterns []LogPattern `yaml:"patterns"`
}

// LogPattern defines log message patterns.
type LogPattern struct {
	Service        string   `yaml:"service"`
	Level          string   `yaml:"level"` // "DEBUG", "INFO", "WARN", "ERROR"
	RatePerRequest float64  `yaml:"rate_per_request,omitempty"`
	OnError        bool     `yaml:"on_error,omitempty"` // Only emit on error spans
	Messages       []string `yaml:"messages"`
}

// MetricsConfig controls metric generation.
type MetricsConfig struct {
	Enabled        bool           `yaml:"enabled"`
	ExportInterval string         `yaml:"export_interval"` // e.g., "60s"
	HTTP           HTTPMetrics    `yaml:"http,omitempty"`
	Database       DBMetrics      `yaml:"database,omitempty"`
	Custom         []CustomMetric `yaml:"custom,omitempty"`
}

// HTTPMetrics for standard HTTP metrics.
type HTTPMetrics struct {
	Enabled    bool     `yaml:"enabled"`
	Histograms []string `yaml:"histograms"`
	Counters   []string `yaml:"counters"`
}

// DBMetrics for database metrics.
type DBMetrics struct {
	Enabled    bool     `yaml:"enabled"`
	Histograms []string `yaml:"histograms"`
	Counters   []string `yaml:"counters"`
}

// CustomMetric defines a custom business metric.
type CustomMetric struct {
	Name       string    `yaml:"name"`
	Type       string    `yaml:"type"` // "counter", "histogram", "gauge"
	Unit       string    `yaml:"unit"`
	Buckets    []float64 `yaml:"buckets,omitempty"`
	Attributes []string  `yaml:"attributes,omitempty"`
}

// ScenarioConfig for time-based traffic patterns.
type ScenarioConfig struct {
	Name              string          `yaml:"name"`
	Schedule          string          `yaml:"schedule"` // Cron-like expression
	TrafficMultiplier float64         `yaml:"traffic_multiplier"`
	Anomalies         []AnomalyConfig `yaml:"anomalies,omitempty"`
}

// ParsedTickInterval returns the tick interval as a time.Duration.
func (s *SimulationConfig) ParsedTickInterval() (time.Duration, error) {
	if s.TickInterval == "" {
		return 100 * time.Millisecond, nil
	}
	return time.ParseDuration(s.TickInterval)
}

// ParsedDuration returns the simulation duration.
// Returns 0 for infinite duration.
func (s *SimulationConfig) ParsedDuration() (time.Duration, error) {
	if s.Duration == "" || s.Duration == "infinite" {
		return 0, nil // 0 means infinite
	}
	return time.ParseDuration(s.Duration)
}

// IsInfinite returns true if simulation should run indefinitely.
func (s *SimulationConfig) IsInfinite() bool {
	return s.Duration == "" || s.Duration == "infinite"
}
