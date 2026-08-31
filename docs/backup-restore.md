# 备份、验证与恢复说明

状态：M4 `smart-bill-manager-backup/2` 已完成实现并通过 1,000 Document 完整本地演练

本说明只覆盖 Clean Slate 新系统，不读取、转换或恢复旧数据库、旧对象布局、旧任务状态或 M1 清单。权威不变量与失败边界见 `docs/decisions/0018-authenticated-offline-backup-and-recovery.md`。

## 恢复集合与保管边界

一次可恢复集合必须同时具备：

1. 数据备份包：`database/sbm.sqlite`、`objects/`、`manifest.json` 和 `manifest.hmac`；
2. 与数据包分开保管的既有主密钥副本。

数据包不包含主密钥，也不能独立通过认证或恢复。独立密钥副本必须位于不同的访问控制和存储故障域；仅把两个目录放在同一普通磁盘上不算独立托管。主密钥文件始终为普通文件、owner-only 权限，且其路径不得位于数据包内。

`manifest.hmac` 使用主密钥按固定域分离规则派生的 HMAC-SHA-256 验证 `manifest.json` 原始字节。每个清单另含 128-bit 随机 `backup_set_id`，backup、独立 verify、restore 与演练的 API/数据库受保护结果必须一致。CLI 成功输出只含安全聚合，不输出完整清单、路径、哈希、密钥身份或业务内容。

## 构建工具

使用与应用相同的已固定 Go 工具链和现有模块缓存，不下载依赖：

```bash
cd apps/api
go build -trimpath -o /secure/operator-bin/sbm-backup ./cmd/backup
```

数据备份包与恢复临时目录都不得位于仓库、Docker build context、日志目录或任何同步到公共制品的位置。

## 创建数据备份包

1. 停止应用和所有会写入同一数据库/对象根的本地命令；不得只暂停 HTTP 流量。
2. 确认 `staging/` 与 `trash/` 已由应用正常协调为空。
3. 通过独立受保护路径提供当前主密钥，并在一个不存在的目标路径创建数据包：

```bash
/secure/operator-bin/sbm-backup backup \
  -database /srv/sbm/database/sbm.sqlite \
  -objects /srv/sbm/object-store \
  -master-key /secure/key-escrow/current-master-key \
  -migrations /opt/sbm/migrations \
  -output /secure/data-backups/sbm-2026-08-31T120000Z \
  -offline-confirmed
```

工具先拒绝数据库、对象文件或主密钥的多硬链接、符号链接，以及落在数据库父目录、对象根或迁移集合内的输出，再取得 `/srv/sbm/database/sbm.sqlite.runtime.lock` 的排他锁。随后完成 WAL checkpoint、SQLite 排他快照、完整性/外键/迁移/Schema 检查和四类对象引用精确对账。备份先写同一父目录的随机 staging，文件与目录同步后才 rename 为 `-output`；已存在目标永不覆盖。失败后没有完整认证清单的目录不是有效备份。

## 独立验证

验证必须再次从独立数据源取得主密钥和当前迁移集合：

```bash
/secure/operator-bin/sbm-backup verify \
  -backup /secure/data-backups/sbm-2026-08-31T120000Z \
  -master-key /secure/key-escrow/current-master-key \
  -migrations /opt/sbm/migrations
```

验证顺序固定为：严格解析清单与标签、HMAC 认证、迁移身份、SQLite 文件、全部对象文件、完整对象清单、数据库完整性/外键/Schema/表数量/审计链、数据库对象引用。旧版本、未知或尾随字段、路径越界、符号链接、特殊文件、重复记录、乱序记录、缺失/多余对象和哈希/大小不符全部失败。

## 恢复到全新目标

数据库、对象根和主密钥目标都必须不存在；不接受预建空目录。主密钥来源仍从独立托管位置读取：

```bash
/secure/operator-bin/sbm-backup restore \
  -backup /secure/data-backups/sbm-2026-08-31T120000Z \
  -master-key-source /secure/key-escrow/current-master-key \
  -migrations /opt/sbm/migrations \
  -database /srv/sbm-restored/database/sbm.sqlite \
  -objects /srv/sbm-restored/object-store \
  -master-key /srv/sbm-restored/secrets/master-key \
  -offline-confirmed
```

恢复先在每个目标文件系统内写随机 staging，并以 staging 路径完成完整离线复核。进入发布阶段前，工具创建 owner-only、单硬链接的 `/srv/sbm-restored/database/sbm.sqlite.restore-state`，内容为版本化 `incomplete` 状态；应用与 `bootstrap-owner` 对 incomplete、未知、损坏、权限过宽、孤立于数据库的状态全部拒绝启动。跨卷发布任一步失败或进程中断时，该阻断态保留，部分目标不得启动或复用。

