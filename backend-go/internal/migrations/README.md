# 数据库迁移约定

数据库启动流程分为两部分：`migrateSchema` 负责新增表和列，`registeredMigrations` 负责需要严格执行一次的数据变换与索引调整。

新增迁移时遵守以下规则：

1. 版本号使用 `YYYYMMDDNN`，并按升序追加到 `registeredMigrations`。
2. 已合并并发布的迁移不得改名、改版本或修改逻辑；修复必须新增迁移。
3. 每个迁移在独立事务中执行，任何步骤失败都不得写入 `schema_migrations`。
4. SQL 错误必须向上返回；只有明确标注为运行期兼容维护的任务可以降级处理。
5. 数据迁移必须提供 SQLite 集成测试，并验证重复执行不会产生重复数据。

SQLite 驱动依赖 CGO。Linux CI 会显式设置 `CGO_ENABLED=1` 运行完整集成测试；没有 C 编译器的本地环境只运行不依赖 SQLite 的迁移单元测试。
