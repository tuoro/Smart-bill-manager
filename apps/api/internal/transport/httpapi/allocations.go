package httpapi

import (
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (s *Server) getAllocationWorkspaceHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	workspace, err := s.allocations.GetWorkspace(
		request.Context(),
		tenantContext(principal),
		domain.DocumentType(request.PathValue("fact_type")),
		request.PathValue("fact_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, workspace)
}

func (s *Server) adjustAllocationHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		ExpectedPlanHash   string `json:"expected_plan_hash"`
		DesiredAllocations *[]struct {
			TargetFactID   string `json:"target_fact_id"`
			AllocatedMinor int64  `json:"allocated_minor"`
		} `json:"desired_allocations"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, request, err)
		return
	}
	if body.DesiredAllocations == nil {
		writeError(response, request, domain.NewRuleError("invalid_allocation_plan", "desired_allocations 必须是数组", domain.ErrInvalidInput))
		return
	}
	input := allocations.AdjustmentInput{
		ExpectedPlanHash:   body.ExpectedPlanHash,
		DesiredAllocations: make([]domain.DesiredAllocation, 0, len(*body.DesiredAllocations)),
		Reason:             body.Reason,
		IdempotencyKey:     request.Header.Get("Idempotency-Key"),
		RequestID:          requestIDFromRequest(request),
	}
	for _, item := range *body.DesiredAllocations {
		input.DesiredAllocations = append(input.DesiredAllocations, domain.DesiredAllocation{
			TargetFactID:   item.TargetFactID,
			AllocatedMinor: item.AllocatedMinor,
		})
	}
	result, err := s.allocations.Adjust(
		request.Context(),
		tenantContext(principal),
		domain.DocumentType(request.PathValue("fact_type")),
		request.PathValue("fact_id"),
		input,
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}
