package claimmapping

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestMapPaymentBuildsEvidenceAndAppliesChineseTimezoneDefault(t *testing.T) {
	t.Parallel()
	source := domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "payment",
		Payment: raw(`{
			"amount":{"text":"CNY 101.37","page":1},
			"currency":{"text":"CNY","page":1},
			"merchant":{"text":"星河科技有限公司","page":1},
			"transaction_time":{"text":"2026年8月29日 14:35","page":1},
			"timezone":null,
			"payment_method":null,
			"order_number":{"text":"SYN-PAY-0001","page":1},
			"category":null,
			"discount":{"text":"2.00 元","page":1}
		}`),
		Invoice: raw(`null`),
	}

	claim, err := Map(source)
	if err != nil {
		t.Fatal(err)
	}
	if claim.SchemaVersion != ClaimSchemaVersion || claim.DocumentType != "payment" || len(claim.Fields) != 9 {
		t.Fatalf("claim identity = %#v", claim)
	}
	amount := fieldByPath(t, claim, "amount_minor")
	if string(amount.Value) != "10137" || len(amount.Evidence) != 1 || amount.Evidence[0].Quote != "CNY 101.37" {
		t.Fatalf("amount = %#v", amount)
	}
	transactionTime := fieldByPath(t, claim, "transaction_time")
	if string(transactionTime.Value) != `"2026-08-29T14:35:00+08:00"` {
		t.Fatalf("transaction time = %#v", transactionTime)
	}
	timezone := fieldByPath(t, claim, "source_timezone")
	if string(timezone.Value) != `"Asia/Shanghai"` || len(timezone.Evidence) != 0 {
		t.Fatalf("default timezone = %#v", timezone)
	}
	supplementary := fieldByPath(t, claim, "supplementary_fields")
	if supplementary.Presence != "present" || len(supplementary.Evidence) != 1 || !json.Valid(supplementary.Value) {
		t.Fatalf("supplementary field = %#v", supplementary)
	}
	validated := domain.ValidateClaim(claim, 1)
	if validated.Status != domain.ClaimReadyForReview {
		t.Fatalf("valid payment was blocked: %#v", validated.Validations)
	}
}

