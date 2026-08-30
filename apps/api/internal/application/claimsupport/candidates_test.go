package claimsupport

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type candidateIDGenerator struct {
	index  int
	failAt int
}

func (g *candidateIDGenerator) NewID() (string, error) {
	g.index++
	if g.index == g.failAt {
		return "", errors.New("id failure")
	}
	return "candidate-id-" + string(rune('0'+g.index)), nil
}

func TestLinkInputAndCandidateHardConditions(t *testing.T) {
	payment := domain.ValidateClaim(candidatePaymentEnvelope(), 1)
	input, ok := LinkInputFromValidated(payment)
	if !ok || input.BusinessDate != "2026-08-27" || input.AmountMinor != 12345 || input.Currency != "CNY" {
		t.Fatalf("payment link input = %#v, ok=%v", input, ok)
	}
	invoice := domain.ValidateClaim(candidateInvoiceEnvelope(), 1)
	invoiceInput, ok := LinkInputFromValidated(invoice)
	if !ok || invoiceInput.BusinessDate != "2026-08-28" || invoiceInput.DisplayName != "Example Merchant" {
		t.Fatalf("invoice link input = %#v, ok=%v", invoiceInput, ok)
	}
	if _, ok := LinkInputFromValidated(domain.ValidatedClaim{DocumentType: domain.DocumentUnknown}); ok {
		t.Fatal("unknown document produced link input")
	}

	targets := []ports.LinkTarget{
		{FactID: "same-type", DocumentType: domain.DocumentPayment, AmountMinor: 12345, RemainingMinor: 12345, Currency: "CNY", BusinessDate: "2026-08-27", DisplayName: "Example Merchant"},
		{FactID: "partial", DocumentType: domain.DocumentInvoice, AmountMinor: 20000, AllocatedMinor: 1000, RemainingMinor: 19000, Currency: "CNY", BusinessDate: "2026-08-27", DisplayName: "Example Merchant"},
		{FactID: "wrong-currency", DocumentType: domain.DocumentInvoice, AmountMinor: 12345, RemainingMinor: 12345, Currency: "USD", BusinessDate: "2026-08-27", DisplayName: "Example Merchant"},
		{FactID: "too-far", DocumentType: domain.DocumentInvoice, AmountMinor: 12345, RemainingMinor: 12345, Currency: "CNY", BusinessDate: "2026-06-01", DisplayName: "Example Merchant"},
		{FactID: "exact", DocumentType: domain.DocumentInvoice, AmountMinor: 12345, RemainingMinor: 12345, Currency: "CNY", BusinessDate: "2026-08-26", DisplayName: "  EXAMPLE   Merchant "},
		{FactID: "warning", DocumentType: domain.DocumentInvoice, AmountMinor: 12345, RemainingMinor: 10000, Currency: "CNY", BusinessDate: "2026-09-02", DisplayName: "Different Merchant"},
		{FactID: "fully-allocated", DocumentType: domain.DocumentInvoice, AmountMinor: 12345, AllocatedMinor: 12345, RemainingMinor: 0, Currency: "CNY", BusinessDate: "2026-08-27", DisplayName: "Example Merchant"},
	}
	ids := &candidateIDGenerator{}
	candidates, err := BuildLinkCandidates(input, targets, "tenant", "claim", ids, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 || !candidates[0].NameExact || candidates[0].DateDistanceDays != 0 || !candidates[1].NameExact || candidates[1].DateDistanceDays != 1 || candidates[2].NameExact || candidates[2].DateDistanceDays != 6 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].ExistingInvoiceID != "partial" || candidates[0].ExistingPaymentID != "" || candidates[0].CandidateKey == "" {
		t.Fatalf("candidate target/key = %#v", candidates[0])
	}
	if _, err := BuildLinkCandidates(LinkMatchInput{BusinessDate: "invalid"}, nil, "tenant", "claim", ids, time.Time{}); err == nil {
		t.Fatal("invalid source date accepted")
	}
	badTarget := []ports.LinkTarget{{FactID: "bad-date", DocumentType: domain.DocumentInvoice, AmountMinor: 12345, RemainingMinor: 12345, Currency: "CNY", BusinessDate: "invalid"}}
	if _, err := BuildLinkCandidates(input, badTarget, "tenant", "claim", ids, time.Time{}); err == nil {
		t.Fatal("invalid persisted target date accepted")
	}
	ids = &candidateIDGenerator{failAt: 1}
	if _, err := BuildLinkCandidates(input, targets[4:5], "tenant", "claim", ids, time.Time{}); err == nil {
		t.Fatal("candidate ID failure ignored")
	}
}

