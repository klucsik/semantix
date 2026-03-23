#!/bin/bash
# Startup script for GCE instance with Caddy + Semantix
# This runs on Container-Optimized OS

set -e

# Get metadata
get_metadata() {
    curl -sf "http://metadata.google.internal/computeMetadata/v1/instance/attributes/$1" -H "Metadata-Flavor: Google" || echo ""
}

export GCP_PROJECT_ID=$(curl -sf "http://metadata.google.internal/computeMetadata/v1/project/project-id" -H "Metadata-Flavor: Google")
export DT_ENDPOINT=$(get_metadata "DT_ENDPOINT")
export DT_API_TOKEN=$(get_metadata "DT_API_TOKEN")
export VERSION=$(get_metadata "VERSION")
export DOMAIN=$(get_metadata "DOMAIN")

# Default domain
DOMAIN="${DOMAIN:-semantix.mreider.com}"

# Create docker-compose.yml
cat > /home/chronos/docker-compose.yml << 'COMPOSE'
version: "3.8"

services:
  caddy:
    image: caddy:2-alpine
    restart: always
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - caddy_data:/data
      - caddy_config:/config
    command: caddy reverse-proxy --from ${DOMAIN} --to semantix:8080
    depends_on:
      - semantix

  semantix:
    image: gcr.io/${GCP_PROJECT_ID}/semantix:${VERSION:-latest}
    restart: always
    environment:
      - DT_ENDPOINT=${DT_ENDPOINT}
      - DT_API_TOKEN=${DT_API_TOKEN}
      - VERSION=${VERSION}

volumes:
  caddy_data:
  caddy_config:
COMPOSE

# Authenticate to GCR
docker-credential-gcr configure-docker --registries=gcr.io

# Run with docker-compose (using docker run since COS doesn't have compose)
cd /home/chronos

# Pull images
docker pull caddy:2-alpine
docker pull "gcr.io/${GCP_PROJECT_ID}/semantix:${VERSION:-latest}"

# Create network
docker network create semantix-net 2>/dev/null || true

# Stop existing containers
docker stop semantix caddy 2>/dev/null || true
docker rm semantix caddy 2>/dev/null || true

# Create volumes
docker volume create caddy_data 2>/dev/null || true
docker volume create caddy_config 2>/dev/null || true

# Start semantix
docker run -d \
    --name semantix \
    --network semantix-net \
    --restart always \
    -e "DT_ENDPOINT=${DT_ENDPOINT}" \
    -e "DT_API_TOKEN=${DT_API_TOKEN}" \
    -e "VERSION=${VERSION}" \
    "gcr.io/${GCP_PROJECT_ID}/semantix:${VERSION:-latest}"

# Start caddy
docker run -d \
    --name caddy \
    --network semantix-net \
    --restart always \
    -p 80:80 \
    -p 443:443 \
    -v caddy_data:/data \
    -v caddy_config:/config \
    caddy:2-alpine \
    caddy reverse-proxy --from "${DOMAIN}" --to semantix:8080

echo "Startup complete"
