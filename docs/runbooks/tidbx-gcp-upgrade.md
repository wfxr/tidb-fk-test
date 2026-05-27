# TiDB-X GCP 集群升级运行手册

本文档说明如何使用仓库内的 `scripts/tidbx-gcp/upgrade-gcp-cluster.sh`，把由 `create-gcp-cluster.sh` 创建的本地 TiDB-X GCP 测试集群升级到固定的 `202603` 目标版本。

本文档反映当前仓库中升级脚本的现状和当前本地拓扑，不扩展到 Cloud 全量组件。

当前这条升级路径已经基于真实集群 `wenxuan-poc` 完成过一轮端到端验证：

- 源版本：
  - `pd`: `v8.5.4-nextgen.202510.6`
  - `tidb`: `v8.5.4-nextgen.202510.6`
  - `tikv`: `v8.5.4-nextgen.202510.26`
- 目标版本：
  - `pd`: `v26.3.0-nextgen`
  - `tidb`: `v26.3.0-nextgen`
  - `tikv`: `v26.3.2-nextgen`

## 适用范围

适用对象：

- 由仓库内 `scripts/tidbx-gcp/create-gcp-cluster.sh` 创建的 TiDB-X GCP 测试集群
- 本地固定拓扑：
  - `1 load`
  - `3 pd`
  - `3 tikv`
  - `1 tidb-system`
  - `1 tidb-0`
  - `1 tikv-worker`
  - `1 minio`
- 通过本地状态文件 `scripts/tidbx-gcp/state/<prefix>.env` 管理的集群

不适用对象：

- Cloud 生产集群
- OP 版本集群
- 不带本地 state 文件的手工集群
- 当前拓扑之外的组件形态，例如：
  - `TiFlash`
  - 独立 `cop-worker`
  - 独立 `tidb-worker`
  - `TiProxy`
  - `keyspace 2` 独立 TiDB 前端

## 固定目标版本

升级脚本当前固定的目标版本是：

- `pd`: `v26.3.0-nextgen`
- `tidb`: `v26.3.0-nextgen`
- `tikv`: `v26.3.2-nextgen`
- `tikv-worker`: 随 `tikv` 包升级，目标代际与 `tikv` 对齐

脚本不会在命令行上暴露版本参数；当前行为就是固定升级到这组版本。

## 前置条件

本地需要具备：

- `bash`
- `gcloud`
- `ssh`
- `scp`
- 可访问 GCP project `gcp-tikv-transaction-dev`
- 可访问 Secret Manager 中的 `transaction-team-auth-key`

脚本依赖本地 state 文件提供以下信息：

- `PROJECT`
- `ZONE`
- `DEPLOYMENT_NAME`
- `LOAD_IP`
- `USER_TIDB_COMMAND`
- `SYSTEM_TIDB_COMMAND`

如果 state 文件缺失或字段不完整，升级脚本会在前置检查阶段失败。

## 升级顺序与 SOP 对齐关系

当前脚本的主流程固定为：

1. 前置检查
2. 远端准备升级二进制
3. `PD family`
4. `tikv-worker`
5. `TiKV` 配置变更
6. `TiKV family`
7. `TiDB family`
8. 开启并验证 `share-lock` 变量
9. 最终验收

这条顺序是按 Cloud SOP 的主顺序映射到本地拓扑后的结果。映射关系如下：

| Cloud SOP 阶段 | 本地脚本映射 |
| --- | --- |
| `PD` / `PD tso` / `PD scheduling` | `3 PD` 统一作为 `PD family` 升级 |
| `Tikv worker` | 单独的 `tikv-worker` 节点与进程 |
| `Tikv` | `3 TiKV` 节点 |
| `TiDB worker` / `TiDB - keyspace 1` / `TiDB - keyspace 2` | 本地的 `tidb-system` 和 `tidb-0` |
| 升级后开启 `tidb_foreign_key_check_in_shared_lock` | 升级后在 `User TiDB` 上执行，reload `tidb` role，并用新连接验证 |

当前脚本不会处理本地不存在的 Cloud 组件。它们不是“忘了做”，而是当前拓扑里确实没有：

