package handlers

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type EmailHandler struct {
	emailService *services.EmailService
}

func NewEmailHandler(emailService *services.EmailService) *EmailHandler {
	return &EmailHandler{emailService: emailService}
}

func (h *EmailHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/configs", h.GetAllConfigs)
	r.POST("/configs", h.CreateConfig)
	r.PUT("/configs/:id", h.UpdateConfig)
	r.DELETE("/configs/:id", h.DeleteConfig)
	r.POST("/test", h.TestConnection)
	r.GET("/logs", h.GetLogs)
	r.POST("/logs/:id/parse", h.ParseLog)
	r.POST("/monitor/start/:id", h.StartMonitoring)
	r.POST("/monitor/stop/:id", h.StopMonitoring)
	r.GET("/monitor/status", h.GetMonitoringStatus)
	r.GET("/status", h.GetMonitoringStatus) // Alias
	r.POST("/check/:id", h.ManualCheck)
}

func (h *EmailHandler) GetAllConfigs(c *gin.Context) {
	ctx, cancel := withReadTimeout(c)
	defer cancel()

	configs, err := h.emailService.GetAllConfigsCtx(ctx, middleware.GetEffectiveUserID(c))
	if err != nil {
		if handleReadTimeoutError(c, err) {
			return
		}
		utils.Error(c, 500, "获取邮箱配置失败", err)
		return
	}

	utils.SuccessData(c, configs)
}

func (h *EmailHandler) CreateConfig(c *gin.Context) {
	var input services.CreateEmailConfigInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "邮箱地址、IMAP服务器和密码是必填项", err)
		return
	}

	// Test connection first
	success, message := h.emailService.TestConnection(input.Email, input.IMAPHost, input.IMAPPort, input.Password)
	if !success {
		utils.Error(c, 400, message, nil)
		return
	}

	config, err := h.emailService.CreateConfig(middleware.GetEffectiveUserID(c), input)
	if err != nil {
		utils.Error(c, 500, "创建邮箱配置失败", err)
		return
	}

	utils.Success(c, 201, "邮箱配置创建成功", config.ToResponse())
}

func (h *EmailHandler) UpdateConfig(c *gin.Context) {
	id := c.Param("id")
	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	if err := h.emailService.UpdateConfig(middleware.GetEffectiveUserID(c), id, data); err != nil {
		utils.Error(c, 404, "邮箱配置不存在或更新失败", err)
		return
	}

	utils.Success(c, 200, "邮箱配置更新成功", nil)
}

func (h *EmailHandler) DeleteConfig(c *gin.Context) {
	id := c.Param("id")
	if err := h.emailService.DeleteConfig(middleware.GetEffectiveUserID(c), id); err != nil {
		utils.Error(c, 404, "邮箱配置不存在", nil)
		return
	}

	utils.Success(c, 200, "邮箱配置删除成功", nil)
}

type TestConnectionInput struct {
	Email    string `json:"email" binding:"required"`
	IMAPHost string `json:"imap_host" binding:"required"`
	IMAPPort int    `json:"imap_port"`
	Password string `json:"password" binding:"required"`
}

func (h *EmailHandler) TestConnection(c *gin.Context) {
	var input TestConnectionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "请提供完整的连接信息", err)
		return
	}

	port := input.IMAPPort
	if port == 0 {
		port = 993
	}

	success, message := h.emailService.TestConnection(input.Email, input.IMAPHost, port, input.Password)
	c.JSON(200, gin.H{
		"success": success,
		"message": message,
	})
}

func (h *EmailHandler) GetLogs(c *gin.Context) {
	configID := c.Query("configId")
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	ctx, cancel := withReadTimeout(c)
	defer cancel()

	logs, err := h.emailService.GetLogsCtx(ctx, middleware.GetEffectiveUserID(c), configID, limit)
	if err != nil {
		if handleReadTimeoutError(c, err) {
			return
		}
		utils.Error(c, 500, "获取邮件日志失败", err)
		return
	}

	utils.SuccessData(c, logs)
}

func (h *EmailHandler) ParseLog(c *gin.Context) {
	id := c.Param("id")
	ownerUserID := middleware.GetEffectiveUserID(c)
	invoice, err := h.emailService.ParseEmailLogCtx(c.Request.Context(), ownerUserID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			utils.Error(c, 404, "邮件日志不存在", nil)
			return
		}
		utils.Error(c, 500, "解析邮件发票失败", err)
		return
	}
	utils.Success(c, 200, "解析成功", invoice)
}

func (h *EmailHandler) StartMonitoring(c *gin.Context) {
	id := c.Param("id")
	started := h.emailService.StartMonitoring(middleware.GetEffectiveUserID(c), id)
	if !started {
		utils.Error(c, 400, "无法启动监控，请检查配置", nil)
		return
	}

	utils.Success(c, 200, "邮箱监控已启动", nil)
}

func (h *EmailHandler) StopMonitoring(c *gin.Context) {
	id := c.Param("id")
	if _, err := h.emailService.GetConfigByID(middleware.GetEffectiveUserID(c), id); err != nil {
		utils.Error(c, 404, "email config not found", nil)
		return
	}
	stopped := h.emailService.StopMonitoring(id)
	if stopped {
		utils.Success(c, 200, "邮箱监控已停止", nil)
	} else {
		utils.Success(c, 200, "监控未在运行", nil)
	}
}

func (h *EmailHandler) GetMonitoringStatus(c *gin.Context) {
	ctx, cancel := withReadTimeout(c)
	defer cancel()

	statuses, err := h.emailService.GetMonitoringStatusCtx(ctx, middleware.GetEffectiveUserID(c))
	if err != nil {
		if handleReadTimeoutError(c, err) {
			return
		}
		utils.Error(c, 500, "获取监控状态失败", err)
		return
	}

	utils.SuccessData(c, statuses)
}

func (h *EmailHandler) ManualCheck(c *gin.Context) {
	id := c.Param("id")
	full := c.Query("full") == "1" || c.Query("full") == "true"
	success, message, newEmails := h.emailService.ManualCheck(middleware.GetEffectiveUserID(c), id, full)

	c.JSON(200, gin.H{
		"success": success,
		"message": message,
		"data": gin.H{
			"newEmails": newEmails,
		},
	})
}
