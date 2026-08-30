# ADR-0007：自然业务 JSON、独立 Evidence 与字段级 Claim 校验

状态：已被 ADR-0008 替代
日期：2026-08-29

## 背景

ADR-0006 确立了“模型理解账单、本地只保留薄 Mapper”的方向，但 `bill-extraction/1` 仍要求每个业务值输出 `{value, source}`，并在 Adapter 中对全部叶子类型和封闭成员做整份 Schema 校验。真实开发集的 16 份输出全部进入模型阶段，却 0 份通过 Provider Schema；Payment 普遍返回自然的 `amount` 标量，Invoice 也普遍返回自然业务键。失败发生在 Claim Mapper 之前，正确字段与语义质量均无法测量。

该包装结构把内部证据存储形状强加给视觉模型，并让一个局部字段错误淘汰整份账单，与“AI 按指定 JSON 提取完整业务信息”的目标不一致。

## 决策

唯一处理链更新为：

`Source -> BillExtractor -> bill-extraction/2 -> claim-mapper/2 -> document-claim/2 -> ValidateClaim -> Review -> Fact`

| 职责 | 唯一归属 |
| --- | --- |
| 文档类型、字段语义、业务值和发票行分组 | 视觉语言模型 |
| 自然 Payment/Invoice JSON 与额外业务信息 | 视觉语言模型 |
| 可见原文、页码和可选区域 | 独立模型 `evidence` 数组 |
| 有效 JSON、根版本和文档类型 | `bill-extraction/2` / Adapter |
| 业务键到 Claim path、null 到 absent | `claim-mapper/2` |
| Evidence 路径绑定、明细顺序和精确金额换算 | `claim-mapper/2` |
| 局部字段、Evidence、补充字段和业务不变量 | `document-claim/2` / `ValidateClaim` |
| Fact 创建 | 用户确认后的 Review 用例 |

具体约束：

- 模型返回自然标量，禁止 `{value, source}` 逐字段包装。核心字段使用固定业务键，非核心信息进入 `other_fields`。
- `bill-extraction/2` 只硬校验有效 JSON、根对象、`schema_version` 和 `document_type`。局部业务字段不在 Adapter 阶段整份拒绝。
- `json_schema` 模式使用确定性、strict 的 `bill-extraction-provider/2` 根投影；`json_object` 模式返回后直接执行权威根 Schema，不用 strict 投影误杀额外键。
- Mapper 不解析 Evidence 摘录、不推断字段、不补默认币种、不重组明细、不修复响应。它只对模型提供的金额做精确十进制换算；JSON 数字只读取原始十进制词法，不经过浮点。
- 单字段类型、金额、Evidence、业务区段或补充字段错误产生 blocked Validation；同一输出的其他正确字段必须保留并持久化。
- 模型额外键和 `other_fields` 聚合为审核专用 `supplementary_fields`，可修订但不进入 Payment/Invoice Fact。
- `sort_order` 是模型数组顺序的机械映射，`source_timezone` 可以来自批准的产品默认值；两者不要求伪造图片文字证据。
- 只有无效 JSON、错误根身份或 strict 传输失败进入 Schema 重试；字段级错误不重试模型。

## 结果

- 模型输出与常见账单抽取 API 一样保持自然，Evidence 存储形状不再污染业务值。
- 一个坏字段不会抹掉整份账单，系统可以审核正确字段并明确定位阻断项。
- 额外业务信息不会丢失，也不会绕过正式 Fact Schema。
- Provider 仍只有一个 OpenAI-compatible Adapter、两个显式标准输出模式和一个确定性投影，不增加品牌适配层。
- ADR-0006 的产品方向保留，但其 `bill-extraction/1` 逐字段包装与整份叶子 Schema 门禁被本决策替代；不保留运行时兼容分支。

## 非目标

- 保证模型业务值 100% 正确；
- 使用 Evidence 文本重新解析或纠正模型值；
- 建立云 OCR、正则账单解析或 Provider 品牌适配层；
- 把未知补充字段直接写入 Fact；
- 降低现有质量、安全、隐私或人工审核门禁。
