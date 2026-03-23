# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.version=${VERSION:-dev}" \
    -o /semantix ./cmd/semantix

# Runtime stage
FROM scratch

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary
COPY --from=builder /semantix /semantix

# Copy example configs (can be overridden with volume mount)
COPY configs/ /configs/

# Set default environment variables
ENV CONFIG_DIR=/configs

# Run as non-root (UID 65534 is 'nobody')
USER 65534:65534

ENTRYPOINT ["/semantix"]
CMD ["--config-dir", "/configs"]
