package reviews

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestDecisionInputAndAssociationBoundaries(t *testing.T) {
	for _, input := range []struct {
		key       string
		requestID string
	}{
		{key: "short", requestID: "request"},
		{key: strings.Repeat("a", 129), requestID: "request"},
		{key: "contains space", requestID: "request"},
		{key: "valid-key", requestID: ""},
	} {
		if err := validateDecisionInput(input.key, input.requestID); err == nil {
			t.Fatalf("invalid decision input accepted: %#v", input)
		}
	}
	if err := validateDecisionInput("valid-key", "request"); err != nil {
		t.Fatalf("valid decision input rejected: %v", err)
	}

	candidates := []ports.LinkCandidate{
		{ID: "candidate-a", Currency: "CNY", RemainingMinor: 400, Available: true},
		{ID: "candidate-b", Currency: "CNY", RemainingMinor: 600, Available: true},
	}
	valid := []struct {
		items       []ports.LinkCandidate
		mode        string
		allocations []domain.AllocationRequest
	}{
		{items: nil, mode: AssociationNoCandidate},
		{items: candidates, mode: AssociationRejectAll},
		{items: candidates, mode: AssociationAllocateCandidates, allocations: []domain.AllocationRequest{{CandidateID: "candidate-b", AllocatedMinor: 500}}},
		{items: candidates, mode: AssociationAllocateCandidates, allocations: []domain.AllocationRequest{{CandidateID: "candidate-a", AllocatedMinor: 300}, {CandidateID: "candidate-b", AllocatedMinor: 500}}},
	}
	for _, input := range valid {
		if err := validateAssociation(input.items, input.mode, input.allocations, 1_000, "CNY"); err != nil {
			t.Fatalf("valid association rejected: %#v, error=%v", input, err)
		}
	}
	invalid := []struct {
		items       []ports.LinkCandidate
		mode        string
		allocations []domain.AllocationRequest
	}{
		{items: nil, mode: AssociationAllocateCandidates, allocations: []domain.AllocationRequest{{CandidateID: "candidate-a", AllocatedMinor: 1}}},
		{items: candidates, mode: AssociationNoCandidate},
		{items: candidates, mode: AssociationAllocateCandidates},
		{items: candidates, mode: AssociationAllocateCandidates, allocations: []domain.AllocationRequest{{CandidateID: "foreign", AllocatedMinor: 1}}},
		{items: candidates, mode: AssociationRejectAll, allocations: []domain.AllocationRequest{{CandidateID: "candidate-a", AllocatedMinor: 1}}},
		{items: candidates, mode: AssociationAllocateCandidates, allocations: []domain.AllocationRequest{{CandidateID: "candidate-a", AllocatedMinor: 401}}},
		{items: candidates, mode: AssociationAllocateCandidates, allocations: []domain.AllocationRequest{{CandidateID: "candidate-a", AllocatedMinor: 400}, {CandidateID: "candidate-b", AllocatedMinor: 601}}},
	}
	for _, input := range invalid {
		if err := validateAssociation(input.items, input.mode, input.allocations, 1_000, "CNY"); err == nil {
			t.Fatalf("invalid association accepted: %#v", input)
		}
	}
	if _, hash, err := normalizeAssociationRequest(AssociationAllocateCandidates, []domain.AllocationRequest{{CandidateID: "candidate-a", AllocatedMinor: 1}}); err != nil || len(hash) != 64 {
		t.Fatalf("allocation request hash = %q, error = %v", hash, err)
	}
	if _, _, err := normalizeAssociationRequest(AssociationRejectAll, []domain.AllocationRequest{{CandidateID: "candidate-a", AllocatedMinor: 1}}); err == nil {
		t.Fatal("reject_all accepted allocations")
	}
}

