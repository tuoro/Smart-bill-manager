package reviews

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/claimsupport"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type CorrectionInput struct {
	ExpectedVersion         int                  `json:"expected_version"`
	CurrentReviewDecisionID string               `json:"current_review_decision_id"`
	Fields                  []RevisionFieldInput `json:"fields"`
	Reason                  string               `json:"reason"`
	WithdrawLinkIDs         []string             `json:"withdraw_link_ids"`
}

type CorrectionConfirmInput struct {
	CorrectionInput
	PreviewHash               string   `json:"preview_hash"`
	AcknowledgedDuplicateKeys []string `json:"acknowledged_duplicate_keys"`
	IdempotencyKey            string   `json:"-"`
	RequestID                 string   `json:"-"`
}

type CorrectionWorkspace struct {
	State  ports.FactCorrectionState
	Review ports.ReviewSnapshot
}

type CorrectionDuplicate struct {
	TargetRevisionID   string   `json:"target_revision_id"`
	Key                string   `json:"key"`
	Kind               string   `json:"kind"`
	ExistingDocumentID string   `json:"existing_document_id"`
	ExistingPaymentID  string   `json:"existing_payment_id"`
	ExistingInvoiceID  string   `json:"existing_invoice_id"`
	ReasonCodes        []string `json:"reason_codes"`
}

type CorrectionPreview struct {
	State           ports.FactCorrectionState `json:"state"`
	Issues          []domain.CorrectionIssue  `json:"issues"`
	Duplicates      []CorrectionDuplicate     `json:"duplicates"`
	WithdrawLinkIDs []string                  `json:"withdraw_link_ids"`
	PreviewHash     string                    `json:"preview_hash"`
	CanConfirm      bool                      `json:"can_confirm"`
}

func requireCorrectionAccess(tenant domain.TenantContext) error {
	if err := tenant.Require(domain.CapabilityFactsRead); err != nil {
		return err
	}
	return tenant.Require(domain.CapabilityClaimsReview)
}

func (s Service) GetCorrection(ctx context.Context, tenant domain.TenantContext, kind domain.DocumentType, id string) (CorrectionWorkspace, error) {
	if err := requireCorrectionAccess(tenant); err != nil {
		return CorrectionWorkspace{}, err
	}
	state, err := s.reviews.GetFactCorrectionState(ctx, tenant.TenantID, kind, id, "")
	if err != nil {
		return CorrectionWorkspace{}, err
	}
	claim, err := s.GetClaimSet(ctx, tenant, state.ClaimSetID)
	if err != nil {
		return CorrectionWorkspace{}, err
	}
	return CorrectionWorkspace{State: state, Review: claim}, nil
}

func (s Service) CorrectionHistory(ctx context.Context, tenant domain.TenantContext, kind domain.DocumentType, id string, before, limit int) ([]ports.FactCorrectionHistory, error) {
	if err := requireCorrectionAccess(tenant); err != nil {
		return nil, err
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, domain.ErrInvalidInput
	}
	return s.reviews.GetFactCorrectionHistory(ctx, tenant.TenantID, kind, id, before, limit)
}

func canonicalCorrectionInput(input CorrectionInput) (CorrectionInput, error) {
	if input.ExpectedVersion < 1 || input.CurrentReviewDecisionID == "" || len(input.Fields) == 0 {
		return input, domain.ErrInvalidInput
	}
	var err error
	input.Reason, err = domain.CorrectionReason(input.Reason)
	if err != nil {
		return input, err
	}
	input.Fields = slices.Clone(input.Fields)
	slices.SortFunc(input.Fields, func(a, b RevisionFieldInput) int {
		if a.Path < b.Path {
			return -1
		}
		if a.Path > b.Path {
			return 1
		}
		return 0
	})
	for index := range input.Fields {
		field := &input.Fields[index]
		if index > 0 && input.Fields[index-1].Path == field.Path {
			return input, domain.ErrInvalidInput
		}
		field.Value, err = canonicalJSON(field.Value)
		if err != nil {
			return input, domain.ErrInvalidInput
		}
		field.EvidenceIDs = append([]string{}, field.EvidenceIDs...)
		slices.Sort(field.EvidenceIDs)
		field.ManualEvidence = append([]domain.ManualEvidenceInput{}, field.ManualEvidence...)
	}
	input.WithdrawLinkIDs = append([]string{}, input.WithdrawLinkIDs...)
	slices.Sort(input.WithdrawLinkIDs)
	return input, nil
}

func correctionHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func (s Service) PreviewCorrection(ctx context.Context, tenant domain.TenantContext, kind domain.DocumentType, id string, input CorrectionInput) (CorrectionPreview, error) {
	if err := requireCorrectionAccess(tenant); err != nil {
		return CorrectionPreview{}, err
	}
	input, err := canonicalCorrectionInput(input)
	if err != nil {
		return CorrectionPreview{}, err
	}
	workspace, err := s.GetCorrection(ctx, tenant, kind, id)
	if err != nil {
		return CorrectionPreview{}, err
	}
	var preview CorrectionPreview
	err = s.tx.WithinTransaction(ctx, func(tx ports.Transaction) error {
		var err error
		preview, _, _, _, err = s.prepareCorrection(ctx, tx, tenant, workspace, input)
		return err
	})
	return preview, err
}

// 预览与确认用同一事务读取/计算路径；随机新身份不进入预览 hash。
func (s Service) prepareCorrection(ctx context.Context, tx ports.Transaction, tenant domain.TenantContext, workspace CorrectionWorkspace, input CorrectionInput) (CorrectionPreview, domain.ValidatedClaim, map[string][]ports.ReviewEvidence, []domain.DuplicateCandidateSpec, error) {
	preview := CorrectionPreview{Issues: []domain.CorrectionIssue{}, Duplicates: []CorrectionDuplicate{}, WithdrawLinkIDs: input.WithdrawLinkIDs}
	current := workspace.Review
	candidates, selections, err := revisionCandidatesForPurpose(current, RevisionInput{DocumentType: workspace.State.FactType, Fields: input.Fields}, true)
	if err != nil {
		return preview, domain.ValidatedClaim{}, nil, nil, err
	}
	validated := domain.ValidateClaim(domain.ClaimEnvelope{SchemaVersion: "document-claim/3", DocumentType: string(workspace.State.FactType), Fields: candidates, DocumentIssues: []string{}}, current.PageCount)
	proposedTime := ""
	for _, field := range validated.Fields {
		if field.Path == "transaction_time" && field.Presence == "present" && validated.Status.CanConfirm() {
			proposedTime, err = candidateCorrectionString(field)
			if err != nil {
				return preview, validated, nil, nil, err
			}
		}
	}
	state, err := tx.GetFactCorrectionState(ctx, tenant.TenantID, workspace.State.FactType, workspace.State.FactID, proposedTime)
	if err != nil {
		return preview, validated, nil, nil, err
	}
	if state.Version != input.ExpectedVersion || state.CurrentReviewDecisionID != input.CurrentReviewDecisionID || state.ClaimSetID != current.ClaimSetID || current.Status != domain.ClaimConfirmed {
		return preview, validated, nil, nil, domain.ErrVersionConflict
	}
	preview.State = state
	if _, err := domain.CanonicalCorrectionWithdrawals(state.Links, input.WithdrawLinkIDs); err != nil {
		return preview, validated, nil, nil, err
	}
	for _, validation := range validated.Validations {
		if validation.Status == "blocked" || validation.Status == "error" {
			preview.Issues = append(preview.Issues, domain.CorrectionIssue{Code: validation.RuleCode, Message: validation.SafeMessage})
		}
	}
	if validated.Status.CanConfirm() {
		fields := make([]ports.ReviewField, 0, len(validated.Fields))
		for _, field := range validated.Fields {
			fields = append(fields, ports.ReviewField{Path: field.Path, ValueType: field.ValueType, Presence: field.Presence, Value: field.Value})
		}
		snapshot := ports.ReviewSnapshot{DocumentType: state.FactType, Fields: fields}
		var amount int64
		var currency, date string
		if state.FactType == domain.DocumentPayment {
			draft, _, err := s.buildPaymentDraft(snapshot)
			if err != nil {
				return preview, validated, nil, nil, err
			}
			amount, currency, date = draft.AmountMinor, draft.Currency, draft.BusinessDate
		} else if state.FactType == domain.DocumentInvoice {
			draft, _, err := s.buildInvoiceDraft(snapshot)
			if err != nil {
				return preview, validated, nil, nil, err
			}
			amount, currency, date = draft.TotalMinor, draft.Currency, draft.InvoiceDate
			conflict, err := tx.InvoiceNumberConflicts(ctx, tenant.TenantID, draft.NormalizedInvoiceNumber, state.FactID)
			if err != nil {
				return preview, validated, nil, nil, err
			}
			if conflict {
				preview.Issues = append(preview.Issues, domain.CorrectionIssue{Code: "invoice_number_conflict", Message: "同一工作区已存在相同规范化发票号码"})
			}
		}
		if state.FactType != domain.DocumentTrip {
			issues, err := domain.ValidateCorrectionLinks(domain.Money{MinorUnits: amount, Currency: domain.Currency(currency)}, date, state.Links, input.WithdrawLinkIDs)
			if err != nil {
				return preview, validated, nil, nil, err
			}
			preview.Issues = append(preview.Issues, issues...)
		}
	}
	specs, err := claimsupport.FindDuplicateCandidateSpecs(ctx, tx, validated, tenant.TenantID, state.DocumentID, state.FactID)
	if err != nil {
		return preview, validated, nil, nil, err
	}
	if len(specs) > domain.MaxDuplicateCandidates {
		preview.Issues = append(preview.Issues, domain.CorrectionIssue{Code: "duplicate_candidate_limit_exceeded", Message: "疑似重复超过 50 项，请先整理重复来源"})
	} else {
		for _, spec := range specs {
			targetRevision, err := tx.CorrectionDuplicateIdentity(ctx, tenant.TenantID, spec)
			if err != nil {
				return preview, validated, nil, nil, err
			}
			key, err := domain.DuplicateCandidateKey(tenant.TenantID, state.ClaimSetID, spec)
			if err != nil {
				return preview, validated, nil, nil, err
			}
			preview.Duplicates = append(preview.Duplicates, CorrectionDuplicate{TargetRevisionID: targetRevision, Key: key, Kind: spec.Kind, ExistingDocumentID: spec.ExistingDocumentID, ExistingPaymentID: spec.ExistingPaymentID, ExistingInvoiceID: spec.ExistingInvoiceID, ReasonCodes: spec.ReasonCodes})
		}
	}
	preview.CanConfirm = validated.Status.CanConfirm() && len(preview.Issues) == 0
	preview.PreviewHash, err = correctionHash(struct {
		Input   CorrectionInput
		Preview CorrectionPreview
	}{input, preview})
	return preview, validated, selections, specs, err
}

