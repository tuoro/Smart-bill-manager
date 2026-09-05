# 本地运行与发布候选运维说明

状态：当前按 v0.4.0 业务工作流预发布范围验收与分发；支持单机公开实测，不代表生产部署。具体候选身份与验收结果以对应 Release 和安全聚合证据为准。

普通使用者应先阅读 [Docker 单机自托管部署](deployment.md)。本文件保留候选构建、深度诊断、恢复和验收细节，不是首次安装的最短路径。

本说明只适用于 `rebirth` Clean Slate 系统。唯一入口是 `infra/docker/app.Dockerfile` 与 `infra/compose/compose.yaml`；旧应用、旧数据库、旧 Compose、旧任务状态和旧架构数据迁移均不受支持。该边界不禁止新架构 PostgreSQL 的连续 Schema migration；认证备份与恢复的完整不变量见 `docs/backup-restore.md`。

## 制品与配置身份

1. 构建前记录当前 40 位小写 Git HEAD；工作区最终证据提交会晚于镜像构建，因此不能把尚不存在的最终提交 SHA 写成镜像身份。
2. 用 `node tools/check-release-image.mjs digest --repository-root <rebirth-absolute-path>` 计算发布输入摘要。摘要覆盖实际镜像输入，证据、文档和原始报告不进入该集合。
3. 将 HEAD 与 64 位发布输入摘要分别写入仓库外的运行环境文件，字段为 `SBM_BUILD_SHA` 与 `SBM_RELEASE_INPUT_SHA256`。其他字段从 `infra/compose/.env.example` 复制后按本机情况填写；不得修改或提交示例文件来保存本地值。
4. 用 `docker image inspect` 核对本机 `golang:1.26.7` 与 `golang:1.26.7-alpine3.23` 的固定 image ID；只能在 ID 精确匹配后，用本地 `docker tag` 将前者别名为 `smart-bill-manager:go-glibc-source-local`。不得 `pull`、解析新标签或从其他镜像替代。
5. 在 owner-only `/tmp` 隔离父目录中运行 `node tools/prepare-local-release-artifacts.mjs --output-directory <new-artifact-dir> --expected-head <HEAD> --expected-release-input-sha256 <digest> --npm-cache <existing-npm-cache-root> --go-module-cache <existing-complete-pkg/mod> --poppler-bundle <existing-audited-poppler-bundle-root>`。准备器会在独立工作区执行固定 Node 24.19.0 的 `npm ci --offline` 和生产构建，在固定 Go 1.26.7 禁网容器中执行 `go mod verify` 与八个二进制构建（七个运行 CLI，另一个 `recovery-exercise` 仅供演练，不进入发布镜像），并校验 Poppler 26.05.0 manifest、来源 SHA 和逐文件 SHA-256；它不会复用现有 `node_modules`。将成功目录写入仓库外环境文件的 `SBM_RELEASE_ARTIFACTS_SOURCE`。
6. `SBM_MASTER_KEY_SOURCE` 必须指向仓库外、owner-only、单硬链接的普通文件。允许 32 字节原始值、64 位十六进制或 padded base64。Owner 密码与 synthetic Provider key 必须使用彼此独立的文件，不能复用主密钥或彼此复用。
7. 当前验收只复用本机已有的固定镜像、Poppler bundle 和依赖缓存。Compose 构建网络为 `none`；IANA 时区库只从已固定 image ID 的 glibc 来源镜像复制，至少核验 `Asia/Shanghai` 与 `zone.tab`。Dockerfile 会复核运行 contract、产物身份、工具来源、精确文件清单、全部 SHA-256 和实际 PDF 工具版本；缓存或产物不完整时必须失败并另行申请下载授权，不能临时改成联网构建。

构建完成后必须用 `tools/check-release-image.mjs check` 核验标签、必需/禁止资产、Go/Node 工具链与包管理器缺席、Compose 规范化配置和 acceptance 内部网络。正式原始报告只允许写入 `/tmp` 下 owner-only 隔离目录。

## 首次 Owner 初始化

空数据库只能由镜像内 `/app/bootstrap-owner` 初始化一次。密码只通过外部 owner-only `/run/secrets/sbm_owner_password` 文件传入；本地 Compose 的文件型 secret 不支持 `uid/gid/mode` 重映射，因此 root entrypoint 只在该一次性命令下把密码安全材料化为 UID/GID 10001、`0600` 的 `/run/sbm-secrets/owner-password`，正常 server 启动不会创建该文件。密码值不得出现在命令参数、环境变量或日志中。标准顺序是：

