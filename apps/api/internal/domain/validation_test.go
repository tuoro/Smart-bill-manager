package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidatePaymentClaim(t *testing.T) {
	t.Parallel()

	envelope := ClaimEnvelope{
		SchemaVersion: "document-claim/3",
		DocumentType:  "payment",
		Fields: []FieldCandidate{
			present("amount_minor", "money_minor", int64(1234)),
			present("currency", "string", "CNY"),
			present("merchant", "string", "  ACME   Store "),
			present("transaction_time", "instant", "2026-08-27T08:30:00+08:00"),
			present("source_timezone", "string", "Asia/Shanghai"),
			absent("payment_method", "string"),
			absent("order_number", "string"),
			absent("category", "string"),
			absent("supplementary_fields", "supplementary"),
		},
		DocumentIssues: []string{},
	}
	validated := ValidateClaim(envelope, 1)
	if validated.Status != ClaimReadyForReview {
		t.Fatalf("status = %s, validations = %#v", validated.Status, validated.Validations)
	}
	for _, field := range validated.Fields {
		if field.Path == "merchant" {
			var normalized string
			if err := json.Unmarshal(field.NormalizedValue, &normalized); err != nil {
				t.Fatal(err)
			}
			if normalized != "acme store" {
				t.Fatalf("normalized merchant = %q", normalized)
			}
		}
	}
}

func TestValidateClaimBlocksIncompleteSnapshotAndBadEvidence(t *testing.T) {
	t.Parallel()

	envelope := ClaimEnvelope{
		SchemaVersion: "document-claim/3",
		DocumentType:  "payment",
		Fields: []FieldCandidate{
			presentWithPage("amount_minor", "money_minor", int64(1234), 2),
			present("currency", "string", "CNY"),
			present("merchant", "string", "ACME"),
			present("transaction_time", "instant", "2026-08-27T08:30:00Z"),
			present("source_timezone", "string", "UTC"),
		},
	}
	validated := ValidateClaim(envelope, 1)
	if validated.Status != ClaimBlocked {
		t.Fatalf("status = %s", validated.Status)
	}
	assertValidation(t, validated.Validations, "invalid_evidence_page")
	assertValidation(t, validated.Validations, "incomplete_claim_snapshot")
}

func TestValidateTripClaimDateAndRequiredFieldBoundaries(t *testing.T) {
	t.Parallel()

	valid := validTripEnvelope()
	validated := ValidateClaim(valid, 2)
	if validated.Status != ClaimReadyForReview {
		t.Fatalf("valid Trip status = %s, validations = %#v", validated.Status, validated.Validations)
	}

	reversed := validTripEnvelope()
	for index := range reversed.Fields {
		if reversed.Fields[index].Path == "end_date" {
			reversed.Fields[index] = present("end_date", "date", "2026-08-25")
		}
	}
	reversedResult := ValidateClaim(reversed, 2)
	if reversedResult.Status != ClaimBlocked {
		t.Fatalf("reversed Trip status = %s", reversedResult.Status)
	}
	assertValidation(t, reversedResult.Validations, "trip_date_range_invalid")

	missingDestination := validTripEnvelope()
	for index := range missingDestination.Fields {
		if missingDestination.Fields[index].Path == "destination" {
			missingDestination.Fields[index] = absent("destination", "string")
		}
	}
	missingResult := ValidateClaim(missingDestination, 2)
	if missingResult.Status != ClaimBlocked {
		t.Fatalf("missing destination Trip status = %s", missingResult.Status)
	}
	assertValidation(t, missingResult.Validations, "required_field_absent")
}