func candidateCorrectionString(field domain.FieldCandidate) (string, error) {
	var value string
	err := json.Unmarshal(field.Value, &value)
	return value, err
}

func (s Service) ConfirmCorrection(ctx context.Context, tenant domain.TenantContext, kind domain.DocumentType, id string, input CorrectionConfirmInput) (ports.FactCorrectionResult, error) {
	if err := requireCorrectionAccess(tenant); err != nil {
		return ports.FactCorrectionResult{}, err
	}
	if err := validateDecisionInput(input.IdempotencyKey, input.RequestID); err != nil {
		return ports.FactCorrectionResult{}, err
	}
	var err error
	input.CorrectionInput, err = canonicalCorrectionInput(input.CorrectionInput)
	if err != nil {
		return ports.FactCorrectionResult{}, err
	}
	input.AcknowledgedDuplicateKeys = append([]string{}, input.AcknowledgedDuplicateKeys...)
	slices.Sort(input.AcknowledgedDuplicateKeys)
	requestHash, err := correctionHash(struct {
		Kind      domain.DocumentType
		ID, Actor string
		Input     CorrectionConfirmInput
	}{kind, id, tenant.UserID, input})
	if err != nil {
		return ports.FactCorrectionResult{}, err
	}
	if result, err := s.correctionReplay(ctx, tenant.TenantID, input.IdempotencyKey, requestHash); !errors.Is(err, domain.ErrNotFound) {
		return result, err
	}
	workspace, err := s.GetCorrection(ctx, tenant, kind, id)
	if err != nil {
		return ports.FactCorrectionResult{}, err
	}
	var result ports.FactCorrectionResult
	err = s.tx.WithinTransaction(ctx, func(tx ports.Transaction) error {
		preview, validated, selections, specs, err := s.prepareCorrection(ctx, tx, tenant, workspace, input.CorrectionInput)
		if err != nil {
			return err
		}
		if preview.PreviewHash != input.PreviewHash {
			return domain.NewRuleError("stale_correction_preview", "字段或关联已变化，请刷新并重新预览", domain.ErrConflict)
		}
		if !preview.CanConfirm {
			return domain.NewRuleError("correction_blocked", "纠错仍有未解决的字段或分配冲突", domain.ErrConflict)
		}
		keys := make([]string, 0, len(preview.Duplicates))
		for _, duplicate := range preview.Duplicates {
			keys = append(keys, duplicate.Key)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, input.AcknowledgedDuplicateKeys) {
			return domain.NewRuleError("duplicate_confirmation_required", "请逐项确认当前全部重复提示", domain.ErrConflict)
		}
		command, err := s.buildCorrectionCommand(tenant, workspace, input, requestHash, preview, validated, selections, specs)
		if err != nil {
			return err
		}
		result, err = tx.ApplyFactCorrection(ctx, command)
		return err
	})
	if err != nil {
		if replay, replayErr := s.correctionReplay(ctx, tenant.TenantID, input.IdempotencyKey, requestHash); replayErr == nil || errors.Is(replayErr, domain.ErrConflict) {
			return replay, replayErr
		}
	}
	return result, err
}

