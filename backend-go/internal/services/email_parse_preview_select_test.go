package services

import "testing"

func TestBestPreviewURLFromText_PrefersKnownProviderOverAssetsAndGenericPages(t *testing.T) {
	body := `
Hello,
Tracking: https://cdn.example.com/pixel.png
NuoNuo landing: https://nnfp.jss.com.cn/scan-invoice/invoiceShow
Baiwang short link: http://u.baiwang.com/k5pE5SNf1ld
`

	got := bestPreviewURLFromText(body)
	if got != "http://u.baiwang.com/k5pE5SNf1ld" {
		t.Fatalf("unexpected best preview url: %q", got)
	}
}

func TestIsBadEmailPreviewURL(t *testing.T) {
	tests := []struct {
		name string
		u    string
		want bool
	}{
		{"empty", "", true},
		{"asset_png", "https://example.com/a.png", true},
		{"asset_js", "https://example.com/app.js", true},
		{"nuonuo_invoiceShow", "https://nnfp.jss.com.cn/scan-invoice/invoiceShow", true},
		{"baiwang_short", "http://u.baiwang.com/k5pE5SNf1ld", false},
		{"baiwang_preview", "https://pis.baiwang.com/smkp-vue/previewInvoiceAllEle?param=abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBadEmailPreviewURL(tt.u)
			if got != tt.want {
				t.Fatalf("isBadEmailPreviewURL(%q)=%v want %v", tt.u, got, tt.want)
			}
		})
	}
}

func TestIsDirectInvoicePDFURL(t *testing.T) {
	tests := []struct {
		u    string
		want bool
	}{
		{"https://example.com/a.pdf", true},
		{"https://example.com/downloadFormat?param=abc&formatType=PDF", true},
		{"https://example.com/preview", false},
	}
	for _, tt := range tests {
		got := isDirectInvoicePDFURL(tt.u)
		if got != tt.want {
			t.Fatalf("isDirectInvoicePDFURL(%q)=%v want %v", tt.u, got, tt.want)
		}
	}
}

