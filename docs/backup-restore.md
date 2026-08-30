# M1 备份与恢复说明

状态：M1 可执行基线（2026-08-28）

本说明只覆盖 Clean Slate 新系统，不读取、转换或恢复任何旧版本数据。M1 使用停机快照：SQLite、对象目录和主密钥作为一个不可拆分的恢复集合，在应用完全停止后备份。

## 一致性边界

备份集合固定包含：

- 完成 WAL checkpoint 后的单一 SQLite 文件；
- 对象根目录下的 `objects/`、`staging/` 和 `trash/`；
- 能解密 ProviderConfig 的主密钥材料；
- `manifest.json`，记录全部业务表数量、数据库 SHA-256、对象文件大小与 SHA-256、主密钥 SHA-256、对象引用数量和审计链 SHA-256。

工具会执行 SQLite `quick_check`，并逐项验证 `documents.storage_key` 和 `document_pages.derived_image_storage_key` 指向的文件及其数据库哈希。符号链接、特殊文件、忙碌或未完成的 WAL checkpoint、无效主密钥、篡改的文件以及已有恢复目标都会被拒绝。对象恢复目标可以是不存在的目录或现有的空目录，以支持全新 Docker volume；非空目录不会被覆盖。

`-offline-confirmed` 是操作者对应用已停止的明确声明。工具持有排他 SQLite 事务，但它不能阻止另一个进程在数据库事务之外写对象文件，因此不得在线执行 M1 备份。

## 构建运维命令

在与生产构建相同的源码版本和 Go 工具链上构建：

```bash
cd apps/api
go build -trimpath -o /secure/operator-bin/sbm-m1-backup ./cmd/m1-backup
```

该命令是离线运维工具，不是 API、运行时管理路由或旧数据导入器。

## 创建与验证备份

1. 记录当前构建 SHA、Compose 配置和备份目标。
2. 停止应用并确认容器已经退出：

   ```bash
   docker compose -f infra/compose/compose.yaml stop app
   docker compose -f infra/compose/compose.yaml ps app
   ```

3. 将以下示例路径替换为当前 Compose volume 的实际挂载点和受保护的主密钥源文件。可用 `docker volume inspect <volume> --format '{{.Mountpoint}}'` 只读解析挂载点，不要硬编码另一个环境的 volume 名称。
4. 创建一个新的、父目录权限为 `0700` 的目标；目标本身必须不存在：

   ```bash
   /secure/operator-bin/sbm-m1-backup backup \
     -database /resolved/database-volume/sbm.sqlite \
     -objects /resolved/object-volume \
     -master-key /secure/secrets/sbm-master-key \
     -output /secure/backups/sbm-2026-08-28T012115Z \
     -offline-confirmed
   ```

5. 在移动或归档前独立复核：

   ```bash
   /secure/operator-bin/sbm-m1-backup verify \
     -backup /secure/backups/sbm-2026-08-28T012115Z
   ```

只有 `verify` 成功且备份目录存在 `manifest.json` 时，该目录才是有效备份；失败后留下但没有有效清单的目录是未完成产物，不得用于恢复。

## 恢复到全新目标

恢复禁止覆盖现有数据库、主密钥或非空对象目录。先准备全新的 Compose project/volumes 或新的主机路径，再执行：

```bash
/secure/operator-bin/sbm-m1-backup restore \
  -backup /secure/backups/sbm-2026-08-28T012115Z \
  -database /new/database-volume/sbm.sqlite \
  -objects /new/object-volume \
  -master-key /new/secret-location/sbm-master-key
```

`restore` 在复制前验证备份，在复制后再次验证目标数据库文件、全部表数量、审计链、对象清单、对象引用和主密钥；任一步失败都不得启动恢复副本。恢复后的主密钥必须保持 `0600`，数据库和对象父目录保持 `0700`。

将新 Compose 部署指向这些新目标并启动后，至少验证：

1. `GET /api/v1/ready` 返回成功；
2. 原 owner 可真实登录；
3. 备份前已确认 Fact 可查询；
4. 原始 Document 可经鉴权下载且 SHA-256 与清单一致；
5. 备份时带租约的 `processing` Job 在租约过期后被重新接管，旧 `running` AiRun 标记为 `lease_expired`，并在两个 150 秒任务期限内进入可审核状态或明确失败；
6. 恢复过程没有产生重复 Fact。

纯合成 M1 冒烟由 `tools/run-backup-smoke.mjs` 的 `seed`、`stage-processing` 和 `verify-restore` 三个阶段重复执行；挂起模型提取调用由 `tools/synthetic-provider.mjs --mode hang-extractions` 提供，不访问外部网络。

## 保留与删除策略

- 生产备份必须保存于加密、最小权限且有访问审计的备份介质；主密钥和财务数据均不得进入源码仓库、普通日志或公共制品。
- M1 生产备份最多保留 30 个日历日，到期按备份介质能力执行不可恢复删除，不允许无限期保留。
- 删除 ProviderConfig 会立即删除在线数据库中的对应密文；历史备份中的残留只允许存在到该备份的既定到期日，最长不超过 30 天。
- 本地验收备份只含纯合成数据，在验收证据固化后删除，不转作开发 fixture 或运行时回归数据源。
- 恢复权限与备份读取权限分离；任何恢复都写入新目标，确认成功后再由部署者完成显式切换。

M4 的 1,000 Document、30 分钟完整演练不属于本说明的 M1 完成声明。