func TestStabilizeInvoiceItemsAndTotalValidation(t *testing.T) {
	t.Parallel()

	envelope := ClaimEnvelope{
		SchemaVersion: "document-claim/3",
		DocumentType:  "invoice",
		Fields: []FieldCandidate{
			present("invoice_number", "string", " INV 001 "),
			present("invoice_date", "date", "2026-08-27"),
			present("total_minor", "money_minor", int64(1000)),
			absent("tax_minor", "money_minor"),
			present("currency", "string", "USD"),
			present("seller_name", "string", "Seller"),
			present("buyer_name", "string", "Buyer"),
			present("items[0].name", "string", "Item"),
			absent("items[0].quantity", "decimal"),
			absent("items[0].unit", "string"),
			absent("items[0].unit_price_minor", "money_minor"),
			present("items[0].amount_minor", "money_minor", int64(900)),
			absent("items[0].tax_minor", "money_minor"),
			present("items[0].sort_order", "integer", int64(0)),
			absent("supplementary_fields", "supplementary"),
		},
	}
	stabilized, err := StabilizeItemPaths(envelope, func() (string, error) {
		return "00000000-0000-4000-8000-000000000201", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	validated := ValidateClaim(stabilized, 1)
	if validated.Status != ClaimBlocked {
		t.Fatalf("status = %s", validated.Status)
	}
	assertValidation(t, validated.Validations, "invoice_item_total_conflict")
}

func TestNormalizeExactOnlyFoldsLatinASCII(t *testing.T) {
	t.Parallel()

	if got := NormalizeExact("  ＡCME   Σeller  "); got != "acme Σeller" {
		t.Fatalf("NormalizeExact() = %q", got)
	}
}

func TestValidateClaimRejectsEnvelopeAndFieldBoundaryViolations(t *testing.T) {
	t.Parallel()

	invalidEnvelope := validPaymentEnvelope()
	invalidEnvelope.SchemaVersion = "unknown"
	assertValidation(t, ValidateClaim(invalidEnvelope, 1).Validations, "invalid_claim_envelope")

	tests := []struct {
		name   string
		mutate func(*ClaimEnvelope)
		code   string
	}{
		{"duplicate path", func(value *ClaimEnvelope) { value.Fields = append(value.Fields, value.Fields[0]) }, "duplicate_field_path"},
		{"unknown path", func(value *ClaimEnvelope) {
			value.Fields = append(value.Fields, present("surprise", "string", "value"))
		}, "unknown_field_path"},
		{"wrong type", func(value *ClaimEnvelope) { value.Fields[2].ValueType = "integer" }, "field_type_mismatch"},
		{"invalid presence", func(value *ClaimEnvelope) { value.Fields[2].Presence = "maybe" }, "invalid_presence"},
		{"absent payload", func(value *ClaimEnvelope) { value.Fields[5].Value = json.RawMessage(`"value"`) }, "absent_field_payload"},
		{"present without value", func(value *ClaimEnvelope) { value.Fields[2].Value = nil }, "present_field_without_value"},
		{"missing evidence", func(value *ClaimEnvelope) { value.Fields[2].Evidence = nil }, "missing_field_evidence"},
		{"empty evidence", func(value *ClaimEnvelope) { value.Fields[2].Evidence = []CandidateEvidence{{Page: 1}} }, "empty_field_evidence"},
		{"required absent", func(value *ClaimEnvelope) { value.Fields[2] = absent("merchant", "string") }, "required_field_absent"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			envelope := validPaymentEnvelope()
			test.mutate(&envelope)
			validated := ValidateClaim(envelope, 1)
			if validated.Status != ClaimBlocked {
				t.Fatalf("status = %s", validated.Status)
			}
			assertValidation(t, validated.Validations, test.code)
		})
	}
}

func TestValidateClaimPersistsOneBlockedCandidatePerDuplicatePath(t *testing.T) {
	t.Parallel()

	envelope := validPaymentEnvelope()
	envelope.Fields = append(envelope.Fields,
		present("merchant", "string", "Duplicate Merchant"),
		present("document_type", "document_type", "invoice"),
	)
	validated := ValidateClaim(envelope, 1)
	if validated.Status != ClaimBlocked {
		t.Fatalf("status = %s", validated.Status)
	}
	assertValidation(t, validated.Validations, "duplicate_field_path")
	seen := make(map[string]struct{}, len(validated.Fields))
	for _, field := range validated.Fields {
		if _, duplicate := seen[field.Path]; duplicate {
			t.Fatalf("duplicate field remained in persistence shape: %s", field.Path)
		}
		seen[field.Path] = struct{}{}
		if field.Path == "document_type" && string(field.Value) != `"payment"` {
			t.Fatalf("model-supplied document type replaced envelope type: %s", field.Value)
		}
	}
}

func TestValidateTypedValueAndDocumentIssueBoundaries(t *testing.T) {
	t.Parallel()

	tests := []FieldCandidate{
		{Path: "merchant", ValueType: "string", Value: json.RawMessage(`""`)},
		{Path: "document_type", ValueType: "document_type", Value: json.RawMessage(`"receipt"`)},
		{Path: "currency", ValueType: "string", Value: json.RawMessage(`"GBP"`)},
		{Path: "source_timezone", ValueType: "string", Value: json.RawMessage(`"Mars/Olympus"`)},
		{Path: "amount_minor", ValueType: "money_minor", Value: json.RawMessage(`-1`)},
		{Path: "amount_minor", ValueType: "money_minor", Value: json.RawMessage(`1.5`)},
		{Path: "amount_minor", ValueType: "money_minor", Value: json.RawMessage(`9007199254740992`)},
		{Path: "invoice_date", ValueType: "date", Value: json.RawMessage(`1`)},
		{Path: "invoice_date", ValueType: "date", Value: json.RawMessage(`"2026-02-30"`)},
		{Path: "transaction_time", ValueType: "instant", Value: json.RawMessage(`1`)},
		{Path: "transaction_time", ValueType: "instant", Value: json.RawMessage(`"yesterday"`)},
		{Path: "quantity", ValueType: "decimal", Value: json.RawMessage(`1`)},
		{Path: "quantity", ValueType: "decimal", Value: json.RawMessage(`"01.2"`)},
		{Path: "unknown", ValueType: "unsupported", Value: json.RawMessage(`1`)},
	}
	for _, field := range tests {
		if err := validateTypedValue(&field); err == nil {
			t.Fatalf("invalid typed value accepted: %#v", field)
		}
	}

	envelope := validPaymentEnvelope()
	envelope.DocumentIssues = []string{
		"cross_page_continuation", "ambiguous_repeated_header", "cross_page_total_conflict",
		"uncertain_page_order", "conflicting_values", "missing_required_field", "other_issue",
	}
	validated := ValidateClaim(envelope, 2)
	for _, code := range []string{
		"cross_page_continuation", "ambiguous_repeated_header", "cross_page_total_conflict",
		"uncertain_page_order", "conflicting_values", "missing_required_field", "model_issue_other_issue",
	} {
		assertValidation(t, validated.Validations, code)
	}
}

func TestValidateSupplementaryValueBoundaries(t *testing.T) {
	t.Parallel()

	if err := validateSupplementaryValue(json.RawMessage(`[{"path":"payment.discount","label":"优惠","value":{"amount":"2.00"}}]`)); err != nil {
		t.Fatalf("valid supplementary value rejected: %v", err)
	}

	tooMany := make([]map[string]any, 101)
	for index := range tooMany {
		tooMany[index] = map[string]any{"path": "field", "value": index}
	}
	tooManyJSON, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "too large", raw: json.RawMessage(strings.Repeat(" ", 64*1024+1))},
		{name: "not an array", raw: json.RawMessage(`{"path":"field"}`)},
		{name: "empty", raw: json.RawMessage(`[]`)},
		{name: "too many", raw: tooManyJSON},
		{name: "blank path", raw: json.RawMessage(`[{"path":" ","value":1}]`)},
		{name: "path too long", raw: json.RawMessage(`[{"path":"` + strings.Repeat("路", 241) + `","value":1}]`)},
		{name: "label too long", raw: json.RawMessage(`[{"path":"field","label":"` + strings.Repeat("签", 161) + `","value":1}]`)},
		{name: "missing value", raw: json.RawMessage(`[{"path":"field"}]`)},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateSupplementaryValue(test.raw); err == nil {
				t.Fatalf("invalid supplementary value accepted: %s", test.raw)
			}
		})
	}
}

func TestValidEvidenceRegionBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   json.RawMessage
		valid bool
	}{
		{name: "valid", raw: json.RawMessage(`{"x":0.1,"y":0.2,"width":0.3,"height":0.4}`), valid: true},
		{name: "malformed", raw: json.RawMessage(`{`), valid: false},
		{name: "missing coordinate", raw: json.RawMessage(`{"x":0,"y":0,"width":1}`), valid: false},
		{name: "non numeric", raw: json.RawMessage(`{"x":"left","y":0,"width":1,"height":1}`), valid: false},
		{name: "negative origin", raw: json.RawMessage(`{"x":-0.1,"y":0,"width":1,"height":1}`), valid: false},
		{name: "origin outside page", raw: json.RawMessage(`{"x":0,"y":1.1,"width":1,"height":1}`), valid: false},
		{name: "zero width", raw: json.RawMessage(`{"x":0,"y":0,"width":0,"height":1}`), valid: false},
		{name: "height outside page", raw: json.RawMessage(`{"x":0,"y":0,"width":1,"height":1.1}`), valid: false},
		{name: "rectangle crosses edge", raw: json.RawMessage(`{"x":0.8,"y":0.8,"width":0.3,"height":0.3}`), valid: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := validEvidenceRegion(test.raw); got != test.valid {
				t.Fatalf("validEvidenceRegion(%s) = %t, want %t", test.raw, got, test.valid)
			}
		})
	}
}

