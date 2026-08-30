package reviews

import (
	"encoding/json"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestWithPagePlanUsesOnlyCurrentFieldEvidence(t *testing.T) {
	itemKey := "00000000-0000-4000-8000-000000000221"
	snapshot := ports.ReviewSnapshot{
		DocumentType: domain.DocumentInvoice,
		PageCount:    3,
		Fields: []ports.ReviewField{
			{
				Path: "items[" + itemKey + "].name", ValueType: "string", Presence: "present",
				Value:    json.RawMessage(`"跨页服务"`),
				Evidence: []ports.ReviewEvidence{{Page: 1, Quote: "跨页服务"}},
			},
			{
				Path: "items[" + itemKey + "].amount_minor", ValueType: "money_minor", Presence: "present",
				Value:    json.RawMessage(`100`),
				Evidence: []ports.ReviewEvidence{{Page: 2, Quote: "1.00"}},
			},
			{
				Path: "items[" + itemKey + "].sort_order", ValueType: "integer", Presence: "present",
				Value: json.RawMessage(`0`),
			},
			{
				Path: "items[00000000-0000-4000-8000-000000000222].name", ValueType: "string", Presence: "absent",
			},
		},
	}

	result := withPagePlan(snapshot)
	if len(result.Pages) != 3 || len(result.InvoiceItemSpans) != 1 {
		t.Fatalf("review page plan = %#v", result)
	}
	span := result.InvoiceItemSpans[0]
	if span.ItemKey != itemKey || !span.CrossPage || span.StartPage != 1 || span.EndPage != 2 {
		t.Fatalf("review item span = %#v", span)
	}
	if len(result.Pages[2].FieldPaths) != 0 || len(result.Pages[2].ItemKeys) != 0 {
		t.Fatalf("empty document page was lost: %#v", result.Pages[2])
	}
}
