package trips

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const tripAttributionCursorVersion = "trip-attribution-cursor/1"

type Service struct {
	repository ports.TripRepository
	tx         ports.TransactionManager
	ids        ports.IDGenerator
	clock      ports.Clock
}

func NewService(
	repository ports.TripRepository,
	tx ports.TransactionManager,
	ids ports.IDGenerator,
	clock ports.Clock,
) Service {
	return Service{repository: repository, tx: tx, ids: ids, clock: clock}
}

type AttributionPage struct {
	Trip        ports.Trip                       `json:"trip"`
	RuleVersion string                           `json:"rule_version"`
	Items       []ports.TripAttributionCandidate `json:"items"`
	NextCursor  string                           `json:"next_cursor,omitempty"`
}

type AssignmentInput struct {
	FactType             domain.DocumentType
	FactID               string
	DesiredTripID        *string
	ExpectedAssignmentID *string
	Reason               string
	IdempotencyKey       string
	RequestID            string
}

type cursorEnvelope struct {
	Version      string              `json:"version"`
	TripID       string              `json:"trip_id"`
	View         string              `json:"view"`
	Rank         int                 `json:"rank"`
	BusinessDate string              `json:"business_date"`
	FactType     domain.DocumentType `json:"fact_type"`
	FactID       string              `json:"fact_id"`
}

func (s Service) AttributionCandidates(
	ctx context.Context,
	tenant domain.TenantContext,
	tripID, view, cursor string,
	limit int,
) (AttributionPage, error) {
	if err := tenant.Require(domain.CapabilityFactsRead); err != nil {
		return AttributionPage{}, err
	}
	if tripID == "" || !domain.ValidTripAttributionView(view) || limit < 1 || limit > 100 {
		return AttributionPage{}, domain.ErrInvalidInput
	}
	after, err := decodeCursor(cursor, tripID, view)
	if err != nil {
		return AttributionPage{}, err
	}
	page, err := s.repository.ListTripAttributionCandidates(ctx, tenant.TenantID, ports.TripAttributionQuery{
		TripID: tripID,
		View:   view,
		Limit:  limit,
		After:  after,
	})
	if err != nil {
		return AttributionPage{}, err
	}
	result := AttributionPage{
		Trip:        page.Trip,
		RuleVersion: domain.TripAttributionRuleVersion,
		Items:       page.Items,
	}
	if result.Items == nil {
		result.Items = []ports.TripAttributionCandidate{}
	}
	if page.NextCursor != nil {
		result.NextCursor, err = encodeCursor(tripID, view, *page.NextCursor)
		if err != nil {
			return AttributionPage{}, err
		}
	}
	return result, nil
}

