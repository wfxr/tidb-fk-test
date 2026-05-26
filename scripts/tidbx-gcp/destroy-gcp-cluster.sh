#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_DIR="${SCRIPT_DIR}/state"

usage() {
  cat <<'EOF'
Usage: scripts/tidbx-gcp/destroy-gcp-cluster.sh [<cluster-prefix>] [--yes]

Delete the full GCP Deployment Manager stack for a TiDB-X test cluster.

Examples:
  scripts/tidbx-gcp/destroy-gcp-cluster.sh alice-05261700-ab12
  scripts/tidbx-gcp/destroy-gcp-cluster.sh alice-05261700-ab12 --yes
  scripts/tidbx-gcp/destroy-gcp-cluster.sh --yes
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

list_state_files() {
  find "$STATE_DIR" -maxdepth 1 -type f -name '*.env' 2>/dev/null | sort
}

resolve_prefix_from_state() {
  mapfile -t state_files < <(list_state_files)

  if [[ ${#state_files[@]} -eq 0 ]]; then
    echo "Error: no cluster prefix provided and no local state files found under $STATE_DIR" >&2
    exit 1
  fi

  if [[ ${#state_files[@]} -gt 1 ]]; then
    echo "Error: multiple local cluster state files found. Re-run with an explicit prefix:" >&2
    local state_file prefix
    for state_file in "${state_files[@]}"; do
      prefix=$(basename "$state_file" .env)
      echo "  - $prefix    ($state_file)" >&2
    done
    exit 1
  fi

  basename "${state_files[0]}" .env
}

AUTO_YES=false
PREFIX=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes)
      AUTO_YES=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      if [[ -n "$PREFIX" ]]; then
        echo "Error: unknown argument: $1" >&2
        usage
        exit 1
      fi
      PREFIX="$1"
      shift
      ;;
  esac
done

if [[ -z "$PREFIX" ]]; then
  PREFIX="$(resolve_prefix_from_state)"
fi

if ! validate_prefix "$PREFIX"; then
  echo "Error: invalid cluster prefix: $PREFIX" >&2
  exit 1
fi

require_cmd gcloud

DEPLOYMENT_NAME="${PREFIX}-cluster"
STATE_FILE="${STATE_DIR}/${PREFIX}.env"

echo "========================================"
echo "TiDB-X GCP Teardown"
echo "========================================"
echo "Cluster Prefix:  $PREFIX"
echo "Deployment:      $DEPLOYMENT_NAME"
echo "========================================"

if ! gcloud deployment-manager deployments describe "$DEPLOYMENT_NAME" >/dev/null 2>&1; then
  echo "Error: deployment not found: $DEPLOYMENT_NAME" >&2
  exit 1
fi

if [[ "$AUTO_YES" != true ]]; then
  read -r -p "Delete deployment '$DEPLOYMENT_NAME' and all managed resources? [y/N] " answer
  if [[ ! "$answer" =~ ^[Yy]$ ]]; then
    echo "Aborted."
    exit 1
  fi
fi

echo "Deleting deployment '$DEPLOYMENT_NAME'..."
gcloud deployment-manager deployments delete "$DEPLOYMENT_NAME" --quiet

echo "✓ GCP deployment deleted"

if [[ -f "$STATE_FILE" ]]; then
  rm -f "$STATE_FILE"
  echo "✓ Deleted local state file: $STATE_FILE"
else
  echo "No local state file found to delete: $STATE_FILE"
fi
