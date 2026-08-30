package domain

import (
	"encoding/json"
	"testing"
)

const (
	pagePlanItemA = "00000000-0000-4000-8000-000000000201"
	pagePlanItemB = "00000000-0000-4000-8000-000000000202"
)

func TestClaimPagePlanReconstructsAdjacentCrossPageItem(t *testing.T) {
	fields := []FieldCandidate{
		presentWithPage("invoice_number", "string", "INV-1", 1),
		presentWithPage("items["+pagePlanItemA+"].name", "string", "跨页服务", 1),
		presentWithPage("items["+pagePlanItemA+"].amount_minor", "money_minor", int64(100), 2),
		present("items["+pagePlanItemA+"].sort_order", "integer", int64(0)),
		presentWithPage("items["+pagePlanItemB+"].name", "string", "第二项", 2),
		presentWithPage("items["+pagePlanItemB+"].amount_minor", "money_minor", int64(200), 2),
		present("items["+pagePlanItemB+"].sort_order", "integer", int64(1)),
	}
	plan := BuildClaimPagePlan(DocumentInvoice, fields, 3)
	if len(plan.Pages) != 3 || len(plan.InvoiceItemSpans) != 2 {
		t.Fatalf("page plan = %#v", plan)
	}
	first := plan.InvoiceItemSpans[0]
	if first.ItemKey != pagePlanItemA || !first.CrossPage || first.StartPage != 1 || first.EndPage != 2 || len(first.PageNumbers) != 2 {
		t.Fatalf("cross-page span = %#v", first)
	}
	if len(plan.Pages[0].ItemKeys) != 1 || plan.Pages[0].ItemKeys[0] != pagePlanItemA || len(plan.Pages[1].ItemKeys) != 2 {
		t.Fatalf("page item projection = %#v", plan.Pages)
	}
	if len(plan.Pages[2].FieldPaths) != 0 || len(plan.Pages[2].ItemKeys) != 0 {
		t.Fatalf("empty page was not preserved: %#v", plan.Pages[2])
	}
}

func TestClaimPagePlanPreservesFullTwentyPageSequence(t *testing.T) {
	fields := []FieldCandidate{
		presentWithPage("items["+pagePlanItemA+"].name", "string", "末页服务", 20),
		presentWithPage("items["+pagePlanItemA+"].amount_minor", "money_minor", int64(100), 20),
		{
			Path: "items[" + pagePlanItemA + "].sort_order", ValueType: "integer",
			Presence: "present", Value: mustRaw(int64(0)), Issues: []string{},
		},
	}

	plan := BuildClaimPagePlan(DocumentInvoice, fields, 20)
	if len(plan.Pages) != 20 || plan.Pages[0].PageNumber != 1 || plan.Pages[19].PageNumber != 20 {
		t.Fatalf("twenty-page plan = %#v", plan.Pages)
	}
	if len(plan.Pages[0].FieldPaths) != 0 || len(plan.Pages[19].ItemKeys) != 1 || plan.Pages[19].ItemKeys[0] != pagePlanItemA {
		t.Fatalf("twenty-page projection = %#v", plan.Pages)
	}
}

