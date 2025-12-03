package services

import (
	"os/exec"
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
			expected: 0.5,
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
