package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type TripManagementCommand struct {
	TenantID, ActorUserID, TripID, DecisionID, AuditEventID string
	Action, IdempotencyKey, RequestHash, Reason, RequestID  string
	ExpectedVersion                                         int
	Details                                                 domain.TripDetails
	CreatedAt                                               time.Time
}

type TripManagementResult struct {
	TripID   string `json:"trip_id"`
	Version  int    `json:"version"`
	Replayed bool   `json:"replayed"`
}

type TripEvidence struct {
	ID               string  `json:"id"`
	DocumentID       string  `json:"document_id"`
	Destination      string  `json:"destination"`
	StartDate        string  `json:"start_date"`
	EndDate          string  `json:"end_date"`
	Origin           *string `json:"origin,omitempty"`
	TransportType    *string `json:"transport_type,omitempty"`
	BookingReference *string `json:"booking_reference,omitempty"`
	Version          int     `json:"version"`
	CurrentLinkID    string  `json:"current_link_id,omitempty"`
	CurrentTripID    string  `json:"current_trip_id,omitempty"`
	CurrentTripName  string  `json:"current_trip_name,omitempty"`
}

type TripMaterialCommand struct {
	TenantID, ActorUserID, EvidenceID, DesiredTripID, ExpectedLinkID                 string
	DecisionID, LinkID, AuditEventID, IdempotencyKey, RequestHash, Reason, RequestID string
	ExpectedVersion                                                                  int
	CreatedAt                                                                        time.Time
}

type TripMaterialResult struct {
	LinkID   string `json:"link_id,omitempty"`
	Version  int    `json:"version"`
	Replayed bool   `json:"replayed"`
}

type TripPreferenceCommand struct {
	TenantID, ActorUserID, PaymentID, Mode, AuditEventID, RequestID string
	ExpectedVersion                                                 int
	CreatedAt                                                       time.Time
}

type TripManagementRepository interface {
	ListTrips(context.Context, string) ([]Trip, error)
	ListTripEvidence(context.Context, string, string, string, int) ([]TripEvidence, error)
}

type TripManagementTransaction interface {
	ManageTrip(context.Context, TripManagementCommand) (TripManagementResult, error)
	AssignTripMaterial(context.Context, TripMaterialCommand) (TripMaterialResult, error)
	ChangeTripPreference(context.Context, TripPreferenceCommand) error
}
