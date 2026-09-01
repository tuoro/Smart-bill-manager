# ADR-0017：确定性 Fact 洞察与筛选查询

状态：已接受，SQLite 实现已通过；ADR-0020 以 PostgreSQL 替代存储实现，业务规则继续有效
日期：2026-08-31

## 背景

M4 首切片需要让用户查询当前已确认账单并理解金额状态。既有 Payment/Invoice、活动 PaymentInvoiceLink 和活动 TripFactAssignment 已经是正式事实、余额和行程归属的唯一来源；若新增统计表、缓存或后台双写，会形成可漂移的第二数据源。自然语言助手仍在 Backlog，也不应借洞察入口绕过确定性筛选、权限或人工确认边界。

## 决策

### 只读取当前 Fact 的同一快照

`application/insights` 使用只读端口，在一个 SQLite 读事务中读取同租户未删除 Payment/Invoice、活动 PaymentInvoiceLink 与活动 TripFactAssignment。每个 Fact 形成一个临时 `FactInsightProjection`；查询汇总和当前页从同一组投影计算，不新增表、物化视图、触发器统计、运行时缓存、搜索索引或后台 Worker。

Payment 业务日期唯一读取确认时持久化的 `business_date`，Invoice 读取 `invoice_date`。分配金额只聚合活动 Link，当前行程只读取活动 Assignment。已终止 Link/Assignment、已删除 Fact 或已删除 Trip 不进入当前结果；指定 Trip 时必须在同一快照中确认它属于当前租户且未删除，不向跨租户调用者暴露存在性。

### 筛选和聚合是封闭契约

规范版本为 `fact-insights/1`。筛选字段固定为：

- `fact_type=all|payment|invoice`，默认 all；
- `date_from` 与 `date_to` 必须同时省略或同时提供合法包含边界的日期，起始不得晚于结束；
- 可选 `currency=CNY|USD|EUR|JPY`；
- `allocation_status=all|unallocated|partial|allocated`，默认 all；
- `trip_scope=all|assigned|unassigned`，默认 all；
- 可选 `trip_id` 只允许与 assigned 同时使用；
- `limit` 默认 50、最大 100；可选不透明 `cursor`。

HTTP 拒绝未知、重复、空白或非法参数，不选择隐藏日期窗口。投影包含 Fact 类型/ID、业务日期、安全显示摘要、总额、已分配、剩余、币种、分配状态和可选当前 Trip 摘要。金额必须为非负安全整数，且 `0 <= allocated <= amount`、`remaining = amount - allocated`；违反持久化不变量时整个请求显式失败。

汇总严格按币种和 Fact 类型分别计算 `count`、`total_minor`、`allocated_minor`、`remaining_minor` 与三种状态数量。禁止生成 Payment 与 Invoice 合并金额、跨币种金额、汇率换算或隐藏默认币种。累计超过安全整数上限返回 `insight_amount_overflow`，不得溢出、截断或饱和。

### 稳定分页绑定完整筛选身份

排序固定为 `business_date DESC, fact_type DESC, fact_id DESC`。游标版本为 `fact-insight-cursor/1`，封装规范筛选 hash 与最后一项完整排序键。解码使用封闭结构；编码、版本、字段、身份、筛选 hash 或边界不合法，以及边界 Fact 已不在当前筛选结果中时返回非法游标，不回退第一页。只有当前结果仍有下一项才返回 next cursor。

### API、权限与 Web

- `GET /api/v1/insights` 返回同一响应内的规范筛选、分组汇总、当前明细页和可选 next cursor；
- 新增 `insights.read`，只授予 Owner、Finance、Viewer；Reviewer 不能调用或借错误差异枚举 Fact、Trip、金额；
- 端点只读，不创建 AuditEvent，不返回 Source 文件名、Evidence、Claim 原文、邮件内容、Provider 数据、理由、存储键或凭据；
- Web `/insights` 提供显式筛选、清除、分组汇总和加载更多。不同币种/类型保持文字分组，不使用误导性总额或无决策价值图表；错误、空结果、离线、权限和分页终态分别呈现。

## 失败与一致性边界

- 同一请求的 Trip 校验、Fact、Link、Assignment、汇总和明细必须来自同一 SQLite 读快照；禁止把多个提交点拼成一个响应。
- 非法筛选、游标、租户、持久化金额或累计溢出明确失败；不吞错、不返回部分汇总、不静默跳过坏行。
- 所有 SQL 使用参数化接口并显式约束 tenant_id；应用和领域函数不修改输入。
- 查询不写 Fact、Link、Assignment、Reimbursement、AuditEvent 或本地浏览器存储。

## 非目标

- 自然语言查询、模型生成洞察、预测、趋势、同比、预算、税务、排名、图表装饰或导出；
- 持久化统计、分析数据库、全文索引、异步聚合或查询缓存；
- 汇率换算、跨币种或跨 Fact 类型总额；
- 修改分类、Fact、分配、行程归属或报销状态；
- 真实 Provider/邮箱/外部账号联调、正式模型正确率评测、部署或发布；
- 旧数据库、旧 API、旧 Dashboard 或旧统计兼容。

## 验证要求

- 领域测试覆盖筛选规范化、分配状态、安全聚合、混合币种/类型、稳定排序、分页、输入不变和累计溢出；
- SQLite/应用集成覆盖活动/终止 Link 与 Assignment、软删除、具体 Trip、同快照读取、稳定游标、筛选不匹配和租户隔离；
- HTTP/OpenAPI 覆盖封闭单值查询、日期/枚举/组合/limit/游标、四角色与安全错误；
- Web 覆盖筛选、清除、分组、三种状态、Trip 范围、空/失败/离线/权限、加载更多、键盘和 768px/384px 回流；
- 完整门禁继续包含后端测试/vet/build、生成客户端、Web check、浏览器场景、关键不变量、两层覆盖率、适用性能基线、diff、敏感信息、大文件、临时产物与进程残留审查。
