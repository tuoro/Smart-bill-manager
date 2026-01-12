package services

import "testing"

func TestParseInvoiceDataWithMeta_TaxBureauHeaderNotSeller_AndBuyerKeepsParens(t *testing.T) {
	svc := &OCRService{}

	text := `
【第1页-分区】
【发票信息】
章 发票号码： 开票日期： 25112000000271095900 2025年12月04日
电子发票（普通发票） 全国北国家税务总局统京一市发税票务局监制
销售方名称：全国北国家税务总局统京一市发税票务局监制
【购买方】
购买方信息 名称： 统一社会信用代码/纳税人识别号： 个人（个人） 销售方信息名称：
项目名称 规格型号 单  位 数 量
【密码区】
统一社会信用代码/纳税人识别号： 北京果老商贸有限公司 91110105MA01JH69XW 下载次数：1
单  价 金  额 197.23 税率/征收率 1% 税  额 1.97
【明细】
*果类加工品*坚果、果仁
`

	got, err := svc.ParseInvoiceDataWithMeta(text, nil)
	if err != nil {
		t.Fatalf("ParseInvoiceDataWithMeta error: %v", err)
	}

	if got.BuyerName == nil || *got.BuyerName != "个人（个人）" {
		t.Fatalf("expected buyer=%q got=%+v (src=%q conf=%v)", "个人（个人）", got.BuyerName, got.BuyerNameSource, got.BuyerNameConfidence)
	}
	if got.SellerName == nil || *got.SellerName != "北京果老商贸有限公司" {
		t.Fatalf("expected seller=%q got=%+v (src=%q conf=%v)", "北京果老商贸有限公司", got.SellerName, got.SellerNameSource, got.SellerNameConfidence)
	}
}

func TestParseInvoiceDataWithMeta_ZonesFirstBuyerSeller(t *testing.T) {
	svc := &OCRService{}

	text := `
【第1页-分区】
【发票信息】
发票号码： 25112000000271095900
开票日期： 2025年12月04日
电子发票（普通发票） 全国北国家税务总局统京一市发税票务局监制

【购买方】
购买方信息 名称： 统一社会信用代码/纳税人识别号： 个人（个人） 销售方信息名称：

【密码区】
统一社会信用代码/纳税人识别号： 北京果老商贸有限公司 91110105MA01JH69XW 下载次数：1
`

	meta := &PDFTextCLIResponse{
		Zones: []PDFTextZonesPage{
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
					{
						Region: "password",
						Y0:     260,
						Y1:     290,
						Text:   "统一社会信用代码/纳税人识别号： 北京果老商贸有限公司 91110105MA01JH69XW 下载次数：1",
					},
				},
			},
		},
	}

	got, err := svc.ParseInvoiceDataWithMeta(text, meta)
	if err != nil {
		t.Fatalf("ParseInvoiceDataWithMeta error: %v", err)
	}
	if got.BuyerName == nil || *got.BuyerName != "个人（个人）" {
		t.Fatalf("expected buyer=%q got %+v (src=%q conf=%v)", "个人（个人）", got.BuyerName, got.BuyerNameSource, got.BuyerNameConfidence)
	}
	if got.SellerName == nil || *got.SellerName != "北京果老商贸有限公司" {
		t.Fatalf("expected seller=%q got %+v (src=%q conf=%v)", "北京果老商贸有限公司", got.SellerName, got.SellerNameSource, got.SellerNameConfidence)
	}
}
