# 智能账单管理系统 (Smart Bill Manager)

Smart Bill Manager 是一个面向个人与小团队的自托管账单系统，统一管理支付记录、电子发票、差旅行程和邮箱票据。系统支持 OCR 自动提取、多用户账本隔离、管理员代操作和异步任务处理。

当前稳定版本：`v0.2.4`。本版本收紧启动迁移与服务边界，确保邮件日志无损去重、邮箱密钥一致性和 OCR worker 可中断停机，并补齐生产 Compose 门禁；升级说明见 [CHANGELOG.md](CHANGELOG.md)，设计边界见 [架构说明](docs/architecture.md)。

## 主要能力

- 支付截图上传、OCR 识别、分类、筛选和统计
- PDF/图片发票批量上传、字段提取、去重和支付匹配
- IMAP 邮箱监控、附件及正文票据链接解析
- 差旅行程归属、待分配处理、报销与坏账状态管理
- 邀请码注册、多用户数据隔离、管理员代操作二次确认
- 异步 OCR 任务、任务取消、回归样本管理
- 鉴权文件预览和下载，上传文件按用户目录隔离

## 技术栈

- 后端：Go 1.24、Gin、GORM、SQLite、JWT、emersion/go-imap
- 前端：Vue 3、TypeScript、Vite、Pinia、PrimeVue、Axios、ECharts
- OCR/PDF：RapidOCR v3、ONNX Runtime、PyMuPDF、Poppler
- 部署：Nginx、Supervisor、Docker Compose、GitHub Actions、GHCR

## 快速开始

### Docker Compose

生产部署使用 `docker-compose.production.yml` override。它固定 `NODE_ENV=production`，并在 `JWT_SECRET` 为空时拒绝创建容器；后端还会拒绝少于 32 个字符的密钥。

首次部署先创建环境文件，用 `openssl rand -hex 32`（或密码管理器）生成密钥，将输出写入 `.env` 的 `JWT_SECRET`，并长期保存该文件或对应的密钥管理项：

```bash
cp .env.example .env
openssl rand -hex 32
# 将上一步输出写入 .env 的 JWT_SECRET 后执行
docker compose -f docker-compose.yml -f docker-compose.production.yml config --quiet
docker compose -f docker-compose.yml -f docker-compose.production.yml up -d --build
```

Windows PowerShell 可使用 `Copy-Item .env.example .env`。`config --quiet` 只校验配置而不打印密钥；启动后访问 <http://localhost>，首次进入 `/setup` 创建管理员账户。本地 Compose 开发仍可直接运行 `docker compose up --build`，基础文件在未设置 `NODE_ENV` 时默认使用 `development`，不要将该入口用于生产。

默认持久化卷：

- `app-data`：SQLite 数据库、邮箱密码加密密钥和 OCR 模型缓存
- `app-uploads`：支付截图、发票和邮件附件

查看运行状态：

```bash
docker compose -f docker-compose.yml -f docker-compose.production.yml ps
docker compose -f docker-compose.yml -f docker-compose.production.yml logs -f smart-bill-manager
```

### 预构建镜像

```bash
docker pull ghcr.io/tuoro/smart-bill-manager:0.2.4
docker run -d --name smart-bill-manager -p 80:80 \
  -e NODE_ENV=production \
  -e JWT_SECRET="replace-with-a-persistent-32-char-secret" \
  -e SBM_OCR_DATA_DIR=/app/backend/data \
  -e SBM_OCR_WORKER=1 \
  -e SBM_REGRESSION_SAMPLES_DIR=/app/backend/internal/services/testdata/regression \
  -v smart-bill-data:/app/backend/data \
  -v smart-bill-uploads:/app/backend/uploads \
  ghcr.io/tuoro/smart-bill-manager:0.2.4
```

生产环境必须持久保存 `JWT_SECRET`，不要在每次启动时重新生成，否则已有登录会话会全部失效。

## 从 v0.1.0 升级

必须先在仍检出 v0.1.0 的目录中停机并备份，然后才能拉取新代码。v0.1.0 尚无 production override，因此此阶段只能使用它已有的基础 `docker-compose.yml`。至少应保存 `bills.db`、`email_password.key` 和整个上传目录；Compose 会为卷名添加项目名前缀，最稳妥的方式是停止服务后从现有容器复制数据：

```bash
docker compose -f docker-compose.yml stop
mkdir -p backup-v0.1.0
docker cp smart-bill-manager:/app/backend/data ./backup-v0.1.0/data
docker cp smart-bill-manager:/app/backend/uploads ./backup-v0.1.0/uploads
```

确认备份可读后再拉取代码，并按新版本的 `.env.example` 创建或更新 `.env`。如果旧部署已有持久 `JWT_SECRET`，必须沿用原值；否则只生成一次新密钥并长期保存：

```bash
git pull --ff-only
[ -f .env ] || cp .env.example .env
# 对照 .env.example 补齐或更新 .env；仅在没有旧持久密钥时执行下一行
openssl rand -hex 32
# 将旧密钥或上一步输出写入 .env 的 JWT_SECRET 后执行
docker compose -f docker-compose.yml -f docker-compose.production.yml config --quiet
docker compose -f docker-compose.yml -f docker-compose.production.yml up -d --build
```

