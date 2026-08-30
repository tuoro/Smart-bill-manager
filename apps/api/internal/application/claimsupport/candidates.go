package claimsupport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const LinkRuleVersion = "payment-invoice-link/2"

type LinkMatchInput struct {
	DocumentType domain.DocumentType
	AmountMinor  int64
	Currency     string
	BusinessDate string
	DisplayName  string
}

func LinkInputFromValidated(validated domain.ValidatedClaim) (LinkMatchInput, bool) {
	fields := make(map[string]domain.FieldCandidate, len(validated.Fields))
	for _, field := range validated.Fields {
		if field.Presence == "present" {
			fields[field.Path] = field
		}
	}
	input := LinkMatchInput{DocumentType: validated.DocumentType}
	var err error
	switch validated.DocumentType {
	case domain.DocumentPayment:
		input.AmountMinor, err = candidateInteger(fields["amount_minor"].Value)
		if err != nil {
			return LinkMatchInput{}, false
		}
		input.Currency, err = candidateString(fields["currency"].Value)
		if err != nil {
			return LinkMatchInput{}, false
		}
		input.DisplayName, err = candidateString(fields["merchant"].Value)
		if err != nil {
			return LinkMatchInput{}, false
		}
		instantText, instantErr := candidateString(fields["transaction_time"].Value)
		timezoneName, timezoneErr := candidateString(fields["source_timezone"].Value)
		if instantErr != nil || timezoneErr != nil {
			return LinkMatchInput{}, false
		}
		instant, instantErr := time.Parse(time.RFC3339Nano, instantText)
		location, timezoneErr := time.LoadLocation(timezoneName)
		if instantErr != nil || timezoneErr != nil {
			return LinkMatchInput{}, false
		}
		input.BusinessDate = instant.In(location).Format("2006-01-02")
	case domain.DocumentInvoice:
		input.AmountMinor, err = candidateInteger(fields["total_minor"].Value)
		if err != nil {
			return LinkMatchInput{}, false
		}
		input.Currency, err = candidateString(fields["currency"].Value)
		if err != nil {
			return LinkMatchInput{}, false
		}
		input.BusinessDate, err = candidateString(fields["invoice_date"].Value)
		if err != nil {
			return LinkMatchInput{}, false
		}
		input.DisplayName, err = candidateString(fields["seller_name"].Value)
		if err != nil {
			return LinkMatchInput{}, false
		}
	default:
		return LinkMatchInput{}, false
	}
	if _, err := time.Parse("2006-01-02", input.BusinessDate); err != nil {
		return LinkMatchInput{}, false
	}
	return input, true
}

func BuildLinkCandidates(
	input LinkMatchInput,
	targets []ports.LinkTarget,
	tenantID, claimSetID string,
	ids ports.IDGenerator,
	now time.Time,
) ([]ports.LinkCandidateRecord, error) {
	inputDate, err := time.Parse("2006-01-02", input.BusinessDate)
	if err != nil {
		return nil, err
	}
	if input.AmountMinor <= 0 {
		return []ports.LinkCandidateRecord{}, nil
	}
	result := make([]ports.LinkCandidateRecord, 0)
	for _, target := range targets {
		if target.DocumentType == input.DocumentType ||
			target.Currency != input.Currency ||
			target.RemainingMinor <= 0 {
			continue
		}
		targetDate, err := time.Parse("2006-01-02", target.BusinessDate)
		if err != nil {
			return nil, fmt.Errorf("parse link target business date: %w", err)
		}
		distance := int(inputDate.Sub(targetDate).Hours() / 24)
		if distance < 0 {
			distance = -distance
		}
		if distance > 30 {
			continue
		}
		nameExact := domain.NormalizeExact(input.DisplayName) == domain.NormalizeExact(target.DisplayName)
		reasons := []string{"currency_exact", "date_within_30_days", "remaining_available"}
		if target.RemainingMinor == input.AmountMinor {
			reasons = append(reasons, "remaining_exact")
		} else {
			reasons = append(reasons, "partial_allocation")
		}
		if nameExact {
			reasons = append(reasons, "name_exact")
		} else {
			reasons = append(reasons, "name_mismatch_warning")
		}
		reasonJSON, _ := json.Marshal(reasons)
		candidateID, err := ids.NewID()
		if err != nil {
			return nil, err
		}
		keySource, _ := json.Marshal([]string{tenantID, claimSetID, target.FactID, LinkRuleVersion})
		record := ports.LinkCandidateRecord{
			ID:               candidateID,
			TenantID:         tenantID,
			ClaimSetID:       claimSetID,
			CandidateKey:     HashBytes(keySource),
			RuleVersion:      LinkRuleVersion,
			ReasonCodesJSON:  string(reasonJSON),
			NameExact:        nameExact,
			DateDistanceDays: distance,
			CreatedAt:        now,
		}
		if target.DocumentType == domain.DocumentPayment {
			record.ExistingPaymentID = target.FactID
		} else {
			record.ExistingInvoiceID = target.FactID
		}
		result = append(result, record)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].NameExact != result[right].NameExact {
			return result[left].NameExact
		}
		if result[left].DateDistanceDays != result[right].DateDistanceDays {
			return result[left].DateDistanceDays < result[right].DateDistanceDays
		}
		leftTarget := result[left].ExistingPaymentID + result[left].ExistingInvoiceID
		rightTarget := result[right].ExistingPaymentID + result[right].ExistingInvoiceID
		return leftTarget < rightTarget
	})
	return result, nil
}

func candidateString(value json.RawMessage) (string, error) {
	if len(value) == 0 {
		return "", errors.New("missing string")
	}
	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		return "", err
	}
	return result, nil
}

func candidateInteger(value json.RawMessage) (int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return 0, err
	}
	return strconv.ParseInt(number.String(), 10, 64)
}
