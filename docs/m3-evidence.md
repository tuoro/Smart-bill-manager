# M3 分切片验收证据

状态：三个切片全部通过；M3 已完成

更新日期：2026-08-31

## 首切片：邮箱 Source、邮件与附件本地归档

本切片以 `docs/decisions/0014-connector-neutral-email-archive.md` 为冻结决策，只使用纯合成 `.invalid` 身份和本地 RFC 822 字节，不连接真实邮箱、不创建凭据、不执行网络轮询或正式外部联调：

- Owner 只能注册显示名、规范邮箱、IMAP 主机、端口与强制 TLS 模式组成的无凭据 Source 描述符；规范连接身份唯一，幂等键同请求稳定重放、改请求明确冲突。
- 未来连接器只能调用内部 `email-message-archive/1` 端口；HTTP、CLI、测试 fixture 和生产装配均不存在邮件归档写入口。
- 原始邮件、附件对象、hash、part 身份与状态不可变；32 MiB 原文、10 层 MIME、200 个 part 和 50 个附件是显式边界，结构失败保留完整原文并形成 blocked 消息，不截断附件。
- 解析器只处理 MIME 结构、传输编码和受限安全头，不渲染正文、不执行 HTML、不加载远端资源、不展开压缩包，也不把正文送入模型。
- 合法 JPEG、PNG、WebP 与 PDF 附件复用既有 Inspector、Document 与 `document_process` Job；精确重复只链接已有 Document，不支持或非法附件逐项 `archived_only`，互不阻断。
- 邮件归档不会创建 AiRun、Claim 或 Fact。新 Document 仍只能进入既有模型、校验与人工审核链，Fact 继续只由确认 ReviewDecision 创建。
- 数据库和对象提交失败会补偿本轮消息聚合、新 Document/Job、审计、Source 状态及已提交对象；删除邮件来源 Document 只断开链接，邮件拥有的原件对象继续保留。
- `email_sources.manage` 只授予 Owner，`email_archive.read` 只授予 Owner/Finance；Reviewer/Viewer、跨租户读取与下载均显式拒绝且不泄露存在性。

## 首切片实现证据

- `apps/api/internal/domain/email.go` 与 `apps/api/internal/adapters/emailmime/parser.go` 固定描述符、外部消息键、容量和安全 MIME 投影边界。
- `infra/migrations/0001_initial.sql` 直接定义 Clean Slate 的 EmailSource、EmailMessage、EmailAttachment、Document 摄取来源、不可变触发器及租户约束；不存在兼容迁移或第二数据源。
- `apps/api/internal/application/emails/service.go` 与 SQLite 事务适配器统一处理暂存、严格重放、逐附件隔离、精确 Document 复用和故障补偿；邮件附件拥有原件对象。
- HTTP/OpenAPI 只公开 Source 登记、归档读取和强制下载；响应不含正文、原始头、外部键、hash、存储键或凭据形状字段。
- Web 邮箱来源页覆盖 Owner 登记、pending/active、空邮件、blocked、混合附件、游标分页、失败重试、离线和角色拒绝，不提供密码、Token、OAuth、Cookie 或同步控件。

## 首切片已执行验收