三部分发布后，工具删除恢复数据库中的全部 Session，再次核对除 `sessions = 0` 外的表数量、Schema、审计链和对象集合并完成 checkpoint。最后在同目录写入并同步新的 `complete` 状态文件，原子替换 durable incomplete 状态并同步父目录；成功后状态文件永久保留，只有精确 complete 内容与实际数据库同时存在才允许启动。旧 Cookie 此后必须失败；操作者使用原有独立登录凭据建立新 Session。成功恢复不会自动启动应用、连接 Provider 或访问邮箱。

## M4 1,000 Document 演练

完整演练由 `tools/run-backup-exercise.mjs` 的受控阶段与上述 CLI 共同执行。它只使用回环 synthetic Provider、`.invalid` 身份和纯合成 MIME/图片字节；Provider 必须以本轮 UUIDv4 `--exercise-id` 启动，health 的精确 Schema、实例、模型、模式和计数必须从 0 绑定到唯一一次提取。受保护的本地状态、密码、synthetic Provider key、数据库、对象、数据包和恢复目标必须位于临时隔离目录并保持 owner-only，不进入 Git。每个会产生业务写入的阶段先以 O_EXCL 创建持久 in-progress 结果，输出冲突不得污染精确数据集。

固定流程为：

1. 在无 Provider 的全新租户中创建 997 个终态普通上传 Document，并归档一封含非空附件且形成第 998 个失败 Document 的纯合成邮件；
2. 用回环 Provider 创建并确认一个 Payment Fact；切换到挂起模式后创建一个持有租约和 `running` AiRun 的 Processing Job；只有这两个成功进入模型边界的上传生成 DocumentPage，最终固定为 1,000 个 Document、2 个 Page、1,004 条对象引用和 1,003 个物理对象；
3. 控制器确认同一 exercise/model/mode/instance 的挂起 Provider 计数从 0 精确变为一次提取后正常停止应用，再确认全局只有一个 `running` AiRun、998 个失败任务全部为 `provider_config_missing`、两个 Page 分别属于已确认与处理中 Document，且 `staging/`、`trash/` 为空；
4. 先创建数据包，再以 O_EXCL 写入不可重置的恢复时钟；时钟必须早于首次独立 `verify`，随后验证同一 `backup_set_id` 并恢复到全新目标；
5. 启动恢复副本，验证 ready、旧 Cookie 未认证、新登录，并先证明原快照中的 Fact/Document 可查询、上传/邮件五个受保护对象可鉴权下载；上述读取全部完成后再读取目标 Job，只有它仍为 `processing` 且 attempt 与备份前相等，才算形成“尚未继续处理”的线性化屏障；
6. 验证旧 `running` AiRun 在全库唯一变为 `failed/lease_expired`、Job attempt 恰好增加一次且 version 按唯一正常路径增长、任务继续到 `needs_review`，最终确认后只新增一条与该任务闭合的 Claim→Review→Payment 链；既有全部行摘要、其他 Job/AiRun 和非目标表必须不变；
7. 从数据包创建完成后的唯一时钟起点到上述最终状态的总时间不得超过 30 分钟。

备份前离线形状、原始清单相等性、Session 清零后的允许差异、既有行稳定摘要和首次启动后的闭合精确增量分别记录，不能把启动后的合法写入继续写成“与清单逐字相等”。所有受保护结果同时绑定一个 `exercise_id` 和同一认证 `backup_set_id`；证据合并器拒绝跨轮拼接、迟启时钟、缺字段和不完整覆盖率对象。

## 保留、RPO 与恢复审批

- 数据包和独立主密钥副本均属于敏感备份，必须使用加密介质、最小权限和访问审计；任一部分不得进入源码、普通日志或公共制品。
- ProviderConfig 删除后，含旧密文的数据包最长保留 30 个日历日，到期按介质能力不可恢复销毁；不得无限期保留。
- 本地验收数据全部是纯合成数据，证据固化后只清理本轮明确创建且路径已核实的临时产物，不转作运行时数据源。
- M4 本地演练 RPO 固定为 0，备份完成到恢复验证间不得发生业务写入或删除。
- 非零 RPO 的生产恢复必须在激活前重放独立、认证、单调的快照后租户删除与凭据撤销登记；该外部运行条件留在生产发布门禁，本地演练不伪造。
- 恢复权限与数据包读取权限、主密钥托管权限分离；真实恢复、真实账号、凭据使用、部署和发布仍需单独授权。

## 证据边界

通过后只提交 `tests/evidence/m4/backup-restore-gate-summary.json` 的安全聚合，包括：构建身份、清单规范、Document/对象/引用聚合数量、各离线相等性布尔值、Session 失效数量、恢复后状态增量、RTO 毫秒和 `passed`。不得提交数据库、对象、数据包、清单全文、主密钥、Cookie、密码、Provider key、业务字段、原始响应或运行日志。
