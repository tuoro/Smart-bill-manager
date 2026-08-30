# M0 验收证据

状态：通过；M0 已完成，M1 未开始
证据版本：`M0-EVIDENCE-2026-08-27`
验证日期：2026-08-27
时区：Asia/Shanghai

## 证据边界

本文件只证明 M0 的工作区、规范文档、量化指标批准、视觉基线、响应式和可访问性门禁。M0 没有业务运行时，因此没有执行旧后端/前端测试、生产构建、数据库迁移、Lighthouse 或 M1 E2E；这些项目从 M1 起按 `docs/acceptance.md` 执行，不能据此宣称 M1 已开始或通过。

本轮未编写业务代码，未提交、推送、删除遗留代码、修改远端或安装运行时依赖。

## 一、工作区证据

| 项目 | 结果 |
| --- | --- |
| 工作树 | `rebirth/` |
| 独立分支 | `codex/m0-design-baseline` |
| 基线 SHA | `318d379b18e4a0bb3f7dfa3a76d8268925acc3a2` |
| 本地 `main` | 同基线 SHA |
| 本地 `origin/main` 快照 | 同基线 SHA |
| upstream | 无；M0 分支没有远端跟踪分支 |
| 干净起点 | reflog：2026-08-27 03:54:28 +0800 完成 clone，03:54:49 从同一 HEAD 创建并切换 M0 分支 |
| 远端状态 | 本轮没有 fetch/push/PR/Tag/Release；`origin` URL 未改 |

复核命令：

```bash
git rev-parse --show-toplevel
git branch --show-current
git rev-parse HEAD
git rev-parse refs/heads/main
git rev-parse refs/remotes/origin/main
git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}'
git worktree list --porcelain
git reflog --date=iso --format='%gd %h %gs %cd' -n 12 --all
git status --short --branch
git diff --name-only
git ls-files --others --exclude-standard
```

最终范围检查要求变更只位于 `AGENTS.md`、`README.md`、`README_EN.md` 和 `docs/`；`backend-go/`、`frontend/`、Compose、迁移、依赖清单和工作流不得出现在 diff 中。

## 二、量化指标批准

- 批准角色：产品负责人。
- 批准日期：2026-08-27。
- 批准版本：`M0-ACCEPTANCE-2026-08-27`。
- 批准范围：`docs/acceptance.md` 中所有“已批准”数值。
- 例外：无。
- 可执行性：模型评估固定分母、关键字段、规范化、配置冻结和三次最差值；性能固定端点、数据量、预热、并发、样本量和窗口；内存固定 20% 中位数上限与 0.5 MiB/Job 斜率；覆盖率固定包边界与关键不变量分支。

这些测量协议只使批准值可重复执行，没有降低门槛。

## 三、视觉资产与复测入口

| 项目 | 结果 |
| --- | --- |
| 基线版本 | `M0-D02-2026-08-27` |
| 唯一方向 | 02「国内大厂中后台」 |
| 页面 | 登录、AI 收件箱、审核工作台、账单列表 |
| 仓库资产 | `docs/design/m0-d02-four-page-baseline.html` |
| 仓库资产 SHA-256 | `ca5d8fa9c8341e29d5a55c503dea9d74f226ef38af7a6ae9f16d3f52efacd464` |
| 会话源片段 SHA-256 | `ae693d5803123c42f5a2d4d505f15dc3c47c0bbb640fd2e3eb92673c04dba149` |
| 浏览器复核脚本 | `docs/design/m0-browser-check.js` |
| 复核脚本 SHA-256 | `dfd9105b7203766b3ace2f110d65fa8a8fdcf941168e1e9275d6e39c5ca0cfb5` |
| 数据 | 全部为纯合成组织、文件名、金额、日期和标识符 |
| 外部网络资源 | 0；图标为 65 个内联 SVG，未水合图标为 0 |

复核命令：

```bash
sha256sum docs/design/m0-d02-four-page-baseline.html docs/design/m0-browser-check.js
node --check docs/design/m0-browser-check.js
python3 -m http.server 43127 --bind 127.0.0.1 --directory docs/design
```

打开自动复核地址：

```text
http://127.0.0.1:43127/m0-d02-four-page-baseline.html?audit=1
```

包装页在产品 iframe 加载后自动加载同目录 `m0-browser-check.js`。结束时，外层 `<html>` 的 `data-m0-browser-check` 必须为 `pass`，完整 JSON 保存在隐藏节点 `#m0-browser-check-result`；DevTools Console 也可执行 `m0BaselineCheck.staticAudit()`、`m0BaselineCheck.measureViewport()`、`m0BaselineCheck.stateAudit()`、`m0BaselineCheck.contrastAudit()`、`m0BaselineCheck.interactionAudit()` 或 `m0BaselineCheck.runAll()`。

