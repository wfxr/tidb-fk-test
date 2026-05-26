# FK Upgrade Workload Driver

This repo contains the Go implementation of a foreign-key upgrade workload
driver for TiDB-compatible clusters.

The current implementation is a two-step CLI:

- `prepare` creates schema, seeds deterministic fixture data, and records seed
  metadata in `fk_prepare_metadata`
- `run` loads that metadata, reconstructs the deterministic seed plan, runs the
  bounded workload for `--duration`, then always executes checker validation

It is still a smoke-path driver, not a full rolling-upgrade controller.

## Current State

- `cmd/fk-upgrade-driver` uses `cobra` with `prepare` and `run` subcommands.
- Connection flags are sysbench-like and explicit:
  `--nodes`, `--user`, `--password`, `--db`.
- Multi-node support is first-class through `--nodes` with
  `host:port,host:port,...`.
- `--user` defaults to `root`; `--db` defaults to `test`.
- `prepare` writes deterministic seed inputs to `fk_prepare_metadata`, so
  `run` does not need seed flags.
- The default worker layout is 8 total workers split into 4 generic, 3
  PropertyMe, and 1 failure-probe worker.
- Whether `tidb_foreign_key_check_in_shared_lock` is enabled is now controlled
  externally by the target environment, not by the driver itself.
- `run` emits recurring `run progress snapshot` logs every
  `--progress-report-interval` while the workload is active.
- `run` prints a final `Run Summary`, then always runs checker validation and
  prints a `Checker Summary`.
- `run` prints an `Error log: logs/error-<epoch>.log` line at startup.
- Detailed runtime errors are written to the error log file, not printed to the
  console.
- The current bounded path includes two expected-failure probes:
  - `payment_bill_update_probe`
  - `statement_folio_parent_update_probe`

## Operator Quick Start

Run the repo checks from the repo root:

```bash
go test ./...
```

Prepare a reachable TiDB/MySQL-compatible target:

```bash
go run ./cmd/fk-upgrade-driver prepare \
  --nodes 127.0.0.1:4000
```

Then run the bounded workload and checker:

```bash
go run ./cmd/fk-upgrade-driver run \
  --nodes 127.0.0.1:4000 \
  --duration 3s \
  --progress-report-interval 1s
```

Three-node local playground example:

```bash
go run ./cmd/fk-upgrade-driver prepare \
  --nodes 127.0.0.1:4000,127.0.0.1:4001,127.0.0.1:4002 && \
go run ./cmd/fk-upgrade-driver run \
  --nodes 127.0.0.1:4000,127.0.0.1:4001,127.0.0.1:4002 \
  --duration 3s \
  --progress-report-interval 1s
```

Expected run behavior on a reachable local playground:

- the command prints `Error log: logs/error-<epoch>.log`
- the command logs `starting fk upgrade driver`
- the log includes `initial_phase=run`
- the command emits recurring `run progress snapshot` lines while the workload
  is still active
- the command prints `Run Summary`
- the command prints `Checker Summary`
- detailed per-error lines are written to the error log file instead of being
  echoed to the console summaries
- the runtime summary should show non-zero `ExpectedFailure` counts for the two
  explicit expected-failure probes when the target TiDB honors shared-lock FK
  checking

Notes:

- `run` is strict: if `fk_prepare_metadata` is missing, version-incompatible,
  or the prepared seed rows are not present, it fails and tells you to run
  `prepare` first.
- `--duration` is the workload execution window. For a minimal smoke path, keep
  it short, such as `3s`.
- If `--duration` is shorter than or equal to
  `--progress-report-interval`, you may only see the startup log plus final
  summaries because no progress tick fits inside the run window.

## Key Files

- `docs/runbooks/fk-upgrade-workload-driver.md`: operator runbook from the repo
  root
- `docs/runbooks/tiup-local-upgrade-scripts.md`: local TiUP cluster create and
  upgrade script runbook
- `cmd/fk-upgrade-driver/main.go`: current CLI entrypoint
