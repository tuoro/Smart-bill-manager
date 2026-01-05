# 智能账单管理系统 (Smart Bill Manager)

一个现代化的账单管理系统：支持支付记录与发票管理（OCR 自动填充）、行程日历归属、邮箱 IMAP 监控与解析。支持多用户隔离账本，管理员可查看全量并代操作（带强制确认）。

## 功能特性

### 认证与用户体系
- 首次启动：通过 `/setup` 创建管理员账户
- 关闭公开注册：新增用户通过邀请码注册（管理员创建邀请码）
- JWT Bearer Token（默认有效期 7 天，可配置）

### 多用户（隔离账本 + Admin 可看全量）
- 数据按 `owner_user_id` 隔离（支付/发票/行程/邮箱配置与日志/OCR 任务等）
- Admin 可在前端进入“用户”页面进行代操作（act-as）
- 代操作写请求强制二次确认：后端要求 `X-Act-As-Confirmed: 1`，前端会弹窗确认后自动重试
- 升级/迁移：旧数据会回填归属到“最早创建的用户”（通常是管理员），避免出现“无 owner”数据

### 文件存储与访问（绝对隔离）
- 上传文件按用户分目录存储：`uploads/<ownerUserID>/...`
- 不公开暴露 `/uploads` 静态目录：预览/下载必须走鉴权 API
  - 支付截图预览：`GET /api/payments/:id/screenshot`
  - 发票文件预览：`GET /api/invoices/:id/file`
  - 发票文件下载：`GET /api/invoices/:id/download`

### 支付记录管理
- 支付记录增删改查、筛选与统计
- 支付截图上传 + OCR 自动抽取（金额/商家/交易时间/支付方式）
- 上传先生成草稿（`is_draft=true`），点击保存才进入正式列表/统计

### 发票管理
- PDF/图片发票上传（支持批量）
- OCR 自动抽取：发票号码/日期/金额/税额/购销方等
- 支付与发票关联：
  - 一张发票最多关联一笔支付
  - 一笔支付可关联多张发票
- 智能匹配建议（金额与日期窗口）

### 行程日历（差旅归属）
- 行程创建、变更、删除（变更后自动重算归属）
- 自动归属：支付时间唯一命中行程 → 自动归属
- 行程重叠/不确定：进入“待处理/待分配”，由用户手动分配

### 邮箱监控与解析
- IMAP 实时监控、手动检查
- 解析邮件发票：支持 PDF 附件；无附件时会尝试从正文链接提取 **XML/PDF 下载链接**
- 若 XML 可获取：优先用 XML 抽取字段（通常更完整/更准），并保存 PDF 供预览

## 技术栈

### 后端
- Go 1.24 + Gin
- SQLite + GORM
- JWT（Bearer Token）+ bcrypt
- IMAP：emersion/go-imap + go-message
- OCR：RapidOCR v3（Python + onnxruntime CPU）
- PDF：PyMuPDF / poppler-utils（`pdftotext`/`pdftoppm`）

### 前端
- Vue 3 + TypeScript + Vite
- PrimeVue + PrimeFlex + PrimeIcons
- Pinia + Vue Router + Axios
- ECharts / Vue-ECharts

## 快速开始

### 方式一：Docker Compose（推荐）

使用仓库自带的 `docker-compose.yml`：

```bash
docker compose up -d --build
```

访问 http://localhost。

数据默认持久化到两个卷：
- `app-data`：数据库与 OCR 缓存（如 RapidOCR 模型）
- `app-uploads`：上传文件（按用户隔离目录）

### 方式二：预构建镜像

```bash
docker pull ghcr.io/tuoro/smart-bill-manager:latest
docker run -d --name smart-bill-manager -p 80:80 \
  -e NODE_ENV=production \
  -e SBM_OCR_DATA_DIR=/app/backend/data \
  -e SBM_OCR_WORKER=1 \
  -e SBM_REGRESSION_SAMPLES_DIR=/app/backend/internal/services/testdata/regression \
  -v smart-bill-data:/app/backend/data \
  -v smart-bill-uploads:/app/backend/uploads \
  ghcr.io/tuoro/smart-bill-manager:latest
```

### 首次初始化与新增用户

1. 首次打开会进入 Setup 页面：创建管理员账号
2. 管理员登录后，在“邀请码管理”创建邀请码
3. 新用户使用邀请码在注册页完成注册（系统不开放公开注册）

## 环境变量（常用）

### 服务与路径
- `PORT=3001`：后端端口（容器内由 Nginx 反代到 80）
- `NODE_ENV=production|development`
- `DATA_DIR=./data`：SQLite 数据目录
- `UPLOADS_DIR=./uploads`：上传目录

### JWT
- `JWT_SECRET`：JWT 签名密钥（不设置则启动时随机生成；容器重启后旧 token 会失效）
- `JWT_EXPIRES_IN=168h`：token 有效期（默认 7 天）

### 草稿清理
- `SBM_DRAFT_TTL_HOURS=6`
- `SBM_DRAFT_CLEANUP_INTERVAL_MINUTES=15`

### OCR（RapidOCR v3，CPU）
- `SBM_OCR_ENGINE=rapidocr`（默认）
- `SBM_OCR_WORKER=1`（推荐：保持常驻 worker，避免每次启动 Python）
- `SBM_OCR_DATA_DIR=/app/backend/data`（推荐：持久化 RapidOCR 模型缓存到 `$SBM_OCR_DATA_DIR/rapidocr-models/`）
- `SBM_PDF_TEXT_EXTRACTOR=pymupdf|off`（默认 `pymupdf`）
- `SBM_PDF_OCR_DPI=220`（可选，建议 `120-450`）
- `SBM_INVOICE_PARTY_ROI=auto|true|false`（默认 `auto`）
- `SBM_INVOICE_TOTAL_ROI=auto|true|false`（默认 `auto`，仅当价税合计/税额缺失时做 ROI 补充识别）
- `SBM_OCR_DEBUG=true`（可选）

