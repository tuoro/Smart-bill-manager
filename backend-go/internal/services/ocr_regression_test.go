package services

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"smart-bill-manager/internal/devtools/regressionfixtures"
)

type regressionSample struct {
	Synthetic  bool            `json:"synthetic"`
	Provenance string          `json:"provenance"`
	Scenario   string          `json:"scenario"`
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	RawText    string          `json:"raw_text"`
	Expected   json.RawMessage `json:"expected"`
}

type paymentExpected struct {
	Amount          *float64 `json:"amount,omitempty"`
	Merchant        *string  `json:"merchant,omitempty"`
	TransactionTime *string  `json:"transaction_time,omitempty"`
	PaymentMethod   *string  `json:"payment_method,omitempty"`
	OrderNumber     *string  `json:"order_number,omitempty"`
}

type invoiceExpected struct {
	InvoiceNumber *string               `json:"invoice_number,omitempty"`
	InvoiceDate   *string               `json:"invoice_date,omitempty"`
	Amount        *float64              `json:"amount,omitempty"`
	TaxAmount     *float64              `json:"tax_amount,omitempty"`
	SellerName    *string               `json:"seller_name,omitempty"`
	BuyerName     *string               `json:"buyer_name,omitempty"`
	Items         []invoiceItemExpected `json:"items,omitempty"`
}

type invoiceItemExpected struct {
	NameContains []string `json:"name_contains"`
	Unit         string   `json:"unit,omitempty"`
	Quantity     *float64 `json:"quantity,omitempty"`
}

func TestRegressionSamples(t *testing.T) {
	root := filepath.Join("testdata", "regression")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no regression samples found")
		return
	}

	var files []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk regression samples: %v", err)
	}
	if len(files) == 0 {
		t.Skip("no regression samples found")
		return
	}

	svc := NewOCRService()

	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read sample: %v", err)
			}

			var s regressionSample
			if err := json.Unmarshal(b, &s); err != nil {
				t.Fatalf("unmarshal sample: %v", err)
			}
			if strings.TrimSpace(s.Kind) == "" || strings.TrimSpace(s.Name) == "" {
				t.Fatalf("invalid sample metadata (kind/name required)")
			}
			if strings.TrimSpace(s.RawText) == "" {
				t.Fatalf("invalid sample (raw_text required)")
			}
			if !s.Synthetic || s.Provenance != "SYNTHETIC_FIXED_CONSTANTS" ||
				!strings.Contains(s.RawText, regressionfixtures.SyntheticMarker) {
				t.Fatalf("fixture must be explicitly marked as fixed synthetic data")
			}

			switch s.Kind {
			case "payment_screenshot":
				var exp paymentExpected
				if err := json.Unmarshal(s.Expected, &exp); err != nil {
					t.Fatalf("unmarshal expected: %v", err)
				}
				got, err := svc.ParsePaymentScreenshot(s.RawText)
				if err != nil {
					t.Fatalf("parse payment screenshot: %v", err)
				}
				if diffs := diffPaymentExpected(exp, got); len(diffs) > 0 {
					t.Fatalf("regression mismatch\nsample=%s\nkind=%s name=%s\n%s\nraw_text(head):\n%s",
						path,
						s.Kind,
						s.Name,
						strings.Join(diffs, "\n"),
						headLines(s.RawText, 12, 1200),
					)
				}
			case "invoice":
				var exp invoiceExpected
				if err := json.Unmarshal(s.Expected, &exp); err != nil {
					t.Fatalf("unmarshal expected: %v", err)
				}
				got, err := svc.ParseInvoiceData(s.RawText)
				if err != nil {
					t.Fatalf("parse invoice: %v", err)
				}
				if diffs := diffInvoiceExpected(exp, got); len(diffs) > 0 {
					t.Fatalf("regression mismatch\nsample=%s\nkind=%s name=%s\n%s\nraw_text(head):\n%s",
						path,
						s.Kind,
						s.Name,
						strings.Join(diffs, "\n"),
						headLines(s.RawText, 12, 1200),
					)
				}
				assertRequiredSyntheticScenario(t, s.Scenario, got)
			default:
				t.Fatalf("unknown kind: %s", s.Kind)
			}
		})
	}
}

