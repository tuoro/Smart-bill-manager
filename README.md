# 智能账单管理系统 (Smart Bill Manager)

一个现代化的个人账单管理系统，支持支付记录管理、发票自动解析、邮箱实时监控和钉钉机器人接收。

## ✨ 功能特性

### 🔐 用户认证
- JWT令牌认证
- 安全的密码加密存储
- 首次启动自动创建管理员账户
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
  - OCR技术支持中英文识别

### 📄 发票管理
- PDF发票上传（支持批量）
- **🆕 智能发票识别** ✨
  - 自动解析发票号码、金额、税额、销售方、购买方
  - 支持增值税电子普通发票、增值税电子专用发票
  - 使用专业PDF解析库和OCR技术
- 发票预览和下载
- 来源追踪（手动上传/邮件下载/钉钉机器人）
- **🆕 发票与支付记录关联** ✨
  - 手动关联发票到支付记录
  - 智能匹配建议（基于金额和日期）
  - 查看关联关系

### 📬 邮箱监控
- **支持QQ邮箱** ✅
- 支持163、126、Gmail、Outlook等主流邮箱
- 实时监控新邮件
- 自动下载PDF附件
- 手动检查新邮件

### 🤖 钉钉机器人
- 支持钉钉群机器人接收发票
- 自动解析钉钉发送的PDF发票
- Webhook签名验证
- 消息日志记录

## 🛠️ 技术栈

### 后端
- Go 1.24 (Gin Web框架)
- SQLite (GORM ORM)
- JWT认证 (golang-jwt/jwt)
- golang.org/x/crypto/bcrypt (密码加密)
- emersion/go-imap (邮箱IMAP协议)
- **🆕 Tesseract OCR 5.3.4** (OCR识别引擎) ✨
- **🆕 gosseract v2** (Go语言Tesseract绑定) ✨
- **🆕 ledongthuc/pdf** (专业PDF解析) ✨
- gin-contrib/cors (CORS支持)
- 内置请求频率限制

### 前端
- Vue 3 + TypeScript + Composition API
- Vite (构建工具)
- Element Plus (UI组件库)
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
      - JWT_SECRET=your-secure-secret-key-here  # 可选：设置JWT密钥
      - ADMIN_PASSWORD=your-admin-password      # 可选：设置管理员密码
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
  -v smart-bill-data:/app/backend/data \
  -v smart-bill-uploads:/app/backend/uploads \
  smart-bill-manager