响应式复测时，先把浏览器 CSS 视口设为“目标产品宽度 + 32 px”，再刷新 `?audit=1` 或执行 `m0BaselineCheck.measureViewport()`；包装层左右各 16 px，iframe 的 `innerWidth` 才是产品视口。本轮从全新 1,472 px 包装视口打开自动复核地址，产品视口精确为 1,440 px，静态、视口、19 个状态、38 组对比度和交互五类结果均为 `pass`。

包装页为了让同目录复核脚本读取可信的纯合成 `srcdoc`，不把 iframe 作为安全沙箱；仍保留 CSP 和 `referrerpolicy`。该约束只适用于离线设计证据，仓库 HTML 与脚本都不得进入 M1 生产构建。

## 四、验证环境与局限

- 浏览器表面：Codex in-app Browser，Chromium 引擎。
- 自动化表面没有暴露 Chromium 精确 build/version，也没有暴露页面缩放或 Lighthouse；因此未伪造这些字段。
- 页面缩放按已批准替代协议以同 DPR、产品 CSS 宽度减半验证 200% reflow；M1 真实页面仍必须使用全新浏览器配置执行 Lighthouse 和实际 200% 缩放。
- 页面自身只产生“复核脚本已安装”和“自动复核完成：通过”两条 info，没有来源于仓库资产的 warning/error。in-app Browser 的页面观察器在同源 iframe 导航时另产生一条无 URL 的 `MutationObserver.observe` TypeError；它来自自动化表面而非仓库脚本，已在此保留为工具局限，未用“控制台零错误”作为 M0 通过依据。
- `m0-browser-check.js` 已通过 `node --check`，并按上述 `?audit=1` 路径从全新页面实际运行；隐藏结果节点和浏览器自动化逐项读取一致。

## 五、响应式证据

最终仓库资产的产品 frame 精确设置为下列宽度，读取根节点与四个产品窗口的 `clientWidth/scrollWidth`：

四个正式宽度和四个 200% 等效宽度均通过全新 `?audit=1` 页面自动复测；每轮的静态、视口、19 状态、38 对比度和关键交互总结果均为 `pass`。

| 产品 CSS 视口 | 根内容宽度 | 侧栏 | 审核列数 | 主内容 padding | 根溢出 | 四页窗口溢出 |
| ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 768 px | 753 px | 64 px | 2 | 16 px | 0 | `0/0/0/0` |
| 1,024 px | 1,009 px | 64 px | 2 | 24 px | 0 | `0/0/0/0` |
| 1,440 px | 1,425 px | 196 px | 3 | 24 px | 0 | `0/0/0/0` |
| 1,920 px | 1,905 px | 196 px | 3 | 24 px | 0 | `0/0/0/0` |

根内容宽度比产品视口少 15 px 是垂直滚动条占位，不是横向裁切。断点边界也已验证：1,280 px 为 196 px 完整侧栏和双栏审核，1,439 px 仍为双栏，1,440 px 切为三栏；960 px 为 64 px 折叠侧栏、双栏和 16 px padding。

### 200% 等效 reflow 与窄屏容错

| 原视口 | 等效 CSS 视口 | 布局 | 主内容 padding | 根溢出 | 四页窗口溢出 |
| ---: | ---: | --- | ---: | ---: | --- |
| 768 px | 384 px | 单列 | 12 px | 0 | `0/0/0/0` |
| 1,024 px | 512 px | 单列 | 12 px | 0 | `0/0/0/0` |
| 1,440 px | 720 px | 单列 | 12 px | 0 | `0/0/0/0` |
| 1,920 px | 960 px | 折叠侧栏、双栏审核 | 16 px | 0 | `0/0/0/0` |

上述四个宽度的登录、上传、确认和账单选择均在各自窗口边界内。核心动作高度为 40 px，账单类型按钮为 38 px，队列和账单行文字操作为 32 px，两个原生关联 radio 的 label 高度为 57/48 px。384、512 和 720 px 不属于 M1 正式操作端，只证明容错布局可恢复且无根级横向溢出。

## 六、状态与交互证据

状态选择器共 19 个，逐项满足“请求状态 = 窗口状态且当前按钮 `aria-pressed=true`”：

| 页面 | 已验证状态 |
| --- | --- |
| 登录 | 默认、凭据错误、提交中、权限不足 |
| AI 收件箱 | 混合队列、处理中、部分结果、失败、空、离线；混合队列另含待确认、取消和已完成行 |
| 审核工作台 | 待审核、阻断、版本冲突、加载中、已完成 |
| 账单列表 | 有数据、加载中、空、权限不足 |

关键交互结果：