func TestMapPaymentUsesExplicitVisibleTimezone(t *testing.T) {
	t.Parallel()
	claim, err := Map(domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "payment",
		Payment: raw(`{
			"amount":{"text":"USD 9.07","page":1},
			"currency":{"text":"USD","page":1},
			"merchant":{"text":"Literal Dot Market","page":1},
			"transaction_time":{"text":"2026-09-11 07:08:09","page":1},
			"timezone":{"text":"America/Chicago","page":1},
			"payment_method":null,"order_number":null,"category":null
		}`),
		Invoice: raw(`null`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(fieldByPath(t, claim, "transaction_time").Value); got != `"2026-09-11T07:08:09-05:00"` {
		t.Fatalf("explicit-zone transaction time = %s", got)
	}
	timezone := fieldByPath(t, claim, "source_timezone")
	if string(timezone.Value) != `"America/Chicago"` || timezone.Evidence[0].Quote != "America/Chicago" {
		t.Fatalf("explicit timezone = %#v", timezone)
	}
}

func TestMapInvoiceUsesGrossTotalAndDoesNotCalculateBlankValues(t *testing.T) {
	t.Parallel()
	source := domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "invoice",
		Payment:       raw(`null`),
		Invoice: raw(`{
			"invoice_number":{"text":"00012345","page":1},
			"invoice_date":{"text":"2026年08月29日","page":1},
			"amount_without_tax":{"text":"￥100.00","page":1},
			"tax_amount":{"text":"￥6.00","page":1},
			"amount_with_tax":{"text":"￥106.00","page":1},
			"currency":{"text":"￥","page":1},
			"seller_name":{"text":"销售方有限公司","page":1},
			"buyer_name":{"text":"购买方有限公司","page":1},
			"items":[{
				"name":{"text":"软件服务","page":1},
				"quantity":{"text":"1.0","page":1},
				"unit":{"text":"项","page":1},
				"unit_price":null,
				"amount":{"text":"100.00","page":1},
				"tax":{"text":"6.00","page":1}
			}]
		}`),
	}

	claim, err := Map(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Fields) != 15 {
		t.Fatalf("invoice field count = %d, want 15", len(claim.Fields))
	}
	if got := string(fieldByPath(t, claim, "total_minor").Value); got != "10600" {
		t.Fatalf("gross total = %s", got)
	}
	if got := string(fieldByPath(t, claim, "tax_minor").Value); got != "600" {
		t.Fatalf("tax = %s", got)
	}
	if got := string(fieldByPath(t, claim, "items[0].quantity").Value); got != `"1"` {
		t.Fatalf("canonical quantity = %s", got)
	}
	if fieldByPath(t, claim, "items[0].unit_price_minor").Presence != "absent" {
		t.Fatal("blank unit price was calculated instead of remaining absent")
	}
	if fieldByPath(t, claim, "supplementary_fields").Presence != "present" {
		t.Fatal("visible amount_without_tax was not preserved for review")
	}
	stabilized, err := domain.StabilizeItemPaths(claim, func() (string, error) {
		return "00000000-0000-0000-0000-000000000001", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	validated := domain.ValidateClaim(stabilized, 1)
	if validated.Status != domain.ClaimReadyForReview {
		t.Fatalf("valid invoice was blocked: %#v", validated.Validations)
	}
}

func TestMapMoneyHandlesVisibleLocaleWithoutFloatingPoint(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]struct {
		amount   string
		currency string
		minor    string
	}{
		"european":    {amount: "EUR 8.765,43", currency: "EUR", minor: "876543"},
		"thousands":   {amount: "$1,234.56", currency: "$", minor: "123456"},
		"large exact": {amount: "90071992547409.91 CNY", currency: "CNY", minor: "9007199254740991"},
	} {
		t.Run(name, func(t *testing.T) {
			claim, err := Map(domain.BillVisibleTextEnvelope{
				SchemaVersion: ExtractionSchemaVersion,
				DocumentType:  "payment",
				Payment: raw(`{
					"amount":{"text":"` + input.amount + `","page":1},
					"currency":{"text":"` + input.currency + `","page":1},
					"merchant":{"text":"Synthetic Merchant","page":1},
					"transaction_time":{"text":"2026-08-29 14:35","page":1},
					"timezone":null,"payment_method":null,"order_number":null,"category":null
				}`),
				Invoice: raw(`null`),
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := string(fieldByPath(t, claim, "amount_minor").Value); got != input.minor {
				t.Fatalf("minor units = %s, want %s", got, input.minor)
			}
		})
	}
}

func TestMapPreservesContractAndNormalizationFailuresAsBlockedFields(t *testing.T) {
	t.Parallel()
	claim, err := Map(domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "payment",
		Payment: raw(`{
			"amount":{"text":"28.80","page":1},
			"currency":null,
			"merchant":"旧版裸字符串",
			"transaction_time":{"text":"2026-08-29 14:35","page":0},
			"timezone":null,"payment_method":null,"order_number":null,"category":null
		}`),
		Invoice: raw(`null`),
	})
	if err != nil {
		t.Fatal(err)
	}
	amount := fieldByPath(t, claim, "amount_minor")
	if !contains(amount.Issues, "money_currency_unavailable") || string(amount.Value) != `"28.80"` {
		t.Fatalf("unconvertible amount = %#v", amount)
	}
	merchant := fieldByPath(t, claim, "merchant")
	if !contains(merchant.Issues, "invalid_visible_text_shape") {
		t.Fatalf("retired bare scalar shape was accepted: %#v", merchant)
	}
	validated := domain.ValidateClaim(claim, 1)
	if validated.Status != domain.ClaimBlocked ||
		!hasValidation(validated, "model_field_money_currency_unavailable") ||
		!hasValidation(validated, "model_field_invalid_visible_text_shape") ||
		!hasValidation(validated, "model_field_invalid_visible_text_page") {
		t.Fatalf("field failures were not explicit: %#v", validated.Validations)
	}
}

func TestMapOnlyRejectsInvalidRootIdentity(t *testing.T) {
	t.Parallel()
	for _, source := range []domain.BillVisibleTextEnvelope{
		{SchemaVersion: "bill-visible-text/0", DocumentType: "unknown"},
		{SchemaVersion: ExtractionSchemaVersion, DocumentType: "receipt"},
	} {
		if _, err := Map(source); err == nil {
			t.Fatal("invalid root identity was accepted")
		}
	}

	claim, err := Map(domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "payment",
		Payment:       raw(`[]`),
		Invoice:       raw(`{"unexpected":true}`),
	})
	if err != nil {
		t.Fatalf("local section errors must produce a blocked Claim: %v", err)
	}
	if !contains(claim.DocumentIssues, "invalid_business_section") ||
		!contains(claim.DocumentIssues, "conflicting_business_sections") {
		t.Fatalf("section issues = %#v", claim.DocumentIssues)
	}
}

func TestMapUnknownProducesNoBusinessFields(t *testing.T) {
	t.Parallel()
	claim, err := Map(domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "unknown",
		Payment:       raw(`null`),
		Invoice:       raw(`null`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Fields) != 0 || len(claim.DocumentIssues) != 0 {
		t.Fatalf("unknown claim = %#v", claim)
	}
	validated := domain.ValidateClaim(claim, 1)
	if validated.Status != domain.ClaimBlocked || !hasValidation(validated, "unknown_document_type") {
		t.Fatalf("unknown document was not blocked: %#v", validated.Validations)
	}
}

func TestNormalizeMoneyCoversApprovedFormatsAndRejectsConflicts(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		text      string
		currency  domain.Currency
		want      int64
		wantIssue string
	}{
		"jpy grouped integer": {text: "JPY 1,234", currency: domain.CurrencyJPY, want: 1234},
		"euro locale":         {text: "EUR 1.234,56", currency: domain.CurrencyEUR, want: 123456},
		"leading zeros":       {text: "USD 000.01", currency: domain.CurrencyUSD, want: 1},
		"two currencies":      {text: "USD 1 CNY", currency: domain.CurrencyUSD, wantIssue: "money_currency_conflict"},
		"field conflict":      {text: "$1.00", currency: domain.CurrencyEUR, wantIssue: "money_currency_conflict"},
		"bad grouping":        {text: "EUR 12,34.56", currency: domain.CurrencyEUR, wantIssue: "invalid_money_value"},
		"jpy fraction":        {text: "JPY 1234.0", currency: domain.CurrencyJPY, wantIssue: "invalid_money_value"},
		"unknown currency":    {text: "1.23", currency: domain.Currency("GBP"), wantIssue: "money_currency_unavailable"},
		"safe integer overflow": {
			text: "$9,007,199,254,740,992.00", currency: domain.CurrencyUSD, wantIssue: "invalid_money_value",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			value, issue := normalizeMoney(test.text, test.currency)
			if issue != test.wantIssue {
				t.Fatalf("issue = %q, want %q", issue, test.wantIssue)
			}
			if issue == "" && value != test.want {
				t.Fatalf("value = %#v, want %d", value, test.want)
			}
		})
	}
}

func TestNormalizeVisibleDateTimeTimezoneAndDecimal(t *testing.T) {
	t.Parallel()
	if value, issue := normalizeDate("2024/2/29"); issue != "" || value != "2024-02-29" {
		t.Fatalf("leap date = %#v, %q", value, issue)
	}
	if _, issue := normalizeDate("2025-02-29"); issue != "invalid_date_value" {
		t.Fatalf("invalid date issue = %q", issue)
	}
	if value, issue := normalizeTimezone("北京时间"); issue != "" || value != defaultSourceTimezone {
		t.Fatalf("Chinese timezone = %#v, %q", value, issue)
	}
	if _, issue := normalizeTimezone("Mars/Base"); issue != "invalid_timezone_value" {
		t.Fatalf("invalid timezone issue = %q", issue)
	}
	location, err := time.LoadLocation(defaultSourceTimezone)
	if err != nil {
		t.Fatal(err)
	}
	if value, issue := normalizeInstant("2026.08.29 14:35", location); issue != "" || value != "2026-08-29T14:35:00+08:00" {
		t.Fatalf("local instant = %#v, %q", value, issue)
	}
	if value, issue := normalizeInstant("2026-08-29T14:35:00+08:00", location); issue != "" || value != "2026-08-29T14:35:00+08:00" {
		t.Fatalf("RFC3339 instant = %#v, %q", value, issue)
	}
	if _, issue := normalizeInstant("2026-08-29T14:35:00Z", location); issue != "timezone_conflict" {
		t.Fatalf("timezone conflict issue = %q", issue)
	}
	if _, issue := normalizeInstant("2026-02-30 10:00", location); issue != "invalid_instant_value" {
		t.Fatalf("invalid instant issue = %q", issue)
	}
	for input, expected := range map[string]string{"001.2300": "1.23", "000.000": "0", "42": "42"} {
		value, issue := normalizeDecimal(input)
		if issue != "" || value != expected {
			t.Fatalf("decimal %q = %#v, %q; want %q", input, value, issue, expected)
		}
	}
	if _, issue := normalizeDecimal("1."); issue != "invalid_decimal_value" {
		t.Fatalf("invalid decimal issue = %q", issue)
	}
	for input, expected := range map[string]domain.Currency{
		"美元": domain.CurrencyUSD,
		"欧元": domain.CurrencyEUR,
		"日元": domain.CurrencyJPY,
	} {
		if value, ok := currencyToken(input); !ok || value != expected {
			t.Fatalf("currency %q = %q, %t", input, value, ok)
		}
	}
	if _, ok := currencyToken("英镑"); ok {
		t.Fatal("unsupported currency was accepted")
	}
}

func TestMapInfersCurrencyOnlyFromVisibleAmountText(t *testing.T) {
	t.Parallel()
	payment, err := Map(domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "payment",
		Payment: raw(`{
			"amount":{"text":"$12.34","page":1},"currency":null,
			"merchant":{"text":"Merchant","page":1},
			"transaction_time":{"text":"2026-08-29 14:35","page":1},
			"timezone":null,"payment_method":null,"order_number":null,"category":null
		}`),
		Invoice: raw(`null`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(fieldByPath(t, payment, "amount_minor").Value); got != "1234" {
		t.Fatalf("inferred payment amount = %s", got)
	}
	currency := fieldByPath(t, payment, "currency")
	if string(currency.Value) != `"USD"` || len(currency.Evidence) != 1 || currency.Evidence[0].Quote != "$12.34" {
		t.Fatalf("inferred payment currency = %#v", currency)
	}

	invoice, err := Map(domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "invoice",
		Payment:       raw(`null`),
		Invoice: raw(`{
			"invoice_number":{"text":"001","page":1},"invoice_date":{"text":"2026-08-29","page":1},
			"amount_without_tax":{"text":"人民币100.00","page":1},
			"tax_amount":{"text":"¥6.00","page":1},
			"amount_with_tax":{"text":"CNY106.00","page":1},"currency":null,
			"seller_name":{"text":"销售方","page":1},"buyer_name":{"text":"购买方","page":1},"items":null
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(fieldByPath(t, invoice, "currency").Value); got != `"CNY"` {
		t.Fatalf("inferred invoice currency = %s", got)
	}
	if got := string(fieldByPath(t, invoice, "total_minor").Value); got != "10600" {
		t.Fatalf("inferred invoice total = %s", got)
	}

	conflicting, err := Map(domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "invoice",
		Payment:       raw(`null`),
		Invoice: raw(`{
			"invoice_number":null,"invoice_date":null,
			"amount_without_tax":{"text":"¥100.00","page":1},
			"tax_amount":{"text":"¥6.00","page":1},
			"amount_with_tax":{"text":"$106.00","page":1},"currency":null,
			"seller_name":null,"buyer_name":null,"items":null
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(conflicting.DocumentIssues, "conflicting_values") {
		t.Fatalf("currency conflict issues = %#v", conflicting.DocumentIssues)
	}
	if fieldByPath(t, conflicting, "currency").Presence != "absent" ||
		!contains(fieldByPath(t, conflicting, "total_minor").Issues, "money_currency_unavailable") {
		t.Fatalf("currency conflict was silently resolved: %#v", conflicting.Fields)
	}
}

func TestMapCompactsValidInvoiceItemsAfterMalformedEntries(t *testing.T) {
	t.Parallel()
	claim, err := Map(domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "invoice",
		Payment:       raw(`null`),
		Invoice: raw(`{
			"invoice_number":{"text":"001","page":1},"invoice_date":{"text":"2026-08-29","page":1},
			"amount_without_tax":{"text":"100.00","page":1},"tax_amount":{"text":"6.00","page":1},
			"amount_with_tax":{"text":"106.00","page":1},"currency":{"text":"CNY","page":1},
			"seller_name":{"text":"销售方","page":1},"buyer_name":{"text":"购买方","page":1},
			"items":[null,{"name":{"text":"服务","page":1},"quantity":null,"unit":null,
				"unit_price":null,"amount":{"text":"100.00","page":1},"tax":{"text":"6.00","page":1},
				"specification":{"text":"标准版","page":1}}]
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(claim.DocumentIssues, "invalid_invoice_item") {
		t.Fatalf("malformed item issue = %#v", claim.DocumentIssues)
	}
	fieldByPath(t, claim, "items[0].name")
	for _, field := range claim.Fields {
		if strings.HasPrefix(field.Path, "items[1].") {
			t.Fatalf("valid item retained a non-contiguous path: %s", field.Path)
		}
	}
	stabilized, err := domain.StabilizeItemPaths(claim, func() (string, error) {
		return "00000000-0000-0000-0000-000000000001", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	validated := domain.ValidateClaim(stabilized, 1)
	if validated.Status != domain.ClaimBlocked || !hasValidation(validated, "invalid_invoice_item") {
		t.Fatalf("malformed item did not produce a reviewable blocked Claim: %#v", validated.Validations)
	}

	invalidCollection, err := Map(domain.BillVisibleTextEnvelope{
		SchemaVersion: ExtractionSchemaVersion,
		DocumentType:  "invoice",
		Payment:       raw(`null`),
		Invoice:       raw(`{"items":{}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(invalidCollection.DocumentIssues, "invalid_invoice_items") {
		t.Fatalf("invalid items collection = %#v", invalidCollection.DocumentIssues)
	}
}

func TestDecodeVisibleValueRejectsExpandedOrInvalidShapes(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"extra property": `{"text":"值","page":1,"confidence":0.9}`,
		"string page":    `{"text":"值","page":"1"}`,
		"blank text":     `{"text":" ","page":1}`,
		"long text":      `{"text":"` + strings.Repeat("字", 501) + `","page":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			value, present := decodeVisibleValue(raw(input))
			if !present || len(value.Issues) == 0 {
				t.Fatalf("invalid visible value was accepted: %#v", value)
			}
		})
	}
}

func raw(value string) json.RawMessage {
	return json.RawMessage(value)
}

func fieldByPath(t *testing.T, claim domain.ClaimEnvelope, path string) domain.FieldCandidate {
	t.Helper()
	for _, field := range claim.Fields {
		if field.Path == path {
			return field
		}
	}
	encoded, _ := json.Marshal(claim)
	t.Fatalf("field %s not found in %s", path, encoded)
	return domain.FieldCandidate{}
}

func hasValidation(claim domain.ValidatedClaim, code string) bool {
	for _, validation := range claim.Validations {
		if validation.RuleCode == code {
			return true
		}
	}
	return false
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
