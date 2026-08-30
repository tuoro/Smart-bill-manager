package reviews

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const (
	AssociationAllocateCandidates = "allocate_candidates"
	AssociationRejectAll          = "reject_all"
	AssociationNoCandidate        = "no_candidate"
)

type ConfirmInput struct {
	ExpectedRevision     int
	AssociationMode      string
	Allocations          []domain.AllocationRequest
	DuplicateResolutions []domain.DuplicateResolution
	IdempotencyKey       string
	RequestID            string
}

type RejectInput struct {
	ExpectedRevision int
	Reason           string
	IdempotencyKey   string
	RequestID        string
}

func (s Service) Confirm(
	ctx context.Context,
	tenant domain.TenantContext,
	jobID string,
	input ConfirmInput,
) (ports.ConfirmResult, error) {
	if err := tenant.Require(domain.CapabilityClaimsReview); err != nil {
		return ports.ConfirmResult{}, err
	}
	if err := validateDecisionInput(input.IdempotencyKey, input.RequestID); err != nil {
		return ports.ConfirmResult{}, err
	}
	canonicalPlan, planHash, err := normalizeAssociationRequest(input.AssociationMode, input.Allocations)
	if err != nil {
		return ports.ConfirmResult{}, err
	}
	input.Allocations = canonicalPlan
	duplicatePlan, duplicatePlanHash, err := domain.CanonicalDuplicatePlan(input.DuplicateResolutions)
	if err != nil {
		return ports.ConfirmResult{}, err
	}
	input.DuplicateResolutions = duplicatePlan
	replay, err := s.reviews.GetConfirmReplay(ctx, tenant.TenantID, jobID, input.IdempotencyKey)
	if err == nil {
		if replay.ExpectedRevision != input.ExpectedRevision ||
			replay.AssociationMode != input.AssociationMode ||
			replay.AllocationPlanHash != planHash ||
			replay.DuplicatePlanHash != duplicatePlanHash {
			return ports.ConfirmResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的确认请求", domain.ErrConflict)
		}
		return replay.Result, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return ports.ConfirmResult{}, err
	}
	current, err := s.reviews.GetReview(ctx, tenant.TenantID, jobID)
	if err != nil {
		return ports.ConfirmResult{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return ports.ConfirmResult{}, domain.ErrVersionConflict
	}
	if !current.Status.CanConfirm() {
		return ports.ConfirmResult{}, domain.NewRuleError("claim_not_confirmable", "当前 Claim 仍被校验阻断", domain.ErrConflict)
	}
	factAmount, factCurrency, err := factAllocationTerms(current)
	if err != nil {
		return ports.ConfirmResult{}, err
	}
	if err := validateAssociation(current.Candidates, input.AssociationMode, input.Allocations, factAmount, factCurrency); err != nil {
		return ports.ConfirmResult{}, err
	}
	duplicateCandidateIDs := make([]string, 0, len(current.DuplicateCandidates))
	for _, candidate := range current.DuplicateCandidates {
		duplicateCandidateIDs = append(duplicateCandidateIDs, candidate.ID)
	}
	if err := domain.ValidateDuplicatePlan(duplicateCandidateIDs, input.DuplicateResolutions); err != nil {
		return ports.ConfirmResult{}, err
	}
	command, err := s.buildConfirmCommand(tenant, jobID, current, input, planHash, duplicatePlanHash)
	if err != nil {
		return ports.ConfirmResult{}, err
	}
	var result ports.ConfirmResult
	err = s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		var operationErr error
		result, operationErr = transaction.ConfirmReview(ctx, command)
		return operationErr
	})
	if err == nil {
		return result, nil
	}
	replay, replayErr := s.reviews.GetConfirmReplay(ctx, tenant.TenantID, jobID, input.IdempotencyKey)
	if replayErr == nil && replay.ExpectedRevision == input.ExpectedRevision &&
		replay.AssociationMode == input.AssociationMode &&
		replay.AllocationPlanHash == planHash &&
		replay.DuplicatePlanHash == duplicatePlanHash {
		return replay.Result, nil
	}
	return ports.ConfirmResult{}, err
}

