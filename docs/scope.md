# 范围与非目标

状态：M0～M4 本地功能与本地发布准备已完成；真实模型评测、真实外部联调和生产发布仍待单独授权
适用范围：Clean Slate 新架构

## 最高边界

Smart Bill Manager 是独立的新系统，不是旧版本的升级路径。

### 明确包含

- 全新应用结构、API 契约和数据库 Schema；
- 自托管部署与新系统自身的备份恢复；
- OpenAI-compatible Chat Completions 模型接入；
- Source、Claim、Fact、审核和审计链；
- 支付、发票、邮箱、行程与报销、数据洞察；
- 租户隔离、文件鉴权、密钥加密和最小日志；
- 纯合成软件测试与本地隔离的中文真实场景模型评估体系。

### 明确排除

- 旧数据库升级、旧数据导入、对账或回滚；
- 旧 API、旧字段、旧任务状态或旧部署兼容；
- 遗留 OCR、旧回归样本和旧版导入导出/同步模块；
- 为兼容旧实现设置的双写、fallback 或隐藏默认值；
- Git 历史重写、旧 Release/GHCR/缓存清理；
- 银行账户直连、自动支付、复式记账和税务申报；
- 原生移动端、实时多人协作、插件市场；
- 模型微调平台、供应商专属适配、多模型路由和自动降级；
- M1 自动入账、自然语言助手、邮箱采集和行程自动归属。

## M0：设计基线

### 范围内

- 恢复隔离工作区和设计分支；
- 产品、范围、验收、架构、AI、数据、UI/UX 和路线图文档；
- 根 `AGENTS.md`；
- 已批准且具有测量协议的量化指标；
- 两套结构真实不同的可比较视觉方向、选型记录与唯一冻结方向；
- 登录、AI 收件箱、审核工作台、账单列表四个代表页面；
- 可复现的视觉基线资产、响应式/可访问性证据；
- 文档一致性、链接和 diff 验证。

### 范围外

- `apps/api`、`apps/web` 或任何业务实现；
- 数据库迁移、API handler、页面组件和运行时配置；
- 安装新运行时依赖；
- 删除旧代码、推送、PR、Tag、Release、镜像或部署。

### 退出条件

- 所有规范文档完成并互相一致；
- 量化指标由产品负责人批准；
- 一套视觉方向被明确选定；
- 独立只读复审无阻断项；
- `docs/m0-evidence.md` 记录可重复命令、结果和证据边界；
- diff 仅包含工作区文档与设计基线资产。

## M1：AI 收件箱首条链路

### 范围内

- 全新项目骨架和数据库 `0001`；
- 租户、用户、Membership、ProviderConfig、Document、ProcessingJob、AiRun、ClaimSet、ReviewDecision、Payment 和 Invoice 的最小闭环；
- 空数据库专用 `bootstrap-owner` 与 `owner`、`finance`、`reviewer`、`viewer` 最小权限矩阵；
- Payment 与 Invoice 的最小一对一关联建议及人工确认；
- 单个 JPEG、PNG、WebP 或 PDF Document 上传，PDF 页数遵循验收上限；
- OpenAI-compatible Adapter 和能力检测；
- 严格 Schema、确定性校验、证据展示；
- 用户修改、确认、拒绝、重试和取消；
- 幂等创建 Payment/Invoice Fact；
- 加载、空、处理中、失败和完成状态；
- 一条真实 Compose 端到端验证。

### 范围外

- 批量上传；
- 一对多、部分金额、自动接受或冲突消解等复杂支付/发票关联；
- 邮箱自动采集；
- 行程自动归属和报销流程；
- 自动入账；
- 多模型路由和自动降级；
- 自然语言助手；
- 移动端和会计软件集成。

### 退出条件

- 本节范围内的数据库、API、Web、Provider、Claim、审核、Fact、一对一关联和备份恢复能力全部实现，契约与实现一致；
- 唯一活动链路固定为 `bill-visible-text-cn/1 -> bill-visible-text/1 -> bill-visible-text-provider/1 -> claim-mapper/3 -> document-claim/2`，AI 不能绕过人工审核创建 Fact；
- `docs/acceptance.md` 中适用于 M1 的确定性业务、租户与安全、性能、可靠性、UI/UX、工程质量和最小场景门禁全部通过，并在 `docs/m1-evidence.md` 留下可重复证据；
- 2026-08-30 批准移至 M4 的模型正确率与 100 份三次正式评测不再作为 M1 退出条件，历史失败必须原样保留且不得改写为通过；
- 工作区不存在 M2 领域、数据库、API 或 UI 实现，M1 完成不自动启动 M2。

