# Smart Bill Manager

> **English:** [README_EN.md](README_EN.md)

Smart Bill Manager 是面向个人和小团队的自托管 AI 财务单据工作台。它把支付截图、发票和行程资料整理成可追溯候选；只有用户明确审核确认后，候选才会成为正式财务事实。

> [!IMPORTANT]
> `v0.3.4` 是 Clean Slate 公开实测预发布版，目前只提供 `linux/amd64` 单机部署。真实模型正确率、真实邮箱联调、TLS/域名和生产部署尚未完成，不应视为生产稳定版。

## Docker 快速部署

需要 `linux/amd64`、Docker Engine、Docker Compose 2.24.4 或更新版本、`curl`、`sha256sum`、`tar`，以及至少 6 GiB 可用内存。

### 一条命令安装（推荐）

直接下载固定 Tag 安装器、校验同版本部署包并进入引导安装：

```bash
version=v0.3.4; curl -fsSL --proto '=https' --tlsv1.2 "https://raw.githubusercontent.com/tuoro/Smart-bill-manager/${version}/tools/install-self-hosted.sh" | sh -s -- --release-version "$version"
```

安装器会询问运行目录、PostgreSQL 数据目录、附件目录、备份目录、Owner 信息和本机端口；直接回车使用默认值。

### Docker Compose

展开同版本 Release 部署包后，可运行 `./install.sh` 完成首次安装。需要显式管理 Compose 时，初始化完成后使用同一配置：

```bash
runtime_directory=/absolute/path/to/sbm-runtime
docker compose --project-name smart-bill-manager \
  --env-file "$runtime_directory/deployment.env" \
  --env-file infra/compose/release.env \
  -f infra/compose/compose.yaml \
  -f infra/compose/compose.release.yaml \
  up -d --no-build --pull never --wait app
```

首次 provision、migration 和 Owner bootstrap 仍由 `./install.sh` 可靠地顺序执行；不要用单独的 `compose up` 跳过它们。

### Docker CLI（`docker run` 风格）

如果 PostgreSQL 17、最小权限角色、Schema 和 Owner 已按部署指南准备完成，应用容器可以用 Docker CLI 启动：

```bash
docker run -d \
  --name smart-bill-manager \
  --restart unless-stopped \
  --init \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=268435456 \
  --tmpfs /run/sbm-secrets:rw,noexec,nosuid,nodev,size=65536,mode=0700 \
  --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add SETGID --cap-add SETUID \
  --security-opt no-new-privileges:true \
  --pids-limit 256 --cpus 2 --memory 3584m --stop-timeout 20 \
  --network smart-bill-manager_database \
  -p 127.0.0.1:7476:8080 \
  -v /absolute/path/to/objects:/var/lib/sbm/objects \
  --mount type=bind,src=/absolute/path/to/master-key,dst=/run/secrets/sbm_master_key,readonly \
  --mount type=bind,src=/absolute/path/to/postgres-runtime-password,dst=/run/secrets/sbm_postgres_runtime_password,readonly \
  -e SBM_DEPLOYMENT_MODE=local \
  -e SBM_COOKIE_SECURE=false \
  -e SBM_SESSION_TTL=168h \
  -e SBM_AI_CONCURRENCY=2 \
  ghcr.io/tuoro/smart-bill-manager:v0.3.4
docker network connect bridge smart-bill-manager
```

运行前应先停止 Compose 管理的 app，避免端口冲突。这条命令只启动应用容器，不会创建 PostgreSQL、角色、Schema 或 Owner；它复用 Compose 已创建的 internal 数据库网络，数据库别名仍为 `database`，随后接入默认 bridge 供 Provider 出站访问。完整安装优先使用一键脚本或 Compose，避免把应用镜像误当成内置数据库的单容器包。安装成功后打开 <http://127.0.0.1:7476>。完整边界见 [部署指南](docs/deployment.md)。

## 数据库与持久化