- `TiFlash CN / WN`
- 独立 `cop-worker`
- 独立 `tidb-worker`
- `TiProxy`
- `keyspace 2` 独立前端

## 当前脚本实际会做什么

当前 `upgrade-gcp-cluster.sh` 的设计行为包括：

- 从 `--prefix` 或 `scripts/tidbx-gcp/state/*.env` 选择目标集群
- 在 state 文件中写入 `UPGRADE_*` 字段，记录：
  - `UPGRADE_STATUS`
  - `UPGRADE_STAGE`
  - `UPGRADE_TARGET_PD_TAG`
  - `UPGRADE_TARGET_TIDB_TAG`
  - `UPGRADE_TARGET_TIKV_TAG`
  - `UPGRADE_LAST_ERROR`
  - `UPGRADE_STARTED_AT`
  - `UPGRADE_FINISHED_AT`
- 检查：
  - deployment 存在
  - `11` 台预期实例都存在且为 `RUNNING`
  - `load` 节点可 SSH 登录
  - `tiup cluster display --versions` 可执行且 managed topology 符合当前设计
  - `User TiDB` 可连接
- 在 `load` 节点上准备：
  - `/tmp/pd.tar.gz`
  - `/tmp/tidb.tar.gz`
  - `/tmp/tikv.tar.gz`
- 使用 `tiup cluster patch` 升级：
  - `pd`
  - `tikv`
  - `tidb`
- 手工替换并重启 `tikv-worker`
- 尝试把 `202603` 所需的 TiKV 配置项写入 TiUP 元数据，并在 TiKV 升级后检查 live `tikv.toml`
- 在升级后执行：
  - `SET GLOBAL tidb_foreign_key_check_in_shared_lock = 1`
  - `tiup cluster reload <prefix> -R tidb -y`
  - 新连接验证该变量
  - 最终版本、连通性、`tikv-worker` metrics 和最小 SQL smoke test

需要明确的是：

- 本文档描述的是当前脚本的已实现行为
- 这条路径已经在一个真实测试集群上跑通过
- 但它仍然只针对当前本地固定拓扑有效，不等于 Cloud 全量 SOP 的通用实现

## 使用方法

### 1. 指定 prefix 升级

推荐显式指定目标集群：

```bash
bash scripts/tidbx-gcp/upgrade-gcp-cluster.sh --prefix wenxuan-poc
```

### 2. 只有一个 state 文件时直接升级

如果 `scripts/tidbx-gcp/state/` 下只有一个 `.env` 文件，也可以直接运行：

```bash
bash scripts/tidbx-gcp/upgrade-gcp-cluster.sh
```

### 3. 有多个 state 文件时的行为

如果 `state/` 目录下有多个 `.env` 文件，而你又没有传 `--prefix`，脚本会直接退出，并列出每个 prefix 及对应文件路径，要求你重新显式指定。

### 4. 升级成功后的输出

脚本成功时会打印这些命令：

- 登录 `load` 节点
- 在 `load` 节点执行 `tiup cluster display --versions`
- 在 `load` 节点连接 `User TiDB`
- 在 `load` 节点检查 `share-lock` 变量
- 销毁当前集群的命令

## 升级阶段说明

### 阶段 1：前置检查

脚本会检查：

- state 文件存在
- 本地依赖命令存在
- deployment 存在
- `11` 台实例齐全且全为 `RUNNING`
- `load` 节点 SSH 正常
- `tiup cluster display --versions` 正常
- `3 PD + 3 TiKV + 2 TiDB` 与当前 managed topology 一致
- `User TiDB` 能执行 `select 1`

这些检查有任一失败，升级不会继续。

### 阶段 2：远端准备二进制

脚本会在 `load` 节点上：

- 确保 `/tmp/oras` 可用
- 确保 `jq` 可用
- 刷新 `fetch-tidb-x-images.sh`
- 登录 `us.gcr.io`
- 拉取固定版本的 `pd` / `tidb` / `tikv` 包

### 阶段 3：升级 `PD family`

当前脚本使用：

```bash
~/.tiup/bin/tiup cluster patch <prefix> /tmp/pd.tar.gz -R pd -y
```

