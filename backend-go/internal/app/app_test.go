package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNewUsesInjectedOCRWorker(t *testing.T) {
	worker := &noopOCRWorker{}
	application, err := NewWithOCRWorker(testConfig(t), &gorm.DB{}, t.TempDir(), worker)
	if err != nil {
		t.Fatalf("创建注入 OCR worker 的应用失败: %v", err)
	}
	if application.ocrWorker != worker {
		t.Fatal("应用未保留注入的 OCR worker 生命周期")
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

func TestNewDoesNotRegisterRegressionSampleRoutes(t *testing.T) {
	application, err := New(testConfig(t), &gorm.DB{}, t.TempDir())
	if err != nil {
		t.Fatalf("创建测试应用失败: %v", err)
	}
	for _, route := range application.Router.Routes() {
		if strings.Contains(strings.ToLower(route.Path), "regression") {
			t.Fatalf("生产路由不应暴露回归样本接口: %s %s", route.Method, route.Path)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/regression-samples", nil)
	application.Router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("旧回归样本接口应不可达，实际状态码为 %d", recorder.Code)
	}
	response := strings.ToLower(recorder.Body.String())
	for _, internalField := range []string{"raw_text", "expected_json", "source_hash"} {
		if strings.Contains(response, internalField) {
			t.Fatalf("不可达接口响应泄露回归内部字段 %q", internalField)
		}
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

type noopOCRWorker struct{}

func (*noopOCRWorker) StartIfEnabled() (bool, error) {
	return false, nil
}

func (*noopOCRWorker) Recognize(context.Context, string, string, string) ([]byte, error) {
	return nil, nil
}

func (*noopOCRWorker) RunFallback(ctx context.Context, fallback func(context.Context) (string, error)) (string, error) {
	return fallback(ctx)
}

func (*noopOCRWorker) Shutdown(context.Context) error {
	return nil
}
