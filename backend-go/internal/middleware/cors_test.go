package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSMiddlewareAllowsConfiguredOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORSMiddleware([]string{"https://bill.example.com"}))
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodOptions, "/health", nil)
	request.Header.Set("Origin", "https://bill.example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", HeaderActAsUser)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("预检请求状态码错误: %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://bill.example.com" {
		t.Fatalf("允许来源错误: %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Bearer Token 模式不应允许 Cookie 凭据: %q", got)
	}
}

func TestCORSMiddlewareRejectsUnknownOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORSMiddleware([]string{"https://bill.example.com"}))
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodOptions, "/health", nil)
	request.Header.Set("Origin", "https://evil.example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("未知来源应被拒绝，状态码为 %d", response.Code)
	}
}
