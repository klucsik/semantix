#!/bin/bash
# Setup script for GitHub Actions -> Cloud Run deployment
# Run this script from your terminal after setting PROJECT_ID

set -e

# ============================================
# CONFIGURE THESE VALUES
# ============================================
PROJECT_ID="dynatrace-dev-on-demand"
REGION="us-central1"
SERVICE_ACCOUNT_NAME="semantix-deploy"

# ============================================
# Dynatrace API Token - set via environment variable
# ============================================
if [ -z "$DT_API_TOKEN" ]; then
  echo "ERROR: DT_API_TOKEN environment variable is not set"
  echo "Usage: DT_API_TOKEN=dt0c01.xxx ./setup-gcp-deploy.sh"
  exit 1
fi

# ============================================
# Don't edit below this line
# ============================================

SA_EMAIL="$SERVICE_ACCOUNT_NAME@$PROJECT_ID.iam.gserviceaccount.com"

echo "==> Setting project to $PROJECT_ID"
gcloud config set project $PROJECT_ID

echo "==> Enabling required APIs"
gcloud services enable run.googleapis.com
gcloud services enable cloudbuild.googleapis.com
gcloud services enable secretmanager.googleapis.com
gcloud services enable iam.googleapis.com

echo "==> Creating service account: $SERVICE_ACCOUNT_NAME"
if gcloud iam service-accounts describe $SA_EMAIL >/dev/null 2>&1; then
  echo "    Service account already exists"
else
  gcloud iam service-accounts create $SERVICE_ACCOUNT_NAME \
    --display-name="Semantix Cloud Run Deployer"
  
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

# Cloud Run admin (deploy services)
echo "    Adding roles/run.admin..."
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:$SA_EMAIL" \
  --role="roles/run.admin" \
  --quiet

# Storage admin (push container images)
echo "    Adding roles/storage.admin..."
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:$SA_EMAIL" \
  --role="roles/storage.admin" \
  --quiet

# Service account user (act as the Cloud Run service account)
echo "    Adding roles/iam.serviceAccountUser..."
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:$SA_EMAIL" \
  --role="roles/iam.serviceAccountUser" \
  --quiet

# Cloud Build editor (submit builds)
echo "    Adding roles/cloudbuild.builds.editor..."
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:$SA_EMAIL" \
  --role="roles/cloudbuild.builds.editor" \
  --quiet

# Artifact Registry writer (if using Artifact Registry)
echo "    Adding roles/artifactregistry.writer..."
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:$SA_EMAIL" \
  --role="roles/artifactregistry.writer" \
  --quiet

echo "==> Creating service account key"
if [ -f gcp-key.json ]; then
  echo "    gcp-key.json already exists, skipping key creation"
else
  gcloud iam service-accounts keys create gcp-key.json \
    --iam-account=$SA_EMAIL
  echo "    Key created: gcp-key.json"
fi

echo "==> Storing Dynatrace API token in Secret Manager"
if gcloud secrets describe dynatrace-api-token >/dev/null 2>&1; then
  echo "    Secret already exists, adding new version..."
  echo -n "$DT_API_TOKEN" | gcloud secrets versions add dynatrace-api-token --data-file=-
else
  echo "    Creating new secret..."
  echo -n "$DT_API_TOKEN" | gcloud secrets create dynatrace-api-token --data-file=-
fi

# Grant Cloud Run access to the secret
echo "==> Granting Cloud Run access to secret"
PROJECT_NUMBER=$(gcloud projects describe $PROJECT_ID --format='value(projectNumber)')
gcloud secrets add-iam-policy-binding dynatrace-api-token \
  --member="serviceAccount:$PROJECT_NUMBER-compute@developer.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor" \
  --quiet

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
echo "2. GCP_REGION:"
echo "   $REGION"
echo ""
echo "3. GCP_SA_KEY (base64 encoded):"
echo "   Run: cat gcp-key.json | base64"
echo ""
echo "To add secrets to GitHub, run:"
echo "  gh secret set GCP_PROJECT_ID --body \"$PROJECT_ID\" --repo mreider/semantix"
echo "  gh secret set GCP_REGION --body \"$REGION\" --repo mreider/semantix"
echo "  gh secret set GCP_SA_KEY --body \"\$(cat gcp-key.json | base64)\" --repo mreider/semantix"
echo ""
echo "IMPORTANT: Delete gcp-key.json after adding to GitHub!"
echo "  rm gcp-key.json"
echo ""
