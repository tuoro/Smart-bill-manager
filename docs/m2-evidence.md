# M2 分切片验收证据

状态：通过；支付—发票金额分配、确定性重复检测、跨页明细分页审核与多 Document 批量上传四个切片已完成，M2 整体仍在进行中

更新日期：2026-08-31

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

## 第三切片：复杂多页 PDF 跨页明细与分页审核

本切片以 `docs/decisions/0011-cross-page-invoice-review.md` 为冻结决策，保持 `bill-visible-text-cn/1 -> bill-visible-text/1 -> claim-mapper/3 -> document-claim/2` 活动链路不变：

- 模型仍在一次请求中查看全部有序规范化页面，并把同一逻辑明细的字段放在一个稳定 item 中；本地不拼接文字、不重新识别表格、不重组 item，也不执行第二次模型调用。
- 当前 Claim revision 的完整页面序列、逐页字段/明细与 InvoiceItem 页跨度，只从 `FieldClaim -> Evidence -> DocumentPage` 实时派生，不新增持久化页计划或第二数据源。
- InvoiceItem 的 `sort_order` 必须唯一且严格连续；同一明细的 Evidence 页集合必须连续，后续明细起始页不能早于前一明细结束页。缺口、重复、跳页或倒退均形成 blocked Validation。
- 规范化单页读取显式使用 `(tenant_id, document_id, page_number)`，不公开 storage key；reviewer 只在待审核或阻断状态可读，越权、不存在和 reviewer 终态均保持不泄露边界。
- Web 提供上一页、下一页和直接页码导航；文档级字段始终可见，明细按当前页筛选，无 Evidence 的阻断明细保持可达，同一跨页 item 复用稳定编辑身份并显示页范围。

### 第三切片实现证据

- `apps/api/internal/domain/pageplan.go` 是页计划与页序校验的唯一领域实现；保存 revision 后服务从当前 FieldClaim Evidence 重算读取投影。
- SQLite 只增加既有 `document_pages` 的 tenant 约束读取，不新增表或迁移；Document 查询服务与 HTTP 入口共同执行页码、能力和 reviewer 状态边界。
- OpenAPI、生成客户端和 Review 响应公开 `page_count`、完整 `pages` 及 `invoice_item_spans`，不公开对象存储路径。
- 审核工作台使用规范化单页 PNG，字段选择定位首条 Evidence 页，分页切换不复制或丢失同一 item 的编辑状态。
- 纯合成测试覆盖相邻跨页、三页连续、空白页、完整 20 页、页跳跃、顺序倒退、重复/缺口顺序、revision 证据重绑、非法页、reviewer 终态和跨租户读取。

### 第三切片已执行验收

