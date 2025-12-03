package handlers

import (
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type PaymentHandler struct {
	paymentService *services.PaymentService
	uploadsDir     string
}

func NewPaymentHandler(paymentService *services.PaymentService) *PaymentHandler {
	return &PaymentHandler{
		paymentService: paymentService,
		uploadsDir:     "", // Will be set if needed
	}
}

func (h *PaymentHandler) SetUploadsDir(uploadsDir string) {
	h.uploadsDir = uploadsDir
}

func (h *PaymentHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.GetAll)
	r.GET("/stats", h.GetStats)
	r.GET("/:id", h.GetByID)
	r.GET("/:id/invoices", h.GetLinkedInvoices)
	r.POST("", h.Create)
	r.POST("/upload-screenshot", h.UploadScreenshot)
	r.PUT("/:id", h.Update)
	r.DELETE("/:id", h.Delete)
}

func (h *PaymentHandler) GetAll(c *gin.Context) {
	var filter services.PaymentFilterInput
	if err := c.ShouldBindQuery(&filter); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	payments, err := h.paymentService.GetAll(filter)
	if err != nil {
		utils.Error(c, 500, "获取支付记录失败", err)
		return
	}

	utils.SuccessData(c, payments)
}

func (h *PaymentHandler) GetStats(c *gin.Context) {
	startDate := c.Query("startDate")
	endDate := c.Query("endDate")

	stats, err := h.paymentService.GetStats(startDate, endDate)
	if err != nil {
		utils.Error(c, 500, "获取统计数据失败", err)
		return
	}

	utils.SuccessData(c, stats)
}

func (h *PaymentHandler) GetByID(c *gin.Context) {
	id := c.Param("id")
	payment, err := h.paymentService.GetByID(id)
	if err != nil {
		utils.Error(c, 404, "支付记录不存在", nil)
		return
	}

	utils.SuccessData(c, payment)
}

func (h *PaymentHandler) Create(c *gin.Context) {
	var input services.CreatePaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "金额和交易时间是必填项", err)
		return
	}

	payment, err := h.paymentService.Create(input)
	if err != nil {
		utils.Error(c, 500, "创建支付记录失败", err)
		return
	}

	utils.Success(c, 201, "支付记录创建成功", payment)
}

func (h *PaymentHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input services.UpdatePaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	if err := h.paymentService.Update(id, input); err != nil {
		utils.Error(c, 404, "支付记录不存在或更新失败", err)
		return
	}

	utils.Success(c, 200, "支付记录更新成功", nil)
}

func (h *PaymentHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.paymentService.Delete(id); err != nil {
		utils.Error(c, 404, "支付记录不存在", nil)
		return
	}

	utils.Success(c, 200, "支付记录删除成功", nil)
}

func (h *PaymentHandler) UploadScreenshot(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, 400, "请上传文件", err)
		return
	}

	// Check file type (jpg, jpeg, png)
	ext := filepath.Ext(file.Filename)
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		utils.Error(c, 400, "只支持 JPG、JPEG、PNG 格式的图片", nil)
		return
	}

	// Check file size (10MB)
	if file.Size > 10*1024*1024 {
		utils.Error(c, 400, "文件大小不能超过10MB", nil)
		return
	}

	// Ensure uploads directory exists
	uploadsDir := h.uploadsDir
	if uploadsDir == "" {
		uploadsDir = "uploads"
	}
	if !filepath.IsAbs(uploadsDir) {
		wd, _ := os.Getwd()
		uploadsDir = filepath.Join(wd, uploadsDir)
	}

	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		utils.Error(c, 500, "创建上传目录失败", err)
		return
	}

	// Generate unique filename
	filename := utils.GenerateUUID() + ext
	filePath := filepath.Join(uploadsDir, filename)

	// Save file
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		utils.Error(c, 500, "保存文件失败", err)
		return
	}

	// Create relative path for database storage
	// Always use forward slashes and "uploads/" prefix for consistency
	relPath := "uploads/" + filename

	// Process screenshot with OCR
	payment, extracted, err := h.paymentService.CreateFromScreenshot(services.CreateFromScreenshotInput{
		ScreenshotPath: relPath,
	})
	if err != nil {
		// Clean up the uploaded file on error
		_ = os.Remove(filePath)
		utils.Error(c, 500, "识别支付截图失败", err)
		return
	}

	utils.Success(c, 201, "支付截图上传成功", gin.H{
		"payment":  payment,
		"extracted": extracted,
	})
}

func (h *PaymentHandler) GetLinkedInvoices(c *gin.Context) {
	id := c.Param("id")
	invoices, err := h.paymentService.GetLinkedInvoices(id)
	if err != nil {
		utils.Error(c, 500, "获取关联发票失败", err)
		return
	}

	utils.SuccessData(c, invoices)
}
