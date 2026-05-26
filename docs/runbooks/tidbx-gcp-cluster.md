# TiDB-X GCP 测试集群运行手册

本文档说明如何使用仓库内的 `scripts/tidbx-gcp/` 工具，在 GCP 上创建、部署并验收一套 TiDB-X next-gen 测试集群。

## 适用范围

- 目标为 TiDB-X next-gen 测试集群
- 部署平台为 GCP
- 集群拓扑为：
  - `1 load`
  - `3 pd`
  - `3 tikv`
  - `1 tidb-system`
  - `1 tidb-0`
  - `1 tikv-worker`
  - `1 minio`

不适用于仓库中现有的 OP / local TiUP 脚本。

## 前置条件

- 已安装并认证 `gcloud`
- 当前 GCP project 为 `gcp-tikv-transaction-dev`
- 默认 compute zone 为 `us-east5-b`
- 可通过 IAP 访问目标实例
- 本地可使用 SSH key `~/.ssh/gcp-tikv-transaction-dev.pem`
- 本地有 `bash`、`tmux`、`mysql`、`tiup`
- 有权限拉取目标 TiDB-X next-gen 二进制或镜像

建议先确认：

```bash
gcloud config get-value project
gcloud config get-value compute/zone
```

期望输出：

- project: `gcp-tikv-transaction-dev`
- zone: `us-east5-b`

## 目录与脚本

本流程只使用仓库内脚本：

- `scripts/tidbx-gcp/create-gcp-cluster.sh`
- `scripts/tidbx-gcp/choose-prefix.sh`
- `scripts/tidbx-gcp/render-gcp-config.sh`
- `scripts/tidbx-gcp/gcp-deployment-config.yaml`
- `scripts/tidbx-gcp/deploy-nextgen-cluster.sh`
- `scripts/tidbx-gcp/start-tikv-worker.sh`
- `scripts/tidbx-gcp/fix-s3-bucket.sh`
- `scripts/tidbx-gcp/destroy-gcp-cluster.sh`

## 一键创建

如果你要按本次已经验证通过的参数和版本，从零创建一套可工作的 TiDB-X 测试集群，直接执行：

```bash
bash scripts/tidbx-gcp/create-gcp-cluster.sh
```

可选地指定 prefix：

```bash
bash scripts/tidbx-gcp/create-gcp-cluster.sh --prefix your-prefix
```

这条脚本会自动完成：

- 生成或使用指定 prefix
- 渲染 Deployment Manager 配置
- 创建 GCP 资源
- 等待 `11` 台实例全部 `RUNNING`
- 用 `transaction-team-auth-key` 直连 `load` 节点
- 安装 `oras`
- 拉取并打包本次验证通过的 next-gen 二进制版本
- 把仓库内部署脚本传到 `load`
- 远端启动部署
- 等待部署脚本内置验收通过

脚本内固定使用的版本为：

- `tiup`: `v8.5.4`
- `pd`: `v8.5.4-nextgen.202510.6`
- `tidb`: `v8.5.4-nextgen.202510.6`
- `tikv`: `v8.5.4-nextgen.202510.26`

## 1. 生成唯一前缀

生成当前用户专属的 prefix：

```bash
export PREFIX=$(bash scripts/tidbx-gcp/choose-prefix.sh "${USER:-tidbx}")
echo "$PREFIX"
```

要求：

- prefix 唯一
- 后续 GCP deployment 名和 TiUP cluster 名都使用这个 prefix

## 2. 渲染 GCP Deployment Manager 配置

```bash
bash scripts/tidbx-gcp/render-gcp-config.sh "$PREFIX" > /tmp/${PREFIX}-deployment.yaml
```

建议检查生成结果中的关键节点：

```bash
rg -n "${PREFIX}-(load|pd-0|pd-1|pd-2|tikv-0|tikv-1|tikv-2|tidb-system|tidb-0|tikv-worker|minio)" /tmp/${PREFIX}-deployment.yaml
```

## 3. 创建 GCP 基础设施

```bash
gcloud deployment-manager deployments create ${PREFIX}-cluster \
  --config /tmp/${PREFIX}-deployment.yaml
```

等待实例创建完成：

```bash
gcloud compute instances list --filter="name~'^${PREFIX}-'"
```

## 4. 验证基础设施与预期一致

这是必做验收，不是可选检查。

必须确认：

- 一共存在 `11` 台目标实例
- 所有实例状态均为 `RUNNING`
- 实例名与设计一致：
  - `${PREFIX}-load`
  - `${PREFIX}-pd-0`
  - `${PREFIX}-pd-1`
  - `${PREFIX}-pd-2`
  - `${PREFIX}-tikv-0`
  - `${PREFIX}-tikv-1`
  - `${PREFIX}-tikv-2`
  - `${PREFIX}-tidb-system`
  - `${PREFIX}-tidb-0`
  - `${PREFIX}-tikv-worker`
  - `${PREFIX}-minio`

如果实例数量、名称或状态不对，不能继续部署集群。

