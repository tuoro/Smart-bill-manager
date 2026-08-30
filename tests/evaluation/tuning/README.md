# M1 模型调优与预检

当前只允许执行不联网的纯合成技术自检。`m1-real-dev-v4` 已冻结为旧契约历史证据；`m1-real-dev-v5` 已完成一次当前最小票面原文契约的真实诊断，但因标签边界不完整而冻结并被 Runner 阻止重复发送。修正版真实调优资产及真实调用仍需分别批准；调优资产不能形成发布证据。

## 当前状态

- Prompt：`bill-visible-text-cn/1`；
- 模型输出 Schema：`bill-visible-text/1`；
- Provider Schema：`bill-visible-text-provider/1`；
- 本地 Claim：`document-claim/2`；
- Mapper：`claim-mapper/3`；
- 输入处理：`document-normalize/2`；
- 重试：`schema_validation_single_retry/1`；
- 默认时区：`Asia/Shanghai`；
- 最终质量门槛：保持 `docs/acceptance.md` 当前批准值。

`m1-real-dev-v1/v2/v3/v4` 和 Prompt v8/v9/v10 canary 是旧契约的冻结历史证据，不再是当前调优输入。v10 从未调用 Provider，现已停止。v5 是当前契约的一次诊断证据，也不是可重复使用的调优输入。当前 Runner 会在读取密码/API Key 或联网前拒绝 v1～v5；评分器只保留对 v5 已有诊断结果的离线复核能力。

下一份真实候选必须冻结 `bill-visible-text-cn/1`、`bill-visible-text/1`、`bill-visible-text-provider/1`、`claim-mapper/3`、`document-claim/2`、输入处理与重试策略，并明确不可见币种的产品来源、Claim 溯源与 `supplementary_fields` 标签。它尚未发布，也没有新的 Provider 调用授权。

## 中文真实调优集 v5（冻结诊断）

v5 从 v4 的 16 份原图首次发布，资产未改像素，清单 SHA-256 为 `30680f8547da883d93f4850c2ec43720f470d2ea16a70a7f036461152b96bee4`。阿里云官方 `qwen3.8-flash` 预检形成 15 / 16 Claim，15 / 15 根 Schema 有效；10 份支付金额原文全部命中，但票面与标签没有可见币种标记，模型币种均为空，本地因此拒绝金额换算。v5 还漏列固定 `supplementary_fields` 的期望路径，不能用其 0% 完整 Claim 率评价模型，也不得进入 100 份门禁。安全聚合见 `docs/m1-evidence.md`，原图、字段值、模型原文和 Key 均未写入仓库文档。

## 纯合成技术自检

以下命令不调用 Provider：

```bash
node tools/check-model-tuning-dataset.mjs
node tools/score-model-evaluation.mjs --preflight-self-test
```

纯合成技术预检必须使用全新数据库和对象目录，且只能报告为 tuning preflight，`eligible_for_release_evidence` 必须为 `false`。运行结果不得改名为正式 `run-1`，也不得进入真实质量三轮门禁。

## 中文真实调优集 v4（冻结历史）

`m1-real-dev-v4` 从冻结 v3 原图和标签按当时的 `bill-extraction/2` 自然 JSON 契约首次发布，没有原地修改 v3，也没有修改原始像素。发布器和清单保留为历史证据；当前 Runner 与评分器会拒绝 v4，不能用旧命令使它重新成为调优输入。

```bash
node tools/publish-real-tuning-dataset-v4.mjs \
  --output /tmp/m1-real-dev-v4/manifest.json
```

该命令只允许在受保护临时目录确定性重建，随后应以 `cmp` 和固定 SHA-256 对照冻结版本并删除临时副本。

v4 清单只存在于 Git 忽略、Docker 排除、权限 `0600` 的本地隔离区；其冻结 SHA-256 为 `cd96056be80b4670c7a315ddcdb37dc5f6a015367013be9bd7336a967157c610`。样本边界保持：

