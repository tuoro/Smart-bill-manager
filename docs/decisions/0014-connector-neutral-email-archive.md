# ADR-0014：连接器中立的邮箱 Source 与不可变邮件归档

状态：已接受
日期：2026-08-31

## 背景

M3 需要把邮箱中的邮件与附件纳入现有 `Source -> Claim -> Fact` 链路，但当前持续开发授权明确排除真实邮箱账号、凭据和外部联调。邮箱连接器如果直接写 Document、Job 或对象文件，会形成第二条上传链路；如果把邮件正文交给模型，既扩大隐私面，也偏离“原图直接交给多模态模型”的活动契约。

首切片因此必须先完成可在纯合成、本地输入上验证的领域、数据库、API 与 Web 能力，同时把真实 IMAP 连接、认证和轮询留在明确的外部集成门禁之后。

## 决策

### Source 描述符不含凭据

1. `EmailSource` 只保存租户、显示名、规范邮箱地址、IMAP 主机、端口、传输安全模式、状态、创建者和时间。传输安全只允许 `implicit_tls` 或 `starttls`，不支持明文连接。
2. 创建来源时不接受、生成、读取或保存密码、OAuth Token、Cookie、客户端秘密、密文或密钥引用；不存在空的凭据列或临时明文配置。
3. 新来源状态为 `pending_connection`。只有连接器中立的归档用例成功保存过至少一封邮件后才派生为 `active`；这不表示真实邮箱联调已通过。
4. `email-source-registration/1` 对规范化后的完整描述符计算请求 hash。`tenant_id + Idempotency-Key` 永久唯一；相同键与相同请求返回同一 Source，同键改请求冲突。相同租户、邮箱、主机、端口与安全模式的身份唯一，不静默合并。

### 原始邮件与附件是不可变 Source

1. 未来连接器只调用内部 `email-message-archive/1` 应用端口，不直接访问 SQLite、对象存储、Document 或 Job。当前切片不提供 HTTP、CLI 或生产 fixture 导入入口，也不实现网络拨号、IMAP 命令、轮询或调度。
2. 连接器为每封邮件提交一个 64 位小写十六进制 `external_message_key`，它必须由稳定的服务器身份元组确定性生成，不能包含可逆 UID、文件夹名、邮箱地址或凭据。`email_source_id + external_message_key` 唯一。
3. 原始 RFC 822 字节以 `message/rfc822` 对象保存并计算 SHA-256。相同外部键与相同原文 hash 是严格幂等重放；相同键对应不同字节时返回 `email_message_identity_conflict`，绝不覆盖旧 Source。
4. 原始邮件最大 32 MiB；超过大小上限时零写入并返回明确错误，因为系统不能安全声称已完整归档。大小合法时，MIME 深度最大 10、总 part 最大 200、具名或 `attachment` disposition 的附件最大 50；超过结构边界时归档完整原始邮件并把消息标记为 `blocked`，不截断、不猜测附件，也不创建 Document/Job。
5. 标准库 MIME 解析器只解码边界、传输编码、受限头字段和附件字节；不执行 HTML、不加载远程资源、不展开压缩包、不解释邮件正文业务语义。正文只存在于原始邮件对象中，不复制到数据库、API 列表、日志或模型请求。
6. 每个附件保存稳定 part 序号、安全文件名、声明 MIME、decoded 字节大小、SHA-256、处理状态和可选 Document ID；非空 decoded 字节另存不可变对象，空附件以明确原因保留在原始邮件 Source 中。附件不是模型结果，也不能被新邮件覆盖。

### 唯一 Document/Job 链路

