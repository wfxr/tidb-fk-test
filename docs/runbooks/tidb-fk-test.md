# tidb-fk-test Runbook

This runbook is for the current repo state. Follow it from the repo root.

## What Exists Today

The current `cmd/tidb-fk-test` binary exposes two subcommands:

- `prepare`
  - creates generic, Billing, `probe_summary`, and `fk_prepare_metadata`
    tables if needed
  - seeds deterministic fixture rows
  - records the seed-plan inputs and version in `fk_prepare_metadata`
- `run`
  - requires an existing prepared database
  - reads `fk_prepare_metadata`
  - rebuilds the deterministic seed plan in memory
  - validates that key prepared rows still exist
- runs the bounded workload for `--duration`
- emits recurring `run progress snapshot` logs on
  `--progress-report-interval`
- prints `Error log: logs/error-<epoch>.log` at startup
- prints `Run Summary`
- always executes checker queries and prints `Checker Summary`

Connection defaults:

- `--nodes` is required
- `--user` defaults to `root`
- `--db` defaults to `test`

The current binary still does not do these things:

- expose extra lifecycle subcommands beyond `prepare` and `run`
- provide a broader orchestration layer around the core FK test flow

## Defaults That Matter

- `duration: 15m`
  This is the default workload execution window for `run`.
- `progress_report_interval: 10s`
  This is the default cadence for recurring workload progress snapshots.
- Worker defaults:
  - `generic_workers: 4`
  - `billing_workers: 3`
  - `failure_probe_workers: 1`
- Seed defaults used by `prepare`:
  - `seed_parent_rows_per_table: 1000`
  - `seed_hot_parent_keys: 16`

## Smoke Verification

### 1. Run the test suite

```bash
go test ./...
```

### 2. Prepare a bounded smoke target

Single-node local example:

```bash
go run ./cmd/tidb-fk-test prepare \
  --nodes 127.0.0.1:4000
```

Three-node local playground example:

```bash
go run ./cmd/tidb-fk-test prepare \
  --nodes 127.0.0.1:4000,127.0.0.1:4001,127.0.0.1:4002
```

Expected result:

- exit code `0`
- a `prepare completed` log line
- the log includes `seed_plan_version`
- the log includes the completed phases, ending with
  `record_prepare_metadata`

### 3. Run a bounded smoke check

```bash
go run ./cmd/tidb-fk-test run \
  --nodes 127.0.0.1:4000 \
  --duration 3s \
  --progress-report-interval 1s
```

Equivalent three-node example:

```bash
go run ./cmd/tidb-fk-test run \
  --nodes 127.0.0.1:4000,127.0.0.1:4001,127.0.0.1:4002 \
  --duration 3s \
  --progress-report-interval 1s
```

Expected result with the current bounded run path:

- exit code `0`
- one console line printing `Error log: logs/error-<epoch>.log`
- one startup log line with message `starting tidb-fk-test`
- `initial_phase=run`
- recurring `run progress snapshot` logs while the `3s` duration is still
  active
- one printed `Run Summary`
- one printed `Checker Summary`
- detailed runtime error events are written to the error log file rather than
  printed in the console summaries
- non-zero `ExpectedFailure` counts if the target TiDB reproduces the current
  two explicit expected-failure probes under shared-lock checking

What you should not expect yet:

- exact deterministic execution counts across machines
- a broader orchestration layer beyond the current `prepare` and `run` flow

## Operator Notes

- `run` is strict. If metadata is missing, version-incompatible, or key seeded
  rows no longer exist, it fails fast and tells you to rerun `prepare`.
- Seed flags belong only to `prepare`. `run` always reads those seed inputs from
  `fk_prepare_metadata`.
- `tidb_foreign_key_check_in_shared_lock` is controlled externally by the
  target environment. The driver does not force that session variable on each
  transaction anymore.
- If a run fails, check the printed `logs/error-<epoch>.log` path first. The
  console output is intentionally concise and does not echo full error details.
- If you want a faster local smoke path, shorten `--duration`. Checker is
  always enabled for `run`.
