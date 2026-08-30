package reimbursements

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const reimbursementCursorVersion = "reimbursement-list-cursor/1"

type Service struct {
	repository ports.ReimbursementRepository
	tx         ports.TransactionManager
	ids        ports.IDGenerator
	clock      ports.Clock
}

type ListPage struct {
	Items      []ports.ReimbursementSummary `json:"items"`
	NextCursor string                       `json:"next_cursor,omitempty"`
}

type SubmissionInput struct {
	TripID                  string
	AssignmentIDs           []string
	ExpectedSnapshotHash    string
	AcknowledgedFindingKeys []string
	Reason                  string
	IdempotencyKey          string
	RequestID               string
}

type StatusInput struct {
	ExpectedStatus  domain.ReimbursementStatus
	DesiredStatus   domain.ReimbursementStatus
	ExpectedVersion int
	Reason          string
	IdempotencyKey  string
	RequestID       string
}

type cursorEnvelope struct {
	Version   string `json:"version"`
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

func NewService(
	repository ports.ReimbursementRepository,
	tx ports.TransactionManager,
	ids ports.IDGenerator,
	clock ports.Clock,
) Service {
	return Service{repository: repository, tx: tx, ids: ids, clock: clock}
}

func (s Service) Preview(
	ctx context.Context,
	tenant domain.TenantContext,
	tripID string,
	assignmentIDs []string,
) (domain.ReimbursementPolicySnapshot, error) {
	if err := tenant.Require(domain.CapabilityReimbursementsManage); err != nil {
		return domain.ReimbursementPolicySnapshot{}, err
	}
	selection, err := domain.CanonicalReimbursementSelection(assignmentIDs)
	if err != nil {
		return domain.ReimbursementPolicySnapshot{}, err
	}
	if tripID == "" || strings.TrimSpace(tripID) != tripID {
		return domain.ReimbursementPolicySnapshot{}, domain.NewRuleError("invalid_reimbursement_trip", "报销 Trip ID 不合法", domain.ErrInvalidInput)
	}
	return s.repository.BuildReimbursementPreview(ctx, tenant.TenantID, tripID, selection)
}

func (s Service) List(
	ctx context.Context,
	tenant domain.TenantContext,
	cursor string,
	limit int,
) (ListPage, error) {
	if err := tenant.Require(domain.CapabilityReimbursementsRead); err != nil {
		return ListPage{}, err
	}
	if limit < 1 || limit > 100 {
		return ListPage{}, domain.NewRuleError("invalid_reimbursement_limit", "报销分页数量必须为 1–100", domain.ErrInvalidInput)
	}
	after, err := decodeCursor(cursor)
	if err != nil {
		return ListPage{}, err
	}
	page, err := s.repository.ListReimbursements(ctx, tenant.TenantID, ports.ReimbursementListQuery{Limit: limit, After: after})
	if err != nil {
		return ListPage{}, err
	}
	result := ListPage{Items: page.Items}
	if result.Items == nil {
		result.Items = []ports.ReimbursementSummary{}
	}
	if page.NextCursor != nil {
		result.NextCursor, err = encodeCursor(*page.NextCursor)
		if err != nil {
			return ListPage{}, err
		}
	}
	return result, nil
}

func (s Service) Get(
	ctx context.Context,
	tenant domain.TenantContext,
	reimbursementID string,
) (ports.ReimbursementDetail, error) {
	if err := tenant.Require(domain.CapabilityReimbursementsRead); err != nil {
		return ports.ReimbursementDetail{}, err
	}
	if reimbursementID == "" || strings.TrimSpace(reimbursementID) != reimbursementID {
		return ports.ReimbursementDetail{}, domain.ErrInvalidInput
	}
	detail, err := s.repository.GetReimbursement(ctx, tenant.TenantID, reimbursementID)
	if err != nil {
		return ports.ReimbursementDetail{}, err
	}
	if detail.Items == nil {
		detail.Items = []ports.ReimbursementItem{}
	}
	if detail.Findings == nil {
		detail.Findings = []ports.ReimbursementFinding{}
	}
	if detail.Decisions == nil {
		detail.Decisions = []ports.ReimbursementDecision{}
	}
	if detail.Totals == nil {
		detail.Totals = []domain.ReimbursementCurrencyTotal{}
	}
	return detail, nil
}

func (s Service) Submit(
	ctx context.Context,
	tenant domain.TenantContext,
	input SubmissionInput,
) (ports.ReimbursementMutationResult, error) {
	if err := tenant.Require(domain.CapabilityReimbursementsManage); err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	if input.RequestID == "" {
		return ports.ReimbursementMutationResult{}, errors.New("request id is required")
	}
	selection, acknowledged, reason, requestHash, err := domain.CanonicalReimbursementSubmissionRequest(
		input.TripID,
		input.AssignmentIDs,
		input.ExpectedSnapshotHash,
		input.AcknowledgedFindingKeys,
		input.Reason,
	)
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	if replay, replayErr := s.repository.GetReimbursementDecisionReplay(ctx, tenant.TenantID, input.IdempotencyKey); replayErr == nil {
		if replay.RequestHash != requestHash {
			return ports.ReimbursementMutationResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的报销请求", domain.ErrConflict)
		}
		return replay.Result, nil
	} else if !errors.Is(replayErr, domain.ErrNotFound) {
		return ports.ReimbursementMutationResult{}, replayErr
	}
	preview, err := s.repository.BuildReimbursementPreview(ctx, tenant.TenantID, input.TripID, selection)
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	if preview.SnapshotHash != input.ExpectedSnapshotHash {
		return ports.ReimbursementMutationResult{}, domain.NewRuleError("reimbursement_snapshot_stale", "报销预检输入已变化，请刷新后重新确认", domain.ErrConflict)
	}
	if !slices.Equal(acknowledged, sortedFindingKeys(preview.Findings)) {
		return ports.ReimbursementMutationResult{}, domain.NewRuleError("reimbursement_findings_unacknowledged", "必须确认当前完整报销提示", domain.ErrConflict)
	}
	command, err := s.newSubmissionCommand(tenant, input, selection, acknowledged, reason, requestHash, preview)
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	var result ports.ReimbursementMutationResult
	err = s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		var operationErr error
		result, operationErr = transaction.SubmitReimbursement(ctx, command)
		return operationErr
	})
	if err == nil {
		return result, nil
	}
	if replay, replayErr := s.repository.GetReimbursementDecisionReplay(ctx, tenant.TenantID, input.IdempotencyKey); replayErr == nil {
		if replay.RequestHash == requestHash {
			return replay.Result, nil
		}
		return ports.ReimbursementMutationResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的报销请求", domain.ErrConflict)
	}
	return ports.ReimbursementMutationResult{}, err
}

