package services

import (
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"

	"smart-bill-manager/internal/models"
)

var (
	dateLikeRegex = regexp.MustCompile(`(\d{4})\D+(\d{1,2})\D+(\d{1,2})`)
)

func parseFlexibleDateTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}

	// Common machine formats.
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"2006/01/02",
		"2006.01.02 15:04:05",
		"2006.01.02 15:04",
		"2006.01.02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}

	// Chinese style date like "2025年10月11日" or any non-digit separators.
	if m := dateLikeRegex.FindStringSubmatch(s); len(m) == 4 {
		iso := m[1] + "-" + leftPad2(m[2]) + "-" + leftPad2(m[3])
		if t, err := time.Parse("2006-01-02", iso); err == nil {
			return t, true
		}
	}

	return time.Time{}, false
}

func leftPad2(s string) string {
	if len(s) >= 2 {
		return s
	}
	return "0" + s
}

func normalizeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}

	// Remove whitespace and punctuation/symbols.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}

	out := b.String()
	// Strip common company suffixes (CN).
	suffixes := []string{
		"有限责任公司",
		"股份有限公司",
		"有限公司",
		"有限责任",
		"公司",
		"集团",
	}
	for _, suf := range suffixes {
		out = strings.TrimSuffix(out, suf)
	}
	return out
}

func bigramJaccard(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return 1
	}

	as := bigramSet([]rune(a))
	bs := bigramSet([]rune(b))
	if len(as) == 0 || len(bs) == 0 {
		return 0
	}

	inter := 0
	union := make(map[string]struct{}, len(as)+len(bs))
	for k := range as {
		union[k] = struct{}{}
	}
	for k := range bs {
		union[k] = struct{}{}
		if _, ok := as[k]; ok {
			inter++
		}
	}
	return float64(inter) / float64(len(union))
}

func bigramSet(rs []rune) map[string]struct{} {
	if len(rs) < 2 {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(rs)-1)
	for i := 0; i < len(rs)-1; i++ {
		out[string(rs[i:i+2])] = struct{}{}
	}
	return out
}

func amountScore(invoiceAmount *float64, paymentAmount float64) float64 {
	if invoiceAmount == nil || *invoiceAmount <= 0 || paymentAmount <= 0 {
		return 0
	}
	diff := math.Abs(paymentAmount-*invoiceAmount) / *invoiceAmount
	switch {
	case diff <= 0.01:
		return 1
	case diff >= 0.3:
		return 0
	default:
		// Linear decay from 1% to 30%.
		return 1 - (diff-0.01)/(0.3-0.01)
	}
}

func dateScore(invoiceDate *string, paymentTime string) float64 {
	if invoiceDate == nil || *invoiceDate == "" || paymentTime == "" {
		return 0
	}
	invT, ok1 := parseFlexibleDateTime(*invoiceDate)
	payT, ok2 := parseFlexibleDateTime(paymentTime)
	if !ok1 || !ok2 {
		return 0
	}

	days := math.Abs(payT.Sub(invT).Hours()) / 24.0
	// 0 days => 1, 3 days => ~0.5, 14 days => small.
	return 1 / (1 + days/3.0)
}

func merchantScore(invoiceSeller *string, paymentMerchant *string) float64 {
	if invoiceSeller == nil || paymentMerchant == nil {
		return 0
	}
	a := normalizeName(*invoiceSeller)
	b := normalizeName(*paymentMerchant)
	return bigramJaccard(a, b)
}

func computeInvoicePaymentScore(invoice *models.Invoice, payment *models.Payment) float64 {
	if invoice == nil || payment == nil {
		return 0
	}
	aScore := amountScore(invoice.Amount, payment.Amount)
	dScore := dateScore(invoice.InvoiceDate, payment.TransactionTime)
	mScore := merchantScore(invoice.SellerName, payment.Merchant)

	// Weighted sum.
	return 0.55*aScore + 0.25*dScore + 0.20*mScore
}

