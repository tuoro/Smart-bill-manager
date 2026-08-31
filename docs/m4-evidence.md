# M4 验收证据

状态：进行中；首、第二切片均已通过，下一切片为运行质量与本地发布准备
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

## 首切片明确排除与交接断点

- 通用代表性 E2E 命令在测试预检阶段要求当前未授权的 Provider/账号环境变量，因此没有进入测试执行，也没有纳入通过数；本切片只采用上述纯合成组件浏览器矩阵。
- 本轮未调用真实 Provider、未发送真实图片、未执行模型正确率正式评测，未连接真实邮箱、云服务或外部账号，也未安装或恢复本地 OCR、模型缓存或新依赖。
- 本切片未推送、部署、发布、创建远端资源、打 Tag 或改写历史。
- 首切片交接断点是冻结 M4 的 1,000 个纯合成 Document 完整备份恢复演练、30 分钟恢复目标、清单对账与失败边界；该范围现已由 ADR-0018 冻结并进入实施。真实模型评测、真实外部系统联调与生产发布仍保持门禁。

可机读摘要见 `tests/evidence/m4/fact-insights-gate-summary.json`。

## 第二切片：认证停机备份与完整恢复演练

本切片以 ADR-0018 为唯一决策边界，旧 M1 两 Document 冒烟只作为历史证据，不作为 M4 通过结论：

- 数据备份包与既有主密钥独立保管；数据包只含 SQLite、精确对象集合和带随机 `backup_set_id` 的 `smart-bill-manager-backup/2` 清单，清单由主密钥域分离派生的 HMAC-SHA-256 认证；
- 应用、Owner 初始化与备份共享运行锁；备份要求 WAL checkpoint、SQLite 排他快照、`integrity_check`、`foreign_key_check`、迁移/Schema 身份，以及空 `staging/` 和空 `trash/`；
- 数据库对象引用覆盖 Document、DocumentPage、EmailMessage 原文与非空 EmailAttachment；共享 key 去重但哈希和已知大小必须一致，已提交对象集合不得缺失或多余；
- 恢复只写与源/迁移/guard 路径不重叠的不存在目标，使用各目标文件系统 staging 与持久 `restore-state`；durable incomplete 阻止半恢复，已单独同步的 complete 原子替换后永久保留并与数据库成对；离线精确复核后删除全部旧 Session，再验证只有 Session 归零这一项允许变化；
- 固定演练最终恰好包含 1,000 个有实际纯合成原件的 Document（997 个普通失败上传、1 个失败邮件附件 Document、1 个已确认 Payment、1 个 Processing）、2 个 DocumentPage、1,004 条对象引用和 1,003 个唯一物理对象；Processing 只有在挂起 Provider 已收到请求后才可作为 `running` AiRun 边界；
- 数据包创建后、首次独立 verify 前启动唯一 30 分钟时钟；恢复启动后先验证原快照查询和上传/邮件对象下载，再覆盖租约接管、`lease_expired` 收口、attempt 单次增长、继续审核和唯一 Fact 增量；
- 备份前冻结既有非 Session 行摘要、append-only 前缀、998 个精确失败 Job/Document、两个 Page 归属和全局 AiRun 形状；目标 Job 只能有唯一旧 `running` AiRun，唯一旧 `succeeded` AiRun 必须归属已确认 Document。恢复后的新 AiRun 必须复用同一 Provider 版本/指纹、模型、Prompt、各 Schema/Mapper/输入版本和确定性请求哈希；只允许目标 Job/Document/旧 AiRun 明确变化及一个闭合 Claim→Review→Payment 链，其余既有行和非目标 Job/AiRun 不变；
- 演练 RPO 固定为 0，不虚构非零 RPO 的生产删除/撤销重放能力，不调用真实 Provider、邮箱、外部账号或部署环境。

### 实现与完整演练证据

