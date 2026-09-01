# M4 验收证据

状态：本地范围已完成；真实模型评测、真实外部联调和生产发布未执行
日期：2026-09-01

## 当前权威结论

M4 当前基线以 ADR-0020 和 ADR-0019 为边界：PostgreSQL 17 是唯一关系数据源，本地对象文件存储保持不变。系统不读取或迁移 SQLite 数据，不保留 SQLite 驱动、生产适配器、迁移、运行配置、数据库卷、备份入口、运行时选择、双写或第二测试数据源。

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

## 范围与剩余门禁

本轮未调用真实 AI Provider、未发送真实图片、未连接真实邮箱、云服务、外部账号或远程 PostgreSQL，未执行正式模型正确率评测，未部署、发布、推送、打 Tag 或创建远端资源。已披露的早期工具网络策略偏差继续保留在最终安全聚合中，不能改写为整个开发过程从未访问外网。

M0～M4 的本地产品功能和本地发布准备现已完成。下一步只能在新的明确授权下执行真实模型正确率正式评测；真实外部系统联调与生产部署/发布仍是后续独立门禁。
