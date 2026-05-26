#!/bin/bash

# Exit immediately if a command exits with a non-zero status
set -euo pipefail

export PATH="$HOME/.tiup/bin:$PATH"

# Function to display usage information
usage() {
    echo "Usage: $0 [-n NAME] [-v VERSION] [-p PD_PATCH] [-d TIDB_PATCH] [-k TIKV_PATCH] [--small-region] [--stress-remote-cop] [--skip-patches] [--force]"
    echo ""
    echo "Deploy TiDB-X (Next-Gen) cluster with binary patching for GCP instances"
    echo "Automatically generates cluster config based on GCP instance naming pattern."
    echo ""
    echo "Options:"
    echo "  -n, --name              Name of the cluster (required, unique prefix)"
    echo "  -v, --version           TiDB version to deploy (default: v8.5.4)"
    echo "  -p, --pd-patch          Path to PD tarball (default: /tmp/pd.tar.gz)"
    echo "  -d, --tidb-patch        Path to TiDB tarball (default: /tmp/tidb.tar.gz)"
    echo "  -k, --tikv-patch        Path to TiKV tarball (default: /tmp/tikv.tar.gz; also contains tikv-worker)"
    echo "  --stress-remote-cop     Use aggressive thresholds to maximize remote cop hit rate"
    echo "  --skip-patches          Skip binary patching step"
    echo "  --skip-start            Deploy and patch only, don't start cluster"
    echo "  --force                 Destroy existing cluster without prompting (non-interactive)"
    echo "  -h, --help              Display this help message"
    echo ""
    echo "Examples:"
    echo "  $0 -n test-nextgen -v v8.5.4"
    echo "  $0 -n my-cluster --skip-patches"
    echo "  $0 -n test-nextgen --small-region"
    echo "  $0 -n bench --stress-remote-cop"
    exit 1
}

# Function to check if file exists
check_file_exists() {
    local file=$1
    local description=$2

    if [[ ! -f "$file" ]]; then
        echo "Error: $description not found at: $file"
        echo "Please ensure the file exists or use appropriate flag to specify location"
        return 1
    fi
    return 0
}

# Validate cluster name / VM prefix. Keep this in sync with render_gcp_config.sh.
validate_cluster_name() {
    local name=$1
    if [[ -z "$name" ]]; then
        echo "Error: --name is required and must match the GCP VM prefix"
        return 1
    fi
    if [[ ! "$name" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,18}[a-zA-Z0-9])?$ ]]; then
        echo "Error: --name must be 1-20 chars, alphanumeric or hyphens, and cannot start/end with a hyphen"
        return 1
    fi
    return 0
}

default_bucket_name() {
    echo "$(echo "$CLUSTER_NAME" | tr '[:upper:]' '[:lower:]')-cse"
}

effective_bucket_name() {
    echo "${MINIO_BUCKET:-$(default_bucket_name)}"
}

pd_hosts() {
    printf '%s\n' \
        "${CLUSTER_NAME}-pd-0" \
        "${CLUSTER_NAME}-pd-1" \
        "${CLUSTER_NAME}-pd-2"
}

pd_endpoints() {
    local endpoints=()
    local host
    while read -r host; do
        endpoints+=("http://${host}:2379")
    done < <(pd_hosts)
    local IFS=,
    printf '%s' "${endpoints[*]}"
}

count_topology_hosts() {
    local start_marker=$1
    local end_marker=$2

    awk -v start="$start_marker" -v end="$end_marker" '
        $1 == start { in_section = 1; next }
        in_section && $1 == end { in_section = 0 }
        in_section && $1 == "-" && $2 == "host:" { count++ }
        END { print count + 0 }
    ' "$CONFIG_FILE"
}

validate_generated_topology() {
    local pd_count
    local tidb_count
    local tikv_count

    pd_count=$(count_topology_hosts "pd_servers:" "tidb_servers:")
    tidb_count=$(count_topology_hosts "tidb_servers:" "tikv_servers:")
    tikv_count=$(count_topology_hosts "tikv_servers:" "monitoring_servers:")

    [[ "$pd_count" == "3" ]] || { echo "Error: expected 3 pd_servers, got $pd_count"; return 1; }
    [[ "$tidb_count" == "2" ]] || { echo "Error: expected 2 tidb_servers, got $tidb_count"; return 1; }
    [[ "$tikv_count" == "3" ]] || { echo "Error: expected 3 tikv_servers, got $tikv_count"; return 1; }

    echo "✓ Generated topology matches expected role counts"
}

