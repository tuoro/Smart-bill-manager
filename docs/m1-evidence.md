# M1 验收证据

状态：通过；M1 已完成，模型质量作为 M4 正式发布门禁保留，M2 未启动

更新日期：2026-08-30

## 当前边界

唯一活动链路为：

`上传 -> bill-visible-text-cn/1 -> bill-visible-text/1 -> claim-mapper/3 -> document-claim/2 -> 字段级校验 -> 人工审核 -> Fact`

- 模型只返回固定 Payment/Invoice 字段的票面 `{text,page}` 或 `null`，不输出内部 Claim、归一化值、独立 Evidence、置信度、问题列表或计算值。
- `bill-visible-text/1` 硬校验有效 JSON、封闭根对象、版本、文档类型和两个业务根成员；嵌套局部错误不能抹掉整份输出。
- Mapper 从同一 `text` 组装 Evidence，并确定性处理批准的币种、金额、日期、时间、时区和数量表示法；不纠正发票号/名称字符，不计算缺失业务值。
- 发票 `amount_with_tax` 唯一映射到 `total_minor`；`amount_without_tax` 仅进入审核专用 `supplementary_fields`，空白单价保持 absent。
- 非法字段形状、页码、金额、日期、时区、数量、Evidence 或业务区段进入 blocked Claim，同时保留其他正确字段。
- Claim、Review、Validation 与 Fact 安全边界不变；只有用户确认才能创建 Fact。
- 旧 Observation、重型 Claim Assembler、`bill-extraction/1` 包装、`bill-extraction/2` 自然标量与独立 Evidence 链均无活动兼容入口。

决策见 `docs/decisions/0008-minimal-visible-text-contract.md`，完整契约见 `docs/ai-pipeline.md`。

## 软件实现证据

当前实现包括：

- `contracts/schemas/bill-visible-text.schema.json`：最小封闭根身份唯一权威 Schema；
- `contracts/schemas/document-claim.schema.json`：包含审核专用 `supplementary` value type 的 Claim v2；
- `apps/api/internal/adapters/openaicompatible`：最小中文 Prompt、确定性根投影、显式输出模式、嵌套票面 JSON 保留和安全诊断；
- `apps/api/internal/application/claimmapping`：`{text,page}` 解码、Evidence 组装、无歧义金额、日期/时区/数量与明细顺序的确定性映射；
- `apps/api/internal/domain/validation.go`：字段问题、Evidence、金额、补充字段与业务不变量的确定性阻断；
- `apps/api/internal/application/reviews`：补充字段可审核/修订，但不进入 Fact 或 Fact origin；
- `infra/migrations/0001_initial.sql`：Clean Slate 数据库直接接受 `supplementary`，未增加迁移或兼容分支；
- `tools/synthetic-provider.mjs`、Runner 与评分器：Visible Text 冻结身份、数值等价和最小充分 Evidence 技术链；
- 当前 Runner 在读取密钥或联网前拒绝旧 `m1-real-dev-v4`；v5 首次发布与真实调用不属于本轮。
- `tools/diagnose-invoice-pipeline.mjs`：对明确授权的五份发票执行混合 Prompt、发票专用直出与同模型两阶段归因诊断；只输出字段路径级布尔结果和聚合指标，不持久化原始模型响应。

关键自动化断言包括：

- `json_object` 模式保留嵌套票面字段错误，额外根键按封闭根契约拒绝；
- strict `json_schema` 模式仍强制声明的根成员和确定性 Provider Schema 身份；
- 旧裸标量、非法页码、币种缺失、金额精度、日期/时区/数量和区段错误形成 blocked Claim，不导致 Mapper 整份失败；
- 常见点号/逗号小数与合规千分位无浮点地映射到内部整数；
- 发票只把含税合计映射为总额，不含税金额供审核；空白单价不计算；
- 发票明细分组由模型保留，Mapper 从数组顺序生成无伪造 Evidence 的 `sort_order`；
- `source_timezone` 可以使用批准的产品默认值而不伪造图片文字；
- 数量 `1` / `1.0`、整值外层括号和币种符号最小 Evidence 的等价规则有正反自检；
- 无效 JSON、错误根身份和 strict 传输失败仍显式失败并遵守单次 Schema 重试。

## 审计身份

新 AiRun 必须精确记录：

- `prompt_version = bill-visible-text-cn/1`；
- `extraction_schema_version = bill-visible-text/1`；
- `provider_schema_version = bill-visible-text-provider/1` 及其 SHA-256；
- `claim_schema_version = document-claim/2`；
- `claim_mapper_version = claim-mapper/3`；
- `input_processing_version = document-normalize/2`。

数据库、评测导出器、Runner、评分器和 HTTP 集成测试使用同一身份，不提供旧契约回退。

## 当前已执行验证

- 固定 Go 1.26 容器中的后端完整 `go test -p=1 -timeout 60s ./...`：通过；最终 `go vet ./...`：通过；
- Visible Text 重构后的覆盖率门禁：domain/application 为 86.57%（1,579 / 1,824，门槛 85%），infrastructure/transport 为 71.60%（1,843 / 2,574，门槛 70%）；关键不变量矩阵 38 / 38（100%）。首次覆盖率运行的 domain/application 为 83.10%，未计为通过；补齐本地金额、日期、时间、时区、币种推断、非法字段和坏明细边界后重跑通过；
- Web 完整检查：契约生成、TypeScript、ESLint、Prettier、Vitest 与生产构建均通过，3 个测试文件共 10 个测试；
- 模型评测 Runner、评分器、发布器与合成 Provider `node --check`：通过；
- release 评分器 self-test、真实 preflight self-test、纯合成调优集隔离/哈希检查：通过；
- 当前独立验收镜像 `smart-bill-manager:m1-visible-text-check` 为 `sha256:b1040533970f3a778dddba31a5884f5a355e94f9a9c8ff539ef48aa3c48b7b49`，大小 23,030,985 bytes；`/app` 只有 15 个生产文件，唯一提取契约为 `bill-visible-text.schema.json`，不含源码、测试、评测数据、旧活动契约或密钥；512 MiB / 1 CPU 临时容器中的 `/api/v1/ready` 真实冒烟通过；
- 更新后的 Worker、OpenAI-compatible Adapter 与 HTTP 集成测试均随完整 Go 测试通过；此前隔离 Compose 13 / 13 结果只保留为未变更上传、审核、Fact 和页面状态链路证据，不替代本轮 Visible Text 身份断言；
- 响应式与可访问性：4 个页面在 768、1,024、1,440、1,920 px 共 16 / 16 宽度检查通过，等效 200% reflow 16 / 16 通过；键盘、焦点、错误绑定、减少动态效果与暗色主题检查均通过；
- Lighthouse 完整重跑 12 / 12 通过；四页各三次运行的最差 Performance 为 100、Accessibility 为 100，门槛分别为 85 和 95；
- 固定 10,000 Facts 与 220 Claims 的性能门禁通过：四页服务端最慢 155.99 ms（门槛 300 ms），1 MiB 文档创建 19.62 ms、审核确认 44.04 ms（门槛 500 ms）；
- 50 个连续 AI 作业的有效服务进程内存检查通过：首 10 个 RSS 中位数 154,990,592 bytes，末 10 个 24,629,248 bytes，比例 0.1589（门槛 1.2），斜率 -2.1965 MiB/作业（上限 +0.5），无遗留 processing 或 cancel_requested 作业；
- 独立新环境备份、显式校验、恢复与租约恢复通过：92,570 ms 内恢复处理，2 个作业、2 个 Payment、2 份下载均验证通过，处理中作业 attempt 由 1 增至 2，并形成两个不同 Fact；恢复使用备份中恢复出的主密钥；
- 本地隔离 `m1-real-dev-v4` 已首次发布，16 份样本，清单 SHA-256 为 `cd96056be80b4670c7a315ddcdb37dc5f6a015367013be9bd7336a967157c610`；原图未修改，目录 `0700`、文件 `0600`，仍被 Git 与 Docker 排除；
- 镜像、恢复数据库、对象卷和应用日志中的本轮合成凭据明文扫描均为 0 命中；活动代码中不存在 v1 契约或兼容入口；
- 浏览器沙箱受限、误采样 `docker-init` PID 和超出租约窗口的首轮测试编排均被明确判为无效并重跑，只有完整且目标正确的运行计入上述结果；
- 真实 Provider 凭据只由受保护文件读取，未进入命令参数、运行结果、日志、仓库或回复；没有提交、推送或进入 M2。

