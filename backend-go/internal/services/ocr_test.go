package services

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestParseInvoiceData_NewlineFormat(t *testing.T) {
	service := NewOCRService()

	// Sample OCR text with newline-separated fields
	sampleText := `电子发票（普通发票）
发票号码：
25312000000336194167
开票日期：
2025年10月21日
购
买
方
信
息
统一社会信用代码/纳税人识别号：
名称：
个人
销
售
方
信
息
统一社会信用代码/纳税人识别号：
92310109MA1KMFLM1K
名称：
上海市虹口区鹏侠百货商店
项目名称
规格型号
单 位
数 量
单 价
金 额
税率/征收率
税 额
*酒*白酒 汾酒青花30
53°*6
瓶
2
841.584158415842
1683.17
1%
16.83
*酒*葡萄酒 奔富407
750ml*6
瓶
2
683.168316831683
1366.34
1%
13.66
合
计
¥
3049.51
¥
30.49
价税合计（大写）
叁仟零捌拾圆整
（小写）
¥
3080.00
备
注
开票人：
江祜璆`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "25312000000336194167" {
		t.Errorf("Expected InvoiceNumber '25312000000336194167', got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2025年10月21日" {
		t.Errorf("Expected InvoiceDate '2025年10月21日', got '%s'", *data.InvoiceDate)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 3080.00
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "上海市虹口区鹏侠百货商店" {
		t.Errorf("Expected SellerName '上海市虹口区鹏侠百货商店', got '%s'", *data.SellerName)
	}

	// Test buyer name
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "个人" {
		t.Errorf("Expected BuyerName '个人', got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_TraditionalFormat(t *testing.T) {
	service := NewOCRService()

	// Test traditional format (fields on same line) to ensure backward compatibility
	sampleText := `电子发票（普通发票）
发票号码：12345678901234567890
开票日期：2024年12月01日
销售方名称：测试公司
购买方名称：购买公司
价税合计（小写）¥1234.56`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "12345678901234567890" {
		t.Errorf("Expected InvoiceNumber '12345678901234567890', got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2024年12月01日" {
		t.Errorf("Expected InvoiceDate '2024年12月01日', got '%s'", *data.InvoiceDate)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 1234.56
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "测试公司" {
		t.Errorf("Expected SellerName '测试公司', got '%s'", *data.SellerName)
	}

	// Test buyer name
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "购买公司" {
		t.Errorf("Expected BuyerName '购买公司', got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_RealWorldFormat(t *testing.T) {
	service := NewOCRService()

	// EXACT OCR format from real invoice - labels and data are completely separated
	sampleText := `电子发票（普通发票）
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
25312000000336194167
2025年10月21日
个人
上海市虹口区鹏侠百货商店
92310109MA1KMFLM1K
¥
3049.51
¥
30.49
叁仟零捌拾圆整
¥
3080.00
江祜璆
江祜璆
*酒*白酒 汾酒青花30
53°*6
1%
瓶
1683.17
16.83
841.584158415842
2
*酒*葡萄酒 奔富407
750ml*6
1%
瓶
1366.34
13.66
683.168316831683
2`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil")
	} else if *data.InvoiceNumber != "25312000000336194167" {
		t.Errorf("Expected InvoiceNumber '25312000000336194167', got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date - SHOULD extract the date from OCR text
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil - should extract '2025年10月21日'")
	} else if *data.InvoiceDate != "2025年10月21日" {
		t.Errorf("Expected InvoiceDate '2025年10月21日', got '%s'", *data.InvoiceDate)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 3080.00
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name - SHOULD extract '上海市虹口区鹏侠百货商店'
	if data.SellerName == nil {
		t.Error("SellerName is nil - should extract '上海市虹口区鹏侠百货商店'")
	} else if *data.SellerName != "上海市虹口区鹏侠百货商店" {
		t.Errorf("Expected SellerName '上海市虹口区鹏侠百货商店', got '%s'", *data.SellerName)
	}

	// Test buyer name - SHOULD extract '个人' NOT '名称：'
	if data.BuyerName == nil {
		t.Error("BuyerName is nil - should extract '个人'")
	} else if *data.BuyerName == "名称：" || *data.BuyerName == "名称:" {
		t.Errorf("BuyerName incorrectly extracted as '%s' - should be '个人'", *data.BuyerName)
	} else if *data.BuyerName != "个人" {
		t.Errorf("Expected BuyerName '个人', got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_PreferTaxInclusiveAmount(t *testing.T) {
	service := NewOCRService()

	// Some invoices contain both:
	// - 合计金额(小写): tax-exclusive subtotal
	// - 价税合计(小写): tax-inclusive total (desired)
	sampleText := `合计金额(小写)：100.00
价税合计(小写)：107.79`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	if data.Amount == nil {
		t.Fatal("Amount is nil, expected 107.79")
	}
	if *data.Amount != 107.79 {
		t.Fatalf("Expected Amount 107.79, got %.2f", *data.Amount)
	}
}

func TestParseInvoiceData_ItemsExtraction(t *testing.T) {
	service := NewOCRService()

	sampleText := `货物或应税劳务、服务名称
规格型号 单位 数量 单价 金额 税率 税额
*乳制品*希腊式酸奶 1.23kg(410g*3)
3X410g
组
2
53.01
106.02
13%
13.78
*日用杂品*包装费配送费
1.77
1.77
13%
0.23
价税合计(小写) ¥121.80`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(data.Items))
	}
	if data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Expected first item quantity 2, got %+v", data.Items[0].Quantity)
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
	sampleText := `货物或应税劳务、服务名称
规 格 型 号
单 位
数 量
单 价
金 额
税 率
税 额
厦门增值税电子普通发票
机器编号：661911919489
发票代码：035021700111
发票号码：31126517
开票日期：2025 年 11 月 16 日
校 验 码：59872 35946 41356 16868
*乳制品*Member's Mark 希腊式酸奶 1.23kg(410g*3)
3X410g
组
2
53.01
106.02
13%
13.78
*日用杂品*包装费配送费
1
1.77
1.77
13%
0.23
价税合计(小写) ¥121.80`

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
		if it.Name == "税额" || it.Name == "厦门增值税电子普通发票" {
			t.Fatalf("Unexpected meta/header captured as item: %+v", data.Items)
		}
	}
}

func TestParseInvoiceData_PyMuPDFZoned_SellerAndItemUnitQty(t *testing.T) {
	service := NewOCRService()

	// PyMuPDF zoned layout: section headers are present, and some item lines may merge unit+qty into the name token.
	// Also ensure we can recover the full seller company name from the tax-id line even if a shorter nearby name exists.
	sampleText := `【第1页-分区】
【明细】
货物或应税劳务、服务名称
规格型号
单位
数量
单价
金额
税率
税额
*电信服务*话费充值元1
200.00
200.00
*
*
合计 价税合计(大写)
(小写) ¥200.00
【销售方】
上海有限公司
方 售 销 名 称: 开户行及账号: 地 址、电 话: 纳税人识别号: 中国移动通信集团上海有限公司91310000132149237G 上海市长寿路200号13800210021`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.SellerName == nil || *data.SellerName != "中国移动通信集团上海有限公司" {
		t.Fatalf("Expected seller name %q, got %+v (source=%q conf=%v)", "中国移动通信集团上海有限公司", data.SellerName, data.SellerNameSource, data.SellerNameConfidence)
	}

	if len(data.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d: %+v", len(data.Items), data.Items)
	}
	if !strings.Contains(data.Items[0].Name, "电信服务") || !strings.Contains(data.Items[0].Name, "话费充值") {
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

	sampleText := `【第1页-分区】
【发票信息】
发票号码： 25312000000341067672
开票日期： 2025年10月24日
电子发票（普通发票）
【购买方】
购买方信息统一社会信用代码/纳税人识别号： 名称： 个人销售方信息名称：
项目名称规格型号单位数量
【密码区】
统一社会信用代码/纳税人识别号： 单价上海市虹口区鹏侠百货商店1683.17金额92310109MA1KMFLM1K 税率/征收率1% 税额16.83下载次数：1
【明细】
*酒*白酒汾酒30 53°*500ml 瓶2 841.584158415842
价税合计（大写） 合计壹仟柒佰圆整 ￥ 1683.17 （小写） ￥ 1700.00 ￥ 16.83
【备注/其他】
开票人： 江祜璆`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	if data.BuyerName == nil || *data.BuyerName != "个人" {
		t.Fatalf("Expected buyer %q, got %+v (source=%q conf=%v)", "个人", data.BuyerName, data.BuyerNameSource, data.BuyerNameConfidence)
	}
	if data.SellerName == nil {
		t.Fatalf("Expected seller %q, got nil", "上海市虹口区鹏侠百货商店")
	}
	if *data.SellerName != "上海市虹口区鹏侠百货商店" {
		t.Fatalf("Expected seller %q, got %q (source=%q conf=%v)", "上海市虹口区鹏侠百货商店", *data.SellerName, data.SellerNameSource, data.SellerNameConfidence)
	}

	if len(data.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d: %+v", len(data.Items), data.Items)
	}
	it := data.Items[0]
	if !strings.Contains(it.Name, "白酒") || !strings.Contains(it.Name, "汾酒") {
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

	sampleText := `【第1页-分区】
【发票信息】
发票号码： 25312000000336194167
开票日期： 2025年10月21日
电子发票（普通发票）
【购买方】
购买方信息统一社会信用代码/纳税人识别号： 名称： 个人销售方信息名称：
项目名称规格型号单位数量
【密码区】
统一社会信用代码/纳税人识别号： 单价上海市虹口区鹏侠百货商店1683.17 1366.34金额92310109MA1KMFLM1K 税率/征收率1% 1% 税额16.83 13.66下载次数：1
【明细】
*酒*白酒汾酒青花30 *酒*葡萄酒奔富407 53°*6 750ml*6瓶瓶2 2 841.584158415842 683.168316831683
价税合计（大写） 合计叁仟零捌拾圆整 ￥ 3049.51 （小写） ￥ 3080.00 ￥ 30.49
【备注/其他】
开票人： 江祜璆`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.Amount == nil || *data.Amount != 3080 {
		t.Fatalf("Expected amount 3080, got %+v (src=%q)", data.Amount, data.AmountSource)
	}
	if data.TaxAmount == nil || fmt.Sprintf("%.2f", *data.TaxAmount) != "30.49" {
		t.Fatalf("Expected tax_amount 30.49, got %+v (src=%q)", data.TaxAmount, data.TaxAmountSource)
	}
	if len(data.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d: %+v", len(data.Items), data.Items)
	}
	if !strings.Contains(data.Items[0].Name, "汾酒") || data.Items[0].Unit != "瓶" || data.Items[0].Quantity == nil || *data.Items[0].Quantity != 2 {
		t.Fatalf("Unexpected item 0: %+v", data.Items[0])
	}
	if data.Items[0].Spec == "" || !strings.Contains(data.Items[0].Spec, "53") {
		t.Fatalf("Unexpected item 0 spec: %+v", data.Items[0])
	}
	if !strings.Contains(data.Items[1].Name, "奔富") || data.Items[1].Unit != "瓶" || data.Items[1].Quantity == nil || *data.Items[1].Quantity != 2 {
		t.Fatalf("Unexpected item 1: %+v", data.Items[1])
	}
	if data.Items[1].Spec == "" || !strings.Contains(strings.ToLower(data.Items[1].Spec), "ml") {
		t.Fatalf("Unexpected item 1 spec: %+v", data.Items[1])
	}
}

func TestParseInvoiceData_ItemsExtraction_PDFTextStopsBeforePartyInfo(t *testing.T) {
	service := NewOCRService()

	sampleText := `货物或应税劳务、服务名称
规格型号   单位
数 量
单 价
金 额
税率
税额
*乳制品*Member's Mark 希腊式酸奶 1.23kg(410g*3)
3X410g
组
2
53.01
106.02
13%
13.78
*日用杂品*包装费配送费
1
1.77
1.77
13%
0.23
名称:
纳税人识别号:
地址、电话:
开户行及账号:
沃尔玛（厦门）商业零售有限公司
收款人：沃尔玛
复核：黄寿全
开票人：林燕红
订单号[3087538259083065845]
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
	sampleText := `项目名称
规格型号
单 位
数 量
单 价
金 额
税率/征收率
税 额
*酒*白酒 汾酒青花30
53°*6
1%
瓶
1683.17
16.83
841.584158415842
2
*酒*葡萄酒 奔富407
750ml*6
1%
瓶
1366.34
13.66
683.168316831683
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
	sampleText := `电子发票（普通发票）
发票号码：
25312000000336194167
开票日期：
2025年10月21日
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
上海市虹口区鹏侠百货商店

项目名称
规 格 型 号
单 位
数 量
单 价
金 额
税率/征收率
税 额
*酒*白酒 汾酒青花30
53°*6
1%
瓶
1683.17
16.83
841.584158415842
2
*酒*葡萄酒 奔富407
750ml*6
1%
瓶
1366.34
13.66
683.168316831683
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
	// - "价税合计(大写)" then "壹佰..." then "（小写）￥121.80"
	// - "名称：xxx" on the same line
	// Ensure these are not captured as line items.
	sampleText := `货物或应税劳务、服务名称
规格型号   单位
数 量
单 价
金 额
税率
税额
*乳制品*Member's Mark 希腊式酸奶 1.23kg(410g*3)
3X410g
组
2
53.01
106.02
13%
13.78
*日用杂品*包装费配送费
1
1.77
1.77
13%
0.23
价税合计(大写)
壹佰贰拾壹圆捌角
（小写）￥121.80
名称：沃尔玛（厦门）商业零售有限公司
纳税人识别号：913502005750394918`

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

	// A real-world image OCR case where:
	// - "合计" is split across lines ("合" then "计")
	// - seller fields are split ("名" then "称：xxx")
	// Ensure we still extract both items and stop before seller/footer blocks.
	sampleText := `发票代码：035021700111
厦门增值税电子普通发票
发票号码：31126517
开票日期：2025年11月16日
校验码：59872359464135616868
购
名
称：邬先生
货物或应税劳务、服务名称
规格型号
单位
数量
单价
金额
税率
税额
*乳制品*Member'sMark希腊式酸奶1.23kg（410g*3)
3X410g
组
2
53.01
106.02
13%
13.78
*日用杂品*包装费配送费
1.77
1.77
13%
0.23
合
计
￥107.79
￥14.01
价税合计(大写)
壹佰贰拾壹圆捌角
（小写）￥121.80
名
称：沃尔玛（厦门）商业零售有限公司
订单号[3087538259083065845]
销售方
纳税人识别号：913502005750394918
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
	if !strings.Contains(data.Items[0].Name, "乳制品") {
		t.Fatalf("Expected first item to contain 乳制品, got %q", data.Items[0].Name)
	}
	if !strings.Contains(data.Items[0].Name, "Member's Mark") {
		t.Fatalf("Expected first item to contain \"Member's Mark\", got %q", data.Items[0].Name)
	}
	if !strings.Contains(data.Items[1].Name, "包装费配送费") {
		t.Fatalf("Expected second item to contain 包装费配送费, got %q", data.Items[1].Name)
	}
	for _, it := range data.Items {
		if strings.Contains(it.Name, "价税合计") || strings.Contains(it.Name, "沃尔玛（厦门）商业零售有限公司") {
			t.Fatalf("Unexpected non-item captured as item: %+v", data.Items)
		}
	}
}

func TestExtractPartyFromROICandidate_NameLabels(t *testing.T) {
	buyerText := `购买方名称：张三
购买方纳税人识别号：91310000132149237G
地址电话：上海`
	buyerName, buyerTax := extractPartyFromROICandidate(buyerText, "buyer")
	if buyerName != "张三" {
		t.Fatalf("Expected buyer name 张三, got %q", buyerName)
	}
	if buyerTax != "91310000132149237G" {
		t.Fatalf("Expected buyer tax 91310000132149237G, got %q", buyerTax)
	}

	sellerText := `销售方名称：测试公司
销售方纳税人识别号：91310000132149237G`
	sellerName, sellerTax := extractPartyFromROICandidate(sellerText, "seller")
	if sellerName != "测试公司" {
		t.Fatalf("Expected seller name 测试公司, got %q", sellerName)
	}
	if sellerTax != "91310000132149237G" {
		t.Fatalf("Expected seller tax 91310000132149237G, got %q", sellerTax)
	}
}

func TestParseInvoiceData_DidiInvoice(t *testing.T) {
	service := NewOCRService()

	// Real OCR text from Didi invoice with 8-digit invoice number and full-width ￥ symbol
	sampleText := `合
计
备
注
上海增值税电子普通发票
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
￥19.01
￥0.57
*运输服务*客运服务费
无
次
1
24
24.00
3%
0.72
*运输服务*客运服务费
-4.99
3%
-0.15
499098504973
壹拾玖圆不角扌分
杜洪亮
张唯
于秋红
03<5>/42>5541143639+79737-<*
59765*+75/>89+/47732281674/2
15<5>/42>5541143631>239-3/<5
+7*9>//<310193<219+4/8-4528-
个人
上海滴滴畅行科技有限公司
91310114MA1GW61J6U
上海市静安区万荣路777弄12号202-7室010-83456275
招商银行股份有限公司上海东方支行121932981110606
031002300211
70739906
2024年07月06日
07908 63166 90581 33371
￥19.58`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice number - should extract 8-digit number
	if data.InvoiceNumber == nil {
		t.Error("InvoiceNumber is nil - should extract '70739906'")
	} else if *data.InvoiceNumber != "70739906" {
		t.Errorf("Expected InvoiceNumber '70739906', got '%s'", *data.InvoiceNumber)
	}

	// Test invoice date
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil")
	} else if *data.InvoiceDate != "2024年07月06日" {
		t.Errorf("Expected InvoiceDate '2024年07月06日', got '%s'", *data.InvoiceDate)
	}

	// Test amount - should extract 19.58 with full-width ￥ symbol
	if data.Amount == nil {
		t.Error("Amount is nil - should extract '19.58'")
	} else {
		expectedAmount := 19.58
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test seller name
	if data.SellerName == nil {
		t.Error("SellerName is nil")
	} else if *data.SellerName != "上海滴滴畅行科技有限公司" {
		t.Errorf("Expected SellerName '上海滴滴畅行科技有限公司', got '%s'", *data.SellerName)
	}

	// Test buyer name
	if data.BuyerName == nil {
		t.Error("BuyerName is nil")
	} else if *data.BuyerName != "个人" {
		t.Errorf("Expected BuyerName '个人', got '%s'", *data.BuyerName)
	}
}

func TestParseInvoiceData_DidiInvoice_PyMuPDFZoned(t *testing.T) {
	service := NewOCRService()

	sampleText := `【第1页-分区】
【发票信息】
发票代码: 发票号码: 开票日期: 校验码: 031002300211 70739906 2024 07908 63166 90581 33371年07月06日
上海增值税电子普通发票
机器编号: 499098504973
【购买方】
购买方名 称: 纳税人识别号: 地址、 开户行及账号: 电话: 个人
货物或应税劳务、服务名称规格型号单位数量
【明细】
* * 运输服务运输服务 * * 客运服务费客运服务费无次1
价税合计（大写） 合计壹拾玖圆伍角捌分 ￥ 19.01 （小写） ￥ 19.58 ￥ 0.57
【销售方】
销售方收款人: 名 称: 纳税人识别号: 地址、 开户行及账号: 电话: 杜洪亮上海滴滴畅行科技有限公司91310114MA1GW61J6U
【备注/其他】
于秋红 销售方:（章）`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}
	if data.InvoiceDate == nil || *data.InvoiceDate != "2024年07月06日" {
		t.Fatalf("Expected InvoiceDate %q, got %+v (src=%q)", "2024年07月06日", data.InvoiceDate, data.InvoiceDateSource)
	}
	if data.BuyerName == nil || *data.BuyerName != "个人" {
		t.Fatalf("Expected BuyerName %q, got %+v (src=%q)", "个人", data.BuyerName, data.BuyerNameSource)
	}
	if data.SellerName == nil || *data.SellerName != "上海滴滴畅行科技有限公司" {
		t.Fatalf("Expected SellerName %q, got %+v (src=%q)", "上海滴滴畅行科技有限公司", data.SellerName, data.SellerNameSource)
	}
	if data.Amount == nil || fmt.Sprintf("%.2f", *data.Amount) != "19.58" {
		t.Fatalf("Expected Amount 19.58, got %+v (src=%q)", data.Amount, data.AmountSource)
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
	validText := "上海增值税电子普通发票 发票号码：12345678"
	if service.isGarbledText(validText) {
		t.Error("Valid Chinese text incorrectly detected as garbled")
	}

	// Test valid English text
	validEnglishText := "Invoice Number: 12345678 Amount: $100.00"
	if service.isGarbledText(validEnglishText) {
		t.Error("Valid English text incorrectly detected as garbled")
	}

	// Test garbled text (from problem statement)
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
			text:     "合计 ¥3049.51 税额 ¥30.49 总计 ¥3080.00",
			expected: 3,
		},
		{
			name:     "Amount with full-width symbol",
			text:     "价税合计（小写）￥19.58",
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
			text:     "纳税人识别号：91310000132149237G",
			expected: 1,
		},
		{
			name:     "Single 20-char tax ID",
			text:     "统一社会信用代码：92310109MA1KMFLM1K",
			expected: 1,
		},
		{
			name:     "Multiple tax IDs",
			text:     "销售方：91310000132149237G 购买方：92310109MA1KMFLM1K",
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
			text:     "开票日期：2025年07月02日",
			expected: 1,
		},
		{
			name:     "Space-separated format",
			text:     "日期：2025 07 02",
			expected: 1,
		},
		{
			name:     "Dash-separated format",
			text:     "2025-07-02",
			expected: 1,
		},
		{
			name:     "Multiple dates",
			text:     "开票日期：2025年07月02日 到期日：2025年08月02日",
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

	// Test case 1: Left-right layout (buyer left, seller right) - Invoice One from problem statement
	t.Run("LeftRightLayout", func(t *testing.T) {
		text1 := `购 名称：个人                                       销 名称：上海市虹口区鹏侠百货商店
买                                             售
方                                             方
信 统一社会信用代码/纳税人识别号：                            信 统一社会信用代码/纳税人识别号：92310109MA1KMFLM1K`

		buyer1, seller1 := service.extractBuyerAndSellerByPosition(text1)

		if buyer1 == nil {
			t.Error("Buyer is nil, expected '个人'")
		} else if *buyer1 != "个人" {
			t.Errorf("Expected buyer '个人', got '%s'", *buyer1)
		}

		if seller1 == nil {
			t.Error("Seller is nil, expected '上海市虹口区鹏侠百货商店'")
		} else if *seller1 != "上海市虹口区鹏侠百货商店" {
			t.Errorf("Expected seller '上海市虹口区鹏侠百货商店', got '%s'", *seller1)
		}
	})

	// Test case 2: Top-bottom layout (buyer top, seller bottom) - Invoice Two from problem statement
	t.Run("TopBottomLayout", func(t *testing.T) {
		text2 := `    名       称: 武亚峰                                             密       *14<<...
购
    纳税人识别号:                                                            ...
买
...
    名       称:中国移动通信集团上海有限公司                                        业务流水号...
销
    纳税人识别号:91310000132149237G
售`

		buyer2, seller2 := service.extractBuyerAndSellerByPosition(text2)

		if buyer2 == nil {
			t.Error("Buyer is nil, expected '武亚峰'")
		} else if *buyer2 != "武亚峰" {
			t.Errorf("Expected buyer '武亚峰', got '%s'", *buyer2)
		}

		if seller2 == nil {
			t.Error("Seller is nil, expected '中国移动通信集团上海有限公司'")
		} else if *seller2 != "中国移动通信集团上海有限公司" {
			t.Errorf("Expected seller '中国移动通信集团上海有限公司', got '%s'", *seller2)
		}
	})

	// Test case 3: No markers found
	t.Run("NoMarkers", func(t *testing.T) {
		text3 := `名称：测试公司`

		buyer3, seller3 := service.extractBuyerAndSellerByPosition(text3)

		// Should return nil when no markers are found
		if buyer3 != nil || seller3 != nil {
			t.Error("Expected both buyer and seller to be nil when no markers found")
		}
	})

	// Test case 4: Only buyer marker
	t.Run("OnlyBuyerMarker", func(t *testing.T) {
		text4 := `购买方
名称：张三`

		buyer4, seller4 := service.extractBuyerAndSellerByPosition(text4)

		if buyer4 == nil {
			t.Error("Buyer is nil, expected '张三'")
		} else if *buyer4 != "张三" {
			t.Errorf("Expected buyer '张三', got '%s'", *buyer4)
		}

		// Seller should be nil
		if seller4 != nil {
			t.Errorf("Expected seller to be nil, got '%s'", *seller4)
		}
	})

	// Test case 5: Only seller marker
	t.Run("OnlySellerMarker", func(t *testing.T) {
		text5 := `销售方
名称：某某公司`

		buyer5, seller5 := service.extractBuyerAndSellerByPosition(text5)

		if seller5 == nil {
			t.Error("Seller is nil, expected '某某公司'")
		} else if *seller5 != "某某公司" {
			t.Errorf("Expected seller '某某公司', got '%s'", *seller5)
		}

		// Buyer should be nil
		if buyer5 != nil {
			t.Errorf("Expected buyer to be nil, got '%s'", *buyer5)
		}
	})
}

func TestParseInvoiceData_SpaceSeparatedDate(t *testing.T) {
	service := NewOCRService()

	// Test case for Invoice Two from problem statement with space-separated date
	sampleText := `电子发票（普通发票）
开票日期: 2025 年07 月02 日
名       称: 武亚峰
购
买
方
纳税人识别号:
名       称:中国移动通信集团上海有限公司
销
售
方
纳税人识别号:91310000132149237G
价税合计（小写）¥100.00`

	data, err := service.ParseInvoiceData(sampleText)
	if err != nil {
		t.Fatalf("ParseInvoiceData returned error: %v", err)
	}

	// Test invoice date - should parse space-separated format
	if data.InvoiceDate == nil {
		t.Error("InvoiceDate is nil - should extract '2025年07月02日'")
	} else if *data.InvoiceDate != "2025年07月02日" {
		t.Errorf("Expected InvoiceDate '2025年07月02日', got '%s'", *data.InvoiceDate)
	}

	// Test buyer name - should extract using position-based method
	if data.BuyerName == nil {
		t.Error("BuyerName is nil - should extract '武亚峰'")
	} else if *data.BuyerName != "武亚峰" {
		t.Errorf("Expected BuyerName '武亚峰', got '%s'", *data.BuyerName)
	}

	// Test seller name - should extract using position-based method
	if data.SellerName == nil {
		t.Error("SellerName is nil - should extract '中国移动通信集团上海有限公司'")
	} else if *data.SellerName != "中国移动通信集团上海有限公司" {
		t.Errorf("Expected SellerName '中国移动通信集团上海有限公司', got '%s'", *data.SellerName)
	}

	// Test amount
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 100.00
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
			pdftotextText: `2025   07   02
*14<<*>07/6>27/*88780<>*>45
¥200.00
91310000132149237G`,
			ocrText: `电子发票（普通发票）
发票号码：25312000000336194167
开票日期：2025年07月02日
金额：¥200.00
销售方名称：上海公司
购买方名称：个人`,
			expectOCR:   true,
			description: "When OCR has Chinese text and pdftotext doesn't, use OCR",
		},
		{
			name: "pdftotext has sufficient Chinese - use pdftotext",
			pdftotextText: `电子发票（普通发票）
发票号码：12345678901234567890
开票日期：2024年12月01日
销售方名称：测试公司
购买方名称：购买公司
价税合计（小写）¥1234.56`,
			ocrText: `电子发票（普通发票）
发票号码：12345678901234567890`,
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
			name:           "Negative amount -1700.00",
			text:           "支付成功\n-1700.00\n商户：测试店",
			expectedAmount: 1700.00,
		},
		{
			name:           "Negative amount with symbol -¥1700.00",
			text:           "支付成功\n-¥1700.00\n商户：测试店",
			expectedAmount: 1700.00,
		},
		{
			name:           "Standard amount ¥1700.00",
			text:           "支付成功\n¥1700.00\n商户：测试店",
			expectedAmount: 1700.00,
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
			input:    "2025 年 10 月 23 日",
			expected: "2025年10月23日",
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
