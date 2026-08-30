package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (s *Server) previewReimbursementHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		TripID        string   `json:"trip_id"`
		AssignmentIDs []string `json:"assignment_ids"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, request, err)
		return
	}
	if err := validateReimbursementResourceIDs(body.TripID, body.AssignmentIDs); err != nil {
		writeError(response, request, err)
		return
	}
	preview, err := s.reimbursements.Preview(
		request.Context(), tenantContext(principal), body.TripID, body.AssignmentIDs,
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, preview)
}

func (s *Server) submitReimbursementHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		TripID                  string   `json:"trip_id"`
		AssignmentIDs           []string `json:"assignment_ids"`
		ExpectedSnapshotHash    string   `json:"expected_snapshot_hash"`
		AcknowledgedFindingKeys []string `json:"acknowledged_finding_keys"`
		Reason                  string   `json:"reason"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, request, err)
		return
	}
	if err := validateReimbursementResourceIDs(body.TripID, body.AssignmentIDs); err != nil {
		writeError(response, request, err)
		return
	}
	result, err := s.reimbursements.Submit(request.Context(), tenantContext(principal), reimbursements.SubmissionInput{
		TripID:                  body.TripID,
		AssignmentIDs:           body.AssignmentIDs,
		ExpectedSnapshotHash:    body.ExpectedSnapshotHash,
		AcknowledgedFindingKeys: body.AcknowledgedFindingKeys,
		Reason:                  body.Reason,
		IdempotencyKey:          request.Header.Get("Idempotency-Key"),
		RequestID:               requestIDFromRequest(request),
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeJSON(response, status, result)
}

func (s *Server) listReimbursementsHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	limit := 50
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(response, request, domain.NewRuleError("invalid_reimbursement_limit", "报销分页数量必须为 1–100", domain.ErrInvalidInput))
			return
		}
		limit = parsed
	}
	page, err := s.reimbursements.List(
		request.Context(), tenantContext(principal), request.URL.Query().Get("cursor"), limit,
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (s *Server) getReimbursementHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	reimbursementID := request.PathValue("reimbursement_id")
	if !validUUIDString(reimbursementID) {
		writeError(response, request, domain.NewRuleError("invalid_reimbursement_id", "报销资源 ID 格式不正确", domain.ErrInvalidInput))
		return
	}
	detail, err := s.reimbursements.Get(
		request.Context(), tenantContext(principal), reimbursementID,
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (s *Server) changeReimbursementStatusHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	reimbursementID := request.PathValue("reimbursement_id")
	if !validUUIDString(reimbursementID) {
		writeError(response, request, domain.NewRuleError("invalid_reimbursement_id", "报销资源 ID 格式不正确", domain.ErrInvalidInput))
		return
	}
	var body struct {
		ExpectedStatus  domain.ReimbursementStatus `json:"expected_status"`
		DesiredStatus   domain.ReimbursementStatus `json:"desired_status"`
		ExpectedVersion int                        `json:"expected_version"`
		Reason          string                     `json:"reason"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, request, err)
		return
	}
	result, err := s.reimbursements.ChangeStatus(
		request.Context(),
		tenantContext(principal),
		reimbursementID,
		reimbursements.StatusInput{
			ExpectedStatus:  body.ExpectedStatus,
			DesiredStatus:   body.DesiredStatus,
			ExpectedVersion: body.ExpectedVersion,
			Reason:          body.Reason,
			IdempotencyKey:  request.Header.Get("Idempotency-Key"),
			RequestID:       requestIDFromRequest(request),
		},
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func validateReimbursementResourceIDs(tripID string, assignmentIDs []string) error {
	if !validUUIDString(tripID) {
		return domain.NewRuleError("invalid_reimbursement_id", "报销 Trip ID 格式不正确", domain.ErrInvalidInput)
	}
	for _, assignmentID := range assignmentIDs {
		if !validUUIDString(assignmentID) {
			return domain.NewRuleError("invalid_reimbursement_id", "报销 Assignment ID 格式不正确", domain.ErrInvalidInput)
		}
	}
	return nil
}

func validUUIDString(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if value[index] != '-' {
				return false
			}
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}
