# 验收标准

状态：M0 已完成（2026-08-27）；M1 已完成（2026-08-30）；M2、M3 已完成（2026-08-31）
硬边界：已达成共识
量化指标：产品负责人已于 2026-08-27 批准当前数值

## 使用方式

本文使用以下术语：

- **必须**：不满足即不能通过验收。
- **应当**：除非有书面例外和替代证据，否则视为必须。
- **批准值**：产品负责人批准的量化门槛；适用里程碑必须满足，除非批准记录写明例外。

验收不是任务清单。每个结论都必须附带可重复的命令、测试结果、截图、请求响应或数据库断言；“已实现”“看起来正常”和启动服务不构成证据。

## 一、全局完成定义

任何里程碑必须同时满足：

1. 范围内用户场景全部可执行，范围外功能没有被顺带实现。
2. 没有旧代码、旧数据库、旧 API、旧任务状态或旧数据兼容逻辑。
3. Source、Claim、Fact 和审核边界没有旁路或第二实现。
4. 对该里程碑已存在的运行时，正常、边界、失败、取消、重试、幂等和跨租户路径均有测试。
5. 对该里程碑已存在的运行时，适用的类型检查、静态检查、单元、集成、契约、E2E 和受影响构建全部通过。
6. 对交付运行时的里程碑，使用生产构建完成一次真实请求或页面操作，不以“服务已启动”代替验证。
7. 完整 diff 经审查，没有真实数据、密钥、吞错、隐藏 fallback、重复规则或生产测试资产。
8. 文档与实际契约同步，验收证据可由另一位审查者重复。

M0 不包含运行时，因此第 4–6 项以“无业务代码或运行时变更”的 Git 范围证据替代；从 M1 起不得使用该替代。

## 二、Clean Slate 验收

新架构进入 M1 后必须满足：

- 活跃实现只位于新目录；新代码不得 import、复制或运行 `backend-go/`、`frontend/` 和旧 OCR 脚本。
- 新数据库从 `0001` 创建空 Schema，不检查、读取或修改旧数据库。
- 新 API 使用自己的 `/api/v1`，不存在旧路由别名和旧响应字段。
- 不存在旧架构数据迁移、旧数据导入、双写、兼容窗口或“旧值为空时回退”等分支；新架构公开版本之间必须使用连续、事务化且内容身份不可变的 PostgreSQL Schema migration 保留现有数据。
- 新活跃目录和生产镜像中不存在回归样本表、管理路由、导入导出、同步或运行时 fixture 读取。
- 搜索范围固定为 `apps/`、`contracts/`、`infra/`、`tests/`、`tools/` 和生产镜像清单。M0 不删除遗留树；遗留纯合成回归工具不得被新代码依赖、复制或打入新生产镜像，并随遗留实现整体退役。
- 新活跃目录搜索、依赖图和生产镜像清单能够证明上述约束。

## 三、M0 验收

M0 只有在以下证据齐全时完成：

| 项目       | 必须证据                                                                   |
| ---------- | -------------------------------------------------------------------------- |
| 工作区     | 独立分支、基线 SHA、干净起点和未修改远端状态                               |
| 产品与范围 | `product.md`、`scope.md` 无冲突，Clean Slate 与非目标明确                  |
| 验收       | 本文包含可量化门槛、证据类型和失败判定                                     |
| 架构       | 依赖方向、运行时边界、状态机、错误和安全边界明确                           |
| AI         | Provider 契约、Schema、校验、证据、失败和审计规则明确                      |
| 数据       | Source/Claim/Fact、租户、金额、时间、不可变性与删除规则明确                |
| UI/UX      | 结构差异明确的方向对比、唯一选型记录、四个代表页面、完整状态和可访问性标准 |
| 决策       | Clean Slate、Source/Claim/Fact、OpenAI-compatible 三项 ADR                 |
| 范围检查   | Git diff 不包含业务代码、运行时依赖、迁移或部署变更                        |
| 批准       | 量化指标、唯一视觉方向和四个代表页面基线均由产品负责人批准                 |
| 复审       | 独立只读复审无阻断级问题                                                   |

截至 2026-08-27，产品负责人已批准量化指标、02「国内大厂中后台」以及登录、AI 收件箱、审核工作台和账单列表；`docs/m0-evidence.md` 已记录全部门禁通过，独立只读复审结论为 blocker/major/minor 均 0。M0 据此完成，但不自动进入 M1。

## 四、M1 输入边界（已批准）

| 维度           | 批准基线                                                |
| -------------- | ------------------------------------------------------- |
| 文件类型       | JPEG、PNG、WebP、PDF                                    |
| 单文件大小     | 最大 20 MiB                                             |
| PDF 页数       | 最大 20 页                                              |
| 图片像素       | 最长边最大 8,000 px；超过时在隔离处理中缩放，不改原件   |
| 模型图片副本   | 经 8 位 RGBA 像素缓冲编码的 PNG；`document-normalize/2` |
| 每次上传       | 1 个 Document；批量上传不属于 M1                        |
| 内容验证       | 扩展名、声明 MIME、文件签名字节必须一致                 |
| 加密或损坏 PDF | 明确拒绝并返回可操作原因，不进入 AI 队列                |
| 原始文件       | 计算 SHA-256，按租户隔离，不可覆盖                      |
| 当前质量承诺   | 原图清晰、完整、无遮挡，关键字段无需增强即可人工辨读    |

边界输入必须在进入存储和任务系统前验证。拒绝响应不得包含服务器路径、Provider 凭据或内部堆栈。

M1 对通过边界的多页 PDF 必须处理全部页面并保留逐页证据。文档级字段或彼此独立、未跨页延续的明细可进入审核；出现跨页续行、重复表头归属不明、跨页合计冲突或无法确定页序时必须进入 `blocked`，不得丢页或拼接猜测。M2 才实现跨页明细重建和复杂分页审阅。

当前产品只对清晰、完整、无遮挡且关键字段可直接人工辨读的原始支付详情页和发票承诺提取体验。低清、失焦、强反光、屏幕摩尔纹、关键字段裁切或遮挡的文件仍可按技术上传边界接收，但模型准确率不属于当前质量承诺；系统必须把不确定、缺失和冲突保留在人工审核或明确失败路径，不能据此补值或自动创建 Fact。该边界不允许在运行时伪造“清晰度通过”，也不允许静默拒绝技术格式有效的文件。

## 五、Provider 与任务边界（已批准）

### Provider 能力检测

保存 ProviderConfig 前必须验证：

1. Base URL 与认证可用；
2. 指定模型可调用；
3. 图片内容可输入；
4. ProviderConfig 显式选择的 `json_schema` 或 `json_object` 输出模式可用；`json_schema` 由 Provider 执行版本化 Provider-facing 根 Schema，`json_object` 由版本化任务说明定义最小票面原文 JSON，并在返回后执行权威 `bill-visible-text/2` 根身份校验；嵌套原始输出无需修复即可保留给字段级 Claim 校验；
5. 超时和错误能够被归类；
6. 检测过程不把 API Key 或完整响应写入日志。

`bill-visible-text/2` 是模型输出的唯一权威本地 Envelope Schema，当前确定性 Provider 投影为 `bill-visible-text-provider/2`；`document-claim/3` 是内部 Claim 的唯一权威 Schema。Visible Text 只硬校验有效 JSON、封闭根对象、版本、文档类型和 Payment/Invoice/Trip 根成员；嵌套业务字段由 Mapper 与 Claim Validation 逐项校验。Provider 投影不得按供应商品牌分支。Adapter 不得删除 `null`、删除数组元素、补默认值、改字段名、剥离 Markdown、截取 JSON 片段或修复非法输出。

模型必须按 `bill-visible-text-cn/2` 只返回文档类型、固定 Payment/Invoice/Trip 业务路径及每个非空值的 `{text,page}`；`text` 是值本身的票面原文，`page` 是一基页码。禁止裸业务标量、内部 Claim、minor units、独立 `evidence`、归一化值、置信度、问题列表、解释、Trip 归属和计算得到的空白字段。唯一 `claim-mapper/4` 把同一 `text` 绑定为 Evidence，并确定性处理批准的币种和金额表示、日期、交易时间、显式时区、中文默认时区、数量、`null`、明细顺序、Trip 日期和审核专用补充字段。发票 `amount_with_tax` 只能映射到 `total_minor`，`amount_without_tax` 只能进入补充审核字段；Mapper 不得纠正字符、推断缺失值或从数量与金额计算空白单价。单字段失败必须形成显式 blocked Validation 并保留其他正确字段；只有无效 JSON 或错误根身份可以不形成 Claim。全部契约版本必须写入 AiRun 与评测冻结配置。

只有通过全部能力检测且检测时显式输出模式、Provider-facing Schema 身份与当前运行时完全一致的配置才能设为活动配置。供应商名称不能替代检测结果；禁止自动切换模式，Schema 投影变化必须重新检测。

### 调用与任务

| 维度             | 批准基线                                                                                                                                             |
| ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| 连接超时         | 10 秒                                                                                                                                                |
| 单次模型请求超时 | 60 秒                                                                                                                                                |
| 自动重试         | 对 429、明确的临时 5xx、网络中断，或模型输出在 JSON / strict Provider-facing / 本地根身份验证阶段失败重试 1 次；票面字段、业务与 Evidence 失败不重试 |
| 总任务期限       | 150 秒；超过后进入明确失败或可重试状态                                                                                                               |
| 默认并发         | 每实例 2 个 AI Job，可配置范围 1–8                                                                                                                   |
| 队列基线         | 50 个待处理 Job 不丢失、不重复执行                                                                                                                   |
| 取消             | 仅可取消尚未创建 Fact 的 `queued`、`processing`、`needs_review` 或 `blocked` 非终态；取消后不得继续写 Claim 或 Fact                                  |
| 重启恢复         | 租约到期的 Processing Job 在 2 个任务期限内被恢复或明确失败                                                                                          |

禁止自动切换模型、供应商或 Base URL。重试不得改变 Prompt、Schema 或模型版本。

模型输出重试必须复用同一已准备请求，不得把失败响应拼入第二次请求；第一次失败的 AiRun、隐私安全诊断和 attempt 必须保留，且不得产生 Claim。第二次仍失败时 Job 明确失败。输入规范化、Claim Mapper、Claim Schema、字段类型、Evidence、业务校验、认证和权限失败不得自动重试；只要根身份有效，局部缺失、冲突、类型或证据问题必须形成一个可审核的 `blocked` Claim并保留其他正确字段。两次请求共同受 150 秒总任务期限约束。评测证据必须冻结 `schema_validation_single_retry/1` 策略身份并报告最终 attempt_count。

用户显式重试只允许尚未创建 ClaimSet 的 `failed` Job，复用 Job、递增 attempt_count 并创建新 AiRun；`needs_review` 与 `blocked` 只允许完整用户修订、驳回或取消，不能重新提取。

## 六、模型评估边界（M4 发布门禁，当前 M1 观测指标）

本节保留 2026-08-27 批准的高精度目标和完整评测协议，但自 2026-08-30 起不再阻塞其余 M1 开发。当前 M1 仍可用真实开发集记录这些指标以发现回归；未达到数值必须如实记录，不能写成通过。至少 100 份、三次取最差的正式运行以及下列准确率门槛统一移至 M4 首次正式发布门禁。这项调整不构成 M1 完成或 M2 启动授权。

M1 当前模型侧继续开发的前提改为安全闭环，且 v5 已提供对应证据：

1. 每个受测 Document 最终必须形成可审核 Claim，或进入带稳定错误码的明确失败；不得丢任务或伪成功；
2. 所有本地接受的模型根对象必须通过权威 Envelope Schema；局部错误必须形成 Validation 并保留可用字段；
3. AI 直接创建或修改 Fact 必须为 0，用户能够在审核工作台修订、确认或拒绝；
4. Provider、Prompt、Schema、Mapper、重试和输入版本仍须可追溯，真实资产继续遵守隔离与逐次授权边界；
5. 安全、租户、幂等、证据、失败暴露、人工审核和确定性业务规则没有任何例外。

### 数据集

