# ADR-0004：分离 Provider 传输 Schema 与完整本地业务 Schema

状态：已被 ADR-0007 的根投影与字段级校验边界替代（保留历史背景）
日期：2026-08-28

## 背景

OpenAI-compatible Provider 对 Structured Outputs 支持不同的 JSON Schema 子集。兼容层需要解决结构传输差异，但不能成为第二套账单业务规则，也不能按供应商或模型建立修补分支。

## 决策

- `contracts/schemas/bill-extraction.schema.json` 的 `bill-extraction/1` 是模型输出的唯一权威本地 Schema。
- 唯一 `OpenAICompatibleAdapter` 从该 Schema 确定性投影 `bill-extraction-provider/1`，禁止供应商品牌或模型名称分支。
- ProviderConfig 显式选择不可变 `output_mode`：`json_schema` 或 `json_object`。模式进入安全配置指纹、能力检测和评测冻结配置；失败后不得自动切换。
- 投影保留类型、对象、数组、枚举、常量、引用、必填键和封闭对象边界，只移除由本地完整 Schema 再次执行的格式、长度、范围、条件、模式与唯一性约束。
- 模型面对的每个业务键都在 Schema 中显式必填；未知值使用 `null`。`source.region` 同样必填且可为 `null`，因此不需要传输层哨兵修复。
- Provider 原始响应先通过当前 Provider-facing Schema，再原样通过完整 `bill-extraction/1`。Adapter 不删除 `null`、补默认值、改字段名、删除数组元素、宽松解析或修复非法输出。
- `document-claim/1` 继续作为本地 Claim 的权威 Schema，但不投影给 Provider。
- Provider-facing Schema 的版本和 SHA-256 写入能力检测、ProviderConfig 检测身份、AiRun 与评测冻结配置；身份变化强制重新检测。
- 旧 Provider Claim、Observation 与相关投影只属于冻结历史证据，不存在可执行兼容分支。

## 结果

- 模型直接输出自然的 Payment/Invoice 业务对象、typed value、来源与发票明细分组。
- Provider 差异不会改变业务字段含义；忽略结构约束的响应由本地 Schema 明确拒绝。
- 结构失败会真实降低评测结果，不通过隐藏修复掩盖。

## 非目标

- 供应商专属 Schema；
- 按错误动态删减 Schema；
- 自动回退到其他输出模式、模型或 Provider；
- 对非法模型 JSON 补值、猜测或宽松解析。