服务启动时会自动执行版本化迁移：

1. 先检查已有 `schema_migrations`，拒绝程序不支持的未来版本，再同步表结构；
2. 回填旧数据所有者、时间索引字段和 OCR 拆分数据；
3. 将金额转换并校验为整数分字段；
4. 无损合并重复邮件日志的发票关联和非空元数据，再建立并校验身份唯一索引；
5. 每个版本化迁移在独立事务内执行；邮箱密码修复也在单个事务中先验证已有密文再加密明文，任一步失败都拒绝启动。

迁移不会自动创建外部备份。v0.2.4 不会删除业务表或字段，也不会丢弃邮件日志的发票关联；冗余邮件日志只会在合并完成后于同一事务中删除。新旧金额字段在 v0.2.x 中继续双写以保留兼容窗口，但正式回退前仍必须恢复已验证的备份。

## 常用配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `3001` | 后端监听端口 |
| `NODE_ENV` | 基础 Compose 为 `development` | 生产 override 固定为 `production` |
| `JWT_SECRET` | 无 | 生产 override 必填，后端要求至少 32 个字符；必须跨重启持久保存 |
| `JWT_EXPIRES_IN` | `168h` | JWT 有效期 |
| `CORS_ALLOWED_ORIGINS` | 开发地址 | 跨域来源白名单，生产环境禁止 `*` |
| `DATA_DIR` | `./data` | SQLite 和本地密钥目录 |
| `UPLOADS_DIR` | `./uploads` | 上传文件根目录 |
| `SBM_OCR_WORKER` | `0` | 设为 `1` 启用常驻 OCR worker |
| `SBM_OCR_DATA_DIR` | 无 | OCR 模型缓存目录 |
| `SBM_PDF_TEXT_EXTRACTOR` | `pymupdf` | PDF 文本提取器，可设为 `off` |
| `SBM_DRAFT_TTL_HOURS` | `6` | 草稿保留时间，`0` 表示禁用清理 |
| `SBM_DRAFT_CLEANUP_INTERVAL_MINUTES` | `15` | 草稿清理周期 |
| `SBM_TASK_PROCESSING_TTL_SECONDS` | `3600` | 处理中任务超时 |

邮箱密码默认使用 `DATA_DIR/email_password.key` 加密。也可通过 `SBM_EMAIL_PASSWORD_KEY` 或 `SBM_EMAIL_PASSWORD_KEY_FILE` 提供稳定密钥；更换或丢失密钥会导致已保存的邮箱密码无法解密。

## 本地开发

环境要求：Go 1.24、Node.js 24、npm、Python 3；完整 SQLite 测试还需要 C 编译器，因为驱动依赖 CGO。

后端：

```bash
cd backend-go
go mod download
go run ./cmd/server
```

前端会把 `/api` 代理到 `http://localhost:3001`：

```bash
cd frontend
npm ci
npm run dev
```

访问 <http://localhost:5173>。

## 质量检查

```bash
cd backend-go
CGO_ENABLED=1 go test -count=1 -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
CGO_ENABLED=1 go vet ./...

cd ../frontend
npm ci
npm audit --audit-level=high
npm run lint:ci
npm run test:run
npm run build
```

CI 要求前端 ESLint 零警告、后端整体语句覆盖率不低于 35%，并完成统一 Docker 镜像构建。关键认证、代操作、文件访问、支付与发票关联已通过真实 HTTP 契约覆盖，但不能将整体数字理解为所有接口都已充分覆盖。

> 交付后续项：`Dockerfile` 当前只把 RapidOCR 限制在 `3.x`，`onnxruntime`、Pillow 和 PyMuPDF 仍由 pip 解析最新兼容版本。精确锁定这些 Python/OCR 依赖前，必须在可用的 Docker daemon 上完成目标架构镜像构建、`/api/health` 检查和一次真实 OCR 冒烟；在这些验证完成前不做无依据的批量升级。

## 项目结构

```text
Smart-bill-manager/
|- backend-go/
|  |- cmd/                 # 服务与辅助命令入口
|  |- internal/app/        # 应用装配和生命周期
|  |- internal/handlers/   # HTTP 适配层
|  |- internal/services/   # 业务与事务编排
|  |- internal/repository/ # 数据访问
|  |- internal/migrations/ # 版本化迁移
|  `- pkg/database/        # SQLite 连接
|- frontend/src/
|  |- api/                 # 请求客户端、存储和领域 API
|  |- stores/              # 跨页面会话状态
|  |- composables/         # 可复用异步流程
|  |- components/          # 领域组件
|  `- views/               # 路由页面
|- docs/architecture.md
|- Dockerfile
|- docker-compose.yml
`- docker-compose.production.yml
```

## 安全边界

- 所有业务数据按 `owner_user_id` 查询，管理员代操作写请求必须二次确认。
- 上传目录不作为公开静态目录；预览和下载必须经过鉴权接口。
- API 的 5xx 响应不返回内部错误详情，完整原因只写服务端日志。
- 生产环境拒绝空或过短的 JWT 密钥，也拒绝通配 CORS。

## License

MIT License，详见 [LICENSE](LICENSE)。
