package reviews

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/claimsupport"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type Service struct {
	reviews ports.ReviewRepository
	tx      ports.TransactionManager
	ids     ports.IDGenerator
	clock   ports.Clock
}

func NewService(
	reviews ports.ReviewRepository,
	tx ports.TransactionManager,
	ids ports.IDGenerator,
	clock ports.Clock,
) Service {
	return Service{reviews: reviews, tx: tx, ids: ids, clock: clock}
}

type RevisionFieldInput struct {
	Path        string
	ValueType   string
	Presence    string
	Value       json.RawMessage
	EvidenceIDs []string
}

type RevisionInput struct {
	ExpectedRevision          int
	ExpectedOptimisticVersion int
	DocumentType              domain.DocumentType
	Fields                    []RevisionFieldInput
}

func (s Service) Get(
	ctx context.Context,
	tenant domain.TenantContext,
	jobID string,
) (ports.ReviewSnapshot, error) {
	if err := tenant.Require(domain.CapabilityClaimsReview); err != nil {
		return ports.ReviewSnapshot{}, err
	}
	return s.reviews.GetReview(ctx, tenant.TenantID, jobID)
}

func (s Service) GetClaimSet(
	ctx context.Context,
	tenant domain.TenantContext,
	claimSetID string,
) (ports.ReviewSnapshot, error) {
	if err := tenant.Require(domain.CapabilityClaimsReview); err != nil {
		return ports.ReviewSnapshot{}, err
	}
	result, err := s.reviews.GetClaimSet(ctx, tenant.TenantID, claimSetID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	if tenant.Role == domain.RoleReviewer && result.Status != domain.ClaimReadyForReview && result.Status != domain.ClaimBlocked {
		return ports.ReviewSnapshot{}, domain.ErrNotFound
	}
	return result, nil
}

func (s Service) Revise(
	ctx context.Context,
	tenant domain.TenantContext,
	jobID string,
	input RevisionInput,
) (ports.ReviewSnapshot, error) {
	if err := tenant.Require(domain.CapabilityClaimsReview); err != nil {
		return ports.ReviewSnapshot{}, err
	}
	current, err := s.reviews.GetReview(ctx, tenant.TenantID, jobID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	if input.ExpectedRevision != current.Revision ||
		input.ExpectedOptimisticVersion != current.OptimisticVersion {
		return ports.ReviewSnapshot{}, domain.ErrVersionConflict
	}
	if !input.DocumentType.Valid() {
		return ports.ReviewSnapshot{}, domain.NewRuleError("invalid_document_type", "文档类型不受支持", domain.ErrInvalidInput)
	}
	candidates, selections, err := revisionCandidates(current, input)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	validated := domain.ValidateClaim(domain.ClaimEnvelope{
		SchemaVersion:  "document-claim/2",
		DocumentType:   string(input.DocumentType),
		Fields:         candidates,
		DocumentIssues: []string{},
	}, current.PageCount)
	for _, validation := range validated.Validations {
		if validation.RuleCode == "incomplete_claim_snapshot" {
			return ports.ReviewSnapshot{}, domain.NewRuleError(
				"incomplete_claim_snapshot",
				"修订必须提交当前文档类型的完整字段快照",
				domain.ErrInvalidInput,
			)
		}
	}
	command, err := s.buildRevisionCommand(tenant, jobID, current, validated, selections)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	if err := s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		if linkInput, ok := claimsupport.LinkInputFromValidated(validated); ok {
			targets, err := transaction.ListEligibleLinkTargets(
				ctx,
				tenant.TenantID,
				linkInput.DocumentType,
				linkInput.Currency,
			)
			if err != nil {
				return err
			}
			command.Candidates, err = claimsupport.BuildLinkCandidates(
				linkInput,
				targets,
				tenant.TenantID,
				command.ClaimSet.ID,
				s.ids,
				command.ClaimSet.CreatedAt,
			)
			if err != nil {
				return err
			}
		}
		duplicateCandidates, limitExceeded, err := claimsupport.BuildDuplicateCandidates(
			ctx,
			transaction,
			validated,
			tenant.TenantID,
			current.DocumentID,
			command.ClaimSet.ID,
			s.ids,
			command.ClaimSet.CreatedAt,
		)
		if err != nil {
			return err
		}
		command.DuplicateCandidates = duplicateCandidates
		if limitExceeded {
			validation, err := claimsupport.NewDuplicateCandidateLimitValidation(
				tenant.TenantID,
				command.ClaimSet.ID,
				s.ids,
				command.ClaimSet.CreatedAt,
			)
			if err != nil {
				return err
			}
			command.Validations = append(command.Validations, validation)
			command.ClaimSet.Status = domain.ClaimBlocked
		}
		return transaction.PersistRevision(ctx, command)
	}); err != nil {
		return ports.ReviewSnapshot{}, err
	}
	return s.reviews.GetReview(ctx, tenant.TenantID, jobID)
}