## Qwen v2 真实预检结果

2026-08-30，产品负责人在明确获知下一步为 v4 的 16 份真实样本预检后授权继续。运行使用：

- Provider 主机：`ws-e0jyv25ziy7n7bg6.cn-beijing.maas.aliyuncs.com`；
- 模型：`qwen3-vl-30b-a3b-instruct`；
- 数据集：`m1-real-dev-v4`，16 份，清单 SHA-256 `cd96056be80b4670c7a315ddcdb37dc5f6a015367013be9bd7336a967157c610`；
- `json_object`、temperature 0、并发 2、60 秒模型超时、150 秒任务超时和单次 Schema 重试；
- Prompt、Extraction Schema、Provider Schema、Claim、Mapper 与输入处理版本分别为 `bill-extract/2`、`bill-extraction/2`、`bill-extraction-provider/2`、`document-claim/2`、`claim-mapper/2`、`document-normalize/2`。

首次能力检测因模型 ID 使用了不被该实例接受的大小写形式而在合成蓝色探针阶段被拒绝，真实样本发送数为 0。改用该 Provider 此前已验证并冻结的小写模型 ID 后能力检测通过，完整 16 份预检执行完成。

聚合结果：

- 9 / 16 形成本地 Claim，7 / 16 在模型阶段终止；已形成 Claim 的 9 / 9 均通过 v2 Schema，完整 Claim 契约为 0 / 16；
- 7 个模型阶段终止包括 5 个 Provider 超时、1 个无效 JSON 和 1 个 Provider 拒绝；
- 9 个 Claim 中 1 个 `ready_for_review`、8 个 `blocked`；Validation 以 13 个关键字段 Evidence 缺失为主，另有 4 个补充字段 Evidence 缺失、2 个 Evidence 过多、1 个非法金额和 1 个非法 typed value；
- 分类、金额、发票号、日期、名称、关键 Evidence 和清单断言分别为 56.25%、50%、33.33%、56.25%、31.82%、59.52% 和 62.26%；
- v4 没有缺失/冲突事件，召回分母为 0，当前不可评估，不能把评分器显示的 0%解释为实际召回能力；
- AI 直接 Fact 为 0，人工审核和 Fact 安全边界未被绕过。

该结果证明 v2 已移除旧 `{value, source}` 整份包装的系统性结构阻塞，但当前模型完成率、语义精度和 Evidence 完整性仍不接近最终门槛。不得启动 100 样本发布评测。原始运行和评分只在权限受限的一次性本地目录中处理；仓库仅记录以上无字段值聚合证据。

本地根因复核没有发现应由 Mapper 修补的第二套账单规则：`bill-extract/2` 已明确要求精确主币金额、语义商户/交易对方、正确购销角色、全部非空核心字段和补充字段 Evidence，以及禁止重复包装；9 个通过根身份的响应也全部成功进入薄 Mapper 和 Claim Validation。5 个超时与 1 个 Provider 拒绝属于当前 Provider/模型完成率问题；无效 JSON、字段语义偏差和 Evidence 缺失属于模型输出质量问题；本地校验正确阻断了非法金额和不完整 Evidence。用本地解析、猜值或 Evidence 重建掩盖这些失败会违反当前架构。若建立下一 Prompt 候选，必须使用新的可审计 Prompt 身份和独立冻结清单，先通过无真实图片验证，不能原地修改 `bill-extract/2`。

## 智谱单变量 A/B 结果

产品负责人随后明确授权智谱处理同一 v4 的 16 份样本。A/B 保持清单、Prompt、Extraction/Provider/Claim Schema、Mapper、输入处理、`json_object`、temperature 0、并发 2、60 秒模型超时、150 秒任务超时和单次 Schema 重试完全不变，只替换：

- Provider 主机：`open.bigmodel.cn`；
- 模型：`glm-5.3-flash`。

合成蓝色能力探针通过后，16 份真实预检完整执行。智谱结果为：

- 11 / 16 形成本地 Claim，形成 Claim 的 11 / 11 均通过 v2 Schema；其余 5 份全部为 Provider 超时；
- 11 个 Claim 全部为 `blocked`，完整 Claim 契约仍为 0 / 16；Validation 包括 12 个关键字段 Evidence 缺失、3 个必填字段缺失、2 个补充字段 Evidence 缺失和 1 个 Evidence 过多；
- 分类、金额、发票号、日期、名称、关键 Evidence 和清单断言分别为 68.75%、68.75%、16.67%、62.5%、45.45%、64% 和 62.26%；
- v4 缺少缺失/冲突事件，召回仍不可评估；AI 直接 Fact 为 0。

与同配置 Qwen 相比，智谱的完成率、分类、金额、日期、名称和关键 Evidence 分别提高 12.5、12.5、18.75、6.25、13.63 和 4.48 个百分点，发票号下降 16.66 个百分点，Schema 同为 100%，完整 Claim 契约同为 0%，清单断言相同。智谱没有出现无效 JSON 或 Provider 拒绝，因此是更好的下一 Prompt 候选模型；但它仍有 5 / 16 超时、全部 Claim 被阻断且发票质量明显不足，未达到也不接近最终门槛，不能据此切换生产模型或启动 100 样本。

该 A/B 的原始运行与评分同样只在权限受限的一次性本地目录中处理；仓库仅记录以上无字段值聚合结果。再次使用任一 Provider 发送真实图片，都需要新的明确授权。

## 五份发票失败归因诊断

产品负责人随后批准按最小样本验证“拆分发票 Prompt”与“视觉转录后再做语义整理”是否优于当前混合直出。诊断继续使用智谱 `glm-5.3-flash` 和 `open.bigmodel.cn`，从冻结 v4 中选择 `V4-INV-001`、`V4-INV-003`、`V4-INV-004`、`V4-INV-005`、`V4-INV-006` 共五份发票；`json_object`、temperature 0、并发 2、60 秒单请求超时与一次根结构重试保持固定。三条路径使用完全相同的原始冻结图片字节：

1. 当前 `bill-extract/2` Payment/Invoice 混合 Prompt 直接从图片输出；
2. `invoice-extract-diagnostic/1` 发票专用 Prompt 直接从图片输出；
3. 同一模型先输出 `invoice-visual-observation/1` 视觉转录，再由 `invoice-semantic-from-observation/1` 从转录结果生成最终对象。