- M4 模型业务质量发布评估集至少包含 100 份固定中文真实场景 Document；“真实场景”指可回溯原始公开页面、且对应一次真实交易或真实开具行为的公开业务单据，或用户逐批明确授权的本地单据。政府/平台票样、产品演示、教程构造图、空白模板和程序化排版生成的票据均不得计入。
- 支付与发票各不少于 40 份；剩余样本覆盖模糊、裁切、缺页、冲突、重复和非法格式。
- 主要场景固定为支付截图、单项目发票、多项目发票、低质量/冲突文档、非法/不支持文档；任一主要场景不得少于 15 份，同一 Document 可带多个场景标签。
- M1 的 `payment` 质量样本只接受微信支付或支付宝的单笔完整支付/账单详情页：页面必须只对应一笔交易，并能看到交易状态、金额、业务日期时间和交易对方或商户。账单列表、聊天/服务通知卡片、优惠立减页、扣费凭证、两笔或多笔交易拼图、教程箭头、第三方记账浮层、文章拼图、遮罩、马赛克、模糊涂抹或关键字段裁切均不得计入；实体消费小票当前不进入 M1 调优集或发布集。
- `invoice` 质量样本必须是完整真实发票，不得存在遮罩、马赛克、模糊涂抹、教程标注或多文档拼图；PDF 查看器边框仅在不遮挡发票且单页边界明确时允许。
- 数据集保存期望结果、允许的规范化方式、失败类别、来源类别、获取日期、内容 SHA-256、授权或公开依据和敏感级别。
- 质量样本来源只允许为可追溯到原始公开页面的真实业务图片，或用户逐批明确授权的本地资料；图片搜索页只作为发现入口，必须登记原始页面与原图 URL。政府/平台官方资料若只是票样、演示或模板，只能用于版式研究。禁止从生产数据库、邮箱、上传目录、模型日志或其他运行时数据源自动采集。
- 程序生成的纯合成资产继续用于 Mock、契约、评分器和失败路径测试，但不得计入模型业务质量的样本数、预检指标或发布门禁。历史 `m1-synthetic-v1/v2` 与 `m1-prompt-dev-v1/v2` 只保留为技术诊断身份。
- 公开真实图片允许按原样保存在本地隔离评测区，包括公开图片中已经出现的企业或自然人信息；原图、真实字段值和个人信息不得提交到 Git、写入仓库文档/日志/生产镜像或在回复中展示。调用视觉模型会把图片发送给活动 Provider，执行任何真实样本预检或正式运行前必须记录 Provider 主机与样本数量，并取得产品负责人当次明确授权。
- 真实原图、逐字段标注、非官方来源定位和原始模型结果全部位于 Git 忽略、Docker 构建排除且权限为 `0600` 的本地评测区；不得写入仓库文档、应用日志、构建上下文或生产镜像。

### 冻结与计分协议

- 本地评估清单必须记录 `sample_id`、文档类型、场景标签、期望字段、缺失字段、证据页码/区域、允许的规范化结果、期望失败类别、来源类别和原图哈希；清单一经发布冻结，修改必须产生新版本。仓库内只保留不含真实字段值和非必要来源定位的安全摘要。
- 支付关键字段固定为金额、币种、商户、交易业务日期/时间；发票关键字段固定为发票号码、开票日期、价税合计、币种、销售方和购买方。某类型不适用的字段不进入分母。
- M1 中文支付默认时区固定为 `Asia/Shanghai`。图片出现完整本地日期与时间但未显示时区时，`transaction_time` 必须按 `+08:00` 确定性规范化，`source_timezone` 必须为 `Asia/Shanghai`；时间证据仍逐字保存图片中的本地时间，时区来源标记为产品默认而非 OCR。图片缺少完整日期/时间或显式时区与默认值冲突时不得补造，必须按缺失/冲突门禁处理。
- 名称比较只使用确定性规范化：Unicode NFKC、去除首尾空白、连续空白折叠为一个、拉丁字母大小写折叠；只包住整个值的可见中英文括号可视为等价，不使用模糊相似度。规范化后与清单期望值完全一致才计为正确。数量按普通非负十进制语义比较，`1` 与 `1.0` 等价。
- 字段完全一致率的分母是清单中该字段期望存在的样本数；缺失、类型错误、额外默认值或规范化后不一致均计为错误。期望缺失却返回值的样本单独计入 Validation 失败断言，不能从分母中隐藏。
- 文档类型分类准确率的分母只包含通过 M1 上传边界并实际进入分类阶段的样本，期望类别固定为 `payment`、`invoice` 或 `unknown`，预测必须完全一致；`unknown` 表示文件格式受支持但内容无法归为 Payment/Invoice。因格式、文件签名、加密、损坏、大小或页数在模型前被拒绝的样本不进入该分母，只计入各自的期望失败断言。
- 关键字段证据覆盖率的分母是本地接受结果中的全部关键字段；页码必须一致，且 quote 或 region 至少一个能直接支持冻结期望值。冻结的较长 quote 不是必须逐字复现：值本身的最小充分片段可以通过，例如 `¥` 支持 CNY，`¥68.00` 支持长金额片段；无关或仅部分相同的数字不能通过。文档级失败不伪造字段证据。
- 缺失/冲突召回率的分母是清单标注为缺失或冲突的全部关键字段事件；只有进入明确 `error`/`blocked` 或需要人工判断的结果才算召回。
- 每次发布评估冻结 ProviderConfig 安全指纹、Base URL 主机标识、模型、显式输出模式、Prompt、Visible Text Schema、Provider-facing Schema 版本与 SHA-256、Claim Schema、Claim Mapper、输入处理版本、超时和支持时的 temperature/seed。完整数据集独立运行 3 次，以三次中最差结果判定；任一次未完成也视为失败。
- 权限为 `0600` 的本地原始逐样本结果、聚合脚本版本、三次运行摘要和失败样本 ID 构成完整证据；仓库只记录原始证据哈希、安全失败 ID 和聚合指标。评估脚本只读取冻结的本地真实评估资产，不读取任何运行时数据源。

### M4 正式发布指标

| 指标                                    | 批准门槛 |
| --------------------------------------- | -------- |
| 本地接受结果的 JSON Schema 合法率       | 100%     |
| 文档类型分类准确率                      | >= 97%   |
| 金额十进制提取并精确换算后的完全一致率  | >= 98%   |
| 发票号码完全一致率                      | >= 95%   |
| 日期规范化完全一致率                    | >= 95%   |
| 商户、买方、卖方规范化一致率            | >= 90%   |
| 关键字段证据覆盖率                      | 100%     |
| 缺失/冲突关键字段进入审核或阻断的召回率 | 100%     |
| AI 直接创建 Fact                        | 0 次     |

关键字段证据至少包含页码以及原文摘录或区域定位之一。模型原始输出不合法时必须产生明确 Validation Result，不能通过补默认值伪装成成功。

模型、Prompt 或 Schema 版本改变时必须在 M4 正式发布前完整重跑冻结评估集。任何指标下降都必须解释并由产品负责人明确接受，否则不得正式发布；该规则不阻塞当前已授权的其余 M1 开发。

### 调优集与小样本预检

- Prompt 或 Provider 输出契约的业务质量调优必须使用独立版本化的中文真实场景开发集，不能从冻结发布集挑选失败样本或复用其图片；纯合成开发集只允许验证契约可执行性，不能证明质量提升；
- 调优集和预检结果必须标记为不具备发布证据资格，不能改名为 `run-1`、计入三次正式运行或替代完整冻结评测；
- 真实小样本预检至少使用 16 份与发布集零图片重合的中文真实场景 Document；当前 M1 期间准确率按本节指标观测但不阻塞其余已授权开发，安全闭环仍要求结果进入可审核 Claim 或明确失败、本地接受根 Schema 100%、AI 直接创建 Fact 为 0；
- Mock 或评分器自检只能证明请求、Schema、校验和计分可执行，不能宣称模型质量达到或接近门槛；
- 任何下一次 100 样本真实诊断或正式评测仍须取得产品负责人当次批准；M4 正式发布评测必须满足本节全部门槛。

## 七、确定性业务验收

必须验证：

- 金额、税额与价税合计只使用整数最小单位运算；M1 接受 `CNY`、`USD`、`EUR`（exponent 2）与 `JPY`（exponent 0），十进制输入超过 exponent 时必须阻断，不做 float 计算或静默舍入；
- 日期可区分业务日期与时间戳，不依赖服务器本地时区；
- 同租户 Document SHA-256 重复必须返回 `duplicate_document` 且不创建第二个 Job；未删除 Invoice 的 NFKC/trim/空白折叠/拉丁大小写规范化号码重复必须 `blocked`；显式幂等键规则只有一个实现位置；
- 同一确认请求重复提交不会产生第二个 Fact；
- 重复文件上传产生明确提示，不静默合并或覆盖；
- Claim 修订保留前后版本和操作者；
- 用户完整修订必须覆盖 `needs_review -> needs_review`、`needs_review -> blocked`、`blocked -> blocked` 与 `blocked -> needs_review`；状态切换、旧 revision `superseded`、当前 revision 与 ValidationResult 在同一事务中提交；
- 文档类型修正和 InvoiceItem 新增、删除、修改、重排必须使用稳定 `item_key`；新路径允许空 `supersedes`，既有路径必须同路径衔接，删除路径必须留下 `absent` 墓碑，Fact 不得读取墓碑或沿旧 revision 补值；
- 拒绝 Claim 不创建 Fact；
- Validation 失败不能被 UI 或 API 参数绕过；
- 所有 Fact 字段能够反查 Source、AiRun、FieldClaim 和 ReviewDecision。

### M1 最小一对一关联

- 候选生成只读取同租户、未删除、未关联的相反类型 Fact；币种与金额最小单位必须完全一致，Payment 交易业务日期与 Invoice 开票日期相差不得超过 30 天。
- 候选名称规范化固定为 Unicode NFKC、去除首尾空白、连续空白折叠为一个、拉丁字母大小写折叠，并按“规范化名称完全一致优先、日期差升序、目标 Fact ID 升序”稳定排序；名称不一致只能产生警告，不能伪造为一致或自动接受。
- 存在候选时，确认请求必须显式提交一个候选 ID 或 `reject_all`；不存在候选时必须提交 `no_candidate`。缺失、伪造、跨租户、过期、已删除或已被关联的候选一律拒绝。
- 接受候选在同一事务中创建 Fact、ReviewDecision、候选决定、唯一 PaymentInvoiceLink 和 AuditEvent；拒绝全部候选在同一事务中创建 Fact、拒绝决定和 AuditEvent，但不创建 Link。
- 同一幂等键重放必须返回完全相同的 Fact、候选决定与可选 Link；不同请求并发接受同一 Payment 或 Invoice 时最多一个成功，失败请求不得部分创建 Fact。
- 用户修订必须保存 revision actor；唯一 AI 初版必须保存产生它的 AiRun，任何 revision 都不能出现作者缺失或双作者。用户 revision 必须是完整快照；上一 revision 已存在的未修改字段及其已有证据要复制并按同路径指回，新路径和删除墓碑遵循本节规则，Fact 创建不得跨 revision 补字段。

最小纯合成场景固定包含：匹配候选并接受、匹配候选但拒绝全部、无候选确认、跨租户候选注入、候选在确认前被删除、两个审核并发争用同一 Fact、接受请求幂等重放。每个场景同时断言 Fact 数、Link 数、候选决定、AuditEvent、Job 终态和无跨租户可见性。

### M2 首切片：支付—发票金额分配（已通过）

M2 首切片以本节替换 M1 的“金额完全一致、每端最多一条活动 Link”运行时规则；M1 历史证据仍按原规则保留。

- 候选目标必须同租户、未删除、类型相反、币种一致、业务日期相差不超过 30 天，且事务外读取时仍有正数可分配余额；金额是否完全相等只形成原因代码，不再是硬条件。
- Review API 必须为每个候选返回总额、当前已分配金额、当前剩余金额和可用状态；任何数字均为整数最小单位，`remaining_minor = amount_minor - allocated_minor`。
- 存在候选时，确认请求必须选择 `allocate_candidates` 并提交至少一条分配，或选择 `reject_all` 且分配数组为空；不存在候选时必须选择 `no_candidate`。旧 `accept_candidate` 和 `selected_candidate_id` 不再是活动契约。
- 每个分配项必须包含唯一的当前候选 ID 和 `1..9,007,199,254,740,991` 范围内的 `allocated_minor`；同一请求重复候选、零、负数、非整数、溢出、跨租户或不属于当前 ClaimSet 的候选必须拒绝。
- 本次分配合计不得超过正在创建 Fact 的金额；每项分配不得超过目标 Fact 在确认事务内重算的剩余余额。允许当前 Fact 或目标 Fact 保持部分未分配。
- 每条 Link 必须保存与双方 Fact 一致的币种和正数分配金额；同一 Payment/Invoice 对同时最多一条活动 Link，不同对可以形成一对多或多对一。Payment 或 Invoice 的活动 Link 合计不得超过自身金额。
- Fact、ReviewDecision、完整候选决定集、所有 Link、AuditEvent、Claim 与 Job 终态必须在同一事务中提交。任一候选删除、余额变化、币种冲突、重复活动对或并发超分配时，整个请求失败且不留下部分 Fact、决定或 Link。
- 幂等身份包含期望 revision、关联模式和按候选 ID 排序后的完整分配计划。相同键、相同计划返回相同 Fact 与全部 Link；相同键改变任一候选或金额返回 `idempotency_key_conflict`。
- Payment 与 Invoice 列表必须返回 `allocated_minor`、`remaining_minor` 和 `allocation_status`；状态仅允许 `unallocated`、`partial`、`allocated`，并由服务端活动 Link 聚合唯一计算。
- 删除任一 Fact 必须终止其全部活动 Link；未删除另一端下一次读取的已分配金额相应减少。历史 Link、候选、决定和审计仍保留。