1. 只有 JPEG、PNG、WebP 或 PDF 且 decoded 字节不超过 20 MiB 的附件才进入既有签名/MIME/PageCount 检查。通过后复用现有 Document 与 `document_process` Job，不创建邮件专用 AI Job、Prompt、Claim 或 Fact 分支。
2. 不支持、空、过大、名称非法、签名/MIME 不一致或损坏的附件保持 `archived_only` 并保存稳定安全原因；该项不阻止同一邮件的其他合法附件入队。
3. 同租户附件 SHA-256 命中已有 Document 时，附件链接该 Document 并标记 `existing_document`，不创建第二个 Document/Job，也不把重复伪装成新任务。
4. 新邮件的消息、全部附件、所有新 Document/Job 和一条安全 AuditEvent 在单一数据库事务中写入；随后提交原始邮件与附件对象。任一对象提交失败时补偿整个新消息聚合和本轮新 Document/Job，并删除已提交对象；既有 Document 不受影响。
5. 邮箱附件创建的 Document 使用来源创建者作为已授权摄取主体，并显式标记 `email_attachment` 来源。其原件对象由 EmailAttachment 持有；删除未确认 Document 时只移除 Document/Job/派生页并断开附件链接，不删除邮件归档对象。手工上传继续标记为 `upload` 且保持原有删除语义。

### API、权限与 Web

- `POST /api/v1/email-sources` 只注册无凭据描述符；`GET /api/v1/email-sources` 列出来源和安全计数。
- `GET /api/v1/email-sources/{source_id}/messages` 使用稳定游标分页列出归档状态、受保护的主题/发件人、时间和附件摘要；不返回正文、原始头、存储键或外部消息键。
- `GET /api/v1/email-messages/{message_id}/raw` 下载原始 `.eml`；`GET /api/v1/email-attachments/{attachment_id}/content` 强制以附件下载。两者均为租户隔离、`private, no-store`，且不允许浏览器执行归档 HTML。
- `email_sources.manage` 只授予 `owner`；`email_archive.read` 授予 `owner` 与 `finance`。`reviewer` 仍只能沿当前审核权限读取对应 Document 证据，`viewer` 不能枚举邮件归档。
- 邮箱来源页面只包含无凭据注册表单、来源状态、分页邮件列表和逐附件文字结果。`pending_connection` 必须明确表示尚未连接，不得伪装成功；页面不出现密码、Token、OAuth 或“立即同步”控件。

## 失败与隐私边界

- 解析失败、边界超限和单附件失败必须返回稳定代码并在 UI 显示安全文字；不得把原始头、主题、地址、正文、文件内容、服务器响应或完整 MIME 错误写入日志和 AuditEvent。
- `email_source_registered` AuditEvent 只记录协议与状态；`email_message_archived` 只记录消息状态、附件总数及 `queued`、`existing_document`、`archived_only` 数量。
- 原始邮件、附件、主题和地址属于租户私有数据，只存在于本地对象存储/数据库及授权响应；测试固定使用 `.invalid` 地址和纯合成 MIME 字节。
- 本切片不调用模型。后续 Worker 只对合法附件 Document 的原始图片/PDF 执行现有“最小中文票面文字 -> 本地确定性 Mapper”链路。

## 非目标

- 真实 IMAP/POP3/Graph/Gmail API 连接、DNS/TLS 握手、OAuth、密码、Token、Cookie、密钥管理和账号联调；
- 轮询计划、服务器游标推进、删除/移动/标记远端邮件或发送邮件；
- 解析正文中的账单字段、链接抓取、压缩包展开、邮件模板规则或模型读取正文；
- 邮件自动创建 Fact、自动确认附件、旧数据迁移、旧 API 兼容或运行时 fixture 导入；
- Trip、Reimbursement、行程归属和报销冲突提示，它们属于后续 M3 切片。

## 验证要求

- 领域与解析测试覆盖 Source 规范化、幂等 hash、外部键、头字段安全、嵌套/part/附件边界、编码附件、非法 MIME 和无正文依赖。
- SQLite/对象集成测试覆盖原始邮件与附件不可变、合法项入队、单项隔离、精确重复 Document、消息重放/身份冲突、跨租户、对象提交补偿、邮件拥有对象的 Document 删除语义和安全审计。
- HTTP/OpenAPI 测试覆盖严格 JSON、CSRF、权限矩阵、游标、原始邮件/附件强制下载与不泄露字段；Web 覆盖 pending/active、空/blocked/混合附件、分页、权限、键盘、768px 与 384px 回流。
- 完整门禁继续包含后端测试/vet/build、生成客户端、Web check、浏览器场景、关键不变量、覆盖率、敏感信息、临时产物与进程残留审查。
