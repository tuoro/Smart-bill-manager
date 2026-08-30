# M2 首切片验收证据

状态：通过；支付—发票金额分配首切片已完成，M2 整体仍在进行中

更新日期：2026-08-30

## 冻结范围

本切片以 `docs/decisions/0009-payment-invoice-allocation.md` 替换 M1 的全额一对一活动规则，未保留运行时兼容分支或旧数据迁移：

- 候选由同租户、相反类型、同币种、日期相差不超过 30 天且目标仍有正余额决定；金额完全相等只形成原因代码。
- 用户在审核时提交 `allocate_candidates`、`reject_all` 或 `no_candidate`；分配计划由唯一候选 ID 和正整数最小单位金额组成。
- 一个确认可形成一对多或多对一的不可变 PaymentInvoiceLink；同一对同时最多一条活动 Link，双方活动分配合计不得超过各自总额。
- 规范排序后的完整计划 SHA-256 进入幂等身份；重排重放返回相同结果，改变候选或金额产生冲突。
- 列表余额由活动 Link 唯一聚合为 `allocated_minor`、`remaining_minor` 和 `unallocated | partial | allocated`，不维护第二份余额。
- 删除 Fact 终止其全部活动 Link 并恢复另一端派生余额；首切片不提供确认后的独立补分配、撤销或替换入口。

## 实现证据

- `apps/api/internal/domain/allocation.go` 负责规范化、计划哈希及候选、币种、金额和余额校验。
- 应用确认事务重新校验目标余额，并原子创建 Fact、ReviewDecision、完整候选决定集、全部 Link、AuditEvent 与 Job 终态；任一冲突不留下部分写入。
- `infra/migrations/0001_initial.sql` 直接定义 Clean Slate 金额 Link、活动对唯一约束及双方预算触发器，没有兼容迁移。
- `contracts/openapi/openapi.yaml` 与生成客户端公开多候选分配请求、全部 Link ID、候选余额和 Fact 分配投影。
- 审核工作台提供多候选选择、逐项金额输入、实时分配合计与剩余金额；Payment 与 Invoice 列表显示派生分配状态。
- 关键不变量矩阵已经用一对多、多对一和最后余额并发分支替换旧一对一分支。

## 已执行验收

- 固定 Go 1.26.7 容器：`go test -p=1 -timeout 60s ./...`、`go vet ./...`、`go build -buildvcs=false ./...` 全部通过。
- Web：`npm run check` 通过契约生成、TypeScript、ESLint、Prettier、Vitest 和生产构建；3 个测试文件共 10 个测试通过。
- 浏览器验收：`apps/web/e2e/m1-state-matrix.spec.ts` 10 / 10 通过，其中包含选择两个候选、提交两项金额并核对两个 Link ID 的真实组件场景。
- 关键不变量：43 / 43（100%）通过，覆盖一对多、多对一、部分余额、计划形状与余额校验、计划幂等、非法或超额分配、活动对唯一、Link 不可变、候选删除、跨租户/跨币种、并发最后余额、无部分写入和删除恢复。
- 领域/应用层语句覆盖率 86.70%（1,656 / 1,910，门槛 85%）；基础设施/传输层 72.70%（1,917 / 2,637，门槛 70%）。
- 最终工作区 `git diff --check`、精确暂存后的 `git diff --cached --check`、证据 JSON 解析和新增文档格式检查均通过；374 个暂存文件中最大对象为 521,163 字节，已知凭据前缀和私有资产路径检查无命中。机读摘要见 `tests/evidence/m2/gate-summary.json`。

## 边界与下一断点

- 本轮未调用真实 Provider，未处理模型正确率，未安装或恢复本地 OCR、RapidOCR、PaddleOCR 或模型缓存。
- 本轮未实施近似/字段组合/跨页重复检测、复杂跨页明细重建、批量上传或独立 Link 调整工作流。
- 门禁记录时尚未部署、提交或推送；本文件随获授权的本地里程碑提交固化后，以包含本文件的本地 Git commit 为事实来源，仍不得推送或部署。
- 下一开发应先冻结 M2 第二切片的范围与验收。按当前路线图顺序，从近似文件、字段组合和跨页重复检测开始；模型正确率继续保留到全部功能完成后的 M4 门禁。
