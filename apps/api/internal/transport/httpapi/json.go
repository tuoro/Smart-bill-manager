package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type errorBody struct {
	Error struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		ResourceID string `json:"resource_id,omitempty"`
	} `json:"error"`
	RequestID string `json:"request_id"`
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return domain.NewRuleError("invalid_json", "请求 JSON 格式不正确", domain.ErrInvalidInput)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return domain.NewRuleError("invalid_json", "请求只能包含一个 JSON 值", domain.ErrInvalidInput)
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, request *http.Request, err error) {
	status, code, message := errorDetails(err)
	body := errorBody{RequestID: requestIDFromRequest(request)}
	body.Error.Code = code
	body.Error.Message = message
	var duplicate *domain.DuplicateDocumentError
	if errors.As(err, &duplicate) {
		body.Error.ResourceID = duplicate.DocumentID
	}
	writeJSON(response, status, body)
}

func errorDetails(err error) (int, string, string) {
	var ruleError *domain.RuleError
	if errors.As(err, &ruleError) {
		return statusFor(ruleError.Cause), ruleError.Code, ruleError.Message
	}
	switch {
	case errors.Is(err, domain.ErrUnauthenticated):
		return http.StatusUnauthorized, "unauthenticated", "登录已失效，请重新登录"
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "forbidden", "当前账号没有执行此操作的权限"
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not_found", "资源不存在"
	case errors.Is(err, domain.ErrTenantRequired):
		return http.StatusConflict, "tenant_required", "该账号属于多个工作区，请选择工作区"
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrVersionConflict):
		return http.StatusConflict, "conflict", "资源状态已变化，请刷新后重试"
	case errors.Is(err, domain.ErrInvalidInput):
		return http.StatusBadRequest, "invalid_input", "输入不合法"
	case errors.Is(err, domain.ErrPayloadTooLarge):
		return http.StatusRequestEntityTooLarge, "document_too_large", "文件不能超过 20 MiB"
	case errors.Is(err, domain.ErrUnavailable):
		return http.StatusServiceUnavailable, "unavailable", "服务尚未就绪"
	default:
		return http.StatusInternalServerError, "internal_error", "服务暂时无法完成请求"
	}
}

func statusFor(cause error) int {
	status, _, _ := errorDetails(cause)
	return status
}