func TestValidationRecordAndInvoiceNormalization(t *testing.T) {
	ids := &candidateIDGenerator{}
	record, err := NewValidationRecord(domain.ClaimValidation{
		FieldPath: "invoice_number", RuleCode: "duplicate_invoice_number", Severity: "blocked", Status: "blocked", SafeMessage: "duplicate",
	}, "tenant", "claim", map[string]string{"invoice_number": "field"}, ids, time.Unix(10, 0))
	if err != nil || record.FieldClaimID != "field" || record.RuleVersion != ValidationRuleVersion {
		t.Fatalf("validation record = %#v, error=%v", record, err)
	}
	ids.failAt = 2
	if _, err := NewValidationRecord(domain.ClaimValidation{}, "tenant", "claim", nil, ids, time.Time{}); err == nil {
		t.Fatal("validation ID failure ignored")
	}
	fields := []domain.FieldCandidate{{Path: "invoice_number", Presence: "present", NormalizedValue: json.RawMessage(`"inv-001"`)}}
	if value := NormalizedInvoiceNumber(fields); value != "inv-001" {
		t.Fatalf("normalized invoice number = %q", value)
	}
	fields[0].NormalizedValue = nil
	fields[0].Value = json.RawMessage(`" ＩＮＶ-001 "`)
	if value := NormalizedInvoiceNumber(fields); value != "inv-001" {
		t.Fatalf("fallback invoice number = %q", value)
	}
	fields[0].Presence = "absent"
	if value := NormalizedInvoiceNumber(fields); value != "" {
		t.Fatalf("absent invoice number = %q", value)
	}
}

func candidatePaymentEnvelope() domain.ClaimEnvelope {
	evidence := []domain.CandidateEvidence{{Page: 1, Quote: "evidence"}}
	return domain.ClaimEnvelope{SchemaVersion: "document-claim/3", DocumentType: "payment", DocumentIssues: []string{}, Fields: []domain.FieldCandidate{
		{Path: "amount_minor", ValueType: "money_minor", Presence: "present", Value: json.RawMessage(`12345`), Evidence: evidence, Issues: []string{}},
		{Path: "currency", ValueType: "string", Presence: "present", Value: json.RawMessage(`"CNY"`), Evidence: evidence, Issues: []string{}},
		{Path: "merchant", ValueType: "string", Presence: "present", Value: json.RawMessage(`"Example Merchant"`), Evidence: evidence, Issues: []string{}},
		{Path: "transaction_time", ValueType: "instant", Presence: "present", Value: json.RawMessage(`"2026-08-27T23:30:00+08:00"`), Evidence: evidence, Issues: []string{}},
		{Path: "source_timezone", ValueType: "string", Presence: "present", Value: json.RawMessage(`"Asia/Shanghai"`), Evidence: evidence, Issues: []string{}},
		{Path: "payment_method", ValueType: "string", Presence: "absent", Issues: []string{}},
		{Path: "order_number", ValueType: "string", Presence: "absent", Issues: []string{}},
		{Path: "category", ValueType: "string", Presence: "absent", Issues: []string{}},
		{Path: "supplementary_fields", ValueType: "supplementary", Presence: "absent", Issues: []string{}},
	}}
}

func candidateInvoiceEnvelope() domain.ClaimEnvelope {
	evidence := []domain.CandidateEvidence{{Page: 1, Quote: "evidence"}}
	return domain.ClaimEnvelope{SchemaVersion: "document-claim/3", DocumentType: "invoice", DocumentIssues: []string{}, Fields: []domain.FieldCandidate{
		{Path: "invoice_number", ValueType: "string", Presence: "present", Value: json.RawMessage(`"INV-001"`), Evidence: evidence, Issues: []string{}},
		{Path: "invoice_date", ValueType: "date", Presence: "present", Value: json.RawMessage(`"2026-08-28"`), Evidence: evidence, Issues: []string{}},
		{Path: "total_minor", ValueType: "money_minor", Presence: "present", Value: json.RawMessage(`12345`), Evidence: evidence, Issues: []string{}},
		{Path: "tax_minor", ValueType: "money_minor", Presence: "absent", Issues: []string{}},
		{Path: "currency", ValueType: "string", Presence: "present", Value: json.RawMessage(`"CNY"`), Evidence: evidence, Issues: []string{}},
		{Path: "seller_name", ValueType: "string", Presence: "present", Value: json.RawMessage(`"Example Merchant"`), Evidence: evidence, Issues: []string{}},
		{Path: "buyer_name", ValueType: "string", Presence: "present", Value: json.RawMessage(`"Example Buyer"`), Evidence: evidence, Issues: []string{}},
		{Path: "supplementary_fields", ValueType: "supplementary", Presence: "absent", Issues: []string{}},
	}}
}