func diffPaymentExpected(exp paymentExpected, got *PaymentExtractedData) []string {
	if got == nil {
		return []string{"diff: parsed payment is nil"}
	}

	diffs := make([]string, 0, 8)

	if exp.Merchant != nil {
		diffs = append(diffs, diffStringField("merchant", exp.Merchant, got.Merchant, normalizeLooseText)...)
	}

	if exp.TransactionTime != nil {
		if got.TransactionTime == nil {
			diffs = append(diffs, fmt.Sprintf("diff: transaction_time expected=%q got=<nil>", strings.TrimSpace(*exp.TransactionTime)))
		} else {
			expT, expErr := parseAnyPaymentTimeToUTC(*exp.TransactionTime)
			gotT, gotErr := parseAnyPaymentTimeToUTC(*got.TransactionTime)
			if expErr == nil && gotErr == nil {
				if !expT.Equal(gotT) {
					diffs = append(diffs, fmt.Sprintf(
						"diff: transaction_time expected(utc)=%s got(utc)=%s (raw expected=%q got=%q)",
						expT.Format(time.RFC3339),
						gotT.Format(time.RFC3339),
						strings.TrimSpace(*exp.TransactionTime),
						strings.TrimSpace(*got.TransactionTime),
					))
				}
			} else if strings.TrimSpace(*got.TransactionTime) != strings.TrimSpace(*exp.TransactionTime) {
				diffs = append(diffs, fmt.Sprintf("diff: transaction_time expected=%q got=%q", strings.TrimSpace(*exp.TransactionTime), strings.TrimSpace(*got.TransactionTime)))
			}
		}
	}

	if exp.PaymentMethod != nil {
		diffs = append(diffs, diffStringField("payment_method", exp.PaymentMethod, got.PaymentMethod, normalizeLooseText)...)
	}

	if exp.OrderNumber != nil {
		diffs = append(diffs, diffStringField("order_number", exp.OrderNumber, got.OrderNumber, normalizeIdentifier)...)
	}

	if exp.Amount != nil {
		if got.Amount == nil {
			diffs = append(diffs, fmt.Sprintf("diff: amount expected=%v got=<nil>", *exp.Amount))
		} else {
			expC := moneyCents(*exp.Amount)
			gotC := moneyCents(*got.Amount)
			if expC != gotC {
				diffs = append(diffs, fmt.Sprintf("diff: amount expected=%s got=%s (raw expected=%v got=%v)",
					formatCents(expC),
					formatCents(gotC),
					*exp.Amount,
					*got.Amount,
				))
			}
		}
	}

	return diffs
}

func diffInvoiceExpected(exp invoiceExpected, got *InvoiceExtractedData) []string {
	if got == nil {
		return []string{"diff: parsed invoice is nil"}
	}

	diffs := make([]string, 0, 8)

	if exp.InvoiceNumber != nil {
		diffs = append(diffs, diffStringField("invoice_number", exp.InvoiceNumber, got.InvoiceNumber, normalizeIdentifier)...)
	}

	if exp.InvoiceDate != nil {
		if got.InvoiceDate == nil {
			diffs = append(diffs, fmt.Sprintf("diff: invoice_date expected=%q got=<nil>", strings.TrimSpace(*exp.InvoiceDate)))
		} else {
			expD, expErr := normalizeAnyInvoiceDate(*exp.InvoiceDate)
			gotD, gotErr := normalizeAnyInvoiceDate(*got.InvoiceDate)
			if expErr == nil && gotErr == nil {
				if expD != gotD {
					diffs = append(diffs, fmt.Sprintf(
						"diff: invoice_date expected=%q got=%q (raw expected=%q got=%q)",
						expD,
						gotD,
						strings.TrimSpace(*exp.InvoiceDate),
						strings.TrimSpace(*got.InvoiceDate),
					))
				}
			} else if strings.TrimSpace(*got.InvoiceDate) != strings.TrimSpace(*exp.InvoiceDate) {
				diffs = append(diffs, fmt.Sprintf("diff: invoice_date expected=%q got=%q", strings.TrimSpace(*exp.InvoiceDate), strings.TrimSpace(*got.InvoiceDate)))
			}
		}
	}

	if exp.SellerName != nil {
		diffs = append(diffs, diffStringField("seller_name", exp.SellerName, got.SellerName, normalizeLooseText)...)
	}

	if exp.BuyerName != nil {
		diffs = append(diffs, diffStringField("buyer_name", exp.BuyerName, got.BuyerName, normalizeLooseText)...)
	}

	if exp.Amount != nil {
		if got.Amount == nil {
			diffs = append(diffs, fmt.Sprintf("diff: amount expected=%v got=<nil>", *exp.Amount))
		} else {
			expC := moneyCents(*exp.Amount)
			gotC := moneyCents(*got.Amount)
			if expC != gotC {
				diffs = append(diffs, fmt.Sprintf("diff: amount expected=%s got=%s (raw expected=%v got=%v)",
					formatCents(expC),
					formatCents(gotC),
					*exp.Amount,
					*got.Amount,
				))
			}
		}
	}

	if exp.TaxAmount != nil {
		if got.TaxAmount == nil {
			diffs = append(diffs, fmt.Sprintf("diff: tax_amount expected=%v got=<nil>", *exp.TaxAmount))
		} else {
			expC := moneyCents(*exp.TaxAmount)
			gotC := moneyCents(*got.TaxAmount)
			if expC != gotC {
				diffs = append(diffs, fmt.Sprintf("diff: tax_amount expected=%s got=%s (raw expected=%v got=%v)",
					formatCents(expC),
					formatCents(gotC),
					*exp.TaxAmount,
					*got.TaxAmount,
				))
			}
		}
	}

	if exp.Items != nil {
		if len(got.Items) != len(exp.Items) {
			diffs = append(diffs, fmt.Sprintf("diff: items expected_count=%d got_count=%d", len(exp.Items), len(got.Items)))
		} else {
			for index, expectedItem := range exp.Items {
				actualItem := got.Items[index]
				for _, token := range expectedItem.NameContains {
					if !strings.Contains(actualItem.Name, token) {
						diffs = append(diffs, fmt.Sprintf("diff: items[%d].name missing required synthetic token", index))
					}
				}
				if expectedItem.Unit != "" && actualItem.Unit != expectedItem.Unit {
					diffs = append(diffs, fmt.Sprintf("diff: items[%d].unit expected=%q got=%q", index, expectedItem.Unit, actualItem.Unit))
				}
				if expectedItem.Quantity != nil {
					if actualItem.Quantity == nil || math.Abs(*actualItem.Quantity-*expectedItem.Quantity) > 0.000001 {
						diffs = append(diffs, fmt.Sprintf("diff: items[%d].quantity mismatch", index))
					}
				}
			}
		}
	}

	return diffs
}

