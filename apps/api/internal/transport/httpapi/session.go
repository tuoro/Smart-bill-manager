package httpapi

import (
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TenantID string `json:"tenant_id,omitempty"`
}

func (s *Server) loginHandler(response http.ResponseWriter, request *http.Request) {
	var input loginRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, request, err)
		return
	}
	password := []byte(input.Password)
	defer clear(password)
	view, err := s.auth.Login(request.Context(), auth.LoginInput{
		Email:    input.Email,
		Password: password,
		TenantID: input.TenantID,
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	s.setSessionCookies(response, view)
	writeJSON(response, http.StatusOK, sessionResponseFromView(view))
}

func (s *Server) sessionHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	csrfCookie, err := request.Cookie(csrfCookieName)
	if err != nil || s.auth.VerifyCSRF(principal, csrfCookie.Value) != nil {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	writeJSON(response, http.StatusOK, sessionResponseFromPrincipal(principal, csrfCookie.Value))
}

func (s *Server) logoutHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if err := s.auth.Logout(request.Context(), principal); err != nil {
		writeError(response, request, err)
		return
	}
	s.clearSessionCookies(response)
	response.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	User struct {
		ID          string `json:"id"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
	} `json:"user"`
	Tenant struct {
		ID              string          `json:"id"`
		Name            string          `json:"name"`
		DefaultCurrency domain.Currency `json:"default_currency"`
		Timezone        string          `json:"timezone"`
	} `json:"tenant"`
	Role         domain.Role         `json:"role"`
	Capabilities []domain.Capability `json:"capabilities"`
	CSRFToken    string              `json:"csrf_token"`
	ExpiresAt    string              `json:"expires_at"`
}

func sessionResponseFromView(view auth.SessionView) sessionResponse {
	response := sessionResponse{
		Role:         view.Role,
		Capabilities: view.Capabilities,
		CSRFToken:    view.CSRFToken,
		ExpiresAt:    view.ExpiresAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
	response.User.ID = view.UserID
	response.User.Email = view.Email
	response.User.DisplayName = view.DisplayName
	response.Tenant.ID = view.TenantID
	response.Tenant.Name = view.TenantName
	response.Tenant.DefaultCurrency = view.Currency
	response.Tenant.Timezone = view.Timezone
	return response
}

func sessionResponseFromPrincipal(principal ports.SessionPrincipal, csrfToken string) sessionResponse {
	view := auth.SessionView{
		UserID:       principal.UserID,
		Email:        principal.Email,
		DisplayName:  principal.DisplayName,
		TenantID:     principal.TenantID,
		TenantName:   principal.TenantName,
		Currency:     principal.Currency,
		Timezone:     principal.Timezone,
		Role:         principal.Role,
		Capabilities: principal.Role.Capabilities(),
		CSRFToken:    csrfToken,
		ExpiresAt:    principal.ExpiresAt,
	}
	return sessionResponseFromView(view)
}
