package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (s *Server) listTripAttributionCandidatesHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	view := strings.TrimSpace(request.URL.Query().Get("view"))
	if view == "" {
		view = domain.TripAttributionViewSuggested
	}
	limit := 50
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(response, request, domain.NewRuleError("invalid_trip_attribution_limit", "行程归属分页数量必须为 1–100", domain.ErrInvalidInput))
			return
		}
		limit = parsed
	}
	page, err := s.trips.AttributionCandidates(
		request.Context(),
		tenantContext(principal),
		request.PathValue("trip_id"),
		view,
		request.URL.Query().Get("cursor"),
		limit,
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (s *Server) assignTripFactHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		FactType             domain.DocumentType `json:"fact_type"`
		FactID               string              `json:"fact_id"`
		DesiredTripID        json.RawMessage     `json:"desired_trip_id"`
		ExpectedAssignmentID json.RawMessage     `json:"expected_assignment_id"`
		Reason               string              `json:"reason"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, request, err)
		return
	}
	desiredTripID, err := decodeRequiredNullableString(body.DesiredTripID)
	if err != nil {
		writeError(response, request, err)
		return
	}
	expectedAssignmentID, err := decodeRequiredNullableString(body.ExpectedAssignmentID)
	if err != nil {
		writeError(response, request, err)
		return
	}
	result, err := s.trips.Assign(request.Context(), tenantContext(principal), trips.AssignmentInput{
		FactType:             body.FactType,
		FactID:               body.FactID,
		DesiredTripID:        desiredTripID,
		ExpectedAssignmentID: expectedAssignmentID,
		Reason:               body.Reason,
		IdempotencyKey:       request.Header.Get("Idempotency-Key"),
		RequestID:            requestIDFromRequest(request),
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func decodeRequiredNullableString(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 {
		return nil, domain.NewRuleError("invalid_trip_assignment", "desired_trip_id 与 expected_assignment_id 必须显式提供字符串或 null", domain.ErrInvalidInput)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, domain.NewRuleError("invalid_trip_assignment", "desired_trip_id 与 expected_assignment_id 必须显式提供字符串或 null", domain.ErrInvalidInput)
	}
	return &value, nil
}
