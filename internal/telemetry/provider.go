// Package telemetry handles OpenTelemetry setup and export.
package telemetry

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/mreider/semantix/internal/config"
)

// Provider wraps OpenTelemetry providers for traces, metrics, and logs.
type Provider struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *metric.MeterProvider
	// LoggerProvider will be added when OTel Go logs API stabilizes
}

// NewProvider creates a new telemetry provider from configuration.
func NewProvider(ctx context.Context, cfg *config.Config) (*Provider, error) {
	// Parse endpoint - remove /v1/traces etc. if present, we add them per-signal
	endpoint := strings.TrimSuffix(cfg.Exporter.Endpoint, "/")
	endpoint = strings.TrimSuffix(endpoint, "/v1/traces")
	endpoint = strings.TrimSuffix(endpoint, "/v1/metrics")
	endpoint = strings.TrimSuffix(endpoint, "/v1/logs")

	// Build headers with auth token
	headers := map[string]string{}
	if cfg.Exporter.Token != "" {
		headers["Authorization"] = "Api-Token " + cfg.Exporter.Token
	}

	// Create base resource with SDK info
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.TelemetrySDKName("semantix"),
			semconv.TelemetrySDKVersion("1.0.0"),
			semconv.TelemetrySDKLanguageGo,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Determine if we need TLS
	isHTTPS := strings.HasPrefix(cfg.Exporter.Endpoint, "https://")

	// Create trace exporter
	traceOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(extractHost(endpoint)),
		otlptracehttp.WithURLPath(extractPath(endpoint) + "/v1/traces"),
		otlptracehttp.WithHeaders(headers),
	}
	if !isHTTPS {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
	}
	traceExporter, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Create metric exporter
	metricOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(extractHost(endpoint)),
		otlpmetrichttp.WithURLPath(extractPath(endpoint) + "/v1/metrics"),
		otlpmetrichttp.WithHeaders(headers),
	}
	if !isHTTPS {
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}
	metricExporter, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	// Create meter provider
	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter)),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return &Provider{
		TracerProvider: tp,
		MeterProvider:  mp,
	}, nil
}

// Shutdown gracefully shuts down all providers.
func (p *Provider) Shutdown(ctx context.Context) error {
	var errs []error

	if p.TracerProvider != nil {
		if err := p.TracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("trace provider shutdown: %w", err))
		}
	}

	if p.MeterProvider != nil {
		if err := p.MeterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider shutdown: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// TracerForService returns a tracer configured for a specific service.
func (p *Provider) TracerForService(svc *config.ServiceConfig, globalAttrs map[string]string) trace.Tracer {
	// Build resource attributes
	attrs := make([]attribute.KeyValue, 0)
	
	// Add service-specific attributes
	attrs = append(attrs, semconv.ServiceName(svc.Name))
	if svc.Version != "" {
		attrs = append(attrs, semconv.ServiceVersion(svc.Version))
	}

	// Add global and service resource attributes
	allAttrs := mergeAttributes(globalAttrs, svc.ResourceAttributes)
	for k, v := range allAttrs {
		attrs = append(attrs, attribute.String(k, v))
	}

	return p.TracerProvider.Tracer(
		"github.com/mreider/semantix/"+svc.Name,
		trace.WithInstrumentationAttributes(attrs...),
	)
}

// extractHost extracts the host:port from a URL.
func extractHost(endpoint string) string {
	// Remove protocol
	s := endpoint
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	
	// Get host part (before first /)
	if idx := strings.Index(s, "/"); idx != -1 {
		s = s[:idx]
	}
	return s
}

// extractPath extracts the path from a URL.
func extractPath(endpoint string) string {
	// Remove protocol
	s := endpoint
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	
	// Get path part (after first /)
	if idx := strings.Index(s, "/"); idx != -1 {
		return s[idx:]
	}
	return ""
}

// mergeAttributes merges two attribute maps, with second taking precedence.
func mergeAttributes(base, overlay map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}
