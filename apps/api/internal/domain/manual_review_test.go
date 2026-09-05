package domain

import (
	"strings"
	"testing"
)

func TestEmptyManualClaim(t *testing.T) {
	for _, kind := range []DocumentType{DocumentPayment, DocumentInvoice, DocumentTrip} {
		claim, err := EmptyManualClaim(kind, 1)
		if err != nil || claim.Status != ClaimBlocked {
			t.Fatalf("%s: %v / %s", kind, err, claim.Status)
		}
		for _, field := range claim.Fields {
			if field.Path != "document_type" && (field.Presence != "absent" || len(field.Value) != 0 || len(field.Evidence) != 0) {
				t.Fatalf("invented %s", field.Path)
			}
		}
		for _, validation := range claim.Validations {
			if validation.RuleCode == "incomplete_claim_snapshot" {
				t.Fatalf("missing field: %s", validation.FieldPath)
			}
		}
	}
	for _, kind := range []DocumentType{DocumentUnknown, "invalid"} {
		if _, err := EmptyManualClaim(kind, 1); err == nil {
			t.Fatal("invalid type accepted")
		}
	}
}

func TestManualReviewIdentity(t *testing.T) {
	reason, first, err := ManualReviewIdentity("job", 2, DocumentPayment, " 人工核对 ")
	if err != nil || reason != "人工核对" || len(first) != 64 {
		t.Fatal("invalid canonical identity")
	}
	_, same, _ := ManualReviewIdentity("job", 2, DocumentPayment, reason)
	_, changed, _ := ManualReviewIdentity("job", 3, DocumentPayment, reason)
	if first != same || first == changed {
		t.Fatal("unstable identity")
	}
	if _, _, err := ManualReviewIdentity("job", 0, DocumentPayment, reason); err == nil {
		t.Fatal("invalid version accepted")
	}
	if _, _, err := ManualReviewIdentity("job", 2, DocumentPayment, " "); err == nil {
		t.Fatal("empty reason accepted")
	}
}

func TestManualEvidenceInput(t *testing.T) {
	for _, input := range []ManualEvidenceInput{{0, "原文"}, {2, "原文"}, {1, " "}, {1, strings.Repeat("字", 501)}} {
		if input.Validate(1) == nil {
			t.Fatal("invalid annotation accepted")
		}
	}
	if err := (ManualEvidenceInput{1, strings.Repeat("字", 500)}).Validate(1); err != nil {
		t.Fatal(err)
	}
}
