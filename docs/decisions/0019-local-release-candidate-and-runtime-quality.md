# ADR-0019：本地发布候选与运行质量门禁

状态：已接受并完成本地验收；由 ADR-0020 修订为 PostgreSQL 17 候选
日期：2026-08-31

## 背景

M4 前两个切片已经完成确定性 Fact 洞察和认证停机恢复，但当前本地发布资产仍以 `m1` 命名，生产镜像没有包含新的 `smart-bill-manager-backup/2` CLI，acceptance Compose 也没有满足 synthetic Provider 当前要求的 exercise 身份。既有性能、内存、Lighthouse 和响应式脚本可以执行已批准协议，但部分写阶段在业务变化之后才占用结果路径，原始报告还包含仅适合临时隔离区的 Job ID、进程 ID、路径或摘要，不能直接作为仓库证据。

M4 最后一个本地功能切片必须把当前 Clean Slate 系统整理为可复现的本地发布候选，并重新执行受架构变化影响的运行质量门禁。该结果证明本地制品和运维流程就绪，不代表真实模型质量、外部账号联调、部署或生产发布获准。

## 决策

### 单一稳定发布入口

- 以 `infra/docker/app.Dockerfile`、`infra/docker/entrypoint.sh`、`infra/compose/compose.yaml` 和 `infra/compose/.env.example` 作为唯一当前发布入口；删除被替代的 `m1` 文件名、镜像名、Compose project 名和报告身份，不保留别名或兼容包装。
- 发布镜像只包含当前 Web 静态产物、`server`、`bootstrap-owner`、认证 `backup` CLI、迁移、Provider-facing Schema 及最小运行依赖。`recovery-exercise`、`seed-performance`、测试、评估数据、工具源码、旧实现、数据库、日志和凭据不得进入镜像。
- 镜像构建固定现有 Node 24.19.0、Go 1.26.7 和 Alpine 3.23 身份；本地验收记录构建时基线 HEAD、确定性的发布输入摘要、镜像 ID、Compose 规范化摘要和构建参数。切片证据本身不进入镜像构建上下文，因此最终提交只需复核发布输入摘要未变化，不把尚未存在的最终提交 SHA 自引用为镜像身份。当前环境只允许复用已有镜像与依赖缓存并禁网构建，不下载新依赖或镜像。
- 发布产物必须先由 `tools/prepare-local-release-artifacts.mjs` 在仓库外 owner-only `/tmp` 隔离区生成。准备器先占用输出目录，再使用固定宿主 Node 24.19.0 在独立工作区执行 `npm ci --offline` 与生产构建，并使用固定 Go 1.26.7 镜像、`network=none`、只读模块缓存、`GOPROXY=off` 和 `go mod verify` 构建四个二进制。缓存缺失或不一致必须失败，不能临时联网、复制现有 `node_modules` 或跳过校验。
- Dockerfile 只接受准备器生成的本地 `release_artifacts` BuildKit 上下文，禁止 URL、Git 或镜像型产物上下文；构建时必须核对基线 HEAD、发布输入摘要、Node/Go 版本、精确文件清单及全部 SHA-256 后才能复制到最终镜像。隔离产物不提交，最终提交后仍以源码发布输入摘要不变作为复核边界。
- 最终运行层固定为已有 Alpine 3.23，并在装配时移除其 `apk`；PDF 工具固定为本机既有、manifest 标识 Poppler 26.05.0 与批准来源 SHA-256 的自包含 bundle，并纳入产物清单逐文件校验。bundle 所需的 glibc 2.41 只从本机既有且 ID 固定的 Go 1.26.7 Debian 镜像选择性复制动态加载器及 `libc/libm/libdl/libpthread` 五个文件；Clean Slate 租户时区所需的完整 IANA `/usr/share/zoneinfo` 也只从该固定来源复制，并至少核验 `Asia/Shanghai` 与 `zone.tab`。不得复制 Go、包管理器或其他 Debian 内容，也不得运行时下载或为单个时区增加兼容分支。降权由当前源码编译的静态 `run-as-sbm` 完成，最终检查同时执行 PDF 版本、实际资产、进程 UID/GID、工具链与包管理器缺席门禁。

### 运行时最小权限与失败边界

- Compose 默认只绑定 `127.0.0.1`，根文件系统只读，持久化位置仅为数据库与对象卷，临时位置使用带 `noexec,nosuid,nodev` 的受限 tmpfs；保留 `no-new-privileges`、PID/CPU/内存限制和显式健康检查。
- 镜像显式以 root 启动唯一容器入口，权限仅用于把外部只读主密钥材料化为 owner-only、单硬链接运行文件和修正两个持久卷的运行用户所有权；运行密钥目录固定为 `root:sbm 0710`，使 UID/GID 10001 只能穿越读取自己的 `0600` 文件而不能列出、创建、替换或删除目录项。随后必须以固定 UID/GID 10001 执行应用，应用二进制不得保留 root。缺失、空、过大、格式错误、符号链接或多硬链接主密钥均 fail-closed，不把值写入 argv、环境、日志或镜像层。
- 本地 Compose 对文件型 secret 声明的 `uid/gid/mode` 不执行重映射，因此配置不声明虚假的权限元数据。Owner 密码只在命令首参数精确为 `/app/bootstrap-owner` 的一次性容器内，由 root entrypoint 从 owner-only 单硬链接源复制为 UID/GID 10001、`0600` 的 tmpfs 文件；正常 server 启动不得材料化该密码。Provider key 由当前 owner UID 1000 与固定 Node UID 1000 的本地验收身份直接只读共享，并由动态启动、探测和日志门禁证明可用且不泄露。
- `local` 模式允许回环 HTTP 与非 Secure Cookie；`production` 模式继续强制 Secure Cookie。真实 TLS 终止、域名、外部密钥托管、远端存储和生产部署仍属于发布门禁。
- acceptance synthetic Provider 与应用共享网络命名空间，只监听回环地址，并绑定既有固定 Node 镜像的不可变 image ID、本轮 UUIDv4 exercise、`synthetic-*` 模型和受保护 key 文件；宿主浏览器关闭后台网络服务，且只允许回环 HTTP origin 绕过本地不可用代理，Node 客户端拒绝跨源重定向；不得连接外网或真实 Provider。
- Linux Docker internal bridge 不激活宿主端口映射；验收期间由无凭据、无 DNS、无内容日志的本地 TCP 桥先核对 app 容器 ID、候选镜像 ID、唯一 internal 网络和未激活映射，再把 `127.0.0.1:8080` 转发到该候选。桥只属于验收控制面并随项目销毁；常规 base Compose 继续使用原生回环映射。运行时登录成功与内部网络检查共同证明浏览器连接的正是本轮新数据库上的候选，而不是放宽 Provider 或 app 的出网边界。

