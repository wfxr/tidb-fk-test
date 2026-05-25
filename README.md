# FK Upgrade Workload Driver

This repo contains the early Go skeleton for the foreign-key upgrade workload
driver described in
`docs/plans/2026-05-25-fk-upgrade-workload-driver.md`.

The current implementation is useful for config loading, seed-plan shaping, the
scenario registry, startup logging, and checker orchestration. It is not yet a
full long-running workload generator.

## Current State

- `cmd/fk-upgrade-driver` accepts `-config` and loads YAML with strict unknown
  key rejection.
- The default worker layout is 8 total workers split into 4 generic, 3
  PropertyMe, and 1 failure-probe worker.
- The default `progress_report_interval` is `10s`.
- Startup builds an initial warmup snapshot and logs the configured
  `progress_report_interval`, but the skeleton does not yet emit recurring
  10-second progress snapshots.
- Seed application currently runs placeholder seed phases so the command can
  exercise orchestration flow without a full schema implementation.
- The checker hook is wired and enabled by default, but it expects real tables
  such as `child_basic`, `child_cascade`, and `probe_summary` that the current
  placeholder seeding does not create.
- Scenario execution loops, runtime event logging, `-dry-run`, and phase control
  flags are not implemented yet.

## Operator Quick Start

Run the repo checks from the repo root:

```bash
go test ./...
```

For a smoke startup against a reachable TiDB/MySQL endpoint, use a tiny override
file so you do not need to edit the checked-in defaults. Partial override files
work because the binary starts from compiled defaults that match
`configs/default.yaml`:

```bash
cat >/tmp/fk-driver-smoke.yaml <<'EOF'
dsn: "root@tcp(127.0.0.1:4000)/test"
checker_enabled: false
EOF

go run ./cmd/fk-upgrade-driver -config /tmp/fk-driver-smoke.yaml
```

Expected smoke behavior today:

- the command logs `starting fk upgrade driver skeleton`
- the log includes `total_workers=8`
- the log includes `progress_report_interval=10s`
- the log includes the four seed phase names and `initial_phase=warmup`
- the command exits after the startup log because there is no recurring worker
  loop yet

If you leave `checker_enabled: true`, the command is expected to proceed into
checker queries. On a plain test database without the checker tables, that will
fail after startup.

## Key Files

- `configs/default.yaml`: checked-in defaults and operator comments
- `docs/runbooks/fk-upgrade-workload-driver.md`: operator runbook from the repo
  root
- `cmd/fk-upgrade-driver/main.go`: current CLI entrypoint
