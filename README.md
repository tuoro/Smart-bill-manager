# Smart Bill Manager

> **English:** [README_EN.md](README_EN.md)

Smart Bill Manager 是面向个人和小团队的自托管 AI 财务单据工作台。它把支付截图、发票和行程资料整理成可追溯候选；只有用户明确审核确认后，候选才会成为正式财务事实。

> [!IMPORTANT]
> `v0.3.1` 是 Clean Slate 公开实测预发布版，目前只提供 `linux/amd64` 单机部署。真实模型正确率、真实邮箱联调、TLS/域名和生产部署尚未完成，不应视为生产稳定版。

## 快速部署

需要 Git、Docker Engine、Docker Compose 2.24.4 或更新版本，以及至少 6 GiB 可用内存。

```bash
git clone https://github.com/tuoro/Smart-bill-manager.git
cd Smart-bill-manager
git checkout v0.3.1

mkdir -p ../sbm-runtime-parent
runtime_directory="$(realpath ../sbm-runtime-parent)/deployment"
./tools/prepare-self-hosted-deployment.sh "$runtime_directory"
./tools/sbm-deploy.sh "$runtime_directory" pull
```

从 `$runtime_directory/owner-password` 记录一次性 Owner 密码，然后初始化并启动：

```bash
./tools/sbm-deploy.sh "$runtime_directory" bootstrap \
  owner@example.invalid "Owner" "My Workspace" CNY Asia/Shanghai
./tools/sbm-deploy.sh "$runtime_directory" start
```

打开 <http://127.0.0.1:8080> 登录。完整前置条件、安全边界、日常命令和备份说明见 [部署指南](docs/deployment.md)。

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
- 不提供旧版本升级、导入或兼容承诺。

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