### 运行质量测量

- 10,000 Fact 非 AI 性能、200 次不等待 Provider 的 Document 创建、200 次首次审核确认、50 个测量 Job 的内存趋势、四个代表页面各三次 Lighthouse、16 个正式响应式页面组合、16 个等效 200% 回流组合、键盘、深色主题和当前完整 Playwright 场景，均继续使用 `docs/acceptance.md` 已批准阈值；性能报告必须明确 Provider 调用被排除，延迟由同一候选的内存门禁单独测量。内存采样必须先把宿主 PID 绑定到 UID/GID 10001、argv[0]=`/app/server` 的实际发布进程，并用精确候选容器的 `docker top` 与容器内同 UID exec 的 PID namespace 双重核对，不能把 Compose init PID 当作 server，也不能为读取其他 UID 的 `/proc` 给入口 root 补回 ptrace capability；同轮 Provider 必须保持端口不向宿主发布，由门禁通过已核验 app 容器的内部回环读取不含请求明细的聚合报告，并精确核对 1 次 probe、60 次 extraction 及其 p50/p95/max 延迟。
- 性能、内存、Lighthouse、响应式和镜像检查在任何 API/数据库写入或浏览器 fixture 创建前，先以 O_EXCL 或等价原子方式占用 owner-only 结果位置。已有、符号链接、多硬链接、宽权限父目录或位于仓库/运行数据/凭据路径的输出一律拒绝。
- 原始报告只能位于本轮 `/tmp` 隔离目录，可以包含验证所需的合成 ID、局部样本或环境细节，但不得包含凭据值、Cookie、真实财务字段或真实 Provider 响应；终端只输出安全聚合或固定分类码。
- 仓库只提交一个由严格合并器生成的安全聚合：构建时基线 HEAD、发布输入摘要、镜像 ID、Compose 摘要、阈值、样本数、p95、内存中位数比/斜率、页面最低分、响应式/回流/键盘结果、E2E 数量、镜像必需/禁止资产布尔值和静态门禁。不得提交原始 Lighthouse、Playwright、性能、内存、镜像文件清单、Compose 展开结果、路径、容器/进程/业务 ID 或摘要原文。
- 网络证据只对最终候选的禁网构建与正式验收隔离作肯定声明；开发过程若发生已披露的工具网络策略偏差，聚合必须保留安全布尔记录，不得用笼统的“从未使用外网”覆盖历史事实。

### 本地运维准备

- 新增当前版本的本地运行与故障诊断说明，覆盖构建、首次 Owner 初始化、健康检查、停止写入、认证备份/验证/恢复、日志最小化、容量告警、升级前备份和失败回滚。
- 回滚只允许回到同一 Clean Slate 数据契约的已验证本地镜像和认证备份；不读取、迁移或恢复旧系统数据，不增加旧 Schema 探测或双写。
- 数据模型、Source→Claim→Fact、人工审核、租户隔离、整数金额和最小中文多模态提取链均不变化；本切片不新增迁移、业务 API 或产品页面。

## 通过条件

1. 稳定发布资产替代全部 `m1` 发布入口，镜像包含必要 CLI 且不含开发/私有资产；
2. Compose 规范化、安全配置、健康检查、固定 UID/GID 和空库 Owner 初始化通过；
3. 当前完整纯合成 E2E、性能、内存、Lighthouse、响应式、回流、键盘和深色主题门禁通过；
4. 全量 Go/Web/Node、关键不变量、覆盖率、构建、敏感信息、临时产物和进程残留门禁通过；
5. 运维文档、安全聚合证据与实际制品一致，本轮凭据和临时数据已销毁；
6. 创建独立本地提交且不推送，随后停在首次正式真实模型评测、真实外部联调和生产发布门禁前。

## 明确不包含

- 真实模型正确率正式评测、真实 Provider 或邮箱/外部账号连接；
- 新依赖、镜像、OCR、模型文件或付费资源下载；
- 域名、TLS 证书、云资源、远端存储、部署、发布、Tag、Release 或推送；
- 旧应用、旧数据库、旧 API、旧任务状态、旧 Compose 或旧发布入口兼容。

## 后果

- 本地发布候选只有一个当前入口，运维与证据不再依赖里程碑命名；
- 原始运行质量报告仍可在隔离区完成严格复核，但仓库只保留安全、可审查的聚合；
- 本切片通过后，本地产品功能开发完成；M4 与持续 Goal 仍必须停在真实模型、真实外部系统和生产发布门禁之前，不能把“本地就绪”改写为“已发布”。
