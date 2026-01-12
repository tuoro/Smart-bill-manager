package services

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/emersion/go-message/mail"
	"golang.org/x/text/encoding/simplifiedchinese"
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

func TestExtractInvoiceArtifactsFromEmail_KeepsOtherPDFsAsExtra(t *testing.T) {
	invoicePDF1 := []byte("%PDF-invoice1\n")
	invoicePDF2 := []byte("%PDF-invoice2\n")
	itineraryPDF := []byte("%PDF-itinerary\n")
	invoice1B64 := base64.StdEncoding.EncodeToString(invoicePDF1)
	invoice2B64 := base64.StdEncoding.EncodeToString(invoicePDF2)
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
		"Content-Disposition: attachment; filename=\"invoice_鐢靛瓙鍙戠エ_1.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		invoice1B64,
		"--b",
		"Content-Type: application/pdf",
		"Content-Disposition: attachment; filename=\"invoice_鐢靛瓙鍙戠エ_2.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		invoice2B64,
		"--b",
		"Content-Type: application/pdf",
		"Content-Disposition: attachment; filename=\"琛岀▼鍗?.pdf\"",
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

	name, b, _, extra, _, err := extractInvoiceArtifactsFromEmail(mr)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if strings.TrimSpace(name) == "" || len(b) == 0 {
		t.Fatalf("expected primary pdf picked, got name=%q len=%d", name, len(b))
	}
	if len(extra) != 2 {
		t.Fatalf("expected 2 extra pdfs, got %+v", extra)
	}
}

func TestExtractInvoiceArtifactsFromEmail_MessageRfc822NestedPDF(t *testing.T) {
	pdfRaw := []byte("%PDF-nested\n")
	pdfB64 := base64.StdEncoding.EncodeToString(pdfRaw)

	inner := strings.Join([]string{
		"From: inner@example.com",
		"To: you@example.com",
		"Subject: inner",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"in\"",
		"",
		"--in",
		"Content-Type: application/pdf",
		"Content-Disposition: attachment; filename=\"nested_invoice.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		pdfB64,
		"--in--",
		"",
	}, "\r\n")

	outer := strings.Join([]string{
		"From: outer@example.com",
		"To: you@example.com",
		"Subject: outer",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"out\"",
		"",
		"--out",
		"Content-Type: message/rfc822",
		"Content-Disposition: inline",
		"",
		inner,
		"--out--",
		"",
	}, "\r\n")

	mr, err := mail.CreateReader(strings.NewReader(outer))
	if err != nil {
		t.Fatalf("CreateReader: %v", err)
	}

	name, b, _, _, _, err := extractInvoiceArtifactsFromEmail(mr)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if strings.TrimSpace(name) != "nested_invoice.pdf" {
		t.Fatalf("expected nested_invoice.pdf, got %q", name)
	}
	if string(b) != string(pdfRaw) {
		t.Fatalf("expected nested pdf bytes, got %q", string(b))
	}
}

func TestExtractInvoiceArtifactsFromEmail_GB18030BodyDoesNotFail(t *testing.T) {
	bodyUTF8 := "这里有个链接 https://example.com/invoice"
	gb, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(bodyUTF8))
	if err != nil {
		t.Fatalf("encode gb18030: %v", err)
	}
	bodyB64 := base64.StdEncoding.EncodeToString(gb)

	pdfRaw := []byte("%PDF-1.4\n%test\n")
	pdfB64 := base64.StdEncoding.EncodeToString(pdfRaw)

	raw := strings.Join([]string{
		"From: test@example.com",
		"To: you@example.com",
		"Subject: test",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"gb\"",
		"",
		"--gb",
		"Content-Type: text/plain; charset=gb18030",
		"Content-Transfer-Encoding: base64",
		"",
		bodyB64,
		"",
		"--gb",
		"Content-Type: application/pdf",
		"Content-Disposition: attachment; filename=\"invoice.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		pdfB64,
		"--gb--",
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
	if strings.TrimSpace(name) != "invoice.pdf" {
		t.Fatalf("expected filename invoice.pdf, got %q", name)
	}
	if len(b) == 0 || !strings.HasPrefix(string(b), "%PDF-") {
		t.Fatalf("expected pdf bytes, got len=%d head=%q", len(b), string(b))
	}
	if !strings.Contains(body, "https://example.com/invoice") {
		t.Fatalf("expected body text collected, got:\n%s", body)
	}
}