func (s Service) ChangeStatus(
	ctx context.Context,
	tenant domain.TenantContext,
	reimbursementID string,
	input StatusInput,
) (ports.ReimbursementMutationResult, error) {
	if err := tenant.Require(domain.CapabilityReimbursementsManage); err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	if input.RequestID == "" {
		return ports.ReimbursementMutationResult{}, errors.New("request id is required")
	}
	action, reason, requestHash, err := domain.CanonicalReimbursementStatusRequest(
		reimbursementID,
		input.ExpectedStatus,
		input.DesiredStatus,
		input.ExpectedVersion,
		input.Reason,
	)
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	if replay, replayErr := s.repository.GetReimbursementDecisionReplay(ctx, tenant.TenantID, input.IdempotencyKey); replayErr == nil {
		if replay.RequestHash != requestHash {
			return ports.ReimbursementMutationResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的报销状态请求", domain.ErrConflict)
		}
		return replay.Result, nil
	} else if !errors.Is(replayErr, domain.ErrNotFound) {
		return ports.ReimbursementMutationResult{}, replayErr
	}
	decisionID, err := s.ids.NewID()
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	auditEventID, err := s.ids.NewID()
	if err != nil {
		return ports.ReimbursementMutationResult{}, err
	}
	command := ports.ReimbursementStatusCommand{
		TenantID: tenant.TenantID, ActorUserID: tenant.UserID,
		ReimbursementID: reimbursementID,
		ExpectedStatus:  input.ExpectedStatus, DesiredStatus: input.DesiredStatus,
		ExpectedVersion: input.ExpectedVersion, Action: action, Reason: reason,
		IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash,
		DecisionID: decisionID, AuditEventID: auditEventID,
		RequestID: input.RequestID, CreatedAt: s.clock.Now(),
	}
	var result ports.ReimbursementMutationResult
	err = s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		var operationErr error
		result, operationErr = transaction.ApplyReimbursementStatus(ctx, command)
		return operationErr
	})
	if err == nil {
		return result, nil
	}
	if replay, replayErr := s.repository.GetReimbursementDecisionReplay(ctx, tenant.TenantID, input.IdempotencyKey); replayErr == nil {
		if replay.RequestHash == requestHash {
			return replay.Result, nil
		}
		return ports.ReimbursementMutationResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的报销状态请求", domain.ErrConflict)
	}
	return ports.ReimbursementMutationResult{}, err
}

