package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type TripAttributionCursor struct {
	Rank         int
	BusinessDate string
	FactType     domain.DocumentType
	FactID       string
}

type TripAttributionCandidate struct {
	FactType            domain.DocumentType `json:"fact_type"`
	FactID              string              `json:"fact_id"`
	DisplayName         string              `json:"display_name"`
	BusinessDate        string              `json:"business_date"`
	AmountMinor         int64               `json:"amount_minor"`
	Currency            string              `json:"currency"`
	CurrentAssignmentID string              `json:"current_assignment_id,omitempty"`
	CurrentTripID       string              `json:"current_trip_id,omitempty"`
	CurrentTripName     string              `json:"current_trip_name,omitempty"`
	FactVersion         int                 `json:"fact_version"`
	AssignmentMode      string              `json:"assignment_mode"`
	AssignmentState     string              `json:"assignment_state"`
	MatchCount          int                 `json:"match_count"`
	Suggested           bool                `json:"suggested"`
	ReasonCodes         []string            `json:"reason_codes"`
	Rank                int                 `json:"-"`
}

type TripAttributionQuery struct {
	TripID string
	View   string
	Limit  int
	After  *TripAttributionCursor
}

type TripAttributionPage struct {
	Trip       Trip
	Items      []TripAttributionCandidate
	NextCursor *TripAttributionCursor
}

type TripAssignmentResult struct {
	FactVersion          int    `json:"fact_version,omitempty"`
	DecisionID           string `json:"decision_id"`
	Action               string `json:"action"`
	PreviousAssignmentID string `json:"previous_assignment_id,omitempty"`
	AssignmentID         string `json:"assignment_id,omitempty"`
	Replayed             bool   `json:"replayed"`
}

type TripAssignmentReplay struct {
	RequestHash string
	Result      TripAssignmentResult
}

type TripAssignmentCommand struct {
	ExpectedFactVersion  int
	DecisionSource       string
	TenantID             string
	ActorUserID          string
	FactType             domain.DocumentType
	FactID               string
	DesiredTripID        string
	ExpectedAssignmentID string
	Reason               string
	IdempotencyKey       string
	RequestHash          string
	DecisionID           string
	AssignmentID         string
	AuditEventID         string
	RequestID            string
	CreatedAt            time.Time
}

type TripRepository interface {
	TripManagementRepository
	ListTripAttributionCandidates(
		ctx context.Context,
		tenantID string,
		query TripAttributionQuery,
	) (TripAttributionPage, error)
	GetTripAssignmentReplay(
		ctx context.Context,
		tenantID, idempotencyKey string,
	) (TripAssignmentReplay, error)
}

type TripAssignmentTransaction interface {
	TripManagementTransaction
	ApplyTripAssignment(
		ctx context.Context,
		command TripAssignmentCommand,
	) (TripAssignmentResult, error)
}
