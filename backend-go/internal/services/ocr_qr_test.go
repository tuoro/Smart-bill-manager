package services

import "testing"

func TestParseInvoiceQRPayload_CombinedCodeNumber20Digits(t *testing.T) {
	payload := "foo 25117000000677781736 20250512 70.39"
	f := parseInvoiceQRPayload(payload)
	if f.InvoiceCode != "251170000006" {
		t.Fatalf("expected code %q, got %q", "251170000006", f.InvoiceCode)
	}
	if f.InvoiceNumber != "77781736" {
		t.Fatalf("expected number %q, got %q", "77781736", f.InvoiceNumber)
	}
	if f.CheckCode != "" {
		t.Fatalf("expected empty check code, got %q", f.CheckCode)
	}
	if f.InvoiceDate != "2025年5月12日" {
		t.Fatalf("expected date %q, got %q", "2025年5月12日", f.InvoiceDate)
	}
	if f.Amount != "70.39" {
		t.Fatalf("expected amount %q, got %q", "70.39", f.Amount)
	}
}

func TestParseInvoiceQRPayload_StandardTokens(t *testing.T) {
	payload := "01,10,251170000006,77781736,70.39,20250512,12345678901234567890"
	f := parseInvoiceQRPayload(payload)
	if f.InvoiceCode != "251170000006" {
		t.Fatalf("expected code %q, got %q", "251170000006", f.InvoiceCode)
	}
	if f.InvoiceNumber != "77781736" {
		t.Fatalf("expected number %q, got %q", "77781736", f.InvoiceNumber)
	}
	if f.InvoiceDate != "2025年5月12日" {
		t.Fatalf("expected date %q, got %q", "2025年5月12日", f.InvoiceDate)
	}
	if f.Amount != "70.39" {
		t.Fatalf("expected amount %q, got %q", "70.39", f.Amount)
	}
	if f.CheckCode != "12345678901234567890" {
		t.Fatalf("expected check code %q, got %q", "12345678901234567890", f.CheckCode)
	}
}

