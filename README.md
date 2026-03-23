# Semantix

A declarative OpenTelemetry simulation engine that generates realistic distributed system telemetry from a single container.

## Overview

Semantix reads YAML configuration files that describe virtual microservice architectures and emits traces, metrics, and logs to Dynatrace's OTLP endpoints—making the backend believe it's observing a complex distributed application when it's actually a single process emitting simulated telemetry.

### Key Features

- **100% OpenTelemetry Semantics**: Follows OTel semantic conventions for HTTP, database, and messaging spans
- **Declarative Configuration**: Define your entire topology in YAML
- **Realistic Traffic Patterns**: Configurable request rates, latency distributions, and error rates
- **Span Links for Async**: Proper correlation across message queues (Kafka, RabbitMQ)
- **Correlated Signals**: Traces, metrics, and logs with proper trace context correlation
- **Chaos/Anomaly Injection**: Simulate latency spikes, error bursts, and scheduled degradation scenarios
- **Time-Based Scenarios**: Schedule traffic multipliers for business hours, off-peak, flash sales, etc.

## Quick Start

### Prerequisites

- Go 1.22+ (or Docker)
- Dynatrace environment with OTLP endpoints enabled
- API token with `openTelemetryTrace.ingest`, `metrics.ingest`, and `logs.ingest` scopes

### Installation

```bash
# Clone the repository
git clone https://github.com/mreider/semantix.git
cd semantix

# Build
go build -o semantix ./cmd/semantix

# Or install directly
go install github.com/mreider/semantix/cmd/semantix@latest
```

### Configuration

Set your Dynatrace credentials:

```bash
export DT_ENDPOINT="https://YOUR_ENV.live.dynatrace.com/api/v2/otlp"
export DT_API_TOKEN="dt0c01.YOUR_TOKEN"
```

### Run

```bash
# Run with example e-commerce configuration
./semantix --config ./configs/examples/ecommerce.yaml

# Or run all configs in a directory
./semantix --config-dir ./configs/examples

# Validate configuration without running
./semantix --config ./configs/examples/ecommerce.yaml --dry-run
```

## Configuration Schema

```yaml
version: "1.0"

simulation:
  duration: "24h"           # How long to run (or "infinite")
  tick_interval: "100ms"    # Simulation clock interval
  seed: 12345               # For reproducible randomness

exporter:
  endpoint: "${DT_ENDPOINT:-http://localhost:4318}"  # Env var with default
  token: "${DT_API_TOKEN:-}"
  protocol: "http/protobuf"

global_resource_attributes:
  deployment.environment: "production"
  cloud.provider: "gcp"

services:
  - name: "frontend"
    version: "2.1.0"
    endpoints:
      - name: "checkout"
        type: "http.server"
        method: "POST"
        route: "/api/v1/checkout"
        traffic:
          requests_per_minute: 60
          burst_probability: 0.1
          burst_multiplier: 3
        latency:
          distribution: "log_normal"
          p50_ms: 45
          p95_ms: 180
        errors:
          rate: 0.02
          types:
            - code: 500
              message: "Internal Server Error"
              weight: 1.0
        anomalies:
          - type: "latency_spike"
            probability: 0.05
            multiplier: 10
            duration: "5m"
        calls:
          - service: "orders"
            endpoint: "create_order"
          - service: "inventory"
            endpoint: "check_stock"
            parallel: true
            
  - name: "kafka"
    type: "messaging"
    system: "kafka"
    endpoints:
      - name: "publish_order"
        type: "messaging.producer"
        topic: "orders"
        
scenarios:
  - name: "business_hours"
    schedule: "0 9-17 * * MON-FRI"
    traffic_multiplier: 1.0
  - name: "flash_sale"
    schedule: "0 12 * * FRI"
    traffic_multiplier: 5.0
```

