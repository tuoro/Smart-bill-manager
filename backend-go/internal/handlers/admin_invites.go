package handlers

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type AdminInvitesHandler struct {
	authService *services.AuthService
}

func NewAdminInvitesHandler(authService *services.AuthService) *AdminInvitesHandler {
	return &AdminInvitesHandler{authService: authService}
}

type CreateInviteInput struct {
	// ExpiresInDays controls invite expiry. Use 0 for no expiry.
	// Defaults to 7 when omitted.
	ExpiresInDays *int `json:"expiresInDays"`
}

func (h *AdminInvitesHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("", h.CreateInvite)
	r.GET("", h.ListInvites)
	r.DELETE("/:id", h.DeleteInvite)
}

func (h *AdminInvitesHandler) CreateInvite(c *gin.Context) {
	var input CreateInviteInput
	_ = c.ShouldBindJSON(&input)

	expiresInDays := 7
	if input.ExpiresInDays != nil {
		expiresInDays = *input.ExpiresInDays
	}
	if expiresInDays < 0 || expiresInDays > 365 {
		utils.Error(c, 400, "expiresInDays 必须在 0-365 之间", nil)
		return
	}

	adminID := middleware.GetUserID(c)
	res, err := h.authService.CreateInvite(adminID, expiresInDays)
	if err != nil {
		utils.Error(c, 500, "生成邀请码失败", err)
		return
	}

	utils.SuccessData(c, gin.H{
		"code":      res.Code,
		"code_hint": res.CodeHint,
		"expiresAt": res.ExpiresAt,
	})
}

func (h *AdminInvitesHandler) ListInvites(c *gin.Context) {
	limit := 30
	if s := c.Query("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}

	ctx, cancel := withReadTimeout(c)
	defer cancel()

	items, err := h.authService.ListInvitesCtx(ctx, limit)
	if err != nil {
		if handleReadTimeoutError(c, err) {
			return
		}
		utils.Error(c, 500, "获取邀请码失败", err)
		return
	}

	idsSet := make(map[string]struct{}, len(items)*2)
	for _, inv := range items {
		if s := strings.TrimSpace(inv.CreatedBy); s != "" {
			idsSet[s] = struct{}{}
		}
		if inv.UsedBy != nil {
			if s := strings.TrimSpace(*inv.UsedBy); s != "" {
				idsSet[s] = struct{}{}
			}
		}
	}
	ids := make([]string, 0, len(idsSet))
	for id := range idsSet {
		ids = append(ids, id)
	}
	nameByID, err := h.authService.GetUsernamesByIDsCtx(ctx, ids)
	if err != nil {
		if handleReadTimeoutError(c, err) {
			return
		}
		utils.Error(c, 500, "获取邀请码用户信息失败", err)
		return
	}

	out := make([]gin.H, 0, len(items))
	now := time.Now()
	for _, inv := range items {
		expired := false
		if inv.ExpiresAt != nil && inv.ExpiresAt.Before(now) {
			expired = true
		}

		createdByUsername := nameByID[strings.TrimSpace(inv.CreatedBy)]
		createdByDeleted := strings.TrimSpace(inv.CreatedBy) != "" && createdByUsername == ""
		usedByUsername := ""
		usedByDeleted := false
		if inv.UsedBy != nil {
			uid := strings.TrimSpace(*inv.UsedBy)
			usedByUsername = nameByID[uid]
			usedByDeleted = uid != "" && inv.UsedAt != nil && usedByUsername == ""
		}
		out = append(out, gin.H{
			"id":                inv.ID,
			"code_hint":         inv.CodeHint,
			"createdBy":         inv.CreatedBy,
			"createdByUsername": createdByUsername,
			"createdByDeleted":  createdByDeleted,
			"createdAt":         inv.CreatedAt,
			"expiresAt":         inv.ExpiresAt,
			"usedAt":            inv.UsedAt,
			"usedBy":            inv.UsedBy,
			"usedByUsername":    usedByUsername,
			"usedByDeleted":     usedByDeleted,
			"expired":           expired,
		})
	}

	utils.SuccessData(c, out)
}

func (h *AdminInvitesHandler) DeleteInvite(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		utils.Error(c, 400, "缺少 id", nil)
		return
	}

	if err := h.authService.DeleteInvite(id); err != nil {
		switch err {
		case services.ErrNotFound:
			utils.Error(c, 404, "邀请码不存在", err)
			return
		case services.ErrInviteUsed:
			utils.Error(c, 400, "邀请码已被使用，无法删除", err)
			return
		default:
			utils.Error(c, 500, "删除邀请码失败", err)
			return
		}
	}

	utils.SuccessData(c, gin.H{"deleted": true})
}
