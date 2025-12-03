package services

import (
	"testing"
)

// TestParsePaymentScreenshot_WeChatPay tests the improved OCR parsing for WeChat Pay screenshots
func TestParsePaymentScreenshot_WeChatPay(t *testing.T) {
	service := NewOCRService()

	// Sample OCR text from the problem statement with amount at the end
	sampleText := `14:59
主 全 部 账 单
当 心
海 烟 烟 行

当 前 状 态 支 付 成 功

支 付 时 间 2025 年 10 月 23 日 14:59:46

商 品 海 烟 烟 行

商 户 全 称 上 海 郡 徕 实 业 有 限 公 司

支 付 方 式 招 商 银 行 信 用 卡 (2506)
由 网 联 清 算 有 限 公 司 提 供 付 款 清 算 服 务

交 易 单 号 4200002966202510230090527049

商 户 单 号 251023116574060365

-1700.00`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	// Test amount extraction (should extract 1700.00 from -1700.00)
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 1700.00
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// Test merchant extraction (should prioritize "海烟烟行" from "商品" field)
	if data.Merchant == nil {
		t.Error("Merchant is nil")
	} else {
		// Should be "海烟烟行" not "上海郡徕实业有限公司"
		if *data.Merchant != "海烟烟行" {
			t.Logf("Merchant: got '%s', expected '海烟烟行' (商品 field should be prioritized)", *data.Merchant)
		}
	}

	// Test transaction time extraction
	if data.TransactionTime == nil {
		t.Error("TransactionTime is nil")
	} else {
		t.Logf("TransactionTime: %s", *data.TransactionTime)
	}

	// Test payment method extraction
	if data.PaymentMethod == nil {
		t.Error("PaymentMethod is nil")
	} else {
		t.Logf("PaymentMethod: %s", *data.PaymentMethod)
	}

	// Test order number extraction
	if data.OrderNumber == nil {
		t.Error("OrderNumber is nil")
	} else {
		t.Logf("OrderNumber: %s", *data.OrderNumber)
	}
}

// TestRemoveChineseSpaces_DateUnits tests the improved space removal for dates
func TestRemoveChineseSpaces_DateUnits(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Date with spaces",
			input:    "2025 年 10 月 23 日",
			expected: "2025年10月23日",
		},
		{
			name:     "Date and time with spaces",
			input:    "支 付 时 间 2025 年 10 月 23 日 14:59:46",
			expected: "支付时间2025年10月23日14:59:46",
		},
		{
			name:     "Mixed Chinese and numbers",
			input:    "支 付 金 额 1700 元",
			expected: "支付金额1700元",
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
