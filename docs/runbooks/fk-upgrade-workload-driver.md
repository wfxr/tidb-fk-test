# FK Upgrade Workload Driver Runbook

This runbook is for the current repo state. Follow it from the repo root.

## What Exists Today

The current `cmd/fk-upgrade-driver` binary does these things:

- loads defaults plus YAML overrides from `-config`
- requires a non-empty MySQL-compatible DSN
- applies real generic, PropertyMe, and `probe_summary` schema/fixture seed
  phases in a fixed order
- enables `tidb_foreign_key_check_in_shared_lock = 1` for every worker
  transaction
- builds and logs an initial warmup progress snapshot
- runs the bounded warmup engine for `warmup_duration`
- emits recurring `warmup progress snapshot` logs on
  `progress_report_interval` while warmup is still active
- logs a final bounded warmup summary
- optionally runs checker queries after warmup when `checker_enabled: true`

The current binary does not yet do these things:

- control an actual rolling upgrade
- run the planned post-upgrade execution window
- expose `-dry-run` or per-phase CLI flags

Keep that boundary in mind when operating it.

## Defaults That Matter

The checked-in defaults live in `configs/default.yaml`.

- `progress_report_interval: 10s`
  This is the configured cadence for recurring warmup progress snapshots.
  Ticker-driven progress logs are only emitted while warmup is still running, so
  very short smoke durations may finish before the first tick.
- `failure_probe_interval: 10s`
  The failure-probe scenario is part of the warmup registry. There is still no
  separate out-of-band probe loop in `main`.
- Current expected-failure probes:
  - `payment_bill_update_probe`
  - `statement_folio_parent_update_probe`
- `generic_workers: 4`, `propertyme_workers: 3`, `failure_probe_workers: 1`
  These worker-group counts drive the bounded warmup run.
- `total_workers: 8`
  This matches the current group-count sum. Keep them in sync when overriding
  the group counts.
- `warmup_duration: 15m` and `post_upgrade_duration: 20m`
  Only `warmup_duration` is exercised today. `post_upgrade_duration` is still
  logged as future intent.
- `checker_enabled: true`
  This is the default for the intended fuller workflow. For a minimal local
  smoke path, set it to `false` so the command exits after bounded warmup.

## Smoke Verification

### 1. Run the test suite

```bash
go test ./...
```

This should pass before you try the CLI.

### 2. Run a bounded smoke check

Create a small override file that points to a reachable TiDB/MySQL endpoint and
disables the checker. Keep the warmup window and progress interval short so the
smoke path finishes quickly. Partial override files work because the binary
starts from compiled defaults and only applies the keys present in the override
file:

```bash
cat >/tmp/fk-driver-smoke.yaml <<'EOF'
dsn: "root@tcp(127.0.0.1:4000)/test"
warmup_duration: 3s
progress_report_interval: 1s
checker_enabled: false
EOF
```

Then start the driver from the repo root:

```bash
go run ./cmd/fk-upgrade-driver -config /tmp/fk-driver-smoke.yaml
```

Or use the repo helper:

```bash
./scripts/run-local-smoke.sh "root@tcp(127.0.0.1:4000)/test" false
```

Expected result with the current bounded smoke path:

- exit code `0`
- one startup log line with message `starting fk upgrade driver skeleton`
- `seed_phases` contains:
  `apply_generic_schema`, `apply_propertyme_schema`,
  `seed_generic_fixtures`, `seed_propertyme_fixtures`
- `total_workers=8`
- `progress_report_interval=1s`
- `initial_phase=warmup`
- `initial_executed=0`
- recurring `warmup progress snapshot` logs while the 3-second warmup is still
  active
- one `bounded warmup completed` log line before exit
- non-zero `ExpectedFailure` counts if the target TiDB reproduces the current
  two explicit parent-upgrade probes under shared-lock checking

What you should not expect yet:

- an upgrade controller or a post-upgrade phase
- exact deterministic execution counts across machines
- extra CLI controls such as `-dry-run`

Scenario execution is live now, so a healthy local playground should usually
produce non-zero runtime counters over a 3-second warmup. If the warmup window
is shorter than the progress interval, the command may legitimately skip
recurring snapshot logs and only emit startup plus final summary lines.

### 3. Optional checker smoke

Only run this if you want the post-warmup checker as part of the smoke path:

```bash
cat >/tmp/fk-driver-checker.yaml <<'EOF'
dsn: "root@tcp(127.0.0.1:4000)/test"
warmup_duration: 3s
progress_report_interval: 1s
checker_enabled: true
EOF

go run ./cmd/fk-upgrade-driver -config /tmp/fk-driver-checker.yaml
```

Expected result on a correctly prepared database:

- the same startup log as above
- bounded warmup progress and completion logs
- a `checker summary` log line

Because the current seed path creates the checker tables itself, missing-table
checker failures now indicate an unexpected environment or schema drift rather
than a known placeholder limitation.

In the current implementation, a healthy checker-enabled smoke run should also
show `probe_error_match Passed:true` after warmup.

## Operator Notes

- Keep override files minimal. The loader starts from compiled defaults, and
  the checked-in `configs/default.yaml` mirrors those defaults for operators.
- You do not need to manually `SET SESSION tidb_foreign_key_check_in_shared_lock = 1`.
  The driver issues that statement for each worker transaction.
- Unknown YAML keys are rejected. This is intentional and useful for catching
  stale config names.
- If you want to change worker layout, update both `total_workers` and the
  three per-group counts together so the config remains coherent.
- If the CLI exits with `dsn is required`, the override file did not provide a
  usable `dsn`.
