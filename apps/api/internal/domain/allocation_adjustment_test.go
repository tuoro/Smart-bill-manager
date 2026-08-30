package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalActiveAllocationPlanIsStableAndAnchorScoped(t *testing.T) {
	t.Parallel()
	links := []ActiveAllocationLink{
		{ID: "link-b", PaymentID: "payment", InvoiceID: "invoice-b", AllocatedMinor: 200, Currency: "CNY"},
		{ID: "link-a", PaymentID: "payment", InvoiceID: "invoice-a", AllocatedMinor: 100, Currency: "CNY"},
	}
	canonical, first, err := CanonicalActiveAllocationPlan(DocumentPayment, "payment", links)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := CanonicalActiveAllocationPlan(DocumentPayment, "payment", []ActiveAllocationLink{links[1], links[0]})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(canonical) != 2 || canonical[0].ID != "link-a" || !ValidSHA256Hex(first) {
		t.Fatalf("canonical plan = %#v, hashes %q %q", canonical, first, second)
	}
	changed := append([]ActiveAllocationLink(nil), links...)
	changed[0].AllocatedMinor++
	_, changedHash, err := CanonicalActiveAllocationPlan(DocumentPayment, "payment", changed)
	if err != nil || changedHash == first {
		t.Fatalf("changed hash = %q, error = %v", changedHash, err)
	}
	if _, _, err := CanonicalActiveAllocationPlan(DocumentPayment, "other", links); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("foreign anchor error = %v", err)
	}
}

func TestCanonicalActiveAllocationPlanRejectsInvalidAndDuplicateLinks(t *testing.T) {
	t.Parallel()
	valid := ActiveAllocationLink{ID: "link", PaymentID: "payment", InvoiceID: "invoice", AllocatedMinor: 1, Currency: "CNY"}
	tests := [][]ActiveAllocationLink{
		{{ID: "", PaymentID: "payment", InvoiceID: "invoice", AllocatedMinor: 1, Currency: "CNY"}},
		{{ID: "link", PaymentID: "payment", InvoiceID: "invoice", AllocatedMinor: 0, Currency: "CNY"}},
		{{ID: "link", PaymentID: "payment", InvoiceID: "invoice", AllocatedMinor: 1, Currency: "GBP"}},
		{valid, valid},
		{valid, {ID: "other", PaymentID: "payment", InvoiceID: "invoice", AllocatedMinor: 2, Currency: "CNY"}},
	}
	for index, items := range tests {
		if _, _, err := CanonicalActiveAllocationPlan(DocumentPayment, "payment", items); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d error = %v", index, err)
		}
	}
}