verify_cluster_display() {
    local display_output
    display_output=$(tiup cluster display "$CLUSTER_NAME")
    printf '%s\n' "$display_output"

    local host
    for host in \
        "${CLUSTER_NAME}-pd-0" \
        "${CLUSTER_NAME}-pd-1" \
        "${CLUSTER_NAME}-pd-2" \
        "${CLUSTER_NAME}-tikv-0" \
        "${CLUSTER_NAME}-tikv-1" \
        "${CLUSTER_NAME}-tikv-2" \
        "${CLUSTER_NAME}-tidb-system" \
        "${CLUSTER_NAME}-tidb-0"; do
        if ! grep -q "$host" <<<"$display_output"; then
            echo "Error: cluster display output is missing expected host: $host"
            return 1
        fi
    done

    echo "✓ Cluster display contains expected PD/TiKV/TiDB hosts"
}

verify_tikv_worker_endpoints() {
    local tikv_worker_host="${CLUSTER_NAME}-tikv-worker"
    local process_info
    process_info=$(ssh -o StrictHostKeyChecking=no "tidb@${tikv_worker_host}" "ps -ef | grep '[t]ikv-worker'") || {
        echo "Error: failed to inspect tikv-worker process on ${tikv_worker_host}"
        return 1
    }

    local host
    while read -r host; do
        if ! grep -q "$host:2379" <<<"$process_info"; then
            echo "Error: tikv-worker process is missing PD endpoint for $host"
            return 1
        fi
    done < <(pd_hosts)

    echo "✓ tikv-worker process is using all three PD endpoints"
}

run_smoke_test() {
    local user_tidb_host="${CLUSTER_NAME}-tidb-0"
    local system_tidb_host="${CLUSTER_NAME}-tidb-system"
    local smoke_db="tidbx_gcp_smoke"

    mysql -h "$system_tidb_host" -P 3000 -u root -e "SELECT VERSION();" >/dev/null
    mysql -h "$user_tidb_host" -P 4000 -u root -e "SELECT VERSION();" >/dev/null

    mysql -h "$user_tidb_host" -P 4000 -u root <<SQL
DROP DATABASE IF EXISTS ${smoke_db};
CREATE DATABASE ${smoke_db};
CREATE TABLE ${smoke_db}.smoke (id BIGINT PRIMARY KEY, note VARCHAR(64));
INSERT INTO ${smoke_db}.smoke VALUES (1, 'ok');
SELECT COUNT(*) FROM ${smoke_db}.smoke;
SQL

    echo "✓ User TiDB smoke test succeeded"
}

verify_post_deploy_acceptance() {
    local user_tidb_host="${CLUSTER_NAME}-tidb-0"
    local cloud_storage_uri

    echo "=== Running Post-Deploy Acceptance Checks ==="
    validate_generated_topology
    verify_cluster_display
    verify_tikv_worker_endpoints
    run_smoke_test

    cloud_storage_uri=$(mysql -h "$user_tidb_host" -P 4000 -u root -N -s -e "SELECT @@tidb_cloud_storage_uri;" 2>/dev/null || true)
    if [[ -z "$cloud_storage_uri" ]]; then
        echo "Error: @@tidb_cloud_storage_uri is empty on ${user_tidb_host}"
        return 1
    fi

    if ! curl -sf "http://${CLUSTER_NAME}-tikv-worker:19000/metrics" >/dev/null; then
        echo "Error: tikv-worker metrics are not reachable"
        return 1
    fi

    echo "✓ Post-deploy acceptance checks passed"
}

