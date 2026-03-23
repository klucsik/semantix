// Package simulation implements the traffic simulation engine.
package simulation

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/mreider/semantix/internal/config"
	"github.com/mreider/semantix/internal/telemetry"
)

// Engine runs the telemetry simulation.
type Engine struct {
	cfg              *config.Config
	factory          *telemetry.ServiceProviderFactory
	serviceProviders map[string]*telemetry.ServiceProvider // per-service providers
	logger           *telemetry.Logger
	anomalyManager   *AnomalyManager
	scenarioManager  *ScenarioManager
	rng              *rand.Rand
	mu               sync.RWMutex
}

// NewEngine creates a new simulation engine.
func NewEngine(ctx context.Context, cfg *config.Config) (*Engine, error) {
	// Initialize random source
	seed := cfg.Simulation.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	// Create the service provider factory
	factory, err := telemetry.NewServiceProviderFactory(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create service provider factory: %w", err)
	}

	// Create a ServiceProvider for each service
	serviceProviders := make(map[string]*telemetry.ServiceProvider)
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		sp, err := factory.CreateProvider(ctx, svc)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider for service %s: %w", svc.Name, err)
		}
		serviceProviders[svc.Name] = sp
	}

	// Create logger
	logger := telemetry.NewLogger(&cfg.Logs, seed)

	// Create anomaly and scenario managers
	anomalyManager := NewAnomalyManager(seed)
	scenarioManager := NewScenarioManager(cfg.Scenarios, seed)

	return &Engine{
		cfg:              cfg,
		factory:          factory,
		serviceProviders: serviceProviders,
		logger:           logger,
		anomalyManager:   anomalyManager,
		scenarioManager:  scenarioManager,
		rng:              rand.New(rand.NewSource(seed)),
	}, nil
}

// Shutdown gracefully shuts down all service providers.
func (e *Engine) Shutdown(ctx context.Context) error {
	return e.factory.Shutdown(ctx)
}

// Run starts the simulation loop.
func (e *Engine) Run(ctx context.Context) error {
	tickInterval, err := e.cfg.Simulation.ParsedTickInterval()
	if err != nil {
		return fmt.Errorf("invalid tick interval: %w", err)
	}

	duration, err := e.cfg.Simulation.ParsedDuration()
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	// Get entry points (endpoints with traffic config)
	entryPoints := e.cfg.GetEntryPoints()
	if len(entryPoints) == 0 {
		return fmt.Errorf("no entry points defined (endpoints with traffic config)")
	}

	log.Printf("Starting simulation with %d entry points, tick=%v", len(entryPoints), tickInterval)

	// Create ticker for simulation loop
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	// Optional duration timer
	var durationTimer <-chan time.Time
	if duration > 0 {
		durationTimer = time.After(duration)
	}

	// Track request timing for each entry point
	type entryPointState struct {
		svc           *config.ServiceConfig
		ep            *config.EndpointConfig
		lastRequest   time.Time
		requestPeriod time.Duration
	}

	states := make([]*entryPointState, len(entryPoints))
	for i, entry := range entryPoints {
		period := time.Duration(float64(time.Minute) / entry.Endpoint.Traffic.RequestsPerMinute)
		states[i] = &entryPointState{
			svc:           entry.Service,
			ep:            entry.Endpoint,
			lastRequest:   time.Now(),
			requestPeriod: period,
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-durationTimer:
			log.Printf("Simulation duration reached")
			return nil

		case now := <-ticker.C:
			// Get current traffic multiplier from scenarios
			scenarioMultiplier := e.scenarioManager.GetTrafficMultiplier()

			// Check each entry point
			for _, state := range states {
				// Adjust request period based on scenario
				adjustedPeriod := time.Duration(float64(state.requestPeriod) / scenarioMultiplier)

				// Check if it's time for a new request
				if now.Sub(state.lastRequest) >= adjustedPeriod {
					state.lastRequest = now

					// Check for burst
					multiplier := 1.0
					if state.ep.Traffic.BurstProbability > 0 {
						e.mu.Lock()
						if e.rng.Float64() < state.ep.Traffic.BurstProbability {
							multiplier = state.ep.Traffic.BurstMultiplier
						}
						e.mu.Unlock()
					}

					// Generate requests
					count := int(multiplier)
					for i := 0; i < count; i++ {
						go e.simulateRequest(ctx, state.svc, state.ep)
					}
				}
			}
		}
	}
}

