#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_DIR="${SCRIPT_DIR}/state"

PD_TAG="v26.3.0-nextgen"
TIDB_TAG="v26.3.0-nextgen"
TIKV_TAG="v26.3.2-nextgen"
PD_VERSION_PATTERN='v26\.3\.0'
TIDB_VERSION_PATTERN='26\.3|202603'
TIKV_GIT_HASH='ff281c2a8711df19f208d713dd5d5a121a12f66a'
KEY_FILE="${HOME}/.ssh/gcp-tikv-transaction-dev.pem"
REMOTE_FETCH_SCRIPT="~/fetch_tidb_x_images.sh"

PREFIX=""
STATE_FILE=""
CURRENT_STAGE="init"
UPGRADE_STARTED=false
UPGRADE_STARTED_AT=""
PROJECT=""
ZONE=""
DEPLOYMENT_NAME=""
LOAD_IP=""
USER_TIDB_COMMAND=""
SYSTEM_TIDB_COMMAND=""
LAST_ERROR_REASON=""

usage() {
  cat <<EOF
Usage: scripts/tidbx-gcp/upgrade-gcp-cluster.sh [--prefix PREFIX]

Prepare to upgrade an existing TiDB-X GCP test cluster to the fixed 202603 target.

Fixed target versions:
  pd tag:        ${PD_TAG}
  tidb tag:      ${TIDB_TAG}
  tikv tag:      ${TIKV_TAG}

Examples:
  scripts/tidbx-gcp/upgrade-gcp-cluster.sh --prefix bench-0526-ab12
  scripts/tidbx-gcp/upgrade-gcp-cluster.sh
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail_step "required command not found: $1"
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
    local state_file resolved_prefix
    for state_file in "${state_files[@]}"; do
      resolved_prefix=$(basename "$state_file" .env)
      echo "  - $resolved_prefix    ($state_file)" >&2
    done
    exit 1
  fi

  basename "${state_files[0]}" .env
}

state_file_for() {
  local prefix=$1
  echo "${STATE_DIR}/${prefix}.env"
}

require_existing_state_file() {
  local file=$1
  if [[ ! -f "$file" ]]; then
    LAST_ERROR_REASON="missing local state file for prefix '${PREFIX}': ${file}"
    echo "Error: ${LAST_ERROR_REASON}" >&2
    echo "Upgrade requires an existing cluster created by create-gcp-cluster.sh." >&2
    return 1
  fi
}

state_value() {
  local key=$1
  local line

  line=$(grep -m1 "^${key}=" "$STATE_FILE" || true)
  printf '%s\n' "${line#*=}"
}

load_cluster_state() {
  PROJECT="$(state_value "PROJECT")"
  ZONE="$(state_value "ZONE")"
  DEPLOYMENT_NAME="$(state_value "DEPLOYMENT_NAME")"
  LOAD_IP="$(state_value "LOAD_IP")"
  USER_TIDB_COMMAND="$(state_value "USER_TIDB_COMMAND")"
  SYSTEM_TIDB_COMMAND="$(state_value "SYSTEM_TIDB_COMMAND")"
}

ensure_key_file() {
  if [[ -f "$KEY_FILE" ]]; then
    chmod 600 "$KEY_FILE"
    return 0
  fi

  echo "Fetching SSH key from Secret Manager..."
  gcloud secrets versions access latest --secret=transaction-team-auth-key >"$KEY_FILE"
  chmod 600 "$KEY_FILE"
}

load_ssh() {
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY_FILE" "transaction@${LOAD_IP}" "$@"
}

load_scp() {
  local source=$1
  local target=$2
  scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY_FILE" "$source" "transaction@${LOAD_IP}:${target}"
}

now_utc() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

upsert_state_field() {
  local file=$1
  local key=$2
  local value=$3
  local tmp_file
  local found=false

  mkdir -p "$STATE_DIR"
  touch "$file"
  tmp_file="$(mktemp)"

  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == "${key}="* ]]; then
      printf '%s=%s\n' "$key" "$value" >>"$tmp_file"
      found=true
    else
      printf '%s\n' "$line" >>"$tmp_file"
    fi
  done <"$file"

  if [[ "$found" == false ]]; then
    printf '%s=%s\n' "$key" "$value" >>"$tmp_file"
  fi

  mv "$tmp_file" "$file"
}

update_upgrade_state() {
  local status=$1
  local stage=$2
  local last_error=$3
  local started_at=$4
  local finished_at=$5

  upsert_state_field "$STATE_FILE" "UPGRADE_STATUS" "$status"
  upsert_state_field "$STATE_FILE" "UPGRADE_STAGE" "$stage"
  upsert_state_field "$STATE_FILE" "UPGRADE_TARGET_PD_TAG" "$PD_TAG"
  upsert_state_field "$STATE_FILE" "UPGRADE_TARGET_TIDB_TAG" "$TIDB_TAG"
  upsert_state_field "$STATE_FILE" "UPGRADE_TARGET_TIKV_TAG" "$TIKV_TAG"
  upsert_state_field "$STATE_FILE" "UPGRADE_LAST_ERROR" "$last_error"
  upsert_state_field "$STATE_FILE" "UPGRADE_STARTED_AT" "$started_at"
  upsert_state_field "$STATE_FILE" "UPGRADE_FINISHED_AT" "$finished_at"
}

mark_upgrade_started() {
  UPGRADE_STARTED=true
  UPGRADE_STARTED_AT="$(now_utc)"
  CURRENT_STAGE="init"
  update_upgrade_state "upgrading" "$CURRENT_STAGE" "" "$UPGRADE_STARTED_AT" ""
}