func revisionCandidates(
	current ports.ReviewSnapshot,
	input RevisionInput,
) ([]domain.FieldCandidate, map[string][]ports.ReviewEvidence, error) {
	oldFields := make(map[string]ports.ReviewField, len(current.Fields))
	evidenceByID := make(map[string]ports.ReviewEvidence)
	for _, field := range current.Fields {
		oldFields[field.Path] = field
		for _, evidence := range field.Evidence {
			evidenceByID[evidence.ID] = evidence
		}
	}
	seen := make(map[string]struct{}, len(input.Fields))
	selections := make(map[string][]ports.ReviewEvidence, len(input.Fields))
	result := make([]domain.FieldCandidate, 0, len(input.Fields))
	for _, entry := range input.Fields {
		if entry.Path == "" || entry.Path == "document_type" {
			return nil, nil, domain.NewRuleError("invalid_field_path", "document_type 必须使用独立属性提交", domain.ErrInvalidInput)
		}
		if _, exists := seen[entry.Path]; exists {
			return nil, nil, domain.NewRuleError("duplicate_field_path", "同一路径只能提交一次", domain.ErrInvalidInput)
		}
		seen[entry.Path] = struct{}{}
		value, err := canonicalJSON(entry.Value)
		if err != nil {
			return nil, nil, domain.NewRuleError("invalid_field_value", "字段值不是有效 JSON", domain.ErrInvalidInput)
		}
		if entry.Presence == "absent" {
			if len(value) != 0 && !bytes.Equal(value, []byte("null")) {
				return nil, nil, domain.NewRuleError("absent_field_payload", "缺失字段不能携带值", domain.ErrInvalidInput)
			}
			if len(entry.EvidenceIDs) != 0 {
				return nil, nil, domain.NewRuleError("absent_field_evidence", "缺失字段不能携带证据", domain.ErrInvalidInput)
			}
			value = nil
		} else if entry.Presence == "present" && len(value) == 0 {
			return nil, nil, domain.NewRuleError("present_field_without_value", "存在字段必须携带值", domain.ErrInvalidInput)
		}
		old, existed := oldFields[entry.Path]
		changed := !existed || old.ValueType != entry.ValueType || old.Presence != entry.Presence || !jsonEqual(old.Value, value)
		selectedIDs := slices.Clone(entry.EvidenceIDs)
		if entry.Presence == "present" && len(selectedIDs) == 0 && !changed {
			for _, evidence := range old.Evidence {
				selectedIDs = append(selectedIDs, evidence.ID)
			}
		}
		if entry.Presence == "present" && changed && len(selectedIDs) == 0 && revisionFieldRequiresEvidence(entry) {
			return nil, nil, domain.NewRuleError(
				"evidence_selection_required",
				"新增或修改的字段必须显式选择同一文档证据",
				domain.ErrInvalidInput,
			)
		}
		candidate := domain.FieldCandidate{
			Path:      entry.Path,
			ValueType: entry.ValueType,
			Presence:  entry.Presence,
			Value:     value,
			Issues:    []string{},
		}
		for _, evidenceID := range selectedIDs {
			evidence, exists := evidenceByID[evidenceID]
			if !exists {
				return nil, nil, domain.NewRuleError("invalid_evidence", "证据不属于当前文档", domain.ErrInvalidInput)
			}
			selections[entry.Path] = append(selections[entry.Path], evidence)
			candidate.Evidence = append(candidate.Evidence, domain.CandidateEvidence{
				Page:   evidence.Page,
				Quote:  evidence.Quote,
				Region: slices.Clone(evidence.Region),
			})
		}
		result = append(result, candidate)
	}
	return result, selections, nil
}

func revisionFieldRequiresEvidence(entry RevisionFieldInput) bool {
	return entry.ValueType != "supplementary" &&
		entry.Path != "source_timezone" &&
		!strings.HasSuffix(entry.Path, "].sort_order")
}

