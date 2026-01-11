package services

import (
	"context"
	"testing"
)

func TestResolveKnownProviderInvoiceLinksCtx_BaiwangPreviewParam(t *testing.T) {
	u := "https://pis.baiwang.com/smkp-vue/previewInvoiceAllEle?param=ABC123"
	xmlURL, pdfURL, ok := resolveKnownProviderInvoiceLinksCtx(context.TODO(), u)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if xmlURL == nil || *xmlURL == "" {
		t.Fatalf("expected xml url")
	}
	if pdfURL == nil || *pdfURL == "" {
		t.Fatalf("expected pdf url")
	}
	if *pdfURL != "https://pis.baiwang.com/bwmg/mix/bw/downloadFormat?formatType=PDF&param=ABC123" {
		t.Fatalf("unexpected pdf url: %q", *pdfURL)
	}
	if *xmlURL != "https://pis.baiwang.com/bwmg/mix/bw/downloadFormat?formatType=XML&param=ABC123" {
		t.Fatalf("unexpected xml url: %q", *xmlURL)
	}
}

func TestExtractInvoiceLinksFromText_IgnoresPdfIconAndKeepsBaiwangPreview(t *testing.T) {
	body := "请点击查看：http://u.baiwang.com/k5pE5SNf1ld\n<img src=\"https://cdn.example.com/assets/pdf_icon.png\">"
	xmlURL, pdfURL := extractInvoiceLinksFromText(body)
	if xmlURL != nil || pdfURL != nil {
		t.Fatalf("expected no direct pdf/xml links, got xml=%v pdf=%v", ptrToString(xmlURL), ptrToString(pdfURL))
	}
}
