# 智能账单管理系统 (Smart Bill Manager)

一个现代化的个人账单管理系统，支持支付记录管理、发票自动解析和邮箱实时监控。

## ✨ 功能特性

### 🔐 用户认证
- Session（HttpOnly Cookie，服务端 Session）
- 可选：PASETO v4.local API Token（用于非浏览器/第三方调用，`Authorization: Bearer <token>`）
- 安全的密码加密存储
- 首次启动通过 Setup 页面完成初始化
- API请求频率限制

### 📊 仪表盘
- 本月支出总览
- 每日支出趋势图
- 支出分类饼图
- 邮箱监控状态
- 最近邮件记录

### 💰 支付记录管理
- 添加、编辑、删除支付记录
- 按日期、分类筛选
- 支出统计分析
- 支持多种支付方式分类
- **🆕 支付截图上传和自动识别** ✨
  - 上传微信支付、支付宝、银行转账截图
  - 自动识别金额、商家、交易时间、支付方式
  - 若无法识别交易时间：上传仍会成功，但需要你手动选择交易时间后再保存（避免错误归属/统计）
  - 上传后进入“可编辑确认页”，点击保存才会变成正式记录（草稿不会出现在列表/统计中）
  - 支持查看 OCR 原始文本/整理文本，便于你校对与修正
  - OCR技术支持中英文识别

### 🗓️ 行程 / 差旅日历
- 创建行程（开始/结束时间精确到秒，可选择时区）
- 行程内消费统计（以支付时间为准）
- 自动归属：支付时间唯一命中行程 → 自动归属
- 行程重叠：自动归属的支付进入“待处理”，在行程页手动选择归属或保持无归属（手动归属不会被自动打回）
- 行程变更（新建/修改/删除）会自动重新计算归属，并提示归属变化数量
- 支持单笔支付移出行程 / 移动到其他行程
- 删除行程：仅删除归属到该行程的支付记录，并按关联关系删除/解绑发票（带预览确认）

### 📄 发票管理
- PDF/图片发票上传（支持批量，PNG/JPG）
- **🆕 智能发票识别** ✨
  - 自动解析发票号码、金额、税额、销售方、购买方
  - 支持增值税电子普通发票、增值税电子专用发票
  - 使用专业PDF解析库和OCR技术
- 上传后进入“可编辑确认页”，点击保存才会变成正式记录（草稿不会出现在列表/统计中）
- 支持查看 OCR 原始文本（用于排查“内容识别对但抽取错”等问题）
- 发票预览和下载
- 来源追踪（手动上传/邮件下载）
- **🆕 发票与支付记录关联** ✨
  - 手动关联发票到支付记录
  - 1:1 约束：一笔支付最多关联一张发票，且一张发票最多关联一笔支付
  - 智能匹配建议（基于金额和日期）
  - 查看关联关系

### 📬 邮箱监控
- **支持QQ邮箱** ✅
- 支持163、126、Gmail、Outlook等主流邮箱
- 实时监控新邮件
- 自动下载PDF附件
- 手动检查新邮件

## 🛠️ 技术栈

### 后端
- Go 1.24 (Gin Web框架)
- SQLite (GORM ORM)
- Session（HttpOnly Cookie，服务端 Session）
- PASETO v4.local（API Token，可撤销）
- golang.org/x/crypto/bcrypt (密码加密)
- emersion/go-imap (邮箱IMAP协议)
- **🆕 RapidOCR v3**（OCR识别引擎，Python + ONNXRuntime）✨
- **🆕 PyMuPDF**（PDF 文本提取/预处理，失败则回退到 OCR）✨
- **🆕 poppler-utils**（`pdftoppm`/`pdftotext`，用于 PDF→图片 与 文本提取）✨
- gin-contrib/cors (CORS支持)
- 内置请求频率限制

### 前端
- Vue 3 + TypeScript + Composition API
- Vite (构建工具)
- PrimeVue + PrimeFlex + PrimeIcons (UI组件/布局/图标)
- ECharts / Vue-ECharts (图表)
- Vue Router (路由)
- Pinia (状态管理)
- Axios (HTTP客户端)

