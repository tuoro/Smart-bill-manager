package ports

import (
	"context"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type ManualReviewState struct {
	Document Document
	Status   domain.JobStatus
	Version  int
	HasClaim bool
	Pages    []NormalizedPage
}

type ManualReviewReplay struct {
	ClaimSetID  string
	JobID       string
	RequestHash string
}

type ManualReviewCommand struct {
	Revision           RevisionCommand
	ExpectedJobVersion int
	Reason             string
	IdempotencyKey     string
	RequestHash        string
	AuditID            string
}

type ManualReviewTransaction interface {
	LockManualReview(ctx context.Context, tenantID, jobID string) (ManualReviewState, error)
	FindManualReviewReplay(ctx context.Context, tenantID, key string) (ManualReviewReplay, error)
	PersistManualReview(ctx context.Context, command ManualReviewCommand) error
}
