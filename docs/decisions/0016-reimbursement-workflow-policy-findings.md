# ADR-0016：报销快照、状态历史与确定性政策提示

状态：已接受，已实施并验收
日期：2026-08-31

## 背景

M3 最后一个切片需要记录报销状态，并提示缺少发票、金额冲突和重复报销。旧实现把二值状态直接写在 Trip 上，既不能说明一次报销包含哪些 Fact，也不能保留提交时的规则输入、提示和后续状态修正历史；它不作为新架构兼容目标。

Reimbursement 也不是新的单据识别 Fact。它来自用户对既有 confirmed Payment/Invoice 的显式工作流决定；若让模型创建、从邮件正文推断或绕过 Trip 归属直接写入，会破坏 `Source -> Claim -> Fact`、人工审核和可追溯边界。

## 决策

### Reimbursement 是 confirmed Fact 上的独立工作流快照

1. 用户只能从一个未删除 Trip 当前活动的 TripFactAssignment 中显式选择 1～200 项；每项必须严格属于该 Trip、仍活动且对应未删除 Payment/Invoice，同一 Assignment 不得重复。
2. 选择可以同时包含 Payment 与 Invoice，也允许只包含其中一种。Reimbursement 不修改 TripFactAssignment、PaymentInvoiceLink、Fact 字段、金额余额、Claim 或 ReviewDecision。
3. 提交时冻结 Trip 摘要、每项 Assignment/Fact 身份、显示摘要、业务日期、整数最小单位金额和币种。Payment 业务日期只读取确认时持久化的 `business_date`，不能在报销链路再次推导；该快照只用于解释历史报销，不成为当前 Fact 的第二可写来源。
4. 混合币种允许进入同一快照，但只按币种分别汇总，禁止换汇、跨币种相加或隐藏默认币种。
5. 不存在模型、邮件正文、手工文本或旧数据库到 Reimbursement 的自动创建入口。预检本身不落库；只有用户完成完整提示确认后才能原子创建记录、Item、Finding、Decision 和 AuditEvent。

### `reimbursement-policy/1` 是唯一确定性提示规则

预检只读取当前选择、活动 PaymentInvoiceLink 和其他 Reimbursement 的当前状态：

- `missing_invoice`：选中的 Payment 与任何选中 Invoice 都没有活动 PaymentInvoiceLink。即使它与未选 Invoice 有 Link，也仍表示本次报销选择缺少发票支持；
- `amount_conflict`：一个选中 Payment/Invoice 与至少一个选中相反类型 Fact 存在活动 Link，但这些选中 Link 的分配金额合计不等于该 Fact 总额；Payment 与 Invoice 各自独立判断；
- `duplicate_reimbursement`：同一选中 Fact 已出现在另一个当前状态为 `submitted` 或 `reimbursed` 的 Reimbursement 中。`rejected` 不构成重复提示。

规则不读取商户/购销方模糊相似、邮件正文、模型输出、外部报销系统、税务政策或汇率。提示不是自动拒绝或合规结论；用户仍可提交，但必须确认当前全部 Finding。

每个 Finding 有由规则版本、代码、相关 Fact、适用于该 Fact 的完整活动 Link 身份与金额、相关 Reimbursement 身份和状态确定性计算的稳定 key；即使 Link 金额相同，Link 被替换也必须改变 key。预检返回：

- 规则版本、规范选择和按币种汇总；
- 当前完整 Finding 集；
- 覆盖 Trip、Assignment、Fact、活动 Link、相关报销状态和规则版本的 `snapshot_hash`。

提交必须回传同一选择、`expected_snapshot_hash` 和与当前 Finding key 集完全相等的 `acknowledged_finding_keys`。服务端在 immediate 事务内重新计算；集合或输入变化返回陈旧冲突，禁止自动补确认、静默忽略或部分提交。Finding 超过 1,000 条时明确失败，不截断。

### 状态只通过不可变 Decision 推进

Reimbursement 当前状态固定为：

```text
submitted -> reimbursed
          -> rejected
reimbursed -> submitted（重新打开）
rejected   -> submitted（重新打开）
```

同一 Trip 同时最多一个 `submitted` Reimbursement；终态后可以对新的显式选择创建下一条记录。重新打开只重新处理原不可变快照，不吸收当前新增 Assignment；需要改变内容时必须创建新记录。