mark_upgrade_failed() {
  local last_error=${1:-unknown upgrade failure}
  local finished_at
  finished_at="$(now_utc)"
  update_upgrade_state "failed" "${CURRENT_STAGE}" "$last_error" "${UPGRADE_STARTED_AT:-$finished_at}" "$finished_at"
}

mark_upgrade_succeeded() {
  local finished_at
  finished_at="$(now_utc)"
  CURRENT_STAGE="done"
  update_upgrade_state "upgraded" "$CURRENT_STAGE" "" "${UPGRADE_STARTED_AT:-$finished_at}" "$finished_at"
}

fail_step() {
  local message=$1
  echo "Error: ${message}" >&2
  return 1
}

pd_endpoints() {
  local endpoints=()
  local host
  for host in \
    "${PREFIX}-pd-0" \
    "${PREFIX}-pd-1" \
    "${PREFIX}-pd-2"; do
    endpoints+=("http://${host}:2379")
  done

  local IFS=,
  printf '%s' "${endpoints[*]}"
}

remote_user_tidb_command() {
  if [[ -n "$USER_TIDB_COMMAND" ]]; then
    printf '%s' "$USER_TIDB_COMMAND"
  else
    printf 'mysql -h %s-tidb-0 -P 4000 -u root' "$PREFIX"
  fi
}

remote_system_tidb_command() {
  if [[ -n "$SYSTEM_TIDB_COMMAND" ]]; then
    printf '%s' "$SYSTEM_TIDB_COMMAND"
  else
    printf 'mysql -h %s-tidb-system -P 3000 -u root' "$PREFIX"
  fi
}

load_login_command() {
  printf 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i %s transaction@%s' \
    "$KEY_FILE" "${LOAD_IP:-<load-ip-unavailable>}"
}

tiup_display_command() {
  printf '~/.tiup/bin/tiup cluster display %s --versions' "$PREFIX"
}

user_tidb_share_lock_check_command() {
  printf '%s -Nse "SELECT @@GLOBAL.tidb_foreign_key_check_in_shared_lock;"' \
    "$(remote_user_tidb_command)"
}

tiup_audit_log_command() {
  printf 'tail -n 200 ~/.tiup/storage/cluster/clusters/%s/audit.log' "$PREFIX"
}

tikv_worker_process_inspect_command() {
  printf 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i %s transaction@%s '\''ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null tidb@%s-tikv-worker "pgrep -af tikv-worker"'\''' \
    "$KEY_FILE" "${LOAD_IP:-<load-ip-unavailable>}" "$PREFIX"
}

destroy_cluster_command() {
  printf 'bash scripts/tidbx-gcp/destroy-gcp-cluster.sh %s --yes' "$PREFIX"
}

cluster_display_output() {
  load_ssh "~/.tiup/bin/tiup cluster display ${PREFIX} --versions"
}

require_display_output_healthy() {
  local scope=$1
  local display_output=$2

  if printf '%s\n' "$display_output" | grep -Eq '\b(Down|Offline|ERR|N/A|Tombstone)\b'; then
    LAST_ERROR_REASON="unhealthy tiup cluster display output detected for ${scope}"
    printf '%s\n' "$display_output" >&2
    fail_step "${LAST_ERROR_REASON}"
  fi
}

require_display_hosts_present() {
  local role=$1
  local display_output=$2
  shift 2

  for host in "$@"; do
    if ! printf '%s\n' "$display_output" | awk -v role="$role" -v host="$host" '
      $2 == role {
        host_field = ($3 == "(patched)" ? $4 : $3)
        if (host_field == host) {
          found = 1
        }
      }
      END { exit(found ? 0 : 1) }
    '; then
      LAST_ERROR_REASON="missing ${role} host '${host}' in tiup cluster display during stage '${CURRENT_STAGE}'"
      fail_step "${LAST_ERROR_REASON}"
    fi
  done
}

require_display_host_version_contains() {
  local role=$1
  local host=$2
  local version_fragment=$3
  local display_output=$4

  if ! printf '%s\n' "$display_output" | awk -v role="$role" -v host="$host" -v version_fragment="$version_fragment" '
    $2 == role {
      host_field = ($3 == "(patched)" ? $4 : $3)
      if (host_field == host && index($0, version_fragment)) {
        found = 1
      }
    }
    END { exit(found ? 0 : 1) }
  '; then
    LAST_ERROR_REASON="missing expected version fragment '${version_fragment}' for ${role} host '${host}' during stage '${CURRENT_STAGE}'"
    fail_step "${LAST_ERROR_REASON}"
  fi
}

verify_pd_binary_versions() {
  CURRENT_STAGE="verify-pd-binary-version"
  LAST_ERROR_REASON="PD binary version check failed"
  load_ssh "~/.tiup/bin/tiup cluster exec ${PREFIX} -R pd --command 'bash -lc '\''/data/${PREFIX}/deploy/pd-2379/bin/pd-server -V | grep -Eq \"Release Version: ${PD_VERSION_PATTERN}\"'\'''"
}

verify_tikv_binary_versions() {
  CURRENT_STAGE="verify-tikv-binary-version"
  LAST_ERROR_REASON="TiKV binary git hash check failed"
  load_ssh "~/.tiup/bin/tiup cluster exec ${PREFIX} -R tikv --command 'bash -lc '\''/data/${PREFIX}/deploy/tikv-20160/bin/tikv-server -V | grep -Eq \"Git Commit Hash:[[:space:]]+${TIKV_GIT_HASH}\"'\'''"
}