- 固定 Go 1.26.7 离线容器：后端全量 `go test -p=1 -count=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 通过；只读复用宿主现有模块缓存，未下载依赖。
- Web：`npm run check` 通过 OpenAPI 客户端生成、类型检查、ESLint、Prettier、3 个 Vitest 文件共 13 项测试和生产构建。
- 浏览器组件矩阵：`playwright test e2e/m1-state-matrix.spec.ts` 13 / 13 通过；新增场景覆盖直接分页、页面图片、审核上下文，以及 768px 与等效 200% 回流下的无横向溢出、键盘聚焦与分页操作。
- 关键不变量：60 / 60（100%）通过，其中 5 个新增映射覆盖稳定跨页 item、空白页与完整 20 页、页序与 `sort_order` 阻断、revision 后重算及规范化页读取边界；跨租户页面读取继续由既有 HTTP 租户隔离分支覆盖。
- 领域/应用层语句覆盖率 85.59%（2,049 / 2,394，门槛 85%）；基础设施/传输层 73.25%（2,174 / 2,968，门槛 70%）。
- 最终工作区与暂存区 diff、安全和残留检查通过；本轮生成的 Web 构建与 Playwright 结果目录以及误建的空 Go 缓存卷已清理，未留下 Vite、浏览器或 Go 验收进程。
- 机读摘要见 `tests/evidence/m2/cross-page-review-gate-summary.json`；只包含纯合成测试身份、聚合门禁和安全元数据。

## 第四切片：多 Document 批量上传与逐项反馈

本切片以 `docs/decisions/0012-client-orchestrated-batch-upload.md` 为冻结决策，批量只作为 Web 对唯一单 Document 上传命令的顺序编排：

- 一次选择保留全部文件的原顺序，前 20 项逐项处理；第 21 项及以后以 `batch_file_limit_exceeded` 显式拒绝。浏览器提前拒绝明显超过 20 MiB 的单项，但服务端仍独立执行权威大小、签名、MIME 与文件名校验。
- 任意时刻最多一个请求在途。每项进入等待、上传中、已入队、已存在或已拒绝之一；中间的服务端或网络失败不会回滚成功项，也不会阻止后续项。
- 精确重复仍逐项发送给服务端并由同租户 SHA-256 规则收敛；客户端不计算内容哈希、不合并文件，也不把重复伪装成新 Job。
- 批次反馈只存在于当前页面内存，结束后刷新权威任务队列；未新增 Batch/BatchItem 表、数据库迁移、服务端批量 multipart、批次事务、并行上传或自动重试。

### 第四切片实现证据

- `apps/web/src/features/inbox/batch.ts` 是顺序状态机、20 项/20 MiB 本地预检、错误归类和汇总的唯一实现；每次状态更新返回新列表，不修改输入或保存全局批次状态。
- 收件箱原生多选入口在运行期间禁用，并以可访问的有序列表显示文件名、大小、文字状态和安全原因；768px 与 384px 回流不产生页面横向溢出。
- `UploadService.Execute`、`POST /api/v1/documents`、OpenAPI 与数据库 Schema 均保持不变。新增后端集成场景只证明两个成功命令夹着一个失败命令时，成功项各一份 Document/Job、失败项零元数据与暂存对象残留。
- 纯合成单元与浏览器场景覆盖三项正常顺序、精确重复、服务端拒绝、网络异常、20 项上限、单项大小上限、失败继续、运行中禁选、逐项安全消息和响应式/键盘边界。

### 第四切片已执行验收

- 固定 Go 1.26.7 离线容器：后端全量 `go test -p=1 -count=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 通过；只读复用宿主既有模块缓存，未下载依赖。
- Web：`npm run check` 通过 OpenAPI 客户端生成、类型检查、ESLint、Prettier、4 个 Vitest 文件共 17 项测试和生产构建。
- 浏览器组件矩阵：`playwright test e2e/m1-state-matrix.spec.ts` 14 / 14 通过；新增场景覆盖三个文件的串行状态、重复与拒绝继续、运行中禁选、逐项终态，以及 768px 与等效 200% 的 384px 回流和键盘聚焦。
- 关键不变量：61 / 61（100%）通过；新增映射证明独立上传命令在中间拒绝后继续，成功项各有且仅有一个 Document/Job，失败项无元数据或暂存对象残留。
- 领域/应用层语句覆盖率 85.63%（2,050 / 2,394，门槛 85%）；基础设施/传输层 73.25%（2,174 / 2,968，门槛 70%）。
- 最终工作区与暂存区 `git diff --check` 通过；19 个暂存文件中最大文件为 54,729 字节，已知凭据前缀与私有/生成资产路径扫描无命中。本轮 Web 构建与 Playwright 结果目录已清理，未留下 Vite、Playwright、Go 门禁进程或本项目容器。
- 机读摘要见 `tests/evidence/m2/batch-upload-gate-summary.json`；只记录纯合成测试身份、聚合门禁和安全元数据。

## 边界与下一断点

- 本轮未调用真实 Provider，未处理模型正确率，未安装或恢复本地 OCR、RapidOCR、PaddleOCR 或模型缓存。
- 本轮未实施已确认 Fact 的独立 Link 调整工作流。
- 第四切片门禁记录时尚未部署、提交或推送；本文件随获授权的独立本地切片提交固化后，以包含本文件的本地 Git commit 为事实来源，仍不得推送或部署。
- 下一断点是先冻结已确认 Fact 之间独立补充分配、撤销或替换工作流的范围、验收、状态机和必要 ADR。模型正确率继续保留到全部功能完成后的 M4 正式门禁。
