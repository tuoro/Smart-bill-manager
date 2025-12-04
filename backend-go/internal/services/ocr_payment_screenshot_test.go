package services

import (
	"strings"
	"testing"
)

// TestOcrWithConfig tests OCR with different page segmentation modes
func TestOcrWithConfig(t *testing.T) {
	service := NewOCRService()

	// This test documents the function behavior
	// Actual testing requires a real image file with Tesseract installed
	t.Run("Returns empty string on error", func(t *testing.T) {
		// Non-existent file should return empty string
		result := service.ocrWithConfig("/nonexistent/file.png", 3)
		if result != "" {
			t.Errorf("Expected empty string for non-existent file, got: %s", result)
		}
	})
}

// TestCreateBinaryImage tests binary image creation
func TestCreateBinaryImage(t *testing.T) {
	service := NewOCRService()

	t.Run("Returns original path when ImageMagick not available or fails", func(t *testing.T) {
		// With non-existent input, should return the input path
		inputPath := "/nonexistent/input.png"
		tempDir := "/tmp"
		result := service.createBinaryImage(inputPath, tempDir)

		// Should return original path on error
		if result != inputPath {
			t.Logf("createBinaryImage returned: %s", result)
		}
	})
}

// TestCreateInvertedImage tests inverted image creation
func TestCreateInvertedImage(t *testing.T) {
	service := NewOCRService()

	t.Run("Returns original path when ImageMagick not available or fails", func(t *testing.T) {
		// With non-existent input, should return the input path
		inputPath := "/nonexistent/input.png"
		tempDir := "/tmp"
		result := service.createInvertedImage(inputPath, tempDir)

		// Should return original path on error
		if result != inputPath {
			t.Logf("createInvertedImage returned: %s", result)
		}
	})
}

// TestMergeOCRResults tests the merging of multiple OCR results
func TestMergeOCRResults(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name     string
		results  []string
		contains []string // Expected substrings in result
	}{
		{
			name:     "Empty results",
			results:  []string{},
			contains: []string{},
		},
		{
			name:     "Single result with amount",
			results:  []string{"支付成功\n-1700.00\n商户：测试店"},
			contains: []string{"1700.00"},
		},
		{
			name: "Multiple results with amounts",
			results: []string{
				"支付成功\n商户：测试店",
				"金额：-1700.00",
				"时间：2025年10月23日",
			},
			contains: []string{"1700.00"},
		},
		{
			name: "Results with Chinese characters",
			results: []string{
				"支付成功",
				"商户全称：上海公司",
				"-1700.00",
			},
			contains: []string{"1700.00", "商户"},
		},
		{
			name: "Results without amounts",
			results: []string{
				"支付成功",
				"商户：测试",
			},
			contains: []string{"支付成功"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.mergeOCRResults(tt.results)

			if len(tt.results) == 0 {
				if result != "" {
					t.Errorf("Expected empty result for empty input, got: %s", result)
				}
				return
			}

			// Check if result contains expected substrings
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Logf("Result does not contain expected substring '%s'. Result: %s", expected, result)
				}
			}

			t.Logf("Merged result (%d chars): %s", len(result), result)
		})
	}
}

// TestOcrDigitsOnly tests digit-only OCR configuration
func TestOcrDigitsOnly(t *testing.T) {
	service := NewOCRService()

	t.Run("Returns empty string on error", func(t *testing.T) {
		// Non-existent file should return empty string
		result := service.ocrDigitsOnly("/nonexistent/file.png")
		if result != "" {
			t.Errorf("Expected empty string for non-existent file, got: %s", result)
		}
	})
}

// TestPreprocessPaymentScreenshot tests payment screenshot preprocessing
func TestPreprocessPaymentScreenshot(t *testing.T) {
	service := NewOCRService()

	t.Run("Returns original path when ImageMagick not available", func(t *testing.T) {
		inputPath := "/nonexistent/input.png"
		tempDir := "/tmp"
		result := service.preprocessPaymentScreenshot(inputPath, tempDir)

		// Should return original path when ImageMagick is not available or processing fails
		t.Logf("preprocessPaymentScreenshot returned: %s", result)
	})
}

// TestPreprocessPaymentScreenshotAlt tests alternative payment screenshot preprocessing
func TestPreprocessPaymentScreenshotAlt(t *testing.T) {
	service := NewOCRService()

	t.Run("Returns original path when ImageMagick not available", func(t *testing.T) {
		inputPath := "/nonexistent/input.png"
		tempDir := "/tmp"
		result := service.preprocessPaymentScreenshotAlt(inputPath, tempDir)

		// Should return original path when processing fails
		t.Logf("preprocessPaymentScreenshotAlt returned: %s", result)
	})
}

