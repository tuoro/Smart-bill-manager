# Smart Bill Manager

> **English:** [README_EN.md](README_EN.md)

Smart Bill Manager 是面向个人和小团队的自托管 AI 财务单据工作台。它把支付截图、发票和行程资料整理成可追溯候选；只有用户明确审核确认后，候选才会成为正式财务事实。

> [!IMPORTANT]
> `v0.4.0` 是 Clean Slate 公开实测预发布版，目前只提供 `linux/amd64` 单机部署。真实模型正确率、真实邮箱联调、TLS/域名和生产部署尚未完成，不应视为生产稳定版。

## 安装

需要 `linux/amd64` 主机、Docker Engine，以及至少 6 GiB 可用内存。两条命令：

```bash
docker network create my-net

docker run -d --name my-postgres --network my-net \
  --restart unless-stopped \
  -e POSTGRES_USER=sbm_app \
  -e POSTGRES_DB=smart_bill_manager \
  -e POSTGRES_PASSWORD=<自己设一个数据库密码> \
  -v sbm-postgres:/var/lib/postgresql/data \
  postgres:17-alpine

docker run -d --name smart-bill-manager --network my-net \
  --restart unless-stopped --init --stop-timeout 20 \
  -p 127.0.0.1:8080:8080 \
  -v sbm-data:/var/lib/sbm \
  ghcr.io/tuoro/smart-bill-manager:v0.4.0
```

打开 <http://127.0.0.1:8080>，页面分两步引导：先填数据库连接（地址填 `my-postgres`，账号密码用上面设的，可先点「检测连接」），验证通过后自动建表；再创建管理员账号（用户名 + 密码）。之后即可使用。

数据库连接也可以用 `-e SBM_POSTGRES_HOST`、`-e SBM_POSTGRES_USER`、`-e SBM_POSTGRES_PASSWORD` 预先指定，页面就会跳过第一步。已经有 PostgreSQL 的话不需要起第一个容器，直接指向它即可。

升级时换用新的镜像 tag 重建应用容器。存在未执行的数据库迁移时应用会拒绝启动并提示——迁移原地修改数据且不可回滚，请先按[备份与恢复](docs/backup-restore.md)创建并验证备份，再加 `-e SBM_ALLOW_MIGRATION=true` 重建。

加固参数、使用已有 PostgreSQL、卷内布局和日常运维见[部署指南](docs/deployment.md)。

## 数据库与持久化

应用容器的所有持久数据都在 `/var/lib/sbm` 下，挂一个卷即可：

```text
/var/lib/sbm/
├── objects/    # 上传的图片和 PDF        （sbm:sbm 0700）
├── secrets/    # 主密钥，由入口脚本管理  （root:sbm 0710，应用只能穿越）
└── config/     # 数据库连接与密码        （sbm:sbm 0700，初始化页写入）
```

PostgreSQL 数据在它自己的容器卷里。首次启动时若未挂载主密钥，入口脚本会在 `secrets/` 内生成一份并在日志中提示——**它和数据在同一个卷里，必须单独复制一份到别处保管**，丢失后已保存的 Provider API Key 无法恢复。

备份必须同时覆盖数据库、对象文件和主密钥，且按[备份与恢复说明](docs/backup-restore.md)生成认证备份包。把卷或数据目录整个复制不构成可恢复的备份。

Clean Slate 只表示不读取旧架构和 SQLite 数据。从当前新架构开始，后续版本默认保留数据，并通过版本化 PostgreSQL Schema migration 升级数据库结构；不会要求用户每次更新都清空数据库。

## 主要能力

- 图片和 PDF 上传、批量逐项反馈与多页审核；
- 最小中文多模态提取、本地确定性规范化和字段级校验；
- 失败转人工审核、正式单据检索与可追溯纠错，不绕过 Source → Claim → Fact；
- 手工创建行程、组合多张机票与其他单据、自动归属及辅助材料管理；
- 重复候选、支付—发票分配、默认 30 天推荐与说明原因后的跨期手工分配；
- 坏账标记/解除及相关行程删除保护，不自动核销金额；
- 邮件附件本地归档、报销状态和材料 ZIP 导出；
- 成员邀请、角色管理、自助改密与本地账号恢复；
- 确定性数据洞察、租户隔离、审计、认证备份与完整恢复。

## 安全与数据边界

```text
Source -> Claim -> Fact
原始证据 -> 可审核候选（AI 或显式人工来源）-> 用户确认的数据
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
| [成员与账号](docs/member-accounts.md) | 成员邀请、停用、自助改密与本地恢复说明 |
| [材料包导出](docs/material-export.md) | 当前行程与固定报销快照 ZIP、文件范围和资源限制 |
| [备份与恢复](docs/backup-restore.md) | 认证备份、验证和完整恢复 |
| [产品与范围](docs/product.md) / [路线图](docs/roadmap.md) | 产品定位、已完成范围和后续门禁 |
| [架构](docs/architecture.md) / [数据模型](docs/data-model.md) | Source、Claim、Fact 与 PostgreSQL 设计 |
| [AI 流水线](docs/ai-pipeline.md) | 模型契约、规范化、校验和审核 |
| [验收标准](docs/acceptance.md) / [M4 证据](docs/m4-evidence.md) | 本地质量门禁和安全聚合证据 |

旧 `backend-go/`、`frontend/`、根 Dockerfile 和根 Compose 只保留为历史参考，不是当前运行入口。旧版本请使用对应历史 Release。

## 安全与许可

安全问题请按 [SECURITY.md](SECURITY.md) 私下报告，不要在公开 Issue 中披露。项目采用 [MIT License](LICENSE)。
