package trips

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type ManagementInput struct {
	Details                           domain.TripDetails
	ExpectedVersion                   int
	Reason, IdempotencyKey, RequestID string
}

func (s Service) List(ctx context.Context, tenant domain.TenantContext) ([]ports.Trip, error) {
	if err := tenant.Require(domain.CapabilityFactsRead); err != nil {
		return nil, err
	}
	return s.repository.ListTrips(ctx, tenant.TenantID)
}

func (s Service) Manage(ctx context.Context, tenant domain.TenantContext, tripID, action string, input ManagementInput) (ports.TripManagementResult, error) {
	if err := tenant.Require(domain.CapabilityTripAssignmentsManage); err != nil {
		return ports.TripManagementResult{}, err
	}
	if action == "delete" {
		if err := tenant.Require(domain.CapabilityResourcesDelete); err != nil {
			return ports.TripManagementResult{}, err
		}
	}
	if action != "create" && action != "edit" && action != "delete" {
		return ports.TripManagementResult{}, domain.ErrInvalidInput
	}
	if (action == "create" && (tripID != "" || input.ExpectedVersion != 0)) ||
		(action != "create" && (tripID == "" || input.ExpectedVersion < 1)) || input.RequestID == "" {
		return ports.TripManagementResult{}, domain.ErrInvalidInput
	}
	if err := domain.ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ports.TripManagementResult{}, err
	}
	reason, err := domain.NormalizeTripReason(input.Reason)
	if err != nil {
		return ports.TripManagementResult{}, err
	}
	var details domain.TripDetails
	if action != "delete" {
		details, err = domain.NormalizeTripDetails(input.Details)
		if err != nil {
			return ports.TripManagementResult{}, err
		}
	}
	hash, err := tripRequestHash(struct {
		Action          string
		TripID          string
		ExpectedVersion int
		Details         domain.TripDetails
		Reason          string
	}{action, tripID, input.ExpectedVersion, details, reason})
	if err != nil {
		return ports.TripManagementResult{}, err
	}
	ids, err := s.operationIDs(3)
	if err != nil {
		return ports.TripManagementResult{}, err
	}
	if action == "create" {
		tripID = ids[2]
	}
	command := ports.TripManagementCommand{
		TenantID: tenant.TenantID, ActorUserID: tenant.UserID, TripID: tripID,
		DecisionID: ids[0], AuditEventID: ids[1], Action: action,
		IdempotencyKey: input.IdempotencyKey, RequestHash: hash, Reason: reason,
		RequestID: input.RequestID, ExpectedVersion: input.ExpectedVersion, Details: details, CreatedAt: s.clock.Now(),
	}
	var result ports.TripManagementResult
	err = s.tx.WithinTransaction(ctx, func(tx ports.Transaction) error {
		var operationErr error
		result, operationErr = tx.ManageTrip(ctx, command)
		return operationErr
	})
	return result, err
}

type MaterialInput struct {
	EvidenceID                        string
	DesiredTripID, ExpectedLinkID     *string
	ExpectedVersion                   int
	Reason, IdempotencyKey, RequestID string
}

type MaterialsPage struct {
	Items      []ports.TripEvidence `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

func (s Service) Materials(ctx context.Context, tenant domain.TenantContext, tripID, after string, limit int) (MaterialsPage, error) {
	if err := tenant.Require(domain.CapabilityFactsRead); err != nil {
		return MaterialsPage{}, err
	}
	if limit < 1 || limit > 100 || len(after) > 128 {
		return MaterialsPage{}, domain.ErrInvalidInput
	}
	items, err := s.repository.ListTripEvidence(ctx, tenant.TenantID, tripID, after, limit+1)
	if err != nil {
		return MaterialsPage{}, err
	}
	if items == nil {
		items = []ports.TripEvidence{}
	}
	result := MaterialsPage{Items: items}
	if len(items) > limit {
		result.Items = items[:limit]
		result.NextCursor = items[limit-1].ID
	}
	return result, nil
}

func (s Service) AssignMaterial(ctx context.Context, tenant domain.TenantContext, input MaterialInput) (ports.TripMaterialResult, error) {
	if err := tenant.Require(domain.CapabilityTripAssignmentsManage); err != nil {
		return ports.TripMaterialResult{}, err
	}
	if input.EvidenceID == "" || input.ExpectedVersion < 1 || input.RequestID == "" {
		return ports.TripMaterialResult{}, domain.ErrInvalidInput
	}
	if err := domain.ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ports.TripMaterialResult{}, err
	}
	desired, previous := stringValue(input.DesiredTripID), stringValue(input.ExpectedLinkID)
	if (input.DesiredTripID != nil && desired == "") || (input.ExpectedLinkID != nil && previous == "") {
		return ports.TripMaterialResult{}, domain.ErrInvalidInput
	}
	reason, err := domain.NormalizeTripReason(input.Reason)
	if err != nil {
		return ports.TripMaterialResult{}, err
	}
	hash, err := tripRequestHash(struct {
		EvidenceID, DesiredTripID, PreviousLinkID, Reason string
		Version                                           int
	}{input.EvidenceID, desired, previous, reason, input.ExpectedVersion})
	if err != nil {
		return ports.TripMaterialResult{}, err
	}
	ids, err := s.operationIDs(3)
	if err != nil {
		return ports.TripMaterialResult{}, err
	}
	command := ports.TripMaterialCommand{
		TenantID: tenant.TenantID, ActorUserID: tenant.UserID, EvidenceID: input.EvidenceID,
		DesiredTripID: desired, ExpectedLinkID: previous, ExpectedVersion: input.ExpectedVersion,
		DecisionID: ids[0], LinkID: ids[1], AuditEventID: ids[2], Reason: reason,
		IdempotencyKey: input.IdempotencyKey, RequestHash: hash, RequestID: input.RequestID, CreatedAt: s.clock.Now(),
	}
	var result ports.TripMaterialResult
	err = s.tx.WithinTransaction(ctx, func(tx ports.Transaction) error {
		var e error
		result, e = tx.AssignTripMaterial(ctx, command)
		return e
	})
	return result, err
}

func (s Service) Preference(ctx context.Context, tenant domain.TenantContext, paymentID, mode, requestID string, expectedVersion int) error {
	if err := tenant.Require(domain.CapabilityTripAssignmentsManage); err != nil {
		return err
	}
	if paymentID == "" || requestID == "" || expectedVersion < 1 || (mode != "auto" && mode != "blocked") {
		return domain.ErrInvalidInput
	}
	ids, err := s.operationIDs(1)
	if err != nil {
		return err
	}
	command := ports.TripPreferenceCommand{TenantID: tenant.TenantID, ActorUserID: tenant.UserID, PaymentID: paymentID,
		Mode: mode, AuditEventID: ids[0], RequestID: requestID, ExpectedVersion: expectedVersion, CreatedAt: s.clock.Now()}
	return s.tx.WithinTransaction(ctx, func(tx ports.Transaction) error { return tx.ChangeTripPreference(ctx, command) })
}

func (s Service) operationIDs(count int) ([]string, error) {
	result := make([]string, count)
	for i := range result {
		id, err := s.ids.NewID()
		if err != nil {
			return nil, err
		}
		result[i] = id
	}
	return result, nil
}

func tripRequestHash(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(append([]byte("trip-workspace/1:"), data...))
	return hex.EncodeToString(hash[:]), nil
}