- 登录点击后立即进入 `loading`、按钮禁用并显示“正在验证…”，750 ms 复核时已进入明确 `error`，邮箱 `aria-invalid=true` 且错误节点可见；
- 收件箱“失败”筛选只保留一条 `failed` 行；混合队列同时显示 `review`、`processing`、`failed`、`cancelled` 和 `completed`；
- 7 个审核字段逐一点击后都只激活相同 `field_path` 的一条证据；销售方同时显示 AI 原值、用户修订值和 `p1-08`；
- 初始未选关联候选时确认按钮禁用；键盘 Space 接受候选后启用，ArrowDown 转到“不关联任何候选”，确认后进入 `completed` 并显示“候选关联已拒绝”；直接预览完成态时自动选择接受候选，摘要与完成事实一致；
- 账单切换为 Payment 后只显示两条支付行；选择 `payment-42` 后详情同步为“星环科技（测试）/支付/CNY 2,000.00”；
- 审核候选是原生 radio，关联决定没有默认值；失败 Job 才有“重试”，审核终态没有重新提取入口。

## 七、可访问性证据

### 键盘与语义

- 登录核心顺序为邮箱 -> 密码 -> 保持登录 -> 登录 -> 下一页“混合队列”；前三个焦点使用浏览器可见 `auto 1px`，页面按钮使用高对比 `solid 2px`。
- 邮箱和密码具有原生 label，并由 `aria-describedby="sbm02-login-error"` 关联同一 `role="alert"` 错误节点。
- 内层文档为 `lang="zh-CN"`、中文 title、恰好一个 main；四个直接子 section 的可访问名称分别为登录、AI 收件箱、审核工作台和账单列表。
- 共 1 个 form、18 个 heading、5 个 table；ID 重复 0、断裂 ARIA/label 引用 0、重复 navigation label 0、无效 `aria-checked` 0。
- 图标不重复播报，可交互图标按钮有可访问名称；动态结果使用 `aria-live`，进度使用原生可读状态；CSS 尊重 `prefers-reduced-motion`。
- 状态同时使用文字、图标和结构，不把颜色作为唯一信号。

### 文本对比度

对每个可见产品文本节点读取 computed foreground 与最近不透明 background，普通文本以 4.5:1、大文本以 3:1 判定。19 个状态在浅色和深色各执行一轮，共 38 次状态/主题审计：

| 主题 | 最低对比度 | 失败节点 |
| --- | ---: | ---: |
| 浅色 | 4.56:1 | 0 |
| 深色 | 5.83:1 | 0 |

不同状态每轮覆盖 110–273 个可见直接文本节点。该结果只证明 M0 视觉基线的颜色配对，不替代 M1 真实页面的 Lighthouse、屏幕阅读器或端到端无障碍验收。

## 八、文档与范围复核

最终复核命令：

```bash
git diff --check
node --check docs/design/m0-browser-check.js
rg -n --glob '!docs/m0-evidence.md' '待批准|推荐基线|M0 草案|M0 进行中|技术验收证据待补齐' AGENTS.md README.md README_EN.md docs
rg -n 'backend-go|frontend|旧 OCR|旧数据库|回归样本' AGENTS.md README.md README_EN.md docs
```

第二条搜索允许命中明确的遗留禁止项和 Clean Slate 说明；任何把遗留目录描述为新运行入口、依赖或兼容目标的命中都失败。Markdown 相对链接必须逐一解析且目标存在。最终 diff 还要证明没有业务代码、运行时依赖、迁移、部署、提交或推送。

## 九、独立复审

只读预审发现并推动闭合了量化协议、删除生命周期、Job 状态机、完整 Claim revision、动态字段墓碑、字段级追溯、Provider 冻结、非法 JSON 归属、金额与重复规则、最小权限/bootstrap、M1 关联和视觉可访问性等边界。

最终独立只读复审由 `m0_readonly_audit` 在 2026-08-27 对最新完整工作树执行，结论如下：

| 级别 | 结果 |
| --- | ---: |
| Blocker | 0 |
| Major | 0 |
| Minor | 0 |

复审者同时确认：变更仅涉及允许的 M0 文档与设计资产；Markdown 相对链接与文档路径引用均无缺失；HTML 语义、ARIA、19 个状态、7 组字段证据和外部资源检查通过；最新 HTML/JS 哈希与本文件一致；`node --check`、`git diff --check` 和浏览器五类自动复测均通过。最终结论是“可以将 M0 标记完成，不进入 M1”。

## 十、M0 退出条件

| 门禁 | 当前结果 |
| --- | --- |
| 独立分支、基线 SHA、干净起点 | 通过 |
| 产品、范围、架构、AI、数据、UI/UX、ADR | 通过 |
| 量化指标批准与测量协议 | 通过 |
| 唯一视觉方向与四页批准 | 通过 |
| 稳定视觉资产、复测脚本与哈希 | 通过 |
| 768/1,024/1,440/1,920 px | 通过 |
| 浅色、深色、键盘、200% 等效 reflow、WCAG 对比度 | 通过 |
| diff 仅含 M0 文档和设计资产 | 通过 |
| 独立只读复审无阻断或重大问题 | 通过；Blocker/Major/Minor 均为 0 |

全部 M0 门禁已通过，M0 于 2026-08-27 完成。M1 未开始，且必须获得产品负责人明确授权后才能进入。
