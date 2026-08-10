package services

import (
	"testing"
)

// TestAmountExtraction_NegativeAmounts tests various negative amount patterns
func TestAmountExtraction_NegativeAmounts(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name           string
		text           string
		expectedAmount float64
		shouldFind     bool
	}{
		{name: "Negative amount without symbol", text: syntheticOCRText("支付成功", "-4242.00", "商户：纯合成测试店"), expectedAmount: 4242.00, shouldFind: true},
		{name: "Negative amount with ¥ symbol", text: syntheticOCRText("支付成功", "-¥4242.00", "商户：纯合成测试店"), expectedAmount: 4242.00, shouldFind: true},
		{name: "Negative amount with full-width ¥", text: syntheticOCRText("支付成功", "-￥4242.00", "商户：纯合成测试店"), expectedAmount: 4242.00, shouldFind: true},
		{name: "Negative amount with spaces", text: syntheticOCRText("支付成功", "- 4242.00", "商户：纯合成测试店"), expectedAmount: 4242.00, shouldFind: true},
		{name: "Negative amount with symbol and space", text: syntheticOCRText("支付成功", "- ¥ 4242.00", "商户：纯合成测试店"), expectedAmount: 4242.00, shouldFind: true},
		{name: "Large amount without symbol", text: syntheticOCRText("支付成功", "4242.00", "商户：纯合成测试店"), expectedAmount: 4242.00, shouldFind: true},
		{name: "Standard amount with ¥", text: syntheticOCRText("支付成功", "¥4242.00", "商户：纯合成测试店"), expectedAmount: 4242.00, shouldFind: true},
		{name: "Amount with comma separator", text: syntheticOCRText("支付成功", "-4,242.00", "商户：纯合成测试店"), expectedAmount: 4242.00, shouldFind: true},
		{name: "Amount with minus sign variant", text: syntheticOCRText("支付成功", "−4242.00", "商户：纯合成测试店"), expectedAmount: 4242.00, shouldFind: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := service.ParsePaymentScreenshot(tt.text)
			if err != nil {
				t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
			}

			if tt.shouldFind {
				if data.Amount == nil {
					t.Errorf("Amount is nil, expected %.2f", tt.expectedAmount)
				} else if *data.Amount != tt.expectedAmount {
					t.Errorf("Expected amount %.2f, got %.2f", tt.expectedAmount, *data.Amount)
				}
			} else {
				if data.Amount != nil {
					t.Errorf("Expected no amount, but got %.2f", *data.Amount)
				}
			}
		})
	}
}

func TestAmountExtraction_SyntheticSpacedOCR(t *testing.T) {
	service := NewOCRService()

	sampleText := `SYNTHETIC / 纯合成测试数据
12:34 合 成 标 记
主 全 部 账 单
当 心
A
纯 合 成 便 利 店

当 前 状 态 支 付 成 功

支 付 时 间 2026 年 08 月 03 日 12:34:56

商 品 纯 合 成 便 利 店 ( 纯 合 成 商 贸 有 限 公 司 )

商 户 全 称 纯 合 成 商 贸 有 限 公 司

收 单 机 构 纯 合 成 支 付 服 务 有 限 公 司
由 纯 合 成 清 算 服 务 提 供

支 付 方 式 纯 合 成 测 试 余 额
由 纯 合 成 清 算 服 务 提 供

交 易 单 号 999999990000000000000003

商 户 单 号 999999990004

账 单 服 务

-4242.00`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil {
		t.Error("Amount is nil for synthetic spaced OCR")
	} else {
		expectedAmount := 4242.00
		if *data.Amount != expectedAmount {
			t.Errorf("Expected amount %.2f, got %.2f", expectedAmount, *data.Amount)
		} else {
			t.Logf("extracted synthetic amount %.2f", *data.Amount)
		}
	}

	// Verify other fields are also extracted correctly
	if data.Merchant == nil {
		t.Error("Merchant is nil")
	} else {
		t.Logf("✓ Extracted merchant: %s", *data.Merchant)
	}

	if data.TransactionTime == nil {
		t.Error("TransactionTime is nil")
	} else {
		t.Logf("✓ Extracted time: %s", *data.TransactionTime)
	}

	if data.OrderNumber == nil {
		t.Error("OrderNumber is nil")
	} else {
		t.Logf("✓ Extracted order number: %s", *data.OrderNumber)
	}
}

// TestAmountExtraction_LargeFontAmounts tests recognition of large font amounts
func TestAmountExtraction_LargeFontAmounts(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name           string
		text           string
		expectedAmount float64
		description    string
	}{
		{
			name:           "Very large amount: 9999.99",
			text:           "9999.99",
			expectedAmount: 9999.99,
			description:    "4-digit amount with decimals",
		},
		{name: "Medium large synthetic amount", text: "4242.00", expectedAmount: 4242.00, description: "4-digit synthetic amount"},
		{
			name:           "Five digit amount: 12345.67",
			text:           "12345.67",
			expectedAmount: 12345.67,
			description:    "5-digit large amount",
		},
		{
			name:           "Large amount with negative: -5000.00",
			text:           "-5000.00",
			expectedAmount: 5000.00,
			description:    "4-digit negative amount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := service.ParsePaymentScreenshot(tt.text)
			if err != nil {
				t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
			}

			if data.Amount == nil {
				t.Errorf("%s: Amount is nil, expected %.2f", tt.description, tt.expectedAmount)
			} else if *data.Amount != tt.expectedAmount {
				t.Errorf("%s: Expected amount %.2f, got %.2f", tt.description, tt.expectedAmount, *data.Amount)
			} else {
				t.Logf("✓ %s: Successfully extracted %.2f", tt.description, *data.Amount)
			}
		})
	}
}
