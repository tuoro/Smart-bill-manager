package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorm.io/gorm"

	"smart-bill-manager/internal/config"
)

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := New(nil, nil, ""); err == nil {
		t.Fatal("配置为空时应返回错误")
	}
	cfg := testConfig(t)
	if _, err := New(cfg, nil, t.TempDir()); err == nil {
		t.Fatal("数据库为空时应返回错误")
	}
}

func TestHealthCheck(t *testing.T) {
	application, err := New(testConfig(t), &gorm.DB{}, t.TempDir())
	if err != nil {
		t.Fatalf("创建测试应用失败: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	application.Router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("健康检查状态码应为 200，实际为 %d", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析健康检查响应失败: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("健康检查状态异常: %#v", body)
	}
	if body["items_parser_rev"] != float64(itemsParserRevision) {
		t.Fatalf("支付解析器版本异常: %#v", body["items_parser_rev"])
	}
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Port:               "3001",
		JWTSecret:          "0123456789abcdef0123456789abcdef",
		JWTExpiresIn:       time.Hour,
		NodeEnv:            "test",
		DataDir:            t.TempDir(),
		UploadsDir:         t.TempDir(),
		CORSAllowedOrigins: []string{"http://localhost:5173"},
	}
}
