// Package telemetry provides log generation with trace correlation.
package telemetry

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/mreider/semantix/internal/config"
)

// LogLevel represents log severity.
type LogLevel int

const (
	LogLevelDebug LogLevel = 5
	LogLevelInfo  LogLevel = 9
	LogLevelWarn  LogLevel = 13
	LogLevelError LogLevel = 17
)

// LogEntry represents a log record to be exported.
type LogEntry struct {
	Timestamp      time.Time
	SeverityNumber LogLevel
	SeverityText   string
	Body           string
	TraceID        string
	SpanID         string
	Attributes     map[string]string
	ServiceName    string
}

// Logger generates correlated logs for the simulation.
type Logger struct {
	cfg      *config.LogsConfig
	patterns map[string][]config.LogPattern // patterns by service name
	rng      *rand.Rand
	entryCh  chan LogEntry
}

// NewLogger creates a new log generator.
func NewLogger(cfg *config.LogsConfig, seed int64) *Logger {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	l := &Logger{
		cfg:      cfg,
		patterns: make(map[string][]config.LogPattern),
		rng:      rand.New(rand.NewSource(seed)),
		entryCh:  make(chan LogEntry, 1000),
	}

	// Index patterns by service
	if cfg != nil {
		for _, p := range cfg.Patterns {
			l.patterns[p.Service] = append(l.patterns[p.Service], p)
		}
	}

	return l
}

// GenerateLogs generates log entries for a request.
func (l *Logger) GenerateLogs(ctx context.Context, serviceName string, isError bool) []LogEntry {
	if l.cfg == nil || !l.cfg.Enabled {
		return nil
	}

	patterns, ok := l.patterns[serviceName]
	if !ok {
		return nil
	}

	// Get trace context
	spanCtx := trace.SpanContextFromContext(ctx)
	traceID := ""
	spanID := ""
	if spanCtx.IsValid() {
		traceID = spanCtx.TraceID().String()
		spanID = spanCtx.SpanID().String()
	}

	var entries []LogEntry

	for _, pattern := range patterns {
		// Skip error patterns if not an error
		if pattern.OnError && !isError {
			continue
		}

		// Skip non-error patterns if this is an error (we want error logs)
		if isError && !pattern.OnError && pattern.Level == "ERROR" {
			continue
		}

		// Check rate
		if pattern.RatePerRequest > 0 && l.rng.Float64() > pattern.RatePerRequest {
			continue
		}

		// Select a random message
		if len(pattern.Messages) == 0 {
			continue
		}
		msg := pattern.Messages[l.rng.Intn(len(pattern.Messages))]

		// Interpolate variables
		msg = l.interpolateMessage(msg)

		entry := LogEntry{
			Timestamp:      time.Now(),
			SeverityNumber: l.parseSeverity(pattern.Level),
			SeverityText:   pattern.Level,
			Body:           msg,
			TraceID:        traceID,
			SpanID:         spanID,
			ServiceName:    serviceName,
			Attributes: map[string]string{
				"service.name": serviceName,
			},
		}

		entries = append(entries, entry)
	}

	return entries
}

// parseSeverity converts level string to severity number.
func (l *Logger) parseSeverity(level string) LogLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return LogLevelDebug
	case "INFO":
		return LogLevelInfo
	case "WARN", "WARNING":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

// interpolateMessage replaces placeholders with generated values.
func (l *Logger) interpolateMessage(msg string) string {
	// Pattern to match {variable_name}
	re := regexp.MustCompile(`\{([^}]+)\}`)

	return re.ReplaceAllStringFunc(msg, func(match string) string {
		varName := strings.Trim(match, "{}")

		switch varName {
		case "user_id":
			return fmt.Sprintf("user-%d", l.rng.Intn(10000))
		case "order_id":
			return fmt.Sprintf("ord-%s", l.randomString(8))
		case "request_id":
			return fmt.Sprintf("req-%s", l.randomString(12))
		case "exception.message":
			return l.randomExceptionMessage()
		case "trace_id":
			return l.randomString(32)
		case "span_id":
			return l.randomString(16)
		case "latency_ms":
			return fmt.Sprintf("%d", l.rng.Intn(1000)+1)
		case "status_code":
			codes := []int{200, 201, 400, 404, 500, 503}
			return fmt.Sprintf("%d", codes[l.rng.Intn(len(codes))])
		default:
			return match // Keep original if unknown
		}
	})
}

// randomString generates a random alphanumeric string.
func (l *Logger) randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[l.rng.Intn(len(charset))]
	}
	return string(b)
}

// randomExceptionMessage returns a random exception message.
func (l *Logger) randomExceptionMessage() string {
	messages := []string{
		"Connection refused",
		"Connection timed out after 30000ms",
		"Database connection pool exhausted",
		"Failed to acquire lock",
		"Transaction rolled back",
		"Null pointer exception",
		"Index out of bounds",
		"Resource not found",
		"Authentication failed",
		"Rate limit exceeded",
	}
	return messages[l.rng.Intn(len(messages))]
}

// ToOTelAttributes converts log attributes to OTel attributes.
func (e *LogEntry) ToOTelAttributes() []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(e.Attributes)+4)

	for k, v := range e.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	if e.TraceID != "" {
		attrs = append(attrs, attribute.String("trace_id", e.TraceID))
	}
	if e.SpanID != "" {
		attrs = append(attrs, attribute.String("span_id", e.SpanID))
	}

	return attrs
}
