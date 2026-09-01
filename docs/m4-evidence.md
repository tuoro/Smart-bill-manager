# M4 验收证据

状态：本地范围与单机公开实测分发已完成；真实模型评测、真实外部联调和生产部署未执行
日期：2026-09-01

## 当前权威结论

M4 当前基线以 ADR-0020、ADR-0019、ADR-0021 和 ADR-0022 为边界：PostgreSQL 17 是唯一关系数据源，本地对象文件存储保持不变。系统不读取或迁移 SQLite 数据，不保留 SQLite 驱动、生产适配器、迁移、运行配置、数据库卷、备份入口、运行时选择、双写或第二测试数据源；新架构公开版本之间则使用连续 PostgreSQL Schema migration 保留数据前向升级。

历史 SQLite 洞察与恢复切片曾于 2026-08-31 通过，但已被 ADR-0020 替代，只保留为决策历史，不能作为当前发布候选证据。当前 PostgreSQL 结论来自本轮重新实现和 final13 候选的完整重新验收。

## PostgreSQL 唯一持久化

- Clean Slate `infra/migrations/0001_initial.sql` 从空 PostgreSQL 17 数据库创建完整 Schema，不含旧数据库升级或导入路径。
- `pgx/v5` 是唯一关系数据库驱动；唯一适配器覆盖身份、Document、Job、Provider、Claim、Review、Fact、重复检测、分配、邮件、Trip、Reimbursement、Insights、删除和审计。
- 显式迁移身份与运行身份分离。API 运行角色没有 DDL 权限；密码只从受保护文件读取；数据库不发布宿主端口。
- 多查询读取使用 PostgreSQL `REPEATABLE READ READ ONLY` 快照；关键写入使用显式行锁、约束、事务隔离和稳定冲突映射。Worker 使用 `FOR UPDATE SKIP LOCKED` 安全竞争，不依赖进程内锁或 SQLite 全库锁。
- 重复检测保持索引缩小候选；Insights 在数据库内聚合整数最小单位并使用 keyset 分页，没有恢复全量载入内存、统计副本或第二数据源。

## PostgreSQL 认证备份与恢复

认证备份只包含固定 PostgreSQL 17 `pg_dump` 自包含 dump、精确对象集合、清单与 HMAC；主密钥和数据库凭据与数据包分离。恢复只写全新数据库和对象目标，核对迁移、Schema、约束、表集合、审计链和对象集合后失效全部旧 Session，并保留唯一恢复任务的确定性续跑边界。

本轮 owner-only `/tmp` 隔离演练使用纯合成数据，结果如下：

- 精确 1,000 个 Document、1,000 个 Job、1,004 条对象引用、1,003 个唯一物理对象和 37 张关系表；
- backup 828 ms、独立 verify 133 ms、restore 1,364 ms；
- 3 个旧 Session 全部失效；恢复后原有稳定状态保持不变，append-only 增量只属于唯一闭合恢复链；
- 目标 attempt 只增加 1，version 只沿正常链增加 3，旧 `running` AiRun 唯一收口为 `failed/lease_expired`，最终没有遗留 running Job；
- 完整 RTO 824,830 ms，低于 1,800,000 ms 门槛；时钟先于独立 verify 启动，并覆盖 verify、全新目标创建、restore、应用启动、只读基线、任务续跑和最终数据库复核。

安全聚合见 `tests/evidence/m4/backup-restore-gate-summary.json`。dump、数据库、对象、清单全文、路径、标识、Cookie、凭据、Provider 原始响应和日志均不进入仓库。

## final13 本地发布候选

最终候选绑定构建前基线 HEAD、确定性发布输入摘要、镜像 ID 与两份 Compose 规范摘要；证据提交本身不冒充尚不存在的提交身份。唯一发布入口为 `infra/docker/app.Dockerfile`、`infra/docker/entrypoint.sh`、`infra/compose/compose.yaml` 与 `infra/compose/.env.example`。

最终串行低内存门禁结果：

- Go 全量测试、`go vet`、构建、Node 工具测试、Web 契约生成/类型/Lint/格式/38 项测试/生产构建全部通过；
- 关键不变量 140 / 140；领域/应用覆盖率 85.69%（3,157 / 3,684），基础设施/传输覆盖率 73.77%（3,727 / 5,052）；
- 镜像、Compose、精确资产、工具链/包管理器缺席、entrypoint 7 个失败边界、首次 Owner 初始化、认证、最小权限和凭据扫描全部通过；
- 10,000 Fact 数据集的六个非 AI JSON API p95 分别为 31.00、40.44、34.85、32.85、37.75、192.61 ms，均不超过 300 ms；Document 创建 10.43 ms、审核确认 26.93 ms，均不超过 500 ms；
- 50 个测量 Job 的 RSS 首末窗口中位数比为 0.1614，线性斜率为 -3.1014 MiB/Job，无孤立 `processing` 或 `cancel_requested` Job；
- Playwright 6 个 spec、37 个场景全部通过；四页各三次 Lighthouse 的最低 Performance 与 Accessibility 均为 100；正式响应式 16 / 16、等效 200% 回流 16 / 16、键盘与深色主题全部通过。

安全聚合见 `tests/evidence/m4/local-release-readiness-gate-summary.json`。原始报告、运行标识、业务 fixture 和临时凭据只在本轮受保护隔离区中复核，并在收口后销毁。

## v0.3.1 单机公开实测分发

