# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go CLI for TiDB foreign-key workload testing. The entrypoint lives in `cmd/tidb-fk-test/`, with the main commands split into `prepare` and `run`. Core packages are under `internal/`: use `config` for defaults, `db` for cluster/session access, `seed` for deterministic fixture setup, `runner` and `scenario` for workload execution, `checker` for validation, and `report`/`logging` for summaries and logs. Operator documentation lives in `docs/runbooks/`. Local cluster helpers are in `scripts/`, and GCP-specific bootstrap assets are in `gcp/`.

## Build, Test, and Development Commands

Use direct Go commands from the repo root; there is no `Makefile`.

- `go test ./...` runs the full unit test suite.
- `go run ./cmd/tidb-fk-test prepare --nodes 127.0.0.1:4000,127.0.0.1:4001,127.0.0.1:4002` creates schema, seeds data, and records `fk_prepare_metadata`.
- `go run ./cmd/tidb-fk-test run --nodes 127.0.0.1:4000,127.0.0.1:4001,127.0.0.1:4002 --duration 3s --progress-report-interval 1s` runs a short smoke workload and checker pass.
- `scripts/create-v855-cluster.sh <name>` provisions a local TiUP v8.5.5 cluster.
- `scripts/upgrade-to-v856.sh <name>` patches that local cluster through the documented upgrade order.
- `scripts/toggle-shared-lock-fk-check.sh <name> --enable|--disable [--restart]` flips the global `tidb_foreign_key_check_in_shared_lock` setting, and can reload TiDB so new sessions pick it up.

## Coding Style & Naming Conventions

Format Go code with `gofmt`; keep imports gofmt-sorted. Follow standard Go naming: exported identifiers in `CamelCase`, package-private helpers in `mixedCaps`, and package names in short lowercase nouns such as `checker` or `runner`. Keep tests beside implementation files with `_test.go` suffixes. Shell scripts should stay Bash-compatible, start with `#!/usr/bin/env bash`, and use `set -euo pipefail`.

## Testing Guidelines

Add unit tests for every behavior change in the affected `internal/` package or CLI command. Prefer table-driven tests where inputs and expected summaries vary. Name tests by behavior, for example `TestRunFailsWithoutPreparedMetadata`. Run `go test ./...` before opening a PR; when changing workflow behavior, also run a local three-node `prepare`/`run` smoke path against `127.0.0.1:4000,127.0.0.1:4001,127.0.0.1:4002`.

## Commit & Pull Request Guidelines

Recent history uses Conventional Commits, for example `fix(logging): ...`, `feat: ...`, and `chore(repo): ...`. Keep scopes specific to the touched area. PRs should explain the operator-visible change, list the commands you ran, and note any required cluster setup or topology assumptions. Include relevant log paths or runbook updates when the change affects runtime behavior or scripts.

## Security & Configuration Tips

Connection settings are flag-driven; config files are intentionally deprecated. Do not commit credentials, generated `logs/`, or local cluster artifacts. If a change depends on TiDB session variables or upgrade state, document that explicitly in `README.md` or `docs/runbooks/`.
