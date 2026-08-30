# ADR-0013：以完整期望计划调整已确认 Fact 的分配关系

状态：已接受
日期：2026-08-31

## 背景

M2 首切片允许用户在确认新 Payment 或 Invoice 时创建一对多、多对一和部分金额的不可变 PaymentInvoiceLink。已确认 Fact 随后可能需要补充一条分配、撤销现有分配，或把金额和目标替换为新的人工决定。现有 Link 不能原地改金额或关系，也不能物理删除；Fact 的余额仍必须只从活动 Link 聚合。

如果 API 分别暴露“新增、修改、删除 Link”三个低层命令，客户端需要自行拼接并发快照，部分成功会留下用户没有确认过的中间计划。若把 Link 改成可变记录，又会丢失审核来源和历史金额。第五切片因此需要一个原子、幂等且可审计的独立调整边界。

## 决策

### 完整期望计划

1. 用户以一个已确认且未删除的 Payment 或 Invoice 作为 anchor。服务端返回 anchor、当前活动 Link、最多 200 个满足硬条件的相反类型目标，以及当前 `plan_hash`。
2. 调整请求提交该 anchor 的**完整期望活动分配计划**，每项只包含唯一 `target_fact_id` 与正整数 `allocated_minor`。请求不是增量 patch；当前 Link 未出现在期望计划中即表示撤销。
3. 当前计划为空且期望计划非空，或只在不改变现有项的前提下增加目标时，结果模式为 `supplement`；只移除现有项且保留项金额不变时为 `withdraw`；修改任一金额、同时增删目标或替换目标时为 `replace`。完全相同的计划以 `allocation_plan_unchanged` 拒绝，不写无意义历史。
4. 空期望计划是合法的“撤销全部”，但仍必须提交非空理由。Web 在存在当前 Link 时还要求用户显式勾选“确认撤销全部”；该控件只防误操作，服务端的完整快照、理由和权限检查才是权威边界。
5. 计划最多 200 项。读取目标超过 200 个或请求超过 200 项时明确失败，不静默截断、分页拼接或自动选择。

### 快照、幂等与并发

1. `payment-invoice-allocation-plan/1` 对 anchor 类型与 ID，以及按 Link ID 排序的活动 `{link_id,payment_id,invoice_id,allocated_minor,currency}` 封闭 JSON 计算 SHA-256。GET 返回的 `plan_hash` 是乐观并发令牌，不是可写业务事实。
2. POST 必须携带 `expected_plan_hash`、8–128 字符且不含空白或控制字符的 `Idempotency-Key`，以及 trim 后 1–500 字符的理由。
3. `allocation-adjustment-request/1` 对 anchor、期望 hash、按目标 ID 排序的完整期望计划和 trim 后理由计算请求 SHA-256。`tenant_id + idempotency_key` 永久唯一；相同键和相同请求重放返回相同 adjustment、Link ID 集与结果 hash，同键改变任一输入返回 `idempotency_key_conflict`。
4. 事务首先检查幂等重放，再重新读取活动 Link 并比较 `expected_plan_hash`。这样响应丢失后的同请求可以重放，而首次提交的陈旧快照必须以 `allocation_plan_stale` 整体失败。
5. SQLite immediate 写事务内再次校验 anchor、目标与余额。并发占用最后余额、Fact 删除或其他调整只能有一个事务成功；失败事务不得留下 Adjustment、Link、AuditEvent 或部分终止状态。

### 资格与余额规则

- anchor 和目标必须同租户、已由 ReviewDecision 确认、未删除且类型相反；资源是否存在不能跨租户泄露。
- Payment 与 Invoice 币种必须相同，Payment 交易业务日期与 Invoice 开票日期相差不超过 30 个日历日。名称只按既有 NFKC、trim、空白折叠和拉丁大小写折叠规则影响排序与警告，不放宽硬条件。
- 每项金额必须在 `1..9,007,199,254,740,991`，同一目标不能重复。anchor 的期望活动合计不得超过自身金额；每个目标的期望金额不得超过“目标当前剩余金额 + 该 anchor 与目标当前 Link 金额”。
- 同一 Payment/Invoice 对同时最多一条活动 Link。跨币种、超额、目标不可用、日期超界或重复目标都整体失败；服务端不得自动缩减、换算、接受或创建候选。
- 余额继续只由活动 `payment_invoice_links` 聚合。Adjustment、AuditEvent、API 响应和 Web 草稿都不能成为第二份可写余额。