## 📦 快速开始

### 方式一：使用预构建镜像（最简单）

直接从 GitHub Container Registry 拉取预构建的 Docker 镜像，无需克隆代码。

```bash
# 拉取最新镜像
docker pull ghcr.io/tuoro/smart-bill-manager:latest

# 运行容器
docker run -d \
  --name smart-bill-manager \
  -p 80:80 \
  -v smart-bill-data:/app/backend/data \
  -v smart-bill-uploads:/app/backend/uploads \
  ghcr.io/tuoro/smart-bill-manager:latest
```

访问 http://localhost 即可使用。

### 方式二：Docker Compose 部署（推荐）

使用 Docker Compose 可以更方便地管理容器和数据卷。

#### 环境要求
- Docker >= 20.10
- Docker Compose >= 2.0

#### 部署步骤

1. **创建 docker-compose.yml 文件**
```yaml
services:
  smart-bill-manager:
    image: ghcr.io/tuoro/smart-bill-manager:latest
    container_name: smart-bill-manager
    restart: unless-stopped
    ports:
      - "80:80"
    environment:
      - ADMIN_PASSWORD=your-admin-password      # 可选：设置管理员密码
      # 可选：固定 PASETO v4.local key（用于 API Token）；不配置则容器重启后 Token 失效
      # - PASETO_V4_LOCAL_KEY=your-base64-or-hex-key
    volumes:
      - app-data:/app/backend/data
      - app-uploads:/app/backend/uploads

volumes:
  app-data:
  app-uploads:
```

2. **启动服务**
```bash
docker-compose up -d
```

3. **首次访问**
- 打开浏览器访问 http://localhost
- 首次访问会自动进入初始化设置页面
- 创建管理员账户：
  - 输入管理员用户名（3-50字符）
  - 设置密码（至少6位）
  - 可选填写邮箱地址
- 创建后将自动登录到系统

4. **查看日志**
```bash
docker-compose logs -f
```

5. **停止服务**
```bash
docker-compose down
```

6. **数据持久化**
数据库和上传文件存储在 Docker 卷中：
- `app-data`: 数据库文件
- `app-uploads`: 上传的文件
  
RapidOCR 的模型缓存也建议持久化（否则每次重建/重启可能需要重新下载模型）：
- 已在 `docker-compose.yml` 默认设置 `SBM_OCR_DATA_DIR=/app/backend/data`，模型将保存到 `app-data` 卷内的 `/app/backend/data/rapidocr-models/`

#### 上传文件生命周期（草稿）

- 上传支付截图/发票会先创建 `is_draft=true` 的草稿记录：不会进入列表/统计，也不会影响行程归属与匹配。
- 点击保存（`confirm=true`）后才变成正式记录：文件路径不变，不会移动/改名。
- 点击取消/关闭弹窗会删除草稿记录并删除上传文件；刷新/崩溃导致的残留草稿会在前端/后端自动清理。
- 可用环境变量（后端）：
  - `UPLOADS_DIR=./uploads`（上传目录，容器内默认 `/app/backend/uploads`）
  - `SBM_DRAFT_TTL_HOURS=6`（草稿超时自动清理阈值）
  - `SBM_DRAFT_CLEANUP_INTERVAL_MINUTES=15`（草稿清理任务的轮询间隔）

#### 重复文件处理（去重）

- 强拒绝：上传时对文件计算 `SHA-256`；若哈希已存在（草稿或已保存），接口返回 `409` 并给出已有记录 `id`。
- 疑似重复（可强制保存）：
  - 发票：以 `invoice_number`（发票号码）为主键做疑似重复判断；保存时会提示，确认“仍然保存”才会落库为正式记录。
  - 支付截图：以“金额 + 交易时间（邻近窗口）”做疑似重复判断；保存时会提示，确认“仍然保存”才会落库为正式记录。
- API：`PUT /api/invoices/:id` / `PUT /api/payments/:id` 在 `confirm=true` 时可传 `force_duplicate_save=true` 进行强制保存（仅对“疑似重复”生效；哈希重复不允许强制）。

#### OCR（RapidOCR v3，CPU）