func assertRequiredSyntheticScenario(t *testing.T, scenario string, got *InvoiceExtractedData) {
	t.Helper()
	requireItemTokens := func(expectedCount int, tokens ...string) {
		t.Helper()
		if len(got.Items) != expectedCount {
			t.Fatalf("scenario %q expected %d parsed items, got %d", scenario, expectedCount, len(got.Items))
		}
		joined := ""
		for _, item := range got.Items {
			joined += " " + item.Name
		}
		for _, token := range tokens {
			if !strings.Contains(joined, token) {
				t.Fatalf("scenario %q did not preserve required synthetic route/service token", scenario)
			}
		}
	}

	switch scenario {
	case regressionfixtures.ScenarioBasic:
		requireItemTokens(1, "纯合成服务", "基础服务")
	case regressionfixtures.ScenarioMultiItem:
		requireItemTokens(2, "测试记录本", "测试配送")
	case regressionfixtures.ScenarioAirTicket:
		requireItemTokens(1, regressionfixtures.SyntheticAirOrigin, regressionfixtures.SyntheticAirDest, regressionfixtures.SyntheticFlightNo)
	case regressionfixtures.ScenarioRailTicket:
		requireItemTokens(1, regressionfixtures.SyntheticRailOrigin, regressionfixtures.SyntheticRailDest, regressionfixtures.SyntheticTrainNo)
	default:
		t.Fatalf("invoice fixture has unexpected synthetic scenario %q", scenario)
	}
}

func diffStringField(field string, exp *string, got *string, normalize func(string) string) []string {
	if exp == nil {
		return nil
	}
	expRaw := strings.TrimSpace(*exp)
	if got == nil {
		return []string{fmt.Sprintf("diff: %s expected=%q got=<nil>", field, expRaw)}
	}
	gotRaw := strings.TrimSpace(*got)
	expN := normalize(expRaw)
	gotN := normalize(gotRaw)
	if expN != gotN {
		return []string{fmt.Sprintf("diff: %s expected=%q got=%q (raw expected=%q got=%q)", field, expN, gotN, expRaw, gotRaw)}
	}
	return nil
}

func moneyCents(v float64) int64 {
	v = math.Abs(v)
	return int64(math.Round(v * 100))
}

func formatCents(c int64) string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

var (
	whitespaceRegex   = regexp.MustCompile(`\s+`)
	identifierKeepReg = regexp.MustCompile(`[A-Za-z0-9]+`)
)

func normalizeLooseText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.NewReplacer(
		"（", "(",
		"）", ")",
		"【", "[",
		"】", "]",
		"：", ":",
		"，", ",",
		"；", ";",
	).Replace(s)
	s = whitespaceRegex.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func normalizeIdentifier(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := identifierKeepReg.FindAllString(s, -1)
	return strings.ToLower(strings.Join(parts, ""))
}

func parseAnyPaymentTimeToUTC(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Truncate(time.Second), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC().Truncate(time.Second), nil
	}
	loc := loadLocationOrUTC("Asia/Shanghai")
	if t, err := parsePaymentTimeToUTC(s, loc); err == nil {
		return t.UTC().Truncate(time.Second), nil
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %q", s)
}

func normalizeAnyInvoiceDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty date")
	}
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		return s, nil
	}

	parts := make([]string, 0, 3)
	var cur strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			cur.WriteRune(r)
			continue
		}
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}

	// YYYYMMDD
	if len(parts) == 1 && len(parts[0]) == 8 {
		ds := parts[0]
		return fmt.Sprintf("%s-%s-%s", ds[0:4], ds[4:6], ds[6:8]), nil
	}

	if len(parts) >= 3 && len(parts[0]) == 4 {
		yyyy := parts[0]
		mm := parts[1]
		dd := parts[2]
		if len(mm) == 1 {
			mm = "0" + mm
		}
		if len(dd) == 1 {
			dd = "0" + dd
		}
		return fmt.Sprintf("%s-%s-%s", yyyy, mm, dd), nil
	}

	return "", fmt.Errorf("unsupported date format: %q", s)
}

func headLines(s string, maxLines int, maxChars int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	out := strings.Join(lines, "\n")
	out = strings.TrimSpace(out)
	if maxChars > 0 && len(out) > maxChars {
		return out[:maxChars] + "\n...(truncated)"
	}
	return out
}
