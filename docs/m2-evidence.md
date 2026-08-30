# M2 分切片验收证据

状态：通过；支付—发票金额分配与确定性重复检测两个切片已完成，M2 整体仍在进行中

更新日期：2026-08-30

## 首切片：冻结范围

本切片以 `docs/decisions/0009-payment-invoice-allocation.md` 替换 M1 的全额一对一活动规则，未保留运行时兼容分支或旧数据迁移：

- 候选由同租户、相反类型、同币种、日期相差不超过 30 天且目标仍有正余额决定；金额完全相等只形成原因代码。
- 用户在审核时提交 `allocate_candidates`、`reject_all` 或 `no_candidate`；分配计划由唯一候选 ID 和正整数最小单位金额组成。
- 一个确认可形成一对多或多对一的不可变 PaymentInvoiceLink；同一对同时最多一条活动 Link，双方活动分配合计不得超过各自总额。
- 规范排序后的完整计划 SHA-256 进入幂等身份；重排重放返回相同结果，改变候选或金额产生冲突。
- 列表余额由活动 Link 唯一聚合为 `allocated_minor`、`remaining_minor` 和 `unallocated | partial | allocated`，不维护第二份余额。
- 删除 Fact 终止其全部活动 Link 并恢复另一端派生余额；首切片不提供确认后的独立补分配、撤销或替换入口。

## 首切片：实现证据

- `apps/api/internal/domain/allocation.go` 负责规范化、计划哈希及候选、币种、金额和余额校验。
- 应用确认事务重新校验目标余额，并原子创建 Fact、ReviewDecision、完整候选决定集、全部 Link、AuditEvent 与 Job 终态；任一冲突不留下部分写入。
- `infra/migrations/0001_initial.sql` 直接定义 Clean Slate 金额 Link、活动对唯一约束及双方预算触发器，没有兼容迁移。
- `contracts/openapi/openapi.yaml` 与生成客户端公开多候选分配请求、全部 Link ID、候选余额和 Fact 分配投影。
- 审核工作台提供多候选选择、逐项金额输入、实时分配合计与剩余金额；Payment 与 Invoice 列表显示派生分配状态。
- 关键不变量矩阵已经用一对多、多对一和最后余额并发分支替换旧一对一分支。

## 首切片：已执行验收

- 固定 Go 1.26.7 容器：`go test -p=1 -timeout 60s ./...`、`go vet ./...`、`go build -buildvcs=false ./...` 全部通过。
- Web：`npm run check` 通过契约生成、TypeScript、ESLint、Prettier、Vitest 和生产构建；3 个测试文件共 10 个测试通过。
- 浏览器验收：`apps/web/e2e/m1-state-matrix.spec.ts` 10 / 10 通过，其中包含选择两个候选、提交两项金额并核对两个 Link ID 的真实组件场景。
- 关键不变量：43 / 43（100%）通过，覆盖一对多、多对一、部分余额、计划形状与余额校验、计划幂等、非法或超额分配、活动对唯一、Link 不可变、候选删除、跨租户/跨币种、并发最后余额、无部分写入和删除恢复。
- 领域/应用层语句覆盖率 86.70%（1,656 / 1,910，门槛 85%）；基础设施/传输层 72.70%（1,917 / 2,637，门槛 70%）。
- 最终工作区 `git diff --check`、精确暂存后的 `git diff --cached --check`、证据 JSON 解析和新增文档格式检查均通过；374 个暂存文件中最大对象为 521,163 字节，已知凭据前缀和私有资产路径检查无命中。机读摘要见 `tests/evidence/m2/gate-summary.json`。

## 第二切片：确定性重复检测

本切片以 `docs/decisions/0010-deterministic-duplicate-detection.md` 为冻结决策，不替代精确 SHA-256 上传冲突或规范化发票号 blocked 规则，也不进入跨页明细重建：