### 异步 OCR 任务（Task Worker）
- `SBM_TASK_PROCESSING_TTL_SECONDS=3600`
- `SBM_TASK_REAPER_INTERVAL_SECONDS=30`
- `SBM_TASK_IDLE_MIN_MS=200`
- `SBM_TASK_IDLE_MAX_MS=5000`

### 回归样本
- `SBM_REGRESSION_SAMPLES_DIR=/app/backend/internal/services/testdata/regression`

## 去重与疑似重复

- 强去重：上传时对文件计算 `SHA-256`；若哈希重复，接口返回 `409`
- 疑似重复：保存时提示，可在确认后强制保存
  - `PUT /api/invoices/:id` / `PUT /api/payments/:id`：`confirm=true` 且 `force_duplicate_save=true`

## 本地开发

环境要求：Go >= 1.21、Node.js >= 18、Python 3（RapidOCR）。

```bash
cd backend-go
go mod download
go run ./cmd/server

cd ../frontend
npm ci
npm run dev
```

前端开发地址：http://localhost:5173

## QQ 邮箱配置说明

1. 登录 QQ 邮箱网页版
2. 「设置」→「账户」→ 开启「IMAP/SMTP 服务」
3. 生成授权码
4. 在系统中添加邮箱配置：
   - IMAP：`imap.qq.com`
   - 端口：`993`
   - 密码：使用授权码（不是 QQ 密码）

## 项目结构

```
Smart-bill-manager/
├── backend-go/                  # Go 后端
│   ├── cmd/server/main.go       # 应用入口
│   ├── internal/
│   │   ├── config/              # 配置
│   │   ├── models/              # 数据模型
│   │   ├── handlers/            # HTTP 接口
│   │   ├── services/            # 业务逻辑（OCR/支付/发票/行程等）
│   │   ├── middleware/          # 中间件
│   │   ├── repository/          # 数据访问
│   │   └── utils/               # 工具
│   ├── pkg/database/            # 数据库连接
│   └── ...
├── frontend/                    # Vue 前端
│   ├── src/
│   │   ├── router/              # 路由
│   │   ├── stores/              # Pinia
│   │   ├── views/               # 页面
│   │   ├── components/          # 复用组件
│   │   ├── api/                 # API 封装
│   │   └── types/               # TS 类型
│   └── ...
├── scripts/                     # 辅助脚本
│   ├── ocr_cli.py               # 调用 RapidOCR（含模型自动下载/校验）
│   ├── ocr_worker.py            # 常驻 OCR worker（可选）
│   └── pdf_text_cli.py          # PDF 文本提取调试
├── default_models.yaml          # RapidOCR 默认模型列表（含哈希校验）
├── Dockerfile                   # 前后端统一镜像
├── docker-compose.yml           # Compose 部署
├── nginx.conf                   # 统一 Nginx 配置
├── supervisord.conf             # 进程管理配置
└── README.md
```

## API 接口（概览）

### 支付记录
- `GET /api/payments`
- `GET /api/payments/stats`
- `GET /api/payments/:id`
- `GET /api/payments/:id/screenshot`
- `GET /api/payments/:id/invoices`
- `GET /api/payments/:id/suggest-invoices`
- `POST /api/payments`
- `POST /api/payments/upload-screenshot`
- `POST /api/payments/upload-screenshot-async`
- `POST /api/payments/upload-screenshot/cancel`
- `POST /api/payments/:id/reparse`
- `PUT /api/payments/:id`
- `DELETE /api/payments/:id`

### 发票管理
- `GET /api/invoices`
- `GET /api/invoices/stats`
- `GET /api/invoices/unlinked`
- `GET /api/invoices/:id`
- `GET /api/invoices/:id/file`
- `GET /api/invoices/:id/download`
- `GET /api/invoices/:id/linked-payments`
- `GET /api/invoices/:id/suggest-payments`
- `POST /api/invoices/upload`
- `POST /api/invoices/upload-async`
- `POST /api/invoices/upload-multiple`
- `POST /api/invoices/upload-multiple-async`
- `POST /api/invoices/:id/link-payment`
- `DELETE /api/invoices/:id/unlink-payment`
- `POST /api/invoices/:id/parse`
- `PUT /api/invoices/:id`
- `DELETE /api/invoices/:id`

### 邮箱
- `GET /api/email/configs`
- `POST /api/email/configs`
- `PUT /api/email/configs/:id`
- `DELETE /api/email/configs/:id`
- `GET /api/email/logs`
- `POST /api/email/logs/:id/parse`
- `POST /api/email/test`
- `POST /api/email/monitor/start/:id`
- `POST /api/email/monitor/stop/:id`
- `GET /api/email/monitor/status`
- `POST /api/email/check/:id`

### 行程
- `GET /api/trips`
- `POST /api/trips`
- `PUT /api/trips/:id`
- `DELETE /api/trips/:id`

### 任务（异步 OCR）
- `GET /api/tasks/:id`
- `POST /api/tasks/:id/cancel`

### 管理员
- `GET /api/admin/users`
- `GET/POST/DELETE /api/admin/invites`
- `GET/POST /api/admin/regression-samples/...`
- `GET /api/logs` / `GET /api/logs/stream`

## License

MIT License - 详见 [LICENSE](LICENSE)
