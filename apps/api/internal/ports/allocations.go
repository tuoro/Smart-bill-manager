package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type AllocationFactSummary struct {
	FactType       domain.DocumentType `json:"fact_type"`
	ID             string              `json:"id"`
	AmountMinor    int64               `json:"amount_minor"`
	AllocatedMinor int64               `json:"allocated_minor"`
	RemainingMinor int64               `json:"remaining_minor"`
	Currency       string              `json:"currency"`
	BusinessDate   string              `json:"business_date"`
	DisplayName    string              `json:"display_name"`
}

type AllocationWorkspaceLink struct {
	ID             string              `json:"id"`
	TargetFactType domain.DocumentType `json:"target_fact_type"`
	TargetFactID   string              `json:"target_fact_id"`
	AllocatedMinor int64               `json:"allocated_minor"`
	Currency       string              `json:"currency"`
	CreatedAt      time.Time           `json:"created_at"`
}

type AllocationTarget struct {
	FactType                domain.DocumentType `json:"fact_type"`
	ID                      string              `json:"id"`
	AmountMinor             int64               `json:"amount_minor"`
	AllocatedMinor          int64               `json:"allocated_minor"`
	RemainingMinor          int64               `json:"remaining_minor"`
	Currency                string              `json:"currency"`
	BusinessDate            string              `json:"business_date"`
	DisplayName             string              `json:"display_name"`
	NameExact               bool                `json:"name_exact"`
	DateDistanceDays        int                 `json:"date_distance_days"`
	CurrentLinkID           string              `json:"current_link_id,omitempty"`
	CurrentAllocatedMinor   int64               `json:"current_allocated_minor"`
	MaximumAllocatableMinor int64               `json:"maximum_allocatable_minor"`
}

type AllocationWorkspace struct {
	Anchor   AllocationFactSummary     `json:"anchor"`
	Links    []AllocationWorkspaceLink `json:"links"`
	Targets  []AllocationTarget        `json:"targets"`
	PlanHash string                    `json:"plan_hash"`
}

type AllocationAdjustmentResult struct {
	AdjustmentID   string   `json:"adjustment_id"`
	Mode           string   `json:"mode"`
	EndedLinkIDs   []string `json:"ended_link_ids"`
	CreatedLinkIDs []string `json:"created_link_ids"`
	PlanHash       string   `json:"plan_hash"`
	Replayed       bool     `json:"replayed"`
}

type AllocationAdjustmentReplay struct {
	RequestHash string
	Result      AllocationAdjustmentResult
}

type AllocationLinkDraft struct {
	TargetFactID   string
	AllocatedMinor int64
	LinkID         string
}

type AllocationAdjustmentCommand struct {
	TenantID         string
	ActorUserID      string
	AnchorFactType   domain.DocumentType
	AnchorFactID     string
	ExpectedPlanHash string
	Desired          []AllocationLinkDraft
	Reason           string
	IdempotencyKey   string
	RequestHash      string
	AdjustmentID     string
	AuditEventID     string
	RequestID        string
	CreatedAt        time.Time
}

type AllocationRepository interface {
	GetAllocationWorkspace(
		ctx context.Context,
		tenantID string,
		anchorType domain.DocumentType,
		anchorID string,
	) (AllocationWorkspace, error)
	GetAllocationAdjustmentReplay(
		ctx context.Context,
		tenantID, idempotencyKey string,
	) (AllocationAdjustmentReplay, error)
}

type AllocationTransaction interface {
	ApplyAllocationAdjustment(
		ctx context.Context,
		command AllocationAdjustmentCommand,
	) (AllocationAdjustmentResult, error)
}
