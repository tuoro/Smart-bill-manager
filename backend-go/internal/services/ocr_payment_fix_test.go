package services

import (
	"strings"
	"testing"
)

// TestParsePaymentScreenshot_ProblemStatementCase tests the exact case from the problem statement
func TestParsePaymentScreenshot_ProblemStatementCase(t *testing.T) {
	service := NewOCRService()

	// Exact OCR text from the problem statement
	sampleText := `14:59 回 怨 5501l| @
主 全 部 账 单
当 心
A
海 烟 烟 行

当 前 状 态 支 付 成 功

支 付 时 间 2025 年 10 月 23 日 14:59:46

商 品 海 烟 烟 行 ( 上 海 郡 徕 实 业 有 限 公 司
910360)

商 户 全 称 上 海 郡 徕 实 业 有 限 公 司

收 单 机 构 通 联 支 付 网 络 服 务 股 份 有 限 公 司
由 中 国 银 联 股 份 有 限 公 司 提 供 收 款 清 算
服 务

支 付 方 式 招 商 银 行 信 用 卡 (2506)
由 网 联 清 算 有 限 公 司 提 供 付 款 清 算 服 务

交 易 单 号 4200002966202510230090527049

商 户 单 号 251023116574060365`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	// Test merchant extraction - should extract "海烟烟行" from "商品" field
	if data.Merchant == nil {
		t.Error("Merchant is nil - should extract merchant name")
	} else {
		t.Logf("Extracted merchant: '%s'", *data.Merchant)
		// Should be "海烟烟行" (prioritized) or "上海郡徕实业有限公司"
		validMerchants := []string{"海烟烟行", "上海郡徕实业有限公司"}
		found := false
		for _, valid := range validMerchants {
			if *data.Merchant == valid {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected merchant to be one of %v, got '%s'", validMerchants, *data.Merchant)
		}
	}

	// Test transaction time extraction - CRITICAL FIX
	// Expected: "2025-10-23 14:59:46"
	if data.TransactionTime == nil {
		t.Error("TransactionTime is nil - should extract '2025-10-23 14:59:46'")
	} else {
		t.Logf("Extracted time: '%s'", *data.TransactionTime)
		expectedTime := "2025-10-23 14:59:46"
		if *data.TransactionTime != expectedTime {
			t.Errorf("Expected TransactionTime '%s', got '%s'", expectedTime, *data.TransactionTime)
		}
	}

	// Test order number extraction
	if data.OrderNumber == nil {
		t.Error("OrderNumber is nil - should extract order number")
	} else {
		t.Logf("Extracted order number: '%s'", *data.OrderNumber)
		expectedOrderNum := "4200002966202510230090527049"
		if *data.OrderNumber != expectedOrderNum {
			t.Errorf("Expected OrderNumber '%s', got '%s'", expectedOrderNum, *data.OrderNumber)
		}
	}

	// Test payment method extraction
	if data.PaymentMethod == nil {
		t.Error("PaymentMethod is nil")
	} else {
		t.Logf("Extracted payment method: '%s'", *data.PaymentMethod)
	}
}

func TestParsePaymentScreenshot_WeChatQrPay_PayeeTitleLine(t *testing.T) {
	service := NewOCRService()

	sampleText := `微信支付
扫二维码付款-给黄凯文
-4500.00
转账时间 2025年12月2日10:07:30
转账单号 10001073012025120200787809648`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.Merchant == nil || *data.Merchant != "黄凯文" {
		t.Fatalf("expected Merchant=黄凯文, got %#v", data.Merchant)
	}
	if data.MerchantConfidence <= 0.0 {
		t.Fatalf("expected MerchantConfidence to be set, got %v", data.MerchantConfidence)
	}
}

func TestParsePaymentScreenshot_WeChatQrPay_PayeeSplitLines(t *testing.T) {
	service := NewOCRService()

	sampleText := `微信支付
扫二维码付款-给
黄凯文
-4500.00
转账时间 2025年12月2日10:07:30`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.Merchant == nil || *data.Merchant != "黄凯文" {
		t.Fatalf("expected Merchant=黄凯文, got %#v", data.Merchant)
	}
}

func TestParsePaymentScreenshot_WeChatBillDetail_LabelListThenValues(t *testing.T) {
	service := NewOCRService()

	// Simulate a layout where OCR outputs all labels first, then values later.
	// Key requirement: do NOT bind the next label as the value (e.g. "商户全称" -> "收单机构").
	sampleText := `微信支付
全部账单
已支付
闽辉超市
-400.00
交易单号
商品
支付方式
当前状态
支付时间
商户全称
收单机构
商户单号
服务
支付成功
2025年11月15日23:02:47
闽辉超市
招商银行信用卡(2506)
4200002843202511153335484390
上海市徐汇区闽辉杂货店
中国工商银行股份有限公司牡丹卡中心
由中国银联股份有限公司提供收款清算服务
可在支持的商户扫码退款
100160000351000012511150504679
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Merchant == nil {
		t.Fatalf("expected Merchant, got nil")
	}
	// Prefer the user-facing store title ("闽辉超市") over the legal full name.
	if *data.Merchant != "闽辉超市" {
		t.Fatalf("expected Merchant=闽辉超市, got %q", *data.Merchant)
	}

	if data.PaymentMethod == nil {
		t.Fatalf("expected PaymentMethod, got nil")
	}
	if *data.PaymentMethod != "招商银行信用卡(2506)" {
		t.Fatalf("expected PaymentMethod=招商银行信用卡(2506), got %q", *data.PaymentMethod)
	}

	if data.TransactionTime == nil {
		t.Fatalf("expected TransactionTime, got nil")
	}
	if *data.TransactionTime != "2025-11-15 23:02:47" {
		t.Fatalf("expected TransactionTime=2025-11-15 23:02:47, got %q", *data.TransactionTime)
	}

	if data.OrderNumber == nil {
		t.Fatalf("expected OrderNumber, got nil")
	}
	if *data.OrderNumber != "4200002843202511153335484390" {
		t.Fatalf("expected OrderNumber=4200002843202511153335484390, got %q", *data.OrderNumber)
	}
}

func TestParsePaymentScreenshot_WeChatBillDetail_PaymentMethodShouldNotBeBarcode(t *testing.T) {
	service := NewOCRService()

	// A real-world pattern: due to layout-aware postprocess, OCR may output:
	// - "服务：招商银行信用卡(2506)" (card got paired to a wrong label)
	// - "支付方式：10016..." (barcode / merchant id got paired to "支付方式")
	// We should still extract the actual payment method (the card), not the long digits.
	sampleText := `微信支付
全部账单
已支付
闽辉超市
-400.00
当前状态：支付成功
支付时间：2025年11月15日23:02:47
商品：闽辉超市
商户全称：上海市徐汇区闽辉杂货店
收单机构：中国工商银行股份有限公司牡丹卡中心
服务：招商银行信用卡(2506)
由中国银联股份有限公司提供收款清算
支付方式：100160000351000012511150504679
交易单号：4200002843202511153335484390
商户单号：可在支持的商户扫码退款
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.PaymentMethod == nil {
		t.Fatalf("expected PaymentMethod, got nil")
	}
	if *data.PaymentMethod != "招商银行信用卡(2506)" {
		t.Fatalf("expected PaymentMethod=招商银行信用卡(2506), got %q", *data.PaymentMethod)
	}
}

func TestParsePaymentScreenshot_WeChatBillDetail_MerchantShouldPreferTitleOverGenericItem(t *testing.T) {
	service := NewOCRService()

	sampleText := `微信支付
11:26
全部账单
泰隆银行
华致酒行东苑新天地广场店
-3420.00
当前状态：支付成功
支付时间：2025年12月13日11:26:26
商品：商户收款
商户全称：上海鑫之河商贸有限公司
收单机构：浙江泰隆商业银行股份有限公司
支付方式：招商银行信用卡(2506)
交易单号：4200002975202512132611185393
商户单号：30220618110444881657953514298117
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}
	if data.Merchant == nil {
		t.Fatalf("expected Merchant, got nil")
	}
	if *data.Merchant != "华致酒行东苑新天地广场店" {
		t.Fatalf("expected Merchant=华致酒行东苑新天地广场店, got %q", *data.Merchant)
	}
}

func TestParsePaymentScreenshot_Alipay_BillDetail_BasicFields(t *testing.T) {
	service := NewOCRService()

	sampleText := `账单详情
美团外卖
-88.00
支付时间
2025年12月3日20:13:28
付款方式
招商银行信用卡(2506)
交易号
202512032013280001234567890123
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 88.00 {
		t.Fatalf("expected Amount=88.00, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant != "美团外卖" {
		t.Fatalf("expected Merchant=美团外卖, got %#v", data.Merchant)
	}
	if data.TransactionTime == nil || *data.TransactionTime != "2025-12-3 20:13:28" {
		t.Fatalf("expected TransactionTime=2025-12-3 20:13:28, got %#v", data.TransactionTime)
	}
	if data.PaymentMethod == nil || *data.PaymentMethod != "招商银行信用卡(2506)" {
		t.Fatalf("expected PaymentMethod=招商银行信用卡(2506), got %#v", data.PaymentMethod)
	}
	if data.OrderNumber == nil || *data.OrderNumber != "202512032013280001234567890123" {
		t.Fatalf("expected OrderNumber=202512032013280001234567890123, got %#v", data.OrderNumber)
	}
}

func TestParsePaymentScreenshot_JDPay_BillDetail_ShouldExtractTimeAndOrder(t *testing.T) {
	service := NewOCRService()

	sampleText := `8:22
账单详情
5+
京东平台商户
-13,897.00
交易成功
支付方式
招商银行信用卡（2506）>
创建时间
2025-12-26 14:51:37
总订单编号
3359217016960312
商户单号
14083542512261451360858907847
服务详情
`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 13897.00 {
		t.Fatalf("expected Amount=13897.00, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant != "京东平台商户" {
		t.Fatalf("expected Merchant=京东平台商户, got %#v", data.Merchant)
	}
	if data.PaymentMethod == nil || *data.PaymentMethod != "招商银行信用卡(2506)" {
		t.Fatalf("expected PaymentMethod=招商银行信用卡(2506), got %#v", data.PaymentMethod)
	}
	if data.TransactionTime == nil || *data.TransactionTime != "2025-12-26 14:51:37" {
		t.Fatalf("expected TransactionTime=2025-12-26 14:51:37, got %#v", data.TransactionTime)
	}
	// Prefer merchant order id when no explicit "交易单号/交易号" is present.
	if data.OrderNumber == nil || *data.OrderNumber != "14083542512261451360858907847" {
		t.Fatalf("expected OrderNumber=14083542512261451360858907847, got %#v", data.OrderNumber)
	}
}

// TestRemoveChineseSpaces_PreserveTimeSpace tests the fix for preserving space after 日
func TestRemoveChineseSpaces_PreserveTimeSpace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Date with spaces - preserve space before time",
			input:    "2025 年 10 月 23 日 14:59:46",
			expected: "2025年10月23日 14:59:46",
		},
		{
			name:     "Payment time text from problem statement",
			input:    "支 付 时 间 2025 年 10 月 23 日 14:59:46",
			expected: "支付时间2025年10月23日 14:59:46",
		},
		{
			name:     "Date only - remove all spaces",
			input:    "2025 年 10 月 23 日",
			expected: "2025年10月23日",
		},
		{
			name:     "Time with different format",
			input:    "支 付 时 间 2025 年 10 月 23 日 09:30:15",
			expected: "支付时间2025年10月23日 09:30:15",
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

// TestConvertChineseDateToISO_BothFormats tests the improved convertChineseDateToISO
func TestConvertChineseDateToISO_BothFormats(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Date and time with space",
			input:    "2025年10月23日 14:59:46",
			expected: "2025-10-23 14:59:46",
		},
		{
			name:     "Date and time without space",
			input:    "2025年10月23日14:59:46",
			expected: "2025-10-23 14:59:46",
		},
		{
			name:     "Date only",
			input:    "2025年10月23日",
			expected: "2025-10-23",
		},
		{
			name:     "Single digit month and day with time",
			input:    "2025年1月5日 9:30:46",
			expected: "2025-1-5 9:30:46",
		},
		{
			name:     "Single digit month and day without space",
			input:    "2025年1月5日9:30:46",
			expected: "2025-1-5 9:30:46",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertChineseDateToISO(tt.input)
			if result != tt.expected {
				t.Errorf("convertChineseDateToISO(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestParsePaymentScreenshot_WithNegativeAmount tests parsing with negative amount
func TestParsePaymentScreenshot_WithNegativeAmount(t *testing.T) {
	service := NewOCRService()

	// Test with negative amount (as mentioned in problem statement)
	sampleText := `支 付 成 功
-1700.00
支 付 时 间 2025 年 10 月 23 日 14:59:46
商 品 海 烟 烟 行
交 易 单 号 4200002966202510230090527049`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	// Test amount extraction
	if data.Amount == nil {
		t.Error("Amount is nil - should extract 1700.00 from -1700.00")
	} else {
		expectedAmount := 1700.00
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test time extraction
	if data.TransactionTime == nil {
		t.Error("TransactionTime is nil")
	} else {
		expectedTime := "2025-10-23 14:59:46"
		if *data.TransactionTime != expectedTime {
			t.Errorf("Expected TransactionTime '%s', got '%s'", expectedTime, *data.TransactionTime)
		}
	}
}

// Helper function to check if string contains any of the substrings
func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