- Payment 只接受微信支付或支付宝的单笔完整支付/账单详情页，且状态、金额、业务日期时间和交易对方或商户可见；
- 账单列表、通知卡片、优惠页、扣费凭证、多交易拼图、教程/第三方浮层、打码、模糊涂抹、关键字段裁切和实体小票全部排除；
- Invoice 只接受无遮挡、无教程标注、无多文档拼图的完整真实发票；
- 每份原图必须对应可回溯的真实交易或真实开具行为；图片搜索只用于发现，必须登记原始页面；
- 原图、完整标签、来源定位和原始模型结果只保存在 Git 忽略、Docker 排除、权限 `0600` 的本地隔离区；
- 清单冻结了当时的 Bill Extraction/Provider/Claim/Mapper/输入处理版本、期望最终 Claim、允许的模型归一化、来源类别和图片 SHA-256；
- 预检沿用全部最终指标，并额外要求模型完成率与完整 Claim 契约率 100%。

冻结候选不等于 Provider 授权。任何真实图片调用前，必须重新明确 Provider 主机和样本数量并取得当次授权；旧授权不能复用。

2026-08-29 的 v3 当次授权预检结果为 0 / 16 Provider Schema、0 / 16 Claim：30 个 AiRun 因旧 `{value, source}` Schema 失败，1 个被 Provider 拒绝。该结果只证明旧输出形状失败。

2026-08-30，产品负责人授权后，v4 使用当前阿里云 Qwen3-VL、`json_object`、temperature 0、并发 2 和单次 Schema 重试完成 16 份预检：9 / 16 形成 Claim，9 / 9 通过 v2 Schema，7 份在模型阶段终止，但完整 Claim 契约仍为 0 / 16。结果证明旧包装结构阻塞已移除，同时模型完成率、语义精度和 Evidence 完整性仍不接近门槛。不得复用该次授权、用本地重型解析、响应修复、降低指标或启动 100 样本运行；再次发送真实图片仍需新的明确授权。无字段值聚合证据见 `docs/m1-evidence.md`。

产品负责人随后授权使用智谱 `glm-5.3-flash` 对同一 v4 做单变量 A/B；除 Provider 和模型外，Prompt、Schema、Mapper、输入处理、输出模式、超时、并发和重试均未改变。结果为 11 / 16 Claim、11 / 11 Schema、0 / 16 完整 Claim，5 份全部因 Provider 超时终止；分类、金额、发票号、日期、名称和关键 Evidence 分别为 68.75%、68.75%、16.67%、62.5%、45.45% 和 64%。智谱整体优于 Qwen，但发票号更差且仍不接近门槛，只能作为下一 Prompt 候选，不能直接切换生产或启动 100 样本。该次授权不得复用。

产品负责人随后批准从 v4 选择五份发票，使用同一智谱模型比较当前混合 Prompt、发票专用直出和同模型“视觉转录 -> 语义整理”。当前混合直出完成 3 / 5，发票专用直出完成 1 / 5，同模型两阶段最终完成 0 / 5；全部未完成均为 60 秒超时，成功返回的阶段没有 JSON 或根身份错误。发票专用直出的唯一完成响应通过了全部期望业务值与 Evidence，但四次超时使其不能替换当前 Prompt。同模型两阶段增加了顺序失败面，也不能代表未测试的专门 OCR/Document Parser。诊断工具不持久化原始模型响应，只输出路径级通过/失败和无字段值聚合；限制与完整结果见 `docs/m1-evidence.md`。该次授权同样不得复用。

产品负责人随后要求使用阿里云百炼官方 Key 和 `qwen3.8-flash` 复测相同五份发票。官方能力探针通过，正式诊断显式设置 `enable_thinking=false`；当前混合直出、发票专用直出、视觉转录和两阶段最终输出均 5 / 5 完成。当前混合直出的业务值和全部期望 Evidence 为 90.52%/79.36%，优于发票专用直出的 83.03%/83.03% 以及两阶段业务值 73.88%，但三者完整契约代理均为 0 / 5。两阶段 Evidence 0% 同时受到诊断 Prompt v1 未声明完整 Evidence 元素形状影响，不能用于否定架构。Qwen 当前混合直出无超时且优于同组智谱结果，成为下一 Prompt 候选；它仍未达到正式门槛，该次授权不得复用。

