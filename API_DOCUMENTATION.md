# Smart Bill Manager - API 文档

## 新增功能 API

### 1. 支付截图上传和识别

#### POST `/api/payments/upload-screenshot`

上传支付截图并自动识别支付信息。

**请求参数:**
- `file`: 图片文件 (multipart/form-data)
  - 支持格式: JPG, JPEG, PNG
  - 最大大小: 10MB

**响应示例:**
```json
{
  "success": true,
  "code": 201,
  "message": "支付截图上传成功",
  "data": {
    "payment": {
      "id": "uuid",
      "amount": 123.45,
      "merchant": "测试商家",
      "payment_method": "微信支付",
      "transaction_time": "2024-01-01T12:00:00Z",
      "screenshot_path": "uploads/screenshot.jpg",
      "extracted_data": "{...}"
    },
    "extracted": {
      "amount": 123.45,
      "merchant": "测试商家",
      "transaction_time": "2024-01-01 12:00:00",
      "payment_method": "微信支付",
      "order_number": "123456789",
      "raw_text": "..."
    }
  }
}
```

**支持的支付平台:**
- 微信支付 - 自动识别金额、收款方、支付时间、交易单号
- 支付宝 - 自动识别金额、商家、创建时间、订单号
- 银行转账 - 自动识别金额、收款人、转账时间

---

### 2. 发票与支付记录关联

#### POST `/api/invoices/:id/link-payment`

将发票关联到支付记录。

**请求参数:**
```json
{
  "payment_id": "payment-uuid"
}
```

**响应示例:**
```json
{
  "success": true,
  "code": 200,
  "message": "关联支付记录成功"
}
```

---

#### DELETE `/api/invoices/:id/unlink-payment?payment_id=xxx`

取消发票与支付记录的关联。

**Query 参数:**
- `payment_id`: 支付记录ID

**响应示例:**
```json
{
  "success": true,
  "code": 200,
  "message": "取消关联成功"
}
```

---

#### GET `/api/invoices/:id/linked-payments`

获取发票关联的所有支付记录。

**响应示例:**
```json
{
  "success": true,
  "data": [
    {
      "id": "payment-uuid",
      "amount": 123.45,
      "merchant": "测试商家",
      "transaction_time": "2024-01-01T12:00:00Z"
    }
  ]
}
```

---

#### GET `/api/invoices/:id/suggest-payments`

智能推荐可能匹配的支付记录（根据金额和日期）。

**响应示例:**
```json
{
  "success": true,
  "data": [
    {
      "id": "payment-uuid",
      "amount": 123.50,
      "merchant": "相似商家",
      "transaction_time": "2024-01-01T12:00:00Z"
    }
  ]
}
```

**匹配规则:**
- 金额在发票金额的±10%范围内
- 优先匹配日期相近的支付记录
- 最多返回10条建议

---

#### GET `/api/payments/:id/invoices`

获取支付记录关联的所有发票。

**响应示例:**
```json
{
  "success": true,
  "data": [
    {
      "id": "invoice-uuid",
      "invoice_number": "12345678",
      "amount": 123.45,
      "invoice_date": "2024-01-01",
      "seller_name": "销售方名称"
    }
  ]
}
```

---

### 3. 改进的PDF发票识别

发票上传接口保持不变 (`POST /api/invoices/upload`)，但现在支持：

**新增识别字段:**
- `tax_amount` - 税额
- `parse_status` - 解析状态 (pending/parsing/success/failed)
- `parse_error` - 解析错误信息
- `raw_text` - OCR提取的原始文本
- 更精确的金额识别
- 支持更多发票格式（增值税电子普通发票、增值税电子专用发票等）

**使用专业PDF解析库:**
- 使用 `ledongthuc/pdf` 进行文本提取
- 使用 Tesseract OCR 处理扫描件PDF
- 支持中英文混合识别

**响应示例:**
```json
{
  "success": true,
  "code": 201,
  "message": "发票上传成功",
  "data": {
    "id": "invoice-uuid",
    "invoice_number": "12345678",
    "invoice_date": "2024年01月01日",
    "amount": 123.45,
    "tax_amount": 15.65,
    "seller_name": "销售方公司",
    "buyer_name": "购买方公司",
    "parse_status": "success",
    "parse_error": null,
    "raw_text": "发票原始文本...",
    "extracted_data": "{...}"
  }
}
```