### 不可变历史与来源

1. 增加 `payment_invoice_allocation_adjustments`，保存租户、actor、anchor、派生模式、幂等键、请求 hash、前后计划 hash、理由、AuditEvent 和创建时间。记录不可更新、不可删除。
2. `payment_invoice_links` 的创建来源改为严格二选一：初次审核 Link 引用 `payment_invoice_link_decision`；独立调整创建的 Link 引用 `payment_invoice_allocation_adjustment`。数据库触发器验证调整 anchor 必须是 Link 的一端。
3. Link 的终止来源也严格二选一：Fact 删除引用 `ended_by_audit_event_id`；独立调整引用 `ended_by_adjustment_id`。一个活动 Link 只能终止一次。
4. 未变化的 Link 保持原 ID 与来源；撤销或金额变化的旧 Link 只写终止时间与 Adjustment；新增或替换项创建新的不可变 Link。任何路径都不得原地修改金额、币种、双方 Fact 或创建来源。
5. 每次成功调整写一条 `payment_invoice_allocation_adjusted` AuditEvent。安全 metadata 只保存模式及创建/终止数量，不写金额、名称、理由、完整计划或其他财务字段；业务理由只保存在受租户隔离的 Adjustment 表。

### API、权限与 Web

- `GET /api/v1/allocations/{fact_type}/{fact_id}` 返回调整工作区；`fact_type` 只允许 `payment` 或 `invoice`。
- `POST /api/v1/allocations/{fact_type}/{fact_id}/adjustments` 原子提交完整期望计划，并返回 `adjustment_id`、派生 `mode`、`ended_link_ids`、`created_link_ids`、新 `plan_hash` 与 `replayed`。
- 新 capability `allocations.manage` 只授予 `owner` 和 `finance`。`reviewer`、`viewer` 均不能读取调整工作区或提交调整；既有 `facts.read` 仍只允许读取 Fact 列表和派生余额。
- Payment/Invoice 列表只对具备 capability 的会话显示“调整分配”。独立路由 `/allocations/:factType/:factId` 使用页面内表单，不使用对话框；展示 anchor、当前 Link、全部合格目标、完整期望合计和剩余金额。
- UI 允许勾选目标并编辑金额，要求理由，明确说明取消勾选会撤销旧 Link。错误绑定具体输入或区域；提交成功后重新读取服务端工作区。键盘、768px 和等效 200% 缩放均不得丢失目标、理由、撤销全部确认或提交动作。

## 取舍

- 完整期望计划比三个低层命令多传少量数据，但把“用户确认的最终状态”收敛为一个原子事务，并让并发冲突可验证。
- `plan_hash` 包含 Link ID，因此相同金额经撤销再建立仍是新快照；这正好保留历史身份，避免 ABA 问题。
- 只返回最多 200 个合格目标，不在本切片增加搜索或分页状态。超过上限明确阻断，后续若需要扩容必须先重新设计可重复读分页和完整计划提交协议。
- 独立调整不修改 Fact 字段和 Source/Claim/ReviewDecision；它只由具有财务权限的用户对已确认 Fact 之间的 Link 作新的人工决定。

## 非目标

- 自动匹配、自动接受、规则引擎代用户调整或模型创建 Link；
- 跨币种分配、汇率换算、负数冲销、退款或会计分录；
- 修改 Fact 金额、日期、名称或来源链；
- 恢复已删除 Fact、物理删除 Link 历史、旧数据迁移或旧 API 兼容；
- M3 邮箱/行程/报销、真实 Provider 调用或模型正确率评测。

## 验证要求

- 领域测试覆盖规范计划、hash、模式派生、补充、单项/全部撤销、同对改金额、换目标、完全相同计划、重复目标、非法金额和 200 项边界。
- SQLite 集成测试覆盖来源二选一、不可变 Link/Adjustment、租户、币种、日期、余额、活动对唯一、Fact 删除竞态、陈旧 hash、幂等重放、同键变更和并发最后余额。
- HTTP/OpenAPI 测试覆盖严格 JSON、路径类型、权限矩阵、安全错误与空数组；Web 测试覆盖完整计划提交、撤销全部确认、刷新冲突、可访问错误、键盘和响应式布局。
- 完整门禁必须包含后端测试/vet/build、OpenAPI 客户端生成、Web check、浏览器场景、关键不变量、覆盖率、敏感信息与临时产物审查，并写入 M2 可机读证据。
