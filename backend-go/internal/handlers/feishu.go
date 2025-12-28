package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type FeishuHandler struct {
	feishuService *services.FeishuService
}

func NewFeishuHandler(feishuService *services.FeishuService) *FeishuHandler {
	return &FeishuHandler{feishuService: feishuService}
}

// RegisterRoutes registers protected (UI/API) routes.
func (h *FeishuHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/configs", h.GetAllConfigs)
	r.POST("/configs", h.CreateConfig)
	r.PUT("/configs/:id", h.UpdateConfig)
	r.DELETE("/configs/:id", h.DeleteConfig)
	r.GET("/logs", h.GetLogs)
}

func (h *FeishuHandler) GetAllConfigs(c *gin.Context) {
	configs, err := h.feishuService.GetAllConfigs()
	if err != nil {
		utils.Error(c, 500, "获取飞书配置失败", err)
		return
	}
	utils.SuccessData(c, configs)
}

func (h *FeishuHandler) CreateConfig(c *gin.Context) {
	var input services.CreateFeishuConfigInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "配置参数错误", err)
		return
	}

	config, err := h.feishuService.CreateConfig(input)
	if err != nil {
		utils.Error(c, 500, "创建飞书配置失败", err)
		return
	}

	utils.Success(c, 201, "飞书配置创建成功", config.ToResponse())
}

func (h *FeishuHandler) UpdateConfig(c *gin.Context) {
	id := c.Param("id")
	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		utils.Error(c, 400, "参数错误", err)
		return
	}

	if err := h.feishuService.UpdateConfig(id, data); err != nil {
		utils.Error(c, 404, "配置不存在或更新失败", err)
		return
	}

	utils.Success(c, 200, "配置更新成功", nil)
}

func (h *FeishuHandler) DeleteConfig(c *gin.Context) {
	id := c.Param("id")
	if err := h.feishuService.DeleteConfig(id); err != nil {
		utils.Error(c, 404, "配置不存在", err)
		return
	}

	utils.Success(c, 200, "配置删除成功", nil)
}

func (h *FeishuHandler) GetLogs(c *gin.Context) {
	configID := c.Query("configId")
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	logs, err := h.feishuService.GetLogs(configID, limit)
	if err != nil {
		utils.Error(c, 500, "获取日志失败", err)
		return
	}

	utils.SuccessData(c, logs)
}

// Webhook is a public endpoint for Feishu event subscription.
func (h *FeishuHandler) Webhook(c *gin.Context) {
	config, err := h.feishuService.GetActiveConfig()
	if err != nil || config == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0})
		return
	}

	payload, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0})
		return
	}

	challenge, err := h.feishuService.ProcessWebhookPayload(c.Request.Context(), payload, config)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": err.Error()})
		return
	}
	if challenge != nil {
		c.JSON(http.StatusOK, gin.H{"challenge": *challenge})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0})
}

// WebhookWithConfig allows using a specific config (useful when multiple configs exist).
func (h *FeishuHandler) WebhookWithConfig(c *gin.Context) {
	configID := c.Param("configId")

	config, err := h.feishuService.GetConfigByID(configID)
	if err != nil || config == nil || config.IsActive == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0})
		return
	}

	payload, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0})
		return
	}

	challenge, err := h.feishuService.ProcessWebhookPayload(c.Request.Context(), payload, config)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": err.Error()})
		return
	}
	if challenge != nil {
		c.JSON(http.StatusOK, gin.H{"challenge": *challenge})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0})
}
