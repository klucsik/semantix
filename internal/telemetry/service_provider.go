// Package telemetry provides per-service OpenTelemetry instrumentation.
package telemetry

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/mreider/semantix/internal/config"
)

// exporterOptions holds the parsed options for trace and metric exporters.
type exporterOptions struct {
	traceOpts  []otlptracehttp.Option
	metricOpts []otlpmetrichttp.Option
}

// buildExporterOptions parses config and builds OTLP exporter options.
func buildExporterOptions(cfg *config.Config) (*exporterOptions, error) {
	// Parse endpoint - remove signal-specific paths if present
	endpoint := strings.TrimSuffix(cfg.Exporter.Endpoint, "/")
	endpoint = strings.TrimSuffix(endpoint, "/v1/traces")
	endpoint = strings.TrimSuffix(endpoint, "/v1/metrics")
	endpoint = strings.TrimSuffix(endpoint, "/v1/logs")

	// Build headers with auth token
	headers := map[string]string{}
	if cfg.Exporter.Token != "" {
		headers["Authorization"] = "Api-Token " + cfg.Exporter.Token
	}

	// Determine if we need TLS
	isHTTPS := strings.HasPrefix(cfg.Exporter.Endpoint, "https://")

	// Build trace exporter options
	traceOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(extractHost(endpoint)),
		otlptracehttp.WithURLPath(extractPath(endpoint) + "/v1/traces"),
		otlptracehttp.WithHeaders(headers),
	}
	if !isHTTPS {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
	}

	// Build metric exporter options
	metricOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(extractHost(endpoint)),
		otlpmetrichttp.WithURLPath(extractPath(endpoint) + "/v1/metrics"),
		otlpmetrichttp.WithHeaders(headers),
	}
	if !isHTTPS {
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}

	return &exporterOptions{
		traceOpts:  traceOpts,
		metricOpts: metricOpts,
	}, nil
}

// ServiceProvider holds all telemetry providers for a single service.
// Each service gets its own providers with the correct service.name in the Resource,
// ensuring proper service attribution in backends like Dynatrace.
type ServiceProvider struct {
	ServiceName    string
	ServiceVersion string

	// Providers - each has a Resource with this service's identity
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	// LoggerProvider will be added when OTel Go logs API stabilizes

	// Convenience accessors
	tracer  trace.Tracer
	meter   metric.Meter
	metrics *Metrics
}

// Tracer returns the tracer for this service.
func (sp *ServiceProvider) Tracer() trace.Tracer {
	return sp.tracer
}

// Meter returns the meter for this service.
func (sp *ServiceProvider) Meter() metric.Meter {
	return sp.meter
}

// Metrics returns the metrics collector for this service.
func (sp *ServiceProvider) Metrics() *Metrics {
	return sp.metrics
}

// Shutdown gracefully shuts down all providers for this service.
func (sp *ServiceProvider) Shutdown(ctx context.Context) error {
	var errs []error

	if sp.TracerProvider != nil {
		if err := sp.TracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer provider shutdown for %s: %w", sp.ServiceName, err))
		}
	}

	if sp.MeterProvider != nil {
		if err := sp.MeterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider shutdown for %s: %w", sp.ServiceName, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// ServiceProviderFactory creates ServiceProviders for each service.
// It shares exporters across all services for efficiency while giving
// each service its own TracerProvider/MeterProvider with correct Resource.
type ServiceProviderFactory struct {
	cfg           *config.Config
	traceExporter sdktrace.SpanExporter
	metricReader  sdkmetric.Reader

	// Track created providers for cleanup
	providers []*ServiceProvider
}

// NewServiceProviderFactory creates a factory for building per-service providers.
func NewServiceProviderFactory(ctx context.Context, cfg *config.Config) (*ServiceProviderFactory, error) {
	// Parse and prepare exporter options
	opts, err := buildExporterOptions(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build exporter options: %w", err)
	}

	// Create shared trace exporter
	traceExporter, err := otlptracehttp.New(ctx, opts.traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create shared metric exporter and reader
	metricExporter, err := otlpmetrichttp.New(ctx, opts.metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}
	metricReader := sdkmetric.NewPeriodicReader(metricExporter)

	return &ServiceProviderFactory{
		cfg:           cfg,
		traceExporter: traceExporter,
		metricReader:  metricReader,
		providers:     make([]*ServiceProvider, 0),
	}, nil
}

// CreateProvider creates a ServiceProvider for a specific service.
// The provider will have the correct service.name and other resource attributes
// embedded in its Resource, ensuring proper service attribution in the backend.
func (f *ServiceProviderFactory) CreateProvider(ctx context.Context, svc *config.ServiceConfig) (*ServiceProvider, error) {
	// Build resource with service identity
	res, err := f.buildServiceResource(svc)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource for %s: %w", svc.Name, err)
	}

	// Create TracerProvider with service-specific resource
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(f.traceExporter),
		sdktrace.WithResource(res),
	)

	// Create MeterProvider with service-specific resource
	// Note: We create a new exporter per service for metrics because
	// the periodic reader can only be used with one provider
	metricOpts, err := buildExporterOptions(f.cfg)
	if err != nil {
		return nil, err
	}
	metricExporter, err := otlpmetrichttp.New(ctx, metricOpts.metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter for %s: %w", svc.Name, err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)

	// Create tracer and meter
	tracer := tp.Tracer("github.com/mreider/semantix/" + svc.Name)
	meter := mp.Meter("github.com/mreider/semantix/" + svc.Name)

	// Create metrics collector
	metrics, err := NewMetrics(meter, &f.cfg.Metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics for %s: %w", svc.Name, err)
	}

	sp := &ServiceProvider{
		ServiceName:    svc.Name,
		ServiceVersion: svc.Version,
		TracerProvider: tp,
		MeterProvider:  mp,
		tracer:         tracer,
		meter:          meter,
		metrics:        metrics,
	}

	f.providers = append(f.providers, sp)
	return sp, nil
}

// buildServiceResource creates an OTel Resource with service identity and attributes.
func (f *ServiceProviderFactory) buildServiceResource(svc *config.ServiceConfig) (*resource.Resource, error) {
	// Start with core service identity
	attrs := []attribute.KeyValue{
		semconv.ServiceName(svc.Name),
		semconv.TelemetrySDKName("semantix"),
		semconv.TelemetrySDKVersion("1.0.0"),
		semconv.TelemetrySDKLanguageGo,
	}

	// Add service version if specified
	if svc.Version != "" {
		attrs = append(attrs, semconv.ServiceVersion(svc.Version))
	}

	// Add service type as a custom attribute
	if svc.Type != "" {
		attrs = append(attrs, attribute.String("service.type", svc.Type))
	}

	// Add system (e.g., "postgresql", "kafka") if specified
	if svc.System != "" {
		attrs = append(attrs, attribute.String("service.system", svc.System))
	}

	// Add global resource attributes from config
	for k, v := range f.cfg.GlobalResource {
		attrs = append(attrs, attribute.String(k, v))
	}

	// Add service-specific resource attributes (override globals)
	for k, v := range svc.ResourceAttributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	return resource.NewWithAttributes(semconv.SchemaURL, attrs...), nil
}

// Shutdown gracefully shuts down all created providers.
func (f *ServiceProviderFactory) Shutdown(ctx context.Context) error {
	var errs []error

	for _, sp := range f.providers {
		if err := sp.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// Providers returns all created service providers.
func (f *ServiceProviderFactory) Providers() []*ServiceProvider {
	return f.providers
}
