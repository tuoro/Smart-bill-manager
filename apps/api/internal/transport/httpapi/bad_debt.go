package httpapi

import (
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"net/http"
)

func (s *Server) setBadDebtHandler(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeError(w, r, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		Marked          *bool  `json:"marked"`
		ExpectedVersion int    `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if body.Marked == nil {
		writeError(w, r, domain.ErrInvalidInput)
		return
	}
	result, err := s.facts.SetBadDebt(r.Context(), tenantContext(principal), domain.DocumentType(r.PathValue("fact_type")), r.PathValue("fact_id"), domain.BadDebtInput{Marked: *body.Marked, ExpectedVersion: body.ExpectedVersion, Reason: body.Reason}, r.Header.Get("Idempotency-Key"), requestIDFromRequest(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