### 当前状态

M1 已于 2026-08-30 完成。清晰、完整、无遮挡且关键字段可直接人工辨读的原始输入是当前质量承诺；模型正确率高目标保留为全部功能开发完成后的 M4 正式发布门禁。安全闭环、人工审核、Source/Claim/Fact、租户隔离和确定性规则没有例外；收口证据见 `docs/m1-evidence.md`。M2 五个切片已于 2026-08-31 全部完成并收口，证据见 `docs/m2-evidence.md` 与 `tests/evidence/m2/m2-closure-gate-summary.json`。

## M2：发票与关联

状态：已完成；启动日期 2026-08-30，完成日期 2026-08-31。

### 范围内

- 一对多、多对一及部分金额的 Payment/Invoice 分配关系；
- 关联候选、分配金额、冲突原因、审核证据与原子确认；
- 近似文件、跨页和字段组合重复检测；
- 复杂多页 PDF 的跨页明细重建与分页审阅；
- 多 Document 批量上传及逐项成功/失败反馈；
- 已确认 Payment/Invoice Fact 之间的独立补充分配、撤销和替换。

### 已完成首切片（2026-08-30）

- 候选从“金额完全相等且未关联”扩展为“同租户、同币种、业务日期相差不超过 30 天、相反类型 Fact 仍有正数可分配余额”；名称只影响排序与警告；
- 确认一个 Payment 或 Invoice 时，可对一个或多个候选提交正整数 `allocated_minor`；本次分配合计不得超过新 Fact 金额，每个候选分配不得超过其事务内最新余额；
- 每条活动 Link 保存不可变的 `allocated_minor + currency`，同一 Payment/Invoice 对同时最多一条活动 Link；允许不同对形成一对多或多对一；
- Payment 与 Invoice 列表返回总额、已分配金额、剩余金额和 `unallocated`、`partial`、`allocated` 状态；
- 同一事务创建 Fact、ReviewDecision、全部候选决定、全部 Link 和 AuditEvent；跨租户、重复候选、零/负数、币种冲突、过期候选或任一侧超分配必须整体失败；
- 同一幂等键绑定规范化后的完整分配计划；重放返回相同 Fact 和 Link，改变候选或金额必须冲突；
- 删除 Fact 继续终止其全部活动 Link，并使未删除另一端的可用余额在后续读取中恢复。

### 已完成第二切片：确定性重复检测（2026-08-30）

- M1 原始 SHA-256 完全重复和规范化发票号完全重复继续分别作为上传硬冲突与 blocked 规则，不能由疑似重复确认绕过；
- 现有 `document-normalize/2` 页面只增加版本化的双 64 位视觉指纹，不修改原始 Source 或模型图片，也不使用 OCR、模型判断、模糊文本或供应商分支；
- 同页数且全部有序页面近似形成 `near_file`；同一 Document 内或不同 Document 间的部分页面近似形成 `cross_page`，整份近似已覆盖的页面对不重复提示；
- Payment 使用正数金额、币种、5 分钟内交易时间和规范化商户组合，Invoice 使用正数价税合计、币种、开票日期及规范化购销方组合，生成 `field_combination` 候选；
- 所有候选只在同租户内产生，作为 Claim revision 的不可变审核输入；确认 Fact 前必须逐项明确 `keep_distinct`，否则只能修订或驳回当前 Claim；
- 确认事务重新计算候选集合并把完整 resolution 计划纳入幂等身份；集合陈旧、伪造候选、跨租户候选或缺少决定时整体失败，不产生部分 Fact、决定、Link 或审计；
- 本切片只检测跨页重复，不重建跨页明细、不删除重复页、不推断续行、重复表头或页序；详细决策见 `docs/decisions/0010-deterministic-duplicate-detection.md`。

### 已完成第三切片：跨页明细重建与分页审阅（2026-08-30）

