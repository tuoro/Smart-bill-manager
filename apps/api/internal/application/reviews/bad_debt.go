package reviews

import (
	"context"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s FactService) SetBadDebt(ctx context.Context, tenant domain.TenantContext, kind domain.DocumentType, id string, input domain.BadDebtInput, key, requestID string) (ports.BadDebtResult, error) {
	if err := tenant.Require(domain.CapabilityAllocationsManage); err != nil {
		return ports.BadDebtResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(key); err != nil {
		return ports.BadDebtResult{}, err
	}
	if requestID == "" {
		return ports.BadDebtResult{}, domain.ErrInvalidInput
	}
	canonical, hash, err := domain.CanonicalBadDebtRequest(kind, id, input)
	if err != nil {
		return ports.BadDebtResult{}, err
	}
	decisionID, err := s.ids.NewID()
	if err != nil {
		return ports.BadDebtResult{}, err
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return ports.BadDebtResult{}, err
	}
	command := ports.BadDebtCommand{TenantID: tenant.TenantID, ActorUserID: tenant.UserID, FactID: id, FactType: kind, Input: canonical, RequestHash: hash, IdempotencyKey: key, RequestID: requestID, DecisionID: decisionID, AuditEventID: auditID, CreatedAt: s.clock.Now()}
	var result ports.BadDebtResult
	err = s.tx.WithinTransaction(ctx, func(tx ports.Transaction) error {
		var err error
		result, err = tx.SetFactBadDebt(ctx, command)
		return err
	})
	return result, err
}
