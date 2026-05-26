#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_DIR="${SCRIPT_DIR}/state"

PROJECT="gcp-tikv-transaction-dev"
ZONE="us-east5-b"
TIUP_VERSION="v8.5.4"
PD_TAG="v8.5.4-nextgen.202510.6"
TIDB_TAG="v8.5.4-nextgen.202510.6"
TIKV_TAG="v8.5.4-nextgen.202510.26"
KEY_FILE="${HOME}/.ssh/gcp-tikv-transaction-dev.pem"
EXPECTED_INSTANCE_COUNT=11
POLL_INTERVAL_SECS=10
DEPLOY_TIMEOUT_SECS=$((45 * 60))

usage() {
  cat <<EOF
Usage: scripts/tidbx-gcp/create-gcp-cluster.sh [--prefix PREFIX] [--base-name NAME]

Create a full TiDB-X GCP test cluster from zero using repo-owned scripts.

Defaults used by this script:
  project:       ${PROJECT}
  zone:          ${ZONE}
  tiup version:  ${TIUP_VERSION}
  pd tag:        ${PD_TAG}
  tidb tag:      ${TIDB_TAG}
  tikv tag:      ${TIKV_TAG}

Examples:
  scripts/tidbx-gcp/create-gcp-cluster.sh
  scripts/tidbx-gcp/create-gcp-cluster.sh --base-name wenxuan
  scripts/tidbx-gcp/create-gcp-cluster.sh --prefix bench-0526-ab12
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command not found: $1" >&2
    exit 1
  fi
}

validate_prefix() {
  local prefix=$1
  [[ "$prefix" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,18}[a-zA-Z0-9])?$ ]]
}

ensure_key_file() {
  if [[ -f "$KEY_FILE" ]]; then
    chmod 600 "$KEY_FILE"
    return 0
  fi

  echo "Fetching SSH key from Secret Manager..."
  gcloud secrets versions access latest --secret=transaction-team-auth-key > "$KEY_FILE"
  chmod 600 "$KEY_FILE"
}

gcloud_ssh() {
  local host_ip=$1
  shift
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY_FILE" "transaction@${host_ip}" "$@"
}

gcloud_scp() {
  local source=$1
  local target=$2
  scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY_FILE" "$source" "$target"
}

wait_for_instances() {
  local prefix=$1
  local attempts=60
  local running_count
  local total_count

  for ((i = 1; i <= attempts; i++)); do
    total_count=$(gcloud compute instances list \
      --project "$PROJECT" \
      --filter="name~'^${prefix}-'" \
      --zones="$ZONE" \
      --format='value(name)' | wc -l | tr -d ' ')
    running_count=$(gcloud compute instances list \
      --project "$PROJECT" \
      --filter="name~'^${prefix}-' AND status=RUNNING" \
      --zones="$ZONE" \
      --format='value(name)' | wc -l | tr -d ' ')

    if [[ "$total_count" == "$EXPECTED_INSTANCE_COUNT" && "$running_count" == "$EXPECTED_INSTANCE_COUNT" ]]; then
      echo "✓ All ${EXPECTED_INSTANCE_COUNT} instances are RUNNING"
      return 0
    fi

    echo "Waiting for instances... total=${total_count}, running=${running_count}, expected=${EXPECTED_INSTANCE_COUNT} (${i}/${attempts})"
    sleep "$POLL_INTERVAL_SECS"
  done

  echo "Error: instances did not reach RUNNING state in time" >&2
  return 1
}

load_ip() {
  local prefix=$1
  gcloud compute instances describe "${prefix}-load" \
    --project "$PROJECT" \
    --zone "$ZONE" \
    --format='value(networkInterfaces[0].accessConfigs[0].natIP)'
}

wait_for_remote_deploy() {
  local load_ip=$1
  local start_ts now
  start_ts=$(date +%s)

  while true; do
    if gcloud_ssh "$load_ip" "test -f /tmp/deploy.log && grep -q '✓ TiDB-X cluster started successfully' /tmp/deploy.log"; then
      echo "✓ Remote deployment reported success"
      return 0
    fi

    if ! gcloud_ssh "$load_ip" "tmux has-session -t deploy >/dev/null 2>&1"; then
      echo "Remote deploy session exited unexpectedly. Recent log:" >&2
      gcloud_ssh "$load_ip" "tail -n 120 /tmp/deploy.log" >&2 || true
      return 1
    fi

    now=$(date +%s)
    if (( now - start_ts > DEPLOY_TIMEOUT_SECS )); then
      echo "Error: remote deployment timed out after ${DEPLOY_TIMEOUT_SECS}s" >&2
      gcloud_ssh "$load_ip" "tail -n 120 /tmp/deploy.log" >&2 || true
      return 1
    fi

    echo "Deployment still running; waiting ${POLL_INTERVAL_SECS}s..."
    sleep "$POLL_INTERVAL_SECS"
  done
}

wait_for_remote_tiup() {
  local load_ip=$1
  local attempts=60

  for ((i = 1; i <= attempts; i++)); do
    if gcloud_ssh "$load_ip" "test -x ~/.tiup/bin/tiup"; then
      echo "✓ Remote tiup is ready on load node"
      return 0
    fi

    echo "Waiting for remote tiup install... (${i}/${attempts})"
    sleep "$POLL_INTERVAL_SECS"
  done

  echo "Error: remote tiup did not become ready in time" >&2
  return 1
}

