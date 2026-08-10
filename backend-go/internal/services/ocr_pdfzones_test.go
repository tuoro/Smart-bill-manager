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
					Text:   "SYNTHETIC / 纯合成测试数据 统一社会信用代码/纳税人识别号: 单价 纯合成长名称百货有限公司 100.00 金额 91110000SYNTH00001 税率/征收率 6% 税额 6.00",
				},
			},
		},
	}

	got := extractSellerNameFromPDFZones(pages)
	if got != "纯合成长名称百货有限公司" {
		t.Fatalf("expected synthetic seller, got %q", got)
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
						{X0: 310, Y0: 220, X1: 380, Y1: 250, T: "纯合成先生"},
					},
				},
			},
		},
	}

	got, ok := extractBuyerNameFromPDFZones(pages)
	if !ok {
		t.Fatalf("expected buyer candidate, got ok=false")
	}
	if got.val != "纯合成先生" {
		t.Fatalf("expected synthetic buyer, got %q (src=%s)", got.val, got.src)
	}
}

func TestExtractBuyerNameFromPDFZones_MergedLabelPersonalKeepsParens(t *testing.T) {
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
					Text:   "购买方信息 名称：统一社会信用代码/纳税人识别号：个人（个人） 销售方信息名称：",
				},
			},
		},
	}

	got, ok := extractBuyerNameFromPDFZones(pages)
	if !ok {
		t.Fatalf("expected buyer candidate, got ok=false")
	}
	if got.val != "个人（个人）" {
		t.Fatalf("expected buyer=%q got %q (src=%s)", "个人（个人）", got.val, got.src)
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
					Text:   "价税合计（大写） 合计壹佰零陆圆整 （小写） ￥ 106.00 ￥ 6.00",
					Spans: []PDFTextZonesSpan{
						{X0: 120, Y0: 820, X1: 200, Y1: 850, T: "\u4ef7\u7a0e\u5408\u8ba1"},
						{X0: 380, Y0: 820, X1: 430, Y1: 850, T: "\u5c0f\u5199"},
						{X0: 450, Y0: 820, X1: 520, Y1: 850, T: "106.00"},
						{X0: 650, Y0: 820, X1: 710, Y1: 850, T: "6.00"},
					},
				},
			},
		},
	}

	total, _, _, tax, _, _ := extractInvoiceTotalsFromPDFZones(pages)
	if total == nil || *total != 106.00 {
		t.Fatalf("expected synthetic total=106.00 got %+v", total)
	}
	if tax == nil || *tax != 6.00 {
		t.Fatalf("expected synthetic tax=6.00 got %+v", tax)
	}
}

