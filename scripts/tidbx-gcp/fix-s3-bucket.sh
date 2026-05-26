#!/bin/bash
#
# Quick fix script to create S3 bucket in MinIO
# Use this if the bucket wasn't created during deployment
#

set -e

if [[ -z "$1" ]]; then
    echo "Usage: $0 <cluster-prefix>"
    exit 1
fi
CLUSTER_NAME="$1"
if [[ ! "$CLUSTER_NAME" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,18}[a-zA-Z0-9])?$ ]]; then
    echo "Error: cluster prefix must be 1-20 chars, alphanumeric or hyphens, and cannot start/end with a hyphen"
    exit 1
fi
MINIO_HOST="${CLUSTER_NAME}-minio"
BUCKET_NAME="${MINIO_BUCKET:-$(echo "$CLUSTER_NAME" | tr '[:upper:]' '[:lower:]')-cse}"
CURRENT_USER=$(whoami)

echo "========================================"
echo "S3 Bucket Creation Fix"
echo "========================================"
echo "Cluster:  $CLUSTER_NAME"
echo "MinIO:    $MINIO_HOST"
echo "Bucket:   $BUCKET_NAME"
echo "========================================"

# Check if MinIO is accessible
echo "Checking MinIO accessibility..."
if ! ssh -o StrictHostKeyChecking=no "$CURRENT_USER@$MINIO_HOST" "curl -s --max-time 5 http://localhost:9000 > /dev/null 2>&1"; then
    echo "Error: MinIO is not accessible on $MINIO_HOST"
    exit 1
fi
echo "✓ MinIO is accessible"

# Install mc and create bucket
echo "Creating S3 bucket..."
ssh -o StrictHostKeyChecking=no "$CURRENT_USER@$MINIO_HOST" "
    # Install MinIO client (mc) if not present
    if ! command -v mc &> /dev/null; then
        echo 'Installing MinIO client (mc)...'
        sudo wget -q https://dl.min.io/client/mc/release/linux-amd64/mc -O /usr/local/bin/mc
        sudo chmod +x /usr/local/bin/mc
        echo '✓ mc installed'
    else
        echo '✓ mc already installed'
    fi

    # Configure MinIO connection
    mc alias set myminio http://localhost:9000 minioadmin minioadmin &> /dev/null

    # Create bucket if it doesn't exist
    if mc ls myminio/$BUCKET_NAME &> /dev/null; then
        echo '✓ Bucket $BUCKET_NAME already exists'
    else
        echo 'Creating bucket $BUCKET_NAME...'
        mc mb myminio/$BUCKET_NAME
        if [ \$? -eq 0 ]; then
            echo '✓ Bucket $BUCKET_NAME created successfully'
        else
            echo '✗ Failed to create bucket $BUCKET_NAME'
            exit 1
        fi
    fi

    # List buckets to verify
    echo ''
    echo 'Current buckets in MinIO:'
    mc ls myminio/
"

echo ""
echo "========================================"
echo "✓ S3 Bucket Fix Completed"
echo "========================================"
echo ""
echo "The TiKV errors should stop now."
echo "If TiKV is still showing errors, wait 1-2 minutes for retry logic to work."