// simulateRequest simulates a single request through the service topology.
func (e *Engine) simulateRequest(ctx context.Context, svc *config.ServiceConfig, ep *config.EndpointConfig) {
	sp := e.serviceProviders[svc.Name]
	if sp == nil {
		log.Printf("No service provider for service %s", svc.Name)
		return
	}

	// Generate span name based on endpoint type
	spanName := e.generateSpanName(svc, ep)

	// Create root span with appropriate kind
	spanKind := e.getSpanKind(ep)
	ctx, span := sp.Tracer().Start(ctx, spanName,
		trace.WithSpanKind(spanKind),
	)
	defer span.End()

	// Add semantic attributes
	e.addSpanAttributes(span, svc, ep)

	// Check for anomalies and get adjusted latency/error rate
	baseLatencyMs := ep.Latency.P50Ms
	baseErrorRate := 0.0
	if ep.Errors != nil {
		baseErrorRate = ep.Errors.Rate
	}
	adjustedLatencyMs, adjustedErrorRate := e.anomalyManager.CheckAndApplyAnomalies(
		svc.Name, ep.Name, ep.Anomalies, baseLatencyMs, baseErrorRate,
	)

	// Simulate latency with anomaly adjustment
	latencyCfg := ep.Latency
	latencyCfg.P50Ms = adjustedLatencyMs
	latency := e.calculateLatency(latencyCfg)
	time.Sleep(latency)

	// Check for errors with anomaly adjustment
	isError := false
	var errorType *config.ErrorType
	e.mu.Lock()
	if e.rng.Float64() < adjustedErrorRate {
		isError = true
		if ep.Errors != nil && len(ep.Errors.Types) > 0 {
			errorType = e.selectErrorType(ep.Errors.Types)
		} else {
			// Default error for anomaly-induced errors
			errorType = &config.ErrorType{
				Code:    500,
				Message: "Internal Server Error (anomaly)",
			}
		}
	}
	e.mu.Unlock()

	if isError && errorType != nil {
		span.SetStatus(codes.Error, errorType.Message)
		span.SetAttributes(
			attribute.Int("http.response.status_code", errorType.Code),
		)
		if errorType.Exception != "" {
			span.SetAttributes(
				attribute.String("exception.type", errorType.Exception),
				attribute.String("exception.message", errorType.Message),
			)
		}
	} else {
		span.SetAttributes(attribute.Int("http.response.status_code", 200))
	}

	// Record metrics using per-service metrics
	statusCode := 200
	if isError && errorType != nil {
		statusCode = errorType.Code
	}
	metricAttrs := []attribute.KeyValue{
		attribute.String("http.request.method", ep.Method),
		attribute.String("http.route", ep.Route),
		attribute.Int("http.response.status_code", statusCode),
	}
	sp.Metrics().RecordHTTPServerRequest(ctx, latency, metricAttrs...)

	// Generate correlated logs
	logEntries := e.logger.GenerateLogs(ctx, svc.Name, isError)
	for _, entry := range logEntries {
		// In a full implementation, these would be exported via OTLP logs
		// For now, we log them to stdout with trace correlation
		log.Printf("[%s] %s: %s (trace=%s)", entry.SeverityText, entry.ServiceName, entry.Body, entry.TraceID)
	}

	// Process downstream calls (if not an error or if we still want to call downstream)
	if !isError {
		e.processDownstreamCalls(ctx, ep.Calls)
	}
}

// processDownstreamCalls handles calls to downstream services.
func (e *Engine) processDownstreamCalls(ctx context.Context, calls []config.CallConfig) {
	if len(calls) == 0 {
		return
	}

	// Separate parallel and sequential calls
	var parallelCalls []config.CallConfig
	var sequentialCalls []config.CallConfig

	for _, call := range calls {
		if call.Parallel {
			parallelCalls = append(parallelCalls, call)
		} else {
			sequentialCalls = append(sequentialCalls, call)
		}
	}

	// Execute parallel calls
	if len(parallelCalls) > 0 {
		var wg sync.WaitGroup
		for _, call := range parallelCalls {
			wg.Add(1)
			go func(c config.CallConfig) {
				defer wg.Done()
				e.executeCall(ctx, c)
			}(call)
		}
		wg.Wait()
	}

	// Execute sequential calls
	for _, call := range sequentialCalls {
		e.executeCall(ctx, call)
	}
}

