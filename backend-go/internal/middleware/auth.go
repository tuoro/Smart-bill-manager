package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

const (
	HeaderActAsUser      = "X-Act-As-User"
	HeaderActAsConfirmed = "X-Act-As-Confirmed"
)

// AuthMiddleware creates JWT authentication middleware
func AuthMiddleware(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			utils.Error(c, 401, "未授权，请先登录", nil)
			c.Abort()
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := authService.VerifyToken(token)

		if err != nil {
			utils.Error(c, 401, "登录已过期，请重新登录", nil)
			c.Abort()
			return
		}

		// Set user info in context (actor)
		c.Set("userId", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)

		actorUserID := claims.UserID
		effectiveUserID := actorUserID

		// Admin can optionally act as another user.
		if claims.Role == "admin" {
			actAs := strings.TrimSpace(c.GetHeader(HeaderActAsUser))
			if actAs != "" && actAs != actorUserID {
				if _, err := authService.GetUserByID(actAs); err != nil {
					utils.Error(c, 400, "代操作用户不存在", nil)
					c.Abort()
					return
				}
				effectiveUserID = actAs
			}
		}

		c.Set("actorUserId", actorUserID)
		c.Set("effectiveUserId", effectiveUserID)
		c.Set("isActingAs", effectiveUserID != actorUserID)

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

func GetActorUserID(c *gin.Context) string {
	if id, exists := c.Get("actorUserId"); exists {
		if s, ok := id.(string); ok {
			return s
		}
	}
	return GetUserID(c)
}

func GetEffectiveUserID(c *gin.Context) string {
	if id, exists := c.Get("effectiveUserId"); exists {
		if s, ok := id.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return GetUserID(c)
}

func IsActingAs(c *gin.Context) bool {
	if v, ok := c.Get("isActingAs"); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return GetActorUserID(c) != GetEffectiveUserID(c)
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
