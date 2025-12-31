package handlers

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

func contentTypeFromInvoiceFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}

func isAllowedInvoiceExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".pdf", ".png", ".jpg", ".jpeg":
		return true
	default:
		return false
	}
}

type InvoiceHandler struct {
	invoiceService *services.InvoiceService
	uploadsDir     string
}

func NewInvoiceHandler(invoiceService *services.InvoiceService, uploadsDir string) *InvoiceHandler {
	return &InvoiceHandler{
		invoiceService: invoiceService,
		uploadsDir:     uploadsDir,
	}
}

func (h *InvoiceHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.GetAll)
	r.GET("/stats", h.GetStats)
	r.GET("/:id", h.GetByID)
	r.GET("/:id/download", h.Download)
	r.GET("/:id/linked-payments", h.GetLinkedPayments)
	r.GET("/:id/suggest-payments", h.SuggestPayments)
	r.GET("/payment/:paymentId", h.GetByPaymentID)
	r.POST("/upload", h.Upload)
	r.POST("/upload-multiple", h.UploadMultiple)
	r.POST("/:id/link-payment", h.LinkPayment)
	r.POST("/:id/parse", h.Parse)
	r.PUT("/:id", h.Update)
	r.DELETE("/:id", h.Delete)
	r.DELETE("/:id/unlink-payment", h.UnlinkPayment)
}

func (h *InvoiceHandler) GetAll(c *gin.Context) {
	var filter services.InvoiceFilterInput
	if err := c.ShouldBindQuery(&filter); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	invoices, err := h.invoiceService.GetAll(filter)
	if err != nil {
		utils.Error(c, 500, "获取发票列表失败", err)
		return
	}

	utils.SuccessData(c, invoices)
}

func (h *InvoiceHandler) GetStats(c *gin.Context) {
	stats, err := h.invoiceService.GetStats()
	if err != nil {
		utils.Error(c, 500, "获取统计数据失败", err)
		return
	}

	utils.SuccessData(c, stats)
}

func (h *InvoiceHandler) GetByID(c *gin.Context) {
	id := c.Param("id")
	invoice, err := h.invoiceService.GetByID(id)
	if err != nil {
		utils.Error(c, 404, "发票不存在", nil)
		return
	}

	utils.SuccessData(c, invoice)
}

func (h *InvoiceHandler) Download(c *gin.Context) {
	id := c.Param("id")
	invoice, err := h.invoiceService.GetByID(id)
	if err != nil {
		utils.Error(c, 404, "发票不存在", nil)
		return
	}

	filePath := invoice.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(h.uploadsDir, "..", filePath)
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		utils.Error(c, 404, "文件不存在", nil)
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+invoice.OriginalName)
	c.Header("Content-Type", contentTypeFromInvoiceFilename(invoice.Filename))
	c.File(filePath)
}

func (h *InvoiceHandler) GetByPaymentID(c *gin.Context) {
	paymentID := c.Param("paymentId")
	invoices, err := h.invoiceService.GetByPaymentID(paymentID)
	if err != nil {
		utils.Error(c, 500, "获取发票失败", err)
		return
	}

	utils.SuccessData(c, invoices)
}

func (h *InvoiceHandler) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, 400, "请上传文件", err)
		return
	}

	// Check file type
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !isAllowedInvoiceExt(ext) {
		utils.Error(c, 400, "只支持PDF或图片格式(PNG/JPG)", nil)
		return
	}

	// Check file size (20MB)
	if file.Size > 20*1024*1024 {
		utils.Error(c, 400, "文件大小不能超过20MB", nil)
		return
	}

	// Generate unique filename
	filename := utils.GenerateUUID() + ext
	filePath := filepath.Join(h.uploadsDir, filename)

	// Ensure uploads directory exists
	if err := os.MkdirAll(h.uploadsDir, 0755); err != nil {
		utils.Error(c, 500, "创建上传目录失败", err)
		return
	}

	// Save file
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		utils.Error(c, 500, "保存文件失败", err)
		return
	}

	// Create invoice record
	var paymentID *string
	if pid := c.PostForm("payment_id"); pid != "" {
		paymentID = &pid
	}

	invoice, err := h.invoiceService.Create(services.CreateInvoiceInput{
		PaymentID:    paymentID,
		Filename:     filename,
		OriginalName: file.Filename,
		FilePath:     "uploads/" + filename,
		FileSize:     file.Size,
		Source:       "upload",
		IsDraft:      true,
	})
	if err != nil {
		utils.Error(c, 500, "上传发票失败", err)
		return
	}

	utils.Success(c, 201, "发票上传成功", invoice)
}