# Function to generate next-gen cluster config
generate_nextgen_config() {
    echo "=== Generating Next-Gen Cluster Configuration ==="

    # Define server names based on cluster name (following GCP naming pattern)
    local MONITORING_SERVER="${CLUSTER_NAME}-load"
    local GRAFANA_SERVER="${CLUSTER_NAME}-load"
    local TIKV_SERVERS="${CLUSTER_NAME}-tikv-0,${CLUSTER_NAME}-tikv-1,${CLUSTER_NAME}-tikv-2"
    local SYSTEM_TIDB_SERVER="${CLUSTER_NAME}-tidb-system"
    local TIDB_SERVER="${CLUSTER_NAME}-tidb-0"
    local TIKV_WORKER_SERVER="${CLUSTER_NAME}-tikv-worker"
    local MINIO_SERVER="${CLUSTER_NAME}-minio"
    local TIKV_WORKER_COP_URL="http://$TIKV_WORKER_SERVER:19000/coprocessor"
    local PD_ENDPOINTS
    local PD_HOSTS
    local remote_cop_threshold_config=""

    PD_ENDPOINTS="$(pd_endpoints)"
    mapfile -t PD_HOSTS < <(pd_hosts)

    if [[ "$STRESS_REMOTE_COP" == true ]]; then
        remote_cop_threshold_config=$(cat << 'EOF_REMOTE_COP_STRESS'
    kvengine.remote-coprocessor-min-blocks-size: 1
    kvengine.remote-coprocessor-num-ranges: 1
EOF_REMOTE_COP_STRESS
)
    fi

    echo "Generating cluster topology..."
    echo "PD Servers: ${PD_HOSTS[*]}"
    echo "PD Endpoints: $PD_ENDPOINTS"
    echo "TiKV Servers: $TIKV_SERVERS"
    echo "System TiDB Server: $SYSTEM_TIDB_SERVER (port 3000)"
    echo "User TiDB Server: $TIDB_SERVER (port 4000)"
    echo "TiKV Worker Server: $TIKV_WORKER_SERVER (port 19000)"
    echo "MinIO Server: $MINIO_SERVER"

    local small_region_config=""
    if [[ "$SMALL_REGION" == true ]]; then
        small_region_config=$(cat << 'EOF_SMALL_REGION'
    coprocessor.region-bucket-size: "1KiB"
    coprocessor.region-split-keys: 4
    coprocessor.region-max-keys: 8
    coprocessor.region-split-size: "256KiB"
EOF_SMALL_REGION
)
        echo "Small region mode: enabled"
    fi

    # Compute bucket name before heredoc to avoid shell-code pollution in YAML
    local BUCKET_NAME
    BUCKET_NAME="$(effective_bucket_name)"

    # Add next-gen specific configurations for TiDB-X mode
    cat > "$CONFIG_FILE" << EOF
# TiDB-X (Next-Gen) Cluster Configuration
# Generated automatically for cluster: $CLUSTER_NAME

global:
  user: "tidb"
  ssh_port: 22
  deploy_dir: "/data/$CLUSTER_NAME/deploy"
  data_dir: "/data/$CLUSTER_NAME/data"
  arch: "amd64"

server_configs:
  tikv:
    storage.api-version: 2
    storage.enable-ttl: true
    coprocessor.enable-region-bucket: true
    kvengine.remote-worker-addr: "$TIKV_WORKER_COP_URL"
    kvengine.remote-coprocessor-addr: "$TIKV_WORKER_COP_URL"
$remote_cop_threshold_config
    dfs.prefix: "tikv"
    dfs.s3-endpoint: "http://$MINIO_SERVER:9000"
    dfs.s3-key-id: "minioadmin"
    dfs.s3-secret-key: "minioadmin"
    dfs.s3-bucket: "$BUCKET_NAME"
    dfs.s3-region: "local"
$small_region_config
  pd:
    replication.location-labels: ["zone", "host"]
    keyspace.pre-alloc: ["SYSTEM", "keyspace1"]
  tidb:
    security.auto-tls: true

pd_servers:
$(for pd_host in "${PD_HOSTS[@]}"; do
    echo "  - host: $pd_host"
done)

tidb_servers:
  # System TiDB - MUST start first with 15s wait before User TiDB
  - host: $SYSTEM_TIDB_SERVER
    port: 3000
    status_port: 10080
    config:
      instance.tidb_service_scope: "dxf_service"
      tikv-worker-url: "http://$TIKV_WORKER_SERVER:19000"
      keyspace-name: "SYSTEM"
      split-table: false
      use-autoscaler: false
      disaggregated-tiflash: false

  # User TiDB - Starts AFTER System TiDB (15s delay)
  - host: $TIDB_SERVER
    port: 4000
    status_port: 10081
    config:
      keyspace-name: "keyspace1"
      tikv-worker-url: "http://$TIKV_WORKER_SERVER:19000"
      enable-safe-point-v2: true
      split-table: false
      use-autoscaler: false
      disaggregated-tiflash: false

tikv_servers:
$(echo "$TIKV_SERVERS" | tr ',' '\n' | while read -r tikv_host; do
    echo "  - host: $tikv_host"
    echo "    port: 20160"
    echo "    status_port: 20180"
done)

monitoring_servers:
  - host: $MONITORING_SERVER

grafana_servers:
  - host: $GRAFANA_SERVER
EOF

    echo "Remote coprocessor offload: enabled"
    echo "  remote worker URL:       $TIKV_WORKER_COP_URL"
    if [[ "$STRESS_REMOTE_COP" == true ]]; then
        echo "  min blocks size (bytes): 1"
        echo "  num ranges threshold:    1"
        echo "  mode:                    stress"
    else
        echo "  thresholds:              TiKV defaults"
        echo "  mode:                    normal"
    fi

    validate_generated_topology
    echo "✓ Next-Gen cluster config generated: $CONFIG_FILE"
}

# Function to verify MinIO is accessible (simple check)
check_minio_status() {
    local minio_endpoint=$(grep "s3-endpoint" "$CONFIG_FILE" | cut -d'"' -f2 | head -1)

    if [[ -z "$minio_endpoint" ]]; then
        echo "No MinIO endpoint configured - skipping check"
        return 0
    fi

    echo "MinIO endpoint: $minio_endpoint"
    if curl -s --max-time 5 "$minio_endpoint" > /dev/null; then
        echo "✓ MinIO is accessible"
    else
        echo "⚠ MinIO may not be accessible (ensure bucket is created before starting TiKV)"
    fi
}

