package httpapi

import (
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (s *Server) startManualReviewHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		ExpectedJobVersion int                 `json:"expected_job_version"`
		DocumentType       domain.DocumentType `json:"document_type"`
		Reason             string              `json:"reason"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, request, err)
		return
	}
	result, err := s.reviews.StartManualReview(request.Context(), tenantContext(principal), request.PathValue("job_id"), reviews.ManualReviewInput{
		ExpectedJobVersion: body.ExpectedJobVersion, DocumentType: body.DocumentType, Reason: body.Reason, IdempotencyKey: request.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}