最小纯合成场景固定包含：一笔 Payment 分配给两张 Invoice、一张 Invoice 被两笔 Payment 分配、双方部分余额、请求合计超额、目标余额超额、重复候选、非法金额、跨币种注入、跨租户注入、候选删除、相同对重复活动 Link、两个事务争用最后余额、幂等重放与同键改金额冲突、删除后余额恢复。每个场景同时断言双方余额、Fact 数、候选决定、Link 金额与币种、AuditEvent 和无部分写入。

验收结果：2026-08-30 通过。后端全量测试、静态检查与构建、Web 完整检查、浏览器场景、43 / 43 关键不变量及两层覆盖率门禁均通过；可机读摘要见 `tests/evidence/m2/gate-summary.json`，完整说明见 `docs/m2-evidence.md`。

### M2 第二切片：近似文件、字段组合和跨页重复检测

本切片扩展疑似重复提示，不替代 M1 的精确 SHA-256 和规范化发票号规则，也不包含下一切片的跨页明细重建。

- `page-visual-dedup/1` 必须从现有 8 位 RGBA 规范化页面确定性计算 64 位 dHash、64 位 aHash 和四个 dHash 检索 band；相同输入重复计算必须逐位一致。
- 视觉近似页必须同时满足宽高比差异不超过 1%、dHash Hamming 距离不超过 3、aHash Hamming 距离不超过 3；四 band 检索不得漏掉定义阈值内的候选。旋转、裁切、遮挡、屏摄或低清修复不在承诺内。
- 同租户另一个 Document 与当前页数相同且每个有序页均近似时，只生成一个 `near_file` 候选；当前 Document 内不同页或与其他 Document 的部分页近似时生成 `cross_page`，已归入整份近似的目标不再重复产生跨文档页候选。
- Payment `field_combination` 必须要求双方未删除、金额为相同正整数最小单位、币种一致、交易时间相差不超过 5 分钟且规范化商户一致；双方非空订单号一致只增加原因代码。Invoice 必须要求双方未删除、价税合计为相同正整数最小单位、币种、开票日期及规范化销售方/购买方一致；规范化发票号完全一致仍走既有 blocked 规则。
- 名称只允许 Unicode NFKC、trim、连续空白折叠和拉丁大小写折叠，不使用编辑距离、拼音、模型或供应商专属逻辑。
- DuplicateCandidate 必须携带规则版本、稳定候选键、结构化原因、同租户目标和适用页码/距离；不得复制原始图片、完整模型响应或形成可写的 Document/Fact 重复状态第二数据源。
- 每个 Claim revision 的候选按种类、距离、目标 ID 和页码稳定排序；候选超过 50 个时产生 `duplicate_candidate_limit_exceeded` blocked ValidationResult，不静默截断后允许确认。
- Review API 和 Web 必须展示候选类型、原因、目标可用性、相关页码和安全摘要；存在候选时，确认请求必须为当前完整集合逐项提交唯一 `keep_distinct` resolution。缺失、重复、伪造、跨租户、旧 revision 或多余 resolution 必须拒绝。
- 用户认为当前 Document 是重复项时必须走既有 Reject 工作流且不创建 Fact；系统不得自动合并、覆盖、删除、跳过、接受或把目标 Fact 复用为当前结果。
- resolution 规范计划 SHA-256 必须进入 ReviewDecision 幂等身份；相同键和相同完整计划重放返回同一结果，同键改变任一 resolution 必须 `idempotency_key_conflict`。
- 确认事务必须重算当前候选键；候选新增、消失或目标状态变化时返回 `duplicate_candidate_set_stale`，并保证 Fact、ReviewDecision、DuplicateCandidateDecision、PaymentInvoiceLink、AuditEvent 和 Job 均无部分写入。保存同字段的新完整 Claim revision 后必须得到新候选快照。
- 数据库必须保证 DuplicateCandidate 生成时同租户、候选形状合法、每个候选最多一个 `keep_distinct` 决定，且 Claim 只有在全部候选决定属于同一个确认 ReviewDecision 时才能进入 `confirmed`。
- UI 必须把重复候选与关联分配分区展示；每项确认控件有可见标签和键盘焦点，缺失确认的错误通过 `aria-describedby` 或等价关系关联，不能仅依赖颜色。

最小纯合成场景固定包含：JPEG/PNG 重新编码、等比缩放、明显不同页面、宽高比和双哈希阈值边界、同文档重复页、跨文档部分页、完整多页近似去重、跨租户零可见、Payment 与 Invoice 字段组合、精确发票号优先、50 项上限、revision 重算、完整/缺失/伪造 resolution、Reject 无 Fact、幂等重放、同键改 resolution、确认时集合陈旧及两个疑似重复 Claim 并发确认。每个事务场景同时断言 Claim/Job 状态、候选与决定数、Fact/Link/AuditEvent 数和无跨租户可见性。

### M2 第三切片：复杂多页 PDF 跨页明细重建与分页审阅

本切片以 `docs/decisions/0011-cross-page-invoice-review.md` 为冻结决策，不修改当前模型契约，不调用真实 Provider，也不通过本地 OCR 或版面规则重新理解发票。

- 模型一次接收按原始顺序排列的全部规范化页面，并继续按阅读顺序返回每个逻辑 InvoiceItem 一次；同一 item 的字段可以分别携带相邻页面的 `{text,page}`，本地必须保留该分组和每个字段的原始 Evidence，不能拆分、合并、纠字或计算空白值。
- 每个 InvoiceItem 的稳定 `item_key` 不因分页或 revision 改变；`sort_order` 必须唯一且严格连续为 `0..n-1`。重复、缺口或非法顺序必须产生 blocked Validation，不能等到 Fact 插入时静默排序。
- 当前 item 的有效 Evidence 页码必须形成连续集合；跳页产生 `invoice_item_page_gap`。按 `sort_order` 读取时，后一个 item 的起始页不得早于前一个 item 的结束页，否则产生 `invoice_item_page_order_conflict`。
- 当前 Claim revision 的页面计划必须只从 `FieldClaim -> Evidence -> DocumentPage` 派生：包含完整 `1..page_count`、每页字段路径与 item key，以及每个 item 的页码、起止页和跨页标志；保存新 revision 后必须重算，不得写入第二份可变业务表。
- 独立明细分布在多页、同一明细跨相邻页、页内多明细和没有明细的封面/尾页均可审核。页码越界、明细自身缺少 Evidence、跨页字段无法形成完整票面值、重复表头被当作缺字段行或全部明细与发票合计冲突时必须保持 blocked，系统不猜测删除或连接。
- 新增 `GET /documents/{document_id}/pages/{page_number}/content`，只读取现有规范化 PNG。`page_number` 必须是合法一基整数；数据库查询显式包含 tenant；reviewer 只能读取待审核或阻断 Job，跨租户、已结束 reviewer 访问和不存在页面都不得泄露资源存在性。
- Review API 必须返回 `page_count`、完整页面摘要和 `invoice_item_spans`。所有页码稳定升序，字段路径去重排序，item key 按 `sort_order` 后稳定键排序；API 不返回内部 storage key。
- Web 必须提供上一页、下一页和直接页码导航，显示“当前页 / 总页数”；选择字段时定位到首条 Evidence 页。当前页展示相关明细和始终可见的文档级字段，无 Evidence 的阻断明细仍可达；同一跨页 item 在相关页显示同一稳定身份和明确跨页范围，不复制编辑状态。
- 分页控件、当前页状态、页面图片替代文本和跨页标记必须可由键盘及辅助技术使用；200% 缩放和 768px 容错布局不得造成横向不可达或丢失确认动作。

最小纯合成场景固定包含：一页发票、多页独立明细、一个 item 的字段横跨相邻两页、跨三页连续 Evidence、页码跳跃、item 阅读顺序倒退、重复/缺口 `sort_order`、重复表头形状缺少必填金额、跨页明细合计冲突、用户 revision 重绑跨页 Evidence、空白中间页、最大 20 页、非法/越界页请求、跨租户页面读取、reviewer 终态隐藏、API 排序和 Web 分页/字段定位/编辑状态保持。每个阻断场景同时断言没有 Fact、Link、ReviewDecision 或部分终态写入。

验收结果：2026-08-30 通过。后端全量测试、静态检查与构建、Web 完整检查、13 / 13 浏览器组件场景、60 / 60 关键不变量及两层覆盖率门禁均通过；可机读摘要见 `tests/evidence/m2/cross-page-review-gate-summary.json`，完整说明见 `docs/m2-evidence.md`。

### M2 第四切片：多 Document 批量上传与逐项反馈

本切片以 `docs/decisions/0012-client-orchestrated-batch-upload.md` 为冻结决策。批量只编排既有单 Document 上传命令，不增加服务端批量端点、数据库批次状态或跨文件事务。

- Web 一次选择必须接受 1–20 个待处理文件，并按原选择顺序逐项调用现有 `POST /documents`；任意时刻最多一个请求在途。第 21 项及以后必须保留为 `batch_file_limit_exceeded` 拒绝行，不能静默丢弃。
- 每项状态只允许等待、上传中、已入队、已存在或已拒绝。状态列表顺序和项目身份在本次选择期间稳定；新选择只能在当前批次结束后开始，并替换上一份临时反馈。
- 单项继续遵守服务端 20 MiB、非空、文件名、扩展名、签名/MIME、租户隔离、对象提交补偿与同租户原始 SHA-256 精确判重。浏览器可提前拒绝明显大于 20 MiB 的文件，但不得把客户端预检作为权威安全边界。
- 中间项目的本地校验、HTTP、网络或服务端失败不得回滚已经创建的 Document/Job，也不得阻止后续项目。失败只显示安全错误码与消息；不得暴露响应正文、堆栈、路径、哈希、完整文档或 Provider 信息。
- 同次选择中两个字节相同的文件仍必须顺序送达服务端：第一项正常创建时，后一项必须显示为已存在，并且不创建第二个 Document 或 ProcessingJob。客户端不得自行合并近似或精确内容。
- 成功项目必须各自恰好产生一个不可变 Document 和一个 queued ProcessingJob；重复或失败项目不得产生 Document、Job、对象或临时元数据残留。服务端独立命令必须证明中间失败后仍可继续处理后续合法命令。
- 批次反馈只存在于当前页面内存。批次结束后 Web 最多主动刷新一次权威收件箱；持久状态始终来自 Document/ProcessingJob，不得由反馈列表推断或写回。
- 文件名、大小、逐项状态、拒绝原因与汇总必须以文字可见，并通过 `aria-live` 或等价关系通知状态变化；键盘、768px 和等效 200% 缩放不得造成横向不可达、丢失选择入口或丢失逐项结果。
- OpenAPI、数据库 Schema 与现有单 Document API 在本切片必须保持不变；若实现需要改变它们，必须先重新冻结 ADR 和验收边界。

最小纯合成场景固定包含：三个合法文件保持请求顺序并各自入队、中间服务端失败后第三项继续、同批次精确重复得到一个创建与一个已存在、空文件或伪造 MIME 后继续、单文件大于 20 MiB 的本地显式拒绝、第 21 项及以后显式拒绝、网络异常后继续、运行期间禁止再次选择、完成后只刷新一次队列、逐项安全消息、键盘操作、768px 与等效 200% 缩放无横向溢出。后端场景同时断言合法项各一份 Document/Job、失败项零残留；Web 场景断言顺序、状态、可访问名称和失败隔离。

验收结果：2026-08-31 通过。后端全量测试、静态检查与构建、Web 完整检查、4 个 Vitest 文件共 17 项测试、14 / 14 浏览器组件场景、61 / 61 关键不变量及两层覆盖率门禁均通过；可机读摘要见 `tests/evidence/m2/batch-upload-gate-summary.json`，完整说明见 `docs/m2-evidence.md`。OpenAPI、数据库 Schema 和单 Document API 均未改变。

