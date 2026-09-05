package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type ReimbursementCursor struct {
	CreatedAt time.Time
	ID        string
}

type ReimbursementSummary struct {
	ID           string                           `json:"id"`
	Trip         domain.ReimbursementTripSnapshot `json:"trip"`
	TripDeleted  bool                             `json:"trip_deleted"`
	Status       domain.ReimbursementStatus       `json:"status"`
	Version      int                              `json:"version"`
	ItemCount    int                              `json:"item_count"`
	FindingCount int                              `json:"finding_count"`
	CreatedAt    time.Time                        `json:"created_at"`
	UpdatedAt    time.Time                        `json:"updated_at"`
}

type ReimbursementListQuery struct {
	Limit int
	After *ReimbursementCursor
}

type ReimbursementListPage struct {
	Items      []ReimbursementSummary
	NextCursor *ReimbursementCursor
}

type ReimbursementItem struct {
	FactReviewDecisionID *string             `json:"fact_review_decision_id"`
	ID                   string              `json:"id"`
	AssignmentID         string              `json:"assignment_id"`
	FactType             domain.DocumentType `json:"fact_type"`
	FactID               string              `json:"fact_id"`
	SourceDeleted        bool                `json:"source_deleted"`
	DisplayName          string              `json:"display_name"`
	BusinessDate         string              `json:"business_date"`
	AmountMinor          int64               `json:"amount_minor"`
	Currency             domain.Currency     `json:"currency"`
	SortOrder            int                 `json:"sort_order"`
}

type ReimbursementFinding struct {
	ID                     string                     `json:"id"`
	ItemID                 string                     `json:"item_id"`
	FindingKey             string                     `json:"finding_key"`
	Code                   string                     `json:"code"`
	ExpectedMinor          *int64                     `json:"expected_minor,omitempty"`
	ActualMinor            *int64                     `json:"actual_minor,omitempty"`
	Currency               domain.Currency            `json:"currency,omitempty"`
	RelatedReimbursementID string                     `json:"related_reimbursement_id,omitempty"`
	RelatedStatus          domain.ReimbursementStatus `json:"related_status,omitempty"`
}

type ReimbursementDecision struct {
	ID              string                      `json:"id"`
	Action          string                      `json:"action"`
	PreviousStatus  *domain.ReimbursementStatus `json:"previous_status"`
	DesiredStatus   domain.ReimbursementStatus  `json:"desired_status"`
	ExpectedVersion int                         `json:"expected_version"`
	ResultVersion   int                         `json:"result_version"`
	Reason          string                      `json:"reason"`
	CreatedAt       time.Time                   `json:"created_at"`
}

type ReimbursementDetail struct {
	MaterialsCaptured bool   `json:"materials_captured"`
	MaterialCount     *int64 `json:"material_count"`
	ReimbursementSummary
	RuleVersion  string                              `json:"rule_version"`
	SnapshotHash string                              `json:"snapshot_hash"`
	Totals       []domain.ReimbursementCurrencyTotal `json:"totals_by_currency"`
	Items        []ReimbursementItem                 `json:"items"`
	Findings     []ReimbursementFinding              `json:"findings"`
	Decisions    []ReimbursementDecision             `json:"decisions"`
}

type ReimbursementMutationResult struct {
	ReimbursementID string                     `json:"reimbursement_id"`
	DecisionID      string                     `json:"decision_id"`
	Status          domain.ReimbursementStatus `json:"status"`
	Version         int                        `json:"version"`
	Replayed        bool                       `json:"replayed"`
}

type ReimbursementDecisionReplay struct {
	RequestHash string
	Result      ReimbursementMutationResult
}

type ReimbursementItemDraft struct {
	AssignmentID string
	ID           string
}

type ReimbursementFindingDraft struct {
	FindingKey string
	ID         string
}

type ReimbursementSubmissionCommand struct {
	TenantID                string
	ActorUserID             string
	TripID                  string
	AssignmentIDs           []string
	ExpectedSnapshotHash    string
	AcknowledgedFindingKeys []string
	Reason                  string
	IdempotencyKey          string
	RequestHash             string
	ReimbursementID         string
	DecisionID              string
	AuditEventID            string
	ItemDrafts              []ReimbursementItemDraft
	FindingDrafts           []ReimbursementFindingDraft
	RequestID               string
	CreatedAt               time.Time
}

type ReimbursementStatusCommand struct {
	TenantID        string
	ActorUserID     string
	ReimbursementID string
	ExpectedStatus  domain.ReimbursementStatus
	DesiredStatus   domain.ReimbursementStatus
	ExpectedVersion int
	Action          string
	Reason          string
	IdempotencyKey  string
	RequestHash     string
	DecisionID      string
	AuditEventID    string
	RequestID       string
	CreatedAt       time.Time
}

type ReimbursementRepository interface {
	BuildReimbursementPreview(
		ctx context.Context,
		tenantID, tripID string,
		assignmentIDs []string,
	) (domain.ReimbursementPolicySnapshot, error)
	ListReimbursements(
		ctx context.Context,
		tenantID string,
		query ReimbursementListQuery,
	) (ReimbursementListPage, error)
	GetReimbursement(
		ctx context.Context,
		tenantID, reimbursementID string,
	) (ReimbursementDetail, error)
	GetReimbursementDecisionReplay(
		ctx context.Context,
		tenantID, idempotencyKey string,
	) (ReimbursementDecisionReplay, error)
}

type ReimbursementTransaction interface {
	SubmitReimbursement(
		ctx context.Context,
		command ReimbursementSubmissionCommand,
	) (ReimbursementMutationResult, error)
	ApplyReimbursementStatus(
		ctx context.Context,
		command ReimbursementStatusCommand,
	) (ReimbursementMutationResult, error)
}
