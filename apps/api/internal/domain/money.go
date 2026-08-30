package domain

import (
	"fmt"
	"strconv"
	"strings"
)

type Currency string

// MaxSafeMinorUnits keeps every API amount exactly representable by JSON clients,
// including the browser runtime used by M1.
const MaxSafeMinorUnits int64 = 9_007_199_254_740_991

const (
	CurrencyCNY Currency = "CNY"
	CurrencyUSD Currency = "USD"
	CurrencyEUR Currency = "EUR"
	CurrencyJPY Currency = "JPY"
)

var currencyExponents = map[Currency]int{
	CurrencyCNY: 2,
	CurrencyUSD: 2,
	CurrencyEUR: 2,
	CurrencyJPY: 0,
}

type Money struct {
	MinorUnits int64    `json:"minor_units"`
	Currency   Currency `json:"currency"`
}

func (c Currency) Exponent() (int, bool) {
	exponent, ok := currencyExponents[c]
	return exponent, ok
}

func ParseMoney(decimal string, currency Currency) (Money, error) {
	exponent, ok := currency.Exponent()
	if !ok {
		return Money{}, NewRuleError("unsupported_currency", "仅支持 CNY、USD、EUR 和 JPY", ErrInvalidInput)
	}
	if decimal == "" || strings.TrimSpace(decimal) != decimal || strings.HasPrefix(decimal, "+") {
		return Money{}, NewRuleError("invalid_money", "金额必须是普通非负十进制字符串", ErrInvalidInput)
	}
	parts := strings.Split(decimal, ".")
	if len(parts) > 2 || parts[0] == "" || !digits(parts[0]) {
		return Money{}, NewRuleError("invalid_money", "金额格式不正确", ErrInvalidInput)
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" || !digits(fraction) {
			return Money{}, NewRuleError("invalid_money", "金额格式不正确", ErrInvalidInput)
		}
	}
	if len(fraction) > exponent {
		return Money{}, NewRuleError("currency_exponent_exceeded", "金额小数位超过币种精度", ErrInvalidInput)
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return Money{}, NewRuleError("invalid_money", "金额超出允许范围", ErrInvalidInput)
	}
	fraction += strings.Repeat("0", exponent-len(fraction))
	fractionValue := int64(0)
	if fraction != "" {
		fractionValue, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return Money{}, NewRuleError("invalid_money", "金额超出允许范围", ErrInvalidInput)
		}
	}
	multiplier := int64(1)
	for range exponent {
		multiplier *= 10
	}
	if whole > (MaxSafeMinorUnits-fractionValue)/multiplier {
		return Money{}, NewRuleError("invalid_money", "金额超出允许范围", ErrInvalidInput)
	}
	return Money{MinorUnits: whole*multiplier + fractionValue, Currency: currency}, nil
}

func (m Money) Validate() error {
	if _, ok := m.Currency.Exponent(); !ok {
		return fmt.Errorf("%w: currency", ErrInvalidInput)
	}
	if m.MinorUnits < 0 || m.MinorUnits > MaxSafeMinorUnits {
		return fmt.Errorf("%w: money is outside the exact JSON integer range", ErrInvalidInput)
	}
	return nil
}

func digits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