- 保持 `bill-visible-text-cn/1`、`bill-visible-text/1` 与 `claim-mapper/3` 不变；模型一次查看全部原始规范化页面，继续负责逻辑明细分组、字段语义和阅读顺序，本地不得重识别或猜接票面文字；
- 同一模型 item 中的字段可以引用相邻多页；稳定 `item_key`、Evidence 页码、连续 `sort_order`、连续页跨度和不倒退阅读顺序组成唯一确定性重建边界；
- 当前 revision 的逐页字段、明细键和跨页跨度只从 `FieldClaim -> Evidence -> DocumentPage` 派生，不新增持久化业务副本；
- Review API 返回完整分页计划，Web 通过租户隔离的规范化单页入口进行分页、字段定位和跨页明细审核；
- 页码越界、明细页跳跃、阅读顺序倒退、必填字段缺失或跨页合计冲突必须阻断；重复表头或字段自身断裂不得自动删除、合并或文字拼接；
- 详细决策见 `docs/decisions/0011-cross-page-invoice-review.md`。
- 领域校验、当前 revision 派生投影、租户隔离的规范化页 API、分页审核 UI、13 / 13 浏览器组件场景、60 / 60 关键不变量和两层覆盖率门禁均已通过；证据见 `docs/m2-evidence.md`。

### 已完成第四切片：多 Document 批量上传与逐项反馈（2026-08-31）

- Web 一次选择 1–20 个文件，并严格按选择顺序逐项调用现有单 Document 上传 API；任一时刻最多一个请求在途；
- 每项独立显示等待、上传中、已入队、已存在或已拒绝。中间失败不回滚或阻止其他项，第 21 项及以后保留为显式超限失败，禁止静默截断；
- 单项继续遵循 20 MiB、签名/MIME、租户隔离、对象提交补偿和同租户原始 SHA-256 精确判重。浏览器大小预检只改善反馈，服务端仍是权威边界；
- 批次状态只存在于当前页面内存；不新增 Batch 实体、数据库迁移、服务端批量端点、批次事务、并行上传、自动重试或第二数据源；
- 完整不变量与失败边界见 `docs/decisions/0012-client-orchestrated-batch-upload.md`。
- 串行状态机、逐项收件箱反馈、后端独立命令失败隔离、17 项 Web 单元测试、14 / 14 浏览器组件场景、61 / 61 关键不变量及两层覆盖率门禁均已通过；证据见 `tests/evidence/m2/batch-upload-gate-summary.json` 和 `docs/m2-evidence.md`。

### 已完成第五切片：已确认 Fact 的独立分配调整（2026-08-31）

- 以一个已确认 Payment 或 Invoice 为 anchor，读取当前活动 Link、合格的相反类型 Fact 和版本化 `plan_hash`；
- 用户提交 anchor 的完整期望活动分配计划及必填理由。只增加为补充、只移除为撤销，改金额或同时增删为替换；空计划明确表示撤销全部；
- 调整在一个 immediate 事务内校验同租户、未删除、同币种、日期窗口、双方余额、活动对唯一、期望快照和幂等身份；任何冲突整体失败；
- 未变化 Link 保持不动；撤销或被替换的旧 Link 只终止一次；新增或替换项创建具有独立调整来源的新不可变 Link；
- 活动 Link 继续是余额唯一来源，不增加可写余额、旧兼容命令、模型自动关联或第二数据源；
- `owner`、`finance` 可使用独立调整页面，`reviewer`、`viewer` 不可读取或提交调整。完整冻结决策见 `docs/decisions/0013-confirmed-fact-allocation-adjustment.md`。
- 领域、SQLite、HTTP/OpenAPI、生成客户端和 Web 独立页面均已实现；21 项 Web 单测、18 / 18 浏览器场景、72 / 72 关键不变量及两层覆盖率门禁通过，证据见 `tests/evidence/m2/allocation-adjustment-gate-summary.json`。

### 范围外

- 自动接受关联、自动入账或绕过人工确认；
- 邮箱采集、行程归属和报销流程；
- 多模型路由、供应商专属运行时分支或本地 OCR 主链；
- 为追求低清、裁切、遮挡或屏摄图片准确率而继续扩张 Prompt/预处理分支；
- M3、M4 功能、远端分支、推送、部署、发布或旧数据兼容。
- 跨币种分配、汇率换算、自动截断余额或自动接受候选；
- 原地修改活动 Link 金额，或为同一 Payment/Invoice 对创建多条并行活动 Link。

