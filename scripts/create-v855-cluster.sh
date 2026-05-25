#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/create-v855-cluster.sh CLUSTER_NAME [--base-dir DIR] [--port-offset N]

Create a local single-host TiUP cluster at v8.5.5 with:
  - 3 PD
  - 3 TiKV
  - 3 TiDB
EOF
}

cluster_name=""
base_dir="$(default_base_dir)"
port_offset="0"
default_database="test"

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
    --port-offset)
      port_offset="$2"
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
require_cmd mysql
require_cmd tar

cluster_root="$(runtime_root "$base_dir" "$cluster_name")"
template_path="$script_dir/topology-v8.5.5.yaml"
rendered_topology="$cluster_root/rendered-topology.yaml"

[[ "$port_offset" =~ ^-?[0-9]+$ ]] || die "--port-offset must be an integer"
[[ -f "$template_path" ]] || die "missing topology template: $template_path"
[[ -n "$cluster_name" ]] || die "cluster name positional argument is required"

if cluster_exists "$cluster_name"; then
  die "cluster '$cluster_name' already exists; destroy it with 'tiup cluster destroy $cluster_name' or choose another name"
fi

ensure_runtime_layout "$cluster_root"
render_topology "$template_path" "$rendered_topology" "$cluster_root" "$port_offset"

log_step "Deploying cluster '$cluster_name' at v8.5.5"
printf 'y\n' | tiup cluster deploy "$cluster_name" v8.5.5 "$rendered_topology" --yes

log_step "Starting cluster '$cluster_name'"
printf 'y\n' | tiup cluster start "$cluster_name"

log_step "Inspecting cluster '$cluster_name'"
assert_cluster_display_healthy "$cluster_name"

log_step "Ensuring default database '$default_database' exists"
ensure_database_exists "$(tidb_port 1 "$port_offset")" "$default_database"

log_step "Running SQL version checks against TiDB nodes"
assert_all_tidb_sql_versions "$port_offset" "v8.5.5"

log_step "Cluster '$cluster_name' is ready with root/no-password defaults and database '$default_database'"
