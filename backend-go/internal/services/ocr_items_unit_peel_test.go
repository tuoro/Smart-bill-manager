package services

import "testing"

func TestExtractInvoiceLineItemsFromPDFZones_PeelsUnitAndSpecFromNameTail(t *testing.T) {
	pages := []PDFTextZonesPage{
		{
			Page:   1,
			Width:  1000,
			Height: 1000,
			Rows: []PDFTextZonesRow{
				{
					Region: "items",
					Y0:     400,
					Y1:     430,
					Spans: []PDFTextZonesSpan{
						// Simulate a mis-splitting where spec+unit tokens land in the name column.
						{X0: 80, Y0: 400, X1: 360, Y1: 430, T: "*餐饮服务*餐饮服务"},
						{X0: 380, Y0: 400, X1: 420, Y1: 430, T: "项"},
						{X0: 430, Y0: 400, X1: 470, Y1: 430, T: "项"},
						{X0: 760, Y0: 400, X1: 780, Y1: 430, T: "1"},
					},
				},
			},
		},
	}

	items := extractInvoiceLineItemsFromPDFZones(pages)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d: %+v", len(items), items)
	}
	it := items[0]
	if it.Spec != "项" || it.Unit != "项" || it.Quantity == nil || *it.Quantity != 1 {
		t.Fatalf("unexpected item parsed: %+v", it)
	}
	if it.Name == "" || it.Name == "*餐饮服务*餐饮服务项项" || it.Name == "*餐饮服务*餐饮服务 项 项" {
		t.Fatalf("expected unit/spec peeled from name, got: %q", it.Name)
	}
}