// executeCall executes a single downstream call.
func (e *Engine) executeCall(ctx context.Context, call config.CallConfig) {
	targetSvc := e.cfg.FindServiceByName(call.Service)
	if targetSvc == nil {
		log.Printf("Target service not found: %s", call.Service)
		return
	}

	targetEp := targetSvc.FindEndpointByName(call.Endpoint)
	if targetEp == nil {
		log.Printf("Target endpoint not found: %s.%s", call.Service, call.Endpoint)
		return
	}

	sp := e.serviceProviders[targetSvc.Name]
	if sp == nil {
		log.Printf("No service provider for service %s", targetSvc.Name)
		return
	}

	// For async calls (messaging), we create a span link instead of parent-child
	var opts []trace.SpanStartOption
	spanKind := e.getSpanKind(targetEp)
	opts = append(opts, trace.WithSpanKind(spanKind))

	if call.Async {
		// Get current span context for linking
		parentSpan := trace.SpanFromContext(ctx)
		if parentSpan.SpanContext().IsValid() {
			opts = append(opts, trace.WithLinks(trace.Link{
				SpanContext: parentSpan.SpanContext(),
				Attributes: []attribute.KeyValue{
					attribute.String("link.type", "async"),
				},
			}))
		}
		// Start with a new root context for async
		ctx = context.Background()
	}

	spanName := e.generateSpanName(targetSvc, targetEp)
	ctx, span := sp.Tracer().Start(ctx, spanName, opts...)
	defer span.End()

	e.addSpanAttributes(span, targetSvc, targetEp)

	// Simulate latency
	latency := e.calculateLatency(targetEp.Latency)
	time.Sleep(latency)

	// Check for errors
	isError := false
	var errorType *config.ErrorType
	if targetEp.Errors != nil && targetEp.Errors.Rate > 0 {
		e.mu.Lock()
		if e.rng.Float64() < targetEp.Errors.Rate {
			isError = true
			errorType = e.selectErrorType(targetEp.Errors.Types)
		}
		e.mu.Unlock()
	}

	if isError && errorType != nil {
		span.SetStatus(codes.Error, errorType.Message)
		span.SetAttributes(attribute.Int("http.response.status_code", errorType.Code))
		if errorType.Exception != "" {
			span.SetAttributes(
				attribute.String("exception.type", errorType.Exception),
				attribute.String("exception.message", errorType.Message),
			)
		}
	} else {
		// Set success status based on type
		switch targetSvc.Type {
		case "database":
			span.SetAttributes(attribute.Int("db.response.rows_affected", 1))
		case "http":
			span.SetAttributes(attribute.Int("http.response.status_code", 200))
		}
	}

	// Record metrics using per-service metrics (no need for service.name attribute - it's in Resource)
	metricAttrs := []attribute.KeyValue{}
	switch targetSvc.Type {
	case "database":
		metricAttrs = append(metricAttrs,
			attribute.String("db.system", targetSvc.System),
			attribute.String("db.operation", targetEp.Operation),
		)
		sp.Metrics().RecordDBOperation(ctx, latency, metricAttrs...)
	case "messaging":
		metricAttrs = append(metricAttrs,
			attribute.String("messaging.system", targetSvc.System),
			attribute.String("messaging.destination.name", targetEp.Topic),
		)
		if targetEp.Type == "messaging.producer" {
			sp.Metrics().RecordMessagingPublish(ctx, latency, metricAttrs...)
		} else {
			sp.Metrics().RecordMessagingReceive(ctx, latency, metricAttrs...)
		}
	default: // HTTP client call
		statusCode := 200
		if isError && errorType != nil {
			statusCode = errorType.Code
		}
		metricAttrs = append(metricAttrs,
			attribute.String("http.request.method", targetEp.Method),
			attribute.String("server.address", targetSvc.Name),
			attribute.Int("http.response.status_code", statusCode),
		)
		sp.Metrics().RecordHTTPClientRequest(ctx, latency, metricAttrs...)
	}

	// Generate correlated logs for this service
	logEntries := e.logger.GenerateLogs(ctx, targetSvc.Name, isError)
	for _, entry := range logEntries {
		log.Printf("[%s] %s: %s (trace=%s)", entry.SeverityText, entry.ServiceName, entry.Body, entry.TraceID)
	}

	// Recursively process downstream calls
	if !isError {
		e.processDownstreamCalls(ctx, targetEp.Calls)
	}
}

