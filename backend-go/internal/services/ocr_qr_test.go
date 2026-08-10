package services

import "testing"

func TestParseInvoiceQRPayload_CombinedCodeNumber20Digits(t *testing.T) {
	payload := "SYNTHETIC 99000000000123456789 20260801 42.36"
	f := parseInvoiceQRPayload(payload)
	if f.InvoiceCode != "990000000001" {
		t.Fatalf("expected synthetic code %q, got %q", "990000000001", f.InvoiceCode)
	}
	if f.InvoiceNumber != "23456789" {
		t.Fatalf("expected synthetic number %q, got %q", "23456789", f.InvoiceNumber)
	}
	if f.CheckCode != "" {
		t.Fatalf("expected empty check code, got %q", f.CheckCode)
	}
	if f.InvoiceDate != "2026年8月1日" {
		t.Fatalf("expected synthetic date %q, got %q", "2026年8月1日", f.InvoiceDate)
	}
	if f.Amount != "42.36" {
		t.Fatalf("expected synthetic amount %q, got %q", "42.36", f.Amount)
	}
}

func TestParseInvoiceQRPayload_StandardTokens(t *testing.T) {
	payload := "01,10,990000000001,23456789,42.36,20260801,99999999999999999999"
	f := parseInvoiceQRPayload(payload)
	if f.InvoiceCode != "990000000001" {
		t.Fatalf("expected synthetic code %q, got %q", "990000000001", f.InvoiceCode)
	}
	if f.InvoiceNumber != "23456789" {
		t.Fatalf("expected synthetic number %q, got %q", "23456789", f.InvoiceNumber)
	}
	if f.InvoiceDate != "2026年8月1日" {
		t.Fatalf("expected synthetic date %q, got %q", "2026年8月1日", f.InvoiceDate)
	}
	if f.Amount != "42.36" {
		t.Fatalf("expected synthetic amount %q, got %q", "42.36", f.Amount)
	}
	if f.CheckCode != "99999999999999999999" {
		t.Fatalf("expected synthetic check code %q, got %q", "99999999999999999999", f.CheckCode)
	}
}
