package httpapi

import (
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// 邮箱连接：换密码、检测、开关同步、立即同步、删除。密码只进不出。
func (s *Server) setEmailSourceCredentialsHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var username, password string
	if err := decodeAccountFields(response, request, map[string]any{"imap_username": &username, "imap_password": &password}); err != nil {
		writeError(response, request, err)
		return
	}
	secret := []byte(password)
	defer clear(secret)
	source, err := s.mailSync.SetCredentials(request.Context(), tenantContext(principal), request.PathValue("source_id"), username, secret)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, source)
}

func (s *Server) emailSourceAction(action func(*http.Request, domain.TenantContext, string) (ports.EmailSource, error)) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		principal, ok := principalFromRequest(request)
		if !ok {
			writeError(response, request, domain.ErrUnauthenticated)
			return
		}
		source, err := action(request, tenantContext(principal), request.PathValue("source_id"))
		if err != nil {
			writeError(response, request, err)
			return
		}
		writeJSON(response, http.StatusOK, source)
	}
}

func (s *Server) deleteEmailSourceHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if err := s.mailSync.Delete(request.Context(), tenantContext(principal), request.PathValue("source_id"), requestIDFromRequest(request)); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
