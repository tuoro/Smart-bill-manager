package httpapi

import (
	"net/http"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/providers"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type createProviderRequest struct {
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	OutputMode string `json:"output_mode"`
}

func (s *Server) createProviderConfigHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var input createProviderRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, request, err)
		return
	}
	apiKey := []byte(input.APIKey)
	defer clear(apiKey)
	config, err := s.providers.Create(request.Context(), providers.CreateInput{
		Tenant:     tenantContext(principal),
		BaseURL:    input.BaseURL,
		APIKey:     apiKey,
		Model:      input.Model,
		OutputMode: input.OutputMode,
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, providerConfigResponse(config))
}

func (s *Server) listProviderConfigsHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	configs, err := s.providers.List(request.Context(), tenantContext(principal))
	if err != nil {
		writeError(response, request, err)
		return
	}
	items := make([]map[string]any, 0, len(configs))
	for _, config := range configs {
		items = append(items, providerConfigResponse(config))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) detectProviderConfigHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	config, err := s.providers.Detect(
		request.Context(),
		tenantContext(principal),
		request.PathValue("provider_config_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, providerConfigResponse(config))
}

func (s *Server) activateProviderConfigHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	config, err := s.providers.Activate(
		request.Context(),
		tenantContext(principal),
		request.PathValue("provider_config_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, providerConfigResponse(config))
}

func (s *Server) deleteProviderConfigHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if err := s.providers.Delete(
		request.Context(),
		tenantContext(principal),
		request.PathValue("provider_config_id"),
		requestIDFromRequest(request),
	); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func providerConfigResponse(config ports.ProviderConfig) map[string]any {
	result := map[string]any{
		"id":                config.ID,
		"base_url":          config.BaseURL,
		"model":             config.Model,
		"output_mode":       config.OutputMode,
		"capability_status": config.CapabilityStatus,
		"active":            config.Active,
		"version":           config.Version,
		"safe_fingerprint":  config.SafeFingerprint,
	}
	if config.CapabilityCheckedAt != nil {
		result["capability_checked_at"] = config.CapabilityCheckedAt.Format(time.RFC3339Nano)
	}
	if config.CapabilitySafeMessage != "" {
		result["capability_safe_message"] = config.CapabilitySafeMessage
	}
	if config.CapabilitySchemaVersion != "" {
		result["capability_schema_version"] = config.CapabilitySchemaVersion
	}
	if config.CapabilitySchemaSHA256 != "" {
		result["capability_schema_sha256"] = config.CapabilitySchemaSHA256
	}
	return result
}