默认 Compose 会自动部署独立的 PostgreSQL 17 容器、创建最小权限角色并初始化数据库结构，普通用户无需填写数据库地址或手工运行 SQL。默认持久化布局位于创建的运行目录；安装时也可把下面三类目录分别映射到其他全新绝对路径：

```text
deployment/
├── data/postgres/     # PostgreSQL 数据
├── data/objects/      # 上传的图片和 PDF
├── backups/           # 独立验证的备份包
├── master-key         # Provider 密文所需主密钥
├── postgres-*-password
└── deployment.env     # 非秘密运行配置和 secret 文件路径
```

必须同时备份数据库、对象文件、主密钥和认证备份；不要把其中任何 secret 提交到 Git。`down` 只删除容器和网络，不删除上述目录。

Clean Slate 只表示不读取旧架构和 SQLite 数据。从当前新架构开始，后续版本默认保留数据，并通过版本化 PostgreSQL Schema migration 升级数据库结构；不会要求用户每次更新都清空数据库。

升级前先按 [备份与恢复说明](docs/backup-restore.md) 创建并独立验证备份，再换用新版本部署包：

```bash
./tools/sbm-deploy.sh "$runtime_directory" pull
./tools/sbm-deploy.sh "$runtime_directory" upgrade --backup-confirmed
```

## 主要能力

- 图片和 PDF 上传、批量逐项反馈与多页审核；
- 最小中文多模态提取、本地确定性规范化和字段级校验；
- Payment、Invoice、Trip 与完整 Source → Claim → Fact 追溯；
- 重复候选、支付—发票金额分配及独立调整；
- 邮件附件本地归档、行程归属和报销状态工作流；
- 确定性数据洞察、租户隔离、审计、认证备份与完整恢复。

## 安全与数据边界

```text
Source -> Claim -> Fact
原始证据 -> AI 候选 -> 用户确认的数据
```

- 模型不能直接创建 Fact；Schema、本地业务规则、权限和人工审核缺一不可。
- PostgreSQL 17 是唯一关系数据源，金额始终使用整数最小单位。
- API Key 加密保存，主密钥独立托管；部署工具不把 secret 放入环境、参数或仓库。
- 新系统不兼容旧代码、旧 API、旧数据库或旧任务状态，也不读取或迁移 `v0.2.4` 及更早版本数据。
- 默认只监听 `127.0.0.1`。不要未经 TLS 和生产验收直接暴露到局域网或公网。

## 当前限制

- 首个镜像仅支持 `linux/amd64`；
- 正式真实模型正确率评测尚未完成；
- 邮箱页面当前只保存无凭据连接描述符，不连接真实邮箱；
- 不包含域名、TLS、反向代理、远程 PostgreSQL、高可用或云对象存储；
- 不提供旧架构或 SQLite 数据导入；当前 Clean Slate PostgreSQL 版本之间默认保留数据并进行结构升级。

## 文档

| 入口 | 内容 |
| --- | --- |
| [部署指南](docs/deployment.md) | 安装、初始化、启动、停止和网络边界 |
| [本地运维](docs/local-operations.md) | 健康检查、容量、诊断与升级边界 |
| [备份与恢复](docs/backup-restore.md) | 认证备份、验证和完整恢复 |
| [产品与范围](docs/product.md) / [路线图](docs/roadmap.md) | 产品定位、已完成范围和后续门禁 |
| [架构](docs/architecture.md) / [数据模型](docs/data-model.md) | Source、Claim、Fact 与 PostgreSQL 设计 |
| [AI 流水线](docs/ai-pipeline.md) | 模型契约、规范化、校验和审核 |
| [验收标准](docs/acceptance.md) / [M4 证据](docs/m4-evidence.md) | 本地质量门禁和安全聚合证据 |

旧 `backend-go/`、`frontend/`、根 Dockerfile 和根 Compose 只保留为历史参考，不是当前运行入口。旧版本请使用对应历史 Release。

## 安全与许可

安全问题请按 [SECURITY.md](SECURITY.md) 私下报告，不要在公开 Issue 中披露。项目采用 [MIT License](LICENSE)。