func (s Service) correctionReplay(ctx context.Context, tenantID, key, requestHash string) (ports.FactCorrectionResult, error) {
	replay, err := s.reviews.GetFactCorrectionReplay(ctx, tenantID, key)
	if err != nil {
		return ports.FactCorrectionResult{}, err
	}
	if replay.RequestHash != requestHash {
		return ports.FactCorrectionResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的请求", domain.ErrConflict)
	}
	return replay.Result, nil
}

func (s Service) buildCorrectionCommand(tenant domain.TenantContext, workspace CorrectionWorkspace, input CorrectionConfirmInput, requestHash string, preview CorrectionPreview, validated domain.ValidatedClaim, selections map[string][]ports.ReviewEvidence, specs []domain.DuplicateCandidateSpec) (ports.FactCorrectionCommand, error) {
	revision, err := s.buildRevisionCommand(tenant, workspace.Review.Job.ID, workspace.Review, validated, selections)
	if err != nil {
		return ports.FactCorrectionCommand{}, err
	}
	revision.DuplicateCandidates, _, err = claimsupport.BuildDuplicateCandidateRecords(specs, tenant.TenantID, revision.ClaimSet.ID, s.ids, revision.ClaimSet.CreatedAt)
	if err != nil {
		return ports.FactCorrectionCommand{}, err
	}
	snapshot := ports.ReviewSnapshot{DocumentType: workspace.State.FactType, ClaimSetID: revision.ClaimSet.ID}
	for _, field := range revision.Fields {
		snapshot.Fields = append(snapshot.Fields, ports.ReviewField{ID: field.ID, Path: field.FieldPath, ValueType: field.ValueType, Presence: field.Presence, Value: json.RawMessage(field.TypedValueJSON)})
	}
	decisions := make([]domain.DuplicateResolution, 0, len(revision.DuplicateCandidates))
	for _, candidate := range revision.DuplicateCandidates {
		decisions = append(decisions, domain.DuplicateResolution{CandidateID: candidate.ID, Action: "keep_distinct"})
	}
	decisions, duplicateHash, err := domain.CanonicalDuplicatePlan(decisions)
	if err != nil {
		return ports.FactCorrectionCommand{}, err
	}
	mode := AssociationNoCandidate
	if workspace.State.FactType == domain.DocumentTrip {
		mode = ""
	}
	confirmation, err := s.buildConfirmCommand(tenant, workspace.Review.Job.ID, snapshot, ConfirmInput{ExpectedRevision: workspace.Review.Revision + 1, AssociationMode: mode, DuplicateResolutions: decisions, IdempotencyKey: input.IdempotencyKey, RequestID: input.RequestID}, "", duplicateHash)
	if err != nil {
		return ports.FactCorrectionCommand{}, err
	}
	if confirmation.Payment != nil {
		confirmation.Payment.ID = workspace.State.FactID
	}
	if confirmation.Invoice != nil {
		confirmation.Invoice.ID = workspace.State.FactID
	}
	if confirmation.Trip != nil {
		confirmation.Trip.ID = workspace.State.FactID
	}
	return ports.FactCorrectionCommand{State: preview.State, Revision: revision, Confirmation: confirmation, Reason: input.Reason, RequestHash: requestHash, PreviewHash: input.PreviewHash, WithdrawLinkIDs: input.WithdrawLinkIDs}, nil
}