func (s Service) Reject(
	ctx context.Context,
	tenant domain.TenantContext,
	jobID string,
	input RejectInput,
) error {
	if err := tenant.Require(domain.CapabilityClaimsReview); err != nil {
		return err
	}
	if err := validateDecisionInput(input.IdempotencyKey, input.RequestID); err != nil {
		return err
	}
	if len([]rune(input.Reason)) > 500 {
		return domain.NewRuleError("reason_too_long", "驳回原因不能超过 500 个字符", domain.ErrInvalidInput)
	}
	replay, err := s.reviews.GetRejectReplay(ctx, tenant.TenantID, jobID, input.IdempotencyKey)
	if err == nil {
		if replay.ExpectedRevision != input.ExpectedRevision || replay.Reason != input.Reason {
			return domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的驳回请求", domain.ErrConflict)
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	current, err := s.reviews.GetReview(ctx, tenant.TenantID, jobID)
	if err != nil {
		return err
	}
	if current.Revision != input.ExpectedRevision {
		return domain.ErrVersionConflict
	}
	decisionID, err := s.ids.NewID()
	if err != nil {
		return err
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return err
	}
	command := ports.RejectCommand{
		TenantID:         tenant.TenantID,
		JobID:            jobID,
		ClaimSetID:       current.ClaimSetID,
		ActorUserID:      tenant.UserID,
		ReviewDecisionID: decisionID,
		IdempotencyKey:   input.IdempotencyKey,
		ExpectedRevision: input.ExpectedRevision,
		Reason:           input.Reason,
		AuditEventID:     auditID,
		RequestID:        input.RequestID,
		CreatedAt:        s.clock.Now(),
	}
	if err := s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.RejectReview(ctx, command)
	}); err == nil {
		return nil
	} else {
		replay, replayErr := s.reviews.GetRejectReplay(ctx, tenant.TenantID, jobID, input.IdempotencyKey)
		if replayErr == nil && replay.ExpectedRevision == input.ExpectedRevision && replay.Reason == input.Reason {
			return nil
		}
		return err
	}
}

func validateDecisionInput(idempotencyKey, requestID string) error {
	if err := domain.ValidateIdempotencyKey(idempotencyKey); err != nil {
		return err
	}
	if requestID == "" {
		return errors.New("request id is required")
	}
	return nil
}

func normalizeAssociationRequest(
	mode string,
	allocations []domain.AllocationRequest,
) ([]domain.AllocationRequest, string, error) {
	switch mode {
	case AssociationAllocateCandidates:
		if len(allocations) == 0 {
			return nil, "", domain.NewRuleError("association_decision_required", "分配模式必须至少选择一个候选", domain.ErrInvalidInput)
		}
		return domain.CanonicalAllocationPlan(allocations)
	case AssociationRejectAll, AssociationNoCandidate:
		if len(allocations) != 0 {
			return nil, "", domain.NewRuleError("invalid_association", "不分配模式不能包含分配计划", domain.ErrInvalidInput)
		}
		return []domain.AllocationRequest{}, "", nil
	default:
		return nil, "", domain.NewRuleError("invalid_association", "关联处理模式不受支持", domain.ErrInvalidInput)
	}
}