#### POST `/api/invoices/:id/parse`

手动重新解析发票。适用于以下场景：
- 首次上传解析失败，需要重新尝试
- OCR服务升级后重新识别旧发票
- 调试发票识别问题

**请求参数:**
- `id`: 发票ID (URL路径参数)

**响应示例:**
```json
{
  "success": true,
  "code": 200,
  "message": "发票解析完成",
  "data": {
    "id": "invoice-uuid",
    "invoice_number": "12345678",
    "invoice_date": "2024年01月01日",
    "amount": 123.45,
    "tax_amount": 15.65,
    "seller_name": "销售方公司",
    "buyer_name": "购买方公司",
    "parse_status": "success",
    "parse_error": null,
    "raw_text": "发票原始文本...",
    "extracted_data": "{...}"
  }
}
```

**解析状态说明:**
- `pending`: 等待解析
- `parsing`: 解析中
- `success`: 解析成功
- `failed`: 解析失败（查看 parse_error 字段了解失败原因）

---

## OCR 功能说明

### 支持的图片格式
- JPG / JPEG
- PNG

### 支持的语言
- 中文（简体）
- 英文

### 识别准确度
OCR识别准确度取决于：
1. 图片清晰度 - 建议使用高分辨率图片
2. 文字大小 - 文字需清晰可辨
3. 背景干扰 - 尽量使用纯色背景
4. 图片格式 - 推荐使用PNG格式

### 性能考虑
- 单次OCR识别通常在1-3秒内完成
- PDF识别时间取决于页数，每页约1-2秒
- 建议单个文件不超过10MB

---

## 数据模型更新

### Payment 模型新增字段
```go
type Payment struct {
    // ... 原有字段
    ScreenshotPath  *string   `json:"screenshot_path"`  // 截图路径
    ExtractedData   *string   `json:"extracted_data"`   // OCR识别的原始数据
}
```

### Invoice 模型新增字段
```go
type Invoice struct {
    // ... 原有字段
    TaxAmount     *float64  `json:"tax_amount"`  // 税额
}
```

### 新增关联表
```go
type InvoicePaymentLink struct {
    InvoiceID string    `json:"invoice_id"`
    PaymentID string    `json:"payment_id"`
    CreatedAt time.Time `json:"created_at"`
}
```

---

## 使用示例

### 1. 上传并识别支付截图

```bash
curl -X POST http://localhost:3001/api/payments/upload-screenshot \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@payment_screenshot.jpg"
```

### 2. 关联发票到支付记录

```bash
curl -X POST http://localhost:3001/api/invoices/invoice-id/link-payment \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"payment_id": "payment-id"}'
```

### 3. 获取智能匹配建议

```bash
curl -X GET http://localhost:3001/api/invoices/invoice-id/suggest-payments \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 4. 查看支付记录的关联发票

```bash
curl -X GET http://localhost:3001/api/payments/payment-id/invoices \
  -H "Authorization: Bearer YOUR_TOKEN"
```

---

## 错误处理

所有API在出错时返回统一格式：

```json
{
  "success": false,
  "code": 400,
  "message": "错误描述"
}
```

常见错误码：
- `400` - 参数错误
- `401` - 未授权
- `404` - 资源不存在
- `500` - 服务器内部错误

---

## 注意事项

1. **OCR识别准确性**: OCR技术并非100%准确，建议用户验证和修改识别结果
2. **文件大小限制**: 单个文件不超过10MB
3. **支持的格式**: 
   - 支付截图: JPG, JPEG, PNG
   - 发票: PDF
4. **数据安全**: 
   - 所有上传的文件都存储在服务器的uploads目录
   - 建议定期备份数据库和uploads目录
5. **性能优化**: 
   - 大批量文件建议分批上传
   - OCR操作为同步操作，可能需要几秒时间

---

## 技术栈

- **OCR引擎**: Tesseract 5.3.4
- **Go绑定**: gosseract v2
- **PDF解析**: ledongthuc/pdf
- **语言支持**: 中文简体(chi_sim), 英文(eng)
