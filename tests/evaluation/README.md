# M1 模型评测

本目录保存版本化的纯合成技术评测资产，不读取用户上传、生产数据库、邮箱、日志或旧版本回归数据。它们只用于传输、Schema、评分器和失败路径验证，不再计入当前中文真实场景模型质量门禁。生产镜像必须排除整个 `tests/evaluation/`。

## 历史冻结技术数据集

- 版本：`m1-synthetic-v2`
- 清单：`manifest-v2.json`
- 数量：100 份；Payment 40、Invoice 40、Unknown/模型前拒绝 20
- 主要场景：支付截图 40、单项目发票 20、多项目发票 20、低质量/冲突 20、非法/不支持 15
- 资产：复用 `assets/m1-synthetic-v1/` 下不可变的纯合成二进制文件
- 发布器：`tools/publish-evaluation-dataset-v2.mjs`

清单记录每个样本的 SHA-256、声明 MIME、期望类型、字段、缺失字段、证据、允许规范化、冲突/缺失事件和期望失败类别。修改生成器或清单必须发布新数据集版本，不能原地改写已作为发布证据使用的版本。

### v1 历史缺陷与 v2 修正边界

2026-08-28 的三次诊断复盘发现：40 份 Payment 图片由生成器写入 `ORDER: SYN-PAY-xxxx`，同一冻结清单却把 `order_number` 标为期望缺失。因此 `m1-synthetic-v1` 的 `manifest_assertion_rate` 不能作为最终发布证据；已发布的分类、金额、发票号、日期、名称和关键证据指标不受这个可选字段矛盾影响。

历史诊断和冻结 v1 均保持原样，禁止为消除失败而覆盖。经产品负责人批准，v2 复用完全相同的不可变合成资产，只为 40 份 Payment 补入图片中已经存在的 `order_number` 期望值和证据，并从期望缺失字段中移除该路径；其他样本注释、资产哈希、分布和所有验收指标不变。旧直接 Claim 分析器已随活动链路移除，冻结报告 `tests/evidence/evaluation/diagnostic-analysis-v1.json` 仅作历史证据，不再由当前工具复算。

发布器固定校验 v1 清单哈希且拒绝覆盖既有输出。通过临时输出验证 v2 可确定性再生成，再检查冻结清单：

```bash
node tools/publish-evaluation-dataset-v2.mjs --output /tmp/manifest-v2.json
cmp tests/evaluation/manifest-v2.json /tmp/manifest-v2.json
node tools/check-evaluation-dataset.mjs
```

## 当前合成运行协议

发布模式运行器与评分器只接受冻结的 `m1-synthetic-v2`；v1 只用于历史诊断。该数据集可验证当前软件链路，但不能通过中文真实模型质量门禁，也不能替代未来至少 100 份真实发布集的三次独立运行。

每次运行必须使用全新的空数据库和对象目录，先通过 `bootstrap-owner` 创建合成 owner，再启动生产构建。三个运行必须冻结相同 Provider 安全指纹、Base URL 主机、模型、显式输出模式、`bill-visible-text-cn/1`、`bill-visible-text/1`、`bill-visible-text-provider/1` 与 SHA-256、`document-claim/2`、`claim-mapper/3`、输入处理版本、Provider 输出重试策略和超时配置。密码和 Provider API Key 只放在权限为 `0600` 的文件中，不通过参数值、环境变量或日志传递。

真实样本运行前还必须检查数据库与对象目录所在文件系统的可用字节和 inode；不得默认把运行卷放进容量受限的 `/tmp` tmpfs。优先使用 Git 忽略、Docker 排除、目录 `0700` 的本地隔离卷，运行结束后删除数据库、对象副本、临时 owner/master key 和含真实字段值的明细，只保留无敏感字段的安全聚合。

每个全新环境分别执行一次，`--run-id` 依次使用 `run-1`、`run-2`、`run-3`：

```bash
node tools/run-model-evaluation.mjs \
  --server http://127.0.0.1:8080 \
  --email synthetic-owner@example.test \
  --password-file /protected/owner-password \
  --provider-base-url https://provider.example/v1 \
  --provider-api-key-file /protected/provider-api-key \
  --model frozen-vision-model \
  --output-mode json_schema \
  --run-id run-1 \
  --manifest tests/evaluation/manifest-v2.json \
  --output tests/evidence/evaluation/run-1.json
```

运行器会先断言工作区中没有 Job，然后执行 100 个固定样本；15 个模型前拒绝样本不进入分类分母。它只保存本地 Claim、Validation、证据和安全元数据，不保存完整模型原始响应、密钥或 Cookie，也不会确认 Claim，因此 `ai_direct_fact_count` 必须为 0。为保留 AiRun 追踪证据，运行结束后不删除合成 Document；整个运行环境应当作为一次性评测环境处理。

每次运行结束后，从对应 SQLite 数据库只读导出 Provider 延迟、token 和 AiRun 终态：

```bash
cd apps/api
go run ./cmd/evaluation-report \
  -database /evaluation/run-1/sbm.sqlite \
  -run-result ../../tests/evidence/evaluation/run-1.json \
  -output ../../tests/evidence/evaluation/run-1-provider.json
```

## 评分

单次运行完成后可以生成不冒充三轮总门禁的逐运行分数；该报告明确记录 `release_gate_complete: false`：

```bash
node tools/score-model-evaluation.mjs \
  --manifest tests/evaluation/manifest-v2.json \
  --single-run tests/evidence/evaluation/run-1.json \
  --output tests/evidence/evaluation/run-1-score.json
```

评分器要求三个运行样本完整、配置完全一致，并按三次最差值执行 `docs/acceptance.md` 的门槛：

```bash
node tools/score-model-evaluation.mjs \
  --manifest tests/evaluation/manifest-v2.json \
  --run-1 tests/evidence/evaluation/run-1.json \
  --run-2 tests/evidence/evaluation/run-2.json \
  --run-3 tests/evidence/evaluation/run-3.json \
  --output tests/evidence/evaluation/release-score.json
```

`node tools/score-model-evaluation.mjs --self-test` 只验证评分器逻辑，不能作为模型评测结果或 M1 发布证据。

## 独立调优与预检

旧 `m1-real-dev-v1/v2/v3/v4` 与 Prompt v8/v9/v10 canary 属于已替代契约的冻结历史证据。`m1-real-dev-v5` 已完成一次 `bill-visible-text/1` 真实诊断，但复制标签没有表达不可见币种边界并漏列固定 `supplementary_fields`，现同样被 Runner 阻止重复发送，且不计入发布证据。修正版真实开发集发布并重新取得明确授权前，只能执行不调用 Provider 的纯合成技术自检。隔离、检查和门禁见 `tests/evaluation/tuning/README.md`。

## 诊断结果边界

能力或质量诊断可以完整运行冻结数据集，但只要代码状态随后变化、配置未与发布冻结值一致，或没有完成三次独立运行，就必须使用 `diagnostic-*` 文件名；不得命名为 `run-1`、传给发布评分器或计入 3 次发布运行。诊断失败应原样保留，用于选模、Prompt 调整和根因分析，不能用后续成功结果覆盖。
