package services

import "testing"

func TestRecognizePaymentScreenshot_NonexistentFile(t *testing.T) {
	service := NewOCRService()
	_, err := service.RecognizePaymentScreenshot("/nonexistent/file.png")
	if err == nil {
		t.Fatalf("expected error for nonexistent file")
	}
}

func TestParsePaymentScreenshot_WeChatPayAmountAndMerchant(t *testing.T) {
	service := NewOCRService()

	text := "\u5fae\u4fe1\u652f\u4ed8\n\u652f\u4ed8\u6210\u529f\n-1700.00\n\u5546\u6237\uff1a\u6d4b\u8bd5\u5e97\n\u652f\u4ed8\u65f6\u95f4\uff1a2025\u5e7410\u670823\u65e514:59:46"

	data, err := service.ParsePaymentScreenshot(text)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 1700.00 {
		t.Fatalf("expected amount 1700.00, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant == "" {
		t.Fatalf("expected merchant, got %#v", data.Merchant)
	}
}

func TestParsePaymentScreenshot_GenericAmount(t *testing.T) {
	service := NewOCRService()

	text := "PAYMENT_SUCCESS\n2500.50\nSOME_TEXT"
	data, err := service.ParsePaymentScreenshot(text)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 2500.50 {
		t.Fatalf("expected amount 2500.50, got %#v", data.Amount)
	}
}

func TestParsePaymentScreenshot_WeChatPay_WithInvisibleSpacesAndCRLF(t *testing.T) {
	service := NewOCRService()

	// Simulate OCR outputs that contain:
	// - Windows newlines (\r\n)
	// - zero-width / invisible spaces between Chinese characters
	text := "\u5fae\u200b\u4fe1\u200b\u652f\u200b\u4ed8\r\n\u652f\u4ed8\u6210\u529f\r\n-1700.00\r\n\u5546\u6237\uff1a\u6d4b\u8bd5\u5e97\r\n\u652f\u4ed8\u65f6\u95f4\uff1a2025\u5e7410\u670823\u65e514:59:46"

	data, err := service.ParsePaymentScreenshot(text)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 1700.00 {
		t.Fatalf("expected amount 1700.00, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant == "" {
		t.Fatalf("expected merchant, got %#v", data.Merchant)
	}
	if data.TransactionTime == nil || *data.TransactionTime == "" {
		t.Fatalf("expected transaction time, got %#v", data.TransactionTime)
	}
}
