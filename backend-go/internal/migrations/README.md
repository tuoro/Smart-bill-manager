# 数据库迁移约定

数据库启动流程先读取已有的 `schema_migrations` 并拒绝程序不支持的未来版本，之后才允许 `migrateSchema` 新增表和列，最后由 `registeredMigrations` 严格执行一次性数据变换与索引调整。未来版本检查不得修改业务 schema，避免旧程序先改变新数据库再退出。

新增迁移时遵守以下规则：

1. 版本号使用 `YYYYMMDDNN`，并按升序追加到 `registeredMigrations`。
2. 已合并并发布的迁移不得改名、改版本或修改逻辑；修复必须新增迁移。
3. 每个迁移在独立事务中执行，任何步骤失败都不得写入 `schema_migrations`。
4. SQL 错误必须向上返回；只有明确标注为运行期兼容维护的任务可以降级处理。
5. 数据迁移必须提供 SQLite 集成测试，并验证重复执行不会产生重复数据。

SQLite 驱动依赖 CGO。Linux CI 会显式设置 `CGO_ENABLED=1` 运行完整集成测试；没有 C 编译器的本地环境只运行不依赖 SQLite 的迁移单元测试。

`EnsureEmailConfigPasswordsEncrypted` 依赖 `SBM_EMAIL_PASSWORD_KEY` 或本地密钥文件，无法作为只依赖数据库状态的版本化迁移重放，因此保留为版本迁移后的启动期修复。该修复必须在单个数据库事务中完成；读取、加密或任一更新失败都要回滚并阻止服务启动，不得保留部分明文、部分密文的中间状态。

金额在数据库中以 `amount_cents`、`tax_amount_cents` 整数分字段参与查询和聚合。旧版元字段继续双写用于兼容 API 与版本回退，不得新增只更新元字段的持久化路径。
