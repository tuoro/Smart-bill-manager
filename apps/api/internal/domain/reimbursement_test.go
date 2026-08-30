package domain

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestEvaluateReimbursementPolicyProducesDeterministicFindingsAndCurrencyTotals(t *testing.T) {
	t.Parallel()

	input := ReimbursementPolicyInput{
		Trip: ReimbursementTripSnapshot{
			ID: "trip-a", Destination: "合成目的地", StartDate: "2026-08-01", EndDate: "2026-08-03",
		},
		Items: []ReimbursementPolicyItem{
			{AssignmentID: "assignment-payment-b", FactType: DocumentPayment, FactID: "payment-b", DisplayName: "合成商户乙", BusinessDate: "2026-08-02", AmountMinor: 50, Currency: CurrencyUSD},
			{AssignmentID: "assignment-invoice-a", FactType: DocumentInvoice, FactID: "invoice-a", DisplayName: "合成销售方", BusinessDate: "2026-08-01", AmountMinor: 80, Currency: CurrencyCNY},
			{AssignmentID: "assignment-payment-a", FactType: DocumentPayment, FactID: "payment-a", DisplayName: "合成商户甲", BusinessDate: "2026-08-01", AmountMinor: 100, Currency: CurrencyCNY},
		},
		Links: []ReimbursementPolicyLink{
			{ID: "link-a", PaymentID: "payment-a", InvoiceID: "invoice-a", AllocatedMinor: 80, Currency: CurrencyCNY},
		},
		PriorUses: []ReimbursementPriorUse{
			{FactType: DocumentPayment, FactID: "payment-a", ReimbursementID: "reimbursement-old", Status: ReimbursementStatusReimbursed},
		},
	}

	first, err := EvaluateReimbursementPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.RuleVersion != ReimbursementPolicyVersion || !ValidSHA256Hex(first.SnapshotHash) {
		t.Fatalf("policy identity = %q / %q", first.RuleVersion, first.SnapshotHash)
	}
	if len(first.Findings) != 3 {
		t.Fatalf("findings = %#v", first.Findings)
	}
	gotCodes := []string{first.Findings[0].Code, first.Findings[1].Code, first.Findings[2].Code}
	wantCodes := []string{
		ReimbursementFindingAmountConflict,
		ReimbursementFindingDuplicate,
		ReimbursementFindingMissingInvoice,
	}
	if !slices.Equal(gotCodes, wantCodes) {
		t.Fatalf("finding codes = %v, want %v", gotCodes, wantCodes)
	}
	for _, finding := range first.Findings {
		if !ValidSHA256Hex(finding.FindingKey) {
			t.Fatalf("invalid finding key %q", finding.FindingKey)
		}
	}
	if got, want := first.Totals, []ReimbursementCurrencyTotal{
		{Currency: CurrencyCNY, AmountMinor: 180},
		{Currency: CurrencyUSD, AmountMinor: 50},
	}; !slices.Equal(got, want) {
		t.Fatalf("totals = %#v, want %#v", got, want)
	}

	reversed := input
	reversed.Items = []ReimbursementPolicyItem{input.Items[2], input.Items[1], input.Items[0]}
	reversed.PriorUses = slices.Clone(input.PriorUses)
	second, err := EvaluateReimbursementPolicy(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if second.SnapshotHash != first.SnapshotHash || !reflect.DeepEqual(second.Findings, first.Findings) {
		t.Fatalf("policy output changed after input reordering: %#v / %#v", first, second)
	}

	replacedLink := input
	replacedLink.Links = []ReimbursementPolicyLink{{
		ID: "link-replacement", PaymentID: "payment-a", InvoiceID: "invoice-a",
		AllocatedMinor: 80, Currency: CurrencyCNY,
	}}
	replaced, err := EvaluateReimbursementPolicy(replacedLink)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Findings[0].Code != ReimbursementFindingAmountConflict ||
		replaced.Findings[0].FindingKey == first.Findings[0].FindingKey {
		t.Fatalf("equal-amount Link replacement did not change amount finding identity: %#v / %#v", first.Findings[0], replaced.Findings[0])
	}
}

func TestEvaluateReimbursementPolicyInvoiceOnlyAndFullyCoveredHaveNoFindings(t *testing.T) {
	t.Parallel()

	trip := ReimbursementTripSnapshot{ID: "trip", Destination: "合成目的地", StartDate: "2026-08-01", EndDate: "2026-08-02"}
	invoiceOnly, err := EvaluateReimbursementPolicy(ReimbursementPolicyInput{
		Trip: trip,
		Items: []ReimbursementPolicyItem{{
			AssignmentID: "assignment-invoice", FactType: DocumentInvoice, FactID: "invoice",
			DisplayName: "合成销售方", BusinessDate: "2026-08-01", AmountMinor: 100, Currency: CurrencyCNY,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(invoiceOnly.Findings) != 0 {
		t.Fatalf("invoice-only findings = %#v", invoiceOnly.Findings)
	}

	covered, err := EvaluateReimbursementPolicy(ReimbursementPolicyInput{
		Trip: trip,
		Items: []ReimbursementPolicyItem{
			{AssignmentID: "assignment-payment", FactType: DocumentPayment, FactID: "payment", DisplayName: "合成商户", BusinessDate: "2026-08-01", AmountMinor: 100, Currency: CurrencyCNY},
			{AssignmentID: "assignment-invoice", FactType: DocumentInvoice, FactID: "invoice", DisplayName: "合成销售方", BusinessDate: "2026-08-01", AmountMinor: 100, Currency: CurrencyCNY},
		},
		Links: []ReimbursementPolicyLink{{ID: "link", PaymentID: "payment", InvoiceID: "invoice", AllocatedMinor: 100, Currency: CurrencyCNY}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(covered.Findings) != 0 {
		t.Fatalf("covered findings = %#v", covered.Findings)
	}
}

func TestEvaluateReimbursementPolicyRejectsInvalidAndOverflowInputs(t *testing.T) {
	t.Parallel()

	trip := ReimbursementTripSnapshot{ID: "trip", Destination: "合成目的地", StartDate: "2026-08-01", EndDate: "2026-08-02"}
	base := ReimbursementPolicyItem{
		AssignmentID: "assignment", FactType: DocumentPayment, FactID: "payment",
		DisplayName: "合成商户", BusinessDate: "2026-08-01", AmountMinor: 100, Currency: CurrencyCNY,
	}
	for name, input := range map[string]ReimbursementPolicyInput{
		"empty items":          {Trip: trip},
		"duplicate assignment": {Trip: trip, Items: []ReimbursementPolicyItem{base, base}},
		"invalid date": {Trip: trip, Items: []ReimbursementPolicyItem{{
			AssignmentID: "assignment", FactType: DocumentPayment, FactID: "payment",
			DisplayName: "合成商户", BusinessDate: "bad", AmountMinor: 100, Currency: CurrencyCNY,
		}}},
		"link outside selection": {Trip: trip, Items: []ReimbursementPolicyItem{base}, Links: []ReimbursementPolicyLink{{
			ID: "link", PaymentID: "payment", InvoiceID: "invoice", AllocatedMinor: 100, Currency: CurrencyCNY,
		}}},
		"rejected prior use": {Trip: trip, Items: []ReimbursementPolicyItem{base}, PriorUses: []ReimbursementPriorUse{{
			FactType: DocumentPayment, FactID: "payment", ReimbursementID: "old", Status: ReimbursementStatusRejected,
		}}},
	} {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := EvaluateReimbursementPolicy(input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v", err)
			}
		})
	}

	overflowItem := base
	overflowItem.AmountMinor = MaxSafeMinorUnits
	other := base
	other.AssignmentID = "assignment-other"
	other.FactID = "payment-other"
	other.AmountMinor = 1
	if _, err := EvaluateReimbursementPolicy(ReimbursementPolicyInput{Trip: trip, Items: []ReimbursementPolicyItem{overflowItem, other}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestCanonicalReimbursementSelectionAndRequests(t *testing.T) {
	t.Parallel()

	selection, err := CanonicalReimbursementSelection([]string{"assignment-b", "assignment-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(selection, []string{"assignment-a", "assignment-b"}) {
		t.Fatalf("selection = %v", selection)
	}
	for _, input := range [][]string{nil, {""}, {"duplicate", "duplicate"}, {" padded "}} {
		if _, err := CanonicalReimbursementSelection(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("selection %q error = %v", input, err)
		}
	}
	tooMany := make([]string, MaxReimbursementItems+1)
	for index := range tooMany {
		tooMany[index] = strings.Repeat("a", index+1)
	}
	if _, err := CanonicalReimbursementSelection(tooMany); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("too many error = %v", err)
	}

	snapshotHash := strings.Repeat("a", 64)
	findingA := strings.Repeat("b", 64)
	findingB := strings.Repeat("c", 64)
	canonicalSelection, acknowledgements, reason, firstHash, err := CanonicalReimbursementSubmissionRequest(
		"trip", []string{"b", "a"}, snapshotHash, []string{findingB, findingA}, "  合成提交理由  ",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, secondHash, err := CanonicalReimbursementSubmissionRequest(
		"trip", []string{"a", "b"}, snapshotHash, []string{findingA, findingB}, "合成提交理由",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(canonicalSelection, []string{"a", "b"}) ||
		!slices.Equal(acknowledgements, []string{findingA, findingB}) ||
		reason != "合成提交理由" || firstHash != secondHash || !ValidSHA256Hex(firstHash) {
		t.Fatalf("canonical request = %v / %v / %q / %q / %q", canonicalSelection, acknowledgements, reason, firstHash, secondHash)
	}
}

func TestReimbursementStatusTransitionAndRequestIdentity(t *testing.T) {
	t.Parallel()

	valid := []struct {
		current ReimbursementStatus
		desired ReimbursementStatus
		action  string
	}{
		{ReimbursementStatusSubmitted, ReimbursementStatusReimbursed, "mark_reimbursed"},
		{ReimbursementStatusSubmitted, ReimbursementStatusRejected, "reject"},
		{ReimbursementStatusReimbursed, ReimbursementStatusSubmitted, "reopen"},
		{ReimbursementStatusRejected, ReimbursementStatusSubmitted, "reopen"},
	}
	for _, scenario := range valid {
		action, err := ReimbursementTransitionAction(scenario.current, scenario.desired)
		if err != nil || action != scenario.action {
			t.Fatalf("transition %s -> %s = %q, %v", scenario.current, scenario.desired, action, err)
		}
	}
	for _, scenario := range [][2]ReimbursementStatus{
		{ReimbursementStatusSubmitted, ReimbursementStatusSubmitted},
		{ReimbursementStatusReimbursed, ReimbursementStatusRejected},
		{ReimbursementStatusRejected, ReimbursementStatusReimbursed},
		{"unknown", ReimbursementStatusSubmitted},
	} {
		if _, err := ReimbursementTransitionAction(scenario[0], scenario[1]); err == nil {
			t.Fatalf("transition %s -> %s unexpectedly succeeded", scenario[0], scenario[1])
		}
	}
	action, reason, requestHash, err := CanonicalReimbursementStatusRequest(
		"reimbursement", ReimbursementStatusSubmitted, ReimbursementStatusReimbursed, 1, "  合成完成理由 ",
	)
	if err != nil || action != "mark_reimbursed" || reason != "合成完成理由" || !ValidSHA256Hex(requestHash) {
		t.Fatalf("status request = %q / %q / %q / %v", action, reason, requestHash, err)
	}
}

func TestReimbursementFindingAndRequestLimitsAreExplicit(t *testing.T) {
	t.Parallel()

	trip := ReimbursementTripSnapshot{
		ID: "trip", Destination: "合成目的地", StartDate: "2026-08-01", EndDate: "2026-08-02",
	}
	item := ReimbursementPolicyItem{
		AssignmentID: "assignment", FactType: DocumentInvoice, FactID: "invoice",
		DisplayName: "合成销售方", BusinessDate: "2026-08-01", AmountMinor: 100, Currency: CurrencyCNY,
	}
	priorUses := make([]ReimbursementPriorUse, MaxReimbursementFindings)
	for index := range priorUses {
		priorUses[index] = ReimbursementPriorUse{
			FactType: DocumentInvoice, FactID: item.FactID,
			ReimbursementID: "reimbursement-" + strings.Repeat("x", index) + "-end",
			Status:          ReimbursementStatusReimbursed,
		}
	}
	boundary, err := EvaluateReimbursementPolicy(ReimbursementPolicyInput{
		Trip: trip, Items: []ReimbursementPolicyItem{item}, PriorUses: priorUses,
	})
	if err != nil || len(boundary.Findings) != MaxReimbursementFindings {
		t.Fatalf("finding boundary = %d, err = %v", len(boundary.Findings), err)
	}
	priorUses = append(priorUses, ReimbursementPriorUse{
		FactType: DocumentInvoice, FactID: item.FactID,
		ReimbursementID: "reimbursement-over-limit", Status: ReimbursementStatusSubmitted,
	})
	if _, err := EvaluateReimbursementPolicy(ReimbursementPolicyInput{
		Trip: trip, Items: []ReimbursementPolicyItem{item}, PriorUses: priorUses,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("finding overflow error = %v", err)
	}

	validHash := strings.Repeat("a", 64)
	validFinding := strings.Repeat("b", 64)
	for name, acknowledgements := range map[string][]string{
		"invalid":   {"not-a-hash"},
		"duplicate": {validFinding, validFinding},
	} {
		if _, _, _, _, err := CanonicalReimbursementSubmissionRequest(
			"trip", []string{"assignment"}, validHash, acknowledgements, "合成理由",
		); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s acknowledgement error = %v", name, err)
		}
	}
	tooManyAcknowledgements := make([]string, MaxReimbursementFindings+1)
	for index := range tooManyAcknowledgements {
		tooManyAcknowledgements[index] = strings.Repeat("a", 63) + string(rune('a'+index%6))
	}
	if _, _, _, _, err := CanonicalReimbursementSubmissionRequest(
		"trip", []string{"assignment"}, validHash, tooManyAcknowledgements, "合成理由",
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("acknowledgement overflow error = %v", err)
	}
	for name, reason := range map[string]string{
		"empty":    "   ",
		"too long": strings.Repeat("理", 501),
	} {
		if _, _, _, _, err := CanonicalReimbursementSubmissionRequest(
			"trip", []string{"assignment"}, validHash, []string{}, reason,
		); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s reason error = %v", name, err)
		}
	}
}
