# ADR-0003：第一阶段使用唯一 OpenAI-compatible Adapter

状态：已接受
日期：2026-08-27

## 背景

千问、DeepSeek、OpenAI、Ollama 和其他服务通常提供不同程度的 OpenAI 兼容接口。为每个品牌建立适配器会把供应商差异扩散到业务层，但“接口兼容”也不能被误认为“能力相同”。

## 决策

- 业务层只依赖 `BillExtractor`；模型返回完整账单业务 JSON，不返回内部 Claim 或 Fact。
- 第一阶段唯一传输实现是 `OpenAICompatibleAdapter`。
- 使用 Chat Completions 兼容端点，不依赖 Responses API、Tools 或供应商专属参数。
- 用户配置 Base URL、API Key 和 Model；M1 的连接超时固定 10 秒、单次模型请求超时固定 60 秒，总 Job 期限固定 150 秒，不提供租户级 timeout 覆盖。
- 保存配置前真实检测认证、模型、图片输入、结构化输出、固定超时和错误能力；Base URL、API Key 或 Model 任一变化都使旧检测失效。
- Provider-facing Schema 采用 ADR-0004 的确定性投影；本地 Bill Extraction Schema、薄 Claim Mapper、Claim Schema 和业务校验始终执行。

## 结果

- 通过能力检测的兼容服务无需修改业务代码；
- 供应商品牌不成为业务分支；
- 兼容差异在能力检测和稳定错误类型中显式暴露。

## 非目标

- 多模型路由；
- 自动故障转移；
- 供应商专属功能；
- 模型投票；
- 本地 OCR fallback。
