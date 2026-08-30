# ADR-0005：以 Claim Assembler 作为 Observation 到 Claim 的唯一边界

状态：已被 ADR-0006 取代；不再存在可执行实现或兼容入口
日期：2026-08-29

## 背景

Prompt v2–v10 让视觉模型直接生成 `document-claim/1` 完整快照。模型不仅要看图，还要承担金额最小单位换算、日期和时区规范化、固定路径、类型、present/absent、Evidence 包装、缺失/冲突规则及发票明细排序。这些任务本可确定性完成，却导致长输出、Schema 失败和反复 Provider 适配。

M1 仍需要同一最终 Claim、人工审核边界和质量指标，但不需要模型成为业务规则执行器。

## 决策

处理链固定为：

`Source -> DocumentObserver -> document-observation/1 -> claim-assembler/1 -> document-claim/1 -> ValidateClaim -> Claim -> Review -> Fact`

职责只有一个归属：

| 职责 | 唯一归属 |
| --- | --- |
| 文档类型、语义字段、可见原文、页码、可选区域和明细序号 | DocumentObserver / 模型 |
| JSON 和 Observation 结构验证 | OpenAI-compatible Adapter |
| Claim path、value type、完整快照和 absent 墓碑 | Claim Assembler |
| 金额、日期、时间、数量和名称的确定性解析 | Claim Assembler |
| `Asia/Shanghai` 默认时区、`source_timezone` 和明细 `sort_order` | Claim Assembler |
| 重复观察合并、冲突、缺失、歧义和非法绑定 | Claim Assembler |
| Claim Schema 与业务不变量 | `document-claim/1` / `ValidateClaim` |
| Fact 创建 | 用户确认后的 Review 用例 |

具体约束：

- Assembler 不访问网络、不读取 ProviderConfig，也不按模型或供应商品牌分支；相同 Observation 必须得到逐字节等价的 Claim。
- 模型遗漏字段时不要求输出 absent；Assembler 按文档类型生成完整固定快照。`unknown` 永远不携带字段。
- Observation 的 `raw_text` 原样成为 Evidence quote；确定性规范化不改写证据。相同值的不同证据最多保留 8 条，超限或绑定非法时阻断。
- 同一路径的不同可解析值视为冲突并输出 absent；无法安全解析的值输出 absent。两者都形成结构化阻断原因，不触发模型重试。
- 货币只在显式币种或无歧义符号下确定；裸 `¥` 不推断 CNY。支付界面的正负号只作为收支方向显示，金额 Claim 保存非负绝对最小单位。币种缺失时，只有恰好两位小数的支付金额可以独立换算为最小单位，币种仍为 absent 并阻断；整数金额不得猜测币种精度。
- 中文支付存在完整本地日期时间且无冲突时，以批准默认值 `Asia/Shanghai` 生成 RFC3339 时间和 `source_timezone`；两者证据锚定原始时间 Observation，不伪造时区文字。
- InvoiceItem 以模型报告的 `item_index` 建组；索引必须从 0 连续。Assembler 生成 `sort_order`，Worker 随后分配稳定 item key。模型不得输出内部路径或排序值。
- AiRun 同时审计 Prompt、Observation Schema、Provider Schema 与 SHA-256、Claim Schema、Claim Assembler 和输入规范化版本。
- 只有 JSON、Provider-facing Schema 或完整 Observation Schema 失败可按既有策略重试一次。Assembler 和 Claim 业务校验失败形成一个可审核的 blocked Claim，不向 Provider 回传错误，也不重试模型。

## 结果

- 最终 `document-claim/1`、人工确认边界和已批准质量指标保持不变。
- Prompt 只描述视觉观察契约，业务规则不再在 Prompt、Adapter 和领域校验之间复制。
- 结构失败与业务不确定性分离：前者是 Provider/AiRun 失败，后者是可审计的 blocked Claim。
- 直接 Claim Provider 链路被移除，不保留兼容分支或双实现。

## 非目标

- OCR 第二数据源；
- 模糊名称匹配或概率置信度；
- 自动修复非法模型 JSON；
- 自动确认或创建 Fact；
- M2 的复杂跨页明细重建。
