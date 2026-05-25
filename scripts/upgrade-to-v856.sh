#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/upgrade-to-v856.sh CLUSTER_NAME [--base-dir DIR] [--port-offset N]

Roll a local TiUP cluster from v8.5.5 to v8.5.6 with:
  - PD patch first
  - TiKV patch second
  - TiDB patch last
EOF
}

cluster_name=""
base_dir="$(default_base_dir)"
port_offset="0"
target_version="v8.5.6"

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

[[ "$port_offset" =~ ^-?[0-9]+$ ]] || die "--port-offset must be an integer"
[[ -n "$cluster_name" ]] || die "cluster name positional argument is required"

ensure_runtime_layout "$cluster_root"

log_step "Checking cluster health before upgrade"
assert_cluster_display_healthy "$cluster_name"

log_step "Checking current TiDB SQL version before upgrade"
assert_all_tidb_sql_versions "$port_offset" "v8.5.5"

pd_package="$(ensure_patch_package "$cluster_root" pd "$target_version")"
tikv_package="$(ensure_patch_package "$cluster_root" tikv "$target_version")"
tidb_package="$(ensure_patch_package "$cluster_root" tidb "$target_version")"

log_step "Patching PD to $target_version"
printf 'y\n' | tiup cluster patch "$cluster_name" "$pd_package" -R pd -y
assert_role_patched "$cluster_name" pd
assert_all_tidb_sql_versions "$port_offset" "v8.5.5"

log_step "Patching TiKV to $target_version"
printf 'y\n' | tiup cluster patch "$cluster_name" "$tikv_package" -R tikv -y
assert_role_patched "$cluster_name" tikv
assert_all_tidb_sql_versions "$port_offset" "v8.5.5"

log_step "Patching TiDB to $target_version"
printf 'y\n' | tiup cluster patch "$cluster_name" "$tidb_package" -R tidb -y
assert_role_patched "$cluster_name" tidb
assert_all_tidb_sql_versions "$port_offset" "$target_version"

log_step "Upgrade of '$cluster_name' to $target_version completed"
