package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type AdminAPITokensHandler struct {
	apiTokenService *services.APITokenService
}

func NewAdminAPITokensHandler(apiTokenService *services.APITokenService) *AdminAPITokensHandler {
	return &AdminAPITokensHandler{apiTokenService: apiTokenService}
}

func (h *AdminAPITokensHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("", h.Create)
	r.GET("", h.List)
	r.POST("/:id/revoke", h.Revoke)
}

type createAPITokenInput struct {
	Name          string `json:"name"`
	ExpiresInDays *int   `json:"expiresInDays"`
}

func (h *AdminAPITokensHandler) Create(c *gin.Context) {
	var input createAPITokenInput
	_ = c.ShouldBindJSON(&input)

	expiresInDays := 30
	if input.ExpiresInDays != nil {
		expiresInDays = *input.ExpiresInDays
	}
	if expiresInDays < 0 || expiresInDays > 3650 {
		utils.Error(c, 400, "expiresInDays 必须在 0-3650 之间", nil)
		return
	}

	userID := middleware.GetUserID(c)
	res, err := h.apiTokenService.CreateForUser(userID, services.CreateAPITokenInput{
		Name:          input.Name,
		ExpiresInDays: expiresInDays,
	})
	if err != nil {
		utils.Error(c, 500, "创建 API Token 失败", err)
		return
	}

	utils.SuccessData(c, res)
}

func (h *AdminAPITokensHandler) List(c *gin.Context) {
	limit := 50
	if s := c.Query("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}

	userID := middleware.GetUserID(c)
	items, err := h.apiTokenService.ListByUser(userID, limit)
	if err != nil {
		utils.Error(c, 500, "获取 API Token 失败", err)
		return
	}
	utils.SuccessData(c, items)
}

func (h *AdminAPITokensHandler) Revoke(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		utils.Error(c, 400, "缺少 id", nil)
		return
	}
	userID := middleware.GetUserID(c)
	if err := h.apiTokenService.Revoke(id, userID); err != nil {
		switch err {
		case services.ErrNotFound:
			utils.Error(c, 404, "Token 不存在", err)
			return
		default:
			utils.Error(c, 500, "撤销 Token 失败", err)
			return
		}
	}
	utils.SuccessData(c, gin.H{"revoked": true})
}

