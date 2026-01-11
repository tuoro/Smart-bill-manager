package services

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestExtractInvoiceArtifactsFromEmail_InlinePDF(t *testing.T) {
	pdfRaw := []byte("%PDF-1.4\n%test\n")
	pdfB64 := base64.StdEncoding.EncodeToString(pdfRaw)

	raw := strings.Join([]string{
		"From: test@example.com",
		"To: you@example.com",
		"Subject: test",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"abc\"",
		"",
		"--abc",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"这里有个链接 https://example.com/invoice",
		"",
		"--abc",
		"Content-Type: application/pdf",
		"Content-Disposition: inline; filename=\"invoice.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		pdfB64,
		"--abc--",
		"",
	}, "\r\n")

	mr, err := mail.CreateReader(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("CreateReader: %v", err)
	}

	name, b, _, _, body, err := extractInvoiceArtifactsFromEmail(mr)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(b) == 0 || !strings.HasPrefix(string(b), "%PDF-") {
		t.Fatalf("expected pdf bytes, got len=%d head=%q", len(b), string(b))
	}
	if strings.TrimSpace(name) != "invoice.pdf" {
		t.Fatalf("expected filename invoice.pdf, got %q", name)
	}
	if !strings.Contains(body, "https://example.com/invoice") {
		t.Fatalf("expected body text collected, got:\n%s", body)
	}
}

func TestExtractInvoiceArtifactsFromEmail_AttachmentPDF_NoFilenameButMime(t *testing.T) {
	pdfRaw := []byte("%PDF-1.7\n%test\n")
	pdfB64 := base64.StdEncoding.EncodeToString(pdfRaw)

	raw := strings.Join([]string{
		"From: test@example.com",
		"To: you@example.com",
		"Subject: test",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"xyz\"",
		"",
		"--xyz",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"hello",
		"",
		"--xyz",
		"Content-Type: application/pdf",
		"Content-Disposition: attachment",
		"Content-Transfer-Encoding: base64",
		"",
		pdfB64,
		"--xyz--",
		"",
	}, "\r\n")

	mr, err := mail.CreateReader(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("CreateReader: %v", err)
	}

	name, b, _, _, _, err := extractInvoiceArtifactsFromEmail(mr)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(b) == 0 || !strings.HasPrefix(string(b), "%PDF-") {
		t.Fatalf("expected pdf bytes, got len=%d head=%q", len(b), string(b))
	}
	// Filename may be empty (no filename/name parameter); caller will fallback to invoice.pdf later.
	if strings.TrimSpace(name) != "" {
		t.Fatalf("expected empty filename when missing params, got %q", name)
	}
}

func TestExtractInvoiceArtifactsFromEmail_PicksInvoicePDFAndKeepsItinerary(t *testing.T) {
	invoicePDF := []byte("%PDF-invoice\n")
	itineraryPDF := []byte("%PDF-itinerary\n")
	invoiceB64 := base64.StdEncoding.EncodeToString(invoicePDF)
	itineraryB64 := base64.StdEncoding.EncodeToString(itineraryPDF)

	raw := strings.Join([]string{
		"From: test@example.com",
		"To: you@example.com",
		"Subject: test",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"b\"",
		"",
		"--b",
		"Content-Type: application/pdf",
		"Content-Disposition: attachment; filename=\"invoice_电子发票.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		invoiceB64,
		"--b",
		"Content-Type: application/pdf",
		"Content-Disposition: attachment; filename=\"invoice_电子行程单.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		itineraryB64,
		"--b--",
		"",
	}, "\r\n")

	mr, err := mail.CreateReader(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("CreateReader: %v", err)
	}

	name, b, _, itins, _, err := extractInvoiceArtifactsFromEmail(mr)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if strings.TrimSpace(name) != "invoice_电子发票.pdf" {
		t.Fatalf("expected invoice pdf picked, got %q", name)
	}
	if string(b) != string(invoicePDF) {
		t.Fatalf("expected invoice pdf bytes, got %q", string(b))
	}
	if len(itins) != 1 || strings.TrimSpace(itins[0].Filename) != "invoice_电子行程单.pdf" {
		t.Fatalf("expected one itinerary pdf, got %+v", itins)
	}
}

