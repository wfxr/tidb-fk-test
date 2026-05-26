# TiUP Local Upgrade Scripts

This runbook covers the local single-host TiUP scripts under `scripts/`.

## What They Do

- `scripts/create-v855-cluster.sh`
  - deploys a local `v8.5.5` cluster
  - topology shape is `3 PD + 3 TiKV + 3 TiDB`
  - requires a positional `CLUSTER_NAME`
  - default deploy root is `scripts/data/<cluster-name>/`
  - default TiDB account is `root`
  - default password is empty
  - ensures a database named `test` exists
- `scripts/upgrade-to-v856.sh`
  - rolls the cluster forward with `tiup cluster patch -R`
  - patch order is `pd -> tikv -> tidb`
  - patch tarballs are generated from local TiUP component binaries under
    `~/.tiup/components/<role>/v8.5.6/`
- `scripts/toggle-shared-lock-fk-check.sh`
  - toggles `tidb_foreign_key_check_in_shared_lock` globally
  - executes the global setting through the first TiDB node
  - optionally reloads the TiDB role with `--restart`
  - verifies the global variable value on the first TiDB node, and on all
    TiDB nodes after restart
- `scripts/destroy-cluster.sh`
  - destroys the TiUP cluster
  - removes the repo-local runtime directory under `scripts/data/<cluster-name>/`

## Prerequisites

- `tiup` installed and able to SSH into `127.0.0.1`
- `mysql` client installed
- enough local disk and memory for a `3 PD + 3 TiKV + 3 TiDB` cluster plus
  Prometheus, Grafana, and Alertmanager
- local TiUP component downloads for:
  - `pd v8.5.6`
  - `tikv v8.5.6`
  - `tidb v8.5.6`

## Create a Cluster

Use a name and optional port offset:

```bash
./scripts/create-v855-cluster.sh \
  upgrade-poc-verify \
  --port-offset 200
```

Expected create result:

- TiUP deploys and starts the cluster
- `tiup cluster display <cluster-name> --versions` shows:
  - PD `v8.5.5`
  - TiKV `v8.5.5`
  - TiDB `v8.5.5`
- TiDB root account is `root` with no password by default
- database `test` exists after the script completes

## Upgrade a Cluster

Use a matching name and optional port offset:

```bash
./scripts/upgrade-to-v856.sh \
  upgrade-poc-verify \
  --port-offset 200
```

Expected upgrade behavior:

1. precheck confirms all TiDB nodes still report `v8.5.5`
2. PD is patched and `tiup cluster display -R pd --versions` shows `pd (patched)`
3. TiKV is patched and `tiup cluster display -R tikv --versions` shows `tikv (patched)`
4. TiDB is patched and `tiup cluster display -R tidb --versions` shows `tidb (patched)`
5. final TiDB SQL version checks report `8.0.11-TiDB-v8.5.6`

Note: with `tiup cluster patch -R`, TiUP may keep the cluster-level version as
`v8.5.5` in display output and indicate patched roles with `(patched)` instead
of rewriting the `Version` column to `v8.5.6`.

## Password Handling

The create script intentionally uses plain `tiup cluster start` instead of
`--init`, so the default script-created cluster keeps:

- username: `root`
- password: empty

If you manually ran `tiup cluster start --init` and enabled a root password,
export it when running verification or upgrade commands:

```bash
MYSQL_PASSWORD='your-password' ./scripts/upgrade-to-v856.sh your-cluster
```

The same `MYSQL_PASSWORD` override applies to the shared-lock toggle script:

```bash
MYSQL_PASSWORD='your-password' ./scripts/toggle-shared-lock-fk-check.sh your-cluster --enable
```

## Toggle Shared-Lock FK Check

Enable it without restart:

```bash
./scripts/toggle-shared-lock-fk-check.sh \
  upgrade-poc-verify \
  --enable \
  --port-offset 200
```

Enable it and restart TiDB so new sessions pick it up:

```bash
./scripts/toggle-shared-lock-fk-check.sh \
  upgrade-poc-verify \
  --enable \
  --restart \
  --port-offset 200
```

Disable it and restart TiDB:

```bash
./scripts/toggle-shared-lock-fk-check.sh \
  upgrade-poc-verify \
  --disable \
  --restart \
  --port-offset 200
```

Expected result:

1. the script verifies `tiup cluster display <cluster-name> --versions` is healthy
2. it executes `SET GLOBAL tidb_foreign_key_check_in_shared_lock = 1` or `0`
   through the first TiDB node
3. it verifies `SHOW GLOBAL VARIABLES LIKE 'tidb_foreign_key_check_in_shared_lock'`
   on the first TiDB node
4. if `--restart` is supplied, it runs `tiup cluster reload <cluster-name> -R tidb`
   and then verifies the
   variable value on all three TiDB nodes

## Destroy a Cluster

Use a matching name:

```bash
./scripts/destroy-cluster.sh upgrade-poc-verify
```

Expected destroy result:

- `tiup cluster destroy <cluster-name>` completes successfully
- the local runtime directory `scripts/data/<cluster-name>/` is removed
- deploy/data/log/package/rendered-topology files under that runtime directory
  are removed with it

## Useful Checks

Show cluster versions:

```bash
tiup cluster display your-cluster --versions
```

Check the default database exists:

```bash
mysql --skip-ssl -h 127.0.0.1 -P 4000 -u root -Nse "show databases like 'test'"
```

Check TiDB version:

```bash
mysql --skip-ssl -h 127.0.0.1 -P 4000 -u root -Nse "select version()"
```
