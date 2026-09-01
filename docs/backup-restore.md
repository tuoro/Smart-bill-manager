# PostgreSQL 备份、验证与恢复说明

状态：已按 ADR-0020 冻结目标；以当前安全聚合证据记录 PostgreSQL 工具与 1,000 Document 演练结果

本说明只覆盖当前 Clean Slate PostgreSQL 17 系统，不读取、转换或恢复旧数据库、SQLite 文件、旧对象布局、旧任务状态或历史清单。历史 SQLite 实现与演练结果只保留在 ADR-0018 和 `docs/m4-evidence.md`，不得用于当前操作。

## 恢复集合与保管边界

一次可恢复集合必须同时具备：

1. 数据备份包：固定 PostgreSQL 17 `pg_dump` 生成的自包含 dump、`objects/`、当前版本 `manifest.json` 和 `manifest.hmac`；
2. 与数据包分开保管的既有主密钥副本；
3. 独立托管、只在操作期间提供的数据库恢复凭据。

数据包不包含主密钥或数据库凭据，也不能独立通过认证或恢复。主密钥文件和数据库密码文件必须是 owner-only 普通文件，不得位于数据包、仓库、日志目录或构建上下文。

清单继续使用主密钥域分离派生的 HMAC-SHA-256，并带随机 `backup_set_id`。清单只记录安全可复核的数据库/对象集合身份，不记录密码、DSN、业务字段或原始工具输出。

## 当前工具边界

- 服务端、`pg_dump` 和 `pg_restore` major 版本固定为 17；版本不一致时失败。
- 数据库备份格式只允许 PostgreSQL 自包含 dump。禁止复制 PostgreSQL 数据目录、WAL、容器可写层或 Docker volume 作为应用级备份。
- 迁移集合、`schema_migrations`、当前 Schema/约束身份、表数量和审计链必须进入认证清单并在 verify/restore 后复核。
- 对象清单必须精确等于数据库引用的唯一物理对象集合；共享 key 去重但引用行数单独记录。
- 当前 CLI 与 Compose 只接受 PostgreSQL 17。不得执行历史 SQLite 命令或把历史 SQLite 演练作为当前恢复证据。

## 创建备份的冻结流程

1. 正常停止 app、Worker、Owner 初始化和所有会写入数据库或对象根的命令；确认数据库没有活动应用写连接。
2. 确认对象存储的 `staging/` 与 `trash/` 为空，并冻结当前精确对象集合。
3. 使用固定 PostgreSQL 17 工具和受保护密码文件，在不发布数据库宿主端口的内部网络创建自包含 dump。
4. 只读连接复核迁移、Schema/约束、表数量、审计链和对象引用；任何未知 Schema 对象、迁移漂移、孤立引用或非法持久化状态都失败。
5. 在 owner-only staging 中生成 dump、对象集合和认证清单；逐文件同步后才原子发布到原本不存在的备份目标。已有目标永不覆盖。

停写窗口内不得发生业务写入或对象删除；本地演练 RPO 固定为 0。失败后没有完整认证清单的数据目录不是有效备份。

## 独立验证的冻结流程

验证必须重新取得独立主密钥、当前迁移集合和受保护的只读数据库工具身份，按顺序完成：

1. 严格解析清单与标签并验证 HMAC；
2. 核对 PostgreSQL 17 工具、迁移集合、dump 哈希和对象文件；
3. 恢复到一次性全新验证数据库；
4. 核对 Schema/约束身份、表数量、审计链、租户边界和精确对象引用；
5. 销毁验证数据库和临时凭据。

旧版本、未知或尾随字段、路径越界、符号链接、特殊文件、重复/乱序记录、缺失/多余对象和哈希/大小不符全部失败。

## 恢复到全新目标的冻结流程

- PostgreSQL 数据库、对象根和恢复状态目标必须全新且为空；不允许覆盖、合并或回填已有数据库。
- 发布镜像中的 `/app/backup restore` 必须通过默认 entrypoint 以固定 UID/GID 10001 执行，使新对象树从创建起归运行身份所有。隔离演练若为调用 PostgreSQL 工具而显式绕过 entrypoint，必须在 app 启动前把本轮全新恢复树一次性归一到 10001:10001；不得把该步骤用于接管既有或归属不明的目录。
- 恢复先建立 durable `incomplete` 状态，再把 dump 恢复到全新数据库、对象恢复到同文件系统 staging，并完成全部离线复核。
- 只有迁移、Schema/约束、表数量、审计链、对象集合与清单全部一致后，才能删除全部 Session 并把恢复状态原子切换为 `complete`。
- `incomplete`、未知、损坏或与数据库身份不匹配的状态必须阻止 app 和 `bootstrap-owner` 启动。
- 旧 Cookie 必须失败；操作者使用原有独立登录凭据建立新 Session。恢复不会自动调用 Provider、邮箱或其他外部系统。
- restore 对快照中仍为 `processing` 的租约只把 `lease_expires_at` 推迟 120 秒，不修改 attempt、version 或 AiRun；该持久化宽限窗覆盖受限 Compose 启动与健康检查，用于在 Server 启动后完成只读基线验证，随后仍由既有过期租约与 `SKIP LOCKED` 竞争语义自动接管。

## M4 1,000 Document 重新演练

PostgreSQL 实现必须重新使用恰好 1,000 个纯合成 Document，保持历史演练已经冻结的对象数量、一个已确认 Fact、一个持有租约与 `running` AiRun 的目标 Job、会话失效、任务只续跑一次和稳定行摘要边界。

演练要求：

1. 所有一次性主密钥、Owner 密码、数据库密码和 synthetic Provider key 只存在于新的 owner-only `/tmp` 隔离目录；
2. PostgreSQL、app 与 synthetic Provider 只使用临时 internal 网络，数据库不发布宿主端口；
3. 创建数据包后启动不可重置的 30 分钟时钟，再执行独立 verify、全新恢复、旧 Cookie 拒绝、新登录、原快照查询和对象下载；
4. 形成任务尚未续跑的线性化屏障后，才允许 Worker 接管；旧 AiRun 只收口一次，Job attempt 只增加一次，并最终只新增一条闭合 Claim→Review→Payment 链；
5. 恢复完成总时间不超过 30 分钟，数据库和对象的非目标稳定摘要保持不变；
6. 通过后只写安全聚合证据，并销毁本轮凭据、数据库、容器、网络、卷、对象和原始报告。

## 保留与生产门禁

- 备份包、主密钥副本和数据库恢复凭据必须位于不同访问控制边界；任一部分不得进入源码、普通日志或公共制品。
- ProviderConfig 删除后，含旧密文的数据包最长保留 30 个日历日，到期按介质能力不可恢复销毁。
- 非零 RPO、在线备份、PITR、WAL 归档、HA、真实灾难切换、托管数据库和跨主机 TLS 属于后续生产发布门禁，本地演练不虚构这些能力。
- 真实恢复、真实账号、生产凭据、部署和发布仍需单独授权。

## 证据边界

当前 PostgreSQL 演练通过后，只提交安全聚合：构建/迁移/Schema 身份、PostgreSQL major、Document/对象/引用聚合数量、相等性布尔值、Session 失效数量、恢复后状态增量、RTO 毫秒和 `passed`。不得提交 dump、数据库、对象、清单全文、主密钥、数据库密码、DSN、Cookie、Provider key、业务字段、原始响应或日志。