func (s Service) buildRevisionCommand(
	tenant domain.TenantContext,
	jobID string,
	current ports.ReviewSnapshot,
	validated domain.ValidatedClaim,
	selections map[string][]ports.ReviewEvidence,
) (ports.RevisionCommand, error) {
	now := s.clock.Now()
	claimSetID, err := s.ids.NewID()
	if err != nil {
		return ports.RevisionCommand{}, err
	}
	command := ports.RevisionCommand{
		TenantID:           tenant.TenantID,
		JobID:              jobID,
		DocumentID:         current.DocumentID,
		PreviousClaimSetID: current.ClaimSetID,
		ClaimSet: ports.ClaimSetRecord{
			ID:            claimSetID,
			TenantID:      tenant.TenantID,
			DocumentID:    current.DocumentID,
			OriginAiRunID: current.OriginAiRunID,
			DocumentType:  validated.DocumentType,
			Status:        validated.Status,
			CreatedAt:     now,
		},
		RevisedByUserID:           tenant.UserID,
		ExpectedRevision:          current.Revision,
		ExpectedOptimisticVersion: current.OptimisticVersion,
		NormalizedInvoiceNumber:   claimsupport.NormalizedInvoiceNumber(validated.Fields),
	}
	oldByPath := make(map[string]ports.ReviewField, len(current.Fields))
	currentPaths := make(map[string]struct{}, len(validated.Fields))
	fieldIDs := make(map[string]string, len(validated.Fields))
	for _, field := range current.Fields {
		oldByPath[field.Path] = field
	}
	for _, field := range validated.Fields {
		fieldID, err := s.ids.NewID()
		if err != nil {
			return ports.RevisionCommand{}, err
		}
		fieldIDs[field.Path] = fieldID
		currentPaths[field.Path] = struct{}{}
		old, existed := oldByPath[field.Path]
		source := "user"
		sourceUserID := tenant.UserID
		if existed && old.ValueType == field.ValueType && old.Presence == field.Presence && jsonEqual(old.Value, field.Value) {
			source = old.Source
			sourceUserID = old.SourceUserID
		}
		command.Fields = append(command.Fields, ports.RevisionFieldRecord{
			FieldClaimRecord: ports.FieldClaimRecord{
				ID:              fieldID,
				TenantID:        tenant.TenantID,
				ClaimSetID:      claimSetID,
				FieldPath:       field.Path,
				ValueType:       field.ValueType,
				Presence:        field.Presence,
				TypedValueJSON:  string(field.Value),
				NormalizedValue: string(field.NormalizedValue),
				CreatedAt:       now,
			},
			Source:            source,
			SourceUserID:      sourceUserID,
			SupersedesFieldID: old.ID,
		})
		for _, evidence := range selections[field.Path] {
			evidenceID, err := s.ids.NewID()
			if err != nil {
				return ports.RevisionCommand{}, err
			}
			region := string(evidence.Region)
			command.Evidence = append(command.Evidence, ports.RevisionEvidenceRecord{
				EvidenceRecord: ports.EvidenceRecord{
					ID:             evidenceID,
					TenantID:       tenant.TenantID,
					FieldClaimID:   fieldID,
					DocumentPageID: evidence.DocumentPageID,
					Quote:          evidence.Quote,
					RegionJSON:     region,
					EvidenceHash:   claimsupport.EvidenceHash(evidence.DocumentPageID, evidence.Quote, region),
					CreatedAt:      now,
				},
				CopiedFromEvidenceID: evidence.ID,
			})
		}
	}
	for path, old := range oldByPath {
		if _, exists := currentPaths[path]; exists {
			continue
		}
		fieldID, err := s.ids.NewID()
		if err != nil {
			return ports.RevisionCommand{}, err
		}
		command.Fields = append(command.Fields, ports.RevisionFieldRecord{
			FieldClaimRecord: ports.FieldClaimRecord{
				ID:         fieldID,
				TenantID:   tenant.TenantID,
				ClaimSetID: claimSetID,
				FieldPath:  path,
				ValueType:  old.ValueType,
				Presence:   "absent",
				CreatedAt:  now,
			},
			Source:            "user",
			SourceUserID:      tenant.UserID,
			SupersedesFieldID: old.ID,
		})
	}
	for _, validation := range validated.Validations {
		record, err := claimsupport.NewValidationRecord(validation, tenant.TenantID, claimSetID, fieldIDs, s.ids, now)
		if err != nil {
			return ports.RevisionCommand{}, err
		}
		command.Validations = append(command.Validations, record)
	}
	duplicate, err := claimsupport.NewValidationRecord(domain.ClaimValidation{
		FieldPath:   "invoice_number",
		RuleCode:    "duplicate_invoice_number",
		Severity:    "blocked",
		Status:      "blocked",
		SafeMessage: "同一工作区已存在规范化号码完全一致的发票",
	}, tenant.TenantID, claimSetID, fieldIDs, s.ids, now)
	if err != nil {
		return ports.RevisionCommand{}, err
	}
	command.DuplicateInvoiceValidation = &duplicate
	return command, nil
}

func canonicalJSON(value json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(value)) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("encode canonical JSON: %w", err)
	}
	return encoded, nil
}

func jsonEqual(left, right json.RawMessage) bool {
	canonicalLeft, leftErr := canonicalJSON(left)
	canonicalRight, rightErr := canonicalJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(canonicalLeft, canonicalRight)
}