- 已验收候选以 `ghcr.io/tuoro/smart-bill-manager:v0.3.0` 发布；公开 manifest digest 与本地镜像 ID 均为 `sha256:83bd3c795b3a7c2413a8f80279ab3fc8b9787e0e9b02cab5b08711c61d3ba6d1`，空 Docker 配置可匿名按 digest 拉取；未发布 `latest`。
- `v0.3.0` Git Tag 保持不可变；`v0.3.1` 只交付分发与文档补丁，固定上述应用 digest 和已验收 PostgreSQL 17 digest，不改变业务源码、Schema、API、Web 或 M4 发布输入摘要。
- 发布 overlay 的 10 项静态边界全部通过：本地 build 段退出最终配置，三个应用角色共享同一固定镜像，数据库无宿主端口，应用只绑定 `127.0.0.1`，internal 网络、只读根和 bootstrap 专用 Owner secret 保持不变。
- 凭据准备器的成功、相对路径、既有目标和仓库内目标边界通过；五份随机 secret 彼此独立、目录 `0700`、文件 `0600`，值未进入输出或环境文件。
- 从空目录、空数据库卷和空对象卷完成 PostgreSQL 健康、角色 provision、Clean Slate `0001` migration、唯一 Owner bootstrap、API ready、登录、双 Cookie 会话、退出和旧会话 `401`；一次性 Owner 文件在成功后删除。
- 15 个 Node 工具测试、两份 shell 语法、30 个 README/部署文档本地链接和 `git diff --check` 通过。冒烟容器、网络、两个卷、一次性凭据与 Docker 登录目录均已销毁。

安全聚合见 `tests/evidence/m4/self-hosted-prerelease-gate-summary.json`。它只记录镜像身份、布尔门禁和数量，不保存路径、容器 ID、Cookie、密码、主密钥、数据库内容或原始响应。

## v0.3.2 通用 Docker 分发与前向升级

ADR-0022 在不改变业务镜像、API、Schema 或 Web 的前提下收敛了后续补丁版分发：GitHub Release 可以附带只含 12 个批准文件的最小 Docker 部署包和 SHA-256；不可变镜像身份由包内 `release.env` 提供，用户运行目录只保存非秘密配置、持久化路径和 secret 文件路径。调用方环境不能覆盖两个固定镜像摘要。

新安装创建 owner-only 的 `data/postgres`、`data/objects` 与 `backups`，Compose 展开后两个数据源都是精确宿主 bind。缺少新存储变量的 `v0.3.1` 配置仍展开为原 `sbm_postgres_data` 与 `sbm_objects` named volume；工具不自动复制、转换或删除既有卷。

隔离 Docker 冒烟使用现有固定应用与 PostgreSQL 17 镜像，从空 bind 目录完成角色创建、Schema 初始化、唯一 Owner、ready 和登录；随后在同一数据库与对象目录执行 `upgrade --backup-confirmed`，再次 ready 和登录成功，一次性 Owner 密码已删除。第一次收口的宿主 `find` 断言因容器按最小权限接管 bind 目录而失败，运行与升级没有失败；验证没有放宽目录权限，改为从容器 Mount 身份核对精确 bind，并使用固定本地镜像执行受控临时目录归属恢复后完成清理。该失败保留在安全聚合中。

16 个 Node 工具测试文件、3 份 shell 语法、33 个本地文档链接、确定性压缩包、SHA-256 sidecar、调用方镜像覆盖拒绝、`git diff --check`、疑似凭据与大文件检查均通过。验证前后可用内存分别为 8.1 GiB 与 8.2 GiB；没有遗留临时容器、网络或凭据目录。

安全聚合见 `tests/evidence/m4/docker-forward-upgrade-gate-summary.json`。本切片未下载新镜像或依赖，未调用真实 Provider、邮箱、外部账号或远程数据库，也未推送、部署或发布远端制品。

## v0.3.3 一条命令与三种部署形式

ADR-0023 在同一 Compose 契约上增加引导式安装入口：PostgreSQL 与应用继续分容器，用户可分别选择全新的数据库、对象和备份绝对目录；安装器复用现有准备器和部署 wrapper，按 `pull -> bootstrap -> start -> status` 执行。单行入口从固定版本 Tag 取得脚本，在 owner-only 临时目录校验同版本 Bundle 后才进入交互；README 同时给出 Compose 和仅适用于已初始化应用容器的 `docker run` 形式，不增加第二完整安装链。

本版本只更新分发脚本、文档和 Bundle allowlist，应用镜像继续固定到已通过 M4 门禁的同一 manifest digest。脚本、成功/摘要失败、Compose 规范化、13 文件 Bundle、文档和资源清理证据见 `tests/evidence/m4/guided-installer-gate-summary.json`。

## 范围与剩余门禁

本分发切片未调用真实 AI Provider、未发送真实图片、未连接真实邮箱、云服务、外部账号或远程 PostgreSQL，未执行正式模型正确率评测，也未部署生产服务器。经产品负责人明确授权，只创建公开 GHCR 版本化镜像、源码补丁 Tag/Release 和对应仓库更新；未发布 `latest`、未创建云运行资源。已披露的早期工具网络策略偏差继续保留在最终安全聚合中，不能改写为整个开发过程从未访问外网。

M0～M4 的本地产品功能、本地发布准备和单机公开实测分发现已完成。下一步只能在新的明确授权下执行真实模型正确率正式评测；真实外部系统联调与生产部署仍是后续独立门禁。
