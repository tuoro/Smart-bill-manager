# M3 分切片验收证据

状态：首切片通过；M3 继续推进

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

## 实现证据

- `apps/api/internal/domain/email.go` 与 `apps/api/internal/adapters/emailmime/parser.go` 固定描述符、外部消息键、容量和安全 MIME 投影边界。
- `infra/migrations/0001_initial.sql` 直接定义 Clean Slate 的 EmailSource、EmailMessage、EmailAttachment、Document 摄取来源、不可变触发器及租户约束；不存在兼容迁移或第二数据源。
- `apps/api/internal/application/emails/service.go` 与 SQLite 事务适配器统一处理暂存、严格重放、逐附件隔离、精确 Document 复用和故障补偿；邮件附件拥有原件对象。
- HTTP/OpenAPI 只公开 Source 登记、归档读取和强制下载；响应不含正文、原始头、外部键、hash、存储键或凭据形状字段。
- Web 邮箱来源页覆盖 Owner 登记、pending/active、空邮件、blocked、混合附件、游标分页、失败重试、离线和角色拒绝，不提供密码、Token、OAuth、Cookie 或同步控件。

## 已执行验收

- 固定 Go 1.26.7 禁网容器：`go test -p=1 -count=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 全部通过；只读复用宿主既有模块缓存，未下载依赖。
- Web：`npm run check` 通过 OpenAPI 客户端生成、类型检查、ESLint、Prettier、6 个 Vitest 文件共 24 项测试和生产构建。
- 浏览器组件矩阵：`playwright test e2e/m1-state-matrix.spec.ts e2e/m3-email-sources.spec.ts` 21 / 21 通过；其中 3 项新增场景覆盖 Owner/Finance/Reviewer、无凭据登记、混合/blocked/分页/离线、键盘、768px 与 384px 等效回流。
- 关键不变量：83 / 83（100%）通过；新增 11 个映射覆盖描述符、MIME 安全投影与精确边界、归档生命周期、幂等并发、故障补偿、对象所有权、32 MiB 原文和 HTTP 隐私/权限边界。
- 领域/应用层语句覆盖率 85.61%（2,468 / 2,883，门槛 85%）；基础设施/传输层 73.88%（2,798 / 3,787，门槛 70%）。
- 首轮浏览器命令因临时 Vite 端口与项目固定基址不一致而在页面加载前失败；改用权威配置端口后 21 / 21 通过。该环境失败未改写为功能通过，失败截图与构建结果均作为本轮临时产物清理。
- 工作区和 55 个精确暂存文件的 diff 检查通过，最大暂存文件为 185,217 字节的既有锁文件；高置信密钥前缀、私有资产路径、生成/临时产物和项目进程残留检查均通过。宿主仅保留两个与本项目无关的 `shenlun` PostgreSQL 容器，本轮未触碰。
- 机读摘要见 `tests/evidence/m3/email-archive-gate-summary.json`；只记录纯合成身份、聚合门禁与安全元数据。

## 边界与下一断点

- 本切片未调用真实 Provider、未发送真实图片、未执行模型正确率正式评测，也未安装或恢复本地 OCR、模型缓存或新依赖。
- 未连接真实邮箱、云服务或外部账号，未创建或变更凭据，未推送、部署、发布或创建远端资源。
- 下一断点是先冻结 M3 行程归属切片的范围、数据模型、失败边界与验收标准，再实施领域、数据库、API、Web 和测试；真实邮箱连接继续需要重新授权。