func (s Service) Assign(
	ctx context.Context,
	tenant domain.TenantContext,
	input AssignmentInput,
) (ports.TripAssignmentResult, error) {
	if err := tenant.Require(domain.CapabilityTripAssignmentsManage); err != nil {
		return ports.TripAssignmentResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ports.TripAssignmentResult{}, err
	}
	if input.RequestID == "" || !domain.ValidTripAssignmentFactType(input.FactType) || input.FactID == "" {
		return ports.TripAssignmentResult{}, domain.ErrInvalidInput
	}
	reason := strings.TrimSpace(input.Reason)
	if len([]rune(reason)) < 1 || len([]rune(reason)) > 500 {
		return ports.TripAssignmentResult{}, domain.NewRuleError("invalid_trip_assignment_reason", "归属理由必须为 1 至 500 个字符", domain.ErrInvalidInput)
	}
	desiredTripID := stringValue(input.DesiredTripID)
	expectedAssignmentID := stringValue(input.ExpectedAssignmentID)
	if (input.DesiredTripID != nil && desiredTripID == "") ||
		(input.ExpectedAssignmentID != nil && expectedAssignmentID == "") {
		return ports.TripAssignmentResult{}, domain.ErrInvalidInput
	}
	requestHash, err := assignmentRequestHash(input.FactType, input.FactID, desiredTripID, expectedAssignmentID, reason)
	if err != nil {
		return ports.TripAssignmentResult{}, err
	}
	if replay, replayErr := s.repository.GetTripAssignmentReplay(ctx, tenant.TenantID, input.IdempotencyKey); replayErr == nil {
		if replay.RequestHash != requestHash {
			return ports.TripAssignmentResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的行程归属请求", domain.ErrConflict)
		}
		return replay.Result, nil
	} else if !errors.Is(replayErr, domain.ErrNotFound) {
		return ports.TripAssignmentResult{}, replayErr
	}
	decisionID, err := s.ids.NewID()
	if err != nil {
		return ports.TripAssignmentResult{}, err
	}
	assignmentID, err := s.ids.NewID()
	if err != nil {
		return ports.TripAssignmentResult{}, err
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return ports.TripAssignmentResult{}, err
	}
	command := ports.TripAssignmentCommand{
		TenantID:             tenant.TenantID,
		ActorUserID:          tenant.UserID,
		FactType:             input.FactType,
		FactID:               input.FactID,
		DesiredTripID:        desiredTripID,
		ExpectedAssignmentID: expectedAssignmentID,
		Reason:               reason,
		IdempotencyKey:       input.IdempotencyKey,
		RequestHash:          requestHash,
		DecisionID:           decisionID,
		AssignmentID:         assignmentID,
		AuditEventID:         auditID,
		RequestID:            input.RequestID,
		CreatedAt:            s.clock.Now(),
	}
	var result ports.TripAssignmentResult
	err = s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		var operationErr error
		result, operationErr = transaction.ApplyTripAssignment(ctx, command)
		return operationErr
	})
	if err == nil {
		return result, nil
	}
	if replay, replayErr := s.repository.GetTripAssignmentReplay(ctx, tenant.TenantID, input.IdempotencyKey); replayErr == nil {
		if replay.RequestHash == requestHash {
			return replay.Result, nil
		}
		return ports.TripAssignmentResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的行程归属请求", domain.ErrConflict)
	}
	return ports.TripAssignmentResult{}, err
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func assignmentRequestHash(
	factType domain.DocumentType,
	factID, desiredTripID, expectedAssignmentID, reason string,
) (string, error) {
	encoded, err := json.Marshal(struct {
		Version              string              `json:"version"`
		FactType             domain.DocumentType `json:"fact_type"`
		FactID               string              `json:"fact_id"`
		DesiredTripID        string              `json:"desired_trip_id"`
		ExpectedAssignmentID string              `json:"expected_assignment_id"`
		Reason               string              `json:"reason"`
	}{
		Version:              domain.TripAttributionRuleVersion,
		FactType:             factType,
		FactID:               factID,
		DesiredTripID:        desiredTripID,
		ExpectedAssignmentID: expectedAssignmentID,
		Reason:               reason,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func decodeCursor(raw, tripID, view string) (*ports.TripAttributionCursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, domain.NewRuleError("invalid_cursor", "行程归属游标无效", domain.ErrInvalidInput)
	}
	var envelope cursorEnvelope
	if err := json.Unmarshal(decoded, &envelope); err != nil ||
		envelope.Version != tripAttributionCursorVersion || envelope.TripID != tripID || envelope.View != view ||
		envelope.Rank < 0 || envelope.Rank > 2 || !domain.ValidTripAssignmentFactType(envelope.FactType) || envelope.FactID == "" {
		return nil, domain.NewRuleError("invalid_cursor", "行程归属游标无效", domain.ErrInvalidInput)
	}
	if _, err := time.Parse("2006-01-02", envelope.BusinessDate); err != nil {
		return nil, domain.NewRuleError("invalid_cursor", "行程归属游标无效", domain.ErrInvalidInput)
	}
	return &ports.TripAttributionCursor{
		Rank: envelope.Rank, BusinessDate: envelope.BusinessDate,
		FactType: envelope.FactType, FactID: envelope.FactID,
	}, nil
}

func encodeCursor(tripID, view string, cursor ports.TripAttributionCursor) (string, error) {
	encoded, err := json.Marshal(cursorEnvelope{
		Version: tripAttributionCursorVersion, TripID: tripID, View: view,
		Rank: cursor.Rank, BusinessDate: cursor.BusinessDate,
		FactType: cursor.FactType, FactID: cursor.FactID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}