# Function to create S3 bucket in MinIO
create_s3_bucket() {
    echo "=== Setting up S3 Bucket ==="

    local MINIO_HOST="${CLUSTER_NAME}-minio"
    local BUCKET_NAME
    BUCKET_NAME="$(effective_bucket_name)"
    local CURRENT_USER=$(whoami)

    echo "Creating S3 bucket '$BUCKET_NAME' on $MINIO_HOST..."

    # Check if MinIO is accessible
    if ! ssh -o StrictHostKeyChecking=no "$CURRENT_USER@$MINIO_HOST" "curl -s --max-time 5 http://localhost:9000 > /dev/null 2>&1"; then
        echo "Error: MinIO is not running or not reachable on $MINIO_HOST"
        return 1
    fi

    # Install mc and create bucket
    ssh -o StrictHostKeyChecking=no "$CURRENT_USER@$MINIO_HOST" "
        # Install MinIO client (mc) if not present
        if ! command -v mc &> /dev/null; then
            echo 'Installing MinIO client (mc)...'
            sudo wget -q https://dl.min.io/client/mc/release/linux-amd64/mc -O /usr/local/bin/mc
            sudo chmod +x /usr/local/bin/mc
        fi

        # Configure MinIO connection
        mc alias set myminio http://localhost:9000 minioadmin minioadmin &> /dev/null

        # Create bucket if it doesn't exist
        if mc ls myminio/$BUCKET_NAME &> /dev/null; then
            echo '✓ Bucket $BUCKET_NAME already exists'
        else
            mc mb myminio/$BUCKET_NAME
            if [ \$? -eq 0 ]; then
                echo '✓ Bucket $BUCKET_NAME created successfully'
            else
                echo '✗ Failed to create bucket $BUCKET_NAME'
                return 1
            fi
        fi

        # Verify bucket exists
        mc ls myminio/ | grep $BUCKET_NAME
    " || {
        echo "Error: Failed to create or verify S3 bucket '$BUCKET_NAME'"
        return 1
    }

    echo "✓ S3 bucket setup completed"
    return 0
}

# Function to extract tikv-worker binary from TiKV patch tarball
extract_tikv_worker() {
    echo "=== Extracting TiKV Worker Binary ==="

    if [[ ! -f "$TIKV_PATCH" ]]; then
        echo "Error: TiKV patch tarball not found: $TIKV_PATCH"
        return 1
    fi

    local extract_dir="/tmp/tikv-worker-extract-$$"
    mkdir -p "$extract_dir"

    echo "Extracting tikv-worker from TiKV patch tarball..."
    tar -xzf "$TIKV_PATCH" -C "$extract_dir"

    # Find tikv-worker binary in extracted directory
    local tikv_worker_bin=$(find "$extract_dir" -name "tikv-worker" -type f | head -1)

    if [[ -z "$tikv_worker_bin" ]]; then
        echo "Error: tikv-worker binary not found in $TIKV_PATCH"
        rm -rf "$extract_dir"
        return 1
    fi

    echo "✓ Found tikv-worker at: $tikv_worker_bin"
    cp "$tikv_worker_bin" /tmp/tikv-worker
    chmod +x /tmp/tikv-worker
    rm -rf "$extract_dir"

    echo "✓ TiKV Worker binary extracted to /tmp/tikv-worker"
    return 0
}

