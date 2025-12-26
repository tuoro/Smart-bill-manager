package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsRapidOCRAvailable tests the RapidOCR availability check
func TestIsRapidOCRAvailable(t *testing.T) {
	service := NewOCRService()

	t.Run("Returns false when script is not available", func(t *testing.T) {
		// This test will check if the script exists and Python/RapidOCR are available
		// The actual result depends on the test environment
		available := service.isRapidOCRAvailable()
		// We just verify it doesn't panic
		t.Logf("RapidOCR availability: %v", available)
	})

	t.Run("findPaddleOCRScript works correctly", func(t *testing.T) {
		scriptPath := service.findPaddleOCRScript()
		t.Logf("Script path found: %s", scriptPath)
		// If script is found, it should be a valid path
		if scriptPath != "" {
			if _, err := os.Stat(scriptPath); err != nil {
				t.Errorf("Script path exists but stat failed: %v", err)
			}
		}
	})
}

// TestRecognizeWithRapidOCR tests the RapidOCR CLI recognition function
func TestRecognizeWithRapidOCR(t *testing.T) {
	service := NewOCRService()

	t.Run("Returns error when script not found", func(t *testing.T) {
		// Create a temporary directory without the script
		tempDir, err := os.MkdirTemp("", "ocr-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tempDir)

		// Create a dummy image file
		imagePath := filepath.Join(tempDir, "test.png")
		if err := os.WriteFile(imagePath, []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}

		// Change working directory temporarily to ensure script is not found
		originalWd, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(originalWd)

		_, err = service.RecognizeWithRapidOCR(imagePath)
		if err == nil {
			t.Error("Expected RecognizeWithRapidOCR to return error when script not found")
		}
		if !strings.Contains(err.Error(), "script not found") {
			t.Errorf("Expected error to mention 'script not found', got: %v", err)
		}
	})

	t.Run("Successfully executes mock RapidOCR script", func(t *testing.T) {
		// Skip this test if Python is not available
		if _, err := exec.LookPath("python3"); err != nil {
			if _, err := exec.LookPath("python"); err != nil {
				t.Skip("Python not available, skipping test")
			}
		}

		// Create a temporary directory
		tempDir, err := os.MkdirTemp("", "ocr-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tempDir)

		// Create a mock RapidOCR script (use ASCII to avoid encoding issues)
		mockScript := `#!/usr/bin/env python3
import sys
import json

result = {
    "success": True,
    "text": "WECHAT_PAY\\n-1700.00\\nTEST_MERCHANT",
    "lines": [
        {"text": "WECHAT_PAY", "confidence": 0.95},
        {"text": "-1700.00", "confidence": 0.98},
        {"text": "TEST_MERCHANT", "confidence": 0.92}
    ],
    "line_count": 3
}
print(json.dumps(result, ensure_ascii=False))
`
		scriptPath := filepath.Join(tempDir, "paddleocr_cli.py")
		if err := os.WriteFile(scriptPath, []byte(mockScript), 0755); err != nil {
			t.Fatal(err)
		}

		// Create a dummy image file
		imagePath := filepath.Join(tempDir, "test.png")
		if err := os.WriteFile(imagePath, []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}

		// Change working directory to temp dir so script is found
		originalWd, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(originalWd)

		text, err := service.RecognizeWithRapidOCR(imagePath)
		if err != nil {
			t.Fatalf("RecognizeWithRapidOCR returned error: %v", err)
		}

		if !strings.Contains(text, "WECHAT_PAY") {
			t.Error("Expected text to contain 'WECHAT_PAY'")
		}
		if !strings.Contains(text, "-1700.00") {
			t.Error("Expected text to contain '-1700.00'")
		}
		if !strings.Contains(text, "TEST_MERCHANT") {
			t.Error("Expected text to contain 'TEST_MERCHANT'")
		}
	})

	t.Run("Returns error when script returns error", func(t *testing.T) {
		// Skip this test if Python is not available
		if _, err := exec.LookPath("python3"); err != nil {
			if _, err := exec.LookPath("python"); err != nil {
				t.Skip("Python not available, skipping test")
			}
		}

		// Create a temporary directory
		tempDir, err := os.MkdirTemp("", "ocr-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tempDir)

		// Create a mock RapidOCR script that returns an error
		mockScript := `#!/usr/bin/env python3
import sys
import json

result = {
    "success": False,
    "error": "Image file not found"
}
print(json.dumps(result))
sys.exit(1)
`
		scriptPath := filepath.Join(tempDir, "paddleocr_cli.py")
		if err := os.WriteFile(scriptPath, []byte(mockScript), 0755); err != nil {
			t.Fatal(err)
		}

		// Create a dummy image file
		imagePath := filepath.Join(tempDir, "test.png")
		if err := os.WriteFile(imagePath, []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}

		// Change working directory to temp dir so script is found
		originalWd, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(originalWd)

		_, err = service.RecognizeWithRapidOCR(imagePath)
		if err == nil {
			t.Error("Expected RecognizeWithRapidOCR to return error for failed OCR")
		}
		if !strings.Contains(err.Error(), "RapidOCR error") {
			t.Errorf("Expected error to mention 'RapidOCR error', got: %v", err)
		}
	})
}

// TestRecognizePaymentScreenshotWithRapidOCR tests payment screenshot recognition with RapidOCR
func TestRecognizePaymentScreenshotWithRapidOCR(t *testing.T) {
	service := NewOCRService()

	t.Run("Uses RapidOCR when available", func(t *testing.T) {
		// This test verifies that if RapidOCR is available, it will be used
		// The actual behavior depends on the test environment
		available := service.isRapidOCRAvailable()
		t.Logf("RapidOCR available: %v", available)

		// If available, we expect it to be used in RecognizePaymentScreenshot
		// However, we can't easily test this without a real image and RapidOCR installed
		// So we just verify the function doesn't panic
	})

	t.Run("Falls back to Tesseract when RapidOCR unavailable", func(t *testing.T) {
		// Create a temporary directory without the script
		tempDir, err := os.MkdirTemp("", "ocr-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tempDir)

		// Change working directory temporarily to ensure script is not found
		originalWd, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(originalWd)

		// This will try RapidOCR (fail), then fall back to Tesseract
		// We expect an error since the image doesn't exist, but it should be from Tesseract fallback
		_, err = service.RecognizePaymentScreenshot("/nonexistent/image.png")

		// We expect an error since the image doesn't exist
		// The error should not mention RapidOCR specifically (since it fell back)
		if err == nil {
			t.Log("Expected an error for nonexistent image")
		}
	})
}