func TestValidateInvoicePageTopologyBlocksGapRollbackAndSortViolations(t *testing.T) {
	tests := []struct {
		name   string
		fields []FieldCandidate
		pages  int
		code   string
	}{
		{
			name: "page gap", pages: 3, code: "invoice_item_page_gap",
			fields: []FieldCandidate{
				presentWithPage("items["+pagePlanItemA+"].name", "string", "A", 1),
				presentWithPage("items["+pagePlanItemA+"].amount_minor", "money_minor", int64(100), 3),
				present("items["+pagePlanItemA+"].sort_order", "integer", int64(0)),
			},
		},
		{
			name: "page rollback", pages: 3, code: "invoice_item_page_order_conflict",
			fields: []FieldCandidate{
				presentWithPage("items["+pagePlanItemA+"].name", "string", "A", 2),
				presentWithPage("items["+pagePlanItemA+"].amount_minor", "money_minor", int64(100), 3),
				present("items["+pagePlanItemA+"].sort_order", "integer", int64(0)),
				presentWithPage("items["+pagePlanItemB+"].name", "string", "B", 2),
				presentWithPage("items["+pagePlanItemB+"].amount_minor", "money_minor", int64(200), 2),
				present("items["+pagePlanItemB+"].sort_order", "integer", int64(1)),
			},
		},
		{
			name: "duplicate order", pages: 1, code: "invoice_item_sort_order_duplicate",
			fields: []FieldCandidate{
				present("items["+pagePlanItemA+"].name", "string", "A"),
				present("items["+pagePlanItemA+"].amount_minor", "money_minor", int64(100)),
				present("items["+pagePlanItemA+"].sort_order", "integer", int64(0)),
				present("items["+pagePlanItemB+"].name", "string", "B"),
				present("items["+pagePlanItemB+"].amount_minor", "money_minor", int64(200)),
				present("items["+pagePlanItemB+"].sort_order", "integer", int64(0)),
			},
		},
		{
			name: "order gap", pages: 1, code: "invoice_item_sort_order_gap",
			fields: []FieldCandidate{
				present("items["+pagePlanItemA+"].name", "string", "A"),
				present("items["+pagePlanItemA+"].amount_minor", "money_minor", int64(100)),
				present("items["+pagePlanItemA+"].sort_order", "integer", int64(1)),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := validInvoiceFieldsForPagePlan()
			base = append(base, test.fields...)
			validated := ValidateClaim(ClaimEnvelope{
				SchemaVersion: "document-claim/2", DocumentType: "invoice", Fields: base,
			}, test.pages)
			if validated.Status != ClaimBlocked {
				t.Fatalf("status = %s", validated.Status)
			}
			assertValidation(t, validated.Validations, test.code)
		})
	}
}

func TestValidateInvoicePageTopologyAllowsIndependentPagesAndThreePageSpan(t *testing.T) {
	fields := validInvoiceFieldsForPagePlan()
	fields = append(fields,
		FieldCandidate{
			Path: "items[" + pagePlanItemA + "].name", ValueType: "string", Presence: "present",
			Value: mustRaw("A"), Evidence: []CandidateEvidence{{Page: 1, Quote: "A"}, {Page: 2, Quote: "A"}, {Page: 3, Quote: "A"}}, Issues: []string{},
		},
		absent("items["+pagePlanItemA+"].quantity", "decimal"),
		absent("items["+pagePlanItemA+"].unit", "string"),
		absent("items["+pagePlanItemA+"].unit_price_minor", "money_minor"),
		presentWithPage("items["+pagePlanItemA+"].amount_minor", "money_minor", int64(300), 3),
		absent("items["+pagePlanItemA+"].tax_minor", "money_minor"),
		present("items["+pagePlanItemA+"].sort_order", "integer", int64(0)),
	)
	validated := ValidateClaim(ClaimEnvelope{SchemaVersion: "document-claim/2", DocumentType: "invoice", Fields: fields}, 3)
	if validated.Status != ClaimReadyForReview {
		t.Fatalf("status = %s, validations = %#v", validated.Status, validated.Validations)
	}
}

func validInvoiceFieldsForPagePlan() []FieldCandidate {
	return []FieldCandidate{
		present("invoice_number", "string", "INV-1"),
		present("invoice_date", "date", "2026-08-30"),
		present("total_minor", "money_minor", int64(300)),
		absent("tax_minor", "money_minor"),
		present("currency", "string", "CNY"),
		present("seller_name", "string", "Seller"),
		present("buyer_name", "string", "Buyer"),
		absent("supplementary_fields", "supplementary"),
	}
}

func mustRaw(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