// generateSpanName creates an appropriate span name based on endpoint type.
func (e *Engine) generateSpanName(svc *config.ServiceConfig, ep *config.EndpointConfig) string {
	switch svc.Type {
	case "database":
		if ep.Operation != "" && ep.Table != "" {
			return fmt.Sprintf("%s %s", ep.Operation, ep.Table)
		}
		return ep.Name

	case "messaging":
		if ep.Topic != "" {
			return fmt.Sprintf("%s %s", ep.Topic, ep.Type)
		}
		return ep.Name

	default: // HTTP
		if ep.Method != "" && ep.Route != "" {
			return fmt.Sprintf("%s %s", ep.Method, ep.Route)
		}
		return ep.Name
	}
}

// getSpanKind returns the appropriate span kind for an endpoint.
func (e *Engine) getSpanKind(ep *config.EndpointConfig) trace.SpanKind {
	switch ep.Type {
	case "http.server":
		return trace.SpanKindServer
	case "http.client":
		return trace.SpanKindClient
	case "db":
		return trace.SpanKindClient
	case "messaging.producer":
		return trace.SpanKindProducer
	case "messaging.consumer":
		return trace.SpanKindConsumer
	default:
		return trace.SpanKindInternal
	}
}

// addSpanAttributes adds semantic convention attributes to a span.
func (e *Engine) addSpanAttributes(span trace.Span, svc *config.ServiceConfig, ep *config.EndpointConfig) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(svc.Name),
	}

	if svc.Version != "" {
		attrs = append(attrs, semconv.ServiceVersion(svc.Version))
	}

	switch svc.Type {
	case "database":
		attrs = append(attrs, e.getDatabaseAttributes(svc, ep)...)
	case "messaging":
		attrs = append(attrs, e.getMessagingAttributes(svc, ep)...)
	default: // HTTP
		attrs = append(attrs, e.getHTTPAttributes(svc, ep)...)
	}

	span.SetAttributes(attrs...)
}

// getHTTPAttributes returns HTTP semantic convention attributes.
func (e *Engine) getHTTPAttributes(svc *config.ServiceConfig, ep *config.EndpointConfig) []attribute.KeyValue {
	attrs := []attribute.KeyValue{}

	if ep.Method != "" {
		attrs = append(attrs, semconv.HTTPRequestMethodKey.String(ep.Method))
	}
	if ep.Route != "" {
		attrs = append(attrs, semconv.URLPath(ep.Route))
		attrs = append(attrs, attribute.String("http.route", ep.Route))
	}

	attrs = append(attrs,
		semconv.URLScheme("http"),
		semconv.ServerAddress(svc.Name),
		semconv.ServerPort(8080),
		semconv.NetworkProtocolVersion("1.1"),
	)

	return attrs
}

// getDatabaseAttributes returns database semantic convention attributes.
func (e *Engine) getDatabaseAttributes(svc *config.ServiceConfig, ep *config.EndpointConfig) []attribute.KeyValue {
	attrs := []attribute.KeyValue{}

	if svc.System != "" {
		attrs = append(attrs, semconv.DBSystemKey.String(svc.System))
	}

	if svc.Connection != nil {
		if svc.Connection.Database != "" {
			attrs = append(attrs, semconv.DBName(svc.Connection.Database))
		}
		if svc.Connection.Host != "" {
			attrs = append(attrs, semconv.ServerAddress(svc.Connection.Host))
		}
		if svc.Connection.Port != 0 {
			attrs = append(attrs, semconv.ServerPort(svc.Connection.Port))
		}
	}

	if ep.Operation != "" {
		attrs = append(attrs, semconv.DBOperation(ep.Operation))
	}
	if ep.Table != "" {
		attrs = append(attrs, semconv.DBSQLTable(ep.Table))
	}

	return attrs
}