func (h *InvoiceHandler) UploadMultiple(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		utils.Error(c, 400, "请上传文件", err)
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		utils.Error(c, 400, "请上传文件", nil)
		return
	}

	if len(files) > 10 {
		utils.Error(c, 400, "最多同时上传10个文件", nil)
		return
	}

	// Ensure uploads directory exists
	if err := os.MkdirAll(h.uploadsDir, 0755); err != nil {
		utils.Error(c, 500, "创建上传目录失败", err)
		return
	}

	var paymentID *string
	if pid := c.PostForm("payment_id"); pid != "" {
		paymentID = &pid
	}

	var invoices []interface{}
	for _, file := range files {
		// Check file type
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if !isAllowedInvoiceExt(ext) {
			continue
		}

		// Check file size (20MB)
		if file.Size > 20*1024*1024 {
			continue
		}

		// Generate unique filename
		filename := utils.GenerateUUID() + ext
		filePath := filepath.Join(h.uploadsDir, filename)

		// Save file
		if err := c.SaveUploadedFile(file, filePath); err != nil {
			continue
		}

		// Create invoice record
		invoice, err := h.invoiceService.Create(services.CreateInvoiceInput{
			PaymentID:    paymentID,
			Filename:     filename,
			OriginalName: file.Filename,
			FilePath:     "uploads/" + filename,
			FileSize:     file.Size,
			Source:       "upload",
			IsDraft:      true,
		})
		if err != nil {
			continue
		}

		invoices = append(invoices, invoice)
	}

	utils.Success(c, 201, fmt.Sprintf("成功上传 %d 个发票", len(invoices)), invoices)
}

func (h *InvoiceHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input services.UpdateInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	if err := h.invoiceService.Update(id, input); err != nil {
		utils.Error(c, 404, "发票不存在或更新失败", err)
		return
	}

	utils.Success(c, 200, "发票更新成功", nil)
}

func (h *InvoiceHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.invoiceService.Delete(id); err != nil {
		utils.Error(c, 404, "发票不存在", nil)
		return
	}

	utils.Success(c, 200, "发票删除成功", nil)
}

func (h *InvoiceHandler) LinkPayment(c *gin.Context) {
	id := c.Param("id")

	var input struct {
		PaymentID string `json:"payment_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	if err := h.invoiceService.LinkPayment(id, input.PaymentID); err != nil {
		utils.Error(c, 500, "关联支付记录失败", err)
		return
	}

	utils.Success(c, 200, "关联支付记录成功", nil)
}

func (h *InvoiceHandler) UnlinkPayment(c *gin.Context) {
	id := c.Param("id")
	paymentID := c.Query("payment_id")

	if paymentID == "" {
		utils.Error(c, 400, "缺少 payment_id 参数", nil)
		return
	}

	if err := h.invoiceService.UnlinkPayment(id, paymentID); err != nil {
		utils.Error(c, 500, "取消关联失败", err)
		return
	}

	utils.Success(c, 200, "取消关联成功", nil)
}

func (h *InvoiceHandler) GetLinkedPayments(c *gin.Context) {
	id := c.Param("id")

	payments, err := h.invoiceService.GetLinkedPayments(id)
	if err != nil {
		utils.Error(c, 500, "获取关联支付记录失败", err)
		return
	}

	utils.SuccessData(c, payments)
}

func (h *InvoiceHandler) SuggestPayments(c *gin.Context) {
	id := c.Param("id")
	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	debug := c.Query("debug") == "1" || c.Query("debug") == "true"

	log.Printf("[MATCH] suggest-payments invoice_id=%s limit=%d debug=%t", id, limit, debug)

	payments, err := h.invoiceService.SuggestPayments(id, limit, debug)
	if err != nil {
		utils.Error(c, 500, "获取建议支付记录失败", err)
		return
	}

	log.Printf("[MATCH] suggest-payments invoice_id=%s -> %d results", id, len(payments))
	utils.SuccessData(c, payments)
}

func (h *InvoiceHandler) Parse(c *gin.Context) {
	id := c.Param("id")

	invoice, err := h.invoiceService.Reparse(id)
	if err != nil {
		utils.Error(c, 500, "解析发票失败", err)
		return
	}

	utils.Success(c, 200, "发票解析完成", invoice)
}