- `page-visual-dedup/1` 对现有规范化 RGBA 页确定性生成 dHash、aHash 和四个检索 band；同租户 band 查询后，再以 1% 宽高比及双哈希 Hamming 距离 3 的阈值作唯一判断。
- `near_file`、`cross_page` 和 `field_combination` 只形成 Claim revision 的不可变疑似重复候选，不自动合并、覆盖、删除、跳过或创建 Fact。
- Payment 字段组合固定为正数金额、币种、规范化商户及 5 分钟窗口；Invoice 固定为正数价税合计、币种、日期及规范化购销方，精确同号继续由既有硬规则处理。
- 每个 revision 最多 50 个候选；超限形成 `duplicate_candidate_limit_exceeded` blocked Validation，不静默截断。
- Review API 与 Web 分区展示安全摘要、页码、距离、原因及目标可用性；确认 Fact 前必须对完整集合逐项提交 `keep_distinct`，认为当前单据重复时走 Reject。
- 规范 resolution 计划 SHA-256 进入 ReviewDecision 幂等身份；确认事务重算候选键，集合新增、消失或目标状态变化时整体回滚并返回 `duplicate_candidate_set_stale`。
- 数据库强制候选形状、生成时租户范围、50 项上限、候选与决定不可变、每候选最多一个决定，以及同一 Claim 的全部决定属于同一个确认 ReviewDecision。

### 第二切片实现证据

- `apps/api/internal/domain/duplicate.go` 固定视觉距离、三类信号、候选形状与稳定键，以及完整 resolution 计划的规范排序和哈希。
- `apps/api/internal/adapters/localstorage/fingerprint.go` 只复用现有图像依赖计算版本化指纹；未引入 OCR、模型、图像修复或外部服务。
- SQLite band 检索、字段目标查询、候选快照、动态目标可用性和确认时重算集中在同一适配器；迁移直接定义 Clean Slate 表、索引与触发器，没有旧结构兼容分支。
- 初版 Claim 与用户完整 revision 均在同一持久化事务内重建候选；Fact、两类候选决定、Link、AuditEvent、Claim 与 Job 继续原子提交。
- OpenAPI、生成客户端和审核工作台公开唯一 `duplicate_resolutions` 契约；缺失或陈旧计划有文字错误、键盘可达控件和 `aria-describedby` 关联。

### 第二切片已执行验收

- 固定 Go 1.26.7 离线容器：后端全量 `go test -p=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 通过。
- Web：`npm run check` 通过 OpenAPI 客户端生成、类型检查、ESLint、Prettier、3 个 Vitest 文件共 11 项测试和生产构建。
- 浏览器组件矩阵：`playwright test e2e/m1-state-matrix.spec.ts` 11 / 11 通过；新增场景验证未逐项确认时主动作禁用、错误关联、键盘复选与规范请求体。
- 关键不变量：55 / 55（100%）通过，其中 12 个新增映射覆盖视觉重编码/等比缩放、阈值、band 租户范围、整份优先、同文档页对、Payment/Invoice 组合、50 项上限、完整/伪造 resolution、目标删除回滚、生产形态 SQLite 并发、单一确认决定、不可变决定和 Reject 无 Fact。
- 领域/应用层语句覆盖率 85.05%（1,917 / 2,254，门槛 85%）；基础设施/传输层 73.20%（2,144 / 2,929，门槛 70%）。
- 最终 `git diff --check` 与精确暂存后的 `git diff --cached --check` 通过；46 个暂存文件中最大文件为 86,817 字节，已知凭据前缀与私有/生成资产路径扫描无命中。本轮生成的 Web 构建和 Playwright 结果目录已清理，未留下 Vite、浏览器、Go 测试进程或本项目容器。
- 机读摘要见 `tests/evidence/m2/duplicate-detection-gate-summary.json`；只包含合成测试身份、聚合门禁和安全元数据。

## 边界与下一断点

- 本轮未调用真实 Provider，未处理模型正确率，未安装或恢复本地 OCR、RapidOCR、PaddleOCR 或模型缓存。
- 本轮未实施复杂跨页明细重建、多 Document 批量上传或已确认 Fact 的独立 Link 调整工作流。
- 第二切片门禁记录时尚未部署、提交或推送；本文件随获授权的独立本地切片提交固化后，以包含本文件的本地 Git commit 为事实来源，仍不得推送或部署。
- 下一断点是先冻结复杂多页 PDF 跨页明细重建与分页审阅的范围、验收和必要 ADR。模型正确率继续保留到全部功能完成后的 M4 正式门禁。
