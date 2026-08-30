package domain

import (
	"errors"
	"testing"
)

func TestParseMoney(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		currency Currency
		want     int64
		wantErr  bool
	}{
		{"CNY integer", "12", CurrencyCNY, 1200, false},
		{"USD one decimal", "12.3", CurrencyUSD, 1230, false},
		{"EUR two decimals", "12.34", CurrencyEUR, 1234, false},
		{"JPY integer", "12", CurrencyJPY, 12, false},
		{"JPY rejects decimal", "12.0", CurrencyJPY, 0, true},
		{"rejects silent rounding", "12.345", CurrencyCNY, 0, true},
		{"rejects negative", "-1.00", CurrencyCNY, 0, true},
		{"rejects exponent", "1e2", CurrencyCNY, 0, true},
		{"rejects spaces", " 1.00", CurrencyCNY, 0, true},
		{"rejects unknown currency", "1", Currency("GBP"), 0, true},
		{"accepts maximum exact JSON integer", "90071992547409.91", CurrencyCNY, MaxSafeMinorUnits, false},
		{"rejects unsafe JSON integer", "90071992547409.92", CurrencyCNY, 0, true},
		{"rejects int64 overflow", "92233720368547759.00", CurrencyCNY, 0, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMoney(test.input, test.currency)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("error = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMoney() error = %v", err)
			}
			if got.MinorUnits != test.want || got.Currency != test.currency {
				t.Fatalf("ParseMoney() = %#v, want minor=%d currency=%s", got, test.want, test.currency)
			}
		})
	}

	if err := (Money{MinorUnits: MaxSafeMinorUnits, Currency: CurrencyCNY}).Validate(); err != nil {
		t.Fatalf("max money must be valid: %v", err)
	}
	if err := (Money{MinorUnits: MaxSafeMinorUnits + 1, Currency: CurrencyCNY}).Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsafe JSON money error = %v", err)
	}
	if err := (Money{MinorUnits: -1, Currency: CurrencyCNY}).Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative money error = %v", err)
	}
	if err := (Money{MinorUnits: 1, Currency: Currency("GBP")}).Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported currency error = %v", err)
	}
}
