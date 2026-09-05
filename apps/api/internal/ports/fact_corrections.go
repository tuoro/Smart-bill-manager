package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type CorrectionAttribution struct {
	Mode                string `json:"mode"`
	AssignmentID        string `json:"assignment_id"`
	CurrentTripID       string `json:"current_trip_id"`
	DesiredTripID       string `json:"desired_trip_id"`
	MatchingTripCount   int    `json:"matching_trip_count"`
	MatchingTripVersion int    `json:"matching_trip_version"`
}

type FactCorrectionState struct {
	FactType                domain.DocumentType     `json:"fact_type"`
	FactID                  string                  `json:"fact_id"`
	Version                 int                     `json:"version"`
	CurrentReviewDecisionID string                  `json:"current_review_decision_id"`
	ClaimSetID              string                  `json:"claim_set_id"`
	DocumentID              string                  `json:"document_id"`
	Links                   []domain.CorrectionLink `json:"links"`
	Attribution             CorrectionAttribution   `json:"attribution"`
}

type FactCorrectionHistory struct {
	ReviewDecisionID         string    `json:"review_decision_id"`
	PreviousReviewDecisionID string    `json:"previous_review_decision_id"`
	ClaimSetID               string    `json:"claim_set_id"`
	Revision                 int       `json:"revision"`
	ActorUserID              string    `json:"actor_user_id"`
	Reason                   string    `json:"reason"`
	CreatedAt                time.Time `json:"created_at"`
}

type FactCorrectionReplay struct {
	RequestHash string
	Result      FactCorrectionResult
}

type FactCorrectionResult struct {
	FactType         domain.DocumentType `json:"fact_type"`
	FactID           string              `json:"fact_id"`
	ReviewDecisionID string              `json:"review_decision_id"`
	ClaimSetID       string              `json:"claim_set_id"`
	Version          int                 `json:"version"`
	Replayed         bool                `json:"replayed"`
}

type FactCorrectionCommand struct {
	State           FactCorrectionState
	Revision        RevisionCommand
	Confirmation    ConfirmCommand
	Reason          string
	RequestHash     string
	PreviewHash     string
	WithdrawLinkIDs []string
}

type FactCorrectionRepository interface {
	GetFactCorrectionState(ctx context.Context, tenantID string, factType domain.DocumentType, factID, proposedPaymentTime string) (FactCorrectionState, error)
	GetFactCorrectionHistory(ctx context.Context, tenantID string, factType domain.DocumentType, factID string, beforeRevision, limit int) ([]FactCorrectionHistory, error)
	GetFactCorrectionReplay(ctx context.Context, tenantID, key string) (FactCorrectionReplay, error)
}

type FactCorrectionTransaction interface {
	CorrectionDuplicateIdentity(ctx context.Context, tenantID string, spec domain.DuplicateCandidateSpec) (string, error)
	InvoiceNumberConflicts(ctx context.Context, tenantID, normalizedNumber, excludedInvoiceID string) (bool, error)
	GetFactCorrectionState(ctx context.Context, tenantID string, factType domain.DocumentType, factID, proposedPaymentTime string) (FactCorrectionState, error)
	ApplyFactCorrection(ctx context.Context, command FactCorrectionCommand) (FactCorrectionResult, error)
}