- 固定 Go 1.26.7 禁网容器：`go test -p=1 -count=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 全部通过；只读复用宿主既有模块缓存，未下载依赖。
- Web：`npm run check` 通过 OpenAPI 客户端生成、类型检查、ESLint、Prettier、6 个 Vitest 文件共 24 项测试和生产构建。
- 浏览器组件矩阵：`playwright test e2e/m1-state-matrix.spec.ts e2e/m3-email-sources.spec.ts` 21 / 21 通过；其中 3 项新增场景覆盖 Owner/Finance/Reviewer、无凭据登记、混合/blocked/分页/离线、键盘、768px 与 384px 等效回流。
- 关键不变量：83 / 83（100%）通过；新增 11 个映射覆盖描述符、MIME 安全投影与精确边界、归档生命周期、幂等并发、故障补偿、对象所有权、32 MiB 原文和 HTTP 隐私/权限边界。
- 领域/应用层语句覆盖率 85.61%（2,468 / 2,883，门槛 85%）；基础设施/传输层 73.88%（2,798 / 3,787，门槛 70%）。
- 首轮浏览器命令因临时 Vite 端口与项目固定基址不一致而在页面加载前失败；改用权威配置端口后 21 / 21 通过。该环境失败未改写为功能通过，失败截图与构建结果均作为本轮临时产物清理。
- 工作区和 55 个精确暂存文件的 diff 检查通过，最大暂存文件为 185,217 字节的既有锁文件；高置信密钥前缀、私有资产路径、生成/临时产物和项目进程残留检查均通过。宿主仅保留两个与本项目无关的 `shenlun` PostgreSQL 容器，本轮未触碰。
- 机读摘要见 `tests/evidence/m3/email-archive-gate-summary.json`；只记录纯合成身份、聚合门禁与安全元数据。

## 首切片边界与当时断点

- 本切片未调用真实 Provider、未发送真实图片、未执行模型正确率正式评测，也未安装或恢复本地 OCR、模型缓存或新依赖。
- 未连接真实邮箱、云服务或外部账号，未创建或变更凭据，未推送、部署、发布或创建远端资源。
- 下一断点是先冻结 M3 行程归属切片的范围、数据模型、失败边界与验收标准，再实施领域、数据库、API、Web 和测试；真实邮箱连接继续需要重新授权。

## 第二切片：行程 Fact 与确定性单据归属

本切片以 `docs/decisions/0015-trip-fact-attribution.md` 为冻结决策，只使用纯合成 Provider 输出、Claim、Fact 和本地数据库数据：

- 活动链升级为 `bill-visible-text-cn/2 -> bill-visible-text/2 -> bill-visible-text-provider/2 -> claim-mapper/4 -> document-claim/3`；Payment、Invoice、Trip 三个业务根成员严格互斥，旧版本不保留活动兼容入口。
- Trip 只从票面原文确定性规范化可选出发地、必填目的地与起止日期，以及可选出行人、交通类型和预订编号；缺失、非法日期、日期倒置和 Evidence 问题形成 blocked Claim。
- Trip 仍只能沿 Document、AiRun、Claim、Revision、Review 创建 Fact。确认事务同时形成 Trip、ReviewDecision、FactFieldOrigin、完整重复决定和安全 AuditEvent；审核专用补充字段不会伪装成 Fact 来源。
- `trip-attribution/1` 只根据行程闭区间、前后 3 个日历日、活动 PaymentInvoiceLink 的已归属另一端和当前归属生成稳定原因；建议不自动接受，未建议 Fact 仍可人工处理。
- 每个 Payment/Invoice 同时最多一个活动 Trip 归属；assign、move、unassign 都提交期望当前 Link、必填理由和幂等键。Decision 与 Link 历史不可变，移动或删除只终止旧活动 Link，不原地改写或删除历史。
- Owner/Finance 可以管理归属，Owner/Finance/Viewer 可以读取 Fact，Reviewer 只沿现有 Claim 审核边界处理 Trip，Owner 才能删除。跨租户和不存在资源均不泄露。

## 第二切片实现证据

- `contracts/schemas/`、`contracts/openapi/openapi.yaml`、Provider Adapter、`claim-mapper/4` 与领域 Validation 共同固定 Trip 的最小可见文本、Claim 和 HTTP 契约。
- `infra/migrations/0001_initial.sql` 直接定义 Clean Slate Trip、归属 Decision/Link、活动唯一索引、不可变触发器和删除终止约束；不存在旧 Schema 迁移或第二数据源。
- `apps/api/internal/application/trips/`、SQLite Trip 适配器与 HTTP Handler 统一处理列表、三种候选视图、稳定游标、确定性原因、严格幂等、期望快照、并发竞争和删除生命周期。
- Review 确认请求区分成员缺失与显式 `null`：Trip 必须同时省略金额关联成员，Payment/Invoice 必须同时提交非空且类型正确的完整计划；已完成 Review 的同请求重放仍返回同一结果。
- Web 行程归属工作区覆盖列表、建议/全部/已归属、分页、assign/move/unassign、冲突保留与同幂等键重试、只读角色、加载、失败、离线、键盘和响应式回流。

## 第二切片已执行验收

- 固定 Go 1.26.7 禁网容器：`go test -p=1 -count=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 全部通过；只读复用宿主既有模块缓存，未下载依赖。
- Web：`npm run check` 通过 OpenAPI 客户端生成、类型检查、ESLint、Prettier、7 个 Vitest 文件共 27 项测试和生产构建。
- 浏览器组件矩阵：`playwright test e2e/m1-state-matrix.spec.ts e2e/m3-email-sources.spec.ts e2e/m3-trip-attribution.spec.ts` 24 / 24 通过；其中 3 项新增行程场景覆盖 Owner/Finance/Viewer/Reviewer、严格可空请求、503 恢复、409 同幂等键重试、assign/move/unassign、键盘、768px 与 384px 等效回流。
- 关键不变量：92 / 92（100%）通过；新增 9 个映射覆盖 Trip Claim/Review/Fact、确定性建议、活动归属唯一、严格幂等、并发、不可变历史、删除、安全审计、游标/权限与租户边界。
- 领域/应用层语句覆盖率 85.53%（2,594 / 3,033，门槛 85%）；基础设施/传输层 74.07%（3,016 / 4,072，门槛 70%）。
- 浏览器场景中有意注入的 503 与 409 先由页面断言可见错误和草稿保留，再执行恢复；它们是已验证的失败边界，不被改写为成功。
- 工作区和 75 个精确暂存文件的 diff 检查通过，最大暂存文件为 91,199 字节的既有集成测试；高置信密钥前缀、私有资产路径、二进制/大文件、生成/临时产物和项目进程残留检查均通过。宿主只保留与本项目无关的 `shenlun` 容器，本轮未触碰。
- 机读摘要见 `tests/evidence/m3/trip-attribution-gate-summary.json`；只记录纯合成场景、聚合门禁与安全元数据。