# Function to deploy and start TiKV Worker
deploy_tikv_worker() {
    echo "=== Deploying TiKV Worker ==="

    local TIKV_WORKER_HOST="${CLUSTER_NAME}-tikv-worker"
    local MINIO_HOST="${CLUSTER_NAME}-minio"
    local PD_ENDPOINTS
    PD_ENDPOINTS="$(pd_endpoints)"

    # Detect the current user
    local CURRENT_USER=$(whoami)
    echo "Deploying as user: $CURRENT_USER"

    # Create tidb user on TiKV Worker VM if it doesn't exist
    echo "Setting up tidb user on $TIKV_WORKER_HOST..."
    ssh -o StrictHostKeyChecking=no "$CURRENT_USER@$TIKV_WORKER_HOST" "
        if ! id tidb &>/dev/null; then
            echo 'Creating tidb user...'
            sudo useradd -m -s /bin/bash tidb
            sudo mkdir -p /home/tidb/.ssh
            sudo cp ~/.ssh/authorized_keys /home/tidb/.ssh/
            sudo chown -R tidb:tidb /home/tidb/.ssh
            sudo chmod 700 /home/tidb/.ssh
            sudo chmod 600 /home/tidb/.ssh/authorized_keys
        fi
        sudo mkdir -p /data/tikv-worker/{bin,conf,logs,schemas}
        sudo chown -R tidb:tidb /data/tikv-worker
    "

    # Copy tikv-worker binary
    echo "Copying tikv-worker binary to $TIKV_WORKER_HOST..."
    scp -o StrictHostKeyChecking=no /tmp/tikv-worker "tidb@$TIKV_WORKER_HOST:/data/tikv-worker/bin/"

    # Compute bucket name before heredoc to avoid shell-code pollution in TOML
    local BUCKET_NAME
    BUCKET_NAME="$(effective_bucket_name)"

    # Generate TiKV Worker config
    echo "Generating TiKV Worker configuration..."
    cat > /tmp/tikv_worker.toml << TIKV_WORKER_CONFIG
[dfs]
prefix = "tikv"
s3-endpoint = "http://$MINIO_HOST:9000"
s3-key-id = "minioadmin"
s3-secret-key = "minioadmin"
s3-bucket = "$BUCKET_NAME"
s3-region = "local"

[raft-engine]
enabled = false

[schema-manager]
dir = "/data/tikv-worker/schemas"
schema-refresh-threshold = 1
enabled = true
keyspace-refresh-interval = "10s"
TIKV_WORKER_CONFIG

    # Copy config to remote host
    scp -o StrictHostKeyChecking=no /tmp/tikv_worker.toml "tidb@$TIKV_WORKER_HOST:/data/tikv-worker/conf/"

    # Use standalone start_tikv_worker.sh if available, otherwise generate inline
    local START_SCRIPT_LOCAL="${SCRIPT_DIR}/start_tikv_worker.sh"
    if [[ -f "$START_SCRIPT_LOCAL" ]]; then
        echo "Using standalone start_tikv_worker.sh..."
        scp -o StrictHostKeyChecking=no "$START_SCRIPT_LOCAL" "tidb@$TIKV_WORKER_HOST:/data/tikv-worker/start_tikv_worker.sh"
        ssh -o StrictHostKeyChecking=no "tidb@$TIKV_WORKER_HOST" "chmod +x /data/tikv-worker/start_tikv_worker.sh"
    else
        echo "Generating inline start script (fallback)..."
        cat > /tmp/start_tikv_worker.sh << 'START_SCRIPT'
#!/bin/bash
cd /data/tikv-worker
nohup ./bin/tikv-worker \
  --addr=0.0.0.0:19000 \
  --pd-endpoints=PD_ENDPOINTS_PLACEHOLDER \
  --config=conf/tikv_worker.toml \
  --log-file=logs/tikv_worker.log \
  > logs/stdout.log 2>&1 &
echo $! > tikv_worker.pid
echo "TiKV Worker started with PID: $(cat tikv_worker.pid)"
START_SCRIPT
        sed -i "s|PD_ENDPOINTS_PLACEHOLDER|$PD_ENDPOINTS|g" /tmp/start_tikv_worker.sh
        chmod +x /tmp/start_tikv_worker.sh
        scp -o StrictHostKeyChecking=no /tmp/start_tikv_worker.sh "tidb@$TIKV_WORKER_HOST:/data/tikv-worker/"
    fi

    # Start TiKV Worker
    echo "Starting TiKV Worker on $TIKV_WORKER_HOST..."
    ssh -o StrictHostKeyChecking=no "tidb@$TIKV_WORKER_HOST" "/data/tikv-worker/start_tikv_worker.sh $PD_ENDPOINTS"

    # Wait for TiKV Worker to be ready
    echo "Waiting for TiKV Worker to be ready..."
    local retries=30
    local wait_time=2
    for ((i=1; i<=retries; i++)); do
        if ssh -o StrictHostKeyChecking=no "tidb@$TIKV_WORKER_HOST" "curl -s http://localhost:19000/metrics > /dev/null 2>&1"; then
            echo "✓ TiKV Worker is ready"
            return 0
        fi
        echo "Waiting for TiKV Worker... ($i/$retries)"
        sleep $wait_time
    done

    echo "⚠ TiKV Worker readiness check timed out"
    return 1
}