func TestCanonicalAllocationAdjustmentRequestNormalizesCompletePlan(t *testing.T) {
	t.Parallel()
	_, emptyHash, err := CanonicalActiveAllocationPlan(DocumentPayment, "payment", nil)
	if err != nil {
		t.Fatal(err)
	}
	items := []DesiredAllocation{
		{TargetFactID: "invoice-b", AllocatedMinor: 200},
		{TargetFactID: "invoice-a", AllocatedMinor: 100},
	}
	canonical, reason, first, err := CanonicalAllocationAdjustmentRequest(
		DocumentPayment, "payment", emptyHash, items, "  人工核对  ",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, second, err := CanonicalAllocationAdjustmentRequest(
		DocumentPayment, "payment", emptyHash, []DesiredAllocation{items[1], items[0]}, "人工核对",
	)
	if err != nil {
		t.Fatal(err)
	}
	if reason != "人工核对" || canonical[0].TargetFactID != "invoice-a" || first != second || !ValidSHA256Hex(first) {
		t.Fatalf("canonical request = %#v, reason %q, hashes %q %q", canonical, reason, first, second)
	}
}

func TestCanonicalAllocationAdjustmentRequestRejectsBoundaryErrors(t *testing.T) {
	t.Parallel()
	_, emptyHash, err := CanonicalActiveAllocationPlan(DocumentInvoice, "invoice", nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		hash    string
		items   []DesiredAllocation
		reason  string
		wantErr error
	}{
		{"bad hash", "ABC", nil, "reason", ErrInvalidInput},
		{"missing target", emptyHash, []DesiredAllocation{{AllocatedMinor: 1}}, "reason", ErrInvalidInput},
		{"zero", emptyHash, []DesiredAllocation{{TargetFactID: "payment", AllocatedMinor: 0}}, "reason", ErrInvalidInput},
		{"duplicate", emptyHash, []DesiredAllocation{{TargetFactID: "payment", AllocatedMinor: 1}, {TargetFactID: "payment", AllocatedMinor: 2}}, "reason", ErrInvalidInput},
		{"blank reason", emptyHash, nil, " \n ", ErrInvalidInput},
		{"long reason", emptyHash, nil, strings.Repeat("理", 501), ErrInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, _, _, err := CanonicalAllocationAdjustmentRequest(DocumentInvoice, "invoice", test.hash, test.items, test.reason); !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	tooMany := make([]DesiredAllocation, MaxAllocationTargets+1)
	for index := range tooMany {
		tooMany[index] = DesiredAllocation{TargetFactID: "payment-" + string(rune(index+1)), AllocatedMinor: 1}
	}
	if _, err := CanonicalDesiredAllocations(tooMany); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("target limit error = %v", err)
	}
}

func TestBuildAllocationAdjustmentDiffDerivesModesAndPreservesUnchanged(t *testing.T) {
	t.Parallel()
	current := []ActiveAllocationLink{
		{ID: "link-a", PaymentID: "payment", InvoiceID: "invoice-a", AllocatedMinor: 100, Currency: "CNY"},
		{ID: "link-b", PaymentID: "payment", InvoiceID: "invoice-b", AllocatedMinor: 200, Currency: "CNY"},
	}
	tests := []struct {
		name      string
		desired   []DesiredAllocation
		mode      string
		unchanged int
		end       int
		create    int
	}{
		{"supplement", []DesiredAllocation{{"invoice-a", 100}, {"invoice-b", 200}, {"invoice-c", 300}}, AllocationModeSupplement, 2, 0, 1},
		{"withdraw", []DesiredAllocation{{"invoice-a", 100}}, AllocationModeWithdraw, 1, 1, 0},
		{"replace amount", []DesiredAllocation{{"invoice-a", 150}, {"invoice-b", 200}}, AllocationModeReplace, 1, 1, 1},
		{"replace target", []DesiredAllocation{{"invoice-b", 200}, {"invoice-c", 100}}, AllocationModeReplace, 1, 1, 1},
		{"withdraw all", []DesiredAllocation{}, AllocationModeWithdraw, 0, 2, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			diff, err := BuildAllocationAdjustmentDiff(DocumentPayment, "payment", current, test.desired)
			if err != nil {
				t.Fatal(err)
			}
			if diff.Mode != test.mode || len(diff.Unchanged) != test.unchanged || len(diff.End) != test.end || len(diff.Create) != test.create {
				t.Fatalf("diff = %#v", diff)
			}
		})
	}
	if _, err := BuildAllocationAdjustmentDiff(DocumentPayment, "payment", current, []DesiredAllocation{{"invoice-a", 100}, {"invoice-b", 200}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("unchanged error = %v", err)
	}
}

func TestValidateDesiredAllocationPlanChecksBothSides(t *testing.T) {
	t.Parallel()
	if err := ValidateDesiredAllocationPlan(0, "CNY", nil, nil); err != nil {
		t.Fatalf("zero-value anchor with empty plan: %v", err)
	}
	targets := []AllocationTargetBalance{
		{ID: "invoice-a", Currency: "CNY", MaximumAllocatableMinor: 400, Available: true},
		{ID: "invoice-b", Currency: "CNY", MaximumAllocatableMinor: 600, Available: true},
	}
	if err := ValidateDesiredAllocationPlan(1_000, "CNY", targets, []DesiredAllocation{{"invoice-a", 400}, {"invoice-b", 600}}); err != nil {
		t.Fatal(err)
	}
	tests := [][]DesiredAllocation{
		{{"foreign", 1}},
		{{"invoice-a", 401}},
		{{"invoice-a", 400}, {"invoice-b", 601}},
	}
	for index, desired := range tests {
		if err := ValidateDesiredAllocationPlan(1_000, "CNY", targets, desired); !errors.Is(err, ErrConflict) {
			t.Fatalf("case %d error = %v", index, err)
		}
	}
	if err := ValidateDesiredAllocationPlan(1_000, "USD", targets, []DesiredAllocation{{"invoice-a", 1}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("currency error = %v", err)
	}
	if err := ValidateDesiredAllocationPlan(0, "CNY", targets, []DesiredAllocation{{"invoice-a", 1}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("zero anchor overflow error = %v", err)
	}
}

func TestValidateIdempotencyKeyAndDigest(t *testing.T) {
	t.Parallel()
	if err := ValidateIdempotencyKey("valid-key"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIdempotencyKey(strings.Repeat("键", 8)); err != nil {
		t.Fatalf("eight unicode characters: %v", err)
	}
	for _, value := range []string{"short", strings.Repeat("x", 129), strings.Repeat("键", 129), "has space", "has\nline"} {
		if err := ValidateIdempotencyKey(value); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%q error = %v", value, err)
		}
	}
	if ValidSHA256Hex(strings.Repeat("A", 64)) || ValidSHA256Hex("abc") || !ValidSHA256Hex(strings.Repeat("a", 64)) {
		t.Fatal("digest validation mismatch")
	}
}
