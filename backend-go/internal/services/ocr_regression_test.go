package services

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type regressionSample struct {
	Kind     string          `json:"kind"`
	Name     string          `json:"name"`
	RawText  string          `json:"raw_text"`
	Expected json.RawMessage `json:"expected"`
}

type paymentExpected struct {
	Amount          *float64 `json:"amount,omitempty"`
	Merchant        *string  `json:"merchant,omitempty"`
	TransactionTime *string  `json:"transaction_time,omitempty"`
	PaymentMethod   *string  `json:"payment_method,omitempty"`
	OrderNumber     *string  `json:"order_number,omitempty"`
}

type invoiceExpected struct {
	InvoiceNumber *string  `json:"invoice_number,omitempty"`
	InvoiceDate   *string  `json:"invoice_date,omitempty"`
	Amount        *float64 `json:"amount,omitempty"`
	TaxAmount     *float64 `json:"tax_amount,omitempty"`
	SellerName    *string  `json:"seller_name,omitempty"`
	BuyerName     *string  `json:"buyer_name,omitempty"`
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
				assertPaymentExpected(t, exp, got)
			case "invoice":
				var exp invoiceExpected
				if err := json.Unmarshal(s.Expected, &exp); err != nil {
					t.Fatalf("unmarshal expected: %v", err)
				}
				got, err := svc.ParseInvoiceData(s.RawText)
				if err != nil {
					t.Fatalf("parse invoice: %v", err)
				}
				assertInvoiceExpected(t, exp, got)
			default:
				t.Fatalf("unknown kind: %s", s.Kind)
			}
		})
	}
}

func assertPaymentExpected(t *testing.T, exp paymentExpected, got *PaymentExtractedData) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil parsed payment")
	}

	if exp.Merchant != nil {
		if got.Merchant == nil {
			t.Fatalf("expected merchant=%q, got nil", *exp.Merchant)
		}
		if strings.TrimSpace(*got.Merchant) != strings.TrimSpace(*exp.Merchant) {
			t.Fatalf("expected merchant=%q, got %q", *exp.Merchant, *got.Merchant)
		}
	}

	if exp.TransactionTime != nil {
		if got.TransactionTime == nil {
			t.Fatalf("expected transaction_time=%q, got nil", *exp.TransactionTime)
		}
		expT, expErr := parseAnyPaymentTimeToUTC(*exp.TransactionTime)
		gotT, gotErr := parseAnyPaymentTimeToUTC(*got.TransactionTime)
		if expErr == nil && gotErr == nil {
			if !expT.Equal(gotT) {
				t.Fatalf("expected transaction_time(utc)=%s, got %s", expT.Format(time.RFC3339), gotT.Format(time.RFC3339))
			}
		} else if strings.TrimSpace(*got.TransactionTime) != strings.TrimSpace(*exp.TransactionTime) {
			t.Fatalf("expected transaction_time=%q, got %q", *exp.TransactionTime, *got.TransactionTime)
		}
	}

	if exp.PaymentMethod != nil {
		if got.PaymentMethod == nil {
			t.Fatalf("expected payment_method=%q, got nil", *exp.PaymentMethod)
		}
		if strings.TrimSpace(*got.PaymentMethod) != strings.TrimSpace(*exp.PaymentMethod) {
			t.Fatalf("expected payment_method=%q, got %q", *exp.PaymentMethod, *got.PaymentMethod)
		}
	}

	if exp.OrderNumber != nil {
		if got.OrderNumber == nil {
			t.Fatalf("expected order_number=%q, got nil", *exp.OrderNumber)
		}
		if strings.TrimSpace(*got.OrderNumber) != strings.TrimSpace(*exp.OrderNumber) {
			t.Fatalf("expected order_number=%q, got %q", *exp.OrderNumber, *got.OrderNumber)
		}
	}

	if exp.Amount != nil {
		if got.Amount == nil {
			t.Fatalf("expected amount=%v, got nil", *exp.Amount)
		}
		if !approxEqMoney(math.Abs(*exp.Amount), math.Abs(*got.Amount)) {
			t.Fatalf("expected amount≈%v, got %v", *exp.Amount, *got.Amount)
		}
	}
}

func assertInvoiceExpected(t *testing.T, exp invoiceExpected, got *InvoiceExtractedData) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil parsed invoice")
	}

	if exp.InvoiceNumber != nil {
		if got.InvoiceNumber == nil {
			t.Fatalf("expected invoice_number=%q, got nil", *exp.InvoiceNumber)
		}
		if strings.TrimSpace(*got.InvoiceNumber) != strings.TrimSpace(*exp.InvoiceNumber) {
			t.Fatalf("expected invoice_number=%q, got %q", *exp.InvoiceNumber, *got.InvoiceNumber)
		}
	}

	if exp.InvoiceDate != nil {
		if got.InvoiceDate == nil {
			t.Fatalf("expected invoice_date=%q, got nil", *exp.InvoiceDate)
		}
		expD, expErr := normalizeAnyInvoiceDate(*exp.InvoiceDate)
		gotD, gotErr := normalizeAnyInvoiceDate(*got.InvoiceDate)
		if expErr == nil && gotErr == nil {
			if expD != gotD {
				t.Fatalf("expected invoice_date=%q, got %q", expD, gotD)
			}
		} else if strings.TrimSpace(*got.InvoiceDate) != strings.TrimSpace(*exp.InvoiceDate) {
			t.Fatalf("expected invoice_date=%q, got %q", *exp.InvoiceDate, *got.InvoiceDate)
		}
	}

	if exp.SellerName != nil {
		if got.SellerName == nil {
			t.Fatalf("expected seller_name=%q, got nil", *exp.SellerName)
		}
		if strings.TrimSpace(*got.SellerName) != strings.TrimSpace(*exp.SellerName) {
			t.Fatalf("expected seller_name=%q, got %q", *exp.SellerName, *got.SellerName)
		}
	}

	if exp.BuyerName != nil {
		if got.BuyerName == nil {
			t.Fatalf("expected buyer_name=%q, got nil", *exp.BuyerName)
		}
		if strings.TrimSpace(*got.BuyerName) != strings.TrimSpace(*exp.BuyerName) {
			t.Fatalf("expected buyer_name=%q, got %q", *exp.BuyerName, *got.BuyerName)
		}
	}

	if exp.Amount != nil {
		if got.Amount == nil {
			t.Fatalf("expected amount=%v, got nil", *exp.Amount)
		}
		if !approxEqMoney(*exp.Amount, *got.Amount) {
			t.Fatalf("expected amount=%v, got %v", *exp.Amount, *got.Amount)
		}
	}

	if exp.TaxAmount != nil {
		if got.TaxAmount == nil {
			t.Fatalf("expected tax_amount=%v, got nil", *exp.TaxAmount)
		}
		if !approxEqMoney(*exp.TaxAmount, *got.TaxAmount) {
			t.Fatalf("expected tax_amount=%v, got %v", *exp.TaxAmount, *got.TaxAmount)
		}
	}
}

func approxEqMoney(a float64, b float64) bool {
	return math.Abs(a-b) <= 0.01
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