expected_instance_names() {
  cat <<EOF
${PREFIX}-load
${PREFIX}-pd-0
${PREFIX}-pd-1
${PREFIX}-pd-2
${PREFIX}-tikv-0
${PREFIX}-tikv-1
${PREFIX}-tikv-2
${PREFIX}-tidb-system
${PREFIX}-tidb-0
${PREFIX}-tikv-worker
${PREFIX}-minio
EOF
}

require_local_prerequisites() {
  require_cmd bash
  require_cmd gcloud
  require_cmd grep
  require_cmd scp
  require_cmd ssh
}

require_state_field() {
  local key=$1
  local value=$2

  if [[ -z "$value" ]]; then
    LAST_ERROR_REASON="state file ${STATE_FILE} is missing required field: ${key}"
    fail_step "${LAST_ERROR_REASON}"
  fi
}

precheck_deployment() {
  CURRENT_STAGE="precheck-deployment"
  require_state_field "PROJECT" "$PROJECT"
  require_state_field "DEPLOYMENT_NAME" "$DEPLOYMENT_NAME"

  LAST_ERROR_REASON="failed to describe deployment ${DEPLOYMENT_NAME}"
  gcloud deployment-manager deployments describe "$DEPLOYMENT_NAME" \
    --project "$PROJECT" >/dev/null
  echo "✓ Deployment exists: ${DEPLOYMENT_NAME}"
}