产品负责人再次明确授权继续到取得测试结果后，以 `m1-invoice-prompt-ab/2` 对当前 `bill-extract/2` 与 `bill-extract/3` 候选执行同组五发票 A/B。v2 本轮完成 4 / 5、完整契约 0 / 5；v3 完成 5 / 5、业务值 86.18%、Evidence 84.18%、严格形状 5 / 5、完整契约 2 / 5。v3 将币种 Evidence 失败从 5 次降为 1 次并消除本轮总额失败，但仍有发票号、销售方失败，并在一份样本新增明细金额和税额回退；前一轮 v2 业务值 90.52% 高于本轮 v3。故 v3 不激活、不进入 16 或 100 样本，该次 Provider 授权不得复用。

继续授权后，依据官方参数说明把 `vl_high_resolution_images=true` 与 `presence_penalty=0` 固定为公共配置，再比较 v3/v4。v3 得到 5 / 5 完成、86.18% 业务值、86.18% Evidence、2 / 5 完整契约；v4 虽修复屏摄样本的明细金额/税额，却让两份原本完整样本新增总额错误，完整契约降至 0 / 5。最后的低档思考模式诊断中，v3 只有 4 / 5 完成、0 / 5 完整契约，平均完成耗时增至 18.95 秒。v4 与思考模式均否决；当前模型上的 Prompt 修补停止，任何模型或视觉输入能力变更及真实图片调用都需要新的明确决策与授权。

产品负责人随后授权阿里云 `qwen3.5-ocr` 处理同组五份发票。`m1-invoice-ocr-transcription/2` 不发送 System、自定义 Prompt 或 `response_format`，只使用 Provider 默认纯文字转录，并且不保存原始 OCR 文本。5 / 5 完成，严格 Evidence 原文召回 69.06%，业务值文本召回 81.36%，两项完整召回均为 0 / 5；金额、税额和明细文本全部出现，但发票号、购销方、日期与币种仍有漏项。该结果不足以增加独立 OCR 阶段，活动链路保持不变；本次授权不得复用。

产品负责人随后否决本地 OCR 主链并授权 `m1-invoice-ocr-direct-extraction/1`：同一 `qwen3.5-ocr` 接收五份原图和未经模型专属改写的现行 `bill-extract/2`，直接返回最终 JSON。模型不支持本轮所需的 System/原生结构化输出，因此只发送 User 消息；工具不剥代码围栏、不抽取 JSON 片段、不修复字段，只允许一次相同请求重试。结果 5 / 5 均为 `invalid_json`，0 / 5 进入业务值或 Evidence 评分。该模型不能作为当前纯 AI Provider，已否决；活动链路、契约和门禁保持不变，本次授权不得复用。

产品负责人随后提出中文简单字段清单由本地薄 Assembler 拼成最终 JSON，并要求先测试官方 `qwen3.8-flash`。`m1-invoice-claims-assembly/1` 使用 `bill-claims-cn/1`、当前最佳高分辨率非思考参数和同组五份发票：字段清单及最终根结构均 5 / 5 有效，业务值 86.18%，Evidence 79.03%，完整契约 0 / 5。业务值及失败路径与直出 v3 完全相同，Evidence 因币种路径额外回退而更低；该方案证明薄拼装可行，但没有突破模型视觉语义上限，因此不激活、不再针对 Qwen3.8 修补。本次授权不得复用。

