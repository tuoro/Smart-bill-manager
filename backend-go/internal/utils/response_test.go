package utils

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestErrorHidesInternalDetailsForServerErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/payments", nil)
	ctx.Request.Header.Set("X-Request-ID", "request-1")

	Error(ctx, http.StatusInternalServerError, "获取支付记录失败", errors.New("database path /secret/bills.db is locked"))

	var body Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析错误响应失败: %v", err)
	}
	if recorder.Code != http.StatusInternalServerError || body.Success || body.Message != "获取支付记录失败" {
		t.Fatalf("服务端错误响应异常: code=%d body=%#v", recorder.Code, body)
	}
	if body.Error != "" {
		t.Fatalf("5xx 响应不应暴露内部错误: %q", body.Error)
	}
}

func TestErrorKeepsBusinessDetailsForClientErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/payments", nil)

	Error(ctx, http.StatusBadRequest, "参数错误", errors.New("transaction_time must be RFC3339"))

	var body Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析业务错误响应失败: %v", err)
	}
	if body.Error != "transaction_time must be RFC3339" {
		t.Fatalf("4xx 响应应保留业务错误详情: %#v", body)
	}
}

func TestErrorDataKeepsConflictPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/invoices/invoice-1", nil)

	payload := map[string]any{"code": "DUPLICATE", "candidate_id": "invoice-2"}
	ErrorData(ctx, http.StatusConflict, "检测到重复", payload, errors.New("duplicate invoice"))

	var body struct {
		Response
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析冲突响应失败: %v", err)
	}
	if body.Error != "duplicate invoice" || body.Data["candidate_id"] != "invoice-2" {
		t.Fatalf("冲突响应应保留结构化数据和业务详情: %#v", body)
	}
}
