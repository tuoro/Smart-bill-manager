package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/materialexports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (s *Server) previewMaterialExportHandler(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var scope domain.ExportScope
	if err := decodeScalarFields(response, request, map[string]any{"kind": &scope.Kind, "id": &scope.ID}); err != nil {
		writeError(response, request, err)
		return
	}
	manifest, err := s.exports.Preview(request.Context(), tenantContext(principal), scope)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, manifest)
}

func (s *Server) prepareMaterialExportHandler(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var scope domain.ExportScope
	var expectedHash string
	var acknowledged bool
	if err := decodeScalarFields(response, request, map[string]any{"kind": &scope.Kind, "id": &scope.ID, "expected_manifest_hash": &expectedHash, "acknowledged_warnings": &acknowledged}); err != nil {
		writeError(response, request, err)
		return
	}
	if err := setExportDeadline(response, materialexports.PrepareTimeout+5*time.Second); err != nil {
		writeError(response, request, err)
		return
	}
	prepared, err := s.exports.Prepare(request.Context(), materialexports.Actor{Tenant: tenantContext(principal), SessionID: principal.SessionID}, scope, expectedHash, acknowledged)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, prepared)
}

func (s *Server) downloadMaterialExportHandler(response http.ResponseWriter, request *http.Request) {
	// ServeMux 的 GET 也匹配 HEAD；禁止 HEAD/Range 消费一次性句柄。
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", "GET")
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if request.URL.RawQuery != "" || request.Header.Get("Range") != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if err := setExportDeadline(response, materialexports.DownloadTimeout); err != nil {
		writeError(response, request, err)
		return
	}
	download, err := s.exports.Take(materialexports.Actor{Tenant: tenantContext(principal), SessionID: principal.SessionID}, request.PathValue("export_id"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	defer download.Body.Close()
	ctx, cancel := context.WithTimeout(request.Context(), materialexports.DownloadTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { download.Body.Close() })
	defer stop()
	response.Header().Set("Content-Type", "application/zip")
	response.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": download.FileName}))
	response.Header().Set("Content-Length", strconv.FormatInt(download.SizeBytes, 10))
	response.Header().Set("Cache-Control", "private, no-store")
	response.Header().Set("Accept-Ranges", "none")
	if _, err := io.CopyBuffer(response, download.Body, make([]byte, 32*1024)); err != nil {
		s.logger.Warn("material export stream interrupted", "request_id", requestIDFromRequest(request))
	}
}

func (s *Server) cancelMaterialExportHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	err := s.exports.Cancel(materialexports.Actor{Tenant: tenantContext(principal), SessionID: principal.SessionID}, request.PathValue("export_id"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func setExportDeadline(response http.ResponseWriter, duration time.Duration) error {
	err := http.NewResponseController(response).SetWriteDeadline(time.Now().Add(duration))
	// ResponseRecorder 不实现 deadline；真实 net/http ResponseWriter 实现该接口。
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}