### M2 第五切片：已确认 Fact 的独立分配调整

本切片以 `docs/decisions/0013-confirmed-fact-allocation-adjustment.md` 为冻结决策。调整只作用于已确认 Fact 之间的活动 PaymentInvoiceLink，不修改 Source、Claim、ReviewDecision 或 Fact 字段。

- `GET /allocations/{fact_type}/{fact_id}` 只接受 `payment` 或 `invoice`，返回同租户、未删除 anchor、当前活动 Link、合格目标及 `payment-invoice-allocation-plan/1` 的 `plan_hash`。合格目标必须类型相反、同币种、业务日期相差不超过 30 天，且有正数可分配余额或已与 anchor 存在活动 Link。
- 工作区必须返回 anchor 和目标的总额、当前已分配、剩余、业务日期、显示名称；目标另返回与 anchor 当前 Link、当前对分配额和 `maximum_allocatable_minor`。排序为名称规范化完全一致优先、日期差升序、目标 ID 升序。所有金额均为整数最小单位，客户端数字不能替代事务校验。
- 合格目标最多 200 个，查询第 201 个时返回 `allocation_target_limit_exceeded`；POST 完整期望计划最多 200 项。任何上限都不得静默截断、自动分页拼接或隐式排除当前活动目标。
- POST 必须携带 8–128 字符且不含空白/控制字符的 `Idempotency-Key`、64 位小写十六进制 `expected_plan_hash`、显式 `desired_allocations` 数组和 trim 后 1–500 字符的 `reason`。未知 JSON 字段、缺失数组、重复目标、空目标 ID、零/负数、非整数或超过安全上限的金额必须拒绝。
- `desired_allocations` 表示 anchor 的完整期望活动计划：新增且不改变现有项派生为 `supplement`；只移除并保持其余金额派生为 `withdraw`；改金额、替换目标或同时增删派生为 `replace`。完全相同计划返回 `allocation_plan_unchanged` 且零写入；空数组只表示明确撤销全部，不解释为“未提供”。
- Web 必须以具名复选框和金额输入编辑完整计划，显示取消选择会撤销旧 Link，要求填写理由；当当前计划非空且期望计划为空时，还必须勾选“确认撤销全部”才能提交。该 UI 门禁不能替代服务端快照、权限和输入验证。
- 事务必须先检查幂等重放，再在 immediate 写锁内重新计算当前计划 hash。首次请求 hash 陈旧返回 `allocation_plan_stale`；相同键和相同规范请求返回原 adjustment、终止/创建 Link ID 与结果 hash且 `replayed=true`，同键改变 anchor、hash、目标、金额或理由返回 `idempotency_key_conflict`。
- anchor 与每个目标必须同租户、由确认 ReviewDecision 产生、未删除、类型相反、币种一致且在日期窗口内。跨租户 ID 与不存在 ID 对外同为未找到或不可用，不能泄露资源存在性。
- anchor 期望合计不得超过自身金额；每个目标金额不得超过该目标当前剩余金额加该对当前分配金额。两个请求争用最后余额、Fact 删除或其他调整导致状态变化时最多一个成功，失败请求不得自动缩减、保留部分终止或创建部分 Link。
- 未变化 Link 必须保持原 ID、创建来源和时间。撤销或被替换的 Link 只允许设置一次 `ended_at + ended_by_adjustment_id`；新增或替换项必须创建新 ID 的不可变 Link。创建来源严格二选一为原审核 LinkDecision 或 AllocationAdjustment；终止来源严格二选一为 Fact 删除 AuditEvent 或 AllocationAdjustment。
- `payment_invoice_allocation_adjustments` 必须保存 actor、anchor、派生模式、幂等请求身份、前后计划 hash、业务理由和 AuditEvent，且不可更新或删除。活动 Link 仍是双方 `allocated_minor`、`remaining_minor` 和状态的唯一数据源。
- 每次成功调整只写一条 `payment_invoice_allocation_adjusted` AuditEvent；safe metadata 只允许模式及创建/终止数量，不得包含金额、名称、理由、完整计划或其他财务字段。
- `allocations.manage` 只授予 `owner` 与 `finance`。`reviewer`、`viewer` 对工作区读取和调整提交均为 403；四角色每个允许/拒绝单元格必须有测试。列表只对有 capability 的用户显示“调整分配”。
- 独立页面必须覆盖加载、无合格目标、有当前分配、校验失败、陈旧冲突、权限不足和成功刷新状态。错误需通过 `aria-describedby` 或等价关系关联；键盘、768px 与等效 200% 缩放不得产生横向不可达或丢失理由、撤销确认和提交动作。

最小纯合成场景固定包含：空计划补充新对、保留现有项再补充、撤销一项、撤销全部、同对增额与减额替换、换目标、同时增删、完全相同计划零写入、重复目标、非法金额、超过 200 项与目标第 201 项、缺失/空白/过长理由、缺失数组、非法路径类型、陈旧 hash、幂等重放、同键改变请求、跨租户 anchor/目标、已删除目标、错误币种、日期超过 30 天、anchor 超额、目标超额、活动对唯一、两个事务争用最后余额、Fact 删除竞态、数据库来源二选一、Adjustment/Link 不可变、权限矩阵、Web 完整计划与撤销全部确认、键盘、768px 和 384px 等效 reflow。每个失败事务同时断言 Adjustment、AuditEvent、Link 创建/终止和双方余额均无部分变化。

验收结果：2026-08-31 通过。后端全量测试、静态检查与构建、OpenAPI 客户端生成、Web 完整检查、5 个 Vitest 文件共 21 项测试、18 / 18 浏览器组件场景、72 / 72 关键不变量及两层覆盖率门禁均通过；领域/应用层为 85.86%（2,216 / 2,581），基础设施/传输层为 73.01%（2,388 / 3,271）。可机读摘要见 `tests/evidence/m2/allocation-adjustment-gate-summary.json`，M2 收口摘要见 `tests/evidence/m2/m2-closure-gate-summary.json`。

### M3 首切片：邮箱 Source、邮件与附件本地归档

本切片以 `docs/decisions/0014-connector-neutral-email-archive.md` 为冻结决策。验收只使用纯合成、本地可重复 RFC 822 输入；不存在真实邮箱连接、凭据、网络轮询或生产导入入口。

- `POST /email-sources` 只接受显示名、纯邮箱地址、IMAP 主机、端口和 `implicit_tls | starttls`；严格 JSON、CSRF、Owner 权限、8–128 字符幂等键和完整请求 hash 必须同时成立。相同键同请求稳定重放，同键改请求冲突，规范连接身份重复明确冲突。
- 数据库与 API 均不得出现密码、Token、Cookie、OAuth、客户端秘密、密文、密钥引用或可恢复凭据字段。新 Source 只显示 `pending_connection`，不得因本地注册伪装为连接成功。
- 内部 `email-message-archive/1` 只接收现有 Source、64 位不可逆外部消息键、UTC 接收时间和原始 RFC 822 流；不得暴露 HTTP/CLI/fixture 写入口，也不得在生产装配中执行网络拨号。
- 原始邮件最大 32 MiB，超出时零写入并明确失败。大小合法时 MIME 深度最大 10、总 part 最大 200、附件最大 50；结构超限或 MIME 失败必须归档完整原文并形成 `blocked` 消息，不截断、不猜测、不创建 Document/Job。
- 原始邮件和每个附件对象、hash、part 身份与状态不可变。同 Source 同外部键同原文严格重放；同键异文整体冲突，不覆盖、不增加附件、Document、Job 或审计。
- 正文不写数据库列表、不渲染 HTML、不加载远端资源、不展开压缩包，也不进入模型。主题、地址和附件名只作为租户私有字段返回，不得进入日志或 AuditEvent safe metadata。
- 每封可解析邮件最多 50 个附件，逐项明确 `queued`、`existing_document` 或 `archived_only`。合法 JPEG/PNG/WebP/PDF 且不超过 20 MiB 的项必须经过既有名称、签名、MIME、页数和租户边界；非法或不支持项不阻止其他合法项。
- 新合法附件创建一个既有 Document 与 `document_process` Job；同租户 SHA-256 命中已有 Document 时只链接并标记 `existing_document`。不得创建邮件专用 Job、Claim、Fact、Prompt、Mapper 或余额/状态第二数据源。
- EmailMessage、全部 EmailAttachment、本轮新 Document/Job 和安全 AuditEvent 必须原子写入。对象提交失败必须补偿整个新消息聚合、本轮新 Document/Job 和已提交对象；已有重复 Document 不得被补偿删除。
- 邮件拥有的附件对象不随未确认 Document 删除；删除后 EmailAttachment 的 Document 链接为空，归档原文和附件仍可下载。手工上传的原件删除语义不变。
- `email_sources.manage` 只允许 Owner；`email_archive.read` 只允许 Owner/Finance。Reviewer 只沿既有当前审核 Source 边界访问对应 Document，Viewer 不可枚举 Source、邮件或附件。四角色读取/创建矩阵、跨租户消息、原文和附件下载均有测试。
- 消息 API 使用稳定游标分页，默认 50、最大 100，并明确返回 `next_cursor`；列表不得返回正文、存储键、外部消息键、原始头或对象 hash。原文和附件下载必须租户隔离、强制附件语义、`private, no-store` 和浏览器不执行边界。
- Web 必须覆盖待连接/已有归档、无来源、空邮件、blocked、混合附件、游标加载、权限不足、加载和失败状态；不出现任何凭据字段。键盘、768px 与 384px 等效 200% 回流不能丢失表单、状态、分页或下载动作。

最小纯合成场景固定包含：Source 规范化、非法地址/主机/端口/TLS、注册重放/同键冲突/身份重复；简单邮件、编码主题、嵌套 multipart、具名 inline 与 attachment、无附件、非法 MIME、深度 11、part 201、附件 51、原文 32 MiB 边界；合法图片/PDF 入队、不支持/空/过大/伪造 MIME 单项隔离、两封邮件精确重复 Document；消息重放、同键异文、跨租户、并发同键；数据库与对象提交失败补偿、邮件拥有对象的 Document 删除；安全审计、分页、强制下载、四角色权限和 Web 响应式/键盘状态。每个失败路径同时断言消息、附件、新 Document/Job、AuditEvent 和对象无未说明部分残留。

验收结果：2026-08-31 通过。后端全量测试、静态检查与构建、OpenAPI 客户端生成、Web 完整检查、6 个 Vitest 文件共 24 项测试、21 / 21 浏览器组件场景、83 / 83 关键不变量及两层覆盖率门禁均通过；领域/应用层为 85.61%（2,468 / 2,883），基础设施/传输层为 73.88%（2,798 / 3,787）。可机读摘要见 `tests/evidence/m3/email-archive-gate-summary.json`。

### M3 第二切片：行程 Fact 与确定性单据归属

本切片以 `docs/decisions/0015-trip-fact-attribution.md` 为冻结决策。实现与验收只使用纯合成图片契约、Claim、Fact 和本地数据库数据；不得调用真实 Provider、执行正式正确率评测或连接外部系统。

- 活动链唯一为 `bill-visible-text-cn/2 -> bill-visible-text/2 -> bill-visible-text-provider/2 -> claim-mapper/4 -> document-claim/3`。根对象严格包含 Payment、Invoice、Trip 三个业务成员并与 `document_type` 三选一；`unknown` 时全部为空。旧版本不得继续接受、双写或自动转换。
- Trip 只抄 `origin`、`destination`、`start_date`、`end_date`、`traveler_name`、`transport_type`、`booking_reference` 的票面 `{text,page}`。本地只规范日期与文字表示；目的地、起止日期缺失，日期非法或结束早于开始必须 blocked，不得计算或推断。
- Trip 必须沿现有 Document/AiRun/Claim/Revision/Review 创建，模型与 Mapper 不能写 Fact。确认事务必须创建一个 Trip、ReviewDecision、全部 FactFieldOrigin、重复候选决定和安全 AuditEvent；任一失败全部回滚。
- Trip 确认不接受 Payment/Invoice `association_mode` 或 allocations；Payment/Invoice 的既有完整计划要求不变。三类 confirm 都必须提交当前全部 `keep_distinct` 重复决定，幂等键同请求返回同一 Fact，同键改变请求冲突。
- `trip-attribution/1` 只从已确认、未删除 Fact 计算：业务日期在行程闭区间、前后 3 日或活动 PaymentInvoiceLink 另一端已归属当前 Trip。每项返回稳定 reason code；建议不自动写入，不命中仍可在全部视图显式选择。
- 每个 Payment/Invoice 同时最多一条活动 TripFactAssignment。assign、move、unassign 必须携带期望当前 Link、8～128 字符幂等键与 1～500 字符理由；事务内重检租户、资源存活、当前 Link 和目标。空到空、同 Trip、陈旧期望、跨租户或已删除资源零写入。
- Decision 与 Link 创建字段不可变、不可删除；旧 Link 只允许一次终止。移动在同一事务终止旧 Link 并创建新 Link；删除 Payment/Invoice/Trip 在删除事务终止相关活动 Link，历史决定与 Link 保留。
- `GET /trips` 返回未删除 Trip 与活动归属计数；候选 API 支持 `all | suggested | assigned`、默认 50/最大 100 的不透明游标和确定性排序，不静默截断；写 API 严格 JSON、CSRF、路径/ID、幂等和安全错误。
- `facts.read` 允许 Owner/Finance/Viewer 读取 Trip 与候选，Reviewer 拒绝；`trip_assignments.manage` 只允许 Owner/Finance。Reviewer 可沿 `claims.review` 审核 Trip，Owner 才可删除；四角色、跨租户和不存在资源不泄露均有测试。
- Web 必须覆盖 Trip 审核/修订/确认、无行程、无候选、全部/建议/已归属筛选、加载更多、assign/move/unassign、理由错误、陈旧冲突保留、权限不足、加载/离线和成功刷新。键盘、768px 与 384px 等效 200% 回流不能丢失证据、筛选、理由或动作。
- `trip_fact_assignment_changed` safe metadata 只允许 action 与 fact_type；不得记录地点、姓名、预订编号、日期、金额、理由、证据或 Provider 数据。