// getMessagingAttributes returns messaging semantic convention attributes.
func (e *Engine) getMessagingAttributes(svc *config.ServiceConfig, ep *config.EndpointConfig) []attribute.KeyValue {
	attrs := []attribute.KeyValue{}

	if svc.System != "" {
		attrs = append(attrs, semconv.MessagingSystemKey.String(svc.System))
	}

	if ep.Topic != "" {
		attrs = append(attrs, semconv.MessagingDestinationName(ep.Topic))
	}

	switch ep.Type {
	case "messaging.producer":
		attrs = append(attrs, attribute.String("messaging.operation.type", "publish"))
	case "messaging.consumer":
		attrs = append(attrs, attribute.String("messaging.operation.type", "receive"))
	}

	// Generate a message ID
	e.mu.Lock()
	msgID := fmt.Sprintf("msg-%d", e.rng.Int63())
	partition := e.rng.Intn(6)
	e.mu.Unlock()

	attrs = append(attrs,
		semconv.MessagingMessageID(msgID),
		attribute.Int("messaging.kafka.destination.partition", partition),
	)

	return attrs
}

// calculateLatency calculates simulated latency based on configuration.
func (e *Engine) calculateLatency(cfg config.LatencyConfig) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()

	if cfg.FixedMs > 0 {
		return time.Duration(cfg.FixedMs) * time.Millisecond
	}

	switch cfg.Distribution {
	case "normal":
		return e.normalLatency(cfg.P50Ms, cfg.P95Ms)
	case "log_normal":
		return e.logNormalLatency(cfg.P50Ms, cfg.P95Ms, cfg.P99Ms)
	case "exponential":
		return e.exponentialLatency(cfg.P50Ms)
	default:
		// Default to simple randomization around P50
		variance := cfg.P50Ms * 0.3
		latency := cfg.P50Ms + (e.rng.Float64()*2-1)*variance
		if latency < 1 {
			latency = 1
		}
		return time.Duration(latency) * time.Millisecond
	}
}

// normalLatency generates normally distributed latency.
func (e *Engine) normalLatency(p50, p95 float64) time.Duration {
	// Approximate std dev from p50 and p95 (p95 is ~1.645 std devs from mean)
	stdDev := (p95 - p50) / 1.645
	latency := p50 + e.rng.NormFloat64()*stdDev
	if latency < 1 {
		latency = 1
	}
	return time.Duration(latency) * time.Millisecond
}

// logNormalLatency generates log-normally distributed latency.
func (e *Engine) logNormalLatency(p50, p95, p99 float64) time.Duration {
	// Use p50 as median and calculate sigma from p95
	if p95 <= p50 {
		p95 = p50 * 2
	}
	sigma := (math.Log(p95) - math.Log(p50)) / 1.645
	mu := math.Log(p50)

	z := e.rng.NormFloat64()
	latency := math.Exp(mu + sigma*z)
	if latency < 1 {
		latency = 1
	}
	return time.Duration(latency) * time.Millisecond
}

// exponentialLatency generates exponentially distributed latency.
func (e *Engine) exponentialLatency(p50 float64) time.Duration {
	// For exponential, p50 = ln(2) * mean
	mean := p50 / 0.693
	latency := e.rng.ExpFloat64() * mean
	if latency < 1 {
		latency = 1
	}
	return time.Duration(latency) * time.Millisecond
}

// selectErrorType selects an error type based on weights.
func (e *Engine) selectErrorType(types []config.ErrorType) *config.ErrorType {
	if len(types) == 0 {
		return nil
	}

	// Calculate total weight
	totalWeight := 0.0
	for _, t := range types {
		totalWeight += t.Weight
	}

	// Select based on weight
	r := e.rng.Float64() * totalWeight
	cumulative := 0.0
	for i := range types {
		cumulative += types[i].Weight
		if r <= cumulative {
			return &types[i]
		}
	}

	return &types[0]
}