# Function to configure global-sort on User TiDB keyspace
configure_global_sort() {
    echo "=== Configuring Global Sort ==="

    local USER_TIDB_HOST="${CLUSTER_NAME}-tidb-0"
    local MINIO_HOST="${CLUSTER_NAME}-minio"

    # URL-encode the endpoint: http://HOST:9000 -> http%3a%2f%2fHOST%3a9000
    local ENCODED_ENDPOINT="http%3a%2f%2f${MINIO_HOST}%3a9000"
    local MINIO_ACCESS="minioadmin"
    local MINIO_SECRET="minioadmin"
    local MINIO_BUCKET_NAME
    MINIO_BUCKET_NAME="$(effective_bucket_name)"
    local CLOUD_STORAGE_URI="s3://${MINIO_BUCKET_NAME}/gsort-tmp?access-key=${MINIO_ACCESS}&secret-access-key=${MINIO_SECRET}&endpoint=${ENCODED_ENDPOINT}"

    echo "Setting tidb_cloud_storage_uri on User TiDB ($USER_TIDB_HOST:4000)..."
    # Mask secret in logs
    local MASKED_URI
    MASKED_URI=$(echo "$CLOUD_STORAGE_URI" | sed 's/secret-access-key=[^&]*/secret-access-key=xxxxxx/g')
    echo "URI: $MASKED_URI"

    # Wait for User TiDB to be fully ready
    local retries=10
    for ((i=1; i<=retries; i++)); do
        if mysql -h "$USER_TIDB_HOST" -P 4000 -u root -e "SELECT 1" &>/dev/null; then
            break
        fi
        echo "Waiting for User TiDB to be ready... ($i/$retries)"
        sleep 2
    done

    # Set the global variable
    mysql -h "$USER_TIDB_HOST" -P 4000 -u root -e "SET GLOBAL tidb_cloud_storage_uri='${CLOUD_STORAGE_URI}';" && {
        echo "✓ Global-sort configured successfully"
        # Verify the setting (mask secret before printing)
        local verified_uri
        verified_uri=$(mysql -h "$USER_TIDB_HOST" -P 4000 -u root -N -s -e "SELECT @@tidb_cloud_storage_uri;" 2>/dev/null | sed 's/secret-access-key=[^&]*/secret-access-key=xxxxxx/g') || true
        if [[ -n "$verified_uri" ]]; then
            echo "  Verified: $verified_uri"
        fi
    } || {
        echo "⚠ Warning: Failed to configure global-sort (you can configure it manually later)"
    }
}

# Function to start cluster with correct TiDB-X component order
start_cluster_tidb_x() {
    echo "=== Starting TiDB-X Cluster (Correct Component Order) ==="

    local SYSTEM_TIDB_HOST="${CLUSTER_NAME}-tidb-system"
    local USER_TIDB_HOST="${CLUSTER_NAME}-tidb-0"

    # Step 1: Start PD
    echo "Step 1: Starting PD..."
    tiup cluster start "$CLUSTER_NAME" -R pd
    echo "Waiting for PD to be ready..."
    sleep 5

    # Step 1.5: Create S3 bucket (before TiKV starts)
    echo "Step 1.5: Creating S3 bucket..."
    create_s3_bucket

    # Step 2: Start TiKV
    echo "Step 2: Starting TiKV..."
    tiup cluster start "$CLUSTER_NAME" -R tikv
    echo "Waiting for TiKV to be ready..."
    sleep 10

    # Step 3: Start TiKV Worker (manual deployment)
    echo "Step 3: Starting TiKV Worker..."
    deploy_tikv_worker

    # Step 4: Start System TiDB FIRST
    echo "Step 4: Starting System TiDB (port 3000)..."
    tiup cluster start "$CLUSTER_NAME" -R tidb -N "$SYSTEM_TIDB_HOST:3000"

    # CRITICAL: Wait 15 seconds for System TiDB initialization
    echo "⏱  Waiting 15 seconds for System TiDB initialization (CRITICAL)..."
    sleep 15

    # Step 5: Start User TiDB
    echo "Step 5: Starting User TiDB (port 4000)..."
    tiup cluster start "$CLUSTER_NAME" -R tidb -N "$USER_TIDB_HOST:4000"

    # Step 6: Start monitoring
    echo "Step 6: Starting monitoring and Grafana..."
    tiup cluster start "$CLUSTER_NAME" -R prometheus,grafana || true

    # Step 7: Configure global-sort for User TiDB keyspace
    echo "Step 7: Configuring global-sort for User TiDB keyspace..."
    configure_global_sort

    verify_post_deploy_acceptance
    echo "✓ TiDB-X cluster started successfully"
}

# Script directory (for finding companion files like start_tikv_worker.sh)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Default values
CLUSTER_NAME=""
VERSION="v8.5.4"
CONFIG_FILE="/tmp/nextgen-cluster.yaml"
PD_PATCH="/tmp/pd.tar.gz"
TIDB_PATCH="/tmp/tidb.tar.gz"
TIKV_PATCH="/tmp/tikv.tar.gz"

SKIP_PATCHES=false
SKIP_START=false
FORCE=false
SMALL_REGION=false
STRESS_REMOTE_COP=false

