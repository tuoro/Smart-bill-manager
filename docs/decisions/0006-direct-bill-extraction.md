# ADR-0006：模型直接输出账单业务 JSON，本地只保留薄 Claim Mapper

状态：已被 ADR-0007 替代（保留历史背景）
日期：2026-08-29

## 背景

旧 Observation + Claim Assembler 方案让模型只返回可见文本，本地再负责金额、日期、时区、字段语义、冲突和发票行组装。它实质上把视觉模型降级为 OCR，并要求应用维护一套专门账单解析服务，偏离“AI 按指定 JSON 提取完整账单”的产品目标。

模型应承担图片理解和账单语义，应用只应守住结构、精度、审核与数据完整性边界。

## 决策

唯一处理链为：

`Source -> BillExtractor -> bill-extraction/1 -> claim-mapper/1 -> document-claim/1 -> ValidateClaim -> Review -> Fact`

| 职责 | 唯一归属 |
| --- | --- |
| 文档类型、账单字段含义和最终业务值 | 视觉语言模型 |
| 日期、时间、时区、名称和币种归一化 | 视觉语言模型 |
| 发票明细行识别、字段归属和数组顺序 | 视觉语言模型 |
| 可见原文、页码和可选区域来源 | 视觉语言模型 |
| JSON 与 Bill Extraction Schema 验证 | OpenAI-compatible Adapter |
| 业务键到内部 Claim path、null 到 absent | Claim Mapper |
| 来源到 Evidence、数组顺序到 `sort_order` | Claim Mapper |
| 精确十进制金额到内部最小单位整数 | Claim Mapper |
| Claim Schema 与业务不变量 | `document-claim/1` / `ValidateClaim` |
| Fact 创建 | 用户确认后的 Review 用例 |

具体约束：

- 模型输出完整、封闭的 `bill-extraction/1`。每个声明字段都存在，不能安全确定时返回 `null`，不得猜测。
- 金额字段对模型使用主币单位十进制字符串，如 `"28.80"`。禁止 JSON 浮点数和 `amount_minor` 等内部编码。
- Mapper 仅按已提取币种调用精确金额转换；它不读取 `source.raw_text` 解析金额，不默认币种，不舍入或截断。
- 中文支付缺少显式时区但本地时间语义明确时，模型按批准默认值 `Asia/Shanghai` 输出 RFC3339 时间及 `source_timezone`。
- Mapper 不访问网络、ProviderConfig 或存储，不解析 OCR 原文，不推断/补齐/纠正业务值，不合并冲突，不重组明细。
- 模型报告的文档级冲突、缺失或歧义在最终校验中形成一个可审核的 blocked Claim；Mapper/业务校验失败不回传模型也不自动重试。
- AiRun 审计 Prompt、Bill Extraction Schema、Provider Schema 与 SHA-256、Claim Schema、Claim Mapper 和输入规范化版本。
- 模型、Adapter 与 Mapper 均不能创建 Fact。

## 结果

- AI 保留账单理解能力，应用不再建设第二套账单 OCR 语义引擎。
- 模型输出保持自然业务结构；数据库金额仍使用整数，兼顾易理解与精确存储。
- Schema 错误、模型语义错误与本地业务阻断各自可观测，不靠 Provider 特例修补。
- ADR-0005 的 Observation 和重型 Claim Assembler 实现被删除，不保留兼容分支。

## 非目标

- OCR 第二数据源或云 OCR fallback；
- 按模型品牌维护字段解析规则；
- 自动修复模型业务值；
- 自动确认或创建 Fact；
- 降低既有模型质量、安全或审核门禁。
