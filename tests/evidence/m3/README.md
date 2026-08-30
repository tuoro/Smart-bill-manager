# M3 证据索引

- `email-archive-gate-summary.json`：邮箱 Source、邮件与附件本地归档首切片的可机读验收摘要。
- `trip-attribution-gate-summary.json`：行程 Fact 与确定性单据归属第二切片的可机读验收摘要。
- `reimbursement-workflow-gate-summary.json`：报销快照、状态历史与确定性政策提示第三切片的可机读验收摘要。
- `m3-closure-gate-summary.json`：M3 三个切片的最终收口摘要。

完整范围、实现说明、命令结果、排除项和下一断点见 `docs/m3-evidence.md`。各切片保留执行时的门禁分母与场景数，M3 最终状态以收口摘要为准。
