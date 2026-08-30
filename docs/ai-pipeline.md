# AI 票面原文提取管线

状态：M1 当前活动基线

当前契约：`bill-visible-text-cn/1` / `bill-visible-text/1` / `bill-visible-text-provider/1` / `claim-mapper/3` / `document-claim/2`

## 唯一活动链路

`Source -> Normalize -> BillExtractor -> Visible Text -> Claim Mapper -> Claim Validation -> Review -> Fact`

- 视觉模型负责判断 `payment`、`invoice` 或 `unknown`，选择约定业务字段，并逐字复制字段值及一基页码。
- 模型不输出内部 Claim、归一化值、minor units、独立 Evidence、置信度、问题码或审核结论，也不计算图片中的空白值。
- `claim-mapper/3` 把同一个 `text` 作为 Evidence quote，在本地确定性处理币种、金额、日期、交易时间、时区和数量，再组装完整 `document-claim/2`。
- 本地转换只改变表示法，不改变字符所表达的业务值；不得纠正发票号、名称或明细文本，不得计算缺失总额、税额、单价或数量。
- 只有用户确认 Review 后才能创建 Fact；模型、Adapter 和 Mapper 都没有 Fact 写权限。

旧 `bill-extraction/2`、独立 `evidence` / `other_fields` / `issues` 模型输出、自然 typed 标量和 `claim-mapper/2` 已退出活动链路，不保留兼容分支。

## 输入边界

- 支持 JPEG、PNG、WebP 和 PDF；声明 MIME、文件签名和扩展名必须一致。
- PDF 在本地规范化为逐页 PNG；1～20 页按连续一基页码一次性发送。
- 原文件和规范化页保持不可变；工作区隔离与哈希校验在调用模型前完成。
- 图片内容属于不可信数据，不能改变任务、触发工具、访问链接或产生副作用。

## 模型输出：`bill-visible-text/1`

根对象只允许四个键：

```json
{
  "schema_version": "bill-visible-text/1",
  "document_type": "payment",
  "payment": {
    "amount": { "text": "¥28.80", "page": 1 },
    "currency": { "text": "¥", "page": 1 },
    "merchant": { "text": "星河科技有限公司", "page": 1 },
    "transaction_time": { "text": "2026年8月29日 14:35", "page": 1 },
    "timezone": null,
    "payment_method": { "text": "微信支付", "page": 1 },
    "order_number": null,
    "category": null
  },
  "invoice": null
}
```

每个非空业务字段只能是 `{"text":"票面值原文","page":1}`。没有看到或不能确定时必须返回 `null`，不能返回裸字符串、JSON 数字、标签加值、归一化值或推导值。

发票区段使用无歧义金额键：

```json
{
  "schema_version": "bill-visible-text/1",
  "document_type": "invoice",
  "payment": null,
  "invoice": {
    "invoice_number": { "text": "00012345", "page": 1 },
    "invoice_date": { "text": "2026年08月29日", "page": 1 },
    "amount_without_tax": { "text": "￥100.00", "page": 1 },
    "tax_amount": { "text": "￥6.00", "page": 1 },
    "amount_with_tax": { "text": "￥106.00", "page": 1 },
    "currency": { "text": "￥", "page": 1 },
    "seller_name": { "text": "销售方有限公司", "page": 1 },
    "buyer_name": { "text": "购买方有限公司", "page": 1 },
    "items": [
      {
        "name": { "text": "软件服务", "page": 1 },
        "quantity": { "text": "1.0", "page": 1 },
        "unit": { "text": "项", "page": 1 },
        "unit_price": null,
        "amount": { "text": "100.00", "page": 1 },
        "tax": { "text": "6.00", "page": 1 }
      }
    ]
  }
}
```

`amount_with_tax` 只表示“价税合计（小写）”；`amount_without_tax` 只表示不含税金额。明细保持阅读顺序，空白单价在本例中必须保持 `null`。

## Schema 与 Provider 边界

- `contracts/schemas/bill-visible-text.schema.json` 是模型根 Envelope 的唯一权威 Schema。
- 本地 Schema 关闭根对象并要求 `schema_version`、`document_type`、`payment` 和 `invoice`；嵌套业务值保持开放给逐字段校验，避免一个局部形状错误抹掉同文档其他正确字段。
- `bill-visible-text-provider/1` 从权威 Schema 确定性投影；`json_schema` 模式由 Provider 执行 strict 根约束，`json_object` 模式返回后执行同一权威根校验。
- Provider 品牌或模型名称不能改变 Prompt、Schema、映射或重试策略，也不能触发自动模式切换。
- Adapter 不剥 Markdown、不截取 JSON 片段、不删除键、不改字段名、不补默认值、不修复响应。

