package httpapi

import (
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/chatconnectors"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func chatConnectorResponse(c chatconnectors.Public) map[string]any {
	return map[string]any{
		"platform": c.Platform, "app_key": c.AppKey, "has_secret": c.HasSecret,
		"detection_status": c.DetectionStatus, "detection_checked_at": c.DetectionAt,
		"detection_message": c.DetectionMessage, "active": c.Active, "version": c.Version,
		"updated_at": c.UpdatedAt,
	}
}

func (s *Server) chatConnectorHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	c, err := s.chatConnectors.Get(request.Context(), tenantContext(principal), request.PathValue("platform"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, chatConnectorResponse(c))
}

// 明文 AppSecret 只在这一次请求里出现，处理完即清零。
func (s *Server) saveChatConnectorHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	var appKey, appSecret string
	if err := decodeAccountFields(response, request, map[string]any{"app_key": &appKey, "app_secret": &appSecret}); err != nil {
		writeError(response, request, err)
		return
	}
	secret := []byte(appSecret)
	defer clear(secret)
	c, err := s.chatConnectors.Save(request.Context(), tenantContext(principal), request.PathValue("platform"), appKey, secret)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, chatConnectorResponse(c))
}

func (s *Server) chatConnectorAction(
	action func(*http.Request, domain.TenantContext, string) (chatconnectors.Public, error),
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		principal, _ := principalFromRequest(request)
		c, err := action(request, tenantContext(principal), request.PathValue("platform"))
		if err != nil {
			writeError(response, request, err)
			return
		}
		writeJSON(response, http.StatusOK, chatConnectorResponse(c))
	}
}
