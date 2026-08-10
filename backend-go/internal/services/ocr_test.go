package services

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"smart-bill-manager/internal/devtools/regressionfixtures"
)

func TestParseInvoiceData_NewlineFormat(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
电子发票（普通发票）
发票号码：
99000000000000000501
开票日期：
2026年08月17日
购
买
方
信
息
统一社会信用代码/纳税人识别号：
名称：
星河采购有限公司
销
售
方
信
息
统一社会信用代码/纳税人识别号：
91110000SYNTH00001
名称：
云海百货有限公司
项目名称
规格型号
单 位
数 量
单 价
金 额
税率/征收率
税 额
*纯合成用品*测试饮品甲
SYN-A*6
瓶
2
40.00
80.00
6%
4.80
*纯合成用品*测试饮品乙
SYN-B*6
瓶
2
20.00
40.00
6%
2.40
合
计
¥
120.00
¥
7.20
价税合计（大写）
壹佰贰拾柒圆贰角
（小写）
¥
127.20
备
注
开票人：
纯合成开票人`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "99000000000000000501" {
		t.Errorf("Expected synthetic InvoiceNumber, got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2026年08月17日" {
		t.Errorf("Expected synthetic InvoiceDate, got '%s'", *data.InvoiceDate)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 127.20
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "云海百货有限公司" {
		t.Errorf("Expected synthetic SellerName, got '%s'", *data.SellerName)
	}

	// Test buyer name
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "星河采购有限公司" {
		t.Errorf("Expected synthetic BuyerName, got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_TraditionalFormat(t *testing.T) {
	service := NewOCRService()

	// Test traditional format (fields on same line) to ensure backward compatibility
	sampleText := syntheticOCRText(
		"电子发票（普通发票）",
		"发票号码：99000000000000000502",
		"开票日期：2026年08月18日",
		"销售方名称：云海服务有限公司",
		"购买方名称：星河采购有限公司",
		"价税合计（小写）¥106.00",
	)

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "99000000000000000502" {
		t.Errorf("Expected synthetic InvoiceNumber, got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2026年08月18日" {
		t.Errorf("Expected synthetic InvoiceDate, got '%s'", *data.InvoiceDate)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 106.00
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "云海服务有限公司" {
		t.Errorf("Expected synthetic SellerName, got '%s'", *data.SellerName)
	}

	// Test buyer name
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "星河采购有限公司" {
		t.Errorf("Expected synthetic BuyerName, got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_SyntheticSeparatedValuesFormat(t *testing.T) {
	service := NewOCRService()

	// Synthetic OCR layout with labels and values completely separated.
	sampleText := `SYNTHETIC / 纯合成测试数据
电子发票（普通发票）
发票号码：
开票日期：
购
买
方
信
息
统一社会信用代码/纳税人识别号：
销
售
方
信
息
统一社会信用代码/纳税人识别号：
名称：
名称：
项目名称
规格型号
单 位
数 量
单 价
金 额
税率/征收率
税 额
合
计
价税合计（大写）
（小写）
备
注
开票人：
99000000000000000503
2026年08月19日
星河女士
云海百货有限公司
91110000SYNTH00002
¥
120.00
¥
7.20
壹佰贰拾柒圆贰角
¥
127.20
纯合成开票人
纯合成开票人
*纯合成用品*测试饮品甲
SYN-A*6
6%
瓶
80.00
4.80
40.00
2
*纯合成用品*测试饮品乙
SYN-B*6
6%
瓶
40.00
2.40
20.00
2`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "99000000000000000503" {
		t.Errorf("Expected synthetic InvoiceNumber, got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date - SHOULD extract the date from OCR text
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil for synthetic separated layout")
	} else if *data.InvoiceDate != "2026年08月19日" {
		t.Errorf("Expected synthetic InvoiceDate, got '%s'", *data.InvoiceDate)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 127.20
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name from the synthetic separated layout.
	if data.SellerName == nil {
		t.Error("SellerName is nil for synthetic separated layout")
	} else if *data.SellerName != "云海百货有限公司" {
		t.Errorf("Expected synthetic SellerName, got '%s'", *data.SellerName)
	}

	// Test buyer name and ensure a label is not selected as the value.
	if data.BuyerName == nil {
		t.Error("BuyerName is nil for synthetic separated layout")
	} else if *data.BuyerName == "名称：" || *data.BuyerName == "名称:" {
		t.Errorf("BuyerName incorrectly extracted as label %q", *data.BuyerName)
	} else if *data.BuyerName != "星河女士" {
		t.Errorf("Expected synthetic BuyerName, got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_PreferTaxInclusiveAmount(t *testing.T) {
	service := NewOCRService()

	// Some invoices contain both:
	// - 合计金额(小写): tax-exclusive subtotal
	// - 价税合计(小写): tax-inclusive total (desired)
	sampleText := `SYNTHETIC / 纯合成测试数据
合计金额(小写)：100.00
价税合计(小写)：106.00`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	if data.Amount == nil {
		t.Fatal("Amount is nil, expected 106.00")
	}
	if *data.Amount != 106.00 {
		t.Fatalf("Expected Amount 106.00, got %.2f", *data.Amount)
	}
}

func TestParseInvoiceData_ItemsExtraction(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
货物或应税劳务、服务名称
规格型号 单位 数量 单价 金额 税率 税额
*纯合成食品*SYNTHETIC测试乳品 1.20kg(300g*4)
4X300g
组
2
12.00
24.00
3%
0.72
*纯合成服务*测试配送
8.00
8.00
3%
0.24
价税合计(小写) ¥32.96`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(data.Items))
	}
	if data.Items[0].Quantity == nil {
		t.Fatal("Expected first item quantity 2, got nil")
	}
	if *data.Items[0].Quantity != 2 {
		t.Fatalf("Expected first item quantity 2, got %.2f (item=%+v)", *data.Items[0].Quantity, data.Items[0])
	}
	if data.Items[1].Quantity == nil || *data.Items[1].Quantity != 1 {
		t.Fatalf("Expected second item quantity 1, got %+v", data.Items[1].Quantity)
	}
	if data.Items[0].Name == "" || data.Items[1].Name == "" {
		t.Fatalf("Expected item names to be non-empty, got %+v", data.Items)
	}
	if data.PrettyText == "" || !strings.Contains(data.PrettyText, "【商品明细(解析)】") {
		t.Fatalf("Expected PrettyText to include items section, got: %q", data.PrettyText)
	}
}

func TestParseInvoiceData_ItemsExtraction_PDFTextNoisy(t *testing.T) {
	service := NewOCRService()

	// PDF text extraction often yields:
	// - spaced-out headers like "税 额"
	// - invoice meta inserted between header and rows
	// Ensure we anchor on the first tax-rate line and backtrack to the real item rows.
	sampleText := `SYNTHETIC / 纯合成测试数据
货物或应税劳务、服务名称
规 格 型 号
单 位
数 量
单 价
金 额
税 率
税 额
纯合成增值税电子普通发票
机器编号：999999990501
发票代码：990000000501
发票号码：99000501
开票日期：2026 年 08 月 17 日
校 验 码：99999 00000 00501 00000
*纯合成食品*SYNTHETIC 测试乳品 1.20kg(300g*4)
4X300g
组
2
12.00
24.00
3%
0.72
*纯合成服务*测试配送
1
8.00
8.00
3%
0.24
价税合计(小写) ¥32.96`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	if data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Expected first item quantity 2, got %+v", data.Items[0].Quantity)
	}
	if data.Items[1].Quantity == nil || *data.Items[1].Quantity != 1 {
		t.Fatalf("Expected second item quantity 1, got %+v", data.Items[1].Quantity)
	}
	for _, it := range data.Items {
		if it.Name == "" {
			t.Fatalf("Expected non-empty item name: %+v", data.Items)
		}
		if it.Name == "税额" || it.Name == "纯合成增值税电子普通发票" {
			t.Fatalf("Unexpected meta/header captured as item: %+v", data.Items)
		}
	}
}

