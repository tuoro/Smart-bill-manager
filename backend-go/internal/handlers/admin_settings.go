package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type AdminSettingsHandler struct{}

func NewAdminSettingsHandler() *AdminSettingsHandler {
	return &AdminSettingsHandler{}
}

func (h *AdminSettingsHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.Get)
	r.PUT("", h.Update)
}

func (h *AdminSettingsHandler) Get(c *gin.Context) {
	s, err := services.GetSystemSettings()
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "获取系统设置失败", err)
		return
	}
	utils.SuccessData(c, s)
}

func (h *AdminSettingsHandler) Update(c *gin.Context) {
	var patch services.SystemSettingsPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		utils.Error(c, http.StatusBadRequest, "参数错误", err)
		return
	}

	userID := middleware.GetUserID(c)
	s, err := services.UpdateSystemSettings(userID, patch)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "保存系统设置失败", err)
		return
	}
	utils.SuccessData(c, s)
}

