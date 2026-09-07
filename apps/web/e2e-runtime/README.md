# 隔离的真实后端贯通验收

本目录实现 [ADR-0036](../../../docs/decisions/0036-review-allocation-throughflow.md)，不属于默认 `e2e/` Mock API 测试。它会真实创建 Provider、上传三个合成原件、确认 Fact 并保存两次分配，**只能连接本轮新建的独立空环境，不能连接用户实例**。

## 环境准备

- 复用 `infra/compose/compose.yaml` 与 `compose.acceptance.yaml`，使用唯一项目名、新空数据卷和本轮随机生成的保护凭据文件。显式指定本地 Docker endpoint、`--env-file /dev/null`、允许清单环境、已核实的本地镜像 ID；启动禁用构建和拉取。
- 私有覆盖配置将两个网络保持为 `internal`，不发布数据库端口，只发布独立的 `127.0.0.1` 应用端口。模拟 Provider 与 app 共享网络命名空间，监听 `127.0.0.1:19086`，明确选择 `--mode review-allocation`。其模型名必须与下方环境变量一致。
- 按既有 provision、migrate、bootstrap-owner 初始化独立账号；使用当前 Web 的生产构建，只读挂载 `/app/web`。构建需使用清空后的允许清单进程环境和空 `envDir`，不可加载用户 `.env`。
- Provider 提取计数必须从 0 开始。测试只支持顺序的 P=10000、A=6000、B=6000 三张合成单据，不用真实单据，也不调用外部模型。失败后保留安全的失败摘要；重跑必须重建本轮独立空环境，不能复用部分执行后的库或重置计数掩盖额外请求。
- 保留 A/B 的近似版式，连续审核顺序为 B、A、P。A 的旧候选快照明确触发一次 `duplicate_candidate_set_stale`；测试必须核对队列未推进及零部分写入，然后经已有 UI 显式保存未改字段的新版本、核对新增重复提示并确认。不能用宽泛重试或更换图片消除此检查，其他 API 错误仍失败。

## 执行

从 `apps/web/` 运行，显式提供以下变量；密码和 Key 只能通过权限为 `0600` 的文件传递。

| 环境变量                              | 内容                                     |
| ------------------------------------- | ---------------------------------------- |
| `SBM_THROUGHFLOW_URL`                 | 本轮应用的独立 loopback HTTP origin      |
| `SBM_THROUGHFLOW_OWNER_EMAIL`         | 本轮合成 Owner 邮箱                      |
| `SBM_THROUGHFLOW_OWNER_PASSWORD_FILE` | 本轮 Owner 密码保护文件的绝对路径        |
| `SBM_THROUGHFLOW_PROVIDER_KEY_FILE`   | 本轮模拟 Provider Key 保护文件的绝对路径 |
| `SBM_THROUGHFLOW_MODEL`               | 本轮 `synthetic-` 模型名                 |
| `SBM_THROUGHFLOW_OUTPUT_DIR`          | 本轮私有临时输出目录                     |

```sh
node node_modules/@playwright/test/cli.js test --config e2e-runtime/playwright.config.ts
```

使用既有依赖和本地浏览器，单 Worker、无自动重试；未提供变量时必须失败，不能默认连到其他服务。测试不替换 API 响应；保存必须经页面明确操作，重读通过真实接口。自动截图、trace、video 和 DOM 错误上下文关闭，避免凭据进入报告；仅在凭据填写页面结束后生成明确选择的合成场景截图。

## 证据与清理

测试输出只保留安全聚合、合成图片/截图摘要及必要的余额断言。执行者还需从本轮 Provider 核对恰好 3 次提取，从本轮 PostgreSQL 用只读聚合交叉核对 3 个 AiRun、4 个 Claim（含 A 的一次显式修订）、3 条完整确认来源链、1 笔支付、2 张发票、2 条有效 Link 和 2 次分配调整；不能把冲突恢复成功称为首次直接成功，也不能等同于模型识别质量或用户时间收益。

先记录安全摘要和失败历史，再核实唯一项目的资源身份，删除本轮容器、网络和新建数据卷；停止本轮临时转发（如有），删除本轮凭据、图片、构建与原始报告。仓库只保留测试代码和不含凭据、原件内容或原始业务响应的聚合证据。
