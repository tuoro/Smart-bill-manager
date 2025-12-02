package handlers

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

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
	r.GET("/payment/:paymentId", h.GetByPaymentID)
	r.POST("/upload", h.Upload)
	r.POST("/upload-multiple", h.UploadMultiple)
	r.PUT("/:id", h.Update)
	r.DELETE("/:id", h.Delete)
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
	c.Header("Content-Type", "application/pdf")
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
	if filepath.Ext(file.Filename) != ".pdf" {
		utils.Error(c, 400, "只支持PDF文件", nil)
		return
	}

	// Check file size (10MB)
	if file.Size > 10*1024*1024 {
		utils.Error(c, 400, "文件大小不能超过10MB", nil)
		return
	}

	// Generate unique filename
	filename := utils.GenerateUUID() + ".pdf"
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
		if filepath.Ext(file.Filename) != ".pdf" {
			continue
		}

		// Check file size (10MB)
		if file.Size > 10*1024*1024 {
			continue
		}

		// Generate unique filename
		filename := utils.GenerateUUID() + ".pdf"
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
