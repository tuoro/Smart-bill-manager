# M4 验收证据

状态：进行中；首切片“确定性 Fact 洞察与筛选查询”已通过，下一断点为完整备份恢复演练
日期：2026-08-31

## 首切片：冻结范围

本切片以 ADR-0017 为唯一决策边界，只读取当前未删除 Payment/Invoice、活动 PaymentInvoiceLink、活动 TripFactAssignment 和同租户未删除 Trip：

- `fact-insights/1` 在同一 SQLite 读快照内形成一次性投影，不增加统计表、物化缓存、双写、后台聚合或第二数据源；
- Payment 使用确认时持久化的 `business_date`，Invoice 使用 `invoice_date`；金额保持整数最小单位；
- 汇总严格按币种和 Fact 类型分组，禁止跨币种或 Payment/Invoice 合并金额；
- 查询只接受封闭单值筛选，`fact-insight-cursor/1` 绑定完整筛选身份与最后排序键；
- Owner、Finance、Viewer 可读，Reviewer 拒绝；查询不写 AuditEvent 或任何业务状态；
- Web 只提供显式筛选、文字汇总、稳定分页和错误恢复，不引入自然语言查询或装饰性图表。

## 实现证据

- `apps/api/internal/domain/insight.go` 统一负责筛选规范化、投影校验、安全整数聚合、稳定排序和分页；非法持久化金额、重复 Fact、坏 Trip、游标边界和累计溢出全部显式失败。
- `apps/api/internal/adapters/sqlite/insights.go` 在一个读事务中参数化查询权威 Fact、活动 Link 和活动归属；迁移 `0002_fact_insight_indexes.sql` 只增加筛选索引，没有 Insight 表或统计触发器。
- `apps/api/internal/application/insights/` 与 HTTP `/api/v1/insights` 固定权限、封闭查询、游标版本和安全错误；OpenAPI 与生成客户端来自同一契约。
- Web `/insights` 覆盖筛选、清除、分组、加载更多、空结果、失败重试、离线、只读和权限不足；不同币种与类型不显示为单一总额。
- `tools/run-performance.mjs` 已把 `/insights?limit=100` 纳入后续 M4 完整 10,000 Fact HTTP 性能运行器。

## 已执行验收

- 固定 Go 1.26.7 禁网容器：`go test -p=1 -count=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 全部通过；只读复用宿主既有模块缓存，未下载依赖。
- Web：`npm run check` 通过 OpenAPI 客户端生成、类型检查、ESLint、Prettier、9 个 Vitest 文件共 38 项测试和生产构建。
- 浏览器组件矩阵：M1 状态矩阵、M3 三个工作区与 M4 洞察共 33 / 33 通过；其中 4 项新增洞察场景覆盖完整筛选、分组、分页、空结果、503 恢复、Owner/Finance/Viewer/Reviewer、键盘、768px 与 384px 等效回流。
- 关键不变量：113 / 113（100%）通过；新增 9 个映射覆盖筛选身份、类型/币种分组、安全整数、当前唯一来源、同快照、权限/租户、只读 HTTP 和无统计副本。
- 领域/应用层语句覆盖率 85.71%（3,101 / 3,618，门槛 85%）；基础设施/传输层 75.24%（3,477 / 4,621，门槛 70%）。
- 10,000 个纯合成 Fact 的 SQLite 查询与领域投影固定基准运行 20 次，结果为 70.58 ms/op、11,367,477 B/op、189,614 allocs/op。该结果验证本切片主要本地计算路径，不冒充并发 HTTP p95；完整 HTTP p95 将在 M4 运行质量切片按批准协议统一重跑。
- 浏览器场景中有意注入的 503 先由页面断言可见错误与可恢复状态，再执行重试；它是已验证失败边界，不被改写为成功。
- 最终工作区与 42 个精确暂存文件的 diff 检查通过，最大暂存文件为 185,217 字节的既有 Web lockfile；高置信凭据前缀、私有资产路径、二进制/大文件及生成/临时产物检查均通过。本轮 Vite、Playwright 和测试容器已停止；宿主仍有一个约 29 小时前启动、命令行不含当前工作区路径的旧 Lighthouse/Chromium 进程组，以及与本项目无关的 `shenlun` 容器。它们并非本轮创建，按清理授权边界保持原状。

## 明确排除与下一断点

- 通用代表性 E2E 命令在测试预检阶段要求当前未授权的 Provider/账号环境变量，因此没有进入测试执行，也没有纳入通过数；本切片只采用上述纯合成组件浏览器矩阵。
- 本轮未调用真实 Provider、未发送真实图片、未执行模型正确率正式评测，未连接真实邮箱、云服务或外部账号，也未安装或恢复本地 OCR、模型缓存或新依赖。
- 本切片未推送、部署、发布、创建远端资源、打 Tag 或改写历史。
- 下一断点是先在权威文档冻结 M4 的 1,000 个纯合成 Document 完整备份恢复演练、30 分钟恢复目标、清单对账与失败边界，再实施代码和演练；真实模型评测、真实外部系统联调与生产发布仍保持门禁。

可机读摘要见 `tests/evidence/m4/fact-insights-gate-summary.json`。
