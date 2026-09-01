# 智能账单管理系统（Smart Bill Manager）

> **English:** [README_EN.md](README_EN.md)

> [!IMPORTANT]
> M0～M4 本地功能与本地发布准备已完成；PostgreSQL 17 是唯一关系数据源。新系统不兼容 v0.x，也不读取或迁移旧数据。真实模型正式评测、真实外部联调和生产发布仍待单独授权。

Smart Bill Manager 是面向个人与小团队的自托管 AI 财务单据工作台。用户上传支付截图或发票后，系统生成可验证的候选字段和证据；只有用户明确确认，候选结果才能成为正式财务事实。

## 核心不变量

```text
Source -> Claim -> Fact
原始证据 -> AI 候选判断 -> 用户确认的正式数据
```

- 原始 Source 不可被新上传覆盖。
- AI 只能生成 Claim，不能直接创建或修改 Fact。
- JSON Schema、本地业务规则、租户权限和人工审核缺一不可。
- 第一阶段只有一个 OpenAI-compatible Chat Completions 传输实现，不做供应商分支、多模型路由或自动降级。
- 新系统不依赖 `backend-go/`、`frontend/`、旧 OCR、旧数据库或旧 Compose。
- 软件测试使用版本化纯合成数据；模型业务质量只使用受保护的中文真实场景评测资产，不建立生产运行时回归样本模块。

## 当前阶段

M0 已于 2026-08-27 完成并冻结以下可执行设计基线：

- 产品、范围、验收、架构、AI、数据模型和 UI/UX；
- 已批准量化指标及测量协议；
- 02「国内大厂中后台」视觉方向和四个代表页面；
- 独立工作区、基线 SHA、范围 diff、响应式和可访问性证据；
- 独立只读复审。

独立只读复审已清零 M0 阻断、重大和次要问题，完成证据见 [M0 验收证据](docs/m0-evidence.md)。[M1 AI 收件箱首条链路](docs/scope.md) 已于 2026-08-30 完成；M2 与 M3 于 2026-08-31 完成，证据见 [M1](docs/m1-evidence.md)、[M2](docs/m2-evidence.md) 与 [M3](docs/m3-evidence.md)。M4 于 2026-09-01 完成 PostgreSQL 17 唯一持久化、确定性 Fact 洞察、1,000 Document 认证恢复、运行质量和本地发布准备，证据见 [M4 验收证据](docs/m4-evidence.md)。真实模型正式评测、真实邮箱/Provider 联调、部署和发布仍在单独门禁外。

## 权威文档

| 文档                                    | 作用                                   |
| --------------------------------------- | -------------------------------------- |
| [产品基线](docs/product.md)             | 产品定位、用户和首条旅程               |
| [范围与非目标](docs/scope.md)           | Clean Slate 与里程碑边界               |
| [验收标准](docs/acceptance.md)          | 量化门槛、测量协议和失败判定           |
| [架构基线](docs/architecture.md)        | 模块边界、依赖方向和状态机             |
| [AI 流水线](docs/ai-pipeline.md)        | Provider、Schema、校验、审核和失败规则 |
| [数据模型](docs/data-model.md)          | Source、Claim、Fact、追溯和删除规则    |
| [UI/UX 基线](docs/ui-ux.md)             | 唯一视觉方向、四页结构和可访问性       |
| [M0 证据](docs/m0-evidence.md)          | 工作区、响应式、WCAG、链接和复审记录   |
| [M1 证据](docs/m1-evidence.md)          | 已执行门禁、真实诊断和当前阶段决定     |
| [M2 证据](docs/m2-evidence.md)          | 五个切片实现、不变量和自动化验收       |
| [M3 证据](docs/m3-evidence.md)          | 邮箱归档切片实现、不变量和自动化验收   |
| [M4 证据](docs/m4-evidence.md)          | 洞察、恢复与本地发布准备证据           |
| [备份与恢复](docs/backup-restore.md)    | 认证快照、验证、恢复和保留策略         |
| [本地运维](docs/local-operations.md)    | 构建、启动、诊断、升级和回滚           |
| [路线图](docs/roadmap.md)               | 里程碑和进入下一阶段的门禁             |

关键决策包括：

- [ADR-0001：采用 Clean Slate 重构](docs/decisions/0001-clean-slate.md)
- [ADR-0002：采用 Source、Claim、Fact 数据边界](docs/decisions/0002-source-claim-fact.md)
- [ADR-0003：第一阶段使用唯一 OpenAI-compatible Adapter](docs/decisions/0003-openai-compatible.md)
- [ADR-0004：分离 Provider 生成 Schema 与本地权威 Schema](docs/decisions/0004-provider-schema-projection.md)
- [ADR-0009：支付—发票金额分配](docs/decisions/0009-payment-invoice-allocation.md)
- [ADR-0010：确定性重复检测](docs/decisions/0010-deterministic-duplicate-detection.md)
- [ADR-0011：跨页发票审核](docs/decisions/0011-cross-page-invoice-review.md)
- [ADR-0012：客户端编排批量上传](docs/decisions/0012-client-orchestrated-batch-upload.md)
- [ADR-0013：已确认 Fact 分配调整](docs/decisions/0013-confirmed-fact-allocation-adjustment.md)
- [ADR-0014：连接器中立的邮箱 Source 与不可变邮件归档](docs/decisions/0014-connector-neutral-email-archive.md)
- [ADR-0015：Trip 与 Fact 确定性归属](docs/decisions/0015-trip-fact-attribution.md)
- [ADR-0016：报销工作流与确定性政策提示](docs/decisions/0016-reimbursement-workflow-policy-findings.md)
- [ADR-0017：确定性 Fact 洞察与查询](docs/decisions/0017-deterministic-fact-insights-and-query.md)
- [ADR-0018：认证的停机备份与完整恢复](docs/decisions/0018-authenticated-offline-backup-and-recovery.md)
- [ADR-0019：本地发布候选与运行质量门禁](docs/decisions/0019-local-release-candidate-and-runtime-quality.md)
- [ADR-0020：PostgreSQL 唯一持久化](docs/decisions/0020-postgresql-only-persistence.md)

## 目标目录

Clean Slate 新实现位于以下目录：

```text
apps/api/
apps/web/
contracts/
infra/
tests/
tools/
```

仓库中的 `backend-go/`、`frontend/` 和旧部署文件仅是遗留参考，在 M0 保持不变，也不构成新系统的运行或验收入口。需要旧版本时请使用历史 Release；新系统不提供升级、导入或兼容承诺。

## 安全

安全问题请按 [SECURITY.md](SECURITY.md) 私下报告，不要在公开 Issue 中披露。

## License

MIT License，详见 [LICENSE](LICENSE)。