创建与状态变化都要求 8～128 字符幂等键和 1～500 字符理由。状态变化还提交期望当前状态与版本；目标与当前相同、非法跨越、陈旧版本或同 Trip 已有另一条 submitted 记录均零写入。相同键和相同规范请求返回原结果，同键改变任一输入明确冲突。

Reimbursement、Item、Finding 和 Decision 禁止删除；Item/Finding/Decision 创建字段不可更新。非 submit Decision 插入后由数据库触发器在同一语句内唯一推进匹配的 `status + version + updated_at`，不能留下已写 Decision 但未更新状态的孤立中间态；无匹配 Decision 的直接状态更新继续失败。删除 Trip、Payment 或 Invoice 继续使用既有软删除语义，不删除报销快照；详情通过快照继续可解释，并明确显示来源已删除。

### API、权限与 Web

- `POST /api/v1/reimbursement-previews`：严格 JSON/CSRF 的只计算预检；不创建业务记录；
- `POST /api/v1/reimbursements`：提交选择、快照、完整 Finding 确认和理由，严格幂等创建；
- `GET /api/v1/reimbursements`：按创建时间与 ID 稳定游标分页，默认 50、最大 100，不静默截断；
- `GET /api/v1/reimbursements/{reimbursement_id}`：读取不可变 Item/Finding 与完整状态 Decision 历史；
- `POST /api/v1/reimbursements/{reimbursement_id}/status-decisions`：按期望状态/版本执行状态变化。

新增 `reimbursements.read`，授予 Owner/Finance/Viewer；新增 `reimbursements.manage`，只授予 Owner/Finance。Reviewer 不能枚举、预检或管理 Reimbursement；跨租户与不存在资源统一不泄露。

Web 使用独立“报销”工作区：先选择 Trip，再从其当前已归属 Fact 中显式勾选，显示每币种汇总与逐条中文提示；有 Finding 时必须勾选“已阅读并确认当前全部提示”才能提交。列表与详情展示快照、状态、提示和历史；Owner/Finance 可提交和变更状态，Viewer 只读，Reviewer 显示权限不足。陈旧快照或状态冲突保留选择、确认和理由，要求刷新后重新确认。

## 失败、审计与隐私边界

- 公开预检与详情中的多条 SQL 必须在同一 SQLite 读快照内完成，禁止拼接不同提交点的 Trip、Assignment、Link、状态或 Decision；提交仍在 immediate 写事务内重算。
- 预检或提交中的非法/重复/超过上限 Assignment、已移动/终止 Assignment、已删除 Fact/Trip、Finding 集不完整、快照陈旧、并发提交和状态竞争均明确失败；事务失败时 Reimbursement、Item、Finding、Decision、AuditEvent 和当前状态全部无部分写入。
- 安全 AuditEvent 只记录动作、前后状态以及 Item/Finding 数量；不得记录地点、日期、名称、金额、币种、Fact ID、理由、Finding 明细或其他财务字段。
- API 不返回 Source 文件名、Evidence、Claim 原文、Provider 响应、邮件正文、存储键或凭据；测试仅使用纯合成身份和金额。

## 非目标

- 企业审批链、审批人分配、组织层级、费用科目、预算、税务合规、发票验真或可报销性判断；
- 外部报销单号、打款、银行、会计、ERP、邮箱或云服务同步；
- 汇率换算、报销金额手工改写、部分 Item 金额、附件导出、PDF 报销单或批量状态操作；
- 模型生成提示、模糊名称政策、自动提交/批准、真实 Provider/邮箱联调或正式正确率评测；
- 旧 Trip 二值报销字段、旧 API、旧数据库或旧状态兼容。

## 验证要求

- 领域测试覆盖选择规范化、三类 Finding、稳定 key/hash、混合币种分组、状态图和边界上限；
- SQLite/应用集成覆盖预检重算、完整确认、严格幂等、同 Trip 并发、状态并发、重新打开、不可变历史、软删除后解释和事务故障回滚；
- HTTP/OpenAPI 覆盖严格 JSON、CSRF、ID/游标/上限、四角色、跨租户和安全错误；
- Web 覆盖 Trip/Assignment 选择、无项目、无提示、三类提示、确认门禁、提交/重放、状态更新、冲突保留、只读、加载/失败/离线、键盘、768px 与 384px 回流；
- 完整门禁继续包含后端测试/vet/build、生成客户端、Web check、浏览器场景、关键不变量、两层覆盖率、diff、敏感信息、大文件、临时产物与进程残留审查。