## 第二切片边界与当时断点

- 本切片未调用真实 Provider、未发送真实图片、未执行模型正确率正式评测，也未安装或恢复本地 OCR、模型缓存或新依赖。
- 未连接真实邮箱、云服务或外部账号，未创建或变更凭据，未推送、部署、发布或创建远端资源。
- 第三切片随后按 `docs/decisions/0016-reimbursement-workflow-policy-findings.md` 冻结报销快照、状态历史与确定性政策提示；真实外部联调继续需要重新授权。

## 第三切片：报销快照、状态历史与确定性政策提示

本切片以 ADR-0016 为冻结决策，只使用已确认的纯合成 Payment、Invoice、Trip、活动 Link 和本地数据库数据：

- Reimbursement 不是 Fact，也不修改 Fact、PaymentInvoiceLink 或 TripFactAssignment；用户只能从一个 Trip 的当前活动 Assignment 中显式选择 1～200 项。
- `reimbursement-policy/1` 只产生缺少选中发票、活动 Link 金额未覆盖 Fact 总额和其他有效报销重复三类确定性 Finding；混合币种独立汇总，rejected 历史不计重复。
- 预检不落库；提交必须携带当前选择、完整 Finding 确认和 `snapshot_hash`，事务内以同一只读快照重算。Link、Assignment、来源业务日期或相关报销状态变化都会使旧预检显式陈旧。
- 成功提交原子创建 submitted Reimbursement、不可变 Item/Finding/Decision 与安全 AuditEvent；状态变更通过数据库约束和追加 Decision 原子推进 version，不形成孤立历史。
- 同一 Trip 最多一个 submitted；创建与状态变更均严格幂等，并对陈旧版本、并发竞争、审计故障、跨租户与软删除历史提供零部分写入或可解释读取。
- Owner/Finance 可管理，Owner/Finance/Viewer 可读，Reviewer 不可枚举；HTTP 使用严格 JSON、CSRF、UUID、游标与安全不存在性边界。