func validateAssociation(
	candidates []ports.LinkCandidate,
	mode string,
	allocations []domain.AllocationRequest,
	factAmount int64,
	factCurrency string,
) error {
	if len(candidates) == 0 {
		if mode != AssociationNoCandidate || len(allocations) != 0 {
			return domain.NewRuleError("invalid_association", "没有候选时必须明确选择 no_candidate", domain.ErrConflict)
		}
		return nil
	}
	if mode == AssociationRejectAll && len(allocations) == 0 {
		return nil
	}
	if mode != AssociationAllocateCandidates || len(allocations) == 0 {
		return domain.NewRuleError("association_decision_required", "存在候选时必须分配至少一个候选或明确拒绝全部", domain.ErrConflict)
	}
	allocationCandidates := make([]domain.AllocationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		allocationCandidates = append(allocationCandidates, domain.AllocationCandidate{
			ID:             candidate.ID,
			Currency:       candidate.Currency,
			RemainingMinor: candidate.RemainingMinor,
			Available:      candidate.Available,
		})
	}
	return domain.ValidateAllocationPlan(factAmount, factCurrency, allocationCandidates, allocations)
}

func factAllocationTerms(snapshot ports.ReviewSnapshot) (int64, string, error) {
	fields := presentFields(snapshot.Fields)
	currency, err := fieldString(fields, "currency")
	if err != nil {
		return 0, "", err
	}
	if snapshot.DocumentType == domain.DocumentPayment {
		amount, err := fieldInt(fields, "amount_minor")
		return amount, currency, err
	}
	if snapshot.DocumentType == domain.DocumentInvoice {
		amount, err := fieldInt(fields, "total_minor")
		return amount, currency, err
	}
	return 0, "", domain.NewRuleError("unknown_document_type", "unknown Claim 不能创建 Fact", domain.ErrConflict)
}

func (s Service) buildConfirmCommand(
	tenant domain.TenantContext,
	jobID string,
	current ports.ReviewSnapshot,
	input ConfirmInput,
	planHash, duplicatePlanHash string,
) (ports.ConfirmCommand, error) {
	decisionID, err := s.ids.NewID()
	if err != nil {
		return ports.ConfirmCommand{}, err
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return ports.ConfirmCommand{}, err
	}
	command := ports.ConfirmCommand{
		TenantID:           tenant.TenantID,
		JobID:              jobID,
		ClaimSetID:         current.ClaimSetID,
		ActorUserID:        tenant.UserID,
		ReviewDecisionID:   decisionID,
		IdempotencyKey:     input.IdempotencyKey,
		ExpectedRevision:   input.ExpectedRevision,
		AssociationMode:    input.AssociationMode,
		AllocationPlanHash: planHash,
		DuplicatePlanHash:  duplicatePlanHash,
		AuditEventID:       auditID,
		RequestID:          input.RequestID,
		CreatedAt:          s.clock.Now(),
	}
	if current.DocumentType == domain.DocumentPayment {
		command.Payment, command.Origins, err = s.buildPaymentDraft(current)
	} else if current.DocumentType == domain.DocumentInvoice {
		command.Invoice, command.Origins, err = s.buildInvoiceDraft(current)
	} else {
		return ports.ConfirmCommand{}, domain.NewRuleError("unknown_document_type", "unknown Claim 不能创建 Fact", domain.ErrConflict)
	}
	if err != nil {
		return ports.ConfirmCommand{}, err
	}
	allocationsByCandidate := make(map[string]int64, len(input.Allocations))
	for _, allocation := range input.Allocations {
		allocationsByCandidate[allocation.CandidateID] = allocation.AllocatedMinor
	}
	candidates := append([]ports.LinkCandidate(nil), current.Candidates...)
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].ID < candidates[right].ID })
	for _, candidate := range candidates {
		candidateDecisionID, err := s.ids.NewID()
		if err != nil {
			return ports.ConfirmCommand{}, err
		}
		action := "reject"
		allocatedMinor, accepted := allocationsByCandidate[candidate.ID]
		decision := ports.CandidateDecisionDraft{
			ID:          candidateDecisionID,
			CandidateID: candidate.ID,
			Action:      action,
		}
		if accepted {
			action = "accept"
			decision.Action = action
			decision.AllocatedMinor = &allocatedMinor
			decision.Currency = candidate.Currency
			decision.LinkID, err = s.ids.NewID()
			if err != nil {
				return ports.ConfirmCommand{}, err
			}
		}
		command.CandidateDecisions = append(command.CandidateDecisions, decision)
	}
	for _, resolution := range input.DuplicateResolutions {
		decisionID, err := s.ids.NewID()
		if err != nil {
			return ports.ConfirmCommand{}, err
		}
		command.DuplicateDecisions = append(command.DuplicateDecisions, ports.DuplicateCandidateDecisionDraft{
			ID:          decisionID,
			CandidateID: resolution.CandidateID,
			Action:      resolution.Action,
		})
	}
	return command, nil
}