## 5. 验证 IAP SSH 到 load 节点

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "echo ready"
```

## 6. 准备 next-gen 二进制

在 `${PREFIX}-load` 节点上准备：

- `/tmp/pd.tar.gz`
- `/tmp/tidb.tar.gz`
- `/tmp/tikv.tar.gz`

可以沿用 skill 中的推荐方式，在 load 节点安装 `oras` 后直接拉取并打包。无论使用哪种方式，最终要求是上面三个 tarball 都存在，并且版本代际与目标 `v8.5.4-nextgen.202510` 一致。

## 7. 复制仓库内脚本到 load 节点

```bash
gcloud compute scp \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --scp-flag="-o StrictHostKeyChecking=accept-new" \
  scripts/tidbx-gcp/deploy-nextgen-cluster.sh \
  scripts/tidbx-gcp/start-tikv-worker.sh \
  scripts/tidbx-gcp/fix-s3-bucket.sh \
  transaction@${PREFIX}-load:~/
```

## 8. 在 load 节点执行部署

建议使用 `tmux`：

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "tmux new-session -d -s deploy 'bash ~/deploy-nextgen-cluster.sh -n ${PREFIX} -v v8.5.4 2>&1 | tee /tmp/deploy.log'"
```

查看进度：

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "tmux capture-pane -pt deploy"
```

## 9. 部署后强制验收

以下项目必须全部通过，才能认定“集群已成功创建并可用于测试”。

### 9.1 TiUP 角色与 topology 一致

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "tiup cluster display ${PREFIX}"
```

必须确认：

- `3 PD`
- `3 TiKV`
- `2 TiDB`
- 角色主机名与设计一致

同时要检查部署脚本实际生成并用于部署的 topology 文件，确认其中角色数量与设计一致。

### 9.2 `tikv-worker` 必须使用三 PD endpoints

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "ssh -o StrictHostKeyChecking=no tidb@${PREFIX}-tikv-worker 'ps -ef | grep \"[t]ikv-worker\"'"
```

必须确认启动参数中包含：

- `${PREFIX}-pd-0:2379`
- `${PREFIX}-pd-1:2379`
- `${PREFIX}-pd-2:2379`

如果只连了单个 `pd-0`，则不能视为验收通过。

### 9.3 System TiDB 与 User TiDB 都可连接

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "mysql -h ${PREFIX}-tidb-system -P 3000 -u root -e 'select version()'"
```

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "mysql -h ${PREFIX}-tidb-0 -P 4000 -u root -e 'select version()'"
```

### 9.4 必须通过最小可工作性 smoke test

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "mysql -h ${PREFIX}-tidb-0 -P 4000 -u root -e \"drop database if exists tidbx_gcp_smoke; create database tidbx_gcp_smoke; create table tidbx_gcp_smoke.smoke(id bigint primary key, note varchar(64)); insert into tidbx_gcp_smoke.smoke values (1, 'ok'); select count(*) from tidbx_gcp_smoke.smoke;\""
```

必须确认：

- 能建库
- 能建表
- 能写入
- 能查询

### 9.5 global-sort 与 worker 可用

检查 `@@tidb_cloud_storage_uri`：

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "mysql -h ${PREFIX}-tidb-0 -P 4000 -u root -N -s -e 'select @@tidb_cloud_storage_uri;'"
```

检查 `tikv-worker` metrics：

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "curl -sf http://${PREFIX}-tikv-worker:19000/metrics >/dev/null && echo metrics-ok"
```

## 10. 失败恢复

### bucket 没创建成功

```bash
gcloud compute ssh transaction@${PREFIX}-load \
  --ssh-key-file ~/.ssh/gcp-tikv-transaction-dev.pem \
  --plain \
  --tunnel-through-iap \
  --ssh-flag="-o StrictHostKeyChecking=accept-new" \
  --command "bash ~/fix-s3-bucket.sh ${PREFIX}"
```

### 需要重新部署

如果 TiUP cluster 已存在，可以重新执行：

```bash
bash ~/deploy-nextgen-cluster.sh -n ${PREFIX} -v v8.5.4 --force
```

## 11. 销毁环境

删除整个 deployment：

```bash
bash scripts/tidbx-gcp/destroy-gcp-cluster.sh "${PREFIX}"
```

如需非交互删除：

```bash
bash scripts/tidbx-gcp/destroy-gcp-cluster.sh "${PREFIX}" --yes
```

## 验收结论

只有当以下条件全部成立时，才能判定这套集群创建成功：

1. `11` 台 GCP 实例都存在并处于 `RUNNING`
2. `tiup cluster display` 显示的角色数量与设计一致
3. 实际 topology 与设计中的主机映射一致
4. `tikv-worker` 进程确实使用三个 PD endpoints
5. `tidb-system` 和 `tidb-0` 都能连接成功
6. 最小 smoke test 成功
7. `@@tidb_cloud_storage_uri` 已设置，`tikv-worker` metrics 可访问

任一项不满足，都不能视为“集群已成功创建并可用于测试”。