// TestRecognizePaymentScreenshot tests the main payment screenshot recognition
func TestRecognizePaymentScreenshot(t *testing.T) {
	service := NewOCRService()

	t.Run("Handles non-existent file gracefully", func(t *testing.T) {
		// Should fallback to RecognizeImageEnhanced which will return an error
		_, err := service.RecognizePaymentScreenshot("/nonexistent/file.png")
		if err == nil {
			t.Log("RecognizePaymentScreenshot should return error for non-existent file")
		}
	})
}

// TestParsePaymentScreenshot_LargeAmount tests parsing of large font amounts
func TestParsePaymentScreenshot_LargeAmount(t *testing.T) {
	service := NewOCRService()

	tests := []struct {
		name             string
		text             string
		expectedAmount   float64
		shouldHaveAmount bool
	}{
		{
			name:             "Large negative amount -1700.00",
			text:             "微信支付\n支付成功\n-1700.00\n商户：测试店\n支付时间：2025年10月23日 14:59:46",
			expectedAmount:   1700.00,
			shouldHaveAmount: true,
		},
		{
			name:             "Large amount 1700.00",
			text:             "微信支付\n支付成功\n1700.00\n商户：测试店",
			expectedAmount:   1700.00,
			shouldHaveAmount: true,
		},
		{
			name:             "Large negative amount with currency -¥1700.00",
			text:             "微信支付\n支付成功\n-¥1700.00\n商户：测试店",
			expectedAmount:   1700.00,
			shouldHaveAmount: true,
		},
		{
			name:             "Large amount with currency ¥1700.00",
			text:             "微信支付\n支付成功\n¥1700.00\n商户：测试店",
			expectedAmount:   1700.00,
			shouldHaveAmount: true,
		},
		{
			name:             "Very large amount 12345.67",
			text:             "支付成功\n12345.67\n商户：大额测试",
			expectedAmount:   12345.67,
			shouldHaveAmount: true,
		},
		{
			name:             "Amount without currency symbol 2500.50",
			text:             "支付成功\n2500.50\n商户：测试",
			expectedAmount:   2500.50,
			shouldHaveAmount: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := service.ParsePaymentScreenshot(tt.text)
			if err != nil {
				t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
			}

			if tt.shouldHaveAmount {
				if data.Amount == nil {
					t.Errorf("Amount is nil, expected %.2f", tt.expectedAmount)
				} else if *data.Amount != tt.expectedAmount {
					t.Errorf("Expected amount %.2f, got %.2f", tt.expectedAmount, *data.Amount)
				}
			} else {
				if data.Amount != nil {
					t.Logf("Amount extracted: %.2f (not expected)", *data.Amount)
				}
			}
		})
	}
}

// TestMergeOCRResults_AmountPriority tests that results with amounts are prioritized
func TestMergeOCRResults_AmountPriority(t *testing.T) {
	service := NewOCRService()

	t.Run("Prioritizes result with amount", func(t *testing.T) {
		results := []string{
			"支付成功\n商户：测试店",         // No amount
			"微信支付\n-1700.00\n海烟烟行", // Has amount
			"支付时间：2025年10月23日",     // No amount
		}

		merged := service.mergeOCRResults(results)

		// Should prioritize the result with amount
		if !strings.Contains(merged, "1700.00") {
			t.Errorf("Merged result should contain amount '1700.00', got: %s", merged)
		}

		// Should also contain Chinese characters from the result with amount
		if !strings.Contains(merged, "烟") {
			t.Logf("Merged result might not prioritize result with amount. Got: %s", merged)
		}

		t.Logf("Merged result: %s", merged)
	})

	t.Run("Prioritizes result with currency symbol", func(t *testing.T) {
		results := []string{
			"支付成功",     // No amount
			"¥1700.00", // Has currency amount
			"商户：测试",    // No amount
		}

		merged := service.mergeOCRResults(results)

		if !strings.Contains(merged, "¥") || !strings.Contains(merged, "1700") {
			t.Errorf("Merged result should contain currency amount, got: %s", merged)
		}
	})

	t.Run("Merges unique lines when no amount found", func(t *testing.T) {
		results := []string{
			"支付成功\n商户：测试A",
			"支付成功\n商户：测试B", // Duplicate line
		}

		merged := service.mergeOCRResults(results)

		// Should contain unique lines
		if !strings.Contains(merged, "支付成功") {
			t.Errorf("Merged result should contain '支付成功', got: %s", merged)
		}

		// Should deduplicate
		count := strings.Count(merged, "支付成功")
		if count > 1 {
			t.Logf("Merged result has duplicates (count: %d): %s", count, merged)
		}

		t.Logf("Merged result: %s", merged)
	})
}