- 新 `cmd/backup` 只支持 `smart-bill-manager-backup/2` 的 backup、verify 与 restore；使用域分离 HMAC、严格清单、完整 SQLite/迁移/Schema/审计链检查和四类对象精确集合，不保留 M1 清单或旧入口兼容。
- 应用、`bootstrap-owner`、性能种子、备份与恢复共享数据库侧排他运行锁；数据库/主密钥多硬链接、数据库/父目录符号链接、宽权限锁、源内备份输出、运行中写入者和非 complete `restore-state` 均 fail-closed。恢复只发布到不存在目标，先完成 staging 复核，删除全部旧 Session 后再做允许差异复核。
- `cmd/recovery-exercise`、`tools/run-backup-exercise.mjs`、回环 synthetic Provider 与证据合并器已实现固定 1,000 Document 形状、exercise/model/mode/instance/计数绑定、同一 `backup_set_id`、不可重置 RTO、旧 Cookie 拒绝、任务继续前的恢复查询/下载、稳定行摘要、租约接管和唯一闭合 Fact 链；所有写阶段先排他保留 owner-only 结果，完整状态只写受保护文件，终端只输出安全聚合或分类码。
- 固定 Go 1.26.7 禁网容器中的全量 `go test -p=1 -count=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 已通过；Web `npm run check` 的契约生成、类型检查、Lint、格式、9 个 Vitest 文件共 38 项测试和生产构建已通过。
- 恢复工具的 3 个 Node 测试文件、14 项逻辑测试已通过；关键不变量 136 / 136（100%）通过，其中新增 23 条备份、激活、运行锁、恢复增量、输出占位与隐私映射。领域/应用层覆盖率 85.71%（3,101 / 3,618），基础设施/传输层覆盖率 75.18%（3,625 / 4,822），均达到门槛。
- 经产品负责人单独授权，完整演练只在 `/tmp` owner-only 隔离目录创建一次性随机主密钥、Owner 密码和 synthetic Provider key；回环 API 与 Provider 固定监听 `127.0.0.1`，邮箱身份使用 `.invalid`，没有调用真实 Provider、邮箱、云服务或外部账号，也没有下载依赖、镜像或模型。
- 备份前离线快照精确得到 1,000 个 Document、1,000 个 Job、998 个 `provider_config_missing` 失败 Job、2 个 DocumentPage、1 个已确认 Fact、唯一旧 `running` AiRun 与唯一旧 `succeeded` AiRun；稳定既有行摘要覆盖 2,039 行。认证清单记录 1,004 条对象引用、1,003 个唯一物理对象和 37 张数据库表。
- 认证 backup、首次时钟后的独立 verify 与全新目标 restore 分别用时 738、317 与 1,356 ms；三者绑定同一随机 `backup_set_id`，恢复前 3 个旧 Session 全部删除。恢复 API 先证明 ready、旧 Cookie 拒绝、新登录、3 个 Document 查询与 5 个鉴权下载，再形成目标 Job 仍为 `processing` 且 attempt 未变化的读取屏障。
- 完整 RTO 为 115,291 ms，低于 1,800,000 ms 门槛。旧 AiRun 全库唯一收口为 `failed/lease_expired`，目标 attempt 从 1 增至 2、version 按唯一正常路径增加 3；最终 Fact 从 1 增至 2，稳定既有状态保持不变，append-only 变化只覆盖唯一闭合 Claim→Review→Payment 链。
- 最终固定 Go 1.26.7 禁网容器的全量测试、`go vet` 与构建，Web 9 个测试文件/38 项测试及生产构建，3 个恢复工具测试文件/14 项逻辑测试、136 / 136 关键不变量、85.71% 与 75.18% 两层覆盖率均通过。受保护结果只用于严格证据合并；正式证据不含路径、ID、哈希、Cookie、凭据、原始响应或业务字段。

可机读安全聚合见 `tests/evidence/m4/backup-restore-gate-summary.json`。本切片已收口；下一切片按路线图冻结 M4 运行质量、性能、安全、可访问性与本地发布准备。真实模型正式评测、真实外部系统联调、部署和生产发布仍保持单独门禁。