最小纯合成场景固定包含：Payment/Invoice/Trip/unknown 根互斥与多余成员；Trip 全字段、可选字段缺失、必填缺失、非法日期、结束早于开始、跨页证据、用户完整 revision、重复候选未解决、确认重放/改请求；行程列表计数；区间内、边界前后 3 日、4 日外、活动 PaymentInvoiceLink 另一端建议、无建议手工归属；assign、move、unassign、空到空、同 Trip、陈旧 Link、幂等重放/改请求、同 Fact 并发竞争、跨租户、已删除 Fact/Trip、删除三类 Fact 的终止语义、Decision/Link 不可变、三种筛选和多页游标；四角色权限、严格 JSON/CSRF、Web 键盘与响应式。每个失败事务同时断言 Decision、AuditEvent、旧 Link、新 Link 和 Fact 均无部分变化。

验收结果：2026-08-31 通过。固定 Go 1.26.7 禁网容器内的全量测试、静态检查与构建全部通过；OpenAPI 客户端生成和 Web 完整检查通过，7 个 Vitest 文件共 27 项测试通过；邮箱、既有状态矩阵与行程归属浏览器组件场景 24 / 24 通过，其中 3 项新增行程场景覆盖冲突恢复、严格可空请求、四角色、键盘及 768px/384px 回流；关键不变量 92 / 92（100%）通过，其中新增 9 个行程映射；领域/应用层覆盖率 85.53%（2,594 / 3,033），基础设施/传输层覆盖率 74.07%（3,016 / 4,072）。本轮只使用纯合成数据，未调用真实 Provider、未连接真实邮箱或外部账号。可机读摘要见 `tests/evidence/m3/trip-attribution-gate-summary.json`。

### M3 第三切片：报销快照、状态历史与确定性政策提示

本切片以 `docs/decisions/0016-reimbursement-workflow-policy-findings.md` 为冻结决策。实现与验收只使用 confirmed Payment/Invoice、Trip、活动本地 Link 和纯合成业务数据；不得调用模型、读取邮件正文、连接真实外部报销系统或创建绕过 Claim 的新 Fact。

- 预检只接受同一未删除 Trip 的 1～200 个唯一活动 TripFactAssignment ID；Assignment 必须对应未删除 Payment/Invoice。空、重复、错误 Trip、已终止、跨租户、已删除和第 201 项均明确失败，不自动移除或截断。
- `reimbursement-policy/1` 唯一产生三类 Finding：Payment 没有选中 Link 发票时 `missing_invoice`；有选中相反类型 Link 但其金额合计不等于该 Fact 总额时 `amount_conflict`；同一 Fact 已在其他 submitted/reimbursed 记录时 `duplicate_reimbursement`。rejected 不计重复；Invoice 独立存在不产生“缺少支付”。
- 金额只读取整数最小单位和活动 PaymentInvoiceLink；混合币种必须按币种分别汇总，禁止换汇或相加。Finding key、规范选择顺序、Finding 顺序与 `snapshot_hash` 在相同输入下必须稳定。
- 预检不落库。提交回传同一选择、64 位 `expected_snapshot_hash`、完整且恰好相等的 `acknowledged_finding_keys`、1～500 字符理由和幂等键；事务内用同一规则重算。遗漏/伪造 Finding、选择/Link/其他报销状态变化或超过 1,000 Finding 全部零写入。
- 成功提交必须原子创建一条 submitted Reimbursement、1～200 个不可变 Item、完整 Finding、submit Decision 和安全 AuditEvent。Item 冻结 Trip/Assignment/Fact 显示与金额快照但不参与当前 Fact/余额/归属计算；软删除来源后历史仍可解释并标明来源已删除。
- 状态图只允许 submitted 到 reimbursed/rejected，以及两个终态到 submitted 重新打开；同一 Trip 同时最多一个 submitted。每次变化提交期望状态、期望 version、理由和幂等键，并原子追加 Decision/AuditEvent 与更新状态/version；同状态、非法跨越、陈旧和并发竞争零写入。
- 创建和状态幂等身份覆盖全部规范请求；相同键同请求返回原 Reimbursement/Decision 且 `replayed=true`，同键改变 Trip、选择、快照、确认、状态、版本或理由返回 `idempotency_key_conflict`。Decision、Item、Finding 与 Reimbursement 不得删除，历史创建字段不可更新。
- API 必须实现严格 JSON/CSRF 的预检与提交、默认 50/最大 100 的不透明列表游标、详情历史和状态决定；错误不得泄露 SQL、跨租户存在性、理由、金额、地点、Provider 或邮件数据。
- `reimbursements.read` 允许 Owner/Finance/Viewer；`reimbursements.manage` 只允许 Owner/Finance。Reviewer 不能列表、详情、预检或写入；四角色、跨租户和真实外部 ID 拒绝均有测试。
- Web 必须覆盖 Trip/Assignment 显式选择、分页、无项目、无提示、三类提示、混合币种、完整确认门禁、提交、状态变化/重新打开、详情历史、来源已删除、快照/版本冲突保留、只读、加载/失败/离线和权限不足。键盘、768px 与 384px 等效 200% 回流不能丢失选择、提示、确认、理由或动作。
- `reimbursement_submitted` 与 `reimbursement_status_changed` safe metadata 只允许动作、前后状态和 Item/Finding 数量；不得记录 Trip/Fact/Assignment ID、地点、日期、名称、金额、币种、理由或 Finding 明细。

最小纯合成场景固定包含：空/重复/201 个 Assignment、错误 Trip、已终止/删除/跨租户；Payment 无发票、选择外发票、全额/部分/多对多 Link、Invoice 单独存在、混合币种；其他 submitted/reimbursed/rejected 与多条重复 Finding；稳定排序/key/hash、完整/遗漏/多余确认、Link/Assignment/报销状态陈旧；首次提交、严格重放/改请求、同 Trip 并发；submitted 到两终态、两终态重新打开、非法/同状态/陈旧版本、重新打开唯一冲突；Item/Finding/Decision 不可变、软删除后历史、安全审计、列表/详情游标；四角色、严格 JSON/CSRF、Web 键盘和响应式。每个失败事务同时断言 Reimbursement、Item、Finding、Decision、AuditEvent 和当前状态/version 均无部分变化。

验收结果：2026-08-31 通过。固定 Go 1.26.7 禁网容器内的全量测试、静态检查与构建全部通过；OpenAPI 客户端生成和 Web 完整检查通过，8 个 Vitest 文件共 30 项测试通过；M1/M2 状态矩阵与 M3 三个工作区浏览器组件场景 29 / 29 通过，其中 5 项报销场景覆盖三类提示、混合币种、无提示、快照/版本冲突、四角色、键盘及 768px/384px 回流；关键不变量 104 / 104（100%）通过；领域/应用层覆盖率 85.42%（2,900 / 3,395），基础设施/传输层覆盖率 75.13%（3,417 / 4,548）。本轮只使用纯合成数据，未调用真实 Provider、未连接真实邮箱或外部账号。可机读摘要见 `tests/evidence/m3/reimbursement-workflow-gate-summary.json`，M3 收口见 `tests/evidence/m3/m3-closure-gate-summary.json`。

## M4 首切片：确定性 Fact 洞察与筛选查询

本切片以 `docs/decisions/0017-deterministic-fact-insights-and-query.md` 为冻结决策。实现与验收只读取当前未删除 Payment/Invoice、活动本地 Link、活动 Trip 归属和纯合成数据；不得调用模型、写回业务状态、连接外部系统或持久化统计副本。

- `GET /api/v1/insights` 只接受封闭、单值查询参数：`fact_type=all|payment|invoice`、成对且包含边界的 `date_from/date_to`、`currency=CNY|USD|EUR|JPY`、`allocation_status=all|unallocated|partial|allocated`、`trip_scope=all|assigned|unassigned`、仅在 assigned 下可用的 `trip_id`、不透明 `cursor` 和默认 50/最大 100 的 `limit`。未知、重复、空值、非法日期/范围/枚举/ID/上限或参数组合必须明确拒绝。
- Payment 日期唯一读取确认时持久化的 `business_date`，Invoice 读取 `invoice_date`；过滤包含起止日。当前分配只聚合活动 PaymentInvoiceLink，当前行程只读取活动 TripFactAssignment；软删除 Fact 或 Trip、已终止 Link/Assignment 不进入当前结果。
- 投影逐项返回 Fact 类型/ID、业务日期、安全显示名、整数最小单位总额/已分配/剩余、币种、`unallocated|partial|allocated` 和可选当前 Trip 摘要；同一 Fact 不重复。已分配必须在 0 到总额之间，任何无效持久化状态或超过安全整数累计上限返回显式安全错误，不截断、不饱和、不伪成功。
- 汇总按币种和 Fact 类型分别返回 `count`、`total_minor`、`allocated_minor`、`remaining_minor` 及三种分配状态数量；禁止生成 Payment+Invoice 总金额或跨币种总金额。空结果返回空汇总和空明细。
- 排序固定为 `business_date DESC, fact_type DESC, fact_id DESC`。游标版本为 `fact-insight-cursor/1`，绑定规范筛选 hash 与最后一项排序键；未知字段、错误版本/编码/身份、筛选不匹配或不存在的边界必须拒绝，不回退第一页。
- 汇总与当前页必须来自同一 PostgreSQL `REPEATABLE READ READ ONLY` 事务快照。实现不得新增统计表、触发器维护统计、运行时缓存或后台聚合；所有 SQL 使用参数化接口并按租户过滤。
- `insights.read` 只允许 Owner/Finance/Viewer；Reviewer 拒绝且不能据此枚举 Fact、Trip 或金额。端点只读，不创建 AuditEvent；错误不得泄露 SQL、跨租户存在性、名称、金额、Provider、邮件或凭据数据。
- Web `/insights` 必须覆盖完整筛选、清除、多币种/多类型分组、三种分配状态、已/未归属、具体 Trip、空结果、加载更多、失败重试、离线和权限不足。键盘、错误关联、可见焦点、768px 与 384px 等效 200% 回流不得依赖横向滚动；禁止无决策价值图表或把不同币种/类型视觉合并为单一金额。

最小纯合成场景固定包含：Payment/Invoice 同日及日期边界、四币种、无/部分/全额分配、多对多 Link、活动与已终止 Link、无归属/活动归属/已终止归属、软删除 Fact/Trip、具体 Trip、空结果、多页稳定游标、筛选不匹配游标、非法查询组合、累计溢出、并发读快照、四角色、跨租户、Web 键盘与响应式。领域、PostgreSQL/应用、HTTP/OpenAPI、Web 单元与浏览器场景、关键不变量、两层覆盖率和完整工程门禁必须全部通过。