之后会重新读取 `tiup cluster display --versions`，检查：

- 输出中没有 `Down` / `Offline` / `ERR` / `N/A` / `Tombstone`
- `pd-0` / `pd-1` / `pd-2` 都仍然存在于 managed topology 中

### 阶段 4：升级 `tikv-worker`

当前脚本会：

- 从 `/tmp/tikv.tar.gz` 中提取 `tikv-worker`
- 刷新 `start-tikv-worker.sh`
- 停止旧的 `tikv-worker`
- 等待旧进程退出
- 替换二进制
- 用 `3 PD endpoints` 重新启动
- 校验：
  - `http://localhost:19000/metrics` 可访问
  - 进程参数中包含：
    - `${prefix}-pd-0:2379`
    - `${prefix}-pd-1:2379`
    - `${prefix}-pd-2:2379`

### 阶段 5：TiKV 配置变更

当前脚本尝试处理这组 TiKV 配置项：

- `rfengine.enable-compact-rate-limiter: true`
- `pd.report-store-size-with-keyspace-name: true`
- `pd.load-all-keyspaces-on-start: true`
- `storage.flow-control.enable: false`

当前实现思路是：

- 从 TiUP `meta.yaml` 中抽取 topology
- 生成带上述 `server_configs.tikv` 配置的 topology 文件
- 调用 `tiup cluster edit-config` 尝试把配置写回 TiUP 元数据
- 在后续 TiKV 升级完成后，再检查 live `tikv.toml`

这里要注意：

- 当前脚本不是直接修改远端 `tikv.toml`
- 它会基于 `~/.tiup/storage/cluster/clusters/<prefix>/meta.yaml` 中的 topology 生成更新版 topology 文件
- 再通过 `tiup cluster edit-config --topology-file ... -y` 写回 TiUP 管理配置
- `TiKV patch` 和后续 `reload` 完成后，脚本会检查 live `tikv.toml`

### 阶段 6：升级 `TiKV family`

当前脚本使用：

```bash
~/.tiup/bin/tiup cluster patch <prefix> /tmp/tikv.tar.gz -R tikv -y
```

之后会检查：

- `tiup cluster display --versions` 依旧健康
- `tikv-0` / `tikv-1` / `tikv-2` 依旧在 topology 中
- `User TiDB` 仍可连接
- 所需 TiKV 配置已出现在各 TiKV 节点 live `tikv.toml`

### 阶段 7：升级 `TiDB family`

当前脚本使用：

```bash
~/.tiup/bin/tiup cluster patch <prefix> /tmp/tidb.tar.gz -R tidb -y
```

之后会检查：

- `tidb-system` / `tidb-0` 依旧在 topology 中
- `select version()` 返回值中包含 `26.3` 或 `202603`

### 阶段 8：开启并验证 `share-lock` 变量

当前脚本会在 `User TiDB` 上执行：

```sql
SET GLOBAL tidb_foreign_key_check_in_shared_lock = 1;
```

然后执行：

```bash
~/.tiup/bin/tiup cluster reload <prefix> -R tidb -y
```

然后通过新的连接执行：

```sql
SELECT @@GLOBAL.tidb_foreign_key_check_in_shared_lock;
```

只有返回 `1` 或 `ON` 才算通过。

### 阶段 9：最终验收

当前脚本的最终验收包括：

- `tiup cluster display --versions` 健康检查
- `pd` / `tikv` / `tidb` 各预期主机仍存在
- `PD` 通过 `pd-server -V` 校验 `Release Version: v26.3.0`
- `TiKV` 通过 `tikv-server -V` 校验目标 git hash
- `System TiDB` / `User TiDB` 连通
- `select version()` 返回值包含 `26.3` 或 `202603`
- `tikv-worker` metrics 可访问
- 最小 SQL smoke test 通过

注意：

- `tiup cluster display --versions` 的 `Version` 列在 `patch` 场景下仍可能保留 `v8.5.4`
- 因此脚本不会再用它作为最终版本真值来源，只把它用于健康和 topology 检查

smoke test 会使用固定测试库名：

- `tidbx_gcp_upgrade_smoke`

脚本在执行前和退出时都会尝试清理这个测试库。