func TestParseInvoiceData_PyMuPDFZoned_SellerAndItemUnitQty(t *testing.T) {
	service := NewOCRService()

	// PyMuPDF zoned layout: section headers are present, and some item lines may merge unit+qty into the name token.
	// Also ensure we can recover the full seller company name from the tax-id line even if a shorter nearby name exists.
	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【明细】
货物或应税劳务、服务名称
规格型号
单位
数量
单价
金额
税率
税额
*纯合成服务*测试充值元1
200.00
200.00
*
*
合计 价税合计(大写)
(小写) ¥200.00
【销售方】
纯合成短名称有限公司
方 售 销 名 称: 开户行及账号: 地 址、电 话: 纳税人识别号: 纯合成通信服务有限公司91110000SYNTH00501 纯合成大道501号19900000501`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.SellerName == nil || *data.SellerName != "纯合成通信服务有限公司" {
		t.Fatalf("Expected seller name %q, got %+v (source=%q conf=%v)", "纯合成通信服务有限公司", data.SellerName, data.SellerNameSource, data.SellerNameConfidence)
	}

	if len(data.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d: %+v", len(data.Items), data.Items)
	}
	if !strings.Contains(data.Items[0].Name, "纯合成服务") || !strings.Contains(data.Items[0].Name, "测试充值") {
		t.Fatalf("Unexpected item name: %+v", data.Items[0])
	}
	if data.Items[0].Unit != "元" {
		t.Fatalf("Expected unit %q, got %q", "元", data.Items[0].Unit)
	}
	if data.Items[0].Quantity == nil || *data.Items[0].Quantity != 1 {
		t.Fatalf("Expected quantity 1, got %+v", data.Items[0].Quantity)
	}

	// Pretty text should keep zoned headers for readability.
	if !strings.Contains(data.PrettyText, "【明细】") || !strings.Contains(data.PrettyText, "【销售方】") {
		t.Fatalf("Expected PrettyText to preserve zoned section headers, got: %q", data.PrettyText)
	}
}

func TestParseInvoiceData_PyMuPDFZoned_MergedBuyerSellerAndPackedItemRow(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【发票信息】
发票号码： 99000000000000000502
开票日期： 2026年08月20日
电子发票（普通发票）
【购买方】
购买方信息统一社会信用代码/纳税人识别号： 名称： 个人销售方信息名称：
项目名称规格型号单位数量
【密码区】
统一社会信用代码/纳税人识别号： 单价纯合成百货商店490.10金额91110000SYNTH00502 税率/征收率1% 税额4.90下载次数：1
【明细】
*纯合成饮品*测试饮品甲 53°*500ml 瓶2 245.049504950495
价税合计（大写） 合计肆佰玖拾伍圆整 ￥ 490.10 （小写） ￥ 495.00 ￥ 4.90
【备注/其他】
开票人： 纯合成开票人`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	if data.BuyerName == nil || *data.BuyerName != "个人" {
		t.Fatalf("Expected buyer %q, got %+v (source=%q conf=%v)", "个人", data.BuyerName, data.BuyerNameSource, data.BuyerNameConfidence)
	}
	if data.SellerName == nil {
		t.Fatalf("Expected seller %q, got nil", "纯合成百货商店")
	}
	if *data.SellerName != "纯合成百货商店" {
		t.Fatalf("Expected seller %q, got %q (source=%q conf=%v)", "纯合成百货商店", *data.SellerName, data.SellerNameSource, data.SellerNameConfidence)
	}

	if len(data.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d: %+v", len(data.Items), data.Items)
	}
	it := data.Items[0]
	if !strings.Contains(it.Name, "纯合成饮品") || !strings.Contains(it.Name, "测试饮品甲") {
		t.Fatalf("Unexpected item name: %+v", it)
	}
	if it.Spec != "53°×500ml" {
		t.Fatalf("Expected spec %q, got %q", "53°×500ml", it.Spec)
	}
	if it.Unit != "瓶" {
		t.Fatalf("Expected unit %q, got %q", "瓶", it.Unit)
	}
	if it.Quantity == nil || *it.Quantity != 2 {
		t.Fatalf("Expected quantity 2, got %+v", it.Quantity)
	}

	if data.PrettyText == "" || !strings.Contains(data.PrettyText, "【购买方】") || !strings.Contains(data.PrettyText, "【明细】") {
		t.Fatalf("Expected PrettyText to preserve zoned section headers, got: %q", data.PrettyText)
	}
}

func TestParseInvoiceData_PyMuPDFZoned_TwoItemsAndCorrectTotalAmount(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【发票信息】
发票号码： 99000000000000000503
开票日期： 2026年08月21日
电子发票（普通发票）
【购买方】
购买方信息统一社会信用代码/纳税人识别号： 名称： 个人销售方信息名称：
项目名称规格型号单位数量
【密码区】
统一社会信用代码/纳税人识别号： 单价纯合成百货商店600.00 380.20金额91110000SYNTH00503 税率/征收率1% 1% 税额6.00 3.80下载次数：1
【明细】
*纯合成饮品*测试饮品甲 *纯合成饮品*测试饮品乙 53°*6 750ml*6瓶瓶2 2 300.000000000000 190.100000000000
价税合计（大写） 合计玖佰玖拾圆整 ￥ 980.20 （小写） ￥ 990.00 ￥ 9.80
【备注/其他】
开票人： 纯合成开票人`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.Amount == nil || *data.Amount != 990 {
		t.Fatalf("Expected amount 990, got %+v (src=%q)", data.Amount, data.AmountSource)
	}
	if data.TaxAmount == nil || fmt.Sprintf("%.2f", *data.TaxAmount) != "9.80" {
		t.Fatalf("Expected tax_amount 9.80, got %+v (src=%q)", data.TaxAmount, data.TaxAmountSource)
	}
	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	if !strings.Contains(data.Items[0].Name, "测试饮品甲") || data.Items[0].Unit != "瓶" || data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Unexpected item 0: %+v", data.Items[0])
	}
	if data.Items[0].Spec == "" || !strings.Contains(data.Items[0].Spec, "53") {
		t.Fatalf("Unexpected item 0 spec: %+v", data.Items[0])
	}
	if !strings.Contains(data.Items[1].Name, "测试饮品乙") || data.Items[1].Unit != "瓶" || data.Items[1].Quantity == nil || *data.Items[1].Quantity != 2 {
		t.Fatalf("Unexpected item 1: %+v", data.Items[1])
	}
	if data.Items[1].Spec == "" || !strings.Contains(strings.ToLower(data.Items[1].Spec), "ml") {
		t.Fatalf("Unexpected item 1 spec: %+v", data.Items[1])
	}
}

func TestParseInvoiceData_ItemsExtraction_PDFTextStopsBeforePartyInfo(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
货物或应税劳务、服务名称
规格型号   单位
数 量
单 价
金 额
税率
税额
*纯合成食品*SYNTHETIC测试乳品 1.20kg(300g*4)
4X300g
组
2
12.00
24.00
3%
0.72
*纯合成服务*测试配送
1
8.00
8.00
3%
0.24
名称:
纳税人识别号:
地址、电话:
开户行及账号:
纯合成零售有限公司
收款人：纯合成收款人
复核：纯合成复核人
开票人：纯合成开票人
订单号[999999990504]
发票专用章`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	if data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Expected first item quantity 2, got %+v", data.Items[0].Quantity)
	}
	if data.Items[1].Quantity == nil || *data.Items[1].Quantity != 1 {
		t.Fatalf("Expected second item quantity 1, got %+v", data.Items[1].Quantity)
	}
	for _, it := range data.Items {
		if it.Name == "" {
			t.Fatalf("Expected non-empty item name: %+v", data.Items)
		}
		if it.Name == "名称" || it.Name == "纳税人识别号" {
			t.Fatalf("Unexpected label captured as item: %+v", data.Items)
		}
	}
}