# Parse command-line arguments
while [[ "$#" -gt 0 ]]; do
    case $1 in
        -n|--name)
            if [[ $# -lt 2 || "$2" =~ ^- ]]; then echo "Error: --name requires a value"; usage; fi
            CLUSTER_NAME="$2"
            shift 2
            ;;
        -v|--version)
            if [[ $# -lt 2 || "$2" =~ ^- ]]; then echo "Error: --version requires a value"; usage; fi
            VERSION="$2"
            shift 2
            ;;
        -p|--pd-patch)
            if [[ $# -lt 2 || "$2" =~ ^- ]]; then echo "Error: --pd-patch requires a value"; usage; fi
            PD_PATCH="$2"
            shift 2
            ;;
        -d|--tidb-patch)
            if [[ $# -lt 2 || "$2" =~ ^- ]]; then echo "Error: --tidb-patch requires a value"; usage; fi
            TIDB_PATCH="$2"
            shift 2
            ;;
        -k|--tikv-patch)
            if [[ $# -lt 2 || "$2" =~ ^- ]]; then echo "Error: --tikv-patch requires a value"; usage; fi
            TIKV_PATCH="$2"
            shift 2
            ;;

        --small-region)
            SMALL_REGION=true
            shift
            ;;
        --stress-remote-cop)
            STRESS_REMOTE_COP=true
            shift
            ;;
        --skip-patches)
            SKIP_PATCHES=true
            shift
            ;;
        --skip-start)
            SKIP_START=true
            shift
            ;;
        --force)
            FORCE=true
            shift
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown parameter passed: $1"
            usage
            ;;
    esac
done

validate_cluster_name "$CLUSTER_NAME" || exit 1

echo "========================================"
echo "TiDB-X Cluster Deployment Script"
echo "========================================"
echo "Cluster Name:       $CLUSTER_NAME"
echo "TiDB Version:       $VERSION"
echo "Config File:        $CONFIG_FILE"
echo "PD Patch:           $PD_PATCH"
echo "TiDB Patch:         $TIDB_PATCH"
echo "TiKV Patch:         $TIKV_PATCH"

echo "Small Region:       $SMALL_REGION"
if [[ "$STRESS_REMOTE_COP" == true ]]; then
    echo "Remote Cop Mode:    stress"
    echo "Remote Cop MinBlk:  1"
    echo "Remote Cop Ranges:  1"
else
    echo "Remote Cop Mode:    normal"
    echo "Remote Cop MinBlk:  default"
    echo "Remote Cop Ranges:  default"
fi
echo "Skip Patches:       $SKIP_PATCHES"
echo "Skip Start:         $SKIP_START"
echo "========================================"

# Generate cluster configuration (this replaces the need for a static config file)
generate_nextgen_config

# Validate patch files exist (if not skipping patches)
if [[ "$SKIP_PATCHES" == false ]]; then
    echo "=== Checking Patch Files ==="
    check_file_exists "$PD_PATCH" "PD tarball" || exit 1
    check_file_exists "$TIDB_PATCH" "TiDB tarball" || exit 1
    check_file_exists "$TIKV_PATCH" "TiKV tarball" || exit 1

    echo "✓ All patch files found"
fi

# Check if cluster already exists
if tiup cluster list | grep -qw "$CLUSTER_NAME"; then
    echo "Warning: Cluster '$CLUSTER_NAME' already exists"
    if [[ "$FORCE" == true ]]; then
        echo "Force mode: destroying existing cluster..."
        tiup cluster destroy "$CLUSTER_NAME" --yes || true
        sleep 5
    else
        echo "Error: Cluster already exists. Use --force to destroy and recreate, or destroy manually."
        exit 1
    fi
fi

# Step 1: Deploy the cluster
echo ""
echo "=== Step 1: Deploying TiDB Cluster ==="
echo "Command: tiup cluster deploy $CLUSTER_NAME $VERSION $CONFIG_FILE --ignore-config-check --yes"
tiup cluster deploy "$CLUSTER_NAME" "$VERSION" "$CONFIG_FILE" --ignore-config-check --yes

# Step 2: Patch binaries (if not skipping)
if [[ "$SKIP_PATCHES" == false ]]; then
    echo ""
    echo "=== Step 2: Patching Binaries ==="

    echo "Patching PD..."
    echo "Command: tiup cluster patch $CLUSTER_NAME $PD_PATCH -R pd --offline -y"
    tiup cluster patch "$CLUSTER_NAME" "$PD_PATCH" -R pd --offline -y

    echo "Patching TiDB..."
    echo "Command: tiup cluster patch $CLUSTER_NAME $TIDB_PATCH -R tidb --offline -y"
    tiup cluster patch "$CLUSTER_NAME" "$TIDB_PATCH" -R tidb --offline -y

    echo "Patching TiKV..."
    echo "Command: tiup cluster patch $CLUSTER_NAME $TIKV_PATCH -R tikv --offline -y"
    tiup cluster patch "$CLUSTER_NAME" "$TIKV_PATCH" -R tikv --offline -y

    echo "✓ All binaries patched successfully"

    # Extract TiKV Worker from the TiKV patch tarball (produced by fetch script from tikv image)
    echo ""
    echo "Extracting TiKV Worker binary from TiKV patch..."
    extract_tikv_worker || {
        if [[ ! -f /tmp/tikv-worker ]]; then
            echo "Error: tikv-worker extraction failed and /tmp/tikv-worker does not exist."
            echo "  Re-run without --skip-patches, or manually place /tmp/tikv-worker."
            exit 1
        fi
        echo "⚠ Warning: tikv-worker extraction failed, but /tmp/tikv-worker already exists. Continuing..."
    }
else
    echo ""
    echo "=== Step 2: Skipping Binary Patching ==="
    # If tikv-worker binary is missing, try extracting from TIKV_PATCH so start can succeed
    if [[ ! -f /tmp/tikv-worker ]]; then
        check_file_exists "$TIKV_PATCH" "TiKV tarball for tikv-worker extraction" || exit 1
        echo "No /tmp/tikv-worker found; extracting from TiKV patch..."
        extract_tikv_worker || {
            echo "Error: Cannot extract tikv-worker from $TIKV_PATCH and /tmp/tikv-worker is missing."
            echo "  Re-run without --skip-patches, or manually place /tmp/tikv-worker."
            exit 1
        }
    fi
fi

# Step 3: Start the cluster (if not skipping)
if [[ "$SKIP_START" == false ]]; then
    echo ""
    echo "=== Step 3: Starting TiDB-X Cluster ==="

    # Use TiDB-X specific startup sequence (PD -> TiKV -> TiKV Worker -> System TiDB [15s] -> User TiDB)
    start_cluster_tidb_x

    echo ""
    echo "=== Cluster Status ==="
    tiup cluster display "$CLUSTER_NAME"

    # Check MinIO status (optional)
    check_minio_status

    echo ""
    echo "🎉 TiDB-X cluster '$CLUSTER_NAME' deployed and started successfully!"
    echo ""
    echo "Cluster Architecture:"
    echo "  - PD:             ${CLUSTER_NAME}-pd-{0,1,2}:2379"
    echo "  - TiKV:           ${CLUSTER_NAME}-tikv-{0,1,2}:20160"
    echo "  - TiKV Worker:    ${CLUSTER_NAME}-tikv-worker:19000"
    echo "  - System TiDB:    ${CLUSTER_NAME}-tidb-system:3000 (keyspace=SYSTEM)"
    echo "  - User TiDB:      ${CLUSTER_NAME}-tidb-0:4000 (keyspace=keyspace1)"
    echo "  - MinIO:          ${CLUSTER_NAME}-minio:9000"
    echo ""
    echo "Next steps:"
    echo "1. Verify cluster status: tiup cluster display $CLUSTER_NAME"
    echo "2. Connect to User TiDB: mysql -h ${CLUSTER_NAME}-tidb-0 -P 4000 -u root"
    echo "3. Check System TiDB: mysql -h ${CLUSTER_NAME}-tidb-system -P 3000 -u root"
    echo "4. Check MinIO console: http://${CLUSTER_NAME}-minio:9001 (admin/minioadmin)"
    echo "5. Validate tikv-worker endpoints: ps -ef | grep tikv-worker on ${CLUSTER_NAME}-tikv-worker"
    echo "6. Run your workload smoke tests"
    echo ""
    echo "Global-sort is enabled on User TiDB (keyspace1)."
    echo "To verify: mysql -h ${CLUSTER_NAME}-tidb-0 -P 4000 -u root -e \"SELECT @@tidb_cloud_storage_uri;\""
else
    echo ""
    echo "=== Step 3: Skipping Cluster Start ==="
    echo ""
    echo "🎉 TiDB-X cluster '$CLUSTER_NAME' deployed successfully!"
    echo ""
    echo "To start the cluster manually, you must follow TiDB-X component order:"
    echo "  1. Start PD:          tiup cluster start $CLUSTER_NAME -R pd"
    echo "  2. Start TiKV:        tiup cluster start $CLUSTER_NAME -R tikv"
    echo "  3. Deploy TiKV Worker manually on ${CLUSTER_NAME}-tikv-worker"
    echo "  4. Start System TiDB: tiup cluster start $CLUSTER_NAME -R tidb -N ${CLUSTER_NAME}-tidb-system:3000"
    echo "  5. WAIT 15 SECONDS"
    echo "  6. Start User TiDB:   tiup cluster start $CLUSTER_NAME -R tidb -N ${CLUSTER_NAME}-tidb-0:4000"
fi
