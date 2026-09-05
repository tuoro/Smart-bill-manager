# ADR-0025：失败单据显式转人工审核

状态：已接受，B1 已通过本地验收
日期：2026-09-04

## 边界

用户授权持续实施 B1～B6。本 ADR 只处理合法 Document 的 failed Job 在尚无 Claim 时转人工；不读取旧数据，不调用 Provider，不调整 30 天分配限制、坏账或邮箱规则。无原件记账、处理中抢占、取消后重开和确认后纠错不在 B1。

## 决策

- 新增 `POST /api/v1/jobs/{job_id}/manual-review`，要求 `claims.review`、会话/CSRF、Idempotency-Key、expected_job_version、payment/invoice/trip 类型及 1～500 字的人工接管理由。成功只创建全部业务字段 absent 的 blocked Claim，不创建 Fact。
- 在 PostgreSQL 事务中锁定 Job，验证 failed、期望版本且无 Claim。完成或核实全部规范化页后，原子保存初始人工 Claim、用户来源字段、Validation、审计与 Job/Document blocked 状态。与重试、删除或另一次接管竞争的输方明确冲突，不留下部分审核。
- 删除预案记录 Job 版本，提交删除时获取同一 Job/Document 锁并核对版本；若人工接管已发布，删除恢复暂存文件并返回冲突，不能拿旧对象清单删除新页面和审核链。规范化补偿失败必须返回本次对象清单及清理错误，供调用方核实持久化引用后重试，不吞掉遗留状态。
- 已有规范化页复用；没有页面时用既有本地 Normalizer 准备完整页面，不能运行整个 Worker。原件及派生页必须可读取且哈希一致；失败时不创建人工 Claim。磁盘与事务失败的补偿只触及本次新建且未被持久化引用的对象，不清除共享文件。
- 新增前向迁移 `0003_explicit_manual_review.sql`，允许初始人工 Claim 的 origin_ai_run_id 为 NULL；其 revised_by_user_id 必填，revision=1、supersedes=NULL，并保存理由、幂等键和请求 hash。AI 初始 Claim 仍必须引用真实 AiRun；后续 revision 继承原始来源，不能在 AI/人工间换源。
- 相同租户幂等键和相同请求返回相同初始 Claim 身份；换 Job、类型、版本或理由后复用键明确失败。幂等信息只保存在初始人工 Claim，不另建第二任务或第二事实源。
- 审核响应增加只读 entry_mode=ai|manual。人工输入仍经过现有 Revision、ValidateClaim、重复检测、分配决定和 Confirm；人工模式不降低金额、日期、类型、页序或确认门槛。
- 人工模式允许用户为 present 字段显式标注原件页码和摘录，作为该用户提交的 Evidence，不自动把字段值复制成 quote。已有证据仍可选择；普通 AI Claim 的证据规则不被全局豁免。页码必须属于同一 Document，摘录必须非空且至多 500 字，单字段已有和新增证据合计最多 8 条，沿用公共校验边界。缺失字段不得携带证据。
- 初始空白快照不伪造默认金额、币种、日期或票面摘录；用户来源与既有模型来源明确区分。原失败 AiRun、错误和 attempt_count 保留；接管不会被统计为 AI 识别成功。
- 页面从失败项进入类型/理由确认，再跳到既有审核页填写；人工输入可保存为受校验的版本，刷新可继续。首次尚未保存的编辑失败保持当前草稿，不承诺浏览器关闭后保存未提交内容。
- 修订冲突后的页面内刷新保留字段、页码及摘录，展示最新服务器内容并要求再次核对后才能保存；同内容 Evidence 更新其版本 ID，失效 Evidence 保留为可见错误，必须显式重选。浏览器刷新或离开可能丢失未保存输入，页面明确提示。

## 验收

1. Payment、Invoice、TripEvidence 三种人工空快照和完整填写确认通过；无 Provider 配置也可走通，不伪造 AiRun，Trip 不创建费用容器。
2. 普通 AI 修订的证据要求保持；人工证据的空摘录、无效页码、跨文档或跨租户引用、缺失字段载荷均拒绝。
3. 重试/接管、双接管、删除/接管、迟到 Worker、相同及冲突幂等请求、陈旧版本和事务回滚均有测试。
4. 原件/页面缺失、哈希不符、规范化失败不产生伪成功，未持久化的新派生文件按精确目标回收。
5. PostgreSQL 前向迁移保留既有 AI Claim 和行程数据；备份/恢复包含人工来源及历史。领域、数据库、HTTP/OpenAPI、Web、相关浏览器与适用质量门禁通过后才收口。

此 ADR 只将“正式字段必须拥有成功 AI 来源”收敛为“必须拥有真实的 AI 或人工来源，以及原件、Claim 和 Review”；不改变模型输出契约，也不允许跳过人工确认。

本地验收结果：508 项 Go 测试、161 项关键不变量、40 项 Web 单元与 25 项浏览器场景通过，前向迁移和真实工具备份恢复通过。范围与限制见 `tests/evidence/business-completion/b1-manual-review.json`；未执行发布、用户实例升级或真实模型评测。