See [configs/examples/ecommerce.yaml](configs/examples/ecommerce.yaml) for a complete example, or [configs/examples/minimal.yaml](configs/examples/minimal.yaml) for a simple 2-service configuration.

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│                       SEMANTIX ENGINE                         │
├───────────────────────────────────────────────────────────────┤
│  Config Loader -> Service Registry -> Simulation Engine      │
│                                                               │
│  ┌─────────────┐  ┌─────────────┐  ┌───────────────────────┐  │
│  │ Traffic     │  │ Trace       │  │ Anomaly               │  │
│  │ Generator   │  │ Builder     │  │ Manager               │  │
│  └─────────────┘  └─────────────┘  └───────────────────────┘  │
│  ┌─────────────┐  ┌─────────────┐  ┌───────────────────────┐  │
│  │ Metrics     │  │ Logger      │  │ Scenario              │  │
│  │ Collector   │  │ (Correlated)│  │ Manager               │  │
│  └─────────────┘  └─────────────┘  └───────────────────────┘  │
│                          │                                    │
│                          v                                    │
│             ┌─────────────────────────┐                       │
│             │    OTLP Exporters       │                       │
│             │ (Traces/Metrics/Logs)   │                       │
│             └────────────┬────────────┘                       │
└──────────────────────────┼────────────────────────────────────┘
                           │
                           v
              ┌─────────────────────────┐
              │   Dynatrace OTLP API    │
              └─────────────────────────┘
```

## Deployment

### Local (no Docker)

```bash
# Build
go build -o semantix ./cmd/semantix

# Run
export DT_ENDPOINT="https://xxx.live.dynatrace.com/api/v2/otlp"
export DT_API_TOKEN="dt0c01.xxx"
./semantix --config configs/examples/ecommerce.yaml
```

### Google Cloud Run (recommended)

```bash
# Deploy directly from source (no local Docker needed)
gcloud run deploy semantix \
  --source . \
  --region us-central1 \
  --set-env-vars "DT_ENDPOINT=https://xxx.live.dynatrace.com/api/v2/otlp" \
  --set-secrets "DT_API_TOKEN=dynatrace-api-token:latest" \
  --memory 256Mi \
  --cpu 1 \
  --min-instances 1 \
  --max-instances 1

# Or use Cloud Build explicitly
gcloud builds submit --config cloudbuild.yaml \
  --substitutions=_DT_ENDPOINT="https://xxx.live.dynatrace.com/api/v2/otlp"
```

### Docker (optional)

```bash
# Build
docker build -t semantix .

# Run
docker run -d \
  -e DT_ENDPOINT="https://xxx.live.dynatrace.com/api/v2/otlp" \
  -e DT_API_TOKEN="dt0c01.xxx" \
  -v $(pwd)/configs:/configs \
  semantix --config-dir /configs
```

## CLI Options

| Flag | Description | Default |
|------|-------------|---------|
| `--config` | Single YAML configuration file | - |
| `--config-dir` | Directory of YAML configs (loads all) | `./configs` |
| `--dry-run` | Validate config without running | `false` |
| `--version` | Show version info | - |

## Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `DT_ENDPOINT` | Dynatrace OTLP endpoint | Yes |
| `DT_API_TOKEN` | Dynatrace API token | Yes |

## Telemetry Generated

### Traces
- HTTP server/client spans with full semantic conventions
- Database spans (PostgreSQL, MySQL, etc.)
- Messaging spans (Kafka producer/consumer)
- Span links for async correlation across message queues
- Proper parent-child relationships for synchronous calls

### Metrics
- `http.server.request.duration` - HTTP server latency histogram
- `http.server.request.count` - HTTP request counter
- `http.client.request.duration` - HTTP client latency histogram
- `db.client.operation.duration` - Database operation latency
- `messaging.publish.duration` - Message publish latency
- `messaging.receive.duration` - Message receive latency
- Custom business metrics (configurable)

### Logs
- Correlated with trace_id and span_id
- Configurable severity levels (DEBUG, INFO, WARN, ERROR)
- Template-based message generation
- Error logs on failed requests

## Examples

### E-commerce Application

The included [ecommerce.yaml](configs/examples/ecommerce.yaml) simulates:

- **Frontend**: Web gateway handling checkout and dashboard requests
- **Orders**: Order creation, database writes, Kafka publishing
- **Inventory**: Stock checking and reservation
- **Fulfillment**: Kafka consumer for order processing with fraud detection
- **Shipping**: Shipment creation and tracking
- **PostgreSQL**: Database operations with realistic latency
- **Kafka**: Message broker with span links for async correlation

Traffic patterns:
- 60 req/min on checkout, 120 req/min on dashboard
- 2% base error rate with 500/503 responses
- Periodic latency spikes and error bursts
- Business hours / off-peak traffic multipliers

## Development

```bash
# Run tests
go test ./...

# Build for all platforms
GOOS=linux GOARCH=amd64 go build -o semantix-linux ./cmd/semantix
GOOS=darwin GOARCH=arm64 go build -o semantix-darwin ./cmd/semantix
GOOS=windows GOARCH=amd64 go build -o semantix.exe ./cmd/semantix
```

## License

MIT
