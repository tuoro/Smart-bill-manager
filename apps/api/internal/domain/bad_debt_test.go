package domain

import (
	"strings"
	"testing"
)

func TestBadDebtCanonicalInputAndImmutableRequestIdentity(t *testing.T) {
	input := BadDebtInput{Marked: true, ExpectedVersion: 1, Reason: "  合成原因  "}
	canonical, hash, err := CanonicalBadDebtRequest(DocumentPayment, "synthetic", input)
	if err != nil || canonical.Reason != "合成原因" || input.Reason == canonical.Reason || !ValidSHA256Hex(hash) {
		t.Fatal("invalid canonical bad debt input")
	}
	_, other, err := CanonicalBadDebtRequest(DocumentInvoice, "synthetic", input)
	if err != nil || other == hash {
		t.Fatal("request identity omitted fact type")
	}
	for _, kind := range []DocumentType{DocumentTrip, "unknown"} {
		if _, _, err := CanonicalBadDebtRequest(kind, "id", input); err == nil {
			t.Fatal("invalid type")
		}
	}
	for _, reason := range []string{"", " ", strings.Repeat("字", 501)} {
		bad := input
		bad.Reason = reason
		if _, _, err := CanonicalBadDebtRequest(DocumentPayment, "id", bad); err == nil {
			t.Fatal("invalid reason")
		}
	}
	input.ExpectedVersion = 0
	if _, _, err := CanonicalBadDebtRequest(DocumentPayment, "id", input); err == nil {
		t.Fatal("invalid version")
	}
}
