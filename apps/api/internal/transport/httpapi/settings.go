package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	postgresqladapter "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// DatabaseSettingsStore 抽象部署级数据库连接的读写，便于在环境变量来源下
// 只读展示、在文件来源下允许修改。
type DatabaseSettingsStore struct {
	Directory     string
	MigrationsDir string
	// Managed 为 false 表示连接由环境变量固定，页面只展示不允许修改。
	Managed bool
}

func (s *Server) databaseSettingsHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	if err := tenantContext(principal).Require(domain.CapabilityDeploymentManage); err != nil {
		writeError(response, request, err)
		return
	}
	current := s.databaseSettings
	body := map[string]any{
		"editable": current.Managed,
		"host":     s.databaseConnection.Host,
		"port":     s.databaseConnection.Port,
		"database": s.databaseConnection.Database,
		"user":     s.databaseConnection.User,
	}
	if !current.Managed {
		body["reason"] = "当前连接由环境变量固定，请在容器参数中修改后重启。"
	}
	writeJSON(response, http.StatusOK, body)
}

func (s *Server) updateDatabaseSettingsHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	if err := tenantContext(principal).Require(domain.CapabilityDeploymentManage); err != nil {
		writeError(response, request, err)
		return
	}
	if !s.databaseSettings.Managed {
		writeError(response, request, domain.NewRuleError(
			"database_settings_not_editable",
			"当前连接由环境变量固定，请在容器参数中修改后重启",
			domain.ErrInvalidInput,
		))
		return
	}
	input, settings, err := decodeDatabaseSetup(request)
	if err != nil {
		writeError(response, request, domain.NewRuleError(
			"database_settings_invalid", err.Error(), domain.ErrInvalidInput,
		))
		return
	}
	password := []byte(input.Password)
	defer clear(password)

	verifyCtx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	if err := postgresqladapter.VerifyConnectionSettings(
		verifyCtx, s.databaseSettings.Directory, settings, password, s.databaseSettings.MigrationsDir,
	); err != nil {
		writeError(response, request, domain.NewRuleError(
			"database_settings_unreachable", err.Error(), domain.ErrInvalidInput,
		))
		return
	}
	if strings.TrimSpace(request.URL.Query().Get("test")) == "1" {
		writeJSON(response, http.StatusOK, map[string]any{"connected": true})
		return
	}
	if err := postgresqladapter.SaveConnectionSettings(
		s.databaseSettings.Directory, settings, password,
	); err != nil {
		writeError(response, request, domain.NewRuleError(
			"database_settings_invalid", err.Error(), domain.ErrInvalidInput,
		))
		return
	}
	s.logger.Info("database settings updated",
		"host", settings.Host, "database", settings.Database, "actor", principal.UserID)
	// 连接池与后台 worker 已绑定在旧连接上，新配置需要重启容器后生效。
	writeJSON(response, http.StatusOK, map[string]any{"restart_required": true})
}
