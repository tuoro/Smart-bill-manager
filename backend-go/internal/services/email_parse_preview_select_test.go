package services

import (
	"encoding/base64"
	"net/url"
	"testing"
)

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

func TestBestPreviewURLFromText_NormalizesProtocolRelativeURL(t *testing.T) {
	body := `Click: //of1.cn/abc123`
	got := bestPreviewURLFromText(body)
	if got != "https://of1.cn/abc123" {
		t.Fatalf("unexpected best preview url: %q", got)
	}
}

func TestBestPreviewURLFromText_ExtractsEmbeddedContentURL(t *testing.T) {
	body := `QR: https://nnfp.jss.com.cn/allow/service/getEwmImg.do?content=https://nnfp.jss.com.cn/8_CszRwjaw-FBnv`
	got := bestPreviewURLFromText(body)
	if got != "https://nnfp.jss.com.cn/8_CszRwjaw-FBnv" {
		t.Fatalf("unexpected best preview url: %q", got)
	}
}

func TestBestInvoicePreviewURLFromBody_HTMLAnchorAfterLabel(t *testing.T) {
	body := `
<div>
  <span>点击链接查看发票：</span>
  <a href="https://nnfp.jss.com.cn/8_CszRwjaw-FBnv">https://nnfp.jss.com.cn/8_CszRwjaw-FBnv</a>
  <a href="https://nst.nuonuo.com/#/">诺税通</a>
</div>
`
	got := bestInvoicePreviewURLFromBody(body)
	if got != "https://nnfp.jss.com.cn/8_CszRwjaw-FBnv" {
		t.Fatalf("unexpected anchored preview url: %q", got)
	}
}

func TestBestInvoicePreviewURLFromBody_DecodesBase64Runs(t *testing.T) {
	html := `<div><span>点击链接查看发票：</span><a href="https://nnfp.jss.com.cn/8_CszRwjaw-FBnv">下载发票</a></div>`
	b64 := base64.StdEncoding.EncodeToString([]byte(html))
	// Split into chunks similar to base64 email body lines.
	body := b64[:40] + "\n" + b64[40:]

	got := bestInvoicePreviewURLFromBody(body)
	if got != "https://nnfp.jss.com.cn/8_CszRwjaw-FBnv" {
		t.Fatalf("unexpected decoded preview url: %q", got)
	}
}

func TestBestInvoicePreviewURLFromBody_PrefersNonTrackingLinkOverTrackingCTA(t *testing.T) {
	body := `
<div>
  <a href="http://linktrace.triggerdelivery.com/u/o1/xxx">下载发票</a>
  <div><span>点击链接查看发票：</span><a href="https://nnfp.jss.com.cn/8_CszRwjaw-FBnv">https://nnfp.jss.com.cn/8_CszRwjaw-FBnv</a></div>
  <a href="https://nst.nuonuo.com/#/">诺税通</a>
</div>
`
	got := bestInvoicePreviewURLFromBody(body)
	if got != "https://nnfp.jss.com.cn/8_CszRwjaw-FBnv" {
		t.Fatalf("unexpected preview url: %q", got)
	}
}

func TestBestPreviewURLFromText_DoesNotPickLinktracePixel(t *testing.T) {
	body := `
Pixel: http://linktrace.triggerdelivery.com/u/o1/N132-XXX
Invoice: https://nnfp.jss.com.cn/8_CszRwjaw-FBnv
`
	got := bestPreviewURLFromText(body)
	if got != "https://nnfp.jss.com.cn/8_CszRwjaw-FBnv" {
		t.Fatalf("unexpected preview url: %q", got)
	}
}

func TestIsBadEmailPreviewURL_DoesNotDiscardTrackingLink(t *testing.T) {
	u := "http://linktrace.triggerdelivery.com/u/o1/N132-XXX"
	if isBadEmailPreviewURL(u) {
		t.Fatalf("tracking links should not be hard-discarded; keep as last-resort candidates")
	}
}

func TestBestPreviewURLFromText_PrefersNuonuoParamListOverPortalRoot(t *testing.T) {
	body := `
Portal: https://fp.nuonuo.com/#/
Invoice: https://fp.nuonuo.com/#/scan-invoice/printQrcode?paramList=abc
`
	got := bestPreviewURLFromText(body)
	if got != "https://fp.nuonuo.com/#/scan-invoice/printQrcode?paramList=abc" {
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

func TestIsNuonuoPortalRootURL(t *testing.T) {
	u, err := url.Parse("https://fp.nuonuo.com/#/")
	if err != nil {
		t.Fatal(err)
	}
	if !isNuonuoPortalRootURL(u) {
		t.Fatalf("expected nuonuo portal root url")
	}

	u2, err := url.Parse("https://fp.nuonuo.com/#/scan-invoice/printQrcode?paramList=abc")
	if err != nil {
		t.Fatal(err)
	}
	if isNuonuoPortalRootURL(u2) {
		t.Fatalf("unexpected portal root for invoice-specific url")
	}
}

func TestIsNuonuoNonInvoicePortalRootURL(t *testing.T) {
	u, err := url.Parse("https://nst.nuonuo.com/#/")
	if err != nil {
		t.Fatal(err)
	}
	if !isNuonuoNonInvoicePortalRootURL(u) {
		t.Fatalf("expected nuonuo non-invoice portal root url")
	}

	u2, err := url.Parse("https://nst.nuonuo.com/#/home")
	if err != nil {
		t.Fatal(err)
	}
	if isNuonuoNonInvoicePortalRootURL(u2) {
		t.Fatalf("unexpected portal root for non-root fragment")
	}
}
