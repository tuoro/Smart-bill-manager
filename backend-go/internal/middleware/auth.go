package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

// AuthMiddleware authenticates either:
// - browser sessions via HttpOnly cookie (server-side sessions)
// - non-browser clients via Authorization: Bearer <PASETO>
func AuthMiddleware(authService *services.AuthService, sessionService *services.SessionService, apiTokenService *services.APITokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var userID string

		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			id, err := apiTokenService.VerifyBearer(token)
			if err != nil {
				utils.Error(c, 401, "未授权，请先登录", nil)
				c.Abort()
				return
			}
			userID = id
		} else {
			id, err := sessionService.GetUserIDFromCookie(c)
			if err != nil {
				utils.Error(c, 401, "未授权，请先登录", nil)
				c.Abort()
				return
			}
			userID = id
		}

		user, err := authService.GetUserByID(userID)
		if err != nil {
			utils.Error(c, 401, "登录已过期，请重新登录", nil)
			c.Abort()
			return
		}
		if user.IsActive != 1 {
			utils.Error(c, 401, "账号已禁用", nil)
			c.Abort()
			return
		}

		c.Set("userId", user.ID)
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		c.Next()
	}
}

// GetUserID gets user ID from context
func GetUserID(c *gin.Context) string {
	if id, exists := c.Get("userId"); exists {
		return id.(string)
	}
	return ""
}

// GetUsername gets username from context
func GetUsername(c *gin.Context) string {
	if username, exists := c.Get("username"); exists {
		return username.(string)
	}
	return ""
}

// GetUserRole gets user role from context
func GetUserRole(c *gin.Context) string {
	if role, exists := c.Get("role"); exists {
		return role.(string)
	}
	return ""
}
