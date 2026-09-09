package httpapi

import (
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// 绑定码明文只在这一次响应里出现，之后库里只有哈希。因此不做任何形式的重发：
// 忘了就重新生成一张，旧的那张到期自然作废。
func (s *Server) createChatBindingCodeHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	var platform string
	if err := decodeAccountFields(
		response,
		request,
		map[string]any{"platform": &platform},
	); err != nil {
		writeError(response, request, err)
		return
	}
	issued, err := s.chatIntake.IssueBindingCode(request.Context(), tenantContext(principal), platform)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{
		"platform":   platform,
		"code":       issued.Code,
		"expires_at": issued.ExpiresAt,
	})
}

func (s *Server) chatBindingsHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	bindings, err := s.chatIntake.ListBindings(request.Context(), tenantContext(principal))
	if err != nil {
		writeError(response, request, err)
		return
	}
	items := make([]map[string]any, 0, len(bindings))
	for _, binding := range bindings {
		items = append(items, map[string]any{
			"platform":         binding.Platform,
			"external_user_id": binding.ExternalUserID,
			"created_at":       binding.CreatedAt,
		})
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

// 解绑是把投件能力收回来的动作——手机丢了、号不用了。因此只允许解自己的，
// 且不要求任何额外能力。
func (s *Server) deleteChatBindingHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	platform := request.PathValue("platform")
	if platform == "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	if err := s.chatIntake.Unbind(request.Context(), tenantContext(principal), platform); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
