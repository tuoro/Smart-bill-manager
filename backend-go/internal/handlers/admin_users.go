package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type AdminUsersHandler struct {
	authService *services.AuthService
	uploadsDir  string
}

func NewAdminUsersHandler(authService *services.AuthService, uploadsDir string) *AdminUsersHandler {
	return &AdminUsersHandler{authService: authService, uploadsDir: uploadsDir}
}

func (h *AdminUsersHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("", h.List)
	r.PATCH("/:id/active", h.SetActive)
	r.DELETE("/:id", h.Delete)
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

type SetUserActiveInput struct {
	Active   *bool `json:"active"`
	IsActive *bool `json:"is_active"`
}

func (h *AdminUsersHandler) SetActive(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		utils.Error(c, http.StatusBadRequest, "缺少用户 id", nil)
		return
	}

	var input SetUserActiveInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, http.StatusBadRequest, "参数错误", err)
		return
	}
	activePtr := input.IsActive
	if activePtr == nil {
		activePtr = input.Active
	}
	if activePtr == nil {
		utils.Error(c, http.StatusBadRequest, "缺少 is_active/active", nil)
		return
	}

	actorID := middleware.GetUserID(c)
	updated, err := h.authService.SetUserActiveCtx(c.Request.Context(), actorID, id, *activePtr)
	if err != nil {
		switch err {
		case services.ErrUserSelfAction:
			utils.Error(c, http.StatusBadRequest, "不能修改自己的启用状态", err)
			return
		case services.ErrUserLastAdmin:
			utils.Error(c, http.StatusBadRequest, "至少保留一个启用的管理员账号", err)
			return
		default:
			utils.Error(c, http.StatusInternalServerError, "更新用户状态失败", err)
			return
		}
	}

	utils.SuccessData(c, updated)
}

func (h *AdminUsersHandler) Delete(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		utils.Error(c, http.StatusBadRequest, "缺少用户 id", nil)
		return
	}

	actorID := middleware.GetUserID(c)
	res, err := h.authService.DeleteUserCtx(c.Request.Context(), actorID, id)
	if err != nil {
		switch err {
		case services.ErrUserSelfAction:
			utils.Error(c, http.StatusBadRequest, "不能删除自己的账号", err)
			return
		case services.ErrUserLastAdmin:
			utils.Error(c, http.StatusBadRequest, "至少保留一个启用的管理员账号", err)
			return
		case services.ErrNotFound:
			utils.Error(c, http.StatusNotFound, "用户不存在", err)
			return
		default:
			utils.Error(c, http.StatusInternalServerError, "删除用户失败", err)
			return
		}
	}

	utils.SuccessData(c, res)

	// Best-effort: remove user upload directory to ensure file isolation.
	if h.uploadsDir != "" {
		_ = os.RemoveAll(filepath.Join(h.uploadsDir, id))
	}
}