安全聚合结果为：

- 当前混合直出完成 3 / 5，另外两份超时；只看三个完成响应，期望业务值平均正确率为 83.89%，Evidence 平均覆盖率为 71.67%，完整契约代理为 0 / 5；
- 发票专用直出完成 1 / 5，另外四份超时；唯一完成响应的期望业务值与 Evidence 均为 100%，形成 1 / 5 完整契约代理，但该完成率不能支持替换当前 Prompt；
- 视觉转录完成 3 / 5，另外两份超时；只看完成响应，预期证据文本召回平均为 94.45%；
- 同模型两阶段最终完成 0 / 5：两份在视觉转录阶段超时，另外三份在语义整理阶段超时；成功返回的各阶段均通过对应 JSON 根结构，没有无效 JSON 或根身份错误；
- 全部 18 次实际调用中有 11 次超时。诊断没有持久化原始模型响应、字段值或额外图片副本，仓库只保留以上样本 ID、冻结配置和无字段值聚合结论。

该结果否定的是“在当前 60 秒边界内，把同一个 `glm-5.3-flash` 拆成两个顺序调用就会自然变好”，并不否定专门 Document Parser/OCR：本次第一阶段仍是同一通用 VLM。发票专用 Prompt 的唯一完成样本显示质量潜力，但四次超时以及固定调用顺序可能带来的 Provider 时序影响，使它不足以形成单变量架构结论。五份样本均为单明细发票，且诊断直接使用原始冻结图片而非 `document-normalize/2` PNG，因此不能测量多行表格对齐，也不能与此前生产链预检做绝对分数比较。本轮不修改活动架构、不降低指标、不进入 100 样本或 M2；任何后续真实调用仍需新的明确授权。

## 阿里云官方 Qwen3.8-Flash 同组诊断

