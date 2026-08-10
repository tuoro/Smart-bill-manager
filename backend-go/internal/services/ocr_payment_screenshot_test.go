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

	text := syntheticOCRText("\u5fae\u4fe1\u652f\u4ed8", "\u652f\u4ed8\u6210\u529f", "-42.36", "\u5546\u6237\uff1a\u7eaf\u5408\u6210\u4fbf\u5229\u5e97", "\u652f\u4ed8\u65f6\u95f4\uff1a2026\u5e7408\u670803\u65e512:34:56")

	data, err := service.ParsePaymentScreenshot(text)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 42.36 {
		t.Fatalf("expected synthetic amount 42.36, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant == "" {
		t.Fatalf("expected merchant, got %#v", data.Merchant)
	}
}

func TestParsePaymentScreenshot_GenericAmount(t *testing.T) {
	service := NewOCRService()

	text := syntheticOCRText("支付成功", "¥52.50", "商户：纯合成测试店")
	data, err := service.ParsePaymentScreenshot(text)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 52.50 {
		t.Fatalf("expected synthetic amount 52.50, got %#v", data.Amount)
	}
}

func TestParsePaymentScreenshot_WeChatPay_WithInvisibleSpacesAndCRLF(t *testing.T) {
	service := NewOCRService()

	// Simulate OCR outputs that contain:
	// - Windows newlines (\r\n)
	// - zero-width / invisible spaces between Chinese characters
	text := "SYNTHETIC / \u7eaf\u5408\u6210\u6d4b\u8bd5\u6570\u636e\r\n\u5fae\u200b\u4fe1\u200b\u652f\u200b\u4ed8\r\n\u652f\u4ed8\u6210\u529f\r\n-42.36\r\n\u5546\u6237\uff1a\u7eaf\u5408\u6210\u4fbf\u5229\u5e97\r\n\u652f\u4ed8\u65f6\u95f4\uff1a2026\u5e7408\u670803\u65e512:34:56"

	data, err := service.ParsePaymentScreenshot(text)
	if err != nil {
		t.Fatalf("ParsePaymentScreenshot returned error: %v", err)
	}

	if data.Amount == nil || *data.Amount != 42.36 {
		t.Fatalf("expected synthetic amount 42.36, got %#v", data.Amount)
	}
	if data.Merchant == nil || *data.Merchant == "" {
		t.Fatalf("expected merchant, got %#v", data.Merchant)
	}
	if data.TransactionTime == nil || *data.TransactionTime == "" {
		t.Fatalf("expected transaction time, got %#v", data.TransactionTime)
	}
}
