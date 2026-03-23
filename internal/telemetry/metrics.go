// Package telemetry provides metrics instrumentation for the simulation.
package telemetry

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/mreider/semantix/internal/config"
)

// Metrics holds all metric instruments for a simulation.
type Metrics struct {
	meter metric.Meter
	cfg   *config.MetricsConfig

	// HTTP metrics
	httpServerDuration   metric.Float64Histogram
	httpServerRequests   metric.Int64Counter
	httpClientDuration   metric.Float64Histogram
	httpClientRequests   metric.Int64Counter

	// Database metrics
	dbOperationDuration  metric.Float64Histogram
	dbConnectionCount    metric.Int64UpDownCounter

	// Messaging metrics
	messagingPublishDuration metric.Float64Histogram
	messagingReceiveDuration metric.Float64Histogram
	messagingProcessDuration metric.Float64Histogram

	// Custom metrics
	customCounters   map[string]metric.Int64Counter
	customHistograms map[string]metric.Float64Histogram
	customGauges     map[string]metric.Float64ObservableGauge

	mu sync.RWMutex
}

// NewMetrics creates a new metrics collector.
func NewMetrics(meter metric.Meter, cfg *config.MetricsConfig) (*Metrics, error) {
	m := &Metrics{
		meter:            meter,
		cfg:              cfg,
		customCounters:   make(map[string]metric.Int64Counter),
		customHistograms: make(map[string]metric.Float64Histogram),
		customGauges:     make(map[string]metric.Float64ObservableGauge),
	}

	if !cfg.Enabled {
		return m, nil
	}

	var err error

	// Initialize HTTP metrics
	if cfg.HTTP.Enabled {
		m.httpServerDuration, err = meter.Float64Histogram(
			"http.server.request.duration",
			metric.WithDescription("Duration of HTTP server requests"),
			metric.WithUnit("s"),
		)
		if err != nil {
			return nil, err
		}

		m.httpServerRequests, err = meter.Int64Counter(
			"http.server.request.count",
			metric.WithDescription("Number of HTTP server requests"),
			metric.WithUnit("1"),
		)
		if err != nil {
			return nil, err
		}

		m.httpClientDuration, err = meter.Float64Histogram(
			"http.client.request.duration",
			metric.WithDescription("Duration of HTTP client requests"),
			metric.WithUnit("s"),
		)
		if err != nil {
			return nil, err
		}

		m.httpClientRequests, err = meter.Int64Counter(
			"http.client.request.count",
			metric.WithDescription("Number of HTTP client requests"),
			metric.WithUnit("1"),
		)
		if err != nil {
			return nil, err
		}
	}

	// Initialize database metrics
	if cfg.Database.Enabled {
		m.dbOperationDuration, err = meter.Float64Histogram(
			"db.client.operation.duration",
			metric.WithDescription("Duration of database operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			return nil, err
		}

		m.dbConnectionCount, err = meter.Int64UpDownCounter(
			"db.client.connection.count",
			metric.WithDescription("Number of database connections"),
			metric.WithUnit("1"),
		)
		if err != nil {
			return nil, err
		}
	}

	// Initialize messaging metrics
	m.messagingPublishDuration, err = meter.Float64Histogram(
		"messaging.publish.duration",
		metric.WithDescription("Duration of message publishing"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	m.messagingReceiveDuration, err = meter.Float64Histogram(
		"messaging.receive.duration",
		metric.WithDescription("Duration of message receiving"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	m.messagingProcessDuration, err = meter.Float64Histogram(
		"messaging.process.duration",
		metric.WithDescription("Duration of message processing"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	// Initialize custom metrics
	for _, cm := range cfg.Custom {
		switch cm.Type {
		case "counter":
			counter, err := meter.Int64Counter(
				cm.Name,
				metric.WithDescription("Custom counter: "+cm.Name),
				metric.WithUnit(cm.Unit),
			)
			if err != nil {
				return nil, err
			}
			m.customCounters[cm.Name] = counter

		case "histogram":
			hist, err := meter.Float64Histogram(
				cm.Name,
				metric.WithDescription("Custom histogram: "+cm.Name),
				metric.WithUnit(cm.Unit),
			)
			if err != nil {
				return nil, err
			}
			m.customHistograms[cm.Name] = hist
		}
	}

	return m, nil
}

// RecordHTTPServerRequest records an HTTP server request metric.
func (m *Metrics) RecordHTTPServerRequest(ctx context.Context, duration time.Duration, attrs ...attribute.KeyValue) {
	if m.httpServerDuration == nil {
		return
	}
	m.httpServerDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	if m.httpServerRequests != nil {
		m.httpServerRequests.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordHTTPClientRequest records an HTTP client request metric.
func (m *Metrics) RecordHTTPClientRequest(ctx context.Context, duration time.Duration, attrs ...attribute.KeyValue) {
	if m.httpClientDuration == nil {
		return
	}
	m.httpClientDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	if m.httpClientRequests != nil {
		m.httpClientRequests.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordDBOperation records a database operation metric.
func (m *Metrics) RecordDBOperation(ctx context.Context, duration time.Duration, attrs ...attribute.KeyValue) {
	if m.dbOperationDuration == nil {
		return
	}
	m.dbOperationDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}

// RecordMessagingPublish records a messaging publish metric.
func (m *Metrics) RecordMessagingPublish(ctx context.Context, duration time.Duration, attrs ...attribute.KeyValue) {
	if m.messagingPublishDuration == nil {
		return
	}
	m.messagingPublishDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}

// RecordMessagingReceive records a messaging receive metric.
func (m *Metrics) RecordMessagingReceive(ctx context.Context, duration time.Duration, attrs ...attribute.KeyValue) {
	if m.messagingReceiveDuration == nil {
		return
	}
	m.messagingReceiveDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}

// RecordMessagingProcess records a messaging process metric.
func (m *Metrics) RecordMessagingProcess(ctx context.Context, duration time.Duration, attrs ...attribute.KeyValue) {
	if m.messagingProcessDuration == nil {
		return
	}
	m.messagingProcessDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}

// IncrementCustomCounter increments a custom counter.
func (m *Metrics) IncrementCustomCounter(ctx context.Context, name string, value int64, attrs ...attribute.KeyValue) {
	m.mu.RLock()
	counter, ok := m.customCounters[name]
	m.mu.RUnlock()
	if ok {
		counter.Add(ctx, value, metric.WithAttributes(attrs...))
	}
}

// RecordCustomHistogram records a value in a custom histogram.
func (m *Metrics) RecordCustomHistogram(ctx context.Context, name string, value float64, attrs ...attribute.KeyValue) {
	m.mu.RLock()
	hist, ok := m.customHistograms[name]
	m.mu.RUnlock()
	if ok {
		hist.Record(ctx, value, metric.WithAttributes(attrs...))
	}
}