func TestValidatedFieldDecodingAndStableItemPaths(t *testing.T) {
	fields := map[string]ports.ReviewField{
		"name":   {Value: json.RawMessage(`"merchant"`)},
		"amount": {Value: json.RawMessage(`9223372036854775807`)},
		"bad":    {Value: json.RawMessage(`1.5`)},
	}
	if value, err := fieldString(fields, "name"); err != nil || value != "merchant" {
		t.Fatalf("string field = %q, error=%v", value, err)
	}
	if value, err := fieldInt(fields, "amount"); err != nil || value != int64(9223372036854775807) {
		t.Fatalf("integer field = %d, error=%v", value, err)
	}
	if _, err := fieldString(fields, "missing"); err == nil {
		t.Fatal("missing string field accepted")
	}
	if _, err := fieldInt(fields, "bad"); err == nil {
		t.Fatal("non-integer field accepted")
	}
	if optionalFieldString(fields, "missing") != nil || optionalFieldInt(fields, "missing") != nil {
		t.Fatal("missing optional fields produced values")
	}
	if value := optionalFieldString(fields, "name"); value == nil || *value != "merchant" {
		t.Fatalf("optional string = %#v", value)
	}

	key := "00000000-0000-0000-0000-000000000001"
	if itemKey, property, ok := splitItemPath("items[" + key + "].amount_minor"); !ok || itemKey != key || property != "amount_minor" {
		t.Fatalf("stable item path = %q/%q/%v", itemKey, property, ok)
	}
	for _, path := range []string{"amount_minor", "items[].name", "items[bad key].name", "items[key]."} {
		if _, _, ok := splitItemPath(path); ok {
			t.Fatalf("invalid item path accepted: %s", path)
		}
	}
}

func TestCanonicalJSONRejectsTrailingValues(t *testing.T) {
	canonical, err := canonicalJSON(json.RawMessage(" { \"b\": 2, \"a\": 1 } "))
	if err != nil || string(canonical) != `{"a":1,"b":2}` {
		t.Fatalf("canonical JSON = %s, error=%v", canonical, err)
	}
	if _, err := canonicalJSON(json.RawMessage(`{"a":1} {"b":2}`)); err == nil {
		t.Fatal("multiple JSON values accepted")
	}
	if _, err := canonicalJSON(json.RawMessage(`{"a":`)); err == nil {
		t.Fatal("truncated JSON accepted")
	}
	if value, err := canonicalJSON(nil); err != nil || value != nil {
		t.Fatalf("empty JSON = %s, error=%v", value, err)
	}
	if !jsonEqual(json.RawMessage(`{"a":1,"b":2}`), json.RawMessage(`{"b":2,"a":1}`)) {
		t.Fatal("semantically equal objects compared unequal")
	}
	if jsonEqual(json.RawMessage(`1`), json.RawMessage(`invalid`)) {
		t.Fatal("invalid JSON compared equal")
	}
}

func TestSupplementaryFieldsNeverBecomeFactOrigins(t *testing.T) {
	service := Service{ids: system.IDGenerator{}}
	origins, err := service.factOrigins([]ports.ReviewField{
		{ID: "field-document-type", Path: "document_type", Presence: "present"},
		{ID: "field-merchant", Path: "merchant", Presence: "present"},
		{ID: "field-supplementary", Path: "supplementary_fields", Presence: "present"},
	}, "payment", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(origins) != 1 || origins[0].FieldPath != "merchant" {
		t.Fatalf("Fact origins included review-only data: %#v", origins)
	}
}

func TestRuleErrorsPreservePublicClassification(t *testing.T) {
	rule := domain.NewRuleError("conflict", "safe message", domain.ErrConflict)
	if rule.Error() != "conflict: safe message" || !errors.Is(rule, domain.ErrConflict) {
		t.Fatalf("rule error = %q, conflict=%v", rule.Error(), errors.Is(rule, domain.ErrConflict))
	}
	duplicate := &domain.DuplicateDocumentError{DocumentID: "document"}
	if duplicate.Error() != "duplicate_document" || !errors.Is(duplicate, domain.ErrConflict) {
		t.Fatalf("duplicate error = %q, conflict=%v", duplicate.Error(), errors.Is(duplicate, domain.ErrConflict))
	}
}
