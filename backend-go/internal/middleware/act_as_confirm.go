package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/utils"
)

// ActAsConfirmMiddleware enforces a per-write confirmation when admin is acting as another user.
// Frontend should retry the request with HeaderActAsConfirmed=1 after user confirmation.
func ActAsConfirmMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsActingAs(c) {
			c.Next()
			return
		}

		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			confirmed := strings.TrimSpace(c.GetHeader(HeaderActAsConfirmed))
			if confirmed != "1" && !strings.EqualFold(confirmed, "true") {
				utils.ErrorData(c, 400, "需要代操作确认", gin.H{
					"code":          "ACT_AS_CONFIRM_REQUIRED",
					"actor_user_id": GetActorUserID(c),
					"target_user_id": GetEffectiveUserID(c),
					"method":        c.Request.Method,
					"path":          c.FullPath(),
				}, nil)
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