func (s Service) newSubmissionCommand(
	tenant domain.TenantContext,
	input SubmissionInput,
	selection, acknowledged []string,
	reason, requestHash string,
	preview domain.ReimbursementPolicySnapshot,
) (ports.ReimbursementSubmissionCommand, error) {
	reimbursementID, err := s.ids.NewID()
	if err != nil {
		return ports.ReimbursementSubmissionCommand{}, err
	}
	decisionID, err := s.ids.NewID()
	if err != nil {
		return ports.ReimbursementSubmissionCommand{}, err
	}
	auditEventID, err := s.ids.NewID()
	if err != nil {
		return ports.ReimbursementSubmissionCommand{}, err
	}
	itemDrafts := make([]ports.ReimbursementItemDraft, 0, len(preview.Items))
	for _, item := range preview.Items {
		id, err := s.ids.NewID()
		if err != nil {
			return ports.ReimbursementSubmissionCommand{}, err
		}
		itemDrafts = append(itemDrafts, ports.ReimbursementItemDraft{AssignmentID: item.AssignmentID, ID: id})
	}
	findingDrafts := make([]ports.ReimbursementFindingDraft, 0, len(preview.Findings))
	for _, finding := range preview.Findings {
		id, err := s.ids.NewID()
		if err != nil {
			return ports.ReimbursementSubmissionCommand{}, err
		}
		findingDrafts = append(findingDrafts, ports.ReimbursementFindingDraft{FindingKey: finding.FindingKey, ID: id})
	}
	return ports.ReimbursementSubmissionCommand{
		TenantID: tenant.TenantID, ActorUserID: tenant.UserID,
		TripID: input.TripID, AssignmentIDs: selection,
		ExpectedSnapshotHash:    input.ExpectedSnapshotHash,
		AcknowledgedFindingKeys: acknowledged,
		Reason:                  reason, IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash,
		ReimbursementID: reimbursementID, DecisionID: decisionID, AuditEventID: auditEventID,
		ItemDrafts: itemDrafts, FindingDrafts: findingDrafts,
		RequestID: input.RequestID, CreatedAt: s.clock.Now(),
	}, nil
}

func sortedFindingKeys(findings []domain.ReimbursementPolicyFinding) []string {
	result := make([]string, 0, len(findings))
	for _, finding := range findings {
		result = append(result, finding.FindingKey)
	}
	sort.Strings(result)
	return result
}

func decodeCursor(raw string) (*ports.ReimbursementCursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, domain.NewRuleError("invalid_cursor", "报销列表游标无效", domain.ErrInvalidInput)
	}
	var envelope cursorEnvelope
	if err := json.Unmarshal(decoded, &envelope); err != nil ||
		envelope.Version != reimbursementCursorVersion || envelope.ID == "" {
		return nil, domain.NewRuleError("invalid_cursor", "报销列表游标无效", domain.ErrInvalidInput)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
	if err != nil {
		return nil, domain.NewRuleError("invalid_cursor", "报销列表游标无效", domain.ErrInvalidInput)
	}
	return &ports.ReimbursementCursor{CreatedAt: createdAt, ID: envelope.ID}, nil
}

func encodeCursor(cursor ports.ReimbursementCursor) (string, error) {
	encoded, err := json.Marshal(cursorEnvelope{
		Version:   reimbursementCursorVersion,
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:        cursor.ID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}
