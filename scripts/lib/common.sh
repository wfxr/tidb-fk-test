#!/usr/bin/env bash

log_step() {
  printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"
}

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

repo_root() {
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  printf '%s\n' "$script_dir"
}

default_base_dir() {
  printf '%s/scripts/data\n' "$(repo_root)"
}

runtime_root() {
  local base_dir="$1"
  local cluster_name="$2"
  printf '%s/%s\n' "$base_dir" "$cluster_name"
}

port_with_offset() {
  local base_port="$1"
  local offset="$2"
  printf '%s\n' "$((base_port + offset))"
}

tidb_port() {
  local index="$1"
  local offset="$2"
  case "$index" in
    1) port_with_offset 4000 "$offset" ;;
    2) port_with_offset 4001 "$offset" ;;
    3) port_with_offset 4002 "$offset" ;;
    *) die "unsupported TiDB index: $index" ;;
  esac
}

ensure_runtime_layout() {
  local root="$1"
  local index

  mkdir -p "$root/packages" "$root/global/deploy" "$root/global/data"

  for index in 1 2 3; do
    mkdir -p \
      "$root/pd-$index/deploy" \
      "$root/pd-$index/data" \
      "$root/pd-$index/log" \
      "$root/tikv-$index/deploy" \
      "$root/tikv-$index/data" \
      "$root/tikv-$index/log" \
      "$root/tidb-$index/deploy" \
      "$root/tidb-$index/log"
  done

  mkdir -p \
    "$root/monitored/deploy" \
    "$root/monitored/data" \
    "$root/monitored/log" \
    "$root/prometheus/deploy" \
    "$root/prometheus/data" \
    "$root/prometheus/log" \
    "$root/grafana/deploy" \
    "$root/alertmanager/deploy" \
    "$root/alertmanager/data" \
    "$root/alertmanager/log"
}

render_topology() {
  local template="$1"
  local output="$2"
  local root="$3"
  local offset="$4"
  local deploy_user

  deploy_user="$(id -un)"

  sed \
    -e "s|__DEPLOY_USER__|$deploy_user|g" \
    -e "s|__RUNTIME_ROOT__|$root|g" \
    -e "s|__NODE_EXPORTER_PORT__|$(port_with_offset 9100 "$offset")|g" \
    -e "s|__BLACKBOX_EXPORTER_PORT__|$(port_with_offset 9115 "$offset")|g" \
    -e "s|__PD_CLIENT_1__|$(port_with_offset 2379 "$offset")|g" \
    -e "s|__PD_PEER_1__|$(port_with_offset 2380 "$offset")|g" \
    -e "s|__PD_CLIENT_2__|$(port_with_offset 2381 "$offset")|g" \
    -e "s|__PD_PEER_2__|$(port_with_offset 2382 "$offset")|g" \
    -e "s|__PD_CLIENT_3__|$(port_with_offset 2383 "$offset")|g" \
    -e "s|__PD_PEER_3__|$(port_with_offset 2384 "$offset")|g" \
    -e "s|__TIDB_PORT_1__|$(port_with_offset 4000 "$offset")|g" \
    -e "s|__TIDB_STATUS_1__|$(port_with_offset 10080 "$offset")|g" \
    -e "s|__TIDB_PORT_2__|$(port_with_offset 4001 "$offset")|g" \
    -e "s|__TIDB_STATUS_2__|$(port_with_offset 10081 "$offset")|g" \
    -e "s|__TIDB_PORT_3__|$(port_with_offset 4002 "$offset")|g" \
    -e "s|__TIDB_STATUS_3__|$(port_with_offset 10082 "$offset")|g" \
    -e "s|__TIKV_PORT_1__|$(port_with_offset 20160 "$offset")|g" \
    -e "s|__TIKV_STATUS_1__|$(port_with_offset 20180 "$offset")|g" \
    -e "s|__TIKV_PORT_2__|$(port_with_offset 20161 "$offset")|g" \
    -e "s|__TIKV_STATUS_2__|$(port_with_offset 20181 "$offset")|g" \
    -e "s|__TIKV_PORT_3__|$(port_with_offset 20162 "$offset")|g" \
    -e "s|__TIKV_STATUS_3__|$(port_with_offset 20182 "$offset")|g" \
    -e "s|__PROMETHEUS_PORT__|$(port_with_offset 9090 "$offset")|g" \
    -e "s|__NG_MONITORING_PORT__|$(port_with_offset 12020 "$offset")|g" \
    -e "s|__GRAFANA_PORT__|$(port_with_offset 3000 "$offset")|g" \
    -e "s|__ALERTMANAGER_WEB_PORT__|$(port_with_offset 9093 "$offset")|g" \
    -e "s|__ALERTMANAGER_CLUSTER_PORT__|$(port_with_offset 9094 "$offset")|g" \
    "$template" > "$output"
}

