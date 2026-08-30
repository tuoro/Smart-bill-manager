# M2 证据索引

- `gate-summary.json`：支付—发票金额分配首切片的可机读验收摘要；
- `duplicate-detection-gate-summary.json`：确定性重复检测第二切片的可机读验收摘要；
- `cross-page-review-gate-summary.json`：复杂多页 PDF 跨页明细与分页审核第三切片的可机读验收摘要。
- `batch-upload-gate-summary.json`：多 Document 批量上传与逐项反馈第四切片的可机读验收摘要；
- `allocation-adjustment-gate-summary.json`：已确认 Fact 独立分配调整第五切片的可机读验收摘要；
- `m2-closure-gate-summary.json`：M2 五切片完成状态与最终全量门禁的收口摘要。

完整范围、实现说明、命令结果、排除项和下一断点见 `docs/m2-evidence.md`。历史切片摘要保留其各自执行时的分母和场景数；M2 当前最终状态以收口摘要为准。