func TestParseInvoiceData_ItemsExtraction_PDFLongDecimalUnitPrice(t *testing.T) {
	service := NewOCRService()

	// Some PDF text extractions list unit price with many decimals before the quantity.
	// Ensure we don't treat long-decimal numbers as quantity, and stop before footer noise.
	sampleText := `SYNTHETIC / 纯合成测试数据
项目名称
规格型号
单 位
数 量
单 价
金 额
税率/征收率
税 额
*纯合成饮品*测试饮品甲
53°*6
1%
瓶
600.00
6.00
300.000000000000
2
*纯合成饮品*测试饮品乙
750ml*6
1%
瓶
380.20
3.80
190.100000000000
2
下载次数：1`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	if data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Expected first item quantity 2, got %+v", data.Items[0].Quantity)
	}
	if data.Items[1].Quantity == nil || *data.Items[1].Quantity != 2 {
		t.Fatalf("Expected second item quantity 2, got %+v", data.Items[1].Quantity)
	}
	for _, it := range data.Items {
		if strings.Contains(it.Name, "下载次数") {
			t.Fatalf("Unexpected footer captured as item: %+v", data.Items)
		}
	}
}

func TestParseInvoiceData_ItemsExtraction_PDFHeaderRegionScoring_MetaBeforeHeader(t *testing.T) {
	service := NewOCRService()

	// Some PDF text extractions include a lot of metadata before the table header.
	// Ensure we still find the table header region and only extract real line items.
	sampleText := `SYNTHETIC / 纯合成测试数据
电子发票（普通发票）
发票号码：
99000000000000000505
开票日期：
2026年08月21日
购
买
方
信
息
名称：
个人
销
售
方
信
息
名称：
纯合成百货商店

项目名称
规 格 型 号
单 位
数 量
单 价
金 额
税率/征收率
税 额
*纯合成饮品*测试饮品甲
53°*6
1%
瓶
600.00
6.00
300.000000000000
2
*纯合成饮品*测试饮品乙
750ml*6
1%
瓶
380.20
3.80
190.100000000000
2
下载次数：1`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	if data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Expected first item quantity 2, got %+v", data.Items[0].Quantity)
	}
	if data.Items[1].Quantity == nil || *data.Items[1].Quantity != 2 {
		t.Fatalf("Expected second item quantity 2, got %+v", data.Items[1].Quantity)
	}
	for _, it := range data.Items {
		if strings.Contains(it.Name, "下载次数") || strings.Contains(it.Name, "电子发票") || strings.Contains(it.Name, "开票日期") {
			t.Fatalf("Unexpected non-item captured as item: %+v", data.Items)
		}
	}
}

func TestParseInvoiceData_ItemsExtraction_ImageTextStopsOnInlineLabelsAndTotals(t *testing.T) {
	service := NewOCRService()

	// Image OCR sometimes merges labels with values and splits totals across multiple lines:
	// - "价税合计(大写)" then a written total, then "（小写）￥32.96"
	// - "名称：xxx" on the same line
	// Ensure these are not captured as line items.
	sampleText := `SYNTHETIC / 纯合成测试数据
货物或应税劳务、服务名称
规格型号   单位
数 量
单 价
金 额
税率
税额
*纯合成食品*SYNTHETIC测试乳品 1.20kg(300g*4)
4X300g
组
2
12.00
24.00
3%
0.72
*纯合成服务*测试配送
1
8.00
8.00
3%
0.24
价税合计(大写)
叁拾贰圆玖角陆分
（小写）￥32.96
名称：纯合成零售有限公司
纳税人识别号：91110000SYNTH00506`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	if data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Expected first item quantity 2, got %+v", data.Items[0].Quantity)
	}
	if data.Items[1].Quantity == nil || *data.Items[1].Quantity != 1 {
		t.Fatalf("Expected second item quantity 1, got %+v", data.Items[1].Quantity)
	}
	for _, it := range data.Items {
		if strings.Contains(it.Name, "壹佰") || strings.Contains(it.Name, "小写") || strings.Contains(it.Name, "名称") {
			t.Fatalf("Unexpected non-item captured as item: %+v", data.Items)
		}
	}
}

func TestParseInvoiceData_ItemsExtraction_ImageText_SplitHejiAndSellerLines(t *testing.T) {
	service := NewOCRService()

	// Synthetic image OCR layout where:
	// - "合计" is split across lines ("合" then "计")
	// - seller fields are split ("名" then "称：xxx")
	// Ensure we still extract both items and stop before seller/footer blocks.
	sampleText := `SYNTHETIC / 纯合成测试数据
发票代码：990000000507
纯合成增值税电子普通发票
发票号码：99000507
开票日期：2026年08月22日
校验码：99999000000005070000
购
名
称：纯合成购买人甲
货物或应税劳务、服务名称
规格型号
单位
数量
单价
金额
税率
税额
*纯合成食品*SYNTHETIC测试乳品1.20kg（300g*4)
4X300g
组
2
12.00
24.00
3%
0.72
*纯合成服务*测试配送
8.00
8.00
3%
0.24
合
计
￥32.00
￥0.96
价税合计(大写)
叁拾贰圆玖角陆分
（小写）￥32.96
名
称：纯合成零售有限公司
订单号[999999990507]
销售方
纳税人识别号：91110000SYNTH00507
发票专用章`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	if data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Expected first item quantity 2, got %+v", data.Items[0].Quantity)
	}
	if data.Items[1].Quantity == nil || *data.Items[1].Quantity != 1 {
		t.Fatalf("Expected second item quantity 1, got %+v", data.Items[1].Quantity)
	}
	if !strings.Contains(data.Items[0].Name, "纯合成食品") {
		t.Fatalf("Expected first item to contain 纯合成食品, got %q", data.Items[0].Name)
	}
	if !strings.Contains(data.Items[0].Name, "SYNTHETIC") {
		t.Fatalf("Expected first item to contain SYNTHETIC, got %q", data.Items[0].Name)
	}
	if !strings.Contains(data.Items[1].Name, "测试配送") {
		t.Fatalf("Expected second item to contain 测试配送, got %q", data.Items[1].Name)
	}
	for _, it := range data.Items {
		if strings.Contains(it.Name, "价税合计") || strings.Contains(it.Name, "纯合成零售有限公司") {
			t.Fatalf("Unexpected non-item captured as item: %+v", data.Items)
		}
	}
}

func TestExtractPartyFromROICandidate_NameLabels(t *testing.T) {
	buyerText := `SYNTHETIC / 纯合成测试数据
购买方名称：纯合成购买人甲
购买方纳税人识别号：91110000SYNTH00508
地址电话：纯合成地址`
	buyerName, buyerTax := extractPartyFromROICandidate(buyerText, "buyer")
	if buyerName != "纯合成购买人甲" {
		t.Fatalf("Expected synthetic buyer name, got %q", buyerName)
	}
	if buyerTax != "91110000SYNTH00508" {
		t.Fatalf("Expected synthetic buyer tax ID, got %q", buyerTax)
	}

	sellerText := `SYNTHETIC / 纯合成测试数据
销售方名称：纯合成销售公司
销售方纳税人识别号：91110000SYNTH00509`
	sellerName, sellerTax := extractPartyFromROICandidate(sellerText, "seller")
	if sellerName != "纯合成销售公司" {
		t.Fatalf("Expected synthetic seller name, got %q", sellerName)
	}
	if sellerTax != "91110000SYNTH00509" {
		t.Fatalf("Expected synthetic seller tax ID, got %q", sellerTax)
	}
}

