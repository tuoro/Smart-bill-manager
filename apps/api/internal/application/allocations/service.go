package allocations

import (
	"context"
	"errors"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type Service struct {
	repository ports.AllocationRepository
	tx         ports.TransactionManager
	ids        ports.IDGenerator
	clock      ports.Clock
}

type AdjustmentInput struct {
	ExpectedPlanHash   string
	DesiredAllocations []domain.DesiredAllocation
	Reason             string
	IdempotencyKey     string
	RequestID          string
}

func NewService(
	repository ports.AllocationRepository,
	tx ports.TransactionManager,
	ids ports.IDGenerator,
	clock ports.Clock,
) Service {
	return Service{repository: repository, tx: tx, ids: ids, clock: clock}
}

func (s Service) GetWorkspace(
	ctx context.Context,
	tenant domain.TenantContext,
	anchorType domain.DocumentType,
	anchorID string,
) (ports.AllocationWorkspace, error) {
	if err := tenant.Require(domain.CapabilityAllocationsManage); err != nil {
		return ports.AllocationWorkspace{}, err
	}
	if !validAnchor(anchorType, anchorID) {
		return ports.AllocationWorkspace{}, domain.NewRuleError("invalid_allocation_anchor", "分配 anchor 不合法", domain.ErrInvalidInput)
	}
	workspace, err := s.repository.GetAllocationWorkspace(ctx, tenant.TenantID, anchorType, anchorID)
	if err != nil {
		return ports.AllocationWorkspace{}, err
	}
	if workspace.Links == nil {
		workspace.Links = []ports.AllocationWorkspaceLink{}
	}
	if workspace.Targets == nil {
		workspace.Targets = []ports.AllocationTarget{}
	}
	return workspace, nil
}

func (s Service) Adjust(
	ctx context.Context,
	tenant domain.TenantContext,
	anchorType domain.DocumentType,
	anchorID string,
	input AdjustmentInput,
) (ports.AllocationAdjustmentResult, error) {
	if err := tenant.Require(domain.CapabilityAllocationsManage); err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	if input.RequestID == "" {
		return ports.AllocationAdjustmentResult{}, errors.New("request id is required")
	}
	canonical, reason, requestHash, err := domain.CanonicalAllocationAdjustmentRequest(
		anchorType,
		anchorID,
		input.ExpectedPlanHash,
		input.DesiredAllocations,
		input.Reason,
	)
	if err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	replay, replayErr := s.repository.GetAllocationAdjustmentReplay(
		ctx,
		tenant.TenantID,
		input.IdempotencyKey,
	)
	if replayErr == nil {
		if replay.RequestHash != requestHash {
			return ports.AllocationAdjustmentResult{}, domain.NewRuleError(
				"idempotency_key_conflict",
				"幂等键已用于不同的分配调整",
				domain.ErrConflict,
			)
		}
		return replay.Result, nil
	}
	if !errors.Is(replayErr, domain.ErrNotFound) {
		return ports.AllocationAdjustmentResult{}, replayErr
	}
	adjustmentID, err := s.ids.NewID()
	if err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	auditEventID, err := s.ids.NewID()
	if err != nil {
		return ports.AllocationAdjustmentResult{}, err
	}
	drafts := make([]ports.AllocationLinkDraft, 0, len(canonical))
	for _, item := range canonical {
		linkID, err := s.ids.NewID()
		if err != nil {
			return ports.AllocationAdjustmentResult{}, err
		}
		drafts = append(drafts, ports.AllocationLinkDraft{
			TargetFactID:   item.TargetFactID,
			AllocatedMinor: item.AllocatedMinor,
			LinkID:         linkID,
		})
	}
	command := ports.AllocationAdjustmentCommand{
		TenantID:         tenant.TenantID,
		ActorUserID:      tenant.UserID,
		AnchorFactType:   anchorType,
		AnchorFactID:     anchorID,
		ExpectedPlanHash: input.ExpectedPlanHash,
		Desired:          drafts,
		Reason:           reason,
		IdempotencyKey:   input.IdempotencyKey,
		RequestHash:      requestHash,
		AdjustmentID:     adjustmentID,
		AuditEventID:     auditEventID,
		RequestID:        input.RequestID,
		CreatedAt:        s.clock.Now(),
	}
	var result ports.AllocationAdjustmentResult
	err = s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		var operationErr error
		result, operationErr = transaction.ApplyAllocationAdjustment(ctx, command)
		return operationErr
	})
	if err == nil {
		return result, nil
	}
	recovered, recoveredErr := s.repository.GetAllocationAdjustmentReplay(ctx, tenant.TenantID, input.IdempotencyKey)
	if recoveredErr == nil && recovered.RequestHash == requestHash {
		return recovered.Result, nil
	}
	return ports.AllocationAdjustmentResult{}, err
}

func validAnchor(anchorType domain.DocumentType, anchorID string) bool {
	return anchorID != "" && (anchorType == domain.DocumentPayment || anchorType == domain.DocumentInvoice)
}