func TestExtractInvoiceTotalsFromPDFZones_MultiAmountsAfterXiaoxiePrefersMax(t *testing.T) {
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
					Text:   "价税合计（大写） 合计伍拾贰圆整 （小写） 48.88 52.00 3.12",
					Spans: []PDFTextZonesSpan{
						{X0: 120, Y0: 820, X1: 200, Y1: 850, T: "价税合计"},
						{X0: 380, Y0: 820, X1: 430, Y1: 850, T: "小写"},
						{X0: 440, Y0: 820, X1: 520, Y1: 850, T: "48.88"},
						{X0: 560, Y0: 820, X1: 640, Y1: 850, T: "52.00"},
						{X0: 760, Y0: 820, X1: 830, Y1: 850, T: "3.12"},
					},
				},
			},
		},
	}

	total, src, _, tax, _, _ := extractInvoiceTotalsFromPDFZones(pages)
	if total == nil || *total != 52.00 {
		t.Fatalf("expected synthetic total=52.00 got %+v (src=%s)", total, src)
	}
	if tax == nil || *tax != 3.12 {
		t.Fatalf("expected synthetic tax=3.12 got %+v", tax)
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
					Text:   "*纯合成用品*测试托盘 - 个 2",
					Spans: []PDFTextZonesSpan{
						{X0: 80, Y0: 430, X1: 320, Y1: 450, T: "*纯合成用品*测试托盘"},
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
	if items[0].Name != "*纯合成用品*测试托盘" {
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

func TestExtractInvoiceLineItemsFromPDFZones_DiscountRowsNotMerged(t *testing.T) {
	pages := []PDFTextZonesPage{
		{
			Page:   1,
			Width:  595,
			Height: 400,
			Rows: []PDFTextZonesRow{
				{
					Region: "items",
					Y0:     140,
					Y1:     155,
					Text:   "项目名称 规格型号 单位 数量",
					Spans: []PDFTextZonesSpan{
						{X0: 50, Y0: 140, X1: 110, Y1: 155, T: "项目名称"},
						{X0: 160, Y0: 140, X1: 220, Y1: 155, T: "规格型号"},
						{X0: 240, Y0: 140, X1: 270, Y1: 155, T: "单位"},
						{X0: 290, Y0: 140, X1: 320, Y1: 155, T: "数量"},
					},
				},
				// Row 1: normal item (qty=1)
				{
					Region: "items",
					Y0:     160,
					Y1:     170,
					Text:   "*纯合成服务*测试主项目 1 40.00 6%",
					Spans: []PDFTextZonesSpan{
						{X0: 20, Y0: 160, X1: 140, Y1: 170, T: "*纯合成服务*测试主项目"},
						{X0: 286, Y0: 160, X1: 292, Y1: 170, T: "1"},
						{X0: 340, Y0: 160, X1: 380, Y1: 170, T: "109.43"},
						{X0: 480, Y0: 160, X1: 500, Y1: 170, T: "6%"},
						{X0: 560, Y0: 160, X1: 590, Y1: 170, T: "6.57"},
					},
				},
				// Row 2: discount/adjustment line (no qty, has money)
				{
					Region: "items",
					Y0:     172,
					Y1:     182,
					Text:   "*纯合成服务*测试主项目 -8.00 6% -0.48",
					Spans: []PDFTextZonesSpan{
						{X0: 20, Y0: 172, X1: 140, Y1: 182, T: "*纯合成服务*测试主项目"},
						{X0: 420, Y0: 172, X1: 460, Y1: 182, T: "-28.30"},
						{X0: 480, Y0: 172, X1: 500, Y1: 182, T: "6%"},
						{X0: 560, Y0: 172, X1: 590, Y1: 182, T: "-1.70"},
					},
				},
				// Row 3: normal item (qty=1)
				{
					Region: "items",
					Y0:     184,
					Y1:     194,
					Text:   "*纯合成服务*测试附加项目 1 12.00 6% 0.72",
					Spans: []PDFTextZonesSpan{
						{X0: 20, Y0: 184, X1: 200, Y1: 194, T: "*纯合成服务*测试附加项目"},
						{X0: 286, Y0: 184, X1: 292, Y1: 194, T: "1"},
						{X0: 360, Y0: 184, X1: 390, Y1: 194, T: "9.43"},
						{X0: 480, Y0: 184, X1: 500, Y1: 194, T: "6%"},
						{X0: 560, Y0: 184, X1: 590, Y1: 194, T: "0.57"},
					},
				},
				// Row 4: discount/adjustment line (no qty, has money)
				{
					Region: "items",
					Y0:     196,
					Y1:     206,
					Text:   "*纯合成服务*测试附加项目 -2.00 6% -0.12",
					Spans: []PDFTextZonesSpan{
						{X0: 20, Y0: 196, X1: 200, Y1: 206, T: "*纯合成服务*测试附加项目"},
						{X0: 420, Y0: 196, X1: 450, Y1: 206, T: "-7.55"},
						{X0: 480, Y0: 196, X1: 500, Y1: 206, T: "6%"},
						{X0: 560, Y0: 196, X1: 590, Y1: 206, T: "-0.45"},
					},
				},
			},
		},
	}

	items := extractInvoiceLineItemsFromPDFZones(pages)
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %+v", items)
	}
	if items[0].Quantity == nil || *items[0].Quantity != 1 {
		t.Fatalf("expected first qty=1 got %+v", items[0].Quantity)
	}
	if items[1].Quantity != nil {
		t.Fatalf("expected discount row qty nil got %+v", items[1].Quantity)
	}
	if items[2].Quantity == nil || *items[2].Quantity != 1 {
		t.Fatalf("expected third qty=1 got %+v", items[2].Quantity)
	}
	if items[3].Quantity != nil {
		t.Fatalf("expected discount row qty nil got %+v", items[3].Quantity)
	}
}