历史验收结果：2026-08-31 的 SQLite 实现通过。固定 Go 1.26.7 禁网容器内的全量测试、静态检查与构建全部通过；OpenAPI 客户端生成和 Web 完整检查通过，9 个 Vitest 文件共 38 项测试通过；M1/M3 状态矩阵与 M4 洞察浏览器组件场景 33 / 33 通过，其中 4 项新增洞察场景覆盖筛选、分组、分页、失败恢复、四角色、键盘及 768px/384px 回流；关键不变量 113 / 113（100%）通过；领域/应用层覆盖率 85.71%（3,101 / 3,618），基础设施/传输层覆盖率 75.24%（3,477 / 4,621）。10,000 个纯合成 Fact 的 SQLite 查询与领域投影基准为 70.58 ms/op（20 次固定运行）。ADR-0020 已替代当前存储实现；上述指标必须在 PostgreSQL 重新生成，历史摘要不得作为当前发布证据。

## 八、租户与安全验收

### 租户

- 所有业务表、文件、任务、ProviderConfig、AiRun、Claim、Review 和 Audit 具有 `tenant_id`。
- 数据访问接口必须显式接收租户上下文，不能从隐式全局状态推断。
- 每个读写、下载、预览、重试、取消和确认入口都有跨租户拒绝测试。
- 资源是否存在的差异不能向其他租户泄露。
- `owner`、`finance`、`reviewer`、`viewer` 的每个允许与拒绝单元格都必须有权限测试；reviewer 可处理当前审核资料和候选摘要，但不能列出 Payment/Invoice/Trip 或 Reimbursement，viewer 则相反。Trip 归属和 Reimbursement 写入只允许 owner/finance。
- 停用或降级最后一个 active owner 必须失败；suspended Membership 不能产生 TenantContext。
- 空库 `bootstrap-owner` 只成功一次并原子创建 User/Tenant/active owner；密码不得出现在 argv、环境、日志或数据库明文字段。非空库执行、HTTP 访问和事务故障注入均必须失败且不留下半成品。

### 密钥与隐私

- API Key 使用认证加密保存；主密钥来自数据库之外的环境或挂载文件。
- 数据库、日志、错误响应、追踪和前端存储中均不得出现明文 API Key。
- 默认日志不记录完整原始文档、完整模型输入/输出或字段证据正文。
- 上传文件只能通过鉴权接口读取，文件路径不能由不可信输入直接拼接。
- 文档内容不能覆盖系统指令、启用工具或请求外部副作用。
- 生产镜像不得包含测试数据、评估数据、开发命令、源代码密钥或本地数据库。

安全验收要求自动化测试、生产镜像清单和人工 diff 审查三类证据同时存在。

## 九、性能与容量（已批准）

参考环境：Linux x86_64、应用 2 vCPU / 3.5 GiB、PostgreSQL 2 vCPU / 2 GiB、20 GiB 可用磁盘、Docker Compose v2；外部模型延迟单独记录。

| 场景                     | 批准门槛                                          |
| ------------------------ | ------------------------------------------------- |
| 非 AI JSON API           | 10,000 条 Fact 数据下 p95 <= 300 ms               |
| 创建 Document/入队       | 不含文件上传传输时间，p95 <= 500 ms               |
| 审核确认事务             | p95 <= 500 ms                                     |
| 前端生产构建             | 四个代表页面无运行时错误                          |
| Lighthouse Accessibility | 四个代表页面 >= 95                                |
| Lighthouse Performance   | 桌面基线四个代表页面 >= 85                        |
| 内存稳定性               | 连续处理 50 个合成 Job 后无持续增长趋势或孤立任务 |

模型请求的网络耗时不计入应用 API 的 300 ms 门槛，但计入 Job 的 150 秒总期限并单独报告 Provider 延迟分布。

### 性能测量协议

- 证据记录构建 SHA、Compose 配置、CPU/内存限制、数据库/对象存储位置、数据种子、请求脚本版本和 Provider 延迟；非 AI 性能门禁明确排除 Provider 调用，Provider 延迟由同一候选的内存门禁单独测量；除明确的 AI 项外，测试使用本机回环网络。
- 10,000 条 Fact 固定为 5,000 Payment 与 5,000 Invoice，并包含至少 1,000 条对应 Document/Claim/Review 链。非 AI JSON API 集合固定为收件箱列表、Document 详情、ClaimSet 详情、Payment 列表、Invoice 列表和 M4 的 Fact 洞察；每个端点预热 100 次，再以并发 10 测量至少 1,000 次，逐端点 p95 均不得超过 300 ms。
- Document 创建使用预载入内存的 1 MiB 固定合成 PNG。服务端计时从请求体完整接收并通过字节数检查后开始，到 Document 与 ProcessingJob 事务提交结束；预热 20 次，再以并发 2 测量 200 次。
- 审核确认使用 200 个彼此独立、已就绪的纯合成 ClaimSet；预热 20 个后，以并发 2 测量 200 个首次确认事务。幂等重放另行验证，不混入首提交流量。
- Lighthouse 使用生产构建、全新无扩展浏览器配置和固定桌面预设，对四页各独立运行 3 次；Accessibility 与 Performance 均以三次最低分判定。
- 内存测试先处理 10 个预热 Job，再连续处理 50 个测量 Job；每个 Job 终态并空闲 2 秒后采样 API 进程 RSS。最后 10 次采样中位数不得比最初 10 次高 20% 以上，50 次线性回归斜率不得高于 0.5 MiB/Job，且任务表中不得存在无有效租约的 `processing` 或 `cancel_requested` 记录。

## 十、可靠性与恢复（已批准）

本节的任务持久化、非法状态和故障幂等属于 M1 门禁；1,000 个 Document、30 分钟恢复目标和完整清单对账属于 M4 正式发布门禁。M1 只需用一组最小纯合成数据证明备份说明可执行，不得把 M4 演练结果伪装为已完成。

- Processing Job 使用持久化状态和租约，不依赖仅存在于内存的队列。
- 状态转换必须满足文档定义，非法转换返回明确错误。
- 进程在上传、模型调用、Claim 保存和 Fact 提交任一点退出后，重新启动不得生成重复 Fact。
- M1 必须提供数据库、对象文件和密钥材料的一致备份说明，并以至少 1 个已确认和 1 个处理中纯合成 Document 完成可登录、可查询、可下载和可恢复任务的冒烟恢复。
- M4 在参考环境中用至少 1,000 个合成 Document 完成完整备份与恢复演练，30 分钟内恢复到可登录、可查询、可下载和可继续处理任务的状态。
- M1 冒烟和 M4 演练恢复后的数据库数量、文件哈希和审计链均必须与各自备份清单一致。

本节只覆盖新系统自身，不读取或恢复旧版本数据。

### 历史 SQLite 完整恢复切片口径

- 恢复集合由经认证的数据包与独立托管的既有主密钥组成；主密钥不得进入数据包、清单、CLI 输出或仓库证据。清单固定为 `smart-bill-manager-backup/2`，使用主密钥域分离派生的 HMAC-SHA-256 认证并包含随机 `backup_set_id`；backup、verify、restore、API 与数据库受保护结果必须属于同一集合，不兼容 M1 清单。
- 应用、Owner 初始化与备份共享运行锁。数据包创建必须在应用锁可独占、WAL checkpoint 完成和排他 SQLite 事务内进行；`staging/`、`trash/` 必须为空。
- SQLite 必须通过完整 `integrity_check`、空 `foreign_key_check`、当前迁移集合与 Schema 身份核对；数据库表数量、审计链和 SQLite 文件记录进入清单。
- 对象清单必须精确等于数据库引用的唯一物理对象集合。引用来源覆盖 Document、DocumentPage、EmailMessage 原文和非空 EmailAttachment；共享 key 去重但引用行数单独记录，同 key 的哈希和已知大小必须一致。缺失、多余、错哈希、错大小或不安全路径全部失败。
- 恢复只写不存在且与备份/迁移/运行锁/激活状态互不重叠的目标。数据库、对象根和主密钥在各自目标文件系统内 staging 并离线复核；发布前持久化 owner-only `restore-state=incomplete`。只有全部发布、同步和后检查成功后，才能用已单独同步的 complete 文件原子替换；状态永久保留，incomplete、未知、损坏或孤立状态均阻止启动。
- 离线恢复先证明原始快照与清单完全一致，再删除全部 Session。会话失效后除 `sessions = 0` 外的表数量、Schema、审计链和对象集合仍须相等；旧 Cookie 必须失败，新登录必须成功。
- 固定数据集最终恰好为 1,000 个具有实际纯合成原件的 Document：997 个普通上传与 1 个邮件附件 Document/Job 明确失败于 `provider_config_missing`、1 个已确认 Payment Fact、1 个备份时带有效租约和唯一旧 `running` AiRun 的 Processing Job；唯一旧 `succeeded` AiRun 必须归属已确认 Document。固定包含 2 个 DocumentPage、1,004 条对象引用与 1,003 个唯一物理对象。控制器必须以同一 exercise/model/mode/instance 的回环 health 精确证明 0→1 次提取，不能只凭 Job=`processing` 推断 AiRun 已落库。
- 先创建数据包，再在首次独立 verify 前启动不可重置的 30 分钟时钟。restore 必须把仍为 `processing` 的租约推迟固定 120 秒且不修改 attempt/version/AiRun。恢复启动后先验证 ready、旧 Cookie 拒绝、新登录和原快照 Fact/Document/五个上传与邮件对象可查询下载；全部读取完成后必须以目标 Job 仍为 `processing` 且 attempt 未变化形成线性化屏障，随后才允许租约接管、旧 AiRun 全库唯一转为 `failed/lease_expired`、attempt 增加一次、version 按唯一正常链增长、继续到 `needs_review` 与最终确认。
- 备份前必须冻结全部既有非 Session 行的稳定摘要；恢复后除目标 Job/Document/旧 AiRun 的明确列外摘要完全相等。目标新 AiRun 必须与旧请求保持同一 Provider 版本/指纹、模型、Prompt、Schema/Mapper/输入版本和请求哈希。最终只允许新增一个与该新 AiRun 闭合的 Claim→Review→Payment 链及其 Evidence、Validation、Origin、可选重复候选/决定和一个确认审计事件；其他变化失败。
- 演练 RPO 固定为 0；备份完成到恢复验证之间不得写入或删除业务数据。非零 RPO、快照后租户删除/凭据撤销重放和真实灾难切换属于生产发布门禁，不得由本地演练虚构。
- 证据 JSON 只允许安全聚合、清单摘要、相等性布尔值、状态数量和耗时；不得包含数据包、数据库、对象、独立主密钥、Cookie、密码、Provider 密钥、真实财务字段、原始响应或日志。

历史验收状态：2026-08-31 按 ADR-0018 的 SQLite 实现通过。完整演练使用经单独授权、仅存在于 `/tmp` 隔离目录的一次性本地主密钥、Owner 密码与 synthetic Provider key；固定数据集精确包含 1,000 个 Document、1,000 个 Job、2 个 DocumentPage、1,004 条对象引用与 1,003 个唯一物理对象。认证备份、独立验证和恢复分别用时 738、317 和 1,356 ms，恢复前 3 个 Session 全部失效；从不可重置时钟到任务继续、最终确认和数据库复核共 115,291 ms，低于 30 分钟门槛。ADR-0020 已替代数据库载荷和恢复实现，该结果不能代替当前 PostgreSQL 恢复验收。

### PostgreSQL 当前恢复口径

- 保持停写、RPO 0、30 分钟 RTO、认证清单、独立主密钥、精确对象集合、会话失效、稳定行摘要和目标任务只续跑一次等业务边界；
- 数据库载荷只接受固定 PostgreSQL 17 `pg_dump` 生成的自包含 dump；禁止复制数据目录、WAL 或 Docker volume，`pg_dump`/`pg_restore` 与服务端 major 必须一致并进入安全聚合身份；
- 清单覆盖 dump 哈希、迁移身份、PostgreSQL Schema/约束身份、表数量、审计链和对象集合；数据库密码、DSN、路径和原始工具输出不得进入证据；
- 恢复只写全新数据库和全新对象目标，完成 Schema、约束、租户、对象和审计复核后删除全部 Session；未知迁移、额外 Schema 对象、缺失约束或恢复到非空目标全部失败；
- 用与历史演练等价的恰好 1,000 Document 纯合成数据集重新执行完整控制器；历史 SQLite 数据包、摘要或耗时不能复用为通过。

## M4 第三切片：PostgreSQL 唯一持久化

本切片以 ADR-0020 为冻结决策，必须在 ADR-0019 最终发布门禁之前通过。