```

### 方式四：本地开发

#### 环境要求
- Go >= 1.21
- Node.js >= 18
- npm >= 8

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

## 🤖 钉钉机器人配置说明

通过钉钉机器人，您可以直接在钉钉群聊中发送PDF发票文件，系统将自动接收并解析。

### 配置步骤

1. **创建钉钉群机器人**
   - 在钉钉群设置中，选择「智能群助手」→「添加机器人」
   - 选择「自定义」机器人
   - 设置机器人名称（如：发票收集助手）
   - 安全设置选择「加签」，记录加签密钥

2. **配置系统**
   - 在系统中进入「钉钉机器人」页面
   - 点击「添加机器人」
   - 填写配置名称
   - 如需启用签名验证，填写加签密钥（Webhook Token）
   - 如需下载文件功能，需在钉钉开放平台创建应用并填写App Key和App Secret

3. **设置Webhook地址**
   - 创建配置后，点击复制Webhook URL按钮
   - 将复制的URL配置到钉钉机器人的「消息接收地址」

4. **使用方法**
   - 在钉钉群中@机器人，然后发送PDF发票文件
   - 系统将自动接收并解析发票信息

### 注意事项

- 确保服务器能被钉钉服务器访问（需要公网IP或内网穿透）
- 建议启用加签验证以确保安全
- 仅支持PDF格式的发票文件

## 📁 项目结构

```
Smart-bill-manager/
├── backend-go/              # Go 后端服务
│   ├── cmd/
│   │   └── server/
│   │       └── main.go      # 应用入口
│   ├── internal/
│   │   ├── config/          # 配置管理
│   │   ├── models/          # 数据模型
│   │   ├── handlers/        # HTTP 处理器
│   │   ├── services/        # 业务逻辑
│   │   ├── middleware/      # 中间件
│   │   ├── repository/      # 数据访问层
│   │   └── utils/           # 工具函数
│   ├── pkg/
│   │   └── database/        # 数据库连接
│   ├── go.mod
│   └── go.sum
├── frontend/               # 前端应用
│   ├── src/
│   │   ├── main.ts         # Vue 应用入口
│   │   ├── App.vue         # 根组件
│   │   ├── router/         # Vue Router 配置
│   │   ├── stores/         # Pinia 状态管理
│   │   ├── views/          # 页面视图
│   │   ├── components/     # 可复用组件
│   │   ├── api/            # API 服务
│   │   └── types/          # TypeScript类型
│   ├── public/             # 静态资源
│   ├── Dockerfile          # 前端单独 Docker 配置
│   └── nginx.conf          # 前端单独 Nginx 配置
├── Dockerfile              # 统一 Docker 配置（前后端合一）
├── nginx.conf              # 统一 Nginx 配置
├── supervisord.conf        # Supervisor 进程管理配置
├── docker-compose.yml      # Docker Compose 配置
└── README.md
```

## 🔑 API 接口

### 支付记录
- `GET /api/payments` - 获取支付记录列表
- `GET /api/payments/stats` - 获取统计数据
- `GET /api/payments/:id` - 获取支付记录详情
- `GET /api/payments/:id/invoices` - **🆕 获取关联的发票列表** ✨
- `POST /api/payments` - 创建支付记录
- `POST /api/payments/upload-screenshot` - **🆕 上传支付截图并OCR识别** ✨
- `PUT /api/payments/:id` - 更新支付记录
- `DELETE /api/payments/:id` - 删除支付记录

### 发票管理
- `GET /api/invoices` - 获取发票列表
- `GET /api/invoices/:id` - 获取发票详情
- `GET /api/invoices/:id/linked-payments` - **🆕 获取关联的支付记录** ✨
- `GET /api/invoices/:id/suggest-payments` - **🆕 智能匹配支付记录建议** ✨
- `POST /api/invoices/upload` - 上传发票（自动OCR识别）
- `POST /api/invoices/upload-multiple` - 批量上传
- `POST /api/invoices/:id/link-payment` - **🆕 关联发票到支付记录** ✨
- `PUT /api/invoices/:id` - 更新发票
- `DELETE /api/invoices/:id` - 删除发票
- `DELETE /api/invoices/:id/unlink-payment` - **🆕 取消关联** ✨

### 邮箱配置
- `GET /api/email/configs` - 获取邮箱配置
- `POST /api/email/configs` - 添加邮箱配置
- `POST /api/email/test` - 测试连接
- `POST /api/email/monitor/start/:id` - 启动监控
- `POST /api/email/monitor/stop/:id` - 停止监控
- `POST /api/email/check/:id` - 手动检查邮件

### 钉钉机器人
- `GET /api/dingtalk/configs` - 获取钉钉配置
- `POST /api/dingtalk/configs` - 添加钉钉配置
- `PUT /api/dingtalk/configs/:id` - 更新钉钉配置
- `DELETE /api/dingtalk/configs/:id` - 删除钉钉配置
- `GET /api/dingtalk/logs` - 获取消息日志
- `POST /api/dingtalk/webhook` - 接收钉钉机器人消息（Webhook）
- `POST /api/dingtalk/webhook/:configId` - 指定配置接收消息

## 📸 界面预览

系统提供美观的可视化界面，包括：

1. **仪表盘** - 数据概览，支出趋势图表
2. **支付记录** - 表格展示，支持筛选和统计
3. **发票管理** - 拖拽上传，自动解析
4. **邮箱监控** - 配置管理，实时状态

## 📚 文档

- [API 文档](./API_DOCUMENTATION.md) - 详细的API接口说明和使用示例
- [测试指南](./TESTING_GUIDE.md) - 功能测试步骤和性能评估方法

## 📝 License

MIT License - 详见 [LICENSE](LICENSE) 文件