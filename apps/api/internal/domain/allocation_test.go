package domain

import (
	"errors"
	"testing"
)

func TestCanonicalAllocationPlan(t *testing.T) {
	plan, hash, err := CanonicalAllocationPlan([]AllocationRequest{
		{CandidateID: "candidate-b", AllocatedMinor: 200},
		{CandidateID: "candidate-a", AllocatedMinor: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0].CandidateID != "candidate-a" || plan[1].CandidateID != "candidate-b" {
		t.Fatalf("canonical plan = %#v", plan)
	}
	if len(hash) != 64 {
		t.Fatalf("plan hash = %q", hash)
	}
	reordered, reorderedHash, err := CanonicalAllocationPlan([]AllocationRequest{
		{CandidateID: "candidate-a", AllocatedMinor: 100},
		{CandidateID: "candidate-b", AllocatedMinor: 200},
	})
	if err != nil || reorderedHash != hash || len(reordered) != 2 {
		t.Fatalf("reordered hash = %q, error = %v", reorderedHash, err)
	}
	changed, changedHash, err := CanonicalAllocationPlan([]AllocationRequest{
		{CandidateID: "candidate-a", AllocatedMinor: 101},
		{CandidateID: "candidate-b", AllocatedMinor: 200},
	})
	if err != nil || changedHash == hash || len(changed) != 2 {
		t.Fatalf("changed hash = %q, error = %v", changedHash, err)
	}
}

func TestCanonicalAllocationPlanRejectsInvalidItems(t *testing.T) {
	tests := [][]AllocationRequest{
		{{CandidateID: "", AllocatedMinor: 1}},
		{{CandidateID: "candidate", AllocatedMinor: 0}},
		{{CandidateID: "candidate", AllocatedMinor: -1}},
		{{CandidateID: "candidate", AllocatedMinor: MaxSafeMinorUnits + 1}},
		{{CandidateID: "candidate", AllocatedMinor: 1}, {CandidateID: "candidate", AllocatedMinor: 2}},
	}
	for _, input := range tests {
		if _, _, err := CanonicalAllocationPlan(input); err == nil {
			t.Fatalf("invalid plan accepted: %#v", input)
		}
	}
}

func TestValidateAllocationPlan(t *testing.T) {
	candidates := []AllocationCandidate{
		{ID: "invoice-a", Currency: "CNY", RemainingMinor: 400, Available: true},
		{ID: "invoice-b", Currency: "CNY", RemainingMinor: 600, Available: true},
	}
	plan := []AllocationRequest{
		{CandidateID: "invoice-a", AllocatedMinor: 300},
		{CandidateID: "invoice-b", AllocatedMinor: 500},
	}
	if err := ValidateAllocationPlan(1_000, "CNY", candidates, plan); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		amount     int64
		currency   string
		candidates []AllocationCandidate
		plan       []AllocationRequest
		cause      error
	}{
		{name: "foreign", amount: 1_000, currency: "CNY", candidates: candidates, plan: []AllocationRequest{{CandidateID: "foreign", AllocatedMinor: 1}}, cause: ErrConflict},
		{name: "unavailable", amount: 1_000, currency: "CNY", candidates: []AllocationCandidate{{ID: "invoice-a", Currency: "CNY", RemainingMinor: 0}}, plan: []AllocationRequest{{CandidateID: "invoice-a", AllocatedMinor: 1}}, cause: ErrConflict},
		{name: "currency", amount: 1_000, currency: "USD", candidates: candidates, plan: []AllocationRequest{{CandidateID: "invoice-a", AllocatedMinor: 1}}, cause: ErrConflict},
		{name: "target", amount: 1_000, currency: "CNY", candidates: candidates, plan: []AllocationRequest{{CandidateID: "invoice-a", AllocatedMinor: 401}}, cause: ErrConflict},
		{name: "fact", amount: 700, currency: "CNY", candidates: candidates, plan: plan, cause: ErrConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateAllocationPlan(test.amount, test.currency, test.candidates, test.plan); !errors.Is(err, test.cause) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
