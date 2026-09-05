package httpapi

import (
	"net/http"
	"strconv"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (s *Server) getFactCorrectionHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	workspace, err := s.reviews.GetCorrection(request.Context(), tenantContext(principal), domain.DocumentType(request.PathValue("fact_type")), request.PathValue("fact_id"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"state": workspace.State, "review": reviewResponse(workspace.Review)})
}

func (s *Server) getFactCorrectionHistoryHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	before, limit := 0, 20
	for key, values := range request.URL.Query() {
		if (key != "before_revision" && key != "limit") || len(values) != 1 {
			writeError(response, request, domain.ErrInvalidInput)
			return
		}
		value, err := strconv.Atoi(values[0])
		if err != nil {
			writeError(response, request, domain.ErrInvalidInput)
			return
		}
		if key == "before_revision" {
			before = value
		} else {
			limit = value
		}
	}
	items, err := s.reviews.CorrectionHistory(request.Context(), tenantContext(principal), domain.DocumentType(request.PathValue("fact_type")), request.PathValue("fact_id"), before, limit)
	if err != nil {
		writeError(response, request, err)
		return
	}
	var next *int
	if len(items) == limit {
		last := items[len(items)-1].Revision
		next = &last
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_before_revision": next})
}

func (s *Server) previewFactCorrectionHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var input reviews.CorrectionInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, request, err)
		return
	}
	if input.WithdrawLinkIDs == nil {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	preview, err := s.reviews.PreviewCorrection(request.Context(), tenantContext(principal), domain.DocumentType(request.PathValue("fact_type")), request.PathValue("fact_id"), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, preview)
}

func (s *Server) confirmFactCorrectionHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var input reviews.CorrectionConfirmInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, request, err)
		return
	}
	if input.AcknowledgedDuplicateKeys == nil || input.WithdrawLinkIDs == nil {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	input.IdempotencyKey, input.RequestID = request.Header.Get("Idempotency-Key"), requestIDFromRequest(request)
	result, err := s.reviews.ConfirmCorrection(request.Context(), tenantContext(principal), domain.DocumentType(request.PathValue("fact_type")), request.PathValue("fact_id"), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}
