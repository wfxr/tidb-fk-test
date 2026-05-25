# FK Upgrade Workload Driver

This repo contains the Go implementation of the foreign-key upgrade workload
driver described in
`docs/plans/2026-05-25-fk-upgrade-workload-driver.md`.

The current implementation can run a real bounded warmup workload against a
TiDB/MySQL-compatible endpoint. It is still a smoke-path driver, not a full
rolling-upgrade controller.

## Current State

- `cmd/fk-upgrade-driver` accepts `-config` and loads YAML with strict unknown
  key rejection.
- The default worker layout is 8 total workers split into 4 generic, 3
  PropertyMe, and 1 failure-probe worker.
- The default `progress_report_interval` is `10s`.
- Startup still logs the initial warmup snapshot metadata before the workload
  starts.
- Seed application creates the generic, PropertyMe, and `probe_summary` tables
  used by scenarios and checker, then inserts bounded fixture rows.
- The CLI runs the real bounded warmup engine for `warmup_duration`, using the
  registered scenarios plus the seeded fixture plans.
- Each worker transaction explicitly runs
  `SET SESSION tidb_foreign_key_check_in_shared_lock = 1`, so local smoke runs
  exercise the shared-lock FK path instead of relying on external session
  setup.
- Recurring `warmup progress snapshot` logs are emitted every
  `progress_report_interval` while the warmup is still running.
- After warmup, the CLI logs a final bounded warmup summary. If
  `checker_enabled: true`, it then runs the checker against the seeded tables
  and the runtime report summary.
- The current bounded smoke path includes two expected-failure probes:
  - `payment_bill_update_probe`
  - `statement_folio_parent_update_probe`
- This is still not a full rolling-upgrade controller: there is no
  post-upgrade execution phase, no upgrade orchestration, and no `-dry-run` or
  phase control flags yet.

## Operator Quick Start

Run the repo checks from the repo root:

```bash
go test ./...
```

For a bounded local smoke run against a reachable TiDB/MySQL endpoint, use a
tiny override file so you do not need to edit the checked-in defaults. Partial
override files work because the binary starts from compiled defaults that match
`configs/default.yaml`:

```bash
cat >/tmp/fk-driver-smoke.yaml <<'EOF'
dsn: "root@tcp(127.0.0.1:4000)/test"
warmup_duration: 3s
progress_report_interval: 1s
checker_enabled: false
EOF

go run ./cmd/fk-upgrade-driver -config /tmp/fk-driver-smoke.yaml

# or, with the repo helper script:
./scripts/run-local-smoke.sh "root@tcp(127.0.0.1:4000)/test" false
```

Expected smoke behavior on a reachable local playground:

- the command logs `starting fk upgrade driver skeleton`
- the log includes `total_workers=8`
- the log includes the configured `progress_report_interval`
- the log includes the four seed phase names and `initial_phase=warmup`
- the command emits recurring `warmup progress snapshot` lines while warmup is
  still active
- the command logs `bounded warmup completed` before exiting
- the runtime summary should show non-zero `ExpectedFailure` counts for the two
  explicit expected-failure probes when the target TiDB honors shared-lock FK
  checking

Notes:

- If `warmup_duration` is shorter than or equal to
  `progress_report_interval`, you may only see the startup log and the final
  warmup summary because no ticker fire fits inside the warmup window.
- Exact execution counts depend on the target database and host speed, but a
  healthy local playground should usually show non-zero executed work over a
  3-second warmup.
- If you leave `checker_enabled: true`, the command runs the checker after the
  bounded warmup completes. For a minimal smoke path, keep it `false`.
- If you use a local playground for semantic verification, make sure the DSN
  points at the TiDB SQL port; the driver itself now enables
  `tidb_foreign_key_check_in_shared_lock` per transaction.

## Key Files

- `configs/default.yaml`: checked-in defaults and operator comments
- `docs/runbooks/fk-upgrade-workload-driver.md`: operator runbook from the repo
  root
- `cmd/fk-upgrade-driver/main.go`: current CLI entrypoint
- `scripts/run-local-smoke.sh`: convenience wrapper for local bounded smoke