### 退出条件

退出条件已满足：五个切片的领域、数据库、API、Web、测试和证据均与冻结决策一致；活动 Link 仍是余额唯一来源，Source/Claim/Fact 与人工审核边界未改变，未增加旧兼容、第二数据源、OCR 或真实 Provider 调用。最终门禁见 `tests/evidence/m2/m2-closure-gate-summary.json`。M2 完成不代表真实模型正确率、真实邮箱/Provider 联调、部署或发布通过。

## M3：邮箱、行程与报销

状态：已完成；三个切片于 2026-08-31 全部通过本地验收。

当前范围顺序固定为邮箱 Source、邮件与附件、行程归属、报销状态和确定性冲突提示。

### 已完成的首切片范围

- 注册不含凭据的 IMAP Source 描述符：显示名、规范邮箱地址、主机、端口与强制 TLS 模式；新来源明确处于 `pending_connection`；
- 通过仅供未来连接器调用的内部应用端口归档纯合成 RFC 822 原文和附件，不提供 HTTP/CLI 导入或运行时 fixture 入口；
- 原始邮件、附件身份、hash、状态和对象不可变；相同稳定外部键严格幂等，同键异文冲突；
- 合法图片/PDF 附件复用唯一 Document/Job/Claim 审核链；不支持或非法项只归档并逐项显示原因；
- 邮箱来源、游标分页邮件、附件状态、原始邮件和附件强制下载 API，以及对应 Web 页面；
- Owner 管理来源，Owner/Finance 阅读归档；Reviewer 只沿既有审核证据边界读取 Document，Viewer 不可枚举；
- 使用纯合成 `.invalid` 地址和 MIME 字节完成事务、补偿、权限、隐私、浏览器与恢复验收。

### 首切片明确排除范围

- 任何真实邮箱连接、DNS/TLS 握手、IMAP 命令、轮询、远端游标推进或真实账号联调；
- 密码、OAuth、Token、Cookie、客户端秘密、密文、密钥引用或凭据 UI；
- 邮件正文语义提取、链接抓取、远端图片、HTML 执行、压缩包展开或邮件专用模型链；
- Trip、Reimbursement、行程归属、报销状态和冲突提示；它们按后续切片继续冻结后实施。

完整不变量、容量、失败补偿、API/UI 与验证要求见 `docs/decisions/0014-connector-neutral-email-archive.md` 和 `docs/acceptance.md`。

### 已完成的第二切片范围

- 将唯一活动模型链升级为 `bill-visible-text-cn/2 -> bill-visible-text/2 -> bill-visible-text-provider/2 -> claim-mapper/4 -> document-claim/3`，增加固定 Trip 区段；旧版本退出活动链路，不保留兼容解析；
- Trip 单据继续经过既有 Document、AiRun、Claim、Validation 与人工 Review，只有确认后创建可追溯 Trip Fact；
- Trip 最小字段为可选出发地、必填目的地、必填起止日期、可选出行人/交通类型/预订编号；不计算、猜测或读取邮件正文补值；
- 对已确认 Payment/Invoice 生成版本化的日期与既有 PaymentInvoiceLink 确定性建议，不自动归属；
- 以不可变 Decision 与可终止一次的 Assignment Link 支持显式 assign、move、unassign，提交期望当前 Link、幂等键与必填理由；
- Trip 列表、游标分页归属候选、单 Fact 归属写 API、行程审核和行程归属 Web 工作区；
- Owner/Finance 管理归属，Owner/Finance/Viewer 读取 Fact，Reviewer 只沿现有审核权限处理 Trip Claim，Owner 才能删除；
- 纯合成测试覆盖事务原子性、并发、删除生命周期、租户、权限、游标、响应式和可访问性。

### 第二切片明确排除范围

- 自动接受归属、模型归属、模糊地点/名称学习、地图路线、多城市航段或行程日程；
- 报销状态、费用政策、缺票/金额/重复报销提示；
- 真实 Provider、正式正确率评测、真实邮箱连接、外部账号联调、部署或发布。

完整决策见 `docs/decisions/0015-trip-fact-attribution.md`；验收证据见 `docs/m3-evidence.md` 与 `tests/evidence/m3/trip-attribution-gate-summary.json`。

