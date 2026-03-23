#!/bin/bash
# Build and push container image to GCR for GCE deployment

set -e

PROJECT_ID="${GCP_PROJECT_ID:-$(gcloud config get-value project)}"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"

echo "Building Semantix container image"
echo "  Project: $PROJECT_ID"
echo "  Version: $VERSION"
echo ""

# Configure Docker to use gcloud credentials
gcloud auth configure-docker gcr.io --quiet

# Build the image
docker build \
    -t "gcr.io/$PROJECT_ID/semantix:$VERSION" \
    -t "gcr.io/$PROJECT_ID/semantix:latest" \
    --build-arg "VERSION=$VERSION" \
    .

# Push to GCR
echo "Pushing to Google Container Registry..."
docker push "gcr.io/$PROJECT_ID/semantix:$VERSION"
docker push "gcr.io/$PROJECT_ID/semantix:latest"

echo ""
echo "Image pushed successfully:"
echo "  gcr.io/$PROJECT_ID/semantix:$VERSION"
echo "  gcr.io/$PROJECT_ID/semantix:latest"
