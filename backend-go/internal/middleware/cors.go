package middleware

import (
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/config"
)

// CORSMiddleware creates CORS middleware
func CORSMiddleware() gin.HandlerFunc {
	origins := make([]string, 0)
	for _, s := range strings.Split(config.AppConfig.CORSAllowedOrigins, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		// With cookies, wildcard origin is unsafe and ignored by browsers.
		if s == "*" {
			continue
		}
		origins = append(origins, s)
	}
	if len(origins) == 0 {
		// Same-origin deployments (most prod setups) do not require CORS headers.
		return func(c *gin.Context) { c.Next() }
	}
	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-CSRF-Token"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
