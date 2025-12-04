package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestIsPaddleOCRAvailable tests the PaddleOCR availability check
func TestIsPaddleOCRAvailable(t *testing.T) {
	service := NewOCRService()

	t.Run("Returns false when service is not available", func(t *testing.T) {
		// Set a URL that doesn't exist
		os.Setenv("PADDLEOCR_URL", "http://localhost:9999")
		defer os.Unsetenv("PADDLEOCR_URL")

		available := service.isPaddleOCRAvailable()
		if available {
			t.Error("Expected isPaddleOCRAvailable to return false for non-existent service")
		}
	})

	t.Run("Returns true when service is available", func(t *testing.T) {
		// Create a mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			}
		}))
		defer server.Close()

		os.Setenv("PADDLEOCR_URL", server.URL)
		defer os.Unsetenv("PADDLEOCR_URL")

		available := service.isPaddleOCRAvailable()
		if !available {
			t.Error("Expected isPaddleOCRAvailable to return true for available service")
		}
	})
}

// TestRecognizeWithPaddleOCR tests the PaddleOCR recognition function
func TestRecognizeWithPaddleOCR(t *testing.T) {
	service := NewOCRService()

	t.Run("Successfully extracts text from PaddleOCR response", func(t *testing.T) {
		// Create a mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/ocr/path" {
				// Verify request
				var req map[string]string
				json.NewDecoder(r.Body).Decode(&req)

				// Send mock response
				response := PaddleOCRResponse{
					Success: true,
					Text:    "微信支付\n-1700.00\n商户全称：测试商户",
					Lines: []PaddleOCRLine{
						{Text: "微信支付", Confidence: 0.95, Box: [][]float64{{0, 0}, {100, 0}, {100, 20}, {0, 20}}},
						{Text: "-1700.00", Confidence: 0.98, Box: [][]float64{{0, 30}, {100, 30}, {100, 50}, {0, 50}}},
						{Text: "商户全称：测试商户", Confidence: 0.92, Box: [][]float64{{0, 60}, {200, 60}, {200, 80}, {0, 80}}},
					},
					LineCount: 3,
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(response)
			}
		}))
		defer server.Close()

		os.Setenv("PADDLEOCR_URL", server.URL)
		defer os.Unsetenv("PADDLEOCR_URL")

		text, err := service.RecognizeWithPaddleOCR("/test/image.png")
		if err != nil {
			t.Fatalf("RecognizeWithPaddleOCR returned error: %v", err)
		}

		if !strings.Contains(text, "微信支付") {
			t.Error("Expected text to contain '微信支付'")
		}
		if !strings.Contains(text, "-1700.00") {
			t.Error("Expected text to contain '-1700.00'")
		}
		if !strings.Contains(text, "测试商户") {
			t.Error("Expected text to contain '测试商户'")
		}
	})

	t.Run("Returns error when PaddleOCR service returns error", func(t *testing.T) {
		// Create a mock server that returns an error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/ocr/path" {
				response := PaddleOCRResponse{
					Success: false,
					Error:   "Image file not found",
				}
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(response)
			}
		}))
		defer server.Close()

		os.Setenv("PADDLEOCR_URL", server.URL)
		defer os.Unsetenv("PADDLEOCR_URL")

		_, err := service.RecognizeWithPaddleOCR("/nonexistent/image.png")
		if err == nil {
			t.Error("Expected RecognizeWithPaddleOCR to return error for failed OCR")
		}
		if !strings.Contains(err.Error(), "PaddleOCR error") {
			t.Errorf("Expected error to mention 'PaddleOCR error', got: %v", err)
		}
	})

	t.Run("Returns error when service is unreachable", func(t *testing.T) {
		os.Setenv("PADDLEOCR_URL", "http://localhost:9999")
		defer os.Unsetenv("PADDLEOCR_URL")

		_, err := service.RecognizeWithPaddleOCR("/test/image.png")
		if err == nil {
			t.Error("Expected RecognizeWithPaddleOCR to return error for unreachable service")
		}
	})
}

// TestRecognizePaymentScreenshotWithPaddleOCR tests payment screenshot recognition with PaddleOCR
func TestRecognizePaymentScreenshotWithPaddleOCR(t *testing.T) {
	service := NewOCRService()

	t.Run("Uses PaddleOCR when available", func(t *testing.T) {
		usedPaddleOCR := false

		// Create a mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			} else if r.URL.Path == "/ocr/path" {
				usedPaddleOCR = true
				response := PaddleOCRResponse{
					Success:   true,
					Text:      "微信支付\n-1700.00",
					LineCount: 2,
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(response)
			}
		}))
		defer server.Close()

		os.Setenv("PADDLEOCR_URL", server.URL)
		defer os.Unsetenv("PADDLEOCR_URL")

		// This will fail with Tesseract not available, but we just want to verify PaddleOCR was called
		text, _ := service.RecognizePaymentScreenshot("/test/image.png")

		if !usedPaddleOCR {
			t.Error("Expected RecognizePaymentScreenshot to use PaddleOCR when available")
		}

		// If PaddleOCR was successful, we should get the text
		if text != "" && !strings.Contains(text, "微信支付") {
			t.Error("Expected text from PaddleOCR to contain '微信支付'")
		}
	})

	t.Run("Falls back to Tesseract when PaddleOCR unavailable", func(t *testing.T) {
		// Set a URL that doesn't exist
		os.Setenv("PADDLEOCR_URL", "http://localhost:9999")
		defer os.Unsetenv("PADDLEOCR_URL")

		// This will try PaddleOCR (fail), then fall back to Tesseract
		// We just verify it doesn't crash and returns something (or error gracefully)
		_, err := service.RecognizePaymentScreenshot("/nonexistent/image.png")

		// We expect an error since the image doesn't exist, but it should be from Tesseract fallback
		// not from a panic or unhandled PaddleOCR error
		if err != nil && strings.Contains(err.Error(), "PaddleOCR") {
			t.Error("Expected error from Tesseract fallback, not PaddleOCR")
		}
	})
}
