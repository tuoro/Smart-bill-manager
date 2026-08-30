package httpapi

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const multipartOverheadLimit int64 = ports.MaxDocumentBytes + 1024*1024

func (s *Server) uploadDocumentHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, multipartOverheadLimit)
	reader, err := request.MultipartReader()
	if err != nil {
		writeError(response, request, domain.NewRuleError("invalid_multipart", "上传请求格式不正确", domain.ErrInvalidInput))
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		writeError(response, request, domain.NewRuleError("document_required", "请选择一个文件", domain.ErrInvalidInput))
		return
	}
	content, err := io.ReadAll(io.LimitReader(part, ports.MaxDocumentBytes+1))
	part.Close()
	if err != nil {
		writeError(response, request, domain.NewRuleError("document_read_failed", "文件读取失败", domain.ErrInvalidInput))
		return
	}
	if int64(len(content)) > ports.MaxDocumentBytes {
		writeError(response, request, domain.NewRuleError("document_too_large", "文件不能超过 20 MiB", domain.ErrPayloadTooLarge))
		return
	}
	if extra, extraErr := reader.NextPart(); extraErr == nil || (extra != nil && !errors.Is(extraErr, io.EOF)) {
		if extra != nil {
			extra.Close()
		}
		writeError(response, request, domain.NewRuleError("single_document_only", "每次只能上传一个文件", domain.ErrInvalidInput))
		return
	} else if !errors.Is(extraErr, io.EOF) {
		writeError(response, request, domain.NewRuleError("invalid_multipart", "上传请求格式不正确", domain.ErrInvalidInput))
		return
	}
	started := time.Now()
	result, err := s.upload.Execute(request.Context(), documents.UploadInput{
		Tenant: tenantContext(principal),
		Name:   part.FileName(),
		MIME:   part.Header.Get("Content-Type"),
		Source: bytes.NewReader(content),
	})
	response.Header().Set("Server-Timing", "document-create;dur="+strconv.FormatFloat(float64(time.Since(started))/float64(time.Millisecond), 'f', 3, 64))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{
		"document_id": result.DocumentID,
		"job_id":      result.JobID,
		"status":      result.Status,
		"sha256":      result.SHA256,
	})
}

func (s *Server) downloadDocumentHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	content, err := s.documents.OpenDocument(
		request.Context(),
		tenantContext(principal),
		request.PathValue("document_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	defer content.Body.Close()
	response.Header().Set("Content-Type", content.MIME)
	response.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": content.Name}))
	response.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'; sandbox")
	if _, err := io.Copy(response, content.Body); err != nil {
		s.logger.Warn("document stream interrupted", "request_id", requestIDFromRequest(request))
	}
}

func (s *Server) listJobsHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var status *domain.JobStatus
	if raw := strings.TrimSpace(request.URL.Query().Get("status")); raw != "" {
		parsed := domain.JobStatus(raw)
		status = &parsed
	}
	items, err := s.documents.ListJobs(request.Context(), tenantContext(principal), status)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": jobResponses(items)})
}

func (s *Server) getJobHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	item, err := s.documents.GetJob(request.Context(), tenantContext(principal), request.PathValue("job_id"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, jobResponse(item))
}

func (s *Server) getDocumentHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	item, err := s.documents.GetDocument(
		request.Context(),
		tenantContext(principal),
		request.PathValue("document_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"id":                 item.ID,
		"original_name":      item.OriginalName,
		"declared_mime":      item.DeclaredMIME,
		"detected_mime":      item.DetectedMIME,
		"size_bytes":         item.SizeBytes,
		"sha256":             item.SHA256,
		"page_count":         item.PageCount,
		"status":             item.Status,
		"created_by_user_id": item.CreatedByUserID,
		"created_at":         item.CreatedAt,
	})
}

func (s *Server) deleteDocumentHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if err := s.deletions.Delete(
		request.Context(),
		tenantContext(principal),
		request.PathValue("document_id"),
		requestIDFromRequest(request),
	); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) cancelJobHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	item, err := s.jobActions.Cancel(
		request.Context(),
		tenantContext(principal),
		request.PathValue("job_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, jobResponse(item))
}

func (s *Server) retryJobHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	item, err := s.jobActions.Retry(
		request.Context(),
		tenantContext(principal),
		request.PathValue("job_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, jobResponse(item))
}

func jobResponses(items []ports.JobSummary) []map[string]any {
	responses := make([]map[string]any, 0, len(items))
	for _, item := range items {
		responses = append(responses, jobResponse(item))
	}
	return responses
}

func jobResponse(item ports.JobSummary) map[string]any {
	result := map[string]any{
		"id":            item.ID,
		"document_id":   item.DocumentID,
		"original_name": item.OriginalName,
		"detected_mime": item.DetectedMIME,
		"status":        item.Status,
		"attempt_count": item.AttemptCount,
		"created_at":    item.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		"version":       item.Version,
	}
	if item.ErrorCode != "" {
		result["error_code"] = item.ErrorCode
	}
	if item.SafeErrorMessage != "" {
		result["safe_error_message"] = item.SafeErrorMessage
	}
	return result
}
