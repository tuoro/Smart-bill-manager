package services

import (
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