- `go.mod` 只保留一个固定 PostgreSQL 驱动，不增加 ORM；源码只保留 PostgreSQL 适配器和当前迁移，不存在 SQLite 驱动、产品适配器、运行配置、发布卷、备份入口或第二测试数据库；
- PostgreSQL Schema 从 Clean Slate `0001` 创建空库，不读取或迁移 SQLite 数据。复合租户外键、CHECK、局部唯一索引、不可变触发器、金额安全范围和迁移内容身份必须与领域不变量一致；
- 迁移由显式入口执行，API 运行角色无 DDL 权限。密码只从 owner-only 文件读取，不能出现在 argv、环境、日志、Compose 展开结果或测试证据；数据库只在 internal 网络可达且不发布宿主端口；
- 多查询读取必须使用同一 `REPEATABLE READ READ ONLY` 快照；关键写入使用显式行锁和适用的 `SERIALIZABLE` 隔离；序列化失败、死锁、陈旧版本和唯一冲突映射为稳定错误且零部分写入；
- Worker 竞争必须用 PostgreSQL 行锁安全领取，同一 Job 只能有一个有效租约；多 API/Worker 并发测试不得依赖进程内锁或 SQLite 全库锁；
- 重复检测查询必须命中租户/band 索引，洞察必须在数据库内精确整数聚合并使用 keyset 分页；10,000 数据集不得退回为全量载入 Go 内存；
- 所有领域、适配器、应用、HTTP/OpenAPI、关键不变量、覆盖率、性能、内存、1,000 Document 恢复和浏览器测试必须只对受限临时 PostgreSQL 17 执行并通过；测试完成立即销毁临时凭据、容器、网络、卷和原始报告。

验收结果：2026-09-01 通过。PostgreSQL 17 已成为唯一关系数据源；Clean Slate `0001`、唯一 pgx 适配器、显式迁移、最小权限、事务/Worker、Compose 与认证恢复均已实现。受影响的领域、数据库、应用、HTTP/OpenAPI、Web、关键不变量、覆盖率、10,000 数据集、内存和浏览器门禁全部只在受限临时 PostgreSQL 17 上重新通过；SQLite 当前实现与第二测试数据源均已移除。

## M4 第五切片：v0.3.1 自托管公开实测分发

本切片以 ADR-0021 为冻结决策，不改变业务领域、数据库 Schema、API、Web 或 AI 契约。

- GHCR 只发布通过 M4 门禁的 `linux/amd64` 候选，使用明确版本 Tag 且不写入 `latest`；远端回读必须得到不可变 manifest digest，并与部署配置一致。`v0.3.0` Git Tag 不重写，部署修复发布为 `v0.3.1`。
- 发布 overlay 只能改变应用镜像来源、拉取策略和 Owner 密码 secret；规范化 Compose 必须保持 PostgreSQL 无宿主端口、应用回环默认、internal 数据库网络、只读根、最小 capability、资源/PID 上限和独立卷。
- 凭据准备器必须拒绝相对路径、既有目标和非法参数；成功时创建一个 `0700` 目录及五个彼此独立的 `0600` secret 文件，不向 stdout/stderr 输出任何 secret 值。
- 部署命令不得把 secret 写入 argv 或普通环境变量；`down` 不删除卷，工具不提供隐式数据销毁、旧数据读取或迁移路径。
- 隔离冒烟必须从空目录和空卷完成数据库健康、角色 provision、Clean Slate `0001` migration、唯一 Owner bootstrap、API ready、登录、当前会话、退出和旧会话 `401`；完成后销毁仅属于本轮的临时资源。
- README 的首屏必须给出版本状态、架构支持和可执行快速开始；详细里程碑、ADR 与证据通过文档索引访问，不继续占用用户主路径。
- 提交前必须通过脚本测试、Compose 规范化检查、文档链接/命令审查、`git diff --check`、敏感信息、大文件、临时产物、进程和 Docker 残留检查。

验收结果：2026-09-01 通过。公开 GHCR manifest 与 M4 候选镜像 ID 一致，空 Docker 配置可匿名按 digest 拉取；发布 Compose 10 项静态边界通过，五份 secret 独立且权限正确，空库 provision、migration、Owner bootstrap、ready、登录、双 Cookie 会话、退出和旧会话失效全部通过。15 个工具测试和 30 个 README/部署文档本地链接通过；临时资源已销毁。

## M4 第六切片：引导式安装与自定义持久化目录

本切片以 ADR-0023 为冻结决策，不改变业务镜像、数据库 Schema、API、Web、AI 契约或唯一 Compose 运行边界。

- 安装器必须允许 PostgreSQL 数据、对象文件和备份分别使用用户选择的全新绝对目录；省略时使用运行目录下的标准布局。生成的 `deployment.env` 只记录路径和非秘密配置。
- 准备器必须拒绝相对路径、重复路径、既有目标、Git 仓库内目标、非法端口和非法参数；不得覆盖、删除或接管用户已有目录。
- 安装器必须复用唯一部署 wrapper，并严格按 `pull -> bootstrap -> start -> status` 执行；bootstrap 前必须暂停提示用户保存一次性 Owner 密码，且不得打印密码内容。
- 流式安装必须要求显式 `vMAJOR.MINOR.PATCH`，只从该固定 GitHub Release 下载同版本 Bundle 与 sidecar，在解包前执行 SHA-256；下载、摘要或 Bundle 入口错误必须失败，成功或失败均只回收本轮 owner-only 临时目录。
- PostgreSQL 与应用必须保持独立镜像和容器；数据库不发布宿主端口，internal 网络、固定 digest、文件型 secret、最小权限角色、只读根和资源上限均不得弱化。
- Release 部署包 allowlist 只新增安装器，不包含源码、凭据、运行数据或新依赖；脚本语法、参数边界、默认/自定义路径、调用顺序、Bundle 确定性、Compose 规范化、文档链接、敏感信息和 `git diff --check` 必须通过。
- 本切片不得发布、推送、创建 Tag、修改 `v0.3.2` 或调用真实外部服务。
- README 必须同时包含可复制的一条命令、显式 Compose 命令和 `docker run -d` 风格；Docker CLI 示例必须明确依赖已经初始化的独立 PostgreSQL，不能声称是完整单容器部署。

验收结果：2026-09-01 通过。16 个工具测试文件、固定版本流式成功/摘要失败路径、临时目录回收、默认与三目录自定义映射、`7476` 端口、安装生命周期顺序、Release Compose 规范化、13 文件确定性 Bundle 与 SHA-256、README 三种部署形式、32 个当前本地文档链接、敏感信息和 diff 检查全部通过；未启动容器，临时文件已清理。

## M4 第四切片：运行质量与本地发布准备

本切片以 `docs/decisions/0019-local-release-candidate-and-runtime-quality.md` 为冻结决策，不新增业务 API、页面、迁移或第二数据源。验收对象是当前 Clean Slate 构建时基线 HEAD 与确定性发布输入摘要共同标识的本地发布候选，而不是旧 M1 镜像或历史证据；最终证据提交后必须复核发布输入摘要未变化。

- 唯一发布资产固定为 `infra/docker/app.Dockerfile`、`infra/docker/entrypoint.sh`、`infra/compose/compose.yaml` 与 `infra/compose/.env.example`；仓库不得保留 `m1` 发布入口、镜像名、Compose project 名、entrypoint 或运行质量报告身份的兼容别名。
- 发布产物准备器必须先在 owner-only `/tmp` 隔离区占用输出，再以固定 Node 24.19.0 独立执行 `npm ci --offline` 和 Web 生产构建，并以固定 Go 1.26.7 禁网容器、只读完整模块缓存、`GOPROXY=off`、`go mod verify` 构建四个二进制。Dockerfile 只接受本地 `release_artifacts` 上下文，并在复制前核对基线 HEAD、发布输入摘要、工具链版本、精确清单与全部 SHA-256；URL、Git、镜像型上下文、缓存缺失和额外文件均失败。
- 运行层必须只由固定 Alpine 3.23、其既有 CA/BusyBox、固定 Poppler 26.05.0 bundle、从固定 Go 1.26.7 Debian 镜像选择性复制的五个 glibc 2.41 文件、静态 `run-as-sbm`、UID/GID 10001 与数据目录组成。Poppler manifest、来源 SHA、逐文件哈希和真实 `pdfinfo/pdftoppm -v` 必须通过；不得复制 Go 工具链、Debian 其余文件、包管理器或未列出资产。
- 镜像必须包含当前 `server`、`bootstrap-owner`、`backup`、`run-as-sbm`、Web dist、迁移和 Provider-facing Schema；不得包含 `recovery-exercise`、`seed-performance`、测试、工具、评估/证据、文档、旧源码、Go/Node 工具链、数据库、对象、日志或任何凭据。镜像标签必须同时绑定构建时基线 HEAD 与确定性的发布输入摘要；最终证据提交后复核该输入摘要不变，不能虚构自引用的最终提交 SHA。
- Compose 默认绑定 `127.0.0.1`；app 与 PostgreSQL 根文件系统均只读，关系数据/对象只写卷和受限 tmpfs 明确；`cap_drop=ALL`、最小增补 capability、`no-new-privileges`、PID/2 CPU/3.5 GiB 应用上限、2 GiB 数据库上限、停止窗口和健康检查均由规范化配置与真实容器验证；加上 256 MiB Provider 后同时运行总配置为 5.75 GiB。
- entrypoint 只接收独立只读主密钥源，拒绝缺失、空、过大、非法格式、符号链接和多硬链接；材料化结果为 owner-only 单硬链接，应用最终以 UID/GID 10001 运行。本地 Compose 文件型 secret 不支持 `uid/gid/mode` 重映射，Owner 密码因此只能在 `/app/bootstrap-owner` 一次性容器中由 root entrypoint 校验并材料化为 UID/GID 10001、`0600` 的 tmpfs 文件；正常 server 容器不得存在该副本。主密钥、Owner 密码与 Provider key 不得进入 argv、环境、镜像历史、Compose 展开证据、日志或仓库。
- 静态 `run-as-sbm` 必须先清空补充组，再按 GID、UID 顺序降到 10001，并用原始 argv/env 直接 `exec`；缺少命令、非 root、任一 syscall 失败或降权后身份不符均 fail-closed，不保留 root 代理进程。
- acceptance Provider 只在应用网络命名空间内监听回环，固定 UUIDv4 exercise、`synthetic-*` 模型与受保护 key；纯合成 E2E 不访问外网。宿主浏览器只接受回环 HTTP origin，关闭后台网络服务，并把回环白名单之外的 HTTP(S) 请求导向本地不可用代理；Node 客户端拒绝跨源重定向。空库必须经 CLI 原子创建唯一 Owner，再完成 ready、登录、上传→Claim→Review→Fact、对象鉴权和当前全部 Web 场景。
- 性能协议保持第九节批准口径：六个非 AI JSON API 各预热 100 次、并发 10 测量 1,000 次且 p95 <= 300 ms；Document 创建预热 20/测量 200、并发 2且 p95 <= 500 ms；审核确认预热 20/首次测量 200、并发 2且 p95 <= 500 ms。
- 内存协议保持 10 个预热和 50 个测量 Job、终态后空闲 2 秒；RSS 采样 PID 必须是命令为 `/app/server` 且 UID/GID 均为 10001 的实际发布进程；同一 synthetic Provider 不向宿主发布端口，门禁只能通过已核验 app 容器的内部回环读取其聚合，并必须精确记录 1 次 capability probe 和 60 次 extraction，以及两类请求的样本数、p50、p95 与 max。最后/最初 10 次 RSS 中位数比 <= 1.2、斜率 <= 0.5 MiB/Job，且无孤立 `processing` 或 `cancel_requested` Job。
- 登录、收件箱、审核和 Payment 列表四页各执行三次 Lighthouse，最低 Accessibility >= 95、最低 Performance >= 85；768/1024/1440/1920 四宽度共 16 个页面组合和对应 16 个等效 200% 回流组合全部无水平溢出、无越界可聚焦元素、无缺失标签并满足 reduced-motion；键盘和深色主题单独通过。
- 当前 6 个 Playwright spec 的完整场景均必须执行且至少保持 33 项通过；不得用删除场景、只跑组件矩阵或复用历史 M1 结果替代当前镜像 E2E。
- 性能、内存、Lighthouse、响应式、镜像与最终证据输出必须在首次写入或 fixture 创建前原子占用 owner-only 路径；未知/重复参数、非回环 URL、符号链接、多硬链接、宽权限父目录、仓库内输出和路径冲突均失败。终端只输出安全聚合或固定分类码。
- 最终安全聚合必须绑定构建时基线 HEAD、发布输入摘要、镜像 ID 和 Compose 摘要，只包含批准阈值、样本数、p95、内存比/斜率、最低页面分数、场景数量和布尔门禁；不得提交原始报告、路径、容器/进程/业务 ID、对象或输入明细、Cookie、凭据、真实字段或日志。
- 最终安全聚合只声明最终候选禁网构建与正式验收网络隔离，不得把整个开发过程笼统记为“从未使用外网”；任何已发生并已披露的工具网络策略偏差必须以安全布尔字段保留，且不能冒充真实 Provider、邮箱或生产联调。