## 失败排查

脚本失败后会：

- 把 state 文件中的：
  - `UPGRADE_STATUS=failed`
  - `UPGRADE_STAGE=<当前阶段>`
  - `UPGRADE_LAST_ERROR=<错误摘要>`
- 打印一组人工排查入口

### 本地先看 state 文件

```bash
cat scripts/tidbx-gcp/state/<prefix>.env
```

重点关注：

- `UPGRADE_STATUS`
- `UPGRADE_STAGE`
- `UPGRADE_LAST_ERROR`
- `UPGRADE_STARTED_AT`
- `UPGRADE_FINISHED_AT`

### 登录 `load` 节点

优先使用脚本失败摘要里打印的命令。典型格式类似：

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
  -i ~/.ssh/gcp-tikv-transaction-dev.pem \
  transaction@<load-ip>
```

### 在 `load` 节点查看 TiUP 集群状态

```bash
~/.tiup/bin/tiup cluster display <prefix> --versions
```

### 在 `load` 节点查看 TiUP audit log

```bash
tail -n 200 ~/.tiup/storage/cluster/clusters/<prefix>/audit.log
```

### 在 `load` 节点连接 `User TiDB`

```bash
mysql -h <prefix>-tidb-0 -P 4000 -u root
```

### 从本地经 `load` 节点查看 `tikv-worker` 进程

脚本失败摘要会打印一条可直接使用的检查命令。它的访问链路是：

- 本地机器
- `load` 节点
- `tikv-worker` 内网主机

如果你要手工执行，逻辑上等价于：

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
  -i ~/.ssh/gcp-tikv-transaction-dev.pem \
  transaction@<load-ip> \
  'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null tidb@<prefix>-tikv-worker "pgrep -af tikv-worker"'
```

### 常见失败阶段与排查方向

`precheck-*`

- 先看 state 文件是否完整
- 看 deployment 是否还存在
- 看 `11` 台实例是否齐全且为 `RUNNING`
- 看 `load` 节点是否还能 SSH

`prepare-binaries-*`

- 看 `load` 节点是否能访问镜像仓库
- 看 `oras` / `jq` 是否可用
- 看 `/tmp/pd.tar.gz`、`/tmp/tidb.tar.gz`、`/tmp/tikv.tar.gz` 是否生成

`upgrade-pd` / `upgrade-tikv` / `upgrade-tidb`

- 看 `tiup cluster display --versions`
- 看 `audit.log`
- 看目标角色是否有 `Down` / `ERR`

`upgrade-tikv-worker-*`

- 看 `pgrep -af tikv-worker`
- 看 `localhost:19000/metrics`
- 看进程参数里是否带了 `3 PD endpoints`

`enable-share-lock-*`

- 在新连接里执行：

```sql
SELECT @@GLOBAL.tidb_foreign_key_check_in_shared_lock;
```

`acceptance-*`

- 先确认 display、连通性和版本信息
- 再看 smoke test 是否留下脏数据或中间失败

## 升级后 share-lock 变量验证

如果你想在脚本执行完成后手工再检查一遍，可以在 `load` 节点上执行：

```bash
mysql -h <prefix>-tidb-0 -P 4000 -u root -Nse \
  "SELECT @@GLOBAL.tidb_foreign_key_check_in_shared_lock;"
```

期望结果：

- `1`
- 或 `ON`

如果你已经有 `state/<prefix>.env` 中记录的 `USER_TIDB_COMMAND`，也可以直接复用：

```bash
<USER_TIDB_COMMAND> -Nse "SELECT @@GLOBAL.tidb_foreign_key_check_in_shared_lock;"
```

## 当前边界与注意事项

- 本手册只覆盖当前本地拓扑，不覆盖 Cloud 全量组件
- 升级目标版本当前是固定写死的，不支持命令行覆盖
- 当前脚本是“失败即停，不自动回滚”
- 如果脚本失败，优先保留现场并按上面的排查路径处理，不建议直接手工跳过失败阶段继续后续步骤
- 关于 TiKV 配置变更，本手册只描述当前脚本的实现方式；是否在真实环境中稳定适用于所有场景，应以实际执行结果为准