func (s Service) buildPaymentDraft(snapshot ports.ReviewSnapshot) (*ports.PaymentDraft, []ports.FactOriginDraft, error) {
	fields := presentFields(snapshot.Fields)
	factID, err := s.ids.NewID()
	if err != nil {
		return nil, nil, err
	}
	draft := &ports.PaymentDraft{ID: factID}
	if draft.AmountMinor, err = fieldInt(fields, "amount_minor"); err != nil {
		return nil, nil, err
	}
	if draft.Currency, err = fieldString(fields, "currency"); err != nil {
		return nil, nil, err
	}
	if draft.Merchant, err = fieldString(fields, "merchant"); err != nil {
		return nil, nil, err
	}
	if draft.TransactionTime, err = fieldString(fields, "transaction_time"); err != nil {
		return nil, nil, err
	}
	if draft.SourceTimezone, err = fieldString(fields, "source_timezone"); err != nil {
		return nil, nil, err
	}
	draft.PaymentMethod = optionalFieldString(fields, "payment_method")
	draft.OrderNumber = optionalFieldString(fields, "order_number")
	draft.Category = optionalFieldString(fields, "category")
	origins, err := s.factOrigins(snapshot.Fields, "payment", nil)
	return draft, origins, err
}

func (s Service) buildInvoiceDraft(snapshot ports.ReviewSnapshot) (*ports.InvoiceDraft, []ports.FactOriginDraft, error) {
	fields := presentFields(snapshot.Fields)
	factID, err := s.ids.NewID()
	if err != nil {
		return nil, nil, err
	}
	draft := &ports.InvoiceDraft{ID: factID}
	if draft.InvoiceNumber, err = fieldString(fields, "invoice_number"); err != nil {
		return nil, nil, err
	}
	draft.NormalizedInvoiceNumber = domain.NormalizeExact(draft.InvoiceNumber)
	if draft.InvoiceDate, err = fieldString(fields, "invoice_date"); err != nil {
		return nil, nil, err
	}
	if draft.TotalMinor, err = fieldInt(fields, "total_minor"); err != nil {
		return nil, nil, err
	}
	draft.TaxMinor = optionalFieldInt(fields, "tax_minor")
	if draft.Currency, err = fieldString(fields, "currency"); err != nil {
		return nil, nil, err
	}
	if draft.SellerName, err = fieldString(fields, "seller_name"); err != nil {
		return nil, nil, err
	}
	if draft.BuyerName, err = fieldString(fields, "buyer_name"); err != nil {
		return nil, nil, err
	}
	itemFields := make(map[string]map[string]ports.ReviewField)
	for path, field := range fields {
		itemKey, property, ok := splitItemPath(path)
		if !ok {
			continue
		}
		if itemFields[itemKey] == nil {
			itemFields[itemKey] = make(map[string]ports.ReviewField)
		}
		itemFields[itemKey][property] = field
	}
	itemIDs := make(map[string]string, len(itemFields))
	for itemKey, values := range itemFields {
		itemID, err := s.ids.NewID()
		if err != nil {
			return nil, nil, err
		}
		itemIDs[itemKey] = itemID
		item := ports.InvoiceItemDraft{ID: itemID, ItemKey: itemKey}
		if item.Name, err = fieldString(values, "name"); err != nil {
			return nil, nil, err
		}
		item.Quantity = optionalFieldString(values, "quantity")
		item.Unit = optionalFieldString(values, "unit")
		item.UnitPriceMinor = optionalFieldInt(values, "unit_price_minor")
		if item.AmountMinor, err = fieldInt(values, "amount_minor"); err != nil {
			return nil, nil, err
		}
		item.TaxMinor = optionalFieldInt(values, "tax_minor")
		sortOrder, err := fieldInt(values, "sort_order")
		if err != nil || sortOrder > int64(^uint(0)>>1) {
			return nil, nil, errors.New("validated invoice item sort_order is invalid")
		}
		item.SortOrder = int(sortOrder)
		draft.Items = append(draft.Items, item)
	}
	sort.Slice(draft.Items, func(left, right int) bool {
		if draft.Items[left].SortOrder != draft.Items[right].SortOrder {
			return draft.Items[left].SortOrder < draft.Items[right].SortOrder
		}
		return draft.Items[left].ItemKey < draft.Items[right].ItemKey
	})
	origins, err := s.factOrigins(snapshot.Fields, "invoice", itemIDs)
	return draft, origins, err
}

