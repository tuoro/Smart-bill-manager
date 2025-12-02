package handlers

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type DingtalkHandler struct {
	dingtalkService *services.DingtalkService
	invoiceService  *services.InvoiceService
	uploadsDir      string
}

func NewDingtalkHandler(dingtalkService *services.DingtalkService, invoiceService *services.InvoiceService, uploadsDir string) *DingtalkHandler {
	return &DingtalkHandler{
		dingtalkService: dingtalkService,
		invoiceService:  invoiceService,
		uploadsDir:      uploadsDir,
	}
}

func (h *DingtalkHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/configs", h.GetAllConfigs)
	r.POST("/configs", h.CreateConfig)
	r.PUT("/configs/:id", h.UpdateConfig)
	r.DELETE("/configs/:id", h.DeleteConfig)
	r.GET("/logs", h.GetLogs)
	r.POST("/webhook", h.Webhook)
	r.POST("/webhook/:configId", h.WebhookWithConfig)
	r.POST("/upload", h.Upload)
	r.POST("/download-url", h.DownloadURL)
}

func (h *DingtalkHandler) GetAllConfigs(c *gin.Context) {
	configs, err := h.dingtalkService.GetAllConfigs()
	if err != nil {
		utils.Error(c, 500, "获取钉钉配置失败", err)
		return
	}

	utils.SuccessData(c, configs)
}

func (h *DingtalkHandler) CreateConfig(c *gin.Context) {
	var input services.CreateDingtalkConfigInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "配置名称不能为空", err)
		return
	}

	config, err := h.dingtalkService.CreateConfig(input)
	if err != nil {
		utils.Error(c, 500, "创建钉钉配置失败", err)
		return
	}

	utils.Success(c, 201, "钉钉配置创建成功", config.ToResponse())
}

func (h *DingtalkHandler) UpdateConfig(c *gin.Context) {
	id := c.Param("id")
	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	if err := h.dingtalkService.UpdateConfig(id, data); err != nil {
		utils.Error(c, 404, "配置不存在或更新失败", err)
		return
	}

	utils.Success(c, 200, "配置更新成功", nil)
}

func (h *DingtalkHandler) DeleteConfig(c *gin.Context) {
	id := c.Param("id")
	if err := h.dingtalkService.DeleteConfig(id); err != nil {
		utils.Error(c, 404, "配置不存在", nil)
		return
	}

	utils.Success(c, 200, "配置删除成功", nil)
}

func (h *DingtalkHandler) GetLogs(c *gin.Context) {
	configID := c.Query("configId")
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	logs, err := h.dingtalkService.GetLogs(configID, limit)
	if err != nil {
		utils.Error(c, 500, "获取消息日志失败", err)
		return
	}

	utils.SuccessData(c, logs)
}

func (h *DingtalkHandler) Webhook(c *gin.Context) {
	timestamp := c.GetHeader("timestamp")
	sign := c.GetHeader("sign")

	// Get active configuration
	config, err := h.dingtalkService.GetActiveConfig()
	if err != nil {
		c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "服务未配置"}})
		return
	}

	// Verify signature if webhook_token is configured
	if config.WebhookToken != nil && *config.WebhookToken != "" {
		if !h.dingtalkService.VerifySignature(timestamp, sign, *config.WebhookToken) {
			c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "签名验证失败"}})
			return
		}
	}

	var message models.DingtalkMessage
	if err := c.ShouldBindJSON(&message); err != nil {
		c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "消息解析失败"}})
		return
	}

	// Process message
	response, _, err := h.dingtalkService.ProcessWebhookMessage(&message, config.ID)
	if err != nil {
		c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "处理消息时发生错误"}})
		return
	}

	// Send response to session webhook if available
	if message.SessionWebhook != "" && response != nil {
		go h.dingtalkService.SendResponse(message.SessionWebhook, response)
	}

	if response != nil {
		c.JSON(200, response)
	} else {
		c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "消息已收到"}})
	}
}

func (h *DingtalkHandler) WebhookWithConfig(c *gin.Context) {
	configID := c.Param("configId")
	timestamp := c.GetHeader("timestamp")
	sign := c.GetHeader("sign")

	config, err := h.dingtalkService.GetConfigByID(configID)
	if err != nil || config.IsActive == 0 {
		c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "配置不存在或已禁用"}})
		return
	}

	// Verify signature if webhook_token is configured
	if config.WebhookToken != nil && *config.WebhookToken != "" {
		if !h.dingtalkService.VerifySignature(timestamp, sign, *config.WebhookToken) {
			c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "签名验证失败"}})
			return
		}
	}

	var message models.DingtalkMessage
	if err := c.ShouldBindJSON(&message); err != nil {
		c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "消息解析失败"}})
		return
	}

	// Process message
	response, _, err := h.dingtalkService.ProcessWebhookMessage(&message, configID)
	if err != nil {
		c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "处理消息时发生错误"}})
		return
	}

	// Send response to session webhook if available
	if message.SessionWebhook != "" && response != nil {
		go h.dingtalkService.SendResponse(message.SessionWebhook, response)
	}

	if response != nil {
		c.JSON(200, response)
	} else {
		c.JSON(200, gin.H{"msgtype": "text", "text": gin.H{"content": "消息已收到"}})
	}
}

func (h *DingtalkHandler) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, 400, "请上传PDF文件", err)
		return
	}

	// Check file type
	if filepath.Ext(file.Filename) != ".pdf" {
		utils.Error(c, 400, "只支持PDF文件", nil)
		return
	}

	// Check file size (20MB for DingTalk)
	if file.Size > 20*1024*1024 {
		utils.Error(c, 400, "文件大小不能超过20MB", nil)
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
		Source:       "dingtalk",
	})
	if err != nil {
		utils.Error(c, 500, "上传发票失败", err)
		return
	}

	utils.Success(c, 201, "发票上传成功", invoice)
}

type DownloadURLInput struct {
	URL      string `json:"url" binding:"required"`
	FileName string `json:"fileName"`
}

func (h *DingtalkHandler) DownloadURL(c *gin.Context) {
	var input DownloadURLInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "请提供文件URL", err)
		return
	}

	fileName := input.FileName
	if fileName == "" {
		fileName = "invoice.pdf"
	}

	config, _ := h.dingtalkService.GetActiveConfig()
	configID := "manual"
	if config != nil {
		configID = config.ID
	}

	if err := h.dingtalkService.DownloadFromURL(input.URL, fileName, configID); err != nil {
		utils.Error(c, 500, "下载文件失败", err)
		return
	}

	utils.Success(c, 200, "文件下载并处理成功", nil)
}
