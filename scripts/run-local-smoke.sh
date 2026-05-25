#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <mysql-dsn> [checker_enabled:true|false]" >&2
  echo "example: $0 'root@tcp(127.0.0.1:33539)/test' false" >&2
  exit 2
fi

dsn="$1"
checker_enabled="${2:-false}"

tmp_config="$(mktemp /tmp/fk-driver-smoke.XXXXXX.yaml)"
trap 'rm -f "$tmp_config"' EXIT

cat >"$tmp_config" <<EOF
dsn: "$dsn"
warmup_duration: 3s
progress_report_interval: 1s
checker_enabled: $checker_enabled
EOF

echo "Running bounded FK smoke with config: $tmp_config"
go run ./cmd/fk-upgrade-driver -config "$tmp_config"
