# ADR-0022：版本化 Docker 分发与新架构前向升级

状态：已实施

日期：2026-09-01

## 背景

`v0.3.1` 已提供公开应用镜像和可执行 Compose，但普通用户仍需取得完整源码树，发布镜像身份与用户配置写在同一个运行文件中，新安装的数据库与对象默认由 Docker named volume 托管，持久化位置和后续升级语义也不够直接。与此同时，Clean Slate 的“不迁移旧数据”只用于旧架构进入 `v0.3` 的一次性边界，不能被解释为新架构今后每次发布都清空用户数据。

## 决策

### 一次性 Clean Slate 与持续升级

- 旧代码、旧 API、SQLite、旧任务状态及其数据仍然没有导入或兼容路径。
- PostgreSQL Clean Slate `0001` 负责在空数据库创建初始结构；这属于数据库结构初始化，不是旧数据迁移。
- 从新架构公开版本开始，数据库结构变更必须通过连续、版本化、事务化的 PostgreSQL migration 前向升级，并默认保留现有业务数据。
- 已应用 migration 的版本、名称与内容摘要必须保持不可变；未知版本、内容漂移、缺号或失败 migration 必须阻止应用启动。不得以清库、静默回退、双写或兼容旧 Schema 代替前向升级。
- 升级前必须取得并独立验证认证备份。migration 失败时不写入版本记录，应用保持停止；回退需要使用升级前已验证备份，不能让旧镜像直接读取未来 Schema。

### Docker 分发

- GitHub Release 除源码外提供只含部署 allowlist 的 `smart-bill-manager-docker-<version>.tar.gz`。包内只包含 README、许可、部署/备份/运维文档、唯一 Compose 契约、发布 overlay、版本镜像清单和两个部署工具，不包含源码、测试、凭据或运行数据。
- `infra/compose/release.env` 是镜像与发布身份的唯一分发来源；用户运行目录中的 `deployment.env` 只保存非秘密运行配置、持久化路径和 secret 文件路径。部署工具以后者为基础、以前者覆盖镜像身份，升级部署包不会要求重建用户配置。
- 新安装默认使用宿主 bind 目录保存 PostgreSQL 数据和对象文件，并预留独立备份目录。准备器创建的目录与 secret 只允许 Owner 访问。
- 已有 `v0.3.1` 运行目录没有 `SBM_STORAGE_TYPE` 时继续使用原来的 Compose named volume；升级不得自动移动、复制或删除既有数据卷。
- 默认 Compose 自动部署内部 PostgreSQL 17、创建最小权限角色并初始化或升级 Schema。普通用户不填写数据库连接；外部 PostgreSQL、远程 TLS 和运行时切换数据库不属于本切片。

### 用户命令

- 首次 `bootstrap` 只用于空数据库：启动 PostgreSQL、创建角色、初始化 Schema 和创建唯一 Owner。
- 日常 `start` 不清空、不重建数据库。
- `upgrade --backup-confirmed` 只在操作者确认已有独立验证备份后执行：停止应用写入、启动数据库、刷新最小权限角色、顺序执行待应用 migration，再启动并等待应用就绪。
- `down` 仍只删除容器与网络；部署工具不提供删除数据库、对象目录或 named volume 的命令。

## 验收

1. 分发包清单严格等于批准 allowlist，重复生成内容确定，且不含源码、旧实现、测试、密钥或运行数据。
2. 新运行目录包含 owner-only 配置、`data/postgres`、`data/objects` 和 `backups`；展开后的 Compose 精确使用这些 bind source，数据库不发布宿主端口。
3. 缺少新持久化变量的既有运行配置展开为原 named volume；不得自动转换或删除卷。
4. 全新 PostgreSQL 完成结构初始化、Owner 创建、应用就绪和登录；保留同一数据库与对象目录再次执行升级流程后，已有 Owner 和持久状态仍可读取，migration ledger 不漂移。
5. 未显式确认备份、migration 失败、未知 migration 或未来 Schema 时升级失败且应用不启动。
6. README 明确数据库由 Compose 自动部署、持久化位置、备份集合、无损升级语义和旧架构一次性 Clean Slate 边界。

## 后果

普通用户不再需要完整源码即可部署；新安装的数据位置可直接识别和纳入宿主备份策略，新架构后续版本以保留数据的前向 Schema migration 为默认升级方式。真实外部数据库、生产 TLS、高可用和自动在线升级仍是独立范围。
