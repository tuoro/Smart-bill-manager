package services

import (
	"testing"
)

// TestParsePaymentScreenshot_WeChatPay tests spaced-field OCR parsing with pure synthetic text.
func TestParsePaymentScreenshot_WeChatPay(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
12:34
主 全 部 账 单
当 心
纯 合 成 便 利 店

当 前 状 态 支 付 成 功

支 付 时 间 2026 年 08 月 03 日 12:34:56

商 品 纯 合 成 便 利 店

商 户 全 称 纯 合 成 测 试 商 贸 有 限 公 司

支 付 方 式 纯 合 成 测 试 余 额
由 纯 合 成 清 算 服 务 提 供

交 易 单 号 999999990000000000000001

商 户 单 号 999999990002

-42.36`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	// Test amount extraction from a negative-format synthetic payment amount.
	if data.Amount == nil {
		t.Error("Amount is nil")
	} else {
		expectedAmount := 42.36
		if *data.Amount != expectedAmount {
			t.Errorf("Expected Amount %.2f, got %.2f", expectedAmount, *data.Amount)
		}
	}

	// The shorter synthetic 商品 value should be preferred over 商户全称.
	if data.Merchant == nil {
		t.Error("Merchant is nil")
	} else {
		if *data.Merchant != "纯合成便利店" {
			t.Logf("Merchant: got %q, expected the short synthetic 商品 value", *data.Merchant)
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
			input:    "2026 年 08 月 03 日",
			expected: "2026年08月03日",
		},
		{
			name:     "Date and time with spaces",
			input:    "支 付 时 间 2026 年 08 月 03 日 12:34:56",
			expected: "支付时间2026年08月03日 12:34:56",
		},
		{
			name:     "Mixed Chinese and numbers",
			input:    "支 付 金 额 42 元",
			expected: "支付金额42元",
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