func TestParseInvoiceData_SyntheticLegacyTransportInvoice(t *testing.T) {
	service := NewOCRService()

	// Synthetic OCR layout with an 8-digit invoice number and full-width ￥ symbol.
	sampleText := `SYNTHETIC / 纯合成测试数据
合
计
备
注
纯合成增值税电子普通发票
价税合计（大写）
（小写）
货物或应税劳务、服务名称
规格型号
单位
数　量
单　价
金　额
税率
税　额
购
买
方
销
售
方
收 款 人:
复 核:
开 票 人:
销 售 方:（章）
密
码
区
机器编号:
名　　　　称:
纳税人识别号:
地 址、
开户行及账号:
名　　　　称:
纳税人识别号:
地 址、
开户行及账号:
发票代码:
发票号码:
开票日期:
校
验
码:
电 话:
电 话:
￥41.16
￥1.20
*纯合成服务*测试路程费用
无
次
1
50
50.00
3%
-8.84
3%
-0.30
999999990509
肆拾贰圆叁角陆分
纯合成收款人
纯合成复核人
纯合成开票人
99<9>/99>9999999999+99999-<*
99999*+99/>99+/99999999999/9
99<9>/99>9999999999>999-9/<9
+9*9>//<999999<999+9/9-9999-
个人
纯合成出行服务有限公司
91110000SYNTH00510
纯合成大道510号19900000510
纯合成测试银行999999990510
990000000510
99000510
2026年08月23日
99999 00000 00510 00000
￥42.36`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number - should extract the synthetic 8-digit number.
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "99000510" {
		t.Errorf("Expected synthetic InvoiceNumber, got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2026年08月23日" {
		t.Errorf("Expected synthetic InvoiceDate, got '%s'", *data.InvoiceDate)
	}

	// Test amount with the full-width symbol.
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 42.36
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "纯合成出行服务有限公司" {
		t.Errorf("Expected synthetic SellerName, got '%s'", *data.SellerName)
	}

	// Test buyer name
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "个人" {
		t.Errorf("Expected BuyerName '个人', got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_SyntheticTransportInvoice_PyMuPDFZoned(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【发票信息】
发票代码: 发票号码: 开票日期: 校验码: 990000000510 99000510 2026 99999 00000 00510 00000年08月23日
纯合成增值税电子普通发票
机器编号: 999999990510
【购买方】
购买方名 称: 纳税人识别号: 地址、 SYNTHETIC 开户行及账号: 电话: 个人
货物或应税劳务、服务名称规格型号单位数量
【明细】
* * 运输服务SYNTHETIC运输服务 * * 客运服务费SYNTHETIC客运服务费无次1
价税合计（大写） 合计肆拾贰圆叁角陆分 ￥ 41.16 （小写） ￥ 42.36 ￥ 1.20
【销售方】
销售方
名 称: 纯合成出行服务有限公司
纳税人识别号: 91110000SYNTH00510
地址、 开户行及账号: 电话:
【备注/其他】
纯合成开票人 销售方:（章）`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.InvoiceDate == nil || *data.InvoiceDate != "2026年08月23日" {
		t.Fatalf("Expected InvoiceDate %q, got %+v (src=%q)", "2026年08月23日", data.InvoiceDate, data.InvoiceDateSource)
	}
	if data.BuyerName == nil || *data.BuyerName != "个人" {
		t.Fatalf("Expected BuyerName %q, got %+v (src=%q)", "个人", data.BuyerName, data.BuyerNameSource)
	}
	if data.SellerName == nil || *data.SellerName != "纯合成出行服务有限公司" {
		t.Fatalf("Expected SellerName %q, got %+v (src=%q)", "纯合成出行服务有限公司", data.SellerName, data.SellerNameSource)
	}
	if data.Amount == nil || fmt.Sprintf("%.2f", *data.Amount) != "42.36" {
		t.Fatalf("Expected Amount 42.36, got %+v (src=%q)", data.Amount, data.AmountSource)
	}
	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	for i, it := range data.Items {
		if it.Unit != "次" {
			t.Fatalf("Expected item[%d].Unit %q, got %q", i, "次", it.Unit)
		}
		if it.Quantity == nil || *it.Quantity != 1 {
			t.Fatalf("Expected item[%d].Quantity 1, got %+v", i, it.Quantity)
		}
		if it.Spec != "无" {
			t.Fatalf("Expected item[%d].Spec %q, got %q", i, "无", it.Spec)
		}
		if !strings.Contains(it.Name, "运输服务") || !strings.Contains(it.Name, "客运服务费") {
			t.Fatalf("Unexpected item[%d].Name: %q", i, it.Name)
		}
	}
}

func TestIsGarbledText(t *testing.T) {
	service := NewOCRService()

	// Test valid Chinese text
	validText := "纯合成增值税电子普通发票 发票号码：99000511"
	if service.isGarbledText(validText) {
		t.Error("Valid Chinese text incorrectly detected as garbled")
	}

	// Test valid English text
	validEnglishText := "Invoice Number: 99000511 Amount: $42.36"
	if service.isGarbledText(validEnglishText) {
		t.Error("Valid English text incorrectly detected as garbled")
	}

	// Test a fixed synthetic garbled string.
	garbledText := "T ��N�zT��(Y'Q�)(\\Q�)�T y�:~�zN���R+S�:W0 W@0u5 ��:_b7�LSʍ&S�:e6k>N�:Y"
	if !service.isGarbledText(garbledText) {
		t.Error("Garbled text not detected as garbled")
	}

	// Test mostly garbled text with some valid characters
	mostlyGarbledText := "��������a��������b��������"
	if !service.isGarbledText(mostlyGarbledText) {
		t.Error("Mostly garbled text not detected as garbled")
	}

	// Test empty text
	if !service.isGarbledText("") {
		t.Error("Empty text should be detected as garbled")
	}

	// Test text with valid ratio around 50% (edge case)
	edgeCaseText := "正常文字��������正常文字"
	// This test documents the behavior at the edge case
	// With roughly 50/50 valid/invalid, it should be detected as garbled (< 0.5)
	result := service.isGarbledText(edgeCaseText)
	t.Logf("Edge case (50%% valid) detected as garbled: %v", result)
}

func TestIsLikelyUsefulInvoicePDFText_IncludesItineraries(t *testing.T) {
	service := NewOCRService()

	// Airline itinerary markers (short text that would normally fail the minChars heuristic).
	air := "SYNTHETIC / 纯合成测试数据\n航空运输电子客票行程单\n电子客票号码: SYNTHETIC-AIR-TEST\n旅客姓名: 纯合成旅客甲\n填开日期: 2026年08月01日"
	if !service.isLikelyUsefulInvoicePDFText(air) {
		t.Error("Air ticket itinerary text should be treated as useful PDF text")
	}

	// Airline variant without the full title.
	air2 := "SYNTHETIC / 纯合成测试数据\n航空运输电子客票\n电子客票号码: SYNTHETIC-AIR-TEST\n填开日期: 2026年08月01日"
	if !service.isLikelyUsefulInvoicePDFText(air2) {
		t.Error("Air ticket itinerary (no full title) should be treated as useful PDF text")
	}

	// Railway e-ticket markers.
	rail := "SYNTHETIC / 纯合成测试数据\n电子发票（铁路电子客票）\n票价: 88.00\n开票日期: 2026年08月04日"
	if !service.isLikelyUsefulInvoicePDFText(rail) {
		t.Error("Railway e-ticket text should be treated as useful PDF text")
	}
}

func TestPdfToImageOCR_ErrorHandling(t *testing.T) {
	service := NewOCRService()

	// Test with non-existent file
	_, err := service.RecognizePDF("/nonexistent/file.pdf")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
	t.Logf("Correctly returned error for non-existent file: %v", err)

	// Test with empty path
	_, err = service.RecognizePDF("")
	if err == nil {
		t.Error("Expected error for empty path, got nil")
	}
	t.Logf("Correctly returned error for empty path: %v", err)
}

func TestExtractTextWithPdftotext(t *testing.T) {
	service := NewOCRService()

	// Test that pdftotext method is available
	_, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext not available, skipping test")
	}

	// Test with non-existent file
	_, err = service.extractTextWithPdftotext("/nonexistent/file.pdf")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
	t.Logf("Correctly returned error for non-existent file: %v", err)

	// Test with empty path
	_, err = service.extractTextWithPdftotext("")
	if err == nil {
		t.Error("Expected error for empty path, got nil")
	}
	t.Logf("Correctly returned error for empty path: %v", err)

	// Note: Testing with a real PDF file would require creating test fixtures
	// In a real scenario, you would create a test PDF with CID fonts and verify
	// that pdftotext extracts the text correctly
}

func TestGetChineseCharRatio(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name     string
		text     string
		expected float64
	}{
		{
			name:     "All Chinese",
			text:     "这是一个测试",
			expected: 1.0,
		},
		{
			name:     "Half Chinese",
			text:     "这是test",
			expected: 2.0 / 6.0,
		},
		{
			name:     "No Chinese",
			text:     "This is a test",
			expected: 0.0,
		},
		{
			name:     "Empty string",
			text:     "",
			expected: 0.0,
		},
		{
			name:     "With spaces",
			text:     "这是 一个 测试",
			expected: 1.0, // Spaces are ignored
		},
		{
			name:     "Mixed content with numbers",
			text:     "发票号码：12345678",
			expected: 4.0 / 13.0, // 4 Chinese chars, 9 non-Chinese chars (1 colon + 8 digits)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.getChineseCharRatio(tt.text)
			// Use approximate comparison for floating point
			if result < tt.expected-0.01 || result > tt.expected+0.01 {
				t.Errorf("getChineseCharRatio() = %.2f, want %.2f", result, tt.expected)
			}
		})
	}
}

func TestExtractAmounts(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name     string
		text     string
		expected int // number of amounts found
	}{
		{
			name:     "Single amount with ¥",
			text:     "金额：¥200.00",
			expected: 1,
		},
		{
			name:     "Multiple amounts",
			text:     "合计 ¥32.00 税额 ¥0.96 总计 ¥32.96",
			expected: 3,
		},
		{
			name:     "Amount with full-width symbol",
			text:     "价税合计（小写）￥42.36",
			expected: 1,
		},
		{
			name:     "No amounts",
			text:     "这是一个测试",
			expected: 0,
		},
		{
			name:     "Amount with comma",
			text:     "¥1,234.56",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.extractAmounts(tt.text)
			if len(result) != tt.expected {
				t.Errorf("extractAmounts() found %d amounts, want %d. Results: %v", len(result), tt.expected, result)
			}
		})
	}
}

func TestExtractTaxIDs(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name     string
		text     string
		expected int // number of tax IDs found
	}{
		{
			name:     "Single 18-char tax ID",
			text:     "纳税人识别号：91110000SYNTH00511",
			expected: 1,
		},
		{
			name:     "Single 20-char tax ID",
			text:     "统一社会信用代码：91110000SYNTH00512",
			expected: 1,
		},
		{
			name:     "Multiple tax IDs",
			text:     "销售方：91110000SYNTH00511 购买方：91110000SYNTH00512",
			expected: 2,
		},
		{
			name:     "No tax IDs",
			text:     "这是一个测试",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.extractTaxIDs(tt.text)
			if len(result) != tt.expected {
				t.Errorf("extractTaxIDs() found %d tax IDs, want %d. Results: %v", len(result), tt.expected, result)
			}
		})
	}
}

func TestExtractDates(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name     string
		text     string
		expected int // number of dates found
	}{
		{
			name:     "Chinese format YYYY年MM月DD日",
			text:     "开票日期：2026年08月02日",
			expected: 1,
		},
		{
			name:     "Space-separated format",
			text:     "日期：2026 08 02",
			expected: 1,
		},
		{
			name:     "Dash-separated format",
			text:     "2026-08-02",
			expected: 1,
		},
		{
			name:     "Multiple dates",
			text:     "开票日期：2026年08月02日 到期日：2026年09月02日",
			expected: 2,
		},
		{
			name:     "No dates",
			text:     "这是一个测试",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.extractDates(tt.text)
			if len(result) != tt.expected {
				t.Errorf("extractDates() found %d dates, want %d. Results: %v", len(result), tt.expected, result)
			}
		})
	}
}

func TestExtractBuyerAndSellerByPosition(t *testing.T) {
	service := NewOCRService()

	// Synthetic left-right party layout.
	t.Run("LeftRightLayout", func(t *testing.T) {
		text1 := `SYNTHETIC / 纯合成测试数据
购 名称：个人                                       销 名称：纯合成百货商店
买                                             售
方                                             方
信 统一社会信用代码/纳税人识别号：                            信 统一社会信用代码/纳税人识别号：91110000SYNTH00513`

		buyer1, seller1 := service.extractBuyerAndSellerByPosition(text1)

		if buyer1 == nil {
			t.Error("Buyer is nil, expected '个人'")
		} else if *buyer1 != "个人" {
			t.Errorf("Expected buyer '个人', got '%s'", *buyer1)
		}

		if seller1 == nil {
			t.Error("Seller is nil, expected synthetic seller")
		} else if *seller1 != "纯合成百货商店" {
			t.Errorf("Expected synthetic seller, got '%s'", *seller1)
		}
	})

	// Synthetic top-bottom party layout.
	t.Run("TopBottomLayout", func(t *testing.T) {
		text2 := `SYNTHETIC / 纯合成测试数据
    名       称: 星河先生                                             密       *99<<...
购
    纳税人识别号:                                                            ...
买
...
    名       称:云海通信服务有限公司
销
    纳税人识别号:99000000000000514X
售`

		buyer2, seller2 := service.extractBuyerAndSellerByPosition(text2)

		if buyer2 == nil {
			t.Error("Buyer is nil, expected synthetic buyer")
		} else if *buyer2 != "星河先生" {
			t.Errorf("Expected synthetic buyer, got '%s'", *buyer2)
		}

		if seller2 == nil {
			t.Error("Seller is nil, expected synthetic seller")
		} else if *seller2 != "云海通信服务有限公司" {
			t.Errorf("Expected synthetic seller, got '%s'", *seller2)
		}
	})

	// Test case 3: No markers found
	t.Run("NoMarkers", func(t *testing.T) {
		text3 := `SYNTHETIC / 纯合成测试数据
名称：纯合成测试公司`

		buyer3, seller3 := service.extractBuyerAndSellerByPosition(text3)

		// Should return nil when no markers are found
		if buyer3 != nil || seller3 != nil {
			t.Error("Expected both buyer and seller to be nil when no markers found")
		}
	})

	// Test case 4: Only buyer marker
	t.Run("OnlyBuyerMarker", func(t *testing.T) {
		text4 := `SYNTHETIC / 纯合成测试数据
购买方
名称：纯合成购买人丙`

		buyer4, seller4 := service.extractBuyerAndSellerByPosition(text4)

		if buyer4 == nil {
			t.Error("Buyer is nil, expected synthetic buyer")
		} else if *buyer4 != "纯合成购买人丙" {
			t.Errorf("Expected synthetic buyer, got '%s'", *buyer4)
		}

		// Seller should be nil
		if seller4 != nil {
			t.Errorf("Expected seller to be nil, got '%s'", *seller4)
		}
	})

	// Test case 5: Only seller marker
	t.Run("OnlySellerMarker", func(t *testing.T) {
		text5 := `SYNTHETIC / 纯合成测试数据
销售方
名称：纯合成销售公司`

		buyer5, seller5 := service.extractBuyerAndSellerByPosition(text5)

		if seller5 == nil {
			t.Error("Seller is nil, expected synthetic seller")
		} else if *seller5 != "纯合成销售公司" {
			t.Errorf("Expected synthetic seller, got '%s'", *seller5)
		}

		// Buyer should be nil
		if buyer5 != nil {
			t.Errorf("Expected buyer to be nil, got '%s'", *buyer5)
		}
	})
}

func TestParseInvoiceData_SyntheticRetailBuyerAndItems_PyMuPDFZoned(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【发票信息】
发票代码: 990000000515
发票号码: 99000515
开票日期: 2026年08月25日
校验码: 99999 00000 00515 00000
纯合成增值税电子普通发票
机器编号： 999999990515
【购买方】
买方购名称: 地  址 、电  话: 纳税人识别号: 开户行及账号: 纯合成购买人丁
货物或应税劳务、服务名称规格型号   单位数量单价
【密码区】
密码区 *9*+9-<9-99>99<99*99+-9999>
12.00金  额24.00税率3% 税  额0.72
8.00 8.00 3% 0.24
【明细】
*纯合成食品*SYNTHETIC测试乳品1.20kg(300g*4) 4X300g 组2
*纯合成服务*测试配送1
合计 ￥32.00 ￥0.96
价税合计(大写) 叁拾贰圆玖角陆分 (小写) ￥32.96
【销售方】
方售销名称: 纯合成零售有限公司
【备注/其他】
备订单号[999999990515]
注
纯合成开票人销售方:(章)`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	if data.BuyerName == nil || *data.BuyerName != "纯合成购买人丁" {
		t.Fatalf("Expected synthetic BuyerName, got %+v (src=%q)", data.BuyerName, data.BuyerNameSource)
	}
	if data.SellerName == nil || *data.SellerName != "纯合成零售有限公司" {
		t.Fatalf("Expected synthetic SellerName, got %+v (src=%q)", data.SellerName, data.SellerNameSource)
	}
	if data.Amount == nil || *data.Amount != 32.96 {
		t.Fatalf("Expected Amount 32.96, got %+v (src=%q)", data.Amount, data.AmountSource)
	}
	if data.TaxAmount == nil || *data.TaxAmount != 0.96 {
		t.Fatalf("Expected TaxAmount 0.96, got %+v (src=%q)", data.TaxAmount, data.TaxAmountSource)
	}

	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	if strings.Contains(data.Items[0].Name, "密码区") || strings.Contains(data.Items[1].Name, "密码区") {
		t.Fatalf("Unexpected password area captured as item: %+v", data.Items)
	}
	if data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 || data.Items[0].Unit != "组" {
		t.Fatalf("Unexpected first item parsed: %+v", data.Items[0])
	}
	if data.Items[1].Quantity == nil || *data.Items[1].Quantity != 1 {
		t.Fatalf("Unexpected second item parsed: %+v", data.Items[1])
	}
}

func TestParseInvoiceData_SyntheticModelCodeMergedIntoName_PyMuPDFZoned(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【发票信息】
开票日期: 发票号码: 99000000000000000516 2026年08月26日
电子发票(普通发票)
【购买方】
购买方信息名统一社会信用代码称项目名称 : 纯合成购买人戊 / 纳税人识别号规格型号 : 单位数量销售方信息名称 :
【密码区】
纯合成设备贸易有限公司
统一社会信用代码 / 纳税人识别号 : 91110000SYNTH00516
1000.00单价1000.00金  额   税率/征收率6% 60.00税  额
【明细】
*纯合成设备*测试温控设备 大型节能测试机 SYN-9000/AB12 SYN-9000/AB12 套1
合计 ￥1000.00 ￥60.00
价税合计(大写) 壹仟零陆拾圆整 (小写) ￥1060.00
【销售方】
备订单号:999999990516
注
开票人: 纯合成开票人`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.BuyerName == nil || *data.BuyerName != "纯合成购买人戊" {
		got := "<nil>"
		if data.BuyerName != nil {
			got = *data.BuyerName
		}
		t.Fatalf("Expected synthetic BuyerName, got %q (src=%q)", got, data.BuyerNameSource)
	}
	if data.SellerName == nil || *data.SellerName != "纯合成设备贸易有限公司" {
		t.Fatalf("Expected synthetic SellerName, got %+v (src=%q)", data.SellerName, data.SellerNameSource)
	}
	if data.Amount == nil || *data.Amount != 1060.00 {
		t.Fatalf("Expected Amount 1060.00, got %+v (src=%q)", data.Amount, data.AmountSource)
	}
	if data.TaxAmount == nil || *data.TaxAmount != 60.00 {
		t.Fatalf("Expected TaxAmount 60.00, got %+v (src=%q)", data.TaxAmount, data.TaxAmountSource)
	}
	if len(data.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d: %+v", len(data.Items), data.Items)
	}
	if data.Items[0].Spec == "" || data.Items[0].Spec != "SYN-9000/AB12" {
		t.Fatalf("Expected synthetic item spec, got %+v", data.Items[0])
	}
	if strings.Contains(data.Items[0].Name, "SYN-9000") {
		t.Fatalf("Expected model code peeled from item name, got %+v", data.Items[0])
	}
	if data.Items[0].Unit != "套" || data.Items[0].Quantity == nil || *data.Items[0].Quantity != 1 {
		t.Fatalf("Unexpected item parsed: %+v", data.Items[0])
	}
}

func TestParseInvoiceData_SyntheticItemNamePrefixLeakedIntoBuyer_PyMuPDFZoned(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【发票信息】
开票日期: 发票号码: 99000000000000000517 2026年08月27日
电子发票(普通发票)
【购买方】
*纯合成设备*测试温控设备前缀 小型节能测试机购买方信息名统一社会信用代码称项目名称 : 纯合成购买人己 / 纳税人识别号规格型号 : 单位数量销售方信息名称 :
【密码区】
纯合成设备销售有限公司
统一社会信用代码 / 纳税人识别号 : 91110000SYNTH00517
500.00单价1000.00金  额   税率/征收率6% 60.00税  额
【明细】
测试温控设备后缀 SYN-3500/AB12 SYN-3500/AB12 套2
合计 ￥1000.00 ￥60.00
价税合计(大写) 壹仟零陆拾圆整 (小写) ￥1060.00
【销售方】
备订单号:999999990517
注
开票人: 纯合成开票人`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.InvoiceNumber == nil || *data.InvoiceNumber != "99000000000000000517" {
		t.Fatalf("Expected synthetic InvoiceNumber, got %+v (src=%q)", data.InvoiceNumber, data.InvoiceNumberSource)
	}
	gotDate := "<nil>"
	if data.InvoiceDate != nil {
		gotDate = *data.InvoiceDate
	}
	normalizedDate := gotDate
	if gotDate != "<nil>" {
		if d, err := normalizeAnyInvoiceDate(gotDate); err == nil {
			normalizedDate = d
		}
	}
	if normalizedDate != "2026-08-27" {
		t.Fatalf("Expected InvoiceDate normalized to '2026-08-27', got %q (raw=%q src=%q)", normalizedDate, gotDate, data.InvoiceDateSource)
	}
	if data.BuyerName == nil || *data.BuyerName != "纯合成购买人己" {
		t.Fatalf("Expected synthetic BuyerName, got %+v (src=%q)", data.BuyerName, data.BuyerNameSource)
	}
	if data.SellerName == nil || *data.SellerName != "纯合成设备销售有限公司" {
		t.Fatalf("Expected synthetic SellerName, got %+v (src=%q)", data.SellerName, data.SellerNameSource)
	}
	if data.Amount == nil || *data.Amount != 1060.00 {
		t.Fatalf("Expected Amount 1060.00, got %+v (src=%q)", data.Amount, data.AmountSource)
	}
	if data.TaxAmount == nil || *data.TaxAmount != 60.00 {
		t.Fatalf("Expected TaxAmount 60.00, got %+v (src=%q)", data.TaxAmount, data.TaxAmountSource)
	}

	if len(data.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d: %+v", len(data.Items), data.Items)
	}
	if data.Items[0].Spec != "SYN-3500/AB12" || data.Items[0].Unit != "套" || data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Unexpected item parsed: %+v", data.Items[0])
	}
	if !strings.Contains(data.Items[0].Name, "测试温控设备前缀") || !strings.Contains(data.Items[0].Name, "测试温控设备后缀") {
		t.Fatalf("Expected item name to include leaked prefix + tail, got %+v", data.Items[0])
	}
	if strings.Contains(data.Items[0].Name, "购买方信息") {
		t.Fatalf("Expected buyer header fragments removed from item name, got %+v", data.Items[0])
	}
}

func TestParseInvoiceData_SyntheticTwoItemsAndGrossTotal_PyMuPDFZoned(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【发票信息】
发票号码： 99000000000000000518
开票日期： 2026年08月28日
电子发票（普通发票）
【购买方】
购买方信息统一社会信用代码/纳税人识别号： 名称： 个人销售方信息名称：
项目名称规格型号单位数量
【密码区】
统一社会信用代码/纳税人识别号： 单价纯合成耗材商店59.40 59.40金额91110000SYNTH00518 税率/征收率1% 1% 税额0.60 0.60下载次数：2
【明细】
*纯合成耗材*测试耗材甲 *纯合成耗材*测试耗材乙包包2 2 29.700000000000 29.700000000000
价税合计（大写） 合计壹佰贰拾圆整 ￥ （小写） 118.80 ￥ 120.00 ￥ 1.20
【销售方】
纯合成耗材商店`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.InvoiceNumber == nil || *data.InvoiceNumber != "99000000000000000518" {
		t.Fatalf("Expected synthetic InvoiceNumber, got %+v (src=%q)", data.InvoiceNumber, data.InvoiceNumberSource)
	}
	if data.Amount == nil || *data.Amount != 120.00 {
		t.Fatalf("Expected Amount 120.00, got %+v (src=%q)", data.Amount, data.AmountSource)
	}
	if data.TaxAmount == nil || *data.TaxAmount != 1.20 {
		t.Fatalf("Expected TaxAmount 1.20, got %+v (src=%q)", data.TaxAmount, data.TaxAmountSource)
	}

	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	for _, it := range data.Items {
		if it.Unit != "包" || it.Quantity == nil || *it.Quantity != 2 {
			t.Fatalf("Unexpected item parsed: %+v", it)
		}
	}
	if !strings.Contains(data.Items[0].Name+data.Items[1].Name, "测试耗材甲") || !strings.Contains(data.Items[0].Name+data.Items[1].Name, "测试耗材乙") {
		t.Fatalf("Expected both item names present, got %+v", data.Items)
	}
}

func TestParseInvoiceData_OneItemWithDimensionSpecAndUnit_PyMuPDFZoned(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【发票信息】
发票号码： 99000000000000000519
开票日期： 2026年08月29日
电子发票（普通发票）
【购买方】
购买方信息统一社会信用代码/纳税人识别号： 名称： 个人销售方信息名称：
项目名称规格型号单位数量
【密码区】
统一社会信用代码/纳税人识别号： 单价纯合成文具有限公司800.00金额91110000SYNTH00519 税率/征收率6% 税额48.00下载次数：1
【明细】
*纯合成组件*测试装置组合（测试部件甲、测试部件乙） 320*240*180mm 套4 200.000000000000
价税合计（大写） 合计捌佰肆拾捌圆整 ￥ 800.00 （小写） ￥ 848.00 ￥ 48.00
【销售方】
纯合成文具有限公司`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.InvoiceNumber == nil || *data.InvoiceNumber != "99000000000000000519" {
		t.Fatalf("Expected synthetic InvoiceNumber, got %+v (src=%q)", data.InvoiceNumber, data.InvoiceNumberSource)
	}
	if data.Amount == nil || *data.Amount != 848.00 {
		t.Fatalf("Expected Amount 848.00, got %+v (src=%q)", data.Amount, data.AmountSource)
	}
	if data.TaxAmount == nil || *data.TaxAmount != 48.00 {
		t.Fatalf("Expected TaxAmount 48.00, got %+v (src=%q)", data.TaxAmount, data.TaxAmountSource)
	}
	if len(data.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d: %+v", len(data.Items), data.Items)
	}
	it := data.Items[0]
	if !strings.Contains(it.Name, "纯合成组件") || !strings.Contains(it.Name, "测试装置组合") {
		t.Fatalf("Unexpected item name: %+v", it)
	}
	if it.Spec != "320×240×180mm" {
		t.Fatalf("Expected synthetic dimension spec, got %+v", it)
	}
	if it.Unit != "套" || it.Quantity == nil || *it.Quantity != 4 {
		t.Fatalf("Unexpected unit/qty: %+v", it)
	}
}

func TestParseInvoiceData_OneItemWithSimpleMLSpec_PyMuPDFZoned(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
【第1页-分区】
【发票信息】
发票号码： 99000000000000000520
开票日期： 2026年08月30日
电子发票（普通发票）
【购买方】
购买方信息统一社会信用代码/纳税人识别号： 名称： 个人销售方信息名称：
项目名称规格型号单位数量
【密码区】
统一社会信用代码/纳税人识别号： 单价纯合成饮品商贸有限公司240.00金额91110000SYNTH00520 税率/征收率6% 税额14.40下载次数：1
【明细】
*纯合成饮品*测试饮品丙99 480ml 瓶6 40.000000000000
价税合计（大写） 合计贰佰伍拾肆圆肆角 ￥ 240.00 （小写） ￥ 254.40 ￥ 14.40
【销售方】
纯合成饮品商贸有限公司`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if len(data.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d: %+v", len(data.Items), data.Items)
	}
	it := data.Items[0]
	if it.Spec != "480ml" {
		t.Fatalf("Expected spec '480ml', got %+v", it)
	}
	if it.Unit != "瓶" || it.Quantity == nil || *it.Quantity != 6 {
		t.Fatalf("Unexpected unit/qty: %+v", it)
	}
}

func TestParseInvoiceData_AirTicketItinerary_RapidOCR(t *testing.T) {
	service := NewOCRService()

	sampleText := fmt.Sprintf(`%s
纯合成旅客甲
旅客姓名
国内国际标识：国内
有效身份证件号码
SYNTHETIC-ID-AIR-01
电子发票
航空运输电子客票行程单）
纯合成税务标识
签注
Q/改期退票收费
开票状态：正常
发票号码：99000000000000000003
承运人
航班号
座位等级
日期
时间
客票级别/客票类别
客票生效日期有效截止日期免费行李
自：%s T1
纯合成航空
%s
Y
2026年08月02日
09:30
Y
20K
至:%s T2
票价
CNY 420.00
燃油附加费
CNY 24.44
增值税税率
%%6
增值税税额
CNY 12.34
民航发展基金
CNY 12.34
其他税费
CNY 0.00
合计
CNY 456.78
电子客票号码：SYNTHETIC-AIR-0001
验证码：9001
提示信息：
保险费：SYNTHETIC
销售网点代号：SYNTHETIC-AIR-DESK
填开单位：纯合成航空服务有限公司
填开日期：2026年08月01日
购买方名称：个人
统一社会信用代码/纳税人识别号：`, regressionfixtures.SyntheticMarker, regressionfixtures.SyntheticAirOrigin, regressionfixtures.SyntheticFlightNo, regressionfixtures.SyntheticAirDest)

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.InvoiceNumber == nil || *data.InvoiceNumber != "99000000000000000003" {
		t.Fatalf("Expected synthetic InvoiceNumber, got %+v (src=%q)", data.InvoiceNumber, data.InvoiceNumberSource)
	}
	if data.InvoiceDate == nil || *data.InvoiceDate != "2026年08月01日" {
		t.Fatalf("Expected synthetic InvoiceDate, got %+v (src=%q)", data.InvoiceDate, data.InvoiceDateSource)
	}
	if data.BuyerName == nil || *data.BuyerName != "纯合成旅客甲" {
		got := "<nil>"
		if data.BuyerName != nil {
			got = *data.BuyerName
		}
		t.Fatalf("Expected synthetic BuyerName, got %q (src=%q)", got, data.BuyerNameSource)
	}
	if data.SellerName == nil || *data.SellerName != "纯合成航空服务有限公司" {
		t.Fatalf("Expected synthetic SellerName, got %+v (src=%q)", data.SellerName, data.SellerNameSource)
	}
	if data.Amount == nil || *data.Amount != 456.78 {
		t.Fatalf("Expected Amount 456.78, got %+v (src=%q)", data.Amount, data.AmountSource)
	}
	if data.TaxAmount == nil || *data.TaxAmount != 12.34 {
		t.Fatalf("Expected TaxAmount 12.34, got %+v (src=%q)", data.TaxAmount, data.TaxAmountSource)
	}
	if len(data.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d: %+v", len(data.Items), data.Items)
	}
	it := data.Items[0]
	if it.Unit != "次" || it.Quantity == nil || *it.Quantity != 1 {
		t.Fatalf("Unexpected item unit/qty: %+v", it)
	}
	if !strings.Contains(it.Name, regressionfixtures.SyntheticFlightNo) || !strings.Contains(it.Name, regressionfixtures.SyntheticAirOrigin) || !strings.Contains(it.Name, regressionfixtures.SyntheticAirDest) {
		t.Fatalf("Unexpected item name: %+v", it)
	}
}

func TestParseInvoiceData_SpaceSeparatedDate(t *testing.T) {
	service := NewOCRService()

	// Synthetic case with a space-separated date.
	sampleText := `SYNTHETIC / 纯合成测试数据
电子发票（普通发票）
开票日期: 2026 年08 月28 日
名       称: 纯合成购买人庚
购
买
方
纳税人识别号:
名       称:纯合成通信服务有限公司
销
售
方
纳税人识别号:91110000SYNTH00521
价税合计（小写）¥42.36`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice date - should parse space-separated format
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2026年08月28日" {
		t.Errorf("Expected synthetic InvoiceDate, got '%s'", *data.InvoiceDate)
	}

	// Test buyer name - should extract using position-based method
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "纯合成购买人庚" {
		t.Errorf("Expected synthetic BuyerName, got '%s'", *data.BuyerName)
	}

	// Test seller name - should extract using position-based method
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "纯合成通信服务有限公司" {
		t.Errorf("Expected synthetic SellerName, got '%s'", *data.SellerName)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 42.36
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}
}

func TestMergeExtractionResults(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name          string
		pdftotextText string
		ocrText       string
		expectOCR     bool // true if we expect OCR result to be used as base
		description   string
	}{
		{
			name: "OCR has more Chinese - use OCR",
			pdftotextText: `SYNTHETIC
2026   08   02
*99<<*>99/9>99/*99999<>*>99
¥42.36
91110000SYNTH00522`,
			ocrText: `SYNTHETIC / 纯合成测试数据
电子发票（普通发票）
发票号码：99000000000000000522
开票日期：2026年08月02日
金额：¥42.36
销售方名称：纯合成销售公司
购买方名称：个人`,
			expectOCR:   true,
			description: "When OCR has Chinese text and pdftotext doesn't, use OCR",
		},
		{
			name: "pdftotext has sufficient Chinese - use pdftotext",
			pdftotextText: `SYNTHETIC / 纯合成测试数据
电子发票（普通发票）
发票号码：99000000000000000523
开票日期：2026年08月03日
销售方名称：纯合成销售公司
购买方名称：纯合成购买公司
价税合计（小写）¥84.72`,
			ocrText: `SYNTHETIC / 纯合成测试数据
电子发票（普通发票）
发票号码：99000000000000000523`,
			expectOCR:   false,
			description: "When pdftotext has more Chinese, use pdftotext",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.mergeExtractionResults(tt.pdftotextText, tt.ocrText)

			// Check which source was used based on Chinese character ratio
			ocrRatio := service.getChineseCharRatio(tt.ocrText)
			pdfRatio := service.getChineseCharRatio(tt.pdftotextText)

			t.Logf("OCR Chinese ratio: %.2f%%, pdftotext Chinese ratio: %.2f%%", ocrRatio*100, pdfRatio*100)

			if tt.expectOCR {
				if result != tt.ocrText {
					t.Errorf("Expected OCR result to be used, but got different result")
				}
			} else {
				if result != tt.pdftotextText {
					t.Errorf("Expected pdftotext result to be used, but got different result")
				}
			}
		})
	}
}

// TestParsePaymentScreenshot_NegativeAmount tests parsing negative amounts
func TestParsePaymentScreenshot_NegativeAmount(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name           string
		text           string
		expectedAmount float64
	}{
		{
			name:           "Negative amount -42.36",
			text:           "SYNTHETIC / 纯合成测试数据\n支付成功\n-42.36\n商户：纯合成测试店",
			expectedAmount: 42.36,
		},
		{
			name:           "Negative amount with symbol -¥42.36",
			text:           "SYNTHETIC / 纯合成测试数据\n支付成功\n-¥42.36\n商户：纯合成测试店",
			expectedAmount: 42.36,
		},
		{
			name:           "Standard amount ¥42.36",
			text:           "SYNTHETIC / 纯合成测试数据\n支付成功\n¥42.36\n商户：纯合成测试店",
			expectedAmount: 42.36,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := service.ParsePaymentScreenshot(tt.text)
			if err != nil {
				t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
			}

			if data.Amount == nil {
				t.Error("Amount is nil")
			} else if *data.Amount != tt.expectedAmount {
				t.Errorf("Expected amount %.2f, got %.2f", tt.expectedAmount, *data.Amount)
			}
			if data.PrettyText == "" || !strings.Contains(data.PrettyText, "【整理摘要】") {
				t.Fatalf("Expected PrettyText to be set, got: %q", data.PrettyText)
			}
		})
	}
}

// TestRemoveChineseSpaces tests the removeChineseSpaces function
func TestRemoveChineseSpaces(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Spaces between Chinese characters",
			input:    "支 付 时 间",
			expected: "支付时间",
		},
		{
			name:     "Mixed Chinese and numbers with spaces",
			input:    "2026 年 08 月 23 日",
			expected: "2026年08月23日",
		},
		{
			name:     "Preserve spaces between English words",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "Mixed content",
			input:    "商 户 全 称 Test Company",
			expected: "商户全称 Test Company",
		},
		{
			name:     "No spaces",
			input:    "支付时间",
			expected: "支付时间",
		},
		{
			name:     "Multiple spaces between Chinese",
			input:    "支  付  时  间",
			expected: "支付时间",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeChineseSpaces(tt.input)
			if result != tt.expected {
				t.Errorf("removeChineseSpaces(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
