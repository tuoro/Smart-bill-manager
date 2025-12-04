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
		{
			name:           "Negative amount without symbol: -1700.00",
			text:           "支付成功\n-1700.00\n商户：测试店",
			expectedAmount: 1700.00,
			shouldFind:     true,
		},
		{
			name:           "Negative amount with ¥ symbol: -¥1700.00",
			text:           "支付成功\n-¥1700.00\n商户：测试店",
			expectedAmount: 1700.00,
			shouldFind:     true,
		},
		{
			name:           "Negative amount with full-width ¥: -￥1700.00",
			text:           "支付成功\n-￥1700.00\n商户：测试店",
			expectedAmount: 1700.00,
			shouldFind:     true,
		},
		{
			name:           "Negative amount with spaces: - 1700.00",
			text:           "支付成功\n- 1700.00\n商户：测试店",
			expectedAmount: 1700.00,
			shouldFind:     true,
		},
		{
			name:           "Negative amount with symbol and space: - ¥ 1700.00",
			text:           "支付成功\n- ¥ 1700.00\n商户：测试店",
			expectedAmount: 1700.00,
			shouldFind:     true,
		},
		{
			name:           "Large amount without symbol: 1700.00",
			text:           "支付成功\n1700.00\n商户：测试店",
			expectedAmount: 1700.00,
			shouldFind:     true,
		},
		{
			name:           "Standard amount with ¥: ¥1700.00",
			text:           "支付成功\n¥1700.00\n商户：测试店",
			expectedAmount: 1700.00,
			shouldFind:     true,
		},
		{
			name:           "Amount with comma separator: -1,700.00",
			text:           "支付成功\n-1,700.00\n商户：测试店",
			expectedAmount: 1700.00,
			shouldFind:     true,
		},
		{
			name:           "Amount with minus sign variant (−): −1700.00",
			text:           "支付成功\n−1700.00\n商户：测试店",
			expectedAmount: 1700.00,
			shouldFind:     true,
		},
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

// TestAmountExtraction_RealWorldCase tests the exact case from problem statement
func TestAmountExtraction_RealWorldCase(t *testing.T) {
	service := NewOCRService()

	// Simulate OCR text that doesn't include the amount in initial extraction
	// This tests the large font amount recognition improvement
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

商 户 单 号 251023116574060365

账 单 服 务

-1700.00`

	data, err := service.ParsePaymentScreenshot(sampleText)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	// Test that amount is correctly extracted as 1700.00
	if data.Amount == nil {
		t.Error("Amount is nil - OCR should recognize -1700.00 and extract 1700.00")
	} else {
		expectedAmount := 1700.00
		if *data.Amount != expectedAmount {
			t.Errorf("Expected amount %.2f, got %.2f", expectedAmount, *data.Amount)
		} else {
			t.Logf("✓ Successfully extracted amount %.2f from -1700.00", *data.Amount)
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
		{
			name:           "Medium large amount: 1700.00",
			text:           "1700.00",
			expectedAmount: 1700.00,
			description:    "4-digit amount (problem case)",
		},
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
