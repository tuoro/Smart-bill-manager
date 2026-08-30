package claimsupport

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type duplicateCandidateSource interface {
	ListVisualDuplicateDocuments(
		ctx context.Context,
		tenantID, documentID string,
	) (domain.VisualDocument, []domain.VisualDocument, error)
	ListFieldDuplicateTargets(
		ctx context.Context,
		tenantID string,
		input domain.FieldDuplicateInput,
	) ([]domain.FieldDuplicateTarget, error)
}

func DuplicateInputFromValidated(validated domain.ValidatedClaim) (domain.FieldDuplicateInput, bool) {
	fields := make(map[string]domain.FieldCandidate, len(validated.Fields))
	for _, field := range validated.Fields {
		if field.Presence == "present" {
			fields[field.Path] = field
		}
	}
	input := domain.FieldDuplicateInput{DocumentType: validated.DocumentType}
	var err error
	switch validated.DocumentType {
	case domain.DocumentPayment:
		input.AmountMinor, err = candidateInteger(fields["amount_minor"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
		input.Currency, err = candidateString(fields["currency"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
		input.Merchant, err = candidateString(fields["merchant"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
		input.TransactionTime, err = candidateString(fields["transaction_time"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
		input.OrderNumber = optionalCandidateString(fields["order_number"])
	case domain.DocumentInvoice:
		input.AmountMinor, err = candidateInteger(fields["total_minor"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
		input.Currency, err = candidateString(fields["currency"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
		input.InvoiceNumber, err = candidateString(fields["invoice_number"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
		input.InvoiceDate, err = candidateString(fields["invoice_date"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
		input.SellerName, err = candidateString(fields["seller_name"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
		input.BuyerName, err = candidateString(fields["buyer_name"].Value)
		if err != nil {
			return domain.FieldDuplicateInput{}, false
		}
	default:
		return domain.FieldDuplicateInput{}, false
	}
	return input, true
}

func BuildDuplicateCandidates(
	ctx context.Context,
	source duplicateCandidateSource,
	validated domain.ValidatedClaim,
	tenantID, documentID, claimSetID string,
	ids ports.IDGenerator,
	now time.Time,
) ([]ports.DuplicateCandidateRecord, bool, error) {
	current, visualTargets, err := source.ListVisualDuplicateDocuments(ctx, tenantID, documentID)
	if err != nil {
		return nil, false, err
	}
	var fieldInput *domain.FieldDuplicateInput
	var fieldTargets []domain.FieldDuplicateTarget
	if input, ok := DuplicateInputFromValidated(validated); ok {
		fieldInput = &input
		fieldTargets, err = source.ListFieldDuplicateTargets(ctx, tenantID, input)
		if err != nil {
			return nil, false, err
		}
	}
	specs, err := domain.BuildDuplicateCandidateSpecs(current, visualTargets, fieldInput, fieldTargets)
	if err != nil {
		return nil, false, err
	}
	if len(specs) > domain.MaxDuplicateCandidates {
		return []ports.DuplicateCandidateRecord{}, true, nil
	}
	result := make([]ports.DuplicateCandidateRecord, 0, len(specs))
	for _, spec := range specs {
		candidateID, err := ids.NewID()
		if err != nil {
			return nil, false, err
		}
		candidateKey, err := domain.DuplicateCandidateKey(tenantID, claimSetID, spec)
		if err != nil {
			return nil, false, err
		}
		reasons, err := json.Marshal(spec.ReasonCodes)
		if err != nil {
			return nil, false, err
		}
		result = append(result, ports.DuplicateCandidateRecord{
			ID:                     candidateID,
			TenantID:               tenantID,
			ClaimSetID:             claimSetID,
			Kind:                   spec.Kind,
			ExistingDocumentID:     spec.ExistingDocumentID,
			CurrentDocumentPageID:  spec.CurrentDocumentPageID,
			ExistingDocumentPageID: spec.ExistingDocumentPageID,
			ExistingPaymentID:      spec.ExistingPaymentID,
			ExistingInvoiceID:      spec.ExistingInvoiceID,
			CandidateKey:           candidateKey,
			RuleVersion:            domain.DuplicateDetectionRuleVersion,
			ReasonCodesJSON:        string(reasons),
			DHashDistance:          cloneInteger(spec.DHashDistance),
			AHashDistance:          cloneInteger(spec.AHashDistance),
			CreatedAt:              now,
		})
	}
	return result, false, nil
}

func NewDuplicateCandidateLimitValidation(
	tenantID, claimSetID string,
	ids ports.IDGenerator,
	now time.Time,
) (ports.ValidationRecord, error) {
	id, err := ids.NewID()
	if err != nil {
		return ports.ValidationRecord{}, err
	}
	return ports.ValidationRecord{
		ID:          id,
		TenantID:    tenantID,
		ClaimSetID:  claimSetID,
		RuleCode:    "duplicate_candidate_limit_exceeded",
		Severity:    "blocked",
		Status:      "blocked",
		SafeMessage: "疑似重复候选超过 50 项，请驳回当前 Claim 或减少重复来源后重新处理",
		RuleVersion: domain.DuplicateDetectionRuleVersion,
		CreatedAt:   now,
	}, nil
}

func optionalCandidateString(field domain.FieldCandidate) string {
	if field.Presence != "present" {
		return ""
	}
	value, err := candidateString(field.Value)
	if err != nil {
		return ""
	}
	return value
}

func cloneInteger(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
