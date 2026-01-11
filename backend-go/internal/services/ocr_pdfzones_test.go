package services

import "testing"

func TestExtractSellerNameFromPDFZones_TaxIDContext(t *testing.T) {
	pages := []PDFTextZonesPage{
		{
			Page:   1,
			Width:  1000,
			Height: 1000,
			Rows: []PDFTextZonesRow{
				{
					Region: "password",
					Y0:     260,
					Y1:     290,
					Text:   "\u7edf\u4e00\u793e\u4f1a\u4fe1\u7528\u4ee3\u7801/\u7eb3\u7a0e\u4eba\u8bc6\u522b\u53f7: \u5355\u4ef7 \u4e49\u4e4c\u5e02\u5927\u8fdb\u767e\u8d27\u6709\u9650\u516c\u53f8 1211.50 \u91d1\u989d 913307827450870674 \u7a0e\u7387/\u5f81\u6536\u7387 13% \u7a0e\u989d 157.50",
				},
			},
		},
	}

	got := extractSellerNameFromPDFZones(pages)
	if got != "\u4e49\u4e4c\u5e02\u5927\u8fdb\u767e\u8d27\u6709\u9650\u516c\u53f8" {
		t.Fatalf("expected seller=\u4e49\u4e4c\u5e02\u5927\u8fdb\u767e\u8d27\u6709\u9650\u516c\u53f8 got %q", got)
	}
}

func TestExtractBuyerNameFromPDFZones_FallbackFromBankFieldWhenNameIsGarbage(t *testing.T) {
	pages := []PDFTextZonesPage{
		{
			Page:   1,
			Width:  1000,
			Height: 1000,
			Rows: []PDFTextZonesRow{
				{
					Region: "buyer",
					Y0:     220,
					Y1:     250,
					Spans: []PDFTextZonesSpan{
						{X0: 50, Y0: 220, X1: 100, Y1: 250, T: "\u540d\u79f0:"},
						{X0: 110, Y0: 220, X1: 125, Y1: 250, T: "\u5730"},
						{X0: 200, Y0: 220, X1: 300, Y1: 250, T: "\u5f00\u6237\u884c\u53ca\u8d26\u53f7:"},
						{X0: 310, Y0: 220, X1: 380, Y1: 250, T: "\u90ac\u5148\u751f"},
					},
				},
			},
		},
	}

	got, ok := extractBuyerNameFromPDFZones(pages)
	if !ok {
		t.Fatalf("expected buyer candidate, got ok=false")
	}
	if got.val != "\u90ac\u5148\u751f" {
		t.Fatalf("expected buyer=\u90ac\u5148\u751f got %q (src=%s)", got.val, got.src)
	}
}

func TestExtractInvoiceTotalsFromPDFZones_PicksXiaoxieAndTax(t *testing.T) {
	pages := []PDFTextZonesPage{
		{
			Page:   1,
			Width:  1000,
			Height: 1000,
			Rows: []PDFTextZonesRow{
				{
					Region: "items",
					Y0:     820,
					Y1:     850,
					Text:   "\u4ef7\u7a0e\u5408\u8ba1\uff08\u5927\u5199\uff09 \u5408\u8ba1\u58f9\u4edf\u53c1\u4f70\u9646\u5341\u4e5d\u5706\u6574 \uff08\u5c0f\u5199\uff09 \uffe5 1369.00 \uffe5 157.50",
					Spans: []PDFTextZonesSpan{
						{X0: 120, Y0: 820, X1: 200, Y1: 850, T: "\u4ef7\u7a0e\u5408\u8ba1"},
						{X0: 380, Y0: 820, X1: 430, Y1: 850, T: "\u5c0f\u5199"},
						{X0: 450, Y0: 820, X1: 520, Y1: 850, T: "1369.00"},
						{X0: 650, Y0: 820, X1: 710, Y1: 850, T: "157.50"},
					},
				},
			},
		},
	}

	total, _, _, tax, _, _ := extractInvoiceTotalsFromPDFZones(pages)
	if total == nil || *total != 1369.00 {
		t.Fatalf("expected total=1369.00 got %+v", total)
	}
	if tax == nil || *tax != 157.50 {
		t.Fatalf("expected tax=157.50 got %+v", tax)
	}
}

func TestExtractInvoiceLineItemsFromPDFZones_SplitsColumns(t *testing.T) {
	pages := []PDFTextZonesPage{
		{
			Page:   1,
			Width:  1000,
			Height: 1000,
			Rows: []PDFTextZonesRow{
				{
					Region: "items",
					Y0:     400,
					Y1:     420,
					Text:   "\u9879\u76ee\u540d\u79f0 \u89c4\u683c\u578b\u53f7 \u5355\u4f4d \u6570\u91cf \u5355\u4ef7 \u91d1\u989d",
					Spans: []PDFTextZonesSpan{
						{X0: 80, Y0: 400, X1: 160, Y1: 420, T: "\u9879\u76ee\u540d\u79f0"},
						{X0: 360, Y0: 400, X1: 450, Y1: 420, T: "\u89c4\u683c\u578b\u53f7"},
						{X0: 620, Y0: 400, X1: 670, Y1: 420, T: "\u5355\u4f4d"},
						{X0: 720, Y0: 400, X1: 770, Y1: 420, T: "\u6570\u91cf"},
					},
				},
				{
					Region: "items",
					Y0:     430,
					Y1:     450,
					Text:   "*\u975e\u91d1\u5c5e\u77ff\u7269\u5236\u54c1*\u679c\u76d8 - \u4e2a 2",
					Spans: []PDFTextZonesSpan{
						{X0: 80, Y0: 430, X1: 320, Y1: 450, T: "*\u975e\u91d1\u5c5e\u77ff\u7269\u5236\u54c1*\u679c\u76d8"},
						{X0: 360, Y0: 430, X1: 380, Y1: 450, T: "-"},
						{X0: 620, Y0: 430, X1: 635, Y1: 450, T: "\u4e2a"},
						{X0: 720, Y0: 430, X1: 730, Y1: 450, T: "2"},
					},
				},
			},
		},
	}

	items := extractInvoiceLineItemsFromPDFZones(pages)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].Name != "*\u975e\u91d1\u5c5e\u77ff\u7269\u5236\u54c1*\u679c\u76d8" {
		t.Fatalf("expected name parsed, got %q", items[0].Name)
	}
	if items[0].Spec != "-" {
		t.Fatalf("expected spec '-', got %q", items[0].Spec)
	}
	if items[0].Unit != "\u4e2a" {
		t.Fatalf("expected unit '\u4e2a', got %q", items[0].Unit)
	}
	if items[0].Quantity == nil || *items[0].Quantity != 2 {
		t.Fatalf("expected qty 2, got %+v", items[0].Quantity)
	}
}

