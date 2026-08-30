# ADR-0011：跨页发票明细与分页审核采用证据派生计划

状态：已接受
日期：2026-08-30

## 背景

M1 已把 PDF 按原始页序规范化为 1–20 张页面图片，并在一次多模态请求中发送全部页面。当前 `bill-visible-text-cn/1` 让模型负责文档类型、字段语义、发票明细分组与阅读顺序；每个非空字段只返回票面 `{text,page}`。`claim-mapper/3` 再把同一原文机械绑定为 Evidence，并分配稳定 `item_key`。

这条链已经具备跨页明细的唯一可信输入：同一个模型明细对象中的各字段可以分别引用不同页面。缺失的是确定性的页序边界、可审核分页投影和逐页原件入口。若再增加 OCR、表格解析器、模糊文字合并或另一套持久化明细页表，会形成第二数据源并重新解释模型结果。

## 决策

第三切片保持活动 AI 契约和版本不变：

`bill-visible-text-cn/1 -> bill-visible-text/1 -> claim-mapper/3 -> document-claim/2`

模型仍一次接收全部原始规范化页面，按阅读顺序返回每个逻辑明细一次；重复表头不是明细。模型把属于同一逻辑明细、但位于相邻页面的可见字段放在同一个 item 对象中，并为每个字段保留自己的页码。本地不拼接文字、不计算空白字段、不重新识别表格，也不改变模型的明细分组。

### 确定性页计划

当前 Claim revision 的分页计划只从以下权威链实时派生：

`FieldClaim.field_path -> Evidence.document_page_id -> DocumentPage.page_number`

计划不落入新的业务表，也不成为 Fact 来源。它包含：

- Document 的完整 `1..page_count` 页面序列；
- 每页具有 Evidence 的字段路径与稳定 `item_key`；
- 每个 InvoiceItem 的 `sort_order`、有序去重页码、起止页及是否跨页。

保存用户 revision 后必须从新 revision 的 Evidence 重新计算，禁止复制或写回旧计划。API 与 Web 只消费该派生结果，不能在客户端另算一套业务结论。

### 不变量与失败边界

- InvoiceItem 的 `sort_order` 必须唯一且严格为 `0..n-1`；稳定身份仍只使用 `item_key`。
- 一个明细引用多页时，页码集合必须连续；页码跳跃形成 `invoice_item_page_gap` blocked Validation。
- 按 `sort_order` 阅读时，后一个明细的起始页不得早于前一个明细的结束页；倒退形成 `invoice_item_page_order_conflict` blocked Validation。
- 独立明细可以分布在不同页面；同一模型 item 的字段引用相邻多页时，系统保留为一个稳定明细并显示跨页标记。
- 所有 Evidence 页码继续受 Document `page_count` 约束；PDF 渲染页序是唯一页面顺序，不接受客户端或模型另行重排。
- 全部明细金额继续参与现有发票总额校验；页小计、重复表头或重复行若被错误当作明细，会因缺少必填字段、排序/页序或合计冲突而阻断，系统不自动删除或合并。
- 若同一业务字段本身被拆成多段且模型不能安全返回一个完整票面值，该字段必须为 `null` 并进入人工修订；本切片不猜测连接字符或跨页拼接文本。

### 分页原件与审核

新增租户隔离的规范化页读取入口。服务端只按 `(tenant_id, document_id, page_number)` 查询现有 `document_pages`，再从 ObjectStore 读取对应 PNG；越权与不存在保持相同的 404 边界，reviewer 仍只可读取处于待审核或阻断状态的 Source。

Review API 返回 `page_count`、每页派生摘要和 InvoiceItem 页跨度。Web 使用规范化单页图进行上一页、下一页和直接页码导航；选择字段时定位其首条 Evidence 页，分页字段区只筛选当前页相关明细，同时始终保留文档级字段和无定位的阻断明细。跨页明细在相关页面重复显示同一稳定 `item_key`，不得复制成多个编辑对象。

## 结果

- 复杂多页 PDF 获得可测试的跨页明细拓扑与分页审核体验，而模型仍只做一次直接多模态提取。
- Source、Claim、Evidence 和 Fact 的唯一来源关系不变；分页计划是可丢弃、可重建的读取投影。
- 用户修订可以为字段选择同一 Document 不同页面的既有 Evidence，并在新 revision 上重新接受全部确定性校验。
- 不调用真实 Provider，不改变模型正确率结论，也不为低清或网络样本增加 Prompt、预处理或 OCR 补丁。

## 非目标

- OCR、表格检测、版面模板、文字相似度、模型二次调用或供应商分支；
- 对被裁切、遮挡、低清或屏摄内容进行恢复；
- 自动删除重复表头、自动合并模型分成两个 item 的行，或自动拆分模型合成的 item；
- 多 Document 批量上传、独立 Link 调整、M3 邮箱能力或真实模型正式评测。
