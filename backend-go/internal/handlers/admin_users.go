package handlers

import (
	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type AdminUsersHandler struct {
	authService *services.AuthService
}

func NewAdminUsersHandler(authService *services.AuthService) *AdminUsersHandler {
	return &AdminUsersHandler{authService: authService}
}

func (h *AdminUsersHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.List)
}

func (h *AdminUsersHandler) List(c *gin.Context) {
	ctx, cancel := withReadTimeout(c)
	defer cancel()

	users, err := h.authService.GetAllUsersCtx(ctx)
	if err != nil {
		if handleReadTimeoutError(c, err) {
			return
		}
		utils.Error(c, 500, "获取用户列表失败", err)
		return
	}
	utils.SuccessData(c, users)
}
