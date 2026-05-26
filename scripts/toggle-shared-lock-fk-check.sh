#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/toggle-shared-lock-fk-check.sh CLUSTER_NAME (--enable | --disable) [--restart] [--base-dir DIR] [--port-offset N]

Toggle tidb_foreign_key_check_in_shared_lock globally. The script executes the
SET GLOBAL statement through the first TiDB node. If --restart is supplied, it
uses TiUP reload on the TiDB role so new sessions pick up the global setting.
EOF
}

cluster_name=""
base_dir="$(default_base_dir)"
port_offset="0"
desired_state=""
restart_tidb="false"

if [[ $# -eq 0 ]]; then
  usage
  exit 1
fi

cluster_name="$1"
shift

while [[ $# -gt 0 ]]; do
  case "$1" in
    --enable)
      [[ -z "$desired_state" ]] || die "choose only one of --enable or --disable"
      desired_state="enabled"
      shift
      ;;
    --disable)
      [[ -z "$desired_state" ]] || die "choose only one of --enable or --disable"
      desired_state="disabled"
      shift
      ;;
    --restart)
      restart_tidb="true"
      shift
      ;;
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

[[ "$port_offset" =~ ^-?[0-9]+$ ]] || die "--port-offset must be an integer"
[[ -n "$cluster_name" ]] || die "cluster name positional argument is required"
[[ -n "$desired_state" ]] || die "one of --enable or --disable is required"

cluster_root="$(runtime_root "$base_dir" "$cluster_name")"
first_tidb_port="$(tidb_port 1 "$port_offset")"

log_step "Checking cluster health before toggling shared-lock FK check"
assert_cluster_display_healthy "$cluster_name"

case "$desired_state" in
  enabled)
    log_step "Enabling tidb_foreign_key_check_in_shared_lock through TiDB port $first_tidb_port"
    run_mysql_statement "$first_tidb_port" \
      "set global tidb_foreign_key_check_in_shared_lock = 1" \
      >/dev/null
    ;;
  disabled)
    log_step "Disabling tidb_foreign_key_check_in_shared_lock through TiDB port $first_tidb_port"
    run_mysql_statement "$first_tidb_port" \
      "set global tidb_foreign_key_check_in_shared_lock = 0" \
      >/dev/null
    ;;
esac

assert_shared_lock_fk_check_value "$first_tidb_port" "$desired_state"

if [[ "$restart_tidb" == "true" ]]; then
  log_step "Reloading the TiDB role so new sessions pick up the global setting"
  printf 'y\n' | tiup cluster reload "$cluster_name" -R tidb -y
  assert_cluster_display_healthy "$cluster_name"

  for index in 1 2 3; do
    assert_shared_lock_fk_check_value "$(tidb_port "$index" "$port_offset")" "$desired_state"
  done
fi

log_step "Shared-lock foreign-key check is now $desired_state for cluster '$cluster_name'"
