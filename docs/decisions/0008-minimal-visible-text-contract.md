# ADR-0008：最小票面原文契约与确定性 Claim 组装

状态：已接受
日期：2026-08-30

## 背景

ADR-0007 让模型直接输出归一化业务标量、独立 Evidence、问题和额外字段。真实发票诊断随后暴露出两类问题：

- 模型需要同时识别图片、理解字段、选择归一化表示并重复生成 Evidence，输出负担大且容易漏项；
- `total` 没有区分含税与不含税，数量 `1` / `1.0`、可见外层括号及币种 `¥` 的充分 Evidence 又被评分器误判。

不落盘人工核对同时确认仍存在真实视觉错误，例如发票号增删零、名称单字误识别和计算票面空白单价。因此不能把失败全部归因于模型，也不能继续靠增加 Prompt 规则修补。

## 决策

唯一处理链更新为：

`Source -> BillExtractor -> bill-visible-text/1 -> claim-mapper/3 -> document-claim/2 -> ValidateClaim -> Review -> Fact`

| 职责 | 唯一归属 |
| --- | --- |
| 文档类型、字段语义和发票行分组 | 视觉语言模型 |
| 需要字段的票面原文和一基页码 | 视觉语言模型 |
| 有效 JSON、封闭根身份 | `bill-visible-text/1` / Adapter |
| Evidence quote/page | `claim-mapper/3` 从同一 `{text,page}` 机械生成 |
| 币种、金额、日期、时间、时区和数量表示法 | `claim-mapper/3` 的确定性规则 |
| Claim 完整性、业务不变量与 Fact 门禁 | `document-claim/2` / `ValidateClaim` |
| Fact 创建 | 用户确认后的 Review 用例 |

具体约束：

- 每个非空模型字段只能是 `{"text":"票面值原文","page":1}`；看不到或不能确定时返回 `null`。
- 模型不得输出内部 Claim、minor units、独立 Evidence、置信度、问题码、解释或计算值。
- Payment 与 Invoice 使用固定字段；发票金额明确拆为 `amount_without_tax`、`tax_amount` 和 `amount_with_tax`。
- 只有 `amount_with_tax` 映射为 `total_minor`；`amount_without_tax` 进入审核专用补充字段。缺失金额、税额或单价不得通过算术补齐。
- Mapper 可以执行批准的纯表示法转换，但不能改写发票号、名称、商户、订单号或明细文本。
- 中文支付未显示时区时使用产品默认 `Asia/Shanghai`，不伪造时区 Evidence；显示的时区必须与交易时间偏移一致。
- 局部字段形状、页码、金额、日期、时区或数量错误形成 blocked Validation，并保留其他正确字段。
- 评分按金额 minor units、十进制数量语义和确定性文本规范化比较；Evidence 采用页码一致的最小充分语义，不要求重放冻结长片段。

## 结果

- 模型请求收敛为中文短任务，模型只负责它擅长的视觉语义选择与票面复制。
- Evidence 不再要求模型重复生成，字段值和证据天然来自同一个票面字符串。
- 含税总额语义不再含糊，空白单价计算会被契约和本地校验显式阻断。
- Provider 仍只有一个 OpenAI-compatible Adapter、两个显式输出模式和一个确定性 Schema 投影。
- ADR-0007 的自然标量、独立 `evidence` / `other_fields` / `issues` 与 `claim-mapper/2` 全部退出活动链路，不保留兼容分支。
- `m1-real-dev-v4` 冻结为旧契约历史证据；任何新真实预检必须首次发布 `m1-real-dev-v5` 并重新授权。

## 非目标

- 本地 OCR、模板 OCR、版面规则或 Provider 品牌分支；
- 用 Evidence 或字段间算术纠正模型字符；
- 降低现有质量、安全、隐私、人工审核或 Fact 门禁；
- 在本决策中执行新的真实 Provider 调用、16 份预检、100 样本评测或进入 M2。