func TestInvoiceTaxAndItemPathFailures(t *testing.T) {
	t.Parallel()

	envelope := ClaimEnvelope{
		SchemaVersion: "document-claim/3", DocumentType: "invoice",
		Fields: []FieldCandidate{
			present("invoice_number", "string", "INV-2"), present("invoice_date", "date", "2026-08-27"),
			present("total_minor", "money_minor", int64(100)), present("tax_minor", "money_minor", int64(101)),
			present("currency", "string", "CNY"), present("seller_name", "string", "Seller"), present("buyer_name", "string", "Buyer"),
			absent("supplementary_fields", "supplementary"),
		},
	}
	assertValidation(t, ValidateClaim(envelope, 1).Validations, "tax_exceeds_total")

	badOrder := envelope
	badOrder.Fields = append(badOrder.Fields, present("items[1].name", "string", "Item"))
	if _, err := StabilizeItemPaths(badOrder, func() (string, error) { return "id", nil }); err == nil {
		t.Fatal("non-contiguous item order accepted")
	}
	if _, err := StabilizeItemPaths(ClaimEnvelope{Fields: []FieldCandidate{present("items[0].name", "string", "Item")}}, func() (string, error) {
		return "", errors.New("id failure")
	}); err == nil {
		t.Fatal("item ID failure ignored")
	}
}

func validPaymentEnvelope() ClaimEnvelope {
	return ClaimEnvelope{
		SchemaVersion: "document-claim/3", DocumentType: "payment", DocumentIssues: []string{},
		Fields: []FieldCandidate{
			present("amount_minor", "money_minor", int64(1234)), present("currency", "string", "CNY"),
			present("merchant", "string", "ACME"), present("transaction_time", "instant", "2026-08-27T08:30:00Z"),
			present("source_timezone", "string", "UTC"), absent("payment_method", "string"),
			absent("order_number", "string"), absent("category", "string"),
			absent("supplementary_fields", "supplementary"),
		},
	}
}

func validTripEnvelope() ClaimEnvelope {
	return ClaimEnvelope{
		SchemaVersion: "document-claim/3", DocumentType: "trip", DocumentIssues: []string{},
		Fields: []FieldCandidate{
			presentWithPage("origin", "string", "上海", 1),
			presentWithPage("destination", "string", "北京", 2),
			presentWithPage("start_date", "date", "2026-08-26", 1),
			presentWithPage("end_date", "date", "2026-08-28", 2),
			absent("traveler_name", "string"), absent("transport_type", "string"),
			absent("booking_reference", "string"), absent("supplementary_fields", "supplementary"),
		},
	}
}

func present(path, valueType string, value any) FieldCandidate {
	return presentWithPage(path, valueType, value, 1)
}

func presentWithPage(path, valueType string, value any, page int) FieldCandidate {
	encoded, _ := json.Marshal(value)
	return FieldCandidate{
		Path:      path,
		ValueType: valueType,
		Presence:  "present",
		Value:     encoded,
		Evidence:  []CandidateEvidence{{Page: page, Quote: "synthetic evidence"}},
		Issues:    []string{},
	}
}

func absent(path, valueType string) FieldCandidate {
	return FieldCandidate{Path: path, ValueType: valueType, Presence: "absent", Issues: []string{}}
}

func assertValidation(t *testing.T, validations []ClaimValidation, code string) {
	t.Helper()
	for _, validation := range validations {
		if validation.RuleCode == code {
			return
		}
	}
	t.Fatalf("validation %q not found in %#v", code, validations)
}
