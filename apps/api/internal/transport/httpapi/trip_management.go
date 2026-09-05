package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (s *Server) createTripHandler(w http.ResponseWriter, r *http.Request) {
	s.manageTripHandler(w, r, "create")
}
func (s *Server) editTripHandler(w http.ResponseWriter, r *http.Request) {
	s.manageTripHandler(w, r, "edit")
}
func (s *Server) deleteTripHandler(w http.ResponseWriter, r *http.Request) {
	s.manageTripHandler(w, r, "delete")
}

func (s *Server) manageTripHandler(w http.ResponseWriter, r *http.Request, action string) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeError(w, r, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		domain.TripDetails
		ExpectedVersion int    `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.trips.Manage(r.Context(), tenantContext(principal), r.PathValue("trip_id"), action, trips.ManagementInput{
		Details: body.TripDetails, ExpectedVersion: body.ExpectedVersion, Reason: body.Reason,
		IdempotencyKey: r.Header.Get("Idempotency-Key"), RequestID: requestIDFromRequest(r),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	status := http.StatusOK
	if action == "create" && !result.Replayed {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

func (s *Server) listTripEvidenceHandler(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeError(w, r, domain.ErrUnauthenticated)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, r, domain.ErrInvalidInput)
			return
		}
	}
	page, err := s.trips.Materials(r.Context(), tenantContext(principal), r.URL.Query().Get("trip_id"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) assignTripMaterialHandler(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeError(w, r, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		EvidenceID      string          `json:"evidence_id"`
		DesiredTripID   json.RawMessage `json:"desired_trip_id"`
		ExpectedLinkID  json.RawMessage `json:"expected_link_id"`
		ExpectedVersion int             `json:"expected_version"`
		Reason          string          `json:"reason"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	desired, err := decodeRequiredNullableString(body.DesiredTripID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	previous, err := decodeRequiredNullableString(body.ExpectedLinkID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.trips.AssignMaterial(r.Context(), tenantContext(principal), trips.MaterialInput{
		EvidenceID: body.EvidenceID, DesiredTripID: desired, ExpectedLinkID: previous, ExpectedVersion: body.ExpectedVersion,
		Reason: body.Reason, IdempotencyKey: r.Header.Get("Idempotency-Key"), RequestID: requestIDFromRequest(r),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) tripPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromRequest(r)
	if !ok {
		writeError(w, r, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		Mode            string `json:"mode"`
		ExpectedVersion int    `json:"expected_version"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.trips.Preference(r.Context(), tenantContext(principal), r.PathValue("payment_id"), body.Mode,
		requestIDFromRequest(r), body.ExpectedVersion); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
