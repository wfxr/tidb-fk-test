# TiDB-X GCP Scripts

这组脚本用于在 GCP 上创建、部署、验证和销毁一套 TiDB-X next-gen 测试集群。

设计目标：

- 所有逻辑都维护在当前仓库，不依赖修改外部 skill 目录
- 固化本次已经验证通过的组件规格、版本和部署路径
- 支持从零一键创建出可工作的测试集群
- 支持通过本地状态文件辅助后续登录、连接和销毁

## 目录说明

- `create-gcp-cluster.sh`
  - 从零创建完整测试集群
  - 自动创建 GCP 资源、等待实例就绪、拉取 next-gen 二进制、远端部署并等待验收成功
- `destroy-gcp-cluster.sh`
  - 删除整个 Deployment Manager deployment
  - 可显式传入 prefix，也可在只有一个本地状态文件时自动读取
- `choose-prefix.sh`
  - 生成唯一 prefix，并检查当前 deployment / instance / disk 是否冲突
- `render-gcp-config.sh`
  - 渲染 `gcp-deployment-config.yaml`
- `gcp-deployment-config.yaml`
  - GCP Deployment Manager 模板
- `deploy-nextgen-cluster.sh`
  - 在 `load` 节点上执行 TiDB-X next-gen 集群部署
- `start-tikv-worker.sh`
  - 在 `tikv-worker` 节点启动 worker
- `fix-s3-bucket.sh`
  - 补建或修复 MinIO bucket
- `fetch-tidb-x-images.sh`
  - 远端抓取并打包 TiDB-X 二进制

## 一键创建

使用默认 base name 自动生成 prefix：

```bash
bash scripts/tidbx-gcp/create-gcp-cluster.sh
```

手工指定 prefix：

```bash
bash scripts/tidbx-gcp/create-gcp-cluster.sh --prefix your-prefix
```

脚本固定使用的版本为：

- `tiup`: `v8.5.4`
- `pd`: `v8.5.4-nextgen.202510.6`
- `tidb`: `v8.5.4-nextgen.202510.6`
- `tikv`: `v8.5.4-nextgen.202510.26`

当前固定拓扑：

- `load=1`
- `pd=3`
- `tikv=3`
- `tidb-system=1`
- `tidb-0=1`
- `tikv-worker=1`
- `minio=1`

## 销毁

显式指定 prefix：

```bash
bash scripts/tidbx-gcp/destroy-gcp-cluster.sh your-prefix --yes
```

如果本地 `state/` 下只有一个状态文件，也可以直接：

```bash
bash scripts/tidbx-gcp/destroy-gcp-cluster.sh --yes
```

如果本地状态文件有多个，脚本会列出每个 prefix 对应的 `.env` 文件，并要求你重新显式指定。

## 本地状态文件

创建脚本会在 `state/<prefix>.env` 中记录本地状态。

写入时机：

- 申请 GCP 资源之前：`STATUS=creating`
- 拿到 `load` 节点 IP 之后：`STATUS=deploying`
- 集群部署和验收成功之后：`STATUS=ready`

状态文件中包含：

- `PREFIX`
- `DEPLOYMENT_NAME`
- `PROJECT`
- `ZONE`
- `LOAD_IP`
- `LOAD_SSH_COMMAND`
- `TIUP_DISPLAY_COMMAND`
- `USER_TIDB_COMMAND`
- `SYSTEM_TIDB_COMMAND`
- `TOPOLOGY`
- `PD_TAG`
- `TIDB_TAG`
- `TIKV_TAG`

运行时生成的 `.env` 文件不应提交到 git。

## 前置条件

- `gcloud` 已安装并可访问 `gcp-tikv-transaction-dev`
- 本地可访问 Secret Manager 中的 `transaction-team-auth-key`
- 本地有 `ssh`、`scp`、`bash`
- 可通过 `transaction` 用户和团队密钥直连实例公网 IP

## 参考文档

- 仓库 runbook: [docs/runbooks/tidbx-gcp-cluster.md](/home/wenxuan/dev/tidbcloud/tidb-fk-test/docs/runbooks/tidbx-gcp-cluster.md:1)
