package handlers

import (
	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type AuthHandler struct {
	authService *services.AuthService
}

func NewAuthHandler(authService *services.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("/register", h.Register)
	r.POST("/login", h.Login)
	r.GET("/me", middleware.AuthMiddleware(h.authService), h.GetMe)
	r.GET("/verify", middleware.AuthMiddleware(h.authService), h.Verify)
	r.POST("/change-password", middleware.AuthMiddleware(h.authService), h.ChangePassword)
	r.GET("/setup-required", h.SetupRequired)
	r.POST("/setup", h.SetupAdmin)
}

type RegisterInput struct {
	Username string  `json:"username" binding:"required"`
	Password string  `json:"password" binding:"required"`
	Email    *string `json:"email"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var input RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "用户名和密码不能为空", err)
		return
	}

	if len(input.Username) < 3 || len(input.Username) > 50 {
		utils.Error(c, 400, "用户名长度应为3-50个字符", nil)
		return
	}

	if len(input.Password) < 6 {
		utils.Error(c, 400, "密码长度至少6个字符", nil)
		return
	}

	result, err := h.authService.Register(input.Username, input.Password, input.Email)
	if err != nil {
		utils.Error(c, 500, "注册失败，请稍后重试", err)
		return
	}

	if !result.Success {
		utils.Error(c, 400, result.Message, nil)
		return
	}

	c.JSON(201, result)
}

type LoginInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "用户名和密码不能为空", err)
		return
	}

	result, err := h.authService.Login(input.Username, input.Password)
	if err != nil {
		utils.Error(c, 500, "登录失败，请稍后重试", err)
		return
	}

	if !result.Success {
		utils.Error(c, 401, result.Message, nil)
		return
	}

	c.JSON(200, result)
}

func (h *AuthHandler) GetMe(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		utils.Error(c, 401, "未授权", nil)
		return
	}

	user, err := h.authService.GetUserByID(userID)
	if err != nil {
		utils.Error(c, 404, "用户不存在", err)
		return
	}

	utils.SuccessData(c, user)
}

func (h *AuthHandler) Verify(c *gin.Context) {
	userID := middleware.GetUserID(c)
	username := middleware.GetUsername(c)
	role := middleware.GetUserRole(c)

	c.JSON(200, gin.H{
		"success": true,
		"message": "Token有效",
		"user": gin.H{
			"userId":   userID,
			"username": username,
			"role":     role,
		},
	})
}

type ChangePasswordInput struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		utils.Error(c, 401, "未授权", nil)
		return
	}

	var input ChangePasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "原密码和新密码不能为空", err)
		return
	}

	if len(input.NewPassword) < 6 {
		utils.Error(c, 400, "新密码长度至少6个字符", nil)
		return
	}

	result, err := h.authService.UpdatePassword(userID, input.OldPassword, input.NewPassword)
	if err != nil {
		utils.Error(c, 500, "修改密码失败，请稍后重试", err)
		return
	}

	if !result.Success {
		utils.Error(c, 400, result.Message, nil)
		return
	}

	c.JSON(200, result)
}

func (h *AuthHandler) SetupRequired(c *gin.Context) {
	hasUsers, err := h.authService.HasUsers()
	if err != nil {
		utils.Error(c, 500, "检查用户失败", err)
		return
	}

	c.JSON(200, gin.H{
		"success":       true,
		"setupRequired": !hasUsers,
	})
}

type SetupInput struct {
	Username string  `json:"username" binding:"required"`
	Password string  `json:"password" binding:"required"`
	Email    *string `json:"email"`
}

func (h *AuthHandler) SetupAdmin(c *gin.Context) {
	var input SetupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		utils.Error(c, 400, "用户名和密码不能为空", err)
		return
	}

	result, err := h.authService.CreateInitialAdmin(input.Username, input.Password, input.Email)
	if err != nil {
		utils.Error(c, 500, "初始化失败，请稍后重试", err)
		return
	}

	if !result.Success {
		utils.Error(c, 400, result.Message, nil)
		return
	}

	c.JSON(201, result)
}