func (s Service) factOrigins(
	fields []ports.ReviewField,
	factScope string,
	itemIDs map[string]string,
) ([]ports.FactOriginDraft, error) {
	result := make([]ports.FactOriginDraft, 0, len(fields))
	for _, field := range fields {
		if field.Presence != "present" || field.Path == "document_type" || field.Path == "supplementary_fields" {
			continue
		}
		originID, err := s.ids.NewID()
		if err != nil {
			return nil, err
		}
		origin := ports.FactOriginDraft{
			ID:           originID,
			FieldPath:    field.Path,
			FieldClaimID: field.ID,
			FactScope:    factScope,
		}
		if itemKey, _, ok := splitItemPath(field.Path); ok {
			if itemIDs[itemKey] == "" {
				return nil, errors.New("invoice item origin has no item")
			}
			origin.FactScope = "item"
			origin.ItemKey = itemKey
		}
		result = append(result, origin)
	}
	return result, nil
}

func presentFields(fields []ports.ReviewField) map[string]ports.ReviewField {
	result := make(map[string]ports.ReviewField)
	for _, field := range fields {
		if field.Presence == "present" {
			result[field.Path] = field
		}
	}
	return result
}

func fieldString(fields map[string]ports.ReviewField, path string) (string, error) {
	field, exists := fields[path]
	if !exists {
		return "", fmt.Errorf("validated field %s is missing", path)
	}
	var value string
	if err := json.Unmarshal(field.Value, &value); err != nil {
		return "", fmt.Errorf("decode validated field %s: %w", path, err)
	}
	return value, nil
}

func optionalFieldString(fields map[string]ports.ReviewField, path string) *string {
	value, err := fieldString(fields, path)
	if err != nil {
		return nil
	}
	return &value
}

func fieldInt(fields map[string]ports.ReviewField, path string) (int64, error) {
	field, exists := fields[path]
	if !exists {
		return 0, fmt.Errorf("validated field %s is missing", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(field.Value))
	decoder.UseNumber()
	var value json.Number
	if err := decoder.Decode(&value); err != nil {
		return 0, fmt.Errorf("decode validated field %s: %w", path, err)
	}
	result, err := strconv.ParseInt(value.String(), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("decode validated integer %s: %w", path, err)
	}
	return result, nil
}

func optionalFieldInt(fields map[string]ports.ReviewField, path string) *int64 {
	value, err := fieldInt(fields, path)
	if err != nil {
		return nil
	}
	return &value
}

func splitItemPath(path string) (string, string, bool) {
	if !strings.HasPrefix(path, "items[") {
		return "", "", false
	}
	separator := strings.Index(path, "].")
	if separator < len("items[")+1 {
		return "", "", false
	}
	itemKey := path[len("items["):separator]
	property := path[separator+2:]
	expected, err := domain.StableItemPath(itemKey, property)
	return itemKey, property, err == nil && expected == path
}