### 已完成的第三切片范围（2026-08-31）

- Reimbursement 是已确认 Payment/Invoice 上的独立人工工作流记录，不是模型或手工直建的新 Fact；活动 AI 契约、Claim 与 Review 边界保持不变；
- 用户从一个未删除 Trip 的活动归属中显式选择 1～200 个 Assignment，提交时冻结 Trip、Fact、金额、币种和 Assignment 摘要；不修改原 Fact、金额 Link 或行程归属；
- `reimbursement-policy/1` 只产生 `missing_invoice`、`amount_conflict` 和 `duplicate_reimbursement` 三类确定性 Finding；按币种分别汇总，不换汇、不自动拒绝；
- 预检返回当前完整 Finding 与 `snapshot_hash`；提交必须确认全部 Finding key，事务内重算并以严格幂等创建不可变 Reimbursement、Item、Finding、Decision 和安全 AuditEvent；
- 状态固定为 `submitted | reimbursed | rejected`，终态只可重新打开为 submitted；同一 Trip 同时最多一个 submitted，期望状态、版本、理由与幂等键保护并发；
- 报销列表、详情、稳定分页、预检、提交、状态历史 API 和独立 Web 工作区；Owner/Finance 管理，Owner/Finance/Viewer 读取，Reviewer 拒绝；
- 纯合成测试覆盖三类 Finding、快照陈旧、并发、严格重放、软删除后历史、事务回滚、租户、权限、游标、响应式和可访问性。

### 第三切片明确排除范围

- 企业审批链、费用科目、预算、税务/发票验真、外部报销单号、打款、会计/ERP/邮箱/云服务同步；
- 汇率换算、手工覆盖报销金额、部分 Item 金额、导出报销单、批量状态操作或模型生成政策；
- 真实 Provider、正式正确率评测、真实邮箱/外部账号联调、部署或发布；
- 旧 Trip 二值报销字段、旧 API、旧数据库或旧状态兼容。

完整冻结决策见 `docs/decisions/0016-reimbursement-workflow-policy-findings.md`；验收证据见 `docs/m3-evidence.md` 与 `tests/evidence/m3/reimbursement-workflow-gate-summary.json`。

### 退出条件

退出条件已满足：三个切片的领域、数据库、API、Web、测试和证据均与冻结决策一致；Source/Claim/Fact、人工审核、活动 Link、租户和整数金额边界未改变，未增加旧兼容、第二数据源、OCR、真实 Provider 或真实邮箱连接。最终门禁见 `tests/evidence/m3/m3-closure-gate-summary.json`。M3 完成不代表正式模型正确率评测、真实外部账号联调、部署或发布通过。

## M4：洞察、加固与本地发布准备

状态：本地范围已完成。SQLite 结果仅作为历史记录；当前基线已按 ADR-0020 切换为 PostgreSQL 17 唯一持久化，并完成受影响不变量与 ADR-0019 本地发布门禁的重新验收。

### 首切片范围内：确定性 Fact 洞察与筛选查询

- 从当前未删除 Payment/Invoice、活动 PaymentInvoiceLink 和活动 TripFactAssignment 的同一关系数据库读快照派生洞察，不增加统计表、物化缓存、双写或第二数据源；当前实现必须使用 PostgreSQL `REPEATABLE READ READ ONLY`；
- 支持 Fact 类型、成对起止日期、币种、分配状态、已/未归属和可选具体 Trip 筛选；Payment 只使用不可变 `business_date`，Invoice 使用 `invoice_date`；
- 按币种与 Fact 类型分别汇总数量、总额、已分配、剩余和三种分配状态数量；金额为整数最小单位，禁止 Payment/Invoice 合并总额、跨币种相加或换汇；
- 提供默认 50、最大 100 的稳定不透明游标分页；游标绑定完整筛选身份，筛选变化必须拒绝旧游标；
- 新增只读 `insights.read` 能力和 `/insights` Web 工作区；Owner/Finance/Viewer 可读，Reviewer 拒绝；
- 只使用纯合成数据完成领域、PostgreSQL、应用、HTTP/OpenAPI、Web、浏览器、覆盖率、性能基线、权限与可访问性验证。

### 首切片范围外