验收结果：2026-09-01 通过。最终 PostgreSQL 候选的镜像/Compose、entrypoint、首次 Owner 初始化、认证和最小权限门禁通过；六个非 AI JSON API、Document 创建和审核确认 p95 全部达标，50 Job 内存趋势达标且无孤立任务；Playwright 37 / 37、Lighthouse 四页三次最低 Performance/Accessibility 均为 100、响应式与等效 200% 回流各 16 / 16，键盘和深色主题通过。全量测试、140 / 140 关键不变量及两层覆盖率同时通过。当前只完成本地发布准备，真实模型正确率、真实外部联调和生产部署/发布仍未执行。

## 十一、UI/UX 与可访问性

- 已冻结 `docs/ui-ux.md` 中的 02「国内大厂中后台」，M1 不得并行实现其他页面骨架。
- 登录、AI 收件箱、审核工作台和账单列表必须与批准基线一致。
- 支持宽度 768、1,024、1,440 和 1,920 px；小于 768 px 不作为 M1 正式操作端，但不得出现不可恢复崩溃。
- 核心流程满足 WCAG 2.2 AA：键盘可达、可见焦点、文本对比度、表单标签、错误关联和减少动画偏好。
- 状态按页面业务适用性覆盖：登录包含默认、凭据错误、提交中和权限不足；AI 收件箱包含混合队列、处理中、部分结果、失败、取消、空和离线；审核工作台包含待审核、阻断、版本冲突、加载和完成；账单列表包含有数据、加载、空和权限不足。
- 金额使用等宽数字，主次操作明确；不得用颜色作为唯一状态信号。
- 不允许默认组件库后台质感、全屏卡片堆叠、AI 发光渐变或无决策价值的统计图。
- M0 对视觉基线逐页验证浅色与深色语义，并在四个指定产品 CSS 视口记录无横向溢出；预览宿主有外层边距或 iframe 时，以产品 frame 的 `innerWidth` 为准并同时记录根节点宽度。
- 200% 验证优先使用浏览器页面缩放；自动化表面不暴露缩放控制时，可在相同 DPR 下把产品 CSS 内容宽度减半作为等效 reflow 验证，并明确记录原宽、等效宽和替代原因。核心操作必须仍可见、可聚焦和可执行。
- M0 的 WCAG 证据验证批准的交互基线；Lighthouse 分数属于 M1 真实页面实现门禁，不能用设计稿结果替代。

## 十二、工程质量

批准质量门槛：

- 领域和应用层语句覆盖率 >= 85%，关键不变量分支覆盖率 100%。
- 基础设施层整体语句覆盖率 >= 70%，外部接口失败使用契约或集成测试覆盖。
- 前端领域状态、审核动作和错误恢复具有单元测试；四个代表流程具有浏览器 E2E。
- Lint、格式、类型检查和静态分析零错误、零新增警告。
- API Schema、数据库约束和前端客户端来自唯一契约或具有自动一致性校验。
- 生产构建必须执行真实健康请求和一条完整合成 E2E，不以容器启动代替验收。

覆盖率不能替代关键场景测试；删除测试或降低门槛必须作为单独决策审查。

覆盖率包边界固定如下：领域层为 `apps/api/internal/domain/...`，应用层为 `apps/api/internal/application/...`，基础设施层为 `apps/api/internal/adapters/...` 与 `apps/api/internal/transport/...`；生成代码、迁移文件、装配入口和纯数据声明不计入分母，排除清单必须由覆盖工具配置显式列出。关键不变量分支固定为：租户隔离、Claim 完整 revision 快照与 actor 检查、Validation 阻断、Review 才能创建 Fact、重复确认幂等、Document SHA-256 与规范化发票号精确判重、关联候选显式分配决定、同币种正数分配、双方余额不超额、同一活动对唯一、并发争用无部分写入、币种 exponent 与金额整数运算、文件签名/MIME 一致、Job 租约恢复、只允许无 ClaimSet 的失败 Job 重试、取消后禁止写入、重复模型字段路径以单一候选和 blocked Validation 持久化、Provider Schema 投影不削弱本地权威校验、Provider 原始响应先满足当前传输契约，以及能力检测 Schema 身份过期时禁止激活或调用模型。上述分支逐项 100%，不能被包级平均值掩盖。

M2 第二切片新增关键分支为：视觉指纹确定性及批准的重新编码/等比缩放、宽高比与双哈希边界、同租户 band 检索、整份近似优先、同文档页对、Payment/Invoice 字段组合、精确发票号优先、50 项上限、完整且规范的 `keep_distinct` 计划、目标删除陈旧回滚、并发确认串行化、同一确认决定数据库约束、重复决定不可变和 Reject 无 Fact。它们同样逐项 100%，并进入 `tests/critical-invariants.tsv` 唯一映射。

M2 第三切片新增关键分支为：同一稳定明细的相邻跨页 Evidence、完整 20 页和空白页投影、页码跳跃、阅读顺序倒退、重复或缺口 `sort_order`、revision 后从当前 Evidence 重算，以及规范化页的权限、非法页与 reviewer 终态隐藏边界。它们同样逐项 100%，并进入 `tests/critical-invariants.tsv` 唯一映射；跨租户页面读取继续由既有 HTTP 租户隔离分支覆盖。

M2 第四切片不增加服务端批量实现；新增关键分支为：独立单 Document 命令在中间拒绝后仍能继续、成功项各自只创建一份 Document/Job且失败项零残留，以及客户端严格串行、原顺序、精确重复、超 20 项、单项超 20 MiB、网络失败继续和逐项安全反馈。后端独立性分支进入 `tests/critical-invariants.tsv` 唯一映射；客户端编排分支由单元测试与浏览器场景覆盖。

M2 第五切片新增关键分支为：anchor-scoped 活动计划 hash、完整期望计划与请求 hash、补充/撤销/替换差异、零金额 anchor 与双方余额、陈旧和无变化零写入、严格幂等重放、不可变 Adjustment/Link 来源、安全审计 metadata、四角色权限、跨租户/删除/币种/日期边界、最后余额与 Fact 删除竞态、200 项显式上限，以及 HTTP 严格 JSON/CSRF/路径边界。它们进入 `tests/critical-invariants.tsv`，最终 72 / 72（100%）通过；Web 的完整计划、加载/空目标、撤销全部、冲突保留草稿、权限、键盘与响应式边界由单元测试和浏览器矩阵覆盖。

M3 第二切片新增关键分支为：Trip 根区段互斥、最小字段与日期倒置、Trip 仍经人工 Review 创建、Trip 确认不接受金额关联计划、完整重复决定、Trip 字段来源；`trip-attribution/1` 日期/邻近/活动 PaymentInvoiceLink 原因；单 Fact 活动归属唯一、assign/move/unassign 期望快照、严格幂等、同 Fact 并发、不可变 Decision/Link、Fact 删除终止、安全审计、三种游标视图、四角色权限与跨租户边界。它们已逐项进入 `tests/critical-invariants.tsv`，最终 92 / 92（100%）通过；Web 的严格可空请求、冲突草稿保留、角色、键盘和响应式边界由单元测试与浏览器矩阵覆盖。

M3 第三切片新增关键分支为：Assignment 选择归属/活动/上限，三类 `reimbursement-policy/1` Finding、稳定 key/hash、完整 Finding 确认、混合币种分组、提交重算、同 Trip submitted 唯一、创建/状态严格幂等、状态图/版本并发、不可变 Item/Finding/Decision、软删除后历史、安全审计、列表/详情游标、四角色权限与跨租户边界。它们已逐项进入 `tests/critical-invariants.tsv`，最终 104 / 104（100%）通过；Web 的选择、提示确认、冲突草稿保留、只读、键盘和响应式边界由单元测试与浏览器矩阵覆盖。

## 十三、M1 最小场景矩阵

至少包含：

1. 正常支付截图；
2. 正常单项目发票；
3. 多项目发票；
4. 缺少金额；
5. 多个候选总金额冲突；
6. 非法日期或发票号；
7. 模糊、裁切或缺页；
8. 非支持文件和伪造 MIME；
9. Provider 401、429、5xx、超时和非法 JSON；
10. 用户取消、重试、修订、拒绝和确认；
11. 重复上传和重复确认；
12. 跨租户读取、下载、取消和确认；
13. 处理中重启；
14. Fact 提交前后故障注入；
15. 生产 Compose 从空数据库完成真实首条链路。
16. M2 多候选金额分配、余额展示、并发超分配拒绝和删除后余额恢复。
17. M2 多 Document 顺序上传、逐项成功/重复/失败反馈和中间失败隔离。

## 十四、批准记录

### 视觉方向

- 批准角色：产品负责人
- 批准日期：2026-08-27
- 批准选择：02「国内大厂中后台」
- 四页基线：登录、AI 收件箱、审核工作台、账单列表
- 四页批准日期：2026-08-27
- 基线版本：`M0-D02-2026-08-27`
- 稳定资产：`docs/design/m0-d02-four-page-baseline.html`
- 资产 SHA-256：`ca5d8fa9c8341e29d5a55c503dea9d74f226ef38af7a6ae9f16d3f52efacd464`
- 被替代方案：01「国际精致型 B2B」及早期 A/B/C 配色稿
- 批准边界：导航模型、页面骨架、信息密度、状态表达和视觉 Token；不包含量化指标、WCAG 结果或性能结果

### 量化指标

- 批准角色：产品负责人
- 批准日期：2026-08-27
- 批准版本：`M0-ACCEPTANCE-2026-08-27`
- 批准范围：本文所有标为“已批准”或“批准门槛”的数值；测量协议用于使这些数值可重复执行，不降低门槛
- 例外：无

批准数值自记录日期起是对应里程碑的强制门禁。任何修改、降低或例外都必须追加新的产品负责人批准记录。

### 模型质量门禁阶段调整

- 批准角色：产品负责人
- 批准日期：2026-08-30
- 批准版本：`M1-MODEL-GATE-2026-08-30`
- 批准范围：清晰、完整、无遮挡且关键字段可直接人工辨读的当前受支持输入；继续其余 M1 开发
- 决策：不修改 2026-08-27 批准的模型准确率数值，将至少 100 份、三次取最差及准确率门槛调整为 M4 正式发布门禁；当前 M1 只把这些数值作为观测指标
- M1 当前证据：v5 的 16 / 16 任务均进入 Claim 或明确 Provider 失败，15 / 15 本地接受根 Schema 合法，AI 直接 Fact 为 0；完整软件、安全、人工审核和生产构建门禁见 `docs/m1-evidence.md`
- 不适用例外：Source/Claim/Fact、人工审核、Schema/Validation、租户隔离、幂等、证据、失败暴露、隐私、凭据与生产镜像门禁不变
- 已知限制：v5 未达到原准确率目标且标签契约存在缺陷；该结果只作为诊断和安全闭环证据，不得改写成模型质量通过
- 阶段边界：本次调整本身不标记 M1 完成，也不授权启动 M2；M1 后续经独立收口复核完成，仍未授权 M2

### M2 首切片授权

- 批准角色：产品负责人
- 批准日期：2026-08-30
- 批准版本：`M2-ALLOCATION-SLICE-2026-08-30`
- 批准范围：先冻结并实施支付—发票金额分配的领域、数据库、API、审核 UI 与自动化验收；其余 M2 能力保持未启动
- 正确率边界：模型正确率专项继续保留到全部功能开发完成后的 M4，不得阻塞本切片，也不得改写历史失败
- 工程边界：不提交、不推送、不部署、不引入本地 OCR，不保留 M1 一对一活动兼容分支
- 验收结果：首切片于 2026-08-30 通过；在当时 M2 整体仍为进行中。后续四个切片继续执行“文档冻结后再实施”门禁，并于 2026-08-31 全部通过后收口 M2

### M2 至 M4 持续本地开发授权

- 批准角色：产品负责人
- 批准日期：2026-08-30
- 批准版本：`LOCAL-GOAL-M2-M4-2026-08-30`
- 批准范围：按权威文档逐切片完成 M2、M3 和 M4 本地功能，每个切片先冻结文档、通过完整验收并建立独立本地提交
- 停止门禁：首次正式真实模型正确率评测、真实邮箱或 Provider/外部账号联调、凭据创建或变更、下载新依赖/大型镜像/OCR/模型文件、付费操作、远端资源、部署与生产发布
- Git 边界：允许已验收切片创建本地提交；不得推送、创建远端分支、Tag、Release、跳过 Hook或重写历史