cluster_exists() {
  local cluster_name="$1"
  tiup cluster list 2>/dev/null | awk 'NR > 1 {print $1}' | grep -Fxq "$cluster_name"
}

mysql_version() {
  local port="$1"
  local user="${MYSQL_USER:-root}"
  local password="${MYSQL_PASSWORD:-}"
  local -a cmd

  cmd=(mysql --skip-ssl -h 127.0.0.1 -P "$port" -u "$user" -Nse 'select version()')
  if [[ -n "$password" ]]; then
    cmd+=(-p"$password")
  fi

  "${cmd[@]}"
}

run_mysql_statement() {
  local port="$1"
  local statement="$2"
  local user="${MYSQL_USER:-root}"
  local password="${MYSQL_PASSWORD:-}"
  local -a cmd

  cmd=(mysql --skip-ssl -h 127.0.0.1 -P "$port" -u "$user" -Nse "$statement")
  if [[ -n "$password" ]]; then
    cmd+=(-p"$password")
  fi

  "${cmd[@]}"
}

assert_tidb_sql_version() {
  local port="$1"
  local expected="$2"
  local observed

  observed="$(mysql_version "$port")" || die "SQL probe failed on TiDB port $port"
  if [[ "$observed" != *"$expected"* ]]; then
    die "unexpected TiDB SQL version on port $port: expected substring '$expected', got '$observed'"
  fi

  log_step "TiDB port $port reports version: $observed"
}

assert_all_tidb_sql_versions() {
  local offset="$1"
  local expected="$2"
  local index

  for index in 1 2 3; do
    assert_tidb_sql_version "$(tidb_port "$index" "$offset")" "$expected"
  done
}

ensure_database_exists() {
  local port="$1"
  local database_name="$2"

  [[ "$database_name" =~ ^[A-Za-z0-9_]+$ ]] || die "unsupported database name: $database_name"
  run_mysql_statement "$port" "create database if not exists \`$database_name\`"
  log_step "Ensured database '$database_name' exists on TiDB port $port"
}

assert_display_output_healthy() {
  local scope="$1"
  local output="$2"

  if printf '%s\n' "$output" | grep -Eq '\b(Down|Offline|ERR|N/A|Tombstone)\b'; then
    echo "$output" >&2
    die "unhealthy TiUP display output detected for $scope"
  fi
}

assert_cluster_display_healthy() {
  local cluster_name="$1"
  local output

  output="$(tiup cluster display "$cluster_name" --versions)"
  printf '%s\n' "$output"
  assert_display_output_healthy "cluster $cluster_name" "$output"
}

assert_role_version() {
  local cluster_name="$1"
  local role="$2"
  local expected="$3"
  local output

  output="$(tiup cluster display "$cluster_name" -R "$role" --versions)"
  printf '%s\n' "$output"
  assert_display_output_healthy "role $role in cluster $cluster_name" "$output"

  if [[ "$output" != *"$expected"* ]]; then
    die "role $role in cluster $cluster_name does not show expected version substring '$expected'"
  fi
}

assert_role_patched() {
  local cluster_name="$1"
  local role="$2"
  local output

  output="$(tiup cluster display "$cluster_name" -R "$role" --versions)"
  printf '%s\n' "$output"
  assert_display_output_healthy "role $role in cluster $cluster_name" "$output"

  if [[ "$output" != *"$role (patched)"* ]]; then
    die "role $role in cluster $cluster_name is not marked as patched in TiUP display output"
  fi
}

component_binary_name() {
  local role="$1"
  case "$role" in
    pd) printf '%s\n' "pd-server" ;;
    tikv) printf '%s\n' "tikv-server" ;;
    tidb) printf '%s\n' "tidb-server" ;;
    *) die "unsupported component role: $role" ;;
  esac
}

component_source_dir() {
  local role="$1"
  local version="$2"
  printf '%s/.tiup/components/%s/%s\n' "$HOME" "$role" "$version"
}

ensure_patch_package() {
  local runtime_dir="$1"
  local role="$2"
  local version="$3"
  local source_dir
  local binary_name
  local package_path

  source_dir="$(component_source_dir "$role" "$version")"
  binary_name="$(component_binary_name "$role")"
  package_path="$runtime_dir/packages/${role}-${version}.tar.gz"

  [[ -d "$source_dir" ]] || die "missing TiUP component directory: $source_dir"
  [[ -f "$source_dir/$binary_name" ]] || die "missing TiUP component binary: $source_dir/$binary_name"

  if [[ ! -f "$package_path" ]]; then
    log_step "Packaging $binary_name from $source_dir into $package_path" >&2
    tar -C "$source_dir" -czf "$package_path" "$binary_name"
  fi

  printf '%s\n' "$package_path"
}