- 自然语言查询助手、模型生成洞察、预测、趋势、同比、预算、税务、排名、图表装饰或报表导出；
- 写回分类、Fact、Link、Trip 归属或 Reimbursement；持久化统计结果、异步聚合 Worker、搜索索引或分析数据库；
- 汇率换算、跨币种或跨 Payment/Invoice 类型的金额合计；
- 真实模型评测、真实邮箱/Provider/外部账号联调、部署、发布、Tag 或远端操作。

完整不变量、失败边界和验收要求见 `docs/decisions/0017-deterministic-fact-insights-and-query.md`、`docs/decisions/0020-postgresql-only-persistence.md` 与 `docs/acceptance.md`。历史 `tests/evidence/m4/fact-insights-gate-summary.json` 只证明当时的 SQLite 实现；当前 PostgreSQL 聚合、快照和 keyset 路径由最终本地发布证据及全量门禁覆盖。

### 历史第二切片：SQLite 认证停机备份与完整恢复演练

- 用新的 `smart-bill-manager-backup/2` 替代 M1 清单和工具入口，不保留旧版本解析或参数兼容；
- 数据备份包只含 SQLite、精确已提交对象集合和经 HMAC 认证且带随机 `backup_set_id` 的清单；既有主密钥由独立托管文件提供，不复制进数据包；
- 应用、初始化命令和备份共享运行锁；备份要求 WAL checkpoint、排他快照、完整/外键/迁移/Schema 校验以及空 `staging/`、空 `trash/`；
- 对上传 Document、DocumentPage、邮件原文和非空邮件附件的全部对象引用做去重后精确对账，拒绝缺失、冲突和未引用文件；
- 恢复只写与源、迁移和 guard 路径不重叠的不存在目标，使用分区 staging 与永久 `restore-state`；durable incomplete 阻断半恢复，独立同步后的 complete 原子替换并与数据库成对；首次启动前失效全部旧 Session；
- 用恰好 1,000 个具有实际纯合成原件的 Document，覆盖 997 个普通失败上传、1 个失败邮件附件 Document、已确认 Fact、处理中租约、已由挂起 Provider 请求证明落库的 `running` AiRun、邮件原文/附件共享对象和恰好 2 个派生页；对象固定为 1,004 条引用与 1,003 个唯一物理文件；
- 创建数据包后、首次独立 verify 前启动唯一 30 分钟时钟；恢复副本先证明原快照查询/下载可用，再覆盖租约接管、AiRun 收口、继续审核和唯一闭合 Fact 链。既有非 Session 行摘要与非目标 Job/AiRun 必须不变。

### 第二切片范围外

- 在线/增量备份、云备份、自动故障转移、非零 RPO 的生产变更重放或真实灾难切换；
- 提交、保留或复用真实/长期凭据；本轮经单独授权的一次性本地演练凭据不构成产品能力；真实 Provider、邮箱、外部账号、云服务、部署或发布；
- 旧 M1 清单、旧数据库、旧对象布局、旧任务状态或旧工具入口兼容；
- 运行质量、生产镜像与本地发布准备的其余门禁，它们在本切片通过后另行冻结。

本切片的历史决策边界为 `docs/decisions/0018-authenticated-offline-backup-and-recovery.md`。既有证据只说明当时的 SQLite 实现已经通过；ADR-0020 已替代当前数据库和恢复格式，必须在 PostgreSQL 上重新实现并验收，不得把历史结果冒充当前恢复或发布结论。

### 第三切片范围内：PostgreSQL 唯一持久化与重新验收

- PostgreSQL 17 成为唯一关系数据源；重建 Clean Slate `0001`、唯一适配器、显式迁移入口、事务/租约语义、Compose 数据库服务和运维边界；
- 删除 SQLite 驱动、适配器、迁移、数据库文件配置、运行锁、发布卷和当前运维入口，不保留运行时选择、双写、兼容导入或第二测试数据源；
- 不迁移任何现有数据；所有验收从空 PostgreSQL 数据库和纯合成 fixture 开始，本地对象文件实现保持不变；
- 数据库密码只从受保护文件读取，PostgreSQL 不发布宿主端口；常规 API 身份无 Schema DDL 权限，迁移使用独立显式入口；
- 只读多查询使用同一 `REPEATABLE READ READ ONLY` 快照；关键写事务使用行锁、唯一约束和适用的 `SERIALIZABLE` 隔离，序列化与死锁失败显式映射为并发冲突；
- Worker 使用 PostgreSQL 行锁安全竞争；重复检测保持索引候选缩小和本地确定性判断，洞察保持数据库聚合与 keyset 分页；
- 将备份载荷改为固定 PostgreSQL 17 自包含 dump 与精确对象集合，重新执行 1,000 Document、RPO 0、30 分钟 RTO、会话失效和任务唯一续跑演练；
- 在受限临时 PostgreSQL 容器上重新运行领域/适配器/HTTP、租户、并发、覆盖率、关键不变量、10,000 数据集性能、内存和完整浏览器验收。