产品负责人随后要求改用官方凭据测试 `qwen3.8-flash`。执行前根据阿里云百炼官方[模型说明](https://help.aliyun.com/zh/model-studio/qwen3-8-flash)与 [OpenAI 兼容接口说明](https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions)确认：模型调用 ID 为 `qwen3.8-flash`，支持 Image/Text 输入和结构化输出，北京业务空间使用专属 `*.cn-beijing.maas.aliyuncs.com/compatible-mode/v1` 端点。诊断读取既有权限 `0600` 的阿里云 Key 文件，先以不含真实图片的 JSON 能力探针验证端点、凭据和模型，单次通过且耗时 543 ms；随后才发送与智谱诊断完全相同的五份冻结发票。为避免默认思考链把耗时混入视觉质量，显式设置 `enable_thinking = false`；其余 `json_object`、temperature 0、并发 2、60 秒单请求超时、一次根结构重试、原始冻结图片字节及三条诊断路径保持一致。

安全聚合结果为：

- 当前 `bill-extract/2` 混合直出完成 5 / 5，平均耗时 7,559 ms、最慢 9,340 ms；期望业务值平均正确率 90.52%，全部期望 Evidence 平均覆盖率 79.36%，完整契约代理仍为 0 / 5；
- 混合直出的业务值错误集中为发票号 2 次、销售方 2 次、总额 1 次；Evidence 失败集中为币种 5 次、发票号 2 次、销售方 2 次、总额和税额各 1 次；
- 发票专用直出完成 5 / 5，业务值与 Evidence 均为 83.03%，完整契约代理 0 / 5；相较当前混合 Prompt 没有改善；
- 视觉转录完成 5 / 5，预期证据文本召回为 92.18%；主要漏项仍是低质量或屏摄样本中的发票号与销售方，与当前直出的主要字段错误基本重合；
- 同模型两阶段完成 5 / 5，业务值正确率 73.88%，低于当前直出；其 Evidence 计分为 0%，但 `invoice-semantic-from-observation/1` 只要求逐字 Evidence、没有完整声明 Evidence 元素的 `path`、`quote`、`page`、`region` 形状，故该 0% 同时暴露了诊断 Prompt v1 的契约缺陷，不能作为否定两阶段架构的有效证据。后续若继续该方向，必须发布新的诊断 Prompt 身份后再测，不能原地解释或修补本轮结果。

与同组智谱探索诊断相比，官方 Qwen 当前混合直出把完成率从 3 / 5 提升到 5 / 5；智谱只看已完成响应的业务值/Evidence 为 83.89%/71.67%，Qwen 五份整体为 90.52%/79.36%，同时没有超时。因此 `qwen3.8-flash` 成为当前更优的下一 Prompt 候选，但发票号、销售方和 Evidence 仍未达到批准门槛，不能切换生产、启动 100 样本或进入 M2。原始模型响应没有持久化，安全结果不含真实字段值或 Key；任何后续真实调用仍需新的明确授权。

## `bill-extract/3` 五发票单变量 A/B

产品负责人随后明确授权继续至取得测试结果。先发布诊断身份 `m1-invoice-prompt-ab/2`，以离线自测固定 `bill-extract/3` 候选的完整 Evidence 元素形状、币种复用可见金额原文、发票号逐字复核和购销方角色复核；模型 Envelope、Schema、Mapper 和活动架构均未改变。能力探针一次通过后，使用同一官方 `qwen3.8-flash`、同一五份原始冻结图片、`json_object`、temperature 0、`enable_thinking=false`、60 秒超时、一次根结构重试和并发 2 做 v2/v3 单变量 A/B。为减少固定先后顺序影响，每份样本内按样本序号交替先运行 v2 或 v3；v2/v3 Prompt SHA-256 分别为 `a7134026c550b7fdc547375ff288ddd3bfc1dce13b92002d03a485996a0e969a` 与 `10e0f2f98c789a3bf396731cfa1087d41eb5a781139fa28a76b05b4e0cf271ec`。

安全聚合结果为：

- v2 本轮完成 4 / 5，一份在两次尝试后仍为错误根身份；只看四份完成响应，业务值为 85.84%、Evidence 为 79.16%，严格形状为 4 / 5，完整契约代理为 0 / 5；
- v3 完成 5 / 5，业务值为 86.18%、Evidence 为 84.18%，严格形状为 5 / 5，完整契约代理为 2 / 5；五份结果分别为两份达到完整契约、一份不变、一份仅 v3 完成、一份 v3 回退；
- v3 把币种 Evidence 失败从 5 次降到 1 次，并消除了本轮三次总额值/Evidence 失败，证明明确的 Evidence 完成清单有效；
- v3 仍有发票号 2 次、销售方 2 次业务值与 Evidence 失败；另有一份样本的明细名称、明细金额和明细税额回退，其中金额与税额是 v3 新增回退，故不能把 Evidence 改善解释为整体语义质量已经稳定提高；
- 前一轮 v2 在相同五份样本上的业务值为 90.52%，高于本轮 v3 的 86.18%，也说明五份小样本与非确定性 Provider 响应不能支持只挑本轮基线的乐观结论。

因此 `bill-extract/3` 保持未激活候选：它验证了“明确 Evidence 契约”方向，但没有达到预定 5 / 5 完整契约目标，并出现明细字段回退。活动 Prompt 仍为 `bill-extract/2`；不启动 16 份预检、100 样本或 M2。原始模型响应没有持久化，临时安全结果只包含样本 ID、路径级失败和聚合指标，完成隐私检查后删除；任何下一次真实调用仍需新的明确授权。

## 高分辨率、v4 与思考模式上限诊断

继续授权后，先核对阿里云百炼官方 [OpenAI 兼容接口说明](https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions)：Qwen3.8 图像默认 `max_pixels` 为 2,621,440，启用 `vl_high_resolution_images=true` 后固定上限为 16,777,216；非思考模式默认 `presence_penalty` 为 1.5。五份样本中，屏摄发票为 4032×3024，超过默认像素上限并会被缩小；其余四份均低于默认上限。由于账单抽取需要多个字段复用同一可见原文，后续诊断把 `vl_high_resolution_images=true` 与 `presence_penalty=0` 同时冻结为两侧公共 Provider 配置，不把它们伪装成 Prompt 单变量。

`m1-invoice-prompt-ab/3` 在该公共配置下比较 v3/v4；v4 只增加发票号码、购销方、合计区和明细行的独立读取步骤，并明确保留“详见销货清单”类可见汇总行。v3/v4 Prompt SHA-256 分别为 `10e0f2f98c789a3bf396731cfa1087d41eb5a781139fa28a76b05b4e0cf271ec` 与 `ed50f9fbea97014dff23302918b663a86d3d2ad7bf6868aa0de11f35aa4b19cb`。安全聚合结果为：

- v3 完成 5 / 5，业务值和 Evidence 均为 86.18%，严格形状 5 / 5，完整契约 2 / 5；币种 Evidence 本轮 5 / 5 通过，但仍有发票号 2 次、销售方 2 次以及一份明细名称/金额/税额失败；
- v4 完成 5 / 5，业务值 86.85%、Evidence 87.18%、严格形状 5 / 5，但完整契约降为 0 / 5；它修复了屏摄样本的明细金额和税额，却让两份原本完整的样本新增总额错误，并且没有改善两份发票号或两份销售方错误；
- 因此 v4 属于字段间回退，不得用略高的平均分掩盖完整契约从 2 / 5 降到 0 / 5，已明确否决且不激活。

最后用相同高分辨率、`presence_penalty=0` 和 60 秒边界，对 v2/v3 开启 Qwen3.8 低档思考模式。v3 只完成 4 / 5，一份在两次尝试后仍为错误根身份；只看完成响应，业务值为 90.65%、Evidence 为 86.48%，但严格形状和完整契约按全部样本分别为 4 / 5 与 0 / 5，平均完成耗时从非思考 v3 的 7.54 秒增至 18.95 秒。思考模式没有突破发票号、销售方和困难屏摄样本，反而失去完成率与完整契约，故也明确否决。

当前同一模型、同一单次图片直出架构内的最佳探索配置是未激活的 `bill-extract/3 + enable_thinking=false + vl_high_resolution_images=true + presence_penalty=0`，但它仍只有 2 / 5 完整契约，远离正式门槛。三轮结果共同表明：明确 Evidence 契约可以修复系统性 Evidence 遗漏；继续增加 Prompt 指令无法稳定恢复低质量图片中的发票号、销售方和明细文本，并会让已正确字段回退。至此停止该模型上的 Prompt 修补；下一步若继续提升质量，需要新的模型/视觉输入能力决策，而不是继续堆字段规则。活动 Prompt 仍为 `bill-extract/2`，不启动 16、100 样本或 M2。

## Qwen3.5-OCR 默认纯转录诊断

产品负责人随后要求测试专用 `qwen3.5-ocr`。执行前根据阿里云百炼官方 [Qwen-OCR 使用说明](https://help.aliyun.com/zh/model-studio/qwen-vl-ocr)与 [qwen3.5-ocr 模型说明](https://help.aliyun.com/zh/model-studio/qwen3-5-ocr)冻结边界：OpenAI 兼容 Chat 请求只发送 User 图片消息，不发送自定义 System Message、`response_format` 或自定义 Prompt，直接使用 Provider 默认纯文字转录；每图 `min_pixels=3,072`、`max_pixels=12,582,912`，单并发、120 秒超时、零重试。相同五份已授权冻结发票各调用一次，原始 OCR 文本不落盘。

首轮严格证据连续文本计分暴露了金额数值正确但币种符号缺失会同时判错金额、税额和币种的耦合。由于隐私边界禁止保存 OCR 原文，发布诊断身份 `m1-invoice-ocr-transcription/2` 后以相同配置重新调用一次，并同时报告严格证据文本召回与去除格式差异后的业务值文本召回。最终安全聚合结果为：

- 5 / 5 完成，平均耗时 3,197 ms、最慢 6,412 ms，没有 Provider 拒绝、超时或重试；
- 严格 Evidence 原文召回按样本平均为 69.06%，按 55 个期望路径加权为 69.09%，0 / 5 达到完整召回；
- 业务值文本召回按样本平均为 81.36%，按 55 个期望路径加权为 81.82%，0 / 5 达到完整召回；
- 金额、税额、明细名称和明细数值在第二轮全部出现；剩余业务值文本漏项为币种 3 次、发票号 2 次、销售方 2 次、日期 2 次、购买方 1 次；
- 相同输入与配置下，首轮严格原文召回为 65.06%，第二轮为 69.06%，其中一份低质量截图单样本变化 20 个百分点；默认 OCR 输出存在可见轮次波动，五份单轮高分不能当作稳定性证据；
- 与同组 `qwen3.8-flash` 字段感知视觉转录的 92.18% 严格证据文本召回相比，默认纯 OCR 没有形成质量提升；两者 Prompt 与输出形状不同，因此该对照只能用于否决“直接增加默认 OCR 就会更好”，不能形成通用模型排名。

结论是暂不把专用 OCR 加入活动链路：它的金额与明细转录有价值，但没有解决当前最难的发票号和销售方，且仍需第二阶段完成语义映射、Evidence 和最终 JSON 契约，会增加调用面。该诊断不修改活动 Prompt、Provider、Schema、Mapper 或核心架构，不启动 16、100 样本或 M2。临时安全报告只包含样本 ID、耗时、token、路径级失败和聚合指标，完成文档记录后删除；后续真实调用仍需新的明确授权。

## Qwen3.5-OCR 直接 JSON 契约诊断

产品负责人否决把本地 OCR 放回活动链路，并授权验证 `qwen3.5-ocr` 能否作为纯 AI Provider 直接输出最终 JSON。`m1-invoice-ocr-direct-extraction/1` 使用同一官方端点、同一五份冻结发票和现行 `bill-extract/2` 原文，Prompt SHA-256 为 `a7134026c550b7fdc547375ff288ddd3bfc1dce13b92002d03a485996a0e969a`。为保持可替换 Provider 边界，没有增加模型专属 Prompt、本地字段解析、正则抽取或响应修复；按模型能力只发送一个 User 图片/文字消息，不发送 System、`response_format` 或 temperature。JSON 必须由 `JSON.parse` 原样接受并满足 `bill-extraction/2` 根身份；只有无效 JSON 或根身份允许一次完全相同的重试。

结果为 0 / 5 有效 JSON：五份样本都在两次响应后保持 `invalid_json`，没有 Provider 拒绝、网络错误或超时。每份含重试的总耗时平均为 6,290 ms，最慢为 12,352 ms。由于没有任何响应通过 JSON 传输门禁，业务值、Evidence 和完整契约均不能作语义质量评分；将其记为 0% 只表示端到端 Provider 失败，不能解释为所有视觉字段都未识别。

同组 `qwen3.8-flash` 至少能够 5 / 5 返回有效根对象，而 `qwen3.5-ocr` 在不允许模型专属修复的产品边界下不能直接替换当前 Provider，因此本候选明确否决，不再进行 Prompt 修补、16 份预检或活动链路接入。该结果同时保留纯 AI、Provider 可替换的目标架构：后续候选必须原生支持稳定 JSON/Schema 输出，再比较视觉语义质量。活动 Prompt、Provider、Schema、Mapper 和 M1 门禁均未修改，不进入 M2。

## Qwen3.8 中文字段清单与薄拼装诊断

产品负责人随后提出由模型使用中文指令只返回简单字段清单，再由本地薄 Assembler 拼装最终对象，并明确先使用官方 `qwen3.8-flash` 验证。`m1-invoice-claims-assembly/1` 发布 `bill-claims-cn/1`：模型只选择受限语义 `path`，复制字符串 `value`、可见 `quote` 和页码，额外信息进入 `other_fields`；本地只补齐 Payment/Invoice 固定空键、连续明细数组、独立 Evidence 和最终 `bill-extraction/2` 根对象，不读取证据文字、不解析金额或标识符、不猜测字段。Prompt SHA-256 为 `33ddec45799156a490d85344150f7c7f3c2278555cbd7667fb16606aa6af4c1d`。

同一五份冻结发票使用当前最佳公共模型配置：`json_object`、temperature 0、`enable_thinking=false`、`vl_high_resolution_images=true`、`presence_penalty=0`、并发 2、60 秒超时和一次根契约重试。安全聚合结果为：

- 字段清单 5 / 5 完成，4 份一次通过，1 份在一次相同重试后通过；薄拼装后的最终根结构 5 / 5 合法，证明重复字段记录可以消除最终嵌套 JSON 的结构负担；
- 期望业务值为 86.18%，与同模型高分辨率 `bill-extract/3` 直出的 86.18% 完全相同；两条链路的失败路径也完全相同：发票号 2 次、销售方 2 次，以及屏摄样本的明细名称、明细金额和明细税额各 1 次；
- Evidence 为 79.03%，低于直出 v3 的 86.18%；新增差距主要是币种 Evidence 4 次不满足，完整契约从直出的 2 / 5 降为 0 / 5；
- 平均完成耗时 6,987 ms、最慢 8,030 ms。全部原始字段清单和模型响应均未持久化，安全结果只包含样本 ID、计数、路径级失败和聚合指标。

该结果证明中文字段清单加薄拼装在架构上可执行，但不能恢复模型没有正确看见或理解的发票号、销售方和困难明细。即使单独补强币种 Evidence，也最多恢复到当前直出 v3 的同等结果，无法突破业务值上限，因此不再对 Qwen3.8 修补该 Prompt，也不激活 `bill-claims-cn/1`。活动链路仍为 `bill-extract/2`；后续若选择更强视觉模型，可以复用这条简单 Claim 契约重新做资格赛，但不得把本轮结果解释为已通过 M1、16 份预检、100 样本或 M2 门禁。

## Qwen3.8 本地通用图像增强单变量 A/B

产品负责人随后提出在本地先改善图片、再保持纯 AI 提取，并授权使用阿里云官方 `qwen3.8-flash` 对同组五份发票测试。候选身份为 `document-normalize/3-candidate-a`：只做 EXIF 方向归一化；最长边小于 1,600 px 时以 Lanczos 进行 2× 放大；在亮部裁切与亮度方差同时满足保护条件时，才对亮度通道执行 0.5% 自动对比度、1.08 倍对比度和有界反锐化。大于一千万像素的屏摄照片使用 4:4:4、质量 95 的 JPEG，避免增强后的 PNG 超出 20 MiB 保护边界。该候选不包含 OCR、字段正则、票据模板、固定坐标、业务裁剪或响应修复；五份派生图及清单只位于权限 `0600`、Git 忽略且 Docker 排除的本地隔离区，候选清单 SHA-256 为 `494b9d3ca667103f35618ce425a9ccbb1635861b266518ab97fa87263446df81`。

`m1-invoice-image-input-ab/1` 使用完全相同的 `bill-claims-cn/1`、薄 Assembler、`json_object`、temperature 0、`enable_thinking=false`、`vl_high_resolution_images=true`、`presence_penalty=0`、并发 2、60 秒超时和一次根契约重试，只在每份样本内交替原图与增强图的调用顺序。沙箱内首次命令的十条路径均立即返回本地 `network_error`，没有图片到达 Provider，也不计入评分；允许联网后的纠正运行结果为：

- 原图与候选均 5 / 5 完成、5 / 5 根结构合法，平均耗时分别为 7,339 ms 与 7,417 ms；
- 原图业务值 84.52%、Evidence 80.70%、完整契约 1 / 5；候选业务值 88.00%、Evidence 82.52%、完整契约仍为 1 / 5；
- 五份逐样本比较为候选改善 3、不变 1、回退 1；候选修复一处币种值、一处发票号值及 Evidence 和一处币种 Evidence，没有新增业务值错误；
- 唯一回退发生在 4032×3024 屏摄样本，为币种 Evidence 回退；该样本原有的明细名称、金额和税额错误没有改善，销售方错误也没有减少；
- 候选输入 token 从 17,268 增至 22,368，增加约 29.5%，但完整契约没有提升。

因此本轮证明“低分辨率自适应放大与轻度锐化”存在正向信号，但没有证明整套候选可进入活动链路。`document-normalize/3-candidate-a` 因屏摄回退、成本增加和完整契约不变而不激活；屏摄照片的自动对比度分支停止，若继续应把“低分辨率放大/轻锐化、原尺寸高分辨率图不变”发布成新的独立候选并重新取得真实调用授权。当前活动输入仍为 `document-normalize/2`，活动 Prompt 仍为 `bill-extract/2`，不启动 16、100 样本或 M2。

产品负责人随后批准继续。`document-normalize/3-candidate-b` 沿用 candidate-a 中四份低分辨率图已经通过保护检查的完全相同派生字节，但删除全部对比度分支；最长边达到 1,600 px 的图像不再生成副本，直接旁路原始字节。候选清单 SHA-256 为 `ea43aebc8a8fa57ead57d1befe8aeeb988932d9af69f17f1f904866329236e68`。`m1-invoice-image-input-ab/2` 对四份实际变化的图片继续交替 A/B；4032×3024 屏摄样本只调用一次并由两侧共享相同响应，避免对相同字节重复调用后把 Provider 波动误判为图像差异，因此本轮实际路径请求为 9 个。

candidate-b 的安全聚合结果为：

- 原图与候选仍均为 5 / 5 完成、5 / 5 根结构合法、完整契约 1 / 5；
- 业务值由 86.18% 提升到 88.00%，Evidence 由 82.52% 提升到 84.52%，逐样本为改善 2、不变 3、整体回退 0；
- 一份低分辨率发票的发票号值在 candidate-a 与 candidate-b 两轮中均被相同增强字节修复；另一份的币种 Evidence 也连续改善，证明低分辨率增强存在可重复信号；
- 一处币种 Evidence 在同一样本内由正确转错，但同时修复了该样本的发票号值与 Evidence，样本总体仍改善；销售方 2 次和屏摄样本的明细名称、金额、税额错误仍完全未改善；
- 候选路径输入 token 仍为 22,368，对照为 17,268，增加约 29.5%；平均耗时由 7,095 ms 增至 7,814 ms，增加约 10.1%。

candidate-b 据此冻结为下一轮 M1 视觉输入开发基线，但不替换活动 `document-normalize/2`：它消除了 candidate-a 的屏摄回退并给出重复的低分辨率收益，却没有增加完整契约、没有触及销售方和困难屏摄明细，并带来明显 token 成本。若继续处理屏摄场景，下一决策应是通用页面裁边/多视图分块等新的视觉输入能力或更强模型，而不是恢复对比度分支、OCR 字段解析或继续堆 Prompt；再次发送真实图片仍需新的授权。

## Qwen3.8 高分辨率同页多视图诊断

产品负责人随后批准继续验证通用多视图分块。`document-normalize/3-candidate-c` 保持 candidate-b 的四份低分辨率派生字节不变；对唯一 4032×3024 屏摄样本生成一张最长边 1,024 px 的全景和四张 2×2、相邻区域重叠 12% 的原像素 JPEG 视图，不使用 OCR、字段标签、模板、固定业务坐标或内容感知裁边。候选清单 SHA-256 为 `020013fc1e3ee2d34c04c507421f726af27e0c6e8e1d55e66cf7b8f8ed58c5e2`，9 份派生视图与清单继续只保存在权限 `0600`、Git 忽略且 Docker 排除的本地隔离区。

`m1-invoice-image-input-ab/3` 给 A/B 两侧使用完全相同的 `bill-claims-cn/1`、薄 Assembler 和 Provider 参数，并增加同一条中性输入协议，明确所有缩放图和分块图都属于文档第 1 页、不得重复字段或明细；协议 SHA-256 为 `ca967649f0fc097a1b8dcbb637050e01c4427fc7462d4095c19e4b91b0673262`。每份原图和候选均独立调用并交替顺序，本轮共完成 10 个实际路径请求：

- 原图与候选均 5 / 5 完成、5 / 5 根结构合法；业务值由 82.18% 提升到 86.00%，Evidence 由 76.70% 提升到 84.18%，完整契约由 1 / 5 提升到 2 / 5；
- 逐样本为候选改善 4、不变 1、整体回退 0；候选共修复税额、发票号、币种各一处业务值以及相应 Evidence，但另有一处币种业务值回退，未发生 Evidence 回退；
- 高分辨率屏摄样本的业务值与 Evidence 均由 50% 提升到 60%，唯一修复路径是币种；销售方、明细名称、明细金额和明细税额仍全部失败，因此多视图没有解决本轮实际目标；
- 候选总输入 token 由 17,388 增至 24,820，增加约 42.7%；平均耗时由 6,229 ms 增至 7,216 ms，增加约 15.8%。屏摄样本本身的输入 token 增加约 18.5%，耗时增加约 29.5%。

candidate-c 不替换 candidate-b，也不进入活动链路。四份低分辨率候选与 candidate-b 是相同字节，本轮新增完整契约来自其中一份低分辨率样本，无法归因于新加入的高分辨率分块；真正由 candidate-c 改变的屏摄路径只恢复币种，没有触及销售方或困难明细，却进一步增加成本。它据此冻结为“通用同页多视图已验证但未采用”的负向边界证据；candidate-b 仍只作为低分辨率视觉输入开发基线，活动输入继续是 `document-normalize/2`，不启动 16、100 样本或 M2，任何再次发送真实图片仍需新的明确授权。

## Qwen3.8 Flash 与 Max 原图模型 A/B

产品负责人检查原图后认为其在手机端可清晰阅读，要求停止本地图像优化，直接比较模型能力。`m1-invoice-model-ab/1` 因此把五份冻结原图逐份、原字节交给 `qwen3.8-flash` 与 `qwen3.8-max`；两侧使用完全相同的阿里云 Provider 主机、`bill-claims-cn/1`、同页输入协议、薄 Assembler、`json_object`、temperature 0、`enable_thinking=false`、`vl_high_resolution_images=true`、`presence_penalty=0`、60 秒超时、一次根契约重试和路径级计分。Max 的无图片能力探针一次通过，随后 10 个真实图片路径请求全部完成；工具没有使用 candidate-a/b/c 派生图，也没有持久化原始响应或字段值。

安全聚合结果为：

- Flash 与 Max 均 5 / 5 完成、5 / 5 根结构合法；输入 token 均为 17,388，确认两侧使用相同视觉输入预算；
- Flash 为 86.18% 业务值、79.03% Evidence、0 / 5 完整契约；Max 为 86.52% 业务值、85.03% Evidence、0 / 5 完整契约。Max 的业务值仅增加 0.34 个百分点，Evidence 增加 6.00 个百分点，逐样本为改善 1、不变 2、回退 2；
- 在 4032×3024 屏摄样本上，Max 把业务值从 60% 提到 80%、Evidence 从 60% 提到 90%，修复明细金额、明细税额及三项明细 Evidence；销售方和明细名称业务值仍失败。该结果证明原图包含模型可提取的信息，Flash 的视觉能力确实是该困难样本的限制之一；
- Max 同时在三份普通发票上统一漏掉一个可见且非零的发票税额字段，每份 Claim 数都比 Flash 少 1，造成三处业务值与 Evidence 回退；发票号只少失败 1 次，销售方失败次数仍为 2；
- Max 平均耗时为 18,877 ms，Flash 为 6,546 ms，约为 2.88 倍；Max 最慢 30,240 ms，并有一份需要一次相同契约重试。输出 token 分别为 3,914 与 4,079。

本轮修正了“失败主要来自图片不清晰”或“失败全部来自 Flash”这两种单一归因：Max 对困难屏摄明细有明确视觉增益，说明更强模型方向有效；但它对普通发票产生稳定的税额遗漏，整体业务值几乎不变且完整契约没有增加，因此不能直接替换 Flash 或启动更大样本。`qwen3.8-max` 冻结为新的模型候选而非活动模型；Prompt、Schema、Assembler、活动输入和最终门槛均不改变，不进入 16、100 样本或 M2，任何复测仍需新的明确授权。

## Shuai GPT-5.6 Sol 原图资格诊断

产品负责人随后授权只通过 Shuai 测试 `gpt-5.6-sol`，千问继续只引用阿里云官方 Key 已冻结的 Flash/Max 结果。目标模型在该 Key 的 `/v1/models` 目录中可见；Shuai 的 Chat Completions 路径连续返回 502，而 Responses 最小能力探针通过，因此 `m1-invoice-original-model/1` 使用 `https://api.shuaiapi.com/v1/responses`。本轮仍使用相同五份冻结原图、`bill-claims-cn/1`、同页输入协议、薄 Assembler 和相同路径级评分，不使用任何本地图像派生物。请求固定 `reasoning.effort=medium`、每图 `detail=high`、`store=false`、8,192 输出 token 上限、120 秒超时和并发 2；Qwen 专属参数全部省略。

Shuai 的 Responses 兼容层拒绝原生 `json_object` 格式，因此模型仍接收同一 JSON-only Prompt，返回文字只允许严格一次 `JSON.parse`；不剥代码围栏、不抽取 JSON 子串、不修复字段，无效 JSON 或根身份只允许一次完全相同的重试。该协议差异、Provider 和超时均与阿里云 Qwen 不同，所以本轮是候选资格诊断，不宣称为严格单变量 A/B，也不能外推为 OpenAI 官方直连结果。

安全聚合结果为：

- 5 / 5 请求完成，5 / 5 字段清单和薄拼装根结构合法，业务值与 Evidence 均为 69.70%，完整契约 0 / 5；
- 失败集中在销售方 4 次、明细名称 4 次、发票号 3 次、购买方 2 次，另有日期、明细金额和明细税额各 1 次；两份本应缺失的明细单价被错误填充；
- 4032×3024 屏摄样本只有 30% 业务值和 30% Evidence，低于同图 Flash 的 60%/60% 与 Max 的 80%/90%，没有复现 Max 对困难明细的提升；
- 平均耗时 64,108 ms、最慢 92,138 ms，输入/输出 token 合计 9,065/5,367。相对官方 Flash，业务值低 16.48 个百分点、Evidence 低 9.33 个百分点、平均耗时约 9.79 倍；相对官方 Max 分别低 16.82 和 15.33 个百分点、平均耗时约 3.40 倍；
- 权限 `0600` 的本地结果只含配置、样本 ID、路径级失败和聚合指标；原始模型输出、字段值、图片副本和密钥均未持久化或写入文档。

Shuai `gpt-5.6-sol + medium` 在当前五图资格赛中明显低于两个官方千问基线，不能替换活动模型，也没有资格扩大到 16 或 100 样本。该结论只否决本次 Shuai 路由与冻结配置，不否定 GPT-5.6 Sol 在其他 Provider 或官方直连下的能力；若未来重测，必须重新明确 Provider、协议、样本数与授权。

## Qwen3.8 最小业务 JSON 与完整契约 A/B

产品负责人随后质疑现有评测是否把本可直接识别发票的模型压在过重 Prompt 与 JSON 契约下，并授权阿里云官方 `qwen3.8-flash` 对同一五份冻结原图做最后一轮分层归因。`m1-invoice-minimal-contract-ab/1` 逐样本交替调用当前原样 `bill-extract/2` 与新诊断候选 `invoice-values-cn/1`；两侧的 Provider、模型、原始图片字节、`json_object`、temperature 0、`enable_thinking=false`、`vl_high_resolution_images=true`、`presence_penalty=0`、60 秒超时和一次根身份重试完全一致。候选只要求发票号、日期、金额、币种、购销方与明细的直接 JSON 值，不要求文档分类、Payment 分支、Evidence、issues、other_fields 或生产根 Envelope，也没有本地 OCR、图像增强、字段解析或响应修复。

10 个真实图片请求全部完成，安全聚合结果为：

- 完整契约与最小候选均为 5 / 5 完成、5 / 5 各自输出形状合法；完整契约业务值为 88.52%，最小候选为 86.85%，候选下降 1.67 个百分点；
- 五份逐样本为 4 份业务值持平、1 份候选回退，候选没有修复任何业务路径，并额外错了一处明细数量；两侧完整业务值样本均为 0 / 5；
- 两侧共同失败为总额 1 次、发票号 2 次、销售方 2 次、明细名称 1 次，说明删除结构与 Evidence 负担没有改变当前核心错误；完整契约自身的 Evidence 为 81.36%，其中币种 Evidence 5 / 5 未满足；
- 最小候选平均耗时为 3,528 ms，对照为 14,809 ms，约减少 76.2%；输出 token 为 935，对照为 4,872，约减少 80.8%。最小契约显著降低时延与输出成本，但没有提高识别质量。

按产品负责人随后要求，仅为当前对话人工核对又执行了一次同配置检查；该次原始响应只进入对话内存，未写入文件或文档，检查脚本运行后已删除。由于 Provider 即使 temperature 0 仍出现跨运行差异，该检查不替换上面的冻结聚合，也不形成新门禁分数，但暴露了原归因遗漏的评测问题：模型在部分样本中已经正确读出价税合计，却因 `total` 语义未明确而把不含税合计映射为核心字段、把价税合计放进 `other_fields`；数值等价的数量表示和可见括号会被当前精确比较判错；只包含货币符号的有效币种 Evidence 也会因未包含冻结的完整金额片段而失败。与此同时，发票号增删零、购销方单字误识别和对空白单价进行计算等真实模型错误仍然存在。

因此，本轮只能得出“删减 Prompt/JSON 没有提高当前评分，且能显著降低成本”，不能再把当前失败主要归因于模型。下一步必须先统一含税/不含税金额语义、数值等价规范化、可见标点规则、Evidence 最小充分性并复核标签，再用修正后的离线评分重算；完成前不应继续调 Prompt、排名模型或扩大真实样本。`invoice-values-cn/1` 仅保留为诊断身份，不替换活动 Prompt 或最终契约，也不启动 16、100 样本或 M2。权限 `0600` 的本地安全结果 SHA-256 为 `2429755e9431e8494a518137df2d57ddac8ba25b71f2cba3a9805347066c5b71`，其中不含字段值、原始响应、图片副本或 Key。

## 最小票面原文活动基线

产品负责人随后明确选择“用最小中文直接询问所需数字或文本，其余由本地确定性处理”，并批准替换 AI 输出契约与 Claim Mapper。当前软件基线已更新为：

`bill-visible-text-cn/1 -> bill-visible-text/1 -> bill-visible-text-provider/1 -> claim-mapper/3 -> document-claim/2`

- 模型每个非空字段只返回 `{text,page}`，本地以同一 `text` 生成 Evidence；不再要求模型重复生成独立 Evidence 或归一化业务值；
- 发票金额键拆为 `amount_without_tax`、`tax_amount` 和 `amount_with_tax`，只有含税合计进入 `total_minor`；
- 本地只执行批准币种的精确金额换算、日期/时间/时区、数量和表示法规范化，不纠正发票号、名称或明细字符，不计算空白单价；
- 评分器现把 `1` 与 `1.0` 视为数量等价、忽略只包住整值的可见中英文括号，并允许页码一致且能直接支持期望值的最小 Evidence；`¥` 支持 CNY，但数字 `1` 不能支持金额 `100.00`；
- 旧 Schema 文件、活动版本常量、合成 Provider、能力探针、Worker/HTTP/Mapper 夹具和关键不变量矩阵均已切到新身份，旧活动分支没有保留；
- `m1-real-dev-v4` 被 Runner 显式归类为旧契约冻结证据；该软件替换轮次结束时，下一真实预检仍须先发布并批准 `m1-real-dev-v5`。

该软件替换轮次本身没有调用 Provider，没有把真实图片或模型原始输出写入仓库，也没有启动 16、100 样本或 M2。新软件基线的 Go、静态、覆盖率、关键不变量、前端、Runner/评分器、数据集、生产构建和就绪冒烟均已实际通过，因此本节标记为“非真实 Provider 门禁通过”；其后的 v5 调用单独记录在下一节，旧 `bill-extraction/2` 的结果不参与该结论。

## 最小票面原文 v5 真实预检

产品负责人随后授权阿里云官方 `qwen3.8-flash` 使用当前活动 Prompt、Schema、Mapper、输入处理和重试策略处理 16 份既有真实开发样本。`m1-real-dev-v5` 首次发布为 10 份支付详情页与 6 份发票，清单 SHA-256 为 `30680f8547da883d93f4850c2ec43720f470d2ea16a70a7f036461152b96bee4`；资产哈希 16 / 16 一致，清单与资产均可确定性重建且权限为 `0600`。本轮固定 `json_object`、temperature 0、并发 2、`schema_validation_single_retry/1`，没有更改最终门槛、核心架构或输入图片。

第一次运行的最后一份样本在模型调用前返回 500。复核确认该图片能被生产 Inspector 正确解析，真实根因是宿主 `/tmp` tmpfs 已满；失败上传没有遗留对象，Provider 调用数为 0。将同一样本移到容量正常、Git 忽略且权限受限的磁盘环境后只补跑这一份，上传成功，但 Provider 在 16.9 秒后以 `provider_invalid_response` 拒绝请求。因此最终样本结论仍为 15 / 16 形成 Claim、1 / 16 Provider 路径失败；15 个 Claim 的根 Schema 为 15 / 15，通过样本中有 1 份使用冻结的单次 Schema 重试，业务样本实际 Provider attempt 共 17 次。

正式评分没有通过：完成率与分类均为 93.75%，发票号 50%，日期 93.75%，名称 68.18%，当前评分器的关键 Evidence 为 91.67%，清单断言为 75.47%，AI 直接 Fact 为 0；缺失/冲突召回分母仍为 0，不得把显示的 0%解释为模型能力。金额门禁显示 31.25%，但不能解释为金额 OCR 只有 31.25%：10 份支付的金额原文与冻结标签 10 / 10 逐字相同，模型却对 10 / 10 的独立币种字段返回空；同时 v5 的 10 份支付币种标签本身没有任何可见币种符号或文字。`claim-mapper/3` 遵守“不猜缺失币种”，因而保留金额原文但拒绝转换 minor units并阻断支付 Claim。该问题是标签、票面可见性与产品默认币种溯源规则之间的冲突，不应通过继续堆 Prompt 或把币种伪装成模型 Evidence 来修补。

仍可直接归于模型的误差包括：支付商户 6 / 10 精确，发票号 3 / 6（只看五份完成发票为 3 / 5），销售方 4 / 6（完成样本 4 / 5），购买方 5 / 6（完成样本 5 / 5）；另有一份发票的明细单价形成非法金额 Validation。与此同时，v5 直接复制 v4 的期望 Claim 路径，漏列新 Mapper 固定生成的 `supplementary_fields`，所以本轮 `claim_contract_rate = 0%` 也不是可用的单一模型质量指标。

安全聚合结果 SHA-256 为 `a2a2c212919f82cbfc6016f18fa41806d65ecbb2da1eb56da1f07fc43cc35dcc`；其中不含原图、真实字段值、模型原文或凭据。v5 已冻结为诊断证据并从当前 Runner 允许列表移除，重复运行会在读取密钥或联网前失败。不得据此启动 100 份评测；下一冻结集必须先解决不可见币种来源与 `supplementary_fields` 标签契约。

## v1 真实预检的历史诊断

`m1-real-dev-v3` 的 SHA-256 为 `0326adc8dab5473faed732e20080d7e0578d7116d46a62f63f3729277c814626`。它曾使用 Qwen3-VL、`json_object`、temperature 0 与单次 Schema 重试处理 16 份已授权原图：16 / 16 进入模型阶段，0 / 16 通过旧 Provider Schema，0 / 16 Claim，0 Fact；30 个 AiRun 为 Schema 失败，1 个被 Provider 拒绝。

Payment 统一出现旧 `/payment/amount#type`，Provider 接受的 Invoice 统一出现逐字段类型和额外属性问题。该证据证明 `{value, source}` 包装与整份叶子 Schema 是系统性阻塞点，不证明图片语义识别质量。v3 及更早的真实/合成运行只保留为冻结历史，当前 Runner 会在读取密钥或联网前拒绝它们。

## M1 当前阶段决定

2026-08-30，产品负责人明确决定：真实产品上传以清晰、完整、无遮挡且关键字段可直接人工辨读为当前质量承诺，不再为低清、屏摄、裁切和边界样本持续阻塞其余 M1 开发。2026-08-27 批准的高精度模型数值、至少 100 份数据和三次取最差协议没有被删除或伪造为通过，现统一移至 M4 首次正式发布门禁；当前 M1 只把这些数值作为观测指标。

M1 当前安全闭环证据：

1. v5 的 16 / 16 个任务均形成可审核 Claim 或带稳定错误码的明确 Provider 失败，没有丢任务或伪成功；
2. 本地接受的 15 / 15 个根对象通过权威 Envelope Schema；局部错误保留为 Validation 和 blocked Claim；
3. AI 直接创建 Fact 为 0，人工修订、确认、拒绝及其审计链已通过软件与 E2E 门禁；
4. 后端、Web、关键不变量、Compose E2E、响应式、可访问性、性能、内存、备份恢复和生产镜像证据均已通过；
5. Source/Claim/Fact、租户隔离、幂等、证据、隐私、凭据和明确失败边界没有例外。

已知模型误差、不可见币种产品语义、缺失/冲突真实样本以及 v5 `supplementary_fields` 标签缺陷继续保留为 M4 质量工作，不再阻塞功能里程碑开发。该门禁调整本身不构成 M1 完成或 M2 授权，正式记录见 `docs/acceptance.md`。

## M1 收口结论

产品负责人随后确认模型正确率专项安排在全部功能开发完成后处理，并授权继续推进。2026-08-30 的独立收口复核得到以下结果：

- `docs/scope.md` 的 M1 范围内能力均有实现和既有验收证据，没有发现可继续编码的 M1 业务缺口；
- Go 1.26.7 禁网、仓库只读容器中的 `go test -p=1 -timeout 60s ./...`、`go vet ./...` 与 `go build -buildvcs=false ./...` 均通过；
- Web `npm run check` 通过，覆盖 OpenAPI 客户端生成、TypeScript、ESLint、Prettier、10 个 Vitest 测试和生产构建；
- 领域/应用层覆盖率为 86.57%（1,579 / 1,824），基础设施/传输层为 71.60%（1,843 / 2,574），分别高于 85% 和 70% 门槛；
- 关键不变量矩阵 38 / 38（100%），包含租户隔离、完整 revision、人工审核、幂等、精确判重、一对一关联、任务租约、Visible Text、补充字段仅审核和取消后禁止写入；
- 首次仅挂载 `apps/api` 的容器运行因看不到仓库级 `contracts/` 与 `infra/` 而失败，已判为无效编排并以仓库根只读挂载纠正；首次构建仅因容器 Git VCS 戳记不可用失败，关闭不影响代码的 `-buildvcs` 后通过；两次无效运行均不计入门禁；
- 旧 Prompt/Observation/Assembler 门禁摘要已移入历史命名文件，当前摘要为 `tests/evidence/m1/gate-summary.json`；未调用真实 Provider，未安装 OCR，未提交、推送或进入 M2。

结合本文件既有 Compose E2E、响应式与可访问性、Lighthouse、性能、内存、备份恢复、生产镜像和安全证据，M1 退出条件全部满足，M1 于 2026-08-30 完成。该完成结论不代表模型正确率通过；历史失败与正式准确率门槛继续保留到 M4。M2 仍未启动且未获授权。
