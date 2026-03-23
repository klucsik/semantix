#!/bin/bash
# Setup script for GitHub Actions -> GCE deployment
# Run this script from your terminal after setting PROJECT_ID

set -e

# ============================================
# CONFIGURE THESE VALUES
# ============================================
PROJECT_ID="dynatrace-dev-on-demand"
ZONE="us-central1-a"
SERVICE_ACCOUNT_NAME="semantix-deploy"

# ============================================
# Dynatrace API Token - set via environment variable
# ============================================
if [ -z "$DT_API_TOKEN" ]; then
  echo "ERROR: DT_API_TOKEN environment variable is not set"
  echo "Usage: DT_API_TOKEN=dt0c01.xxx ./setup-gcp-deploy.sh"
  exit 1
fi

if [ -z "$DT_ENDPOINT" ]; then
  echo "ERROR: DT_ENDPOINT environment variable is not set"
  echo "Usage: DT_ENDPOINT=https://xxx.live.dynatrace.com/api/v2/otlp DT_API_TOKEN=dt0c01.xxx ./setup-gcp-deploy.sh"
  exit 1
fi

# ============================================
# Don't edit below this line
# ============================================

SA_EMAIL="$SERVICE_ACCOUNT_NAME@$PROJECT_ID.iam.gserviceaccount.com"

echo "==> Setting project to $PROJECT_ID"
gcloud config set project $PROJECT_ID

echo "==> Enabling required APIs"
gcloud services enable compute.googleapis.com
gcloud services enable containerregistry.googleapis.com
gcloud services enable iam.googleapis.com

echo "==> Creating service account: $SERVICE_ACCOUNT_NAME"
if gcloud iam service-accounts describe $SA_EMAIL >/dev/null 2>&1; then
  echo "    Service account already exists"
else
  gcloud iam service-accounts create $SERVICE_ACCOUNT_NAME \
    --display-name="Semantix GCE Deployer"
  
  echo "    Waiting for service account to propagate..."
  sleep 10
  
  # Verify service account was created
  if ! gcloud iam service-accounts describe $SA_EMAIL >/dev/null 2>&1; then
    echo "ERROR: Service account creation failed. Please check your permissions."
    exit 1
  fi
  echo "    Service account created successfully"
fi

echo "==> Granting IAM permissions to $SA_EMAIL"

# Compute admin (create/manage VMs)
echo "    Adding roles/compute.admin..."
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:$SA_EMAIL" \
  --role="roles/compute.admin" \
  --quiet

# Storage admin (push container images to GCR)
echo "    Adding roles/storage.admin..."
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:$SA_EMAIL" \
  --role="roles/storage.admin" \
  --quiet

# Service account user (act as the compute service account)
echo "    Adding roles/iam.serviceAccountUser..."
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:$SA_EMAIL" \
  --role="roles/iam.serviceAccountUser" \
  --quiet

echo "==> Creating service account key"
if [ -f gcp-key.json ]; then
  echo "    gcp-key.json already exists, skipping key creation"
else
  gcloud iam service-accounts keys create gcp-key.json \
    --iam-account=$SA_EMAIL
  echo "    Key created: gcp-key.json"
fi

echo ""
echo "============================================"
echo "SETUP COMPLETE!"
echo "============================================"
echo ""
echo "Now add these secrets to GitHub:"
echo ""
echo "1. GCP_PROJECT_ID:"
echo "   $PROJECT_ID"
echo ""
echo "2. GCP_ZONE:"
echo "   $ZONE"
echo ""
echo "3. GCP_SA_KEY (the JSON content, not base64):"
echo "   Copy the contents of gcp-key.json"
echo ""
echo "4. DT_ENDPOINT:"
echo "   $DT_ENDPOINT"
echo ""
echo "5. DT_API_TOKEN:"
echo "   (your Dynatrace API token)"
echo ""
echo "To add secrets to GitHub, run:"
echo "  gh secret set GCP_PROJECT_ID --body \"$PROJECT_ID\" --repo mreider/semantix"
echo "  gh secret set GCP_ZONE --body \"$ZONE\" --repo mreider/semantix"
echo "  gh secret set GCP_SA_KEY < gcp-key.json --repo mreider/semantix"
echo "  gh secret set DT_ENDPOINT --body \"$DT_ENDPOINT\" --repo mreider/semantix"
echo "  gh secret set DT_API_TOKEN --body \"<your-token>\" --repo mreider/semantix"
echo ""
echo "IMPORTANT: Delete gcp-key.json after adding to GitHub!"
echo "  rm gcp-key.json"
echo ""