默认使用 `RapidOCR v3 + onnxruntime` 在 CPU 上进行识别，使用 RapidOCR 的**内置默认模型/配置**。

发票识别支持 **PDF/图片**：
- PDF：优先使用 PyMuPDF 提取内嵌文本（失败再走 RapidOCR）
- 图片（PNG/JPG）：直接使用 RapidOCR v3

支付截图（微信/支付宝等账单详情类页面）：会做一次布局感知后处理，把“标签列/值列”的 OCR 输出合并成更稳定的 `标签：值` 文本，提升字段提取稳定性。

可用环境变量：
- `SBM_OCR_ENGINE=rapidocr`（默认）
- `SBM_OCR_DATA_DIR=/app/backend/data`（推荐；用于把 RapidOCR 自动下载的模型缓存到可持久化目录，便于容器重启后复用；模型会写入 `$SBM_OCR_DATA_DIR/rapidocr-models/`）
- `SBM_PDF_TEXT_EXTRACTOR=pymupdf|off`（默认 `pymupdf`：优先用 PyMuPDF 提取 PDF 内嵌文本；失败再走 OCR）
- `SBM_PDF_OCR_DPI=220`（可选，范围建议 `120-450`；更高更清晰但更慢）
- `SBM_RAPIDOCR_MULTIPASS=1`（可选；对 `profile=pdf` 默认启用，用多种增强版本选最优结果）
- `SBM_RAPIDOCR_ROTATE180=true`（可选；对 `profile=pdf` 默认启用，兼容倒置扫描）
- `SBM_OCR_DEBUG=true`（可选；返回更多 OCR 变体评分信息，便于排查）


#### 发票购销方 ROI（可选）

默认情况下，发票 PDF 解析会对 **整页图片** 做 OCR，并仅注入二维码可稳定提取的抬头字段（如发票代码/号码/日期/价税合计）。

如果你希望额外启用“购买方/销售方”区域 ROI 识别（可能提升，也可能因为注入片段导致解析受干扰），可设置：
- `SBM_INVOICE_PARTY_ROI=auto|true|false`（默认 `auto`：仅在主 OCR 未能识别购销方时再尝试 ROI）

### 方式三：从源码构建

如果需要自定义或开发，可以从源码构建镜像。

1. **克隆仓库**
```bash
git clone https://github.com/tuoro/Smart-bill-manager.git
cd Smart-bill-manager
```

2. **构建并启动**
```bash
docker-compose up -d --build
```

或者单独构建镜像：

```bash
# 构建镜像
docker build -t smart-bill-manager .

# 运行容器
  docker run -d \
  --name smart-bill-manager \
  -p 80:80 \
  -e SBM_OCR_DATA_DIR=/app/backend/data \
  -v smart-bill-data:/app/backend/data \
  -v smart-bill-uploads:/app/backend/uploads \
  smart-bill-manager
```

### 方式四：本地开发

#### 环境要求
- Go >= 1.21
- Node.js >= 18
- npm >= 8
- RapidOCR v3 (用于图片文字识别，Python + ONNXRuntime)
- poppler-utils (用于PDF文本提取，支持CID字体)

安装系统依赖：
```bash
# Ubuntu/Debian
sudo apt-get install poppler-utils python3 python3-pip
python3 -m pip install "rapidocr==3.*" onnxruntime

# macOS
brew install poppler python
python3 -m pip install "rapidocr==3.*" onnxruntime
```

#### 安装步骤

1. **克隆仓库**
```bash
git clone https://github.com/tuoro/Smart-bill-manager.git
cd Smart-bill-manager
```

2. **安装后端依赖并运行**
```bash
cd backend-go
go mod download
go run ./cmd/server
```

3. **安装前端依赖**
```bash
cd ../frontend
npm install
```

4. **启动前端开发服务器**
```bash
npm run dev
```

5. **访问应用**
打开浏览器访问 http://localhost:5173

## 📧 QQ邮箱配置说明