### 第三切片范围外

- SQLite 数据迁移、旧 Schema 探测、双数据库、灰度双写、回滚到 SQLite 或兼容旧备份；
- 托管 PostgreSQL、跨主机 TLS、HA、复制、自动故障转移、连接代理、读副本或 S3/对象存储；
- 新业务 API、页面或领域规则；真实 Provider/邮箱、正式模型评测、部署、发布、付费或远端资源。

本切片以 `docs/decisions/0020-postgresql-only-persistence.md` 为唯一数据库决策边界，已完成并通过重新验收。

### 第四切片范围内：运行质量与本地发布准备

- 用稳定的最小 Alpine 运行层、`app.Dockerfile`、entrypoint、Compose 和环境模板替代全部 `m1` 发布入口，不保留旧文件名、镜像名或 wrapper；镜像同时包含当前 Web、API、Owner 初始化与认证备份 CLI；
- 用受保护准备器在独立工作区执行固定 Node 离线锁文件安装/Web 构建与固定 Go 禁网校验/构建，再由 Dockerfile 严格核对本地产物身份、清单和 SHA-256；不提交产物、缓存或工具链；
- 固定镜像内容、构建时基线 HEAD 与确定性发布输入摘要、只读根文件系统、回环默认绑定、运行用户、最小 capability、tmpfs、资源上限、健康检查、主密钥材料化和 production Secure Cookie 失败边界；最终证据提交后复核发布输入摘要不变，不虚构自引用提交身份；
- 修正 acceptance synthetic Provider 的 exercise/model/mode 身份，保持回环、纯合成和无外网；
- 重新执行 10,000 Fact HTTP p95、Document 创建、审核确认、50 Job 内存趋势、四页三次 Lighthouse、响应式/等效 200% 回流、键盘、深色主题和当前完整 Playwright 场景；
- 所有会产生 fixture 或业务写入的运行器先占用受保护结果路径，原始报告只留在 `/tmp`，终端和仓库证据只保留安全聚合；
- 新增当前本地运行、首次 Owner 初始化、健康检查、备份恢复、日志、容量、升级前备份、故障诊断和同一 Clean Slate 契约回滚说明；
- 运行全量测试、静态检查、构建、关键不变量、覆盖率、镜像资产、安全配置、敏感信息、临时产物与进程残留门禁，并生成 `tests/evidence/m4/local-release-readiness-gate-summary.json`。

### 第四切片范围外

- 新业务领域、API、页面、迁移、统计副本或自然语言查询；
- 真实模型正确率正式评测、真实 Provider/邮箱/外部账号、真实图片发送或生产数据；
- 新依赖、镜像、OCR、模型文件、付费资源、域名、证书、云资源、远端存储、部署、发布、Tag、Release 或推送；
- 旧实现、旧数据库、旧 API、旧 Compose、旧发布入口或旧任务状态兼容。

本切片以经 ADR-0020 修订的 `docs/decisions/0019-local-release-candidate-and-runtime-quality.md` 为决策边界，现已通过。结论只表示 PostgreSQL 本地产品功能与本地发布候选完成；真实模型、真实外部系统和生产发布仍在单独授权门禁前。

## 范围变更规则

- 新需求默认进入 `docs/roadmap.md` 的 Backlog。
- 当前里程碑新增范围时，必须同时更新本文件和 `docs/acceptance.md`。
- 如果新增内容改变核心架构、数据安全或审核边界，必须新增或更新架构决策记录。
- “顺便优化”“旧版已有”或“模型可以完成”都不能替代明确验收证据。