1. 在应用未运行时执行一次 Compose `run --rm --no-deps --pull never app /app/bootstrap-owner`，显式提供数据库、迁移、邮箱、显示名、租户名、币种、时区和材料化密码文件路径 `/run/sbm-secrets/owner-password`；
2. 在仍未启动应用时立刻重复同一命令，确认非空数据库被拒绝且没有新增第二个 Owner；
3. 使用 `docker compose up -d --no-build` 启动当前镜像；
4. `/api/v1/ready` 返回 `200` 后，用 Owner 完成一次登录、当前会话读取、退出和旧会话 `401` 验证。

`tools/run-bootstrap-owner-gate.mjs` 与 `tools/check-release-runtime.mjs` 固化了上述安全检查。它们只接受限定的本地 project 身份、回环 URL、受保护文件和 synthetic exercise；输出冲突、远端 URL、未知参数或宽权限目录都会在业务写入前失败。

## 日常启动与停止

- Compose 默认只发布 `127.0.0.1:8080`。除非另有 TLS、反向代理和生产授权，不得改成全接口监听。
- `local` 模式只用于回环 HTTP，允许非 Secure Cookie；任何经网络暴露的运行必须使用 `production` 且 `SBM_COOKIE_SECURE=true`。本切片不提供 TLS、域名或部署授权。
- 正常停止使用 Compose stop/down，不附带 `--volumes`。20 秒宽限期用于 API 和 Worker 收敛；超时或强杀后先检查 `processing`、`cancel_requested`、运行锁和恢复状态，再决定是否重启。
- 应用根文件系统只读；应用进程的合法写入仅限数据库卷、对象卷和 `/tmp`。`/run/sbm-secrets` 仅允许 root 入口写入，必须为 `root:sbm 0710`，应用进程只能穿越且不可列出、创建、替换或删除文件。应用进程应为 UID/GID 10001，主密钥材料化文件应为单硬链接 `0600` 且归该用户所有；原始只读 secret 对应用用户不可读。

## 健康与故障诊断

按以下顺序定位问题，不用重试循环掩盖根因：

1. `docker compose ps`：确认 app 为 healthy、Provider 仅在 acceptance overlay 中运行；
2. `/api/v1/ready`：区分数据库不可用与 Job 调度器未就绪；
3. 容器配置：核对镜像 ID、发布输入标签、只读根、capability、资源上限、tmpfs 和两个持久卷；
4. 入口分类码：`master_key_source_invalid`、`master_key_source_permissions`、`master_key_format_invalid`、`master_key_target_*` 与 `data_directory_*` 必须按对应文件或挂载修复，不能复制宽权限密钥绕过；
5. 数据库 `sbm_restore.state` 与对象根 `restore-identity.json`：阶段仅存数据库，身份文件只负责配对；`incomplete`、损坏、错配或孤立身份均阻止启动，不得手工删除/改写为 complete。失败恢复应保留离线诊断并使用全新目标重试；
6. 最小化查看最近日志，只在交互终端中使用，不把完整日志持久化到仓库或验收证据。日志中发现凭据或真实财务字段时立即停止、隔离并按安全事件处理。

Synthetic Provider 在 acceptance overlay 中与 app 共享网络命名空间，只监听 `127.0.0.1:19086`；其既有固定 Node 镜像必须通过不可变 image ID 门禁，该 overlay 的默认网络为 internal，不能访问外网。Provider 在 app 容器启动后立即启动，app 的 acceptance 启动屏障会在 Provider `/healthz` 可用后才执行 Server，避免恢复中的过期租约在本地 Provider 尚未就绪时消耗冻结的单次重试；30 秒内未就绪则明确启动失败。模型必须是 `synthetic-*`，exercise 必须为本轮 UUIDv4。任何真实 Provider、真实邮箱或真实图片发送仍需重新授权。

Linux Docker 的 internal bridge 不激活声明的宿主端口映射。正式本地验收必须在 app healthy 后，用 `tools/run-acceptance-loopback-bridge.mjs` 核对候选容器、不可变镜像 ID、唯一 internal 网络、未激活的 Docker 映射和私有容器地址，再建立 `127.0.0.1:8080` 到该 app 的进程内 TCP 桥。桥接器不接收凭据、不解析或记录流量、不做 DNS 解析且不监听非回环地址；停止验收项目时必须同时停止桥接器。常规 base Compose 使用非 internal 默认网络，其原生回环端口映射不需要该验收桥。