1. 登录QQ邮箱网页版
2. 进入「设置」→「账户」
3. 找到「IMAP/SMTP服务」并开启
4. 点击「生成授权码」
5. 在系统中添加邮箱配置：
   - 邮箱地址：你的QQ邮箱
   - IMAP服务器：imap.qq.com
   - 端口：993
   - 密码：**使用授权码，不是QQ密码**

## 📁 项目结构

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
│   ├── go.mod / go.sum
│   └── ...
├── frontend/                    # Vue 前端
│   ├── src/
│   │   ├── main.ts / App.vue    # 入口与根组件
│   │   ├── router/              # 路由
│   │   ├── stores/              # Pinia
│   │   ├── views/               # 页面
│   │   ├── components/          # 复用组件
│   │   ├── api/                 # API 封装
│   │   └── types/               # TS 类型
│   ├── public/                  # 静态资源
│   ├── Dockerfile / nginx.conf  # 前端独立部署
│   └── ...
├── scripts/                     # 辅助脚本
│   ├── ocr_cli.py               # 调用 RapidOCR（含模型自动下载/校验）
│   ├── pdf_text_cli.py          # PDF 文本提取调试
│   └── install_ocr.sh           # OCR 依赖安装脚本
├── default_models.yaml          # RapidOCR 默认模型列表（含哈希校验）
├── Dockerfile                   # 前后端统一镜像
├── docker-compose.yml           # Compose 部署
├── nginx.conf                   # 统一 Nginx 配置
├── supervisord.conf             # 进程管理配置
└── README.md
```

## 🔑 API 接口

### 支付记录
- `GET /api/payments` - 获取支付记录列表
- `GET /api/payments/stats` - 获取统计数据
- `GET /api/payments/:id` - 获取支付记录详情
- `GET /api/payments/:id/invoices` - **🆕 获取关联的发票列表** ✨
- `POST /api/payments` - 创建支付记录
- `POST /api/payments/upload-screenshot` - **🆕 上传支付截图并OCR识别** ✨（返回草稿 `payment` + `extracted` + `dedup`）
- `POST /api/payments/upload-screenshot/cancel` - 取消上传（删除草稿文件）
- `PUT /api/payments/:id` - 更新支付记录（`confirm=true` 确认保存；`force_duplicate_save=true` 强制保存疑似重复）
- `DELETE /api/payments/:id` - 删除支付记录

### 发票管理
- `GET /api/invoices` - 获取发票列表
- `GET /api/invoices/:id` - 获取发票详情
- `GET /api/invoices/:id/download` - 下载原文件
- `GET /api/invoices/:id/linked-payments` - **🆕 获取关联的支付记录** ✨
- `GET /api/invoices/:id/suggest-payments` - **🆕 智能匹配支付记录建议** ✨
- `POST /api/invoices/upload` - 上传发票（自动OCR识别，返回草稿 `invoice` + `dedup`）
- `POST /api/invoices/upload-multiple` - 批量上传
- `POST /api/invoices/:id/link-payment` - **🆕 关联发票到支付记录** ✨
- `POST /api/invoices/:id/parse` - 重新解析 OCR/PDF
- `PUT /api/invoices/:id` - 更新发票（`confirm=true` 确认保存；`force_duplicate_save=true` 强制保存疑似重复）
- `DELETE /api/invoices/:id` - 删除发票
- `DELETE /api/invoices/:id/unlink-payment` - **🆕 取消关联** ✨

### 邮箱配置
- `GET /api/email/configs` - 获取邮箱配置
- `POST /api/email/configs` - 添加邮箱配置
- `POST /api/email/test` - 测试连接
- `POST /api/email/monitor/start/:id` - 启动监控
- `POST /api/email/monitor/stop/:id` - 停止监控
- `POST /api/email/check/:id` - 手动检查邮件

## 📸 界面预览

系统提供美观的可视化界面，包括：

1. **仪表盘** - 数据概览，支出趋势图表
2. **支付记录** - 表格展示，支持筛选和统计
3. **发票管理** - 拖拽上传，自动解析
4. **邮箱监控** - 配置管理，实时状态

## 📚 文档

本文档已包含日常使用所需的主要说明。

## 📝 License

MIT License - 详见 [LICENSE](LICENSE) 文件
