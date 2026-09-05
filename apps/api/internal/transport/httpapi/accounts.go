package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/accounts"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (s *Server) workspaceChoicesHandler(response http.ResponseWriter, request *http.Request) {
	var email, password string
	if err := decodeAccountFields(response, request, map[string]any{"email": &email, "password": &password}); err != nil {
		writeError(response, request, err)
		return
	}
	value := []byte(password)
	defer clear(value)
	items, err := s.auth.Workspaces(request.Context(), auth.LoginInput{Email: email, Password: value})
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func accountPageQuery(request *http.Request) (string, int, error) {
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return "", 0, domain.ErrInvalidInput
	}
	for key, values := range query {
		if (key != "cursor" && key != "limit") || len(values) != 1 {
			return "", 0, domain.ErrInvalidInput
		}
	}
	limit := 20
	if raw, exists := query["limit"]; exists {
		limit, err = strconv.Atoi(raw[0])
		if err != nil {
			return "", 0, domain.ErrInvalidInput
		}
	}
	return query.Get("cursor"), limit, nil
}

func (s *Server) membersHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	cursor, limit, err := accountPageQuery(request)
	if err != nil {
		writeError(response, request, err)
		return
	}
	page, err := s.accounts.Members(request.Context(), principal, cursor, limit)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (s *Server) memberHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	member, err := s.accounts.Member(request.Context(), principal, request.PathValue("user_id"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, member)
}

func (s *Server) invitationHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	invitation, err := s.accounts.Invitation(request.Context(), principal, request.PathValue("invitation_id"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, invitation)
}

func (s *Server) invitationsHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	cursor, limit, err := accountPageQuery(request)
	if err != nil {
		writeError(response, request, err)
		return
	}
	page, err := s.accounts.Invitations(request.Context(), principal, cursor, limit)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (s *Server) createInvitationHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	var input accounts.InviteInput
	if err := decodeAccountFields(response, request, map[string]any{"email": &input.Email, "role": &input.Role, "reason": &input.Reason, "idempotency_key": &input.IdempotencyKey}); err != nil {
		writeError(response, request, err)
		return
	}
	result, err := s.accounts.Invite(request.Context(), principal, input, requestIDFromRequest(request))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) revokeInvitationHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	var version int
	var reason string
	if err := decodeAccountFields(response, request, map[string]any{"expected_version": &version, "reason": &reason}); err != nil {
		writeError(response, request, err)
		return
	}
	result, err := s.accounts.Revoke(request.Context(), principal, request.PathValue("invitation_id"), version, reason, requestIDFromRequest(request))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) changeMemberHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	var input accounts.MemberChange
	if err := decodeAccountFields(response, request, map[string]any{"role": &input.Role, "status": &input.Status, "expected_version": &input.ExpectedVersion, "reason": &input.Reason}); err != nil {
		writeError(response, request, err)
		return
	}
	result, err := s.accounts.ChangeMember(request.Context(), principal, request.PathValue("user_id"), input, requestIDFromRequest(request))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) checkInvitationHandler(response http.ResponseWriter, request *http.Request) {
	var code string
	if err := decodeAccountFields(response, request, map[string]any{"code": &code}); err != nil {
		writeError(response, request, err)
		return
	}
	view, err := s.accounts.CheckInvitation(request.Context(), code)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}

func (s *Server) acceptInvitationHandler(response http.ResponseWriter, request *http.Request) {
	var code, displayName, password string
	if err := decodeAccountFields(response, request, map[string]any{"code": &code, "display_name": &displayName, "password": &password}); err != nil {
		writeError(response, request, err)
		return
	}
	value := []byte(password)
	defer clear(value)
	if err := s.accounts.Join(request.Context(), code, displayName, value, requestIDFromRequest(request)); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) changePasswordHandler(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFromRequest(request)
	var current, next string
	if err := decodeAccountFields(response, request, map[string]any{"current_password": &current, "new_password": &next}); err != nil {
		writeError(response, request, err)
		return
	}
	currentBytes, nextBytes := []byte(current), []byte(next)
	defer clear(currentBytes)
	defer clear(nextBytes)
	if err := s.accounts.ChangePassword(request.Context(), principal, currentBytes, nextBytes); err != nil {
		writeError(response, request, err)
		return
	}
	s.clearSessionCookies(response)
	response.WriteHeader(http.StatusNoContent)
}

func decodeAccountFields(response http.ResponseWriter, request *http.Request, fields map[string]any) error {
	if request.URL.RawQuery != "" {
		return domain.ErrInvalidInput
	}
	return decodeScalarFields(response, request, fields)
}
