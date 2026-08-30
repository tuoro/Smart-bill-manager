# ADR-0015：行程 Fact 与确定性单据归属

状态：已接受，已实施
日期：2026-08-31

## 背景

M3 第二切片需要形成正式 Trip，并让已确认的 Payment 与 Invoice 归属于行程。Trip 如果由独立表单直接创建，会绕过 `Source -> Claim -> Fact` 与字段证据；如果由模型直接判断归属，又会把可变业务关系交给不可审计的输出。另一方面，支付与发票可能晚于行程单据进入系统，归属也会发生补充、移动或撤销，因此不能把归属写成 Trip、Payment 或 Invoice 的可变外键。

## 决策

### Trip 继续走唯一 Source、Claim、Review、Fact 链

1. 活动模型契约整体升级为 `bill-visible-text-cn/2 -> bill-visible-text/2 -> bill-visible-text-provider/2 -> claim-mapper/4 -> document-claim/3`。旧版本停止活动，不保留兼容解析、双写或回填。
2. 根对象增加固定 `trip` 成员，`document_type` 增加 `trip`。Payment、Invoice、Trip 三个业务区段严格三选一；`unknown` 时三个区段都为 `null`。
3. Trip 区段只抄 `origin`、`destination`、`start_date`、`end_date`、`traveler_name`、`transport_type` 与 `booking_reference` 的票面 `{text,page}`；没有看到或不能确定时返回 `null`。模型不得生成行程标题、计算日期、推断归属或返回内部 ID。
4. `claim-mapper/4` 只做 NFKC/首尾空白和批准日期表示法的确定性规范化，并把同一原文绑定为 Evidence。`destination`、`start_date`、`end_date` 为 Trip 必填字段；结束日期早于开始日期必须 blocked。其余字段可缺失，不得从正文、邮件头、支付或发票反推。
5. Trip Fact 只能由通过校验的 Trip Claim 经用户确认创建；ReviewDecision、FactFieldOrigin 和 AuditEvent 与 Payment/Invoice 使用同一事务边界。Trip 确认不提交 Payment/Invoice 分配计划，但仍必须解决当前 Claim 的全部疑似重复候选。
6. 原始文件视觉近似和跨页重复检测继续适用于 Trip；本切片不增加 Trip 字段组合重复规则，不自动合并、覆盖或删除 Source。

### 归属是独立、可追溯且可逆的人工决定

1. 每个未删除 Payment 或 Invoice 同时最多存在一个活动 Trip 归属；一个 Trip 可以拥有任意多个 Payment/Invoice。归属不改变 PaymentInvoiceLink、金额余额或 Fact 字段。
2. `trip_fact_assignments` 保存不可变的 Trip、Fact、创建决定与时间；只允许一次性追加 `ended_at + ended_by_decision_id | ended_by_audit_event_id`。活动 Fact 唯一由局部唯一索引保证，历史 Link 禁止更新或删除。
3. `trip_fact_assignment_decisions` 保存 actor、Fact、上一活动 Link、目标 Trip、派生动作 `assign | move | unassign`、幂等请求 hash、必填理由和 AuditEvent，记录不可更新或删除。
4. 写请求提交 `fact_type`、`fact_id`、可空 `desired_trip_id`、可空 `expected_assignment_id` 和 1～500 字符理由。期望 ID 必须与事务内活动 Link 精确一致；无 Link 到目标 Trip 是 assign，有 Link 到其他 Trip 是 move，有 Link 到空目标是 unassign。空到空、同 Trip 重提、陈旧期望或跨租户资源都明确失败且零写入。
5. `tenant_id + Idempotency-Key` 永久唯一；相同键与相同规范请求返回同一决定和结果，同键改变 Fact、目标、期望 Link 或理由必须冲突。数据库使用 immediate 事务串行化同一 Fact 的竞争请求。
6. 删除 Payment/Invoice/Trip 时，删除 AuditEvent 在同一事务终止相关活动归属；Fact 保持软删除，历史决定与 Link 保留。删除不能形成孤立活动 Link。

### 建议只来自版本化确定性规则

