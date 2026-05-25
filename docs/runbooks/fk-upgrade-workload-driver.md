# FK Upgrade Workload Driver Runbook

This runbook is for the current repo state, not the target end state from the
plan. Follow it from the repo root.

## What Exists Today

The current `cmd/fk-upgrade-driver` binary does these things:

- loads defaults plus YAML overrides from `-config`
- requires a non-empty MySQL-compatible DSN
- runs placeholder seed phases in a fixed order
- builds an initial warmup progress snapshot
- logs the configured `progress_report_interval`
- optionally runs checker queries when `checker_enabled: true`

The current binary does not yet do these things:

- run a long-lived workload loop
- execute registered scenarios against worker sessions
- emit recurring 10-second progress snapshots on a ticker
- expose `-dry-run` or per-phase CLI flags

Keep that boundary in mind when operating it.

## Defaults That Matter

The checked-in defaults live in `configs/default.yaml`.

- `progress_report_interval: 10s`
  This is the configured cadence for progress reporting. In the current
  skeleton, it appears in the startup log and is used to build the initial
  snapshot metadata. There is not yet a recurring ticker loop.
- `failure_probe_interval: 10s`
  This default exists in config, but there is not yet a live probe scheduler in
  `main`.
- `generic_workers: 4`, `propertyme_workers: 3`, `failure_probe_workers: 1`
  These are the worker-group counts that currently drive the reported worker
  total.
- `total_workers: 8`
  This matches the current group-count sum. Keep them in sync when overriding
  the group counts.
- `warmup_duration: 15m` and `post_upgrade_duration: 20m`
  These are logged today as intended future phase durations.
- `checker_enabled: true`
  This is the default for the intended full workflow, but it is too aggressive
  for a plain smoke run because the placeholder seed path does not create the
  checker tables.

## Smoke Verification

### 1. Run the test suite

```bash
go test ./...
```

This should pass before you try the CLI.

### 2. Run a startup-only smoke check

Create a small override file that points to a reachable TiDB/MySQL endpoint and
disables the checker. Partial override files work because the binary starts
from compiled defaults and only applies the keys present in the override file:

```bash
cat >/tmp/fk-driver-smoke.yaml <<'EOF'
dsn: "root@tcp(127.0.0.1:4000)/test"
checker_enabled: false
EOF
```

Then start the driver from the repo root:

```bash
go run ./cmd/fk-upgrade-driver -config /tmp/fk-driver-smoke.yaml
```

Expected result with the current skeleton:

- exit code `0`
- one startup log line with message `starting fk upgrade driver skeleton`
- `seed_phases` contains:
  `apply_generic_schema`, `apply_propertyme_schema`,
  `seed_generic_fixtures`, `seed_propertyme_fixtures`
- `total_workers=8`
- `progress_report_interval=10s`
- `initial_phase=warmup`
- `initial_executed=0`

What you should not expect yet:

- recurring progress logs every 10 seconds
- non-zero success counters
- scenario execution or classified runtime failure summaries

Those behaviors belong to the next implementation steps, not the current
binary.

### 3. Optional checker smoke

Only run this if your target database already contains the checker tables and
expected fixture data:

```bash
cat >/tmp/fk-driver-checker.yaml <<'EOF'
dsn: "root@tcp(127.0.0.1:4000)/test"
checker_enabled: true
EOF

go run ./cmd/fk-upgrade-driver -config /tmp/fk-driver-checker.yaml
```

Expected result on a correctly prepared database:

- the same startup log as above
- a `checker summary` log line

Expected result on an unprepared database:

- startup succeeds
- checker phase fails on missing tables such as `child_basic`,
  `child_cascade`, or `probe_summary`

That failure is consistent with the current placeholder seeding behavior.

## Operator Notes

- Keep override files minimal. The loader starts from compiled defaults, and
  the checked-in `configs/default.yaml` mirrors those defaults for operators.
- Unknown YAML keys are rejected. This is intentional and useful for catching
  stale config names.
- If you want to change worker layout, update both `total_workers` and the
  three per-group counts together so the config remains coherent.
- If the CLI exits with `dsn is required`, the override file did not provide a
  usable `dsn`.