只有无效 JSON、错误根版本、错误文档类型、缺少固定根成员或多余根成员会在 Adapter 阶段拒绝。只要根身份有效，嵌套字段错误必须进入一个可审核的 blocked Claim。

## `claim-mapper/3` 的确定性职责

| 模型字段 | 内部 Claim | 本地处理 |
| --- | --- | --- |
| `payment.amount` | `amount_minor` | 结合明确币种精确换算 |
| `payment.currency` | `currency` | 规范为 CNY、USD、EUR 或 JPY |
| `payment.transaction_time` | `transaction_time` | 规范为 RFC3339 |
| `payment.timezone` | `source_timezone` | 明确 IANA 时区；未显示时使用产品默认 |
| `invoice.amount_with_tax` | `total_minor` | 只接受票面含税合计 |
| `invoice.tax_amount` | `tax_minor` | 精确换算，不反推 |
| `invoice.amount_without_tax` | `supplementary_fields` | 仅供审核，不成为 Fact 字段 |
| `invoice.items[*]` | `items[*]` | 保持数组顺序并生成无 Evidence 的 `sort_order` |
| 任一 `{text,page}` | 字段 Evidence | `quote = text`，`page = page` |

批准的表示法处理：

- Unicode NFKC、首尾空白清理；
- CNY/RMB/人民币/元/¥、USD/美元/$、EUR/欧元/€、JPY/日元/円到批准币种；
- 普通小数、合规千分位及常见点号/逗号金额表示到精确 minor units，全程不经过浮点；
- `YYYY-MM-DD`、`YYYY/MM/DD`、`YYYY.MM.DD` 与中文年月日到 ISO 日期；
- 完整本地日期时间到 RFC3339；图片未显示时区时，中文 M1 场景使用 `Asia/Shanghai` 与 `+08:00`，且不伪造时区 Evidence；
- 数量只接受非负普通十进制并去除无意义尾零，因此 `1` 与 `1.0` 语义等价。

禁止的本地行为：

- 改写发票号、购销方、商户、明细名称或订单号字符；
- 从 Evidence 标签、其他字段或算术关系补造缺失业务值；
- 把 `amount_without_tax` 当成 `total_minor`；
- 从数量和金额计算空白单价；
- 用模糊相似度或 Provider 专属规则接受不确定值。

## Validation 与重试

- 非法 `{text,page}`、页码越界、币种未知、金额超精度、日期非法、时区冲突和数量非法都形成字段级 blocked Validation；原始局部值仍保留供审核。
- 缺少必填字段、业务区段冲突、发票算术冲突和 Evidence 缺失继续由 `document-claim/2` 校验。
- 429、明确临时 5xx、网络中断以及 JSON/根 Schema 失败可按 `schema_validation_single_retry/1` 原样重试一次。
- 字段级格式或业务失败不自动重试；重试不得改变图片、Prompt、Schema、模型或参数。
- 每次 attempt 独立追加 AiRun，并冻结 ProviderConfig 安全指纹、模型、输出模式、全部契约版本、Provider Schema SHA-256、输入处理版本、token、耗时和安全失败类别。

## 评分语义

- 金额在精确 minor units 上比较。
- 数量按普通十进制数值比较，`1` 与 `1.0` 等价。
- 文本执行 NFKC、空白与拉丁大小写规范化；仅包住整个值的可见中英文括号不制造失败。
- Evidence 必须页码一致且足以直接支持期望值；冻结长片段不是必须逐字复现。例如 `¥` 足以支持 CNY，`¥68.00` 足以支持长片段“价税合计（小写）：¥68.00”中的金额。
- Evidence 不能只包含一个与期望值不相等的数字片段，也不能用模糊相似度通过。

## 隐私与评测

- 日志、仓库文档和普通测试结果不得包含完整模型响应、原图、真实字段值、API Key 或不必要的个人财务信息。
- 程序化合成资产只验证传输、Schema、Mapper、Validation、评分器和失败路径，不能形成模型业务质量结论。
- `m1-real-dev-v4` 与此前结果冻结为旧 `bill-extraction/2` 历史证据，当前 Runner 明确拒绝发送。
- `m1-real-dev-v5` 已按当前契约发布并完成一次 16 份真实预检，随后因复制自 v4 的标签没有表达“票面不可见币种”边界且漏列固定 `supplementary_fields` Claim 路径而冻结为诊断证据；Runner 现会在读取密钥或联网前拒绝重复发送。
- 下一份真实预检必须先明确不可见币种的产品来源与 Claim 溯源规则，发布修正后的独立冻结集并重新取得 Provider 主机、模型和样本数量授权；v5 分数不得用于 100 份发布门禁。