`trip-attribution/1` 只读取已确认、未删除 Fact：

- Payment 的业务日期是 `transaction_time` 在 `source_timezone` 下的本地日期；Invoice 使用 `invoice_date`；
- 日期落在 Trip 闭区间内产生 `date_inside_trip`；距开始日前或结束日后不超过 3 个日历日分别产生 `date_within_3_days_before` 或 `date_within_3_days_after`；
- 与该 Fact 存在活动 PaymentInvoiceLink 的另一端已归属于当前 Trip 时产生 `linked_fact_assigned_to_trip`；
- 当前已经归属于所选 Trip 时产生 `currently_assigned`。

命中任一前三项即为 suggested。规则不使用模型、名称模糊匹配、邮件正文、金额阈值或隐藏默认值；未命中仍允许用户在“全部”视图中手工归属。建议从不自动写 Link，也不阻止范围外日期的显式决定。

### API、权限与 Web

- `GET /api/v1/trips` 列出当前租户未删除 Trip 和活动 Payment/Invoice 归属计数；`DELETE /api/v1/trips/{trip_id}` 复用 Fact 删除边界。
- `GET /api/v1/trips/{trip_id}/attribution-candidates` 支持 `view=all|suggested|assigned`、默认 50/最大 100 的不透明游标分页。静态数据下排序固定为当前 Trip 已归属、建议、其他，随后业务日期降序、Fact 类型和 ID 升序；不静默截断。
- `POST /api/v1/trip-assignments` 执行单个 Fact 的 assign/move/unassign；严格 JSON、CSRF、幂等键、期望 Link 和事务内重检同时成立。
- `facts.read` 可读取 Trip 与归属候选，授予 Owner/Finance/Viewer；Reviewer 仍只能在当前审核边界看到 Trip Claim。新增 `trip_assignments.manage` 只授予 Owner/Finance；删除仍只允许 Owner。
- Web 增加行程列表与选中行程的归属工作区。每行显示 Fact 摘要、业务日期、当前行程、建议原因和具名 assign/move/unassign 动作；理由有可见标签与错误关系。并发冲突保留输入并要求刷新，不自动覆盖。

## 失败、审计与隐私边界

- 日期非法、结束早于开始、业务区段冲突、字段证据缺失继续形成 blocked Claim；根 JSON/身份失败才不创建 Claim。
- 归属写入任一步失败时 Decision、AuditEvent、旧 Link 终止和新 Link 创建全部回滚。两个请求争用同一 Fact 时最多一个成功。
- `trip_fact_assignment_changed` safe metadata 只记录动作和 Fact 类型；不得记录地点、人员、日期、金额、理由或其他财务字段。
- API 不返回 FieldClaim 原文、Evidence、Source 文件名、邮件头或 Provider 响应。测试只使用纯合成地点、人员与单据。

## 非目标

- 模型自动归属、自动接受建议、名称/地点模糊学习、路线或里程推断；
- 多城市分段、航段实体、日程管理、地图、费用政策或报销状态；
- 邮件正文语义提取、真实邮箱连接、真实 Provider 调用或模型正确率评测；
- 旧 Schema、旧 Prompt、旧 Claim 或旧数据库兼容。

## 验证要求

- 领域/Mapper/Validation 覆盖 Trip 根身份、三业务区段互斥、字段墓碑、日期规范化、日期倒置和 Evidence；Provider Schema 投影与能力身份随新契约变化。
- SQLite/应用覆盖确认 Trip、字段来源、严格幂等、归属 assign/move/unassign、活动唯一、陈旧期望、并发竞争、跨租户、Fact 删除和不可变历史。
- HTTP/OpenAPI 覆盖严格 JSON、CSRF、游标/筛选/上限、权限矩阵、跨租户不泄露与安全错误；Web 覆盖 Trip 审核、空列表、建议/全部/已归属、加载更多、移动/撤销、冲突保留、权限、键盘、768px 与 384px 回流。
- 完整门禁继续包含后端测试/vet/build、生成客户端、Web check、浏览器场景、关键不变量、两层覆盖率、diff、敏感信息、大文件、临时产物与进程残留审查。