浏览器质量门禁只接受回环 HTTP origin，关闭 Chrome 后台网络服务，并把非回环 HTTP(S) 请求送往本地不可用代理；Node 质量客户端拒绝重定向。内存门禁只采样 UID/GID 10001 且 argv[0] 为 `/app/server` 的实际宿主进程 PID，不能把 Compose `init: true` 对应的容器 init PID 或其他稳定进程代替；门禁用精确候选容器的 `docker top` 绑定该宿主 PID，并在同一容器内以 UID/GID 10001 核对 server 身份及其与容器绑定 exec 的 PID namespace。该方式不要求给入口 root 补回 ptrace capability。60 个输入必须是具有不同确定性高对比视觉指纹的有效 synthetic PNG，Provider 同时返回不同 merchant 文本，避免内存协议被近似文件或字段组合候选上限提前阻断；这不是放宽重复检测。同轮 Provider `/metrics` 只能由门禁通过已核验 app 容器的内部回环读取，不能向宿主发布端口，并且必须精确聚合 1 次 capability probe 和 60 次 extraction 的样本数、p50、p95、max，不保存请求或响应明细。

## 容量、备份与升级

本地 Go 回归使用低并发和每个测试进程 60 秒超时；审核模块增长后可按 `go test -list` 的完整顶层清单拆成最多 20 项的串行批次。必须核对全部测试恰好执行一次、不遗漏或跳过，并将所有批次的语句覆盖取并集后执行原 85% / 70% 门槛；不能通过延长卡住用例、减少断言或缩小数据集获得通过。

- 新安装分别监控运行目录下的 `data/postgres`、`data/objects`、`backups` 和宿主可用空间；既有 `v0.3.1` named volume 部署继续监控对应数据库卷与对象卷。任一数据位置达到 80% 使用率时停止大批量导入并先扩容或清理已获批准的历史备份。不得从对象目录手工删除文件释放空间。
- 升级前停止全部写入者，并使用镜像内 `/app/backup backup` 创建新的认证数据包；随后用独立主密钥副本执行 `/app/backup verify`，并用同版本 `backup restore` 在一次性空数据库、对象根及独立密钥目标完成语义恢复核对。只有包认证通过不等于独立验证完成，未完整验证的数据包不得作为回滚点。
- 新架构版本升级默认保留数据库与对象；`upgrade --backup-confirmed` 只刷新角色、按版本顺序事务化执行尚未应用的 PostgreSQL migration，并在全部成功后启动 app。它不接受旧数据库、不复制对象、不删除当前数据，也不自动回退。
- 数据库、对象和主密钥必须分别托管；备份包不含主密钥。ProviderConfig 删除后的旧密文备份最长保留 30 天。
- 恢复只写全新目标，成功后全部旧 Session 失效。详见 `docs/backup-restore.md`，不得省略 HMAC、迁移、Schema、对象精确清单、完整性、外键和恢复状态检查。

## 回滚边界

本地回滚只允许选择同时满足以下条件的组合：同一 Clean Slate 数据契约、已验证镜像、与该数据状态匹配且独立验证通过的认证备份、对应独立主密钥。回滚前先保存当前认证备份；恢复到新目标并验证后再切换。不得读取或转换旧系统数据，不得增加旧 Schema 探测、双写或兼容入口。

若新镜像引入迁移，而旧镜像不能读取迁移后的数据，只能恢复升级前备份，不能让旧二进制直接打开新数据库。真实灾难切换、非零 RPO、远端存储、TLS、部署与正式发布继续留在生产门禁外。

## 证据与清理

正式本地验收的性能、内存、Lighthouse、响应式、Playwright、镜像和运行时原始报告只存在于本轮 `/tmp` 隔离目录，权限为目录 `0700`、文件 `0600`。严格合并器只输出批准的数字、布尔值和制品身份；仓库不得包含路径、Cookie、凭据、业务 ID、原始日志或展开的 Compose。

验收结束后先停止并删除仅属于本轮 project 的容器、网络和卷，确认无残留进程，再销毁本轮凭据、数据库、对象、浏览器产物和原始报告。只允许删除本轮明确创建且已核实的目标；不得使用 `git clean`、宽泛 glob 或未解析变量。
