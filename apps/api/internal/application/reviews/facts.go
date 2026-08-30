package reviews

import (
	"context"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type FactService struct {
	facts ports.FactRepository
	tx    ports.TransactionManager
	ids   ports.IDGenerator
	clock ports.Clock
}

func NewFactService(
	facts ports.FactRepository,
	tx ports.TransactionManager,
	ids ports.IDGenerator,
	clock ports.Clock,
) FactService {
	return FactService{facts: facts, tx: tx, ids: ids, clock: clock}
}

func (s FactService) Delete(
	ctx context.Context,
	tenant domain.TenantContext,
	factType domain.DocumentType,
	factID, requestID string,
) error {
	if err := tenant.Require(domain.CapabilityResourcesDelete); err != nil {
		return err
	}
	if factType != domain.DocumentPayment && factType != domain.DocumentInvoice {
		return domain.ErrInvalidInput
	}
	if factID == "" || requestID == "" {
		return domain.ErrInvalidInput
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return err
	}
	command := ports.FactDeleteCommand{
		TenantID: tenant.TenantID, FactType: factType, FactID: factID,
		ActorUserID: tenant.UserID, AuditEventID: auditID, RequestID: requestID, DeletedAt: s.clock.Now(),
	}
	return s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.DeleteFact(ctx, command)
	})
}

func (s FactService) ListPayments(
	ctx context.Context,
	tenant domain.TenantContext,
) ([]ports.Payment, error) {
	if err := tenant.Require(domain.CapabilityFactsRead); err != nil {
		return nil, err
	}
	return s.facts.ListPayments(ctx, tenant.TenantID)
}

func (s FactService) ListInvoices(
	ctx context.Context,
	tenant domain.TenantContext,
) ([]ports.Invoice, error) {
	if err := tenant.Require(domain.CapabilityFactsRead); err != nil {
		return nil, err
	}
	return s.facts.ListInvoices(ctx, tenant.TenantID)
}