`tools/prepare-image-input-candidate.py` 只用于在 `tests/evaluation/real-local/` 内构造无 OCR、无字段解析的所有者只读图像派生物；必须使用工作区受控 Python/Pillow 运行，输出目录只能首次创建。`m1-invoice-image-input-ab/1` 把原图和候选图交替发送给同一 `qwen3.8-flash + bill-claims-cn/1 + 薄 Assembler`，只持久化路径级安全分数。首个 `document-normalize/3-candidate-a` 将业务值从 84.52% 提到 88.00%、Evidence 从 80.70% 提到 82.52%，但完整契约保持 1 / 5，高分辨率屏摄样本发生 Evidence 回退且输入 token 增加约 29.5%，因此不激活。任何新候选必须使用新身份和首次创建的清单；再次调用 Provider 仍须重新明确主机与五份样本授权。

当前工具只发布可复现诊断用的 `document-normalize/3-candidate-c`：最长边小于 1,600 px 时做 2× Lanczos 和有界反锐化，高分辨率输入生成同页全景与 2×2 重叠原像素分块。`m1-invoice-image-input-ab/3` 对两侧使用同一中性同页视图说明，10 个请求得到业务值 82.18%→86.00%、Evidence 76.70%→84.18%、完整契约 1 / 5→2 / 5；但高分辨率样本只修复币种，销售方和困难明细仍未改善，输入 token 增加约 42.7%。candidate-c 已冻结为验证后不采用的边界证据；candidate-b 仍只作为低分辨率开发基线，两者都不是活动输入或发布证据，再次调用 Provider 仍须重新授权。

`m1-invoice-model-ab/1` 使用完全相同的五份原图、Prompt、输入协议、薄 Assembler 和 Provider 参数，交替比较 `qwen3.8-flash` 与 `qwen3.8-max`。两侧均 5 / 5 完成且输入 token 相同；业务值为 86.18%/86.52%，Evidence 为 79.03%/85.03%，完整契约均为 0 / 5。Max 明显改善屏摄明细，却在三份普通发票上统一漏掉非零税额，逐样本改善 1、不变 2、回退 2，平均耗时约为 Flash 的 2.88 倍。因此 Max 只冻结为模型候选，不是活动模型或发布证据；再次调用 Provider 仍须重新授权。

`m1-invoice-original-model/1` 随后只通过 Shuai 资格测试 `gpt-5.6-sol + medium`，不再通过 Shuai 调用千问。该 Key 可见目标模型，但 Chat Completions 返回 502，故按实际可用能力使用 Responses：同一五份原图、`bill-claims-cn/1`、同页说明和薄 Assembler，固定 `detail=high`、`store=false`、8,192 输出 token 上限与 120 秒超时。Shuai 不接受 Responses `json_object`，返回文本因此只做严格 `JSON.parse`，不做任何响应修复。5 / 5 完成且根结构合法，业务值/Evidence 均为 69.70%，完整契约 0 / 5，平均耗时 64.1 秒；困难屏摄样本只有 30%/30%。该结果明显低于阿里云官方 Flash/Max，只否决本次 Shuai 路由与冻结配置，不是严格单变量 A/B 或 OpenAI 官方直连结论，也不得扩大到 16、100 样本或 M2。

`m1-invoice-minimal-contract-ab/1` 随后使用阿里云官方 `qwen3.8-flash`、同一五份原图和完全相同的冻结参数，逐样本交替比较当前 `bill-extract/2` 与只返回发票业务值的 `invoice-values-cn/1`。两侧均 5 / 5 完成且输出形状合法；完整契约/最小候选业务值为 88.52%/86.85%，逐样本 4 份持平、1 份候选回退；候选只把平均耗时从 14.81 秒降至 3.53 秒、输出 token 从 4,872 降至 935。随后按要求进行的不落盘人工检查发现，当前总额语义、数量数值等价、可见括号和币种 Evidence 片段规则会制造假失败；另有发票号、名称单字与空白单价等真实模型错误。因此候选不激活，但当前分数也不能直接用于模型归因；必须先本地修正评测定义与标签，再决定下一模型动作。再次调用 Provider 仍须重新授权。
