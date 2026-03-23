#!/bin/bash
# Deploy Semantix to Google Compute Engine
# This creates an e2-micro instance (free tier eligible in us-west1, us-central1, us-east1)

set -e

# Configuration - modify these or set as environment variables
PROJECT_ID="${GCP_PROJECT_ID:-$(gcloud config get-value project)}"
ZONE="${GCP_ZONE:-us-central1-a}"
INSTANCE_NAME="${INSTANCE_NAME:-semantix}"
MACHINE_TYPE="${MACHINE_TYPE:-e2-micro}"

# Dynatrace configuration (required)
DT_ENDPOINT="${DT_ENDPOINT:?Error: DT_ENDPOINT environment variable is required}"
DT_API_TOKEN="${DT_API_TOKEN:?Error: DT_API_TOKEN environment variable is required}"

echo "Deploying Semantix to Compute Engine"
echo "  Project:  $PROJECT_ID"
echo "  Zone:     $ZONE"
echo "  Instance: $INSTANCE_NAME"
echo "  Machine:  $MACHINE_TYPE"
echo ""

# Check if instance exists
if gcloud compute instances describe "$INSTANCE_NAME" --zone="$ZONE" --project="$PROJECT_ID" &>/dev/null; then
    echo "Instance already exists. Updating..."
    
    # Stop, update metadata, and restart
    gcloud compute instances stop "$INSTANCE_NAME" \
        --zone="$ZONE" \
        --project="$PROJECT_ID"
    
    gcloud compute instances add-metadata "$INSTANCE_NAME" \
        --zone="$ZONE" \
        --project="$PROJECT_ID" \
        --metadata="DT_ENDPOINT=$DT_ENDPOINT,DT_API_TOKEN=$DT_API_TOKEN"
    
    gcloud compute instances start "$INSTANCE_NAME" \
        --zone="$ZONE" \
        --project="$PROJECT_ID"
    
    echo "Instance updated and restarted"
else
    echo "Creating new instance..."
    
    # Create the instance with Container-Optimized OS
    gcloud compute instances create-with-container "$INSTANCE_NAME" \
        --project="$PROJECT_ID" \
        --zone="$ZONE" \
        --machine-type="$MACHINE_TYPE" \
        --image-family="cos-stable" \
        --image-project="cos-cloud" \
        --boot-disk-size="10GB" \
        --boot-disk-type="pd-standard" \
        --container-image="gcr.io/$PROJECT_ID/semantix:latest" \
        --container-env="DT_ENDPOINT=$DT_ENDPOINT,DT_API_TOKEN=$DT_API_TOKEN" \
        --container-restart-policy="always" \
        --tags="http-server" \
        --scopes="https://www.googleapis.com/auth/logging.write,https://www.googleapis.com/auth/monitoring.write"
    
    # Create firewall rule for dashboard access (optional)
    if ! gcloud compute firewall-rules describe allow-semantix-http --project="$PROJECT_ID" &>/dev/null; then
        echo "Creating firewall rule for HTTP access..."
        gcloud compute firewall-rules create allow-semantix-http \
            --project="$PROJECT_ID" \
            --allow="tcp:8080" \
            --target-tags="http-server" \
            --description="Allow HTTP access to Semantix dashboard"
    fi
    
    echo "Instance created successfully"
fi

# Get external IP
EXTERNAL_IP=$(gcloud compute instances describe "$INSTANCE_NAME" \
    --zone="$ZONE" \
    --project="$PROJECT_ID" \
    --format="get(networkInterfaces[0].accessConfigs[0].natIP)")

echo ""
echo "Deployment complete!"
echo "  Dashboard: http://$EXTERNAL_IP:8080"
echo ""
echo "Useful commands:"
echo "  View logs:    gcloud compute ssh $INSTANCE_NAME --zone=$ZONE -- 'docker logs \$(docker ps -q)'"
echo "  SSH into VM:  gcloud compute ssh $INSTANCE_NAME --zone=$ZONE"
echo "  Stop:         gcloud compute instances stop $INSTANCE_NAME --zone=$ZONE"
echo "  Delete:       gcloud compute instances delete $INSTANCE_NAME --zone=$ZONE"
