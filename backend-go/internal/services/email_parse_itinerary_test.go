package services

import "testing"

func TestShouldParseExtraPDFAsInvoice_ItineraryWhitelist(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"滴滴行程单.pdf", false},
		{"高德行程单.pdf", false},
		{"航空电子行程单.pdf", true},
		{"机票行程单.pdf", true},
		{"航班行程单.pdf", true},
		{"高铁行程单.pdf", true},
		{"动车行程单.pdf", true},
		{"铁路电子客票行程单.pdf", true},
		{"invoice.pdf", true},
		{"增值税电子普通发票.pdf", true},
	}
	for _, c := range cases {
		if got := shouldParseExtraPDFAsInvoice(c.name); got != c.want {
			t.Fatalf("shouldParseExtraPDFAsInvoice(%q)=%v want %v", c.name, got, c.want)
		}
	}
}

