package httpapi

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	applicationemails "github.com/tuoro/smart-bill-manager/apps/api/internal/application/emails"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (s *Server) registerEmailSourceHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var registration domain.EmailSourceRegistration
	if err := decodeJSON(response, request, &registration); err != nil {
		writeError(response, request, err)
		return
	}
	result, err := s.emails.Register(request.Context(), applicationemails.RegisterInput{
		Tenant: tenantContext(principal), Registration: registration,
		IdempotencyKey: request.Header.Get("Idempotency-Key"), RequestID: requestIDFromRequest(request),
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeJSON(response, status, result.Source)
}

func (s *Server) listEmailSourcesHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	items, err := s.emails.ListSources(request.Context(), tenantContext(principal))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listEmailMessagesHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	limit := 0
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(response, request, domain.NewRuleError("invalid_email_page_limit", "邮件分页数量必须为 1–100", domain.ErrInvalidInput))
			return
		}
		limit = parsed
	}
	page, err := s.emails.ListMessages(
		request.Context(), tenantContext(principal), request.PathValue("source_id"),
		request.URL.Query().Get("cursor"), limit,
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (s *Server) downloadEmailMessageHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	content, err := s.emails.OpenMessage(
		request.Context(), tenantContext(principal), request.PathValue("message_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	defer content.Body.Close()
	s.writeArchivedDownload(response, request, content)
}

func (s *Server) downloadEmailAttachmentHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	content, err := s.emails.OpenAttachment(
		request.Context(), tenantContext(principal), request.PathValue("attachment_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	defer content.Body.Close()
	s.writeArchivedDownload(response, request, content)
}

func (s *Server) writeArchivedDownload(
	response http.ResponseWriter,
	request *http.Request,
	content applicationemails.ArchivedContent,
) {
	response.Header().Set("Content-Type", content.MIME)
	response.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": content.Name}))
	response.Header().Set("Cache-Control", "private, no-store")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; sandbox")
	response.Header().Set("X-Download-Options", "noopen")
	if _, err := io.Copy(response, content.Body); err != nil {
		s.logger.Warn("email archive stream interrupted", "request_id", requestIDFromRequest(request))
	}
}
