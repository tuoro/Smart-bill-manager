package documents

import (
	"context"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type ActionService struct {
	jobs  ports.JobRepository
	tx    ports.TransactionManager
	ids   ports.IDGenerator
	clock ports.Clock
}

func NewActionService(
	jobs ports.JobRepository,
	tx ports.TransactionManager,
	ids ports.IDGenerator,
	clock ports.Clock,
) ActionService {
	return ActionService{jobs: jobs, tx: tx, ids: ids, clock: clock}
}

func (s ActionService) Cancel(
	ctx context.Context,
	tenant domain.TenantContext,
	jobID string,
) (ports.JobSummary, error) {
	if err := tenant.Require(domain.CapabilityDocumentsProcess); err != nil {
		return ports.JobSummary{}, err
	}
	decisionID, err := s.ids.NewID()
	if err != nil {
		return ports.JobSummary{}, err
	}
	err = s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.RequestJobCancellation(
			ctx,
			tenant.TenantID,
			jobID,
			tenant.UserID,
			decisionID,
			"cancel:"+decisionID,
			s.clock.Now(),
		)
	})
	if err != nil {
		return ports.JobSummary{}, err
	}
	return s.jobs.GetJob(ctx, tenant.TenantID, jobID)
}

func (s ActionService) Retry(
	ctx context.Context,
	tenant domain.TenantContext,
	jobID string,
) (ports.JobSummary, error) {
	if err := tenant.Require(domain.CapabilityDocumentsProcess); err != nil {
		return ports.JobSummary{}, err
	}
	if err := s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.RetryJob(ctx, tenant.TenantID, jobID)
	}); err != nil {
		return ports.JobSummary{}, err
	}
	return s.jobs.GetJob(ctx, tenant.TenantID, jobID)
}
