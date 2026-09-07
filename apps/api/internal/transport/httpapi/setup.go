package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/bootstrap"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// SetupInspector 报告部署是否仍未创建任何身份记录。
type SetupInspector interface {
	IdentityIsEmpty(ctx context.Context) (bool, error)
}

// setupHandler 供未认证的首启页面判断是否还需要初始化。
// 一旦 Owner 存在就永久返回 false，前端据此不再展示初始化入口。
func (s *Server) setupHandler(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	required, err := s.setupInspector.IdentityIsEmpty(request.Context())
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"required": required})
}

// createSetupHandler 在空数据库上创建唯一 Owner。并发与重放由
// BootstrapOwner 的 Serializable 事务拒绝，不依赖本层的预检查。
func (s *Server) createSetupHandler(response http.ResponseWriter, request *http.Request) {
	var email, password, displayName, tenantName, currency, timezone string
	fields := map[string]any{
		"email":            &email,
		"password":         &password,
		"display_name":     &displayName,
		"tenant_name":      &tenantName,
		"default_currency": &currency,
		"timezone":         &timezone,
	}
	if err := decodeAccountFields(response, request, fields); err != nil {
		writeError(response, request, err)
		return
	}
	value := []byte(password)
	defer clear(value)
	_, err := s.setup.Execute(request.Context(), bootstrap.Input{
		Email:           email,
		Password:        value,
		DisplayName:     displayName,
		TenantName:      tenantName,
		DefaultCurrency: domain.Currency(currency),
		Timezone:        timezone,
	})
	if errors.Is(err, domain.ErrBootstrapNotEmpty) {
		writeError(response, request, domain.NewRuleError(
			"setup_already_complete",
			"该部署已经完成初始化",
			domain.ErrBootstrapNotEmpty,
		))
		return
	}
	if err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
