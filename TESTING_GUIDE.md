# Smart Bill Manager - 测试指南

## 新功能测试说明

### 1. 环境准备

确保系统已安装 Tesseract OCR:
```bash
# 检查 Tesseract 版本
tesseract --version

# 检查可用语言
tesseract --list-langs
```

应该看到 `chi_sim` (中文简体) 和 `eng` (英文) 在列表中。

### 2. 支付截图识别测试

#### 准备测试数据
创建或使用真实的支付截图，应包含以下信息：
- 金额
- 商家/收款方
- 支付时间
- 支付方式（微信/支付宝/银行）

#### 测试步骤
1. 启动服务器
2. 登录获取token
3. 上传支付截图:
```bash
curl -X POST http://localhost:3001/api/payments/upload-screenshot \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@/path/to/payment_screenshot.jpg"
```

#### 预期结果
- 返回状态码 201
- 响应包含 `payment` 对象和 `extracted` 对象
- `extracted` 对象包含识别的信息
- 金额、商家、时间等字段应正确识别（允许部分偏差）

#### 支持的支付平台测试

**微信支付截图特征：**
- 包含 "微信支付"、"支付成功"、"转账成功" 等关键字
- 金额格式：¥123.45
- 应识别：收款方、支付时间、交易单号

**支付宝截图特征：**
- 包含 "支付宝"、"付款成功" 等关键字
- 金额格式：¥123.45 或 123.45元
- 应识别：商家、创建时间、订单号

**银行转账截图特征：**
- 包含 "银行"、"转账"、"交易成功" 等关键字
- 应识别：收款人、转账金额、转账时间

### 3. PDF发票识别测试

#### 准备测试数据
使用真实的中文电子发票PDF，应包含：
- 发票号码
- 开票日期
- 金额
- 税额
- 销售方
- 购买方

#### 测试步骤
```bash
curl -X POST http://localhost:3001/api/invoices/upload \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@/path/to/invoice.pdf"
```

#### 预期结果
- 返回状态码 201
- 自动提取发票号码、日期、金额等信息
- 如果是文本型PDF，应能识别大部分字段
- 如果是扫描件PDF，会尝试OCR但可能准确度较低

### 4. 发票-支付记录关联测试

#### 测试步骤

**4.1 上传发票和支付记录**
```bash
# 1. 上传发票
INVOICE_ID=$(curl -X POST http://localhost:3001/api/invoices/upload \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@invoice.pdf" | jq -r '.data.id')

# 2. 上传支付截图
PAYMENT_ID=$(curl -X POST http://localhost:3001/api/payments/upload-screenshot \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@payment.jpg" | jq -r '.data.payment.id')
```

**4.2 获取智能匹配建议**
```bash
curl -X GET "http://localhost:3001/api/invoices/$INVOICE_ID/suggest-payments" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

**4.3 关联发票和支付记录**
```bash
curl -X POST "http://localhost:3001/api/invoices/$INVOICE_ID/link-payment" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"payment_id\": \"$PAYMENT_ID\"}"
```

**4.4 查询关联关系**
```bash
# 查询发票的关联支付
curl -X GET "http://localhost:3001/api/invoices/$INVOICE_ID/linked-payments" \
  -H "Authorization: Bearer YOUR_TOKEN"

# 查询支付的关联发票
curl -X GET "http://localhost:3001/api/payments/$PAYMENT_ID/invoices" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

**4.5 取消关联**
```bash
curl -X DELETE "http://localhost:3001/api/invoices/$INVOICE_ID/unlink-payment?payment_id=$PAYMENT_ID" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 5. Docker 测试

#### 构建镜像
```bash
cd /path/to/Smart-bill-manager
docker build -t smart-bill-manager:test .
```

#### 运行容器
```bash
docker run -d \
  --name smart-bill-test \
  -p 8080:80 \
  -v $(pwd)/test-data:/app/backend/data \
  -v $(pwd)/test-uploads:/app/backend/uploads \
  smart-bill-manager:test
```

#### 验证OCR可用性
```bash
# 进入容器
docker exec -it smart-bill-test sh

# 检查 Tesseract
tesseract --version
tesseract --list-langs

# 退出
exit
```

#### 测试上传功能
在容器运行后，使用上述API测试命令，将端口改为8080。

### 6. 性能测试

#### 单个文件测试
```bash
# 记录开始时间
time curl -X POST http://localhost:3001/api/payments/upload-screenshot \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@payment.jpg"
```

预期：
- 小图片（<1MB）：1-3秒
- 大图片（1-5MB）：2-5秒
- PDF文件（1-5MB）：3-10秒

#### 批量测试
创建脚本上传多个文件，观察：
- 并发处理能力
- 内存使用情况
- 响应时间变化

### 7. 准确度测试

#### OCR准确度评估
使用5-10个不同来源的支付截图和发票，记录：
- 识别成功率
- 字段准确率（金额、日期、商家等）
- 常见错误类型

#### 预期准确度
- **清晰截图**: 80-95%准确率
- **模糊截图**: 50-70%准确率
- **文本PDF**: 90-99%准确率
- **扫描PDF**: 60-80%准确率

### 8. 错误处理测试

#### 测试场景
1. **上传非图片文件**
   ```bash
   curl -X POST http://localhost:3001/api/payments/upload-screenshot \
     -H "Authorization: Bearer YOUR_TOKEN" \
     -F "file=@document.txt"
   ```
   预期：400错误，提示只支持图片格式

2. **上传超大文件**
   ```bash
   # 创建一个>10MB的文件测试
   ```
   预期：400错误，提示文件过大

3. **关联不存在的记录**
   ```bash
   curl -X POST "http://localhost:3001/api/invoices/invalid-id/link-payment" \
     -H "Authorization: Bearer YOUR_TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"payment_id": "invalid-id"}'
   ```
   预期：404或500错误

4. **无文字的图片**
   上传纯色图片或无文字图片
   预期：成功上传，但extracted字段为空或最小值

### 9. 已知限制

1. **扫描PDF**: 目前扫描PDF的OCR功能未完全实现，建议先转换为图片
2. **复杂背景**: 背景复杂的截图可能影响识别准确度
3. **手写文字**: Tesseract对手写文字识别效果较差
4. **特殊格式**: 某些特殊格式的发票可能识别不完整

### 10. 故障排查

#### OCR识别失败
1. 检查Tesseract是否正确安装：`tesseract --version`
2. 检查语言包：`tesseract --list-langs`
3. 查看服务器日志了解详细错误
4. 确认图片格式和大小符合要求

#### 文件上传失败
1. 检查uploads目录权限
2. 确认磁盘空间充足
3. 验证文件大小<10MB
4. 检查网络连接

#### 关联失败
1. 确认发票和支付记录都已创建
2. 检查ID是否正确
3. 查看是否已存在相同的关联

### 11. 数据清理

测试完成后清理数据：
```bash
# 删除测试支付记录
curl -X DELETE "http://localhost:3001/api/payments/$PAYMENT_ID" \
  -H "Authorization: Bearer YOUR_TOKEN"

# 删除测试发票
curl -X DELETE "http://localhost:3001/api/invoices/$INVOICE_ID" \
  -H "Authorization: Bearer YOUR_TOKEN"

# 或直接删除数据库和uploads目录
rm -rf data/*.db uploads/*
```

### 12. 下一步开发建议

1. 添加前端界面集成
2. 实现批量处理功能
3. 添加OCR结果人工修正界面
4. 优化OCR性能（并行处理、缓存等）
5. 添加更多支付平台支持
6. 实现PDF扫描件的图像预处理增强