write_local_state() {
  local prefix=$1
  local load_ip=${2:-}
  local status=${3:-creating}
  local state_file="${STATE_DIR}/${prefix}.env"

  mkdir -p "$STATE_DIR"
  cat >"$state_file" <<EOF
PREFIX=${prefix}
STATUS=${status}
PROJECT=${PROJECT}
ZONE=${ZONE}
DEPLOYMENT_NAME=${prefix}-cluster
LOAD_IP=${load_ip}
LOAD_SSH_COMMAND=${load_ip:+ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i ${KEY_FILE} transaction@${load_ip}}
TIUP_DISPLAY_COMMAND=tiup cluster display ${prefix}
USER_TIDB_COMMAND=mysql -h ${prefix}-tidb-0 -P 4000 -u root
SYSTEM_TIDB_COMMAND=mysql -h ${prefix}-tidb-system -P 3000 -u root
TOPOLOGY=load=1,pd=3,tikv=3,tidb-system=1,tidb-0=1,tikv-worker=1,minio=1
PD_TAG=${PD_TAG}
TIDB_TAG=${TIDB_TAG}
TIKV_TAG=${TIKV_TAG}
EOF

  echo "✓ Local cluster state written to ${state_file}"
}

PREFIX=""
BASE_NAME="${USER:-tidbx}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix)
      PREFIX="$2"
      shift 2
      ;;
    --base-name)
      BASE_NAME="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Error: unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

require_cmd gcloud
require_cmd ssh
require_cmd scp
require_cmd bash
require_cmd tmux

if [[ -z "$PREFIX" ]]; then
  PREFIX="$(bash "${SCRIPT_DIR}/choose-prefix.sh" "$BASE_NAME")"
fi

if ! validate_prefix "$PREFIX"; then
  echo "Error: invalid prefix: $PREFIX" >&2
  exit 1
fi

ensure_key_file

echo "========================================"
echo "TiDB-X GCP Cluster Create"
echo "========================================"
echo "Prefix:         $PREFIX"
echo "Project:        $PROJECT"
echo "Zone:           $ZONE"
echo "TiUP Version:   $TIUP_VERSION"
echo "PD Tag:         $PD_TAG"
echo "TiDB Tag:       $TIDB_TAG"
echo "TiKV Tag:       $TIKV_TAG"
echo "========================================"

write_local_state "$PREFIX" "" "creating"

RENDERED_CONFIG="/tmp/${PREFIX}-deployment.yaml"
bash "${SCRIPT_DIR}/render-gcp-config.sh" "$PREFIX" > "$RENDERED_CONFIG"

gcloud deployment-manager deployments create "${PREFIX}-cluster" \
  --project "$PROJECT" \
  --config "$RENDERED_CONFIG"

wait_for_instances "$PREFIX"

LOAD_IP="$(load_ip "$PREFIX")"
echo "Load node IP: $LOAD_IP"
write_local_state "$PREFIX" "$LOAD_IP" "deploying"

gcloud_ssh "$LOAD_IP" "echo direct-ssh-ready && whoami && hostname"
wait_for_remote_tiup "$LOAD_IP"

gcloud_ssh "$LOAD_IP" "rm -f /tmp/oras && curl -sL https://github.com/oras-project/oras/releases/download/v1.3.0/oras_1.3.0_linux_amd64.tar.gz | tar -xzf - -C /tmp oras && /tmp/oras version"

gcloud_scp "${SCRIPT_DIR}/fetch-tidb-x-images.sh" "transaction@${LOAD_IP}:~/fetch_tidb_x_images.sh"
gcloud_scp "${SCRIPT_DIR}/deploy-nextgen-cluster.sh" "transaction@${LOAD_IP}:~/deploy-nextgen-cluster.sh"
gcloud_scp "${SCRIPT_DIR}/start-tikv-worker.sh" "transaction@${LOAD_IP}:~/start-tikv-worker.sh"
gcloud_scp "${SCRIPT_DIR}/fix-s3-bucket.sh" "transaction@${LOAD_IP}:~/fix-s3-bucket.sh"

gcloud auth print-access-token | gcloud_ssh "$LOAD_IP" "cat | /tmp/oras login -u oauth2accesstoken --password-stdin us.gcr.io"

gcloud_ssh "$LOAD_IP" "PATH=/tmp:\$PATH bash ~/fetch_tidb_x_images.sh --component pd --tag ${PD_TAG} --output /tmp"
gcloud_ssh "$LOAD_IP" "PATH=/tmp:\$PATH bash ~/fetch_tidb_x_images.sh --component tidb --tag ${TIDB_TAG} --output /tmp"
gcloud_ssh "$LOAD_IP" "PATH=/tmp:\$PATH bash ~/fetch_tidb_x_images.sh --component tikv --tag ${TIKV_TAG} --output /tmp"

gcloud_ssh "$LOAD_IP" "tmux kill-session -t deploy 2>/dev/null || true; tmux new-session -d -s deploy 'bash ~/deploy-nextgen-cluster.sh -n ${PREFIX} -v ${TIUP_VERSION} 2>&1 | tee /tmp/deploy.log'"

wait_for_remote_deploy "$LOAD_IP"
write_local_state "$PREFIX" "$LOAD_IP" "ready"

echo ""
echo "Cluster created successfully."
echo "Prefix: ${PREFIX}"
echo "Load IP: ${LOAD_IP}"
echo "Topology: load=1, pd=3, tikv=3, tidb-system=1, tidb-0=1, tikv-worker=1, minio=1"
echo ""
echo "Load node login:"
echo "  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i ${KEY_FILE} transaction@${LOAD_IP}"
echo ""
echo "On load node, show cluster status:"
echo "  tiup cluster display ${PREFIX}"
echo ""
echo "On load node, connect to User TiDB:"
echo "  mysql -h ${PREFIX}-tidb-0 -P 4000 -u root"
echo ""
echo "Destroy command:"
echo "  bash scripts/tidbx-gcp/destroy-gcp-cluster.sh ${PREFIX} --yes"