precheck_instances() {
  CURRENT_STAGE="precheck-instances"
  require_state_field "ZONE" "$ZONE"

  local row name status
  local -a actual_rows=() unexpected_instances=() missing_instances=() not_running_instances=()
  local -a expected_instances=()
  declare -A expected_lookup=()
  declare -A status_by_name=()

  mapfile -t actual_rows < <(
    gcloud compute instances list \
      --project "$PROJECT" \
      --zones "$ZONE" \
      --filter="name~'^${PREFIX}-'" \
      --format='csv[no-heading](name,status)'
  )

  mapfile -t expected_instances < <(expected_instance_names)
  for name in "${expected_instances[@]}"; do
    expected_lookup["$name"]=1
  done

  LAST_ERROR_REASON="instance set for ${PREFIX} is incomplete or not fully RUNNING"

  if [[ ${#actual_rows[@]} -ne ${#expected_instances[@]} ]]; then
    LAST_ERROR_REASON="expected ${#expected_instances[@]} GCP instances for '${PREFIX}', found ${#actual_rows[@]}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  for row in "${actual_rows[@]}"; do
    IFS=, read -r name status <<<"$row"
    status_by_name["$name"]="$status"
    if [[ -z "${expected_lookup[$name]:-}" ]]; then
      unexpected_instances+=("$name")
    fi
  done

  for name in "${expected_instances[@]}"; do
    if [[ -z "${status_by_name[$name]:-}" ]]; then
      missing_instances+=("$name")
      continue
    fi
    if [[ "${status_by_name[$name]}" != "RUNNING" ]]; then
      not_running_instances+=("${name}=${status_by_name[$name]}")
    fi
  done

  if [[ ${#unexpected_instances[@]} -gt 0 ]]; then
    LAST_ERROR_REASON="unexpected GCP instances found for '${PREFIX}': ${unexpected_instances[*]}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  if [[ ${#missing_instances[@]} -gt 0 ]]; then
    LAST_ERROR_REASON="missing expected GCP instances for '${PREFIX}': ${missing_instances[*]}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  if [[ ${#not_running_instances[@]} -gt 0 ]]; then
    LAST_ERROR_REASON="some expected GCP instances are not RUNNING: ${not_running_instances[*]}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  echo "✓ Expected GCP instance set is present and RUNNING for ${PREFIX}"
}

precheck_load_ssh() {
  CURRENT_STAGE="precheck-load-ssh"
  require_state_field "LOAD_IP" "$LOAD_IP"
  LAST_ERROR_REASON="failed to SSH to load node ${LOAD_IP}"
  load_ssh "echo ssh-ready >/dev/null"
  echo "✓ Load node SSH is ready: ${LOAD_IP}"
}

precheck_tiup_display() {
  CURRENT_STAGE="precheck-tiup-display"
  local display_output row
  local -a actual_rows=() expected_rows=() missing_rows=() unexpected_rows=()
  declare -A expected_lookup=()
  declare -A actual_lookup=()

  LAST_ERROR_REASON="failed to run tiup cluster display for ${PREFIX} on load"
  display_output="$(cluster_display_output)"
  require_display_output_healthy "cluster ${PREFIX}" "$display_output"

  mapfile -t actual_rows < <(printf '%s\n' "$display_output" | awk '
    $2 ~ /^(pd|tikv|tidb)$/ {
      host_field = ($3 == "(patched)" ? $4 : $3)
      print $2 "," host_field
    }
  ')
  mapfile -t expected_rows < <(cat <<EOF
pd,${PREFIX}-pd-0
pd,${PREFIX}-pd-1
pd,${PREFIX}-pd-2
tikv,${PREFIX}-tikv-0
tikv,${PREFIX}-tikv-1
tikv,${PREFIX}-tikv-2
tidb,${PREFIX}-tidb-system
tidb,${PREFIX}-tidb-0
EOF
)

  LAST_ERROR_REASON="tiup cluster display for ${PREFIX} does not match expected managed topology"

  if [[ ${#actual_rows[@]} -ne ${#expected_rows[@]} ]]; then
    fail_step "expected ${#expected_rows[@]} managed pd/tikv/tidb rows in tiup display for '${PREFIX}', found ${#actual_rows[@]}"
  fi

  for row in "${expected_rows[@]}"; do
    expected_lookup["$row"]=1
  done

  for row in "${actual_rows[@]}"; do
    actual_lookup["$row"]=1
    if [[ -z "${expected_lookup[$row]:-}" ]]; then
      unexpected_rows+=("$row")
    fi
  done

  for row in "${expected_rows[@]}"; do
    if [[ -z "${actual_lookup[$row]:-}" ]]; then
      missing_rows+=("$row")
    fi
  done

  if [[ ${#unexpected_rows[@]} -gt 0 ]]; then
    LAST_ERROR_REASON="unexpected managed tiup rows for '${PREFIX}': ${unexpected_rows[*]}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  if [[ ${#missing_rows[@]} -gt 0 ]]; then
    LAST_ERROR_REASON="missing managed tiup rows for '${PREFIX}': ${missing_rows[*]}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  echo "✓ tiup cluster display matches expected managed topology"
}

precheck_user_tidb() {
  CURRENT_STAGE="precheck-user-tidb"
  LAST_ERROR_REASON="failed to connect to User TiDB for ${PREFIX}"
  if [[ -n "$USER_TIDB_COMMAND" ]]; then
    load_ssh "${USER_TIDB_COMMAND} -Nse 'select 1' >/dev/null"
  else
    load_ssh "mysql -h ${PREFIX}-tidb-0 -P 4000 -u root -Nse 'select 1' >/dev/null"
  fi
  echo "✓ User TiDB is reachable"
}

run_prechecks() {
  CURRENT_STAGE="precheck"
  require_existing_state_file "$STATE_FILE"
  require_local_prerequisites
  load_cluster_state
  ensure_key_file
  precheck_deployment
  precheck_instances
  precheck_load_ssh
  precheck_tiup_display
  precheck_user_tidb
}

ensure_remote_oras() {
  CURRENT_STAGE="prepare-binaries-oras"
  LAST_ERROR_REASON="failed to ensure oras on load node ${LOAD_IP}"
  load_ssh "if [ ! -x /tmp/oras ]; then rm -f /tmp/oras && curl -sL https://github.com/oras-project/oras/releases/download/v1.3.0/oras_1.3.0_linux_amd64.tar.gz | tar -xzf - -C /tmp oras; fi; /tmp/oras version >/dev/null"
  echo "✓ oras is ready on load"
}

ensure_remote_jq() {
  CURRENT_STAGE="prepare-binaries-jq"
  LAST_ERROR_REASON="failed to ensure jq on load node ${LOAD_IP}"
  load_ssh "if command -v jq >/dev/null 2>&1; then jq --version >/dev/null; elif command -v apt-get >/dev/null 2>&1 && command -v sudo >/dev/null 2>&1; then sudo apt-get update >/dev/null && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y jq >/dev/null && jq --version >/dev/null; else exit 1; fi"
  echo "✓ jq is ready on load"
}

copy_fetch_script_to_load() {
  local local_fetch_script="${SCRIPT_DIR}/fetch-tidb-x-images.sh"

  if [[ ! -f "$local_fetch_script" ]]; then
    LAST_ERROR_REASON="local fetch script not found: ${local_fetch_script}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  CURRENT_STAGE="prepare-binaries-copy-fetch-script"
  LAST_ERROR_REASON="failed to copy fetch-tidb-x-images.sh to load node ${LOAD_IP}"
  load_scp "$local_fetch_script" "$REMOTE_FETCH_SCRIPT"
  echo "✓ Remote fetch script refreshed on load"
}

login_remote_registry() {
  CURRENT_STAGE="prepare-binaries-registry-login"
  LAST_ERROR_REASON="failed to log in to us.gcr.io on load node ${LOAD_IP}"
  gcloud auth print-access-token | load_ssh "cat | /tmp/oras login -u oauth2accesstoken --password-stdin us.gcr.io >/dev/null"
  echo "✓ Logged in to us.gcr.io on load"
}

fetch_remote_binary() {
  local component=$1
  local tag=$2
  local output_path="/tmp/${component}.tar.gz"

  CURRENT_STAGE="prepare-binaries-fetch-${component}"
  LAST_ERROR_REASON="failed to fetch ${component} binary tag ${tag} on load node ${LOAD_IP}"
  load_ssh "PATH=/tmp:\$PATH bash ${REMOTE_FETCH_SCRIPT} --component ${component} --tag ${tag} --output /tmp"
  LAST_ERROR_REASON="fetched ${component} tag ${tag} but missing expected artifact ${output_path}"
  load_ssh "test -s ${output_path}"
  echo "✓ Remote ${component} binary prepared at ${output_path}"
}

prepare_remote_binaries() {
  CURRENT_STAGE="prepare-binaries"
  ensure_remote_oras
  ensure_remote_jq
  copy_fetch_script_to_load
  login_remote_registry
  fetch_remote_binary "pd" "$PD_TAG"
  fetch_remote_binary "tidb" "$TIDB_TAG"
  fetch_remote_binary "tikv" "$TIKV_TAG"
}

upgrade_pd_family() {
  local display_output

  CURRENT_STAGE="upgrade-pd"
  LAST_ERROR_REASON="failed to patch PD family to ${PD_TAG}"
  load_ssh "~/.tiup/bin/tiup cluster patch ${PREFIX} /tmp/pd.tar.gz -R pd -y"

  CURRENT_STAGE="upgrade-pd-verify"
  LAST_ERROR_REASON="failed to verify PD family after patch"
  display_output="$(cluster_display_output)"
  require_display_output_healthy "PD family for ${PREFIX}" "$display_output"
  require_display_hosts_present "pd" "$display_output" \
    "${PREFIX}-pd-0" \
    "${PREFIX}-pd-1" \
    "${PREFIX}-pd-2"

  echo "✓ PD family upgraded to ${PD_TAG}"
}

upgrade_tikv_worker() {
  local worker_host="${PREFIX}-tikv-worker"
  local worker_start_script="${SCRIPT_DIR}/start-tikv-worker.sh"
  local pd_urls

  if [[ ! -f "$worker_start_script" ]]; then
    LAST_ERROR_REASON="local tikv-worker start script not found: ${worker_start_script}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  pd_urls="$(pd_endpoints)"

  CURRENT_STAGE="upgrade-tikv-worker-extract"
  LAST_ERROR_REASON="failed to extract tikv-worker from /tmp/tikv.tar.gz on load node ${LOAD_IP}"
  load_ssh "bash -s" <<'EOF'
set -euo pipefail
tmpdir="$(mktemp -d /tmp/tikv-worker-upgrade.XXXXXX)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT
tar -xzf /tmp/tikv.tar.gz -C "$tmpdir"
worker_bin="$(find "$tmpdir" -name tikv-worker -type f | head -n 1)"
test -n "$worker_bin"
cp "$worker_bin" /tmp/tikv-worker
chmod +x /tmp/tikv-worker
EOF

  CURRENT_STAGE="upgrade-tikv-worker-copy-start-script"
  LAST_ERROR_REASON="failed to refresh start-tikv-worker.sh on load node ${LOAD_IP}"
  load_scp "$worker_start_script" "~/start-tikv-worker.sh"

  CURRENT_STAGE="upgrade-tikv-worker-stop"
  LAST_ERROR_REASON="failed to stop tikv-worker on ${worker_host}"
  load_ssh "bash -s" <<EOF
set -euo pipefail
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null tidb@${worker_host} <<'INNER'
if [ -f /data/tikv-worker/tikv_worker.pid ]; then
  pid=\$(cat /data/tikv-worker/tikv_worker.pid)
  kill "\$pid" >/dev/null 2>&1 || true
fi
pkill -f tikv-worker >/dev/null 2>&1 || true
rm -f /data/tikv-worker/tikv_worker.pid
INNER
EOF

  CURRENT_STAGE="upgrade-tikv-worker-wait-stop"
  LAST_ERROR_REASON="timed out waiting for old tikv-worker process to exit on ${worker_host}"
  load_ssh "bash -s" <<EOF
set -euo pipefail
for _ in \$(seq 1 30); do
  if ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null tidb@${worker_host} "pgrep -af tikv-worker >/dev/null"; then
    sleep 2
  else
    exit 0
  fi
done
exit 1
EOF

  CURRENT_STAGE="upgrade-tikv-worker-install"
  LAST_ERROR_REASON="failed to install upgraded tikv-worker on ${worker_host}"
  load_ssh "bash -s" <<EOF
set -euo pipefail
scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null /tmp/tikv-worker tidb@${worker_host}:/data/tikv-worker/bin/tikv-worker
scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ~/start-tikv-worker.sh tidb@${worker_host}:/data/tikv-worker/start_tikv_worker.sh
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null tidb@${worker_host} "chmod +x /data/tikv-worker/start_tikv_worker.sh"
EOF

  CURRENT_STAGE="upgrade-tikv-worker-start"
  LAST_ERROR_REASON="failed to start upgraded tikv-worker on ${worker_host}"
  load_ssh "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null tidb@${worker_host} \"/data/tikv-worker/start_tikv_worker.sh ${pd_urls}\""

  CURRENT_STAGE="upgrade-tikv-worker-verify"
  LAST_ERROR_REASON="tikv-worker metrics did not become ready on ${worker_host}"
  load_ssh "bash -s" <<EOF
set -euo pipefail
for _ in \$(seq 1 30); do
  if ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null tidb@${worker_host} "curl -sf http://localhost:19000/metrics >/dev/null"; then
    exit 0
  fi
  sleep 2
done
exit 1
EOF

  LAST_ERROR_REASON="tikv-worker process on ${worker_host} is missing one or more PD endpoints after restart"
  load_ssh "bash -s" <<EOF
set -euo pipefail
process_info="\$(ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null tidb@${worker_host} "pgrep -af tikv-worker")"
grep -q "${PREFIX}-pd-0:2379" <<<"\$process_info"
grep -q "${PREFIX}-pd-1:2379" <<<"\$process_info"
grep -q "${PREFIX}-pd-2:2379" <<<"\$process_info"
EOF

  echo "✓ tikv-worker upgraded to ${TIKV_TAG}"
}

apply_tikv_config_changes() {
  local remote_topology="/tmp/${PREFIX}-tikv-202603-topology.yaml"
  local display_output

  CURRENT_STAGE="upgrade-tikv-config-prepare"
  LAST_ERROR_REASON="failed to prepare TiKV 202603 config changes for ${PREFIX}"
  display_output="$(cluster_display_output)"
  require_display_output_healthy "pre-TiKV-config cluster ${PREFIX}" "$display_output"
  require_display_hosts_present "tikv" "$display_output" \
    "${PREFIX}-tikv-0" \
    "${PREFIX}-tikv-1" \
    "${PREFIX}-tikv-2"

  load_ssh "bash -s" <<EOF
set -euo pipefail
meta_path="\$HOME/.tiup/storage/cluster/clusters/${PREFIX}/meta.yaml"
topology_path="${remote_topology}"
test -s "\$meta_path"
command -v yq >/dev/null 2>&1

awk '
  BEGIN { in_topology = 0 }
  /^topology:$/ { in_topology = 1; next }
  in_topology {
    sub(/^  /, "")
    print
  }
' "\$meta_path" > "\$topology_path"
test -s "\$topology_path"

yq -i '.server_configs.tikv."rfengine.enable-compact-rate-limiter" = true' "\$topology_path"
yq -i '.server_configs.tikv."pd.report-store-size-with-keyspace-name" = true' "\$topology_path"
yq -i '.server_configs.tikv."pd.load-all-keyspaces-on-start" = true' "\$topology_path"
yq -i '.server_configs.tikv."storage.flow-control.enable" = false' "\$topology_path"

grep -q '^server_configs:$' "\$topology_path"
grep -Eq '^[[:space:]]+tikv:$' "\$topology_path"
grep -Eq '^[[:space:]]+rfengine.enable-compact-rate-limiter: true$' "\$topology_path"
grep -Eq '^[[:space:]]+pd.report-store-size-with-keyspace-name: true$' "\$topology_path"
grep -Eq '^[[:space:]]+pd.load-all-keyspaces-on-start: true$' "\$topology_path"
grep -Eq '^[[:space:]]+storage.flow-control.enable: false$' "\$topology_path"
EOF

  CURRENT_STAGE="upgrade-tikv-config-apply"
  LAST_ERROR_REASON="failed to apply TiKV 202603 config changes into TiUP metadata for ${PREFIX}"
  load_ssh "~/.tiup/bin/tiup cluster edit-config ${PREFIX} --topology-file ${remote_topology} -y"

  CURRENT_STAGE="upgrade-tikv-config-verify-meta"
  LAST_ERROR_REASON="TiUP metadata for ${PREFIX} is missing required TiKV 202603 config changes after edit-config"
  load_ssh "grep -Eq '^[[:space:]]+rfengine.enable-compact-rate-limiter: true$' ~/.tiup/storage/cluster/clusters/${PREFIX}/meta.yaml && \
    grep -Eq '^[[:space:]]+pd.report-store-size-with-keyspace-name: true$' ~/.tiup/storage/cluster/clusters/${PREFIX}/meta.yaml && \
    grep -Eq '^[[:space:]]+pd.load-all-keyspaces-on-start: true$' ~/.tiup/storage/cluster/clusters/${PREFIX}/meta.yaml && \
    grep -Eq '^[[:space:]]+storage.flow-control.enable: false$' ~/.tiup/storage/cluster/clusters/${PREFIX}/meta.yaml"

  echo "✓ TiKV 202603 config changes recorded in TiUP metadata"
}

verify_tikv_config_applied() {
  CURRENT_STAGE="upgrade-tikv-config-verify-live"
  LAST_ERROR_REASON="required TiKV 202603 config changes were not rendered into live tikv.toml after patch"

  load_ssh "~/.tiup/bin/tiup cluster exec ${PREFIX} -R tikv --command 'bash -lc '\''set -euo pipefail; config_file=/data/${PREFIX}/deploy/tikv-20160/conf/tikv.toml; test -s \"\$config_file\"; grep -Eq \"^enable-compact-rate-limiter *= *true$\" \"\$config_file\"; awk \"BEGIN { section = \\\"\\\" } /^\\\\[/ { section = \\\$0 } section == \\\"[pd]\\\" && \\\$1 == \\\"report-store-size-with-keyspace-name\\\" && \\\$3 == \\\"true\\\" { pd_report = 1 } section == \\\"[pd]\\\" && \\\$1 == \\\"load-all-keyspaces-on-start\\\" && \\\$3 == \\\"true\\\" { pd_load = 1 } section == \\\"[storage.flow-control]\\\" && \\\$1 == \\\"enable\\\" && \\\$3 == \\\"false\\\" { flow_control = 1 } END { exit(pd_report && pd_load && flow_control ? 0 : 1) }\" \"\$config_file\"'\'''"

  echo "✓ TiKV 202603 config changes are present in live tikv.toml on all TiKV hosts"
}

upgrade_tikv_family() {
  local display_output
  local user_tidb_cmd

  CURRENT_STAGE="upgrade-tikv"
  LAST_ERROR_REASON="failed to patch TiKV family to ${TIKV_TAG}"
  load_ssh "~/.tiup/bin/tiup cluster patch ${PREFIX} /tmp/tikv.tar.gz -R tikv -y"

  CURRENT_STAGE="upgrade-tikv-reload-config"
  LAST_ERROR_REASON="failed to reload TiKV family after applying 202603 config changes"
  load_ssh "~/.tiup/bin/tiup cluster reload ${PREFIX} -R tikv -y"

  CURRENT_STAGE="upgrade-tikv-verify"
  LAST_ERROR_REASON="failed to verify TiKV family after patch"
  display_output="$(cluster_display_output)"
  require_display_output_healthy "TiKV family for ${PREFIX}" "$display_output"
  require_display_hosts_present "tikv" "$display_output" \
    "${PREFIX}-tikv-0" \
    "${PREFIX}-tikv-1" \
    "${PREFIX}-tikv-2"

  user_tidb_cmd="$(remote_user_tidb_command)"
  LAST_ERROR_REASON="User TiDB is unreachable after TiKV upgrade"
  load_ssh "${user_tidb_cmd} -Nse 'select 1' >/dev/null"
  verify_tikv_config_applied

  echo "✓ TiKV family upgraded to ${TIKV_TAG}"
}

upgrade_tidb_family() {
  local display_output
  local user_tidb_cmd system_tidb_cmd
  local user_version system_version

  CURRENT_STAGE="upgrade-tidb"
  LAST_ERROR_REASON="failed to patch TiDB family to ${TIDB_TAG}"
  load_ssh "~/.tiup/bin/tiup cluster patch ${PREFIX} /tmp/tidb.tar.gz -R tidb -y"

  CURRENT_STAGE="upgrade-tidb-verify"
  LAST_ERROR_REASON="failed to verify TiDB family after patch"
  display_output="$(cluster_display_output)"
  require_display_output_healthy "TiDB family for ${PREFIX}" "$display_output"
  require_display_hosts_present "tidb" "$display_output" \
    "${PREFIX}-tidb-system" \
    "${PREFIX}-tidb-0"

  user_tidb_cmd="$(remote_user_tidb_command)"
  system_tidb_cmd="$(remote_system_tidb_command)"

  LAST_ERROR_REASON="failed to query TiDB versions after TiDB patch"
  system_version="$(load_ssh "${system_tidb_cmd} -Nse 'select version()'")"
  user_version="$(load_ssh "${user_tidb_cmd} -Nse 'select version()'")"

  if [[ ! "$system_version" =~ $TIDB_VERSION_PATTERN ]]; then
    LAST_ERROR_REASON="system TiDB did not report a 202603 version after patch: ${system_version}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  if [[ ! "$user_version" =~ $TIDB_VERSION_PATTERN ]]; then
    LAST_ERROR_REASON="user TiDB did not report a 202603 version after patch: ${user_version}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  echo "✓ TiDB family upgraded to ${TIDB_TAG}"
}

enable_share_lock_setting() {
  local user_tidb_cmd share_lock_value display_output

  user_tidb_cmd="$(remote_user_tidb_command)"

  CURRENT_STAGE="enable-share-lock-set"
  LAST_ERROR_REASON="failed to enable tidb_foreign_key_check_in_shared_lock on User TiDB"
  load_ssh "${user_tidb_cmd} -e \"SET GLOBAL tidb_foreign_key_check_in_shared_lock = 1;\" >/dev/null"

  CURRENT_STAGE="enable-share-lock-reload"
  LAST_ERROR_REASON="failed to reload TiDB family after enabling tidb_foreign_key_check_in_shared_lock"
  load_ssh "~/.tiup/bin/tiup cluster reload ${PREFIX} -R tidb -y"

  CURRENT_STAGE="enable-share-lock-reload-verify"
  LAST_ERROR_REASON="failed to verify TiDB family health after reloading tidb for share lock setting"
  display_output="$(cluster_display_output)"
  require_display_output_healthy "TiDB family after share lock reload for ${PREFIX}" "$display_output"
  require_display_hosts_present "tidb" "$display_output" \
    "${PREFIX}-tidb-system" \
    "${PREFIX}-tidb-0"

  CURRENT_STAGE="enable-share-lock-verify"
  LAST_ERROR_REASON="share lock setting is not visible from a new User TiDB connection"
  share_lock_value="$(load_ssh "${user_tidb_cmd} -Nse \"SELECT @@GLOBAL.tidb_foreign_key_check_in_shared_lock;\"" | tr '[:lower:]' '[:upper:]')"

  if [[ "$share_lock_value" != "1" && "$share_lock_value" != "ON" ]]; then
    LAST_ERROR_REASON="unexpected share lock setting value after upgrade: ${share_lock_value}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  echo "✓ share lock setting enabled and visible on a new User TiDB connection"
}

run_upgrade_acceptance_checks() {
  local display_output
  local user_tidb_cmd system_tidb_cmd
  local smoke_db="tidbx_gcp_upgrade_smoke"
  local smoke_result
  local user_version system_version

  user_tidb_cmd="$(remote_user_tidb_command)"
  system_tidb_cmd="$(remote_system_tidb_command)"

  CURRENT_STAGE="acceptance-display"
  LAST_ERROR_REASON="failed to verify tiup cluster display during final acceptance"
  display_output="$(cluster_display_output)"
  require_display_output_healthy "final acceptance for ${PREFIX}" "$display_output"
  require_display_hosts_present "pd" "$display_output" \
    "${PREFIX}-pd-0" \
    "${PREFIX}-pd-1" \
    "${PREFIX}-pd-2"
  require_display_hosts_present "tikv" "$display_output" \
    "${PREFIX}-tikv-0" \
    "${PREFIX}-tikv-1" \
    "${PREFIX}-tikv-2"
  require_display_hosts_present "tidb" "$display_output" \
    "${PREFIX}-tidb-system" \
    "${PREFIX}-tidb-0"

  CURRENT_STAGE="acceptance-system-tidb"
  LAST_ERROR_REASON="failed final connection check to System TiDB"
  load_ssh "${system_tidb_cmd} -Nse 'select 1' >/dev/null"

  CURRENT_STAGE="acceptance-user-tidb"
  LAST_ERROR_REASON="failed final connection check to User TiDB"
  load_ssh "${user_tidb_cmd} -Nse 'select 1' >/dev/null"

  CURRENT_STAGE="acceptance-version-check"
  LAST_ERROR_REASON="failed final 202603 version check on TiDB endpoints"
  verify_pd_binary_versions
  verify_tikv_binary_versions
  system_version="$(load_ssh "${system_tidb_cmd} -Nse 'select version()'")"
  user_version="$(load_ssh "${user_tidb_cmd} -Nse 'select version()'")"
  if [[ ! "$system_version" =~ $TIDB_VERSION_PATTERN ]]; then
    LAST_ERROR_REASON="System TiDB final acceptance version is not 202603: ${system_version}"
    fail_step "${LAST_ERROR_REASON}"
  fi
  if [[ ! "$user_version" =~ $TIDB_VERSION_PATTERN ]]; then
    LAST_ERROR_REASON="User TiDB final acceptance version is not 202603: ${user_version}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  CURRENT_STAGE="acceptance-tikv-worker-metrics"
  LAST_ERROR_REASON="tikv-worker metrics are not reachable during final acceptance"
  load_ssh "bash -lc 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null tidb@${PREFIX}-tikv-worker \"curl -sf http://localhost:19000/metrics >/dev/null\"'"

  CURRENT_STAGE="acceptance-smoke"
  LAST_ERROR_REASON="minimal SQL smoke test failed after upgrade"
  smoke_result="$(load_ssh "bash -s" <<EOF
set -euo pipefail
smoke_db="${smoke_db}"
read -r -a mysql_cmd <<< "$(printf '%s' "$user_tidb_cmd")"
cleanup() {
  "\${mysql_cmd[@]}" -Nse "DROP DATABASE IF EXISTS \${smoke_db};" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup
"\${mysql_cmd[@]}" -Nse "CREATE DATABASE \${smoke_db};"
"\${mysql_cmd[@]}" -Nse "CREATE TABLE \${smoke_db}.smoke (id BIGINT PRIMARY KEY, note VARCHAR(64));"
"\${mysql_cmd[@]}" -Nse "INSERT INTO \${smoke_db}.smoke VALUES (1, 'ok');"
"\${mysql_cmd[@]}" -Nse "SELECT COUNT(*) FROM \${smoke_db}.smoke;"
EOF
)"
  if [[ "$(printf '%s\n' "$smoke_result" | tail -n 1)" != "1" ]]; then
    LAST_ERROR_REASON="unexpected smoke test row count after upgrade: ${smoke_result}"
    fail_step "${LAST_ERROR_REASON}"
  fi

  echo "✓ Final upgrade acceptance checks passed"
}

print_failure_summary() {
  echo >&2
  echo "Upgrade failed." >&2
  echo "Prefix: ${PREFIX}" >&2
  echo "State file: ${STATE_FILE:-<unavailable>}" >&2
  echo "Stage: ${CURRENT_STAGE}" >&2
  echo "Reason: ${LAST_ERROR_REASON:-unknown upgrade failure}" >&2
  echo >&2
  echo "Load node login:" >&2
  echo "  $(load_login_command)" >&2
  echo >&2
  echo "On load node, show cluster status:" >&2
  echo "  $(tiup_display_command)" >&2
  echo >&2
  echo "On load node, inspect TiUP audit log:" >&2
  echo "  $(tiup_audit_log_command)" >&2
  echo >&2
  echo "On load node, connect to User TiDB:" >&2
  echo "  $(remote_user_tidb_command)" >&2
  echo >&2
  echo "On load node, inspect tikv-worker process:" >&2
  echo "  $(tikv_worker_process_inspect_command)" >&2
}

print_success_summary() {
  echo
  echo "Cluster upgraded successfully."
  echo "Prefix: ${PREFIX}"
  echo "State file: ${STATE_FILE}"
  echo
  echo "Load node login:"
  echo "  $(load_login_command)"
  echo
  echo "On load node, show cluster status:"
  echo "  $(tiup_display_command)"
  echo
  echo "On load node, connect to User TiDB:"
  echo "  $(remote_user_tidb_command)"
  echo
  echo "On load node, check share lock setting:"
  echo "  $(user_tidb_share_lock_check_command)"
  echo
  echo "Destroy command:"
  echo "  $(destroy_cluster_command)"
}

handle_error() {
  local exit_code=$?
  trap - ERR
  if [[ "$UPGRADE_STARTED" == true ]]; then
    mark_upgrade_failed "${LAST_ERROR_REASON:-upgrade-gcp-cluster.sh exited with status ${exit_code}}"
  fi
  print_failure_summary
  exit "$exit_code"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --prefix)
        if [[ $# -lt 2 ]]; then
          echo "Error: --prefix requires a value" >&2
          usage
          exit 1
        fi
        PREFIX="$2"
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
}

resolve_target_cluster() {
  if [[ -z "$PREFIX" ]]; then
    PREFIX="$(resolve_prefix_from_state)"
  fi

  if ! validate_prefix "$PREFIX"; then
    echo "Error: invalid cluster prefix: $PREFIX" >&2
    exit 1
  fi

  STATE_FILE="$(state_file_for "$PREFIX")"
}

main() {
  trap handle_error ERR

  parse_args "$@"
  resolve_target_cluster
  require_existing_state_file "$STATE_FILE"
  mark_upgrade_started
  run_prechecks
  prepare_remote_binaries
  upgrade_pd_family
  upgrade_tikv_worker
  apply_tikv_config_changes
  upgrade_tikv_family
  upgrade_tidb_family
  enable_share_lock_setting
  run_upgrade_acceptance_checks
  mark_upgrade_succeeded
  print_success_summary
}

main "$@"
