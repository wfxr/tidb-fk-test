#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/destroy-cluster.sh CLUSTER_NAME [--base-dir DIR]

Destroy a local single-host TiUP cluster and remove its repo-local runtime
directory, including deploy/data/log/package files.
EOF
}

cluster_name=""
base_dir="$(default_base_dir)"

if [[ $# -eq 0 ]]; then
  usage
  exit 1
fi

cluster_name="$1"
shift

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-dir)
      base_dir="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

require_cmd tiup
require_cmd rm
[[ -n "$cluster_name" ]] || die "cluster name positional argument is required"

cluster_root="$(runtime_root "$base_dir" "$cluster_name")"

if cluster_exists "$cluster_name"; then
  log_step "Destroying TiUP cluster '$cluster_name'"
  printf 'y\n' | tiup cluster destroy "$cluster_name" -y
else
  log_step "TiUP cluster '$cluster_name' does not exist; skipping destroy"
fi

if [[ -d "$cluster_root" ]]; then
  log_step "Removing local runtime directory '$cluster_root'"
  rm -rf "$cluster_root"
else
  log_step "Local runtime directory '$cluster_root' does not exist; skipping removal"
fi

log_step "Cluster '$cluster_name' and its local data directory have been removed"