## 第三切片实现证据

- 领域、SQLite、应用层、HTTP/OpenAPI、生成客户端和独立报销工作区已实现；确认时持久化的 Payment 业务日期是行程、重复和报销规则的唯一日期来源。
- Finding 身份绑定相关 Link 的完整不可变身份；预检与详情读取使用数据库一致性快照，列表和详情从不可变 Item/Finding/Decision 历史解释软删除来源。
- 数据库直接约束 Item/Finding/Decision 不可变、Reimbursement 不可删除、状态图/version 推进、同 Trip submitted 唯一和业务日期不可变；不存在旧状态兼容或第二数据源。
- Web 覆盖显式选择、三类提示、无提示、混合币种、分页、提交、状态变化、冲突草稿保留、来源删除、四角色、加载/失败/离线、键盘和响应式回流。

## 第三切片已执行验收

- 固定 Go 1.26.7 禁网容器：`go test -p=1 -count=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 全部通过；只读复用宿主既有模块缓存，未下载依赖。
- Web：`npm run check` 通过 OpenAPI 客户端生成、类型检查、ESLint、Prettier、8 个 Vitest 文件共 30 项测试和生产构建。
- 浏览器组件矩阵：`playwright test e2e/m1-state-matrix.spec.ts e2e/m3-email-sources.spec.ts e2e/m3-trip-attribution.spec.ts e2e/m3-reimbursements.spec.ts` 29 / 29 通过；其中 5 项新增报销场景覆盖 Owner/Finance/Viewer/Reviewer、三类提示、混合币种、无提示、503 恢复、冲突草稿、键盘、768px 与 384px 等效回流。
- 关键不变量：104 / 104（100%）通过；新增映射覆盖选择上限、三类 Finding、稳定 key/hash、完整确认、混合币种、陈旧重算、创建/状态严格幂等与并发、同 Trip submitted 唯一、不可变历史、读取快照、Payment 业务日期、安全审计、权限/租户和 HTTP 生命周期。
- 领域/应用层语句覆盖率 85.42%（2,900 / 3,395，门槛 85%）；基础设施/传输层 75.13%（3,417 / 4,548，门槛 70%）。
- 浏览器场景中有意注入的 503 与冲突先由页面断言可见错误和草稿保留，再执行恢复；它们是已验证失败边界，不被改写为成功。
- 最终工作区与 51 个精确暂存文件的 diff 检查通过，最大暂存文件为 81,560 字节的 Clean Slate 初始迁移；高置信凭据前缀、私有资产路径、二进制/大文件、生成/临时产物和项目进程残留检查均通过。宿主仅保留与本项目无关的 `shenlun` 容器，本轮未触碰。
- 机读摘要见 `tests/evidence/m3/reimbursement-workflow-gate-summary.json`；M3 汇总见 `tests/evidence/m3/m3-closure-gate-summary.json`。

## M3 收口与下一断点

- 邮箱本地归档、Trip Fact/归属和报销工作流三个切片均与 ADR-0014～0016 一致，Source/Claim/Fact、人工审核、租户隔离、整数金额、不可变历史和安全审计边界保持不变。
- 本轮未调用真实 Provider、未发送真实图片、未执行模型正确率正式评测，未连接真实邮箱、云服务或外部账号，未创建或变更凭据，也未安装或恢复本地 OCR、模型缓存或新依赖。
- M3 完成不代表真实模型正确率、真实邮箱/Provider 联调、部署或发布通过；这些门禁继续明确排除。
- 下一断点是先冻结 M4 首个本地切片的数据洞察与查询范围、失败边界和验收标准，再实施代码。
