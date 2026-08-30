package claimmapping

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"golang.org/x/text/unicode/norm"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

const (
	Version                 = "claim-mapper/3"
	ExtractionSchemaVersion = "bill-visible-text/1"
	ClaimSchemaVersion      = "document-claim/2"
	defaultSourceTimezone   = "Asia/Shanghai"
)

type valueNormalizer func(string) (any, string)

type visibleValue struct {
	Text    string
	Page    int
	HasText bool
	Issues  []string
}

type supplementaryEntry struct {
	Path  string          `json:"path"`
	Label string          `json:"label,omitempty"`
	Value json.RawMessage `json:"value"`
}

type supplementaryValue struct {
	Entry    supplementaryEntry
	Evidence []domain.CandidateEvidence
}

var (
	moneyTextPattern = regexp.MustCompile(`(?i)^\s*(CNY|RMB|USD|EUR|JPY|人民币元?|美元|欧元|日元|元|¥|\$|€|円)?\s*([0-9][0-9.,]*)\s*(CNY|RMB|USD|EUR|JPY|人民币元?|美元|欧元|日元|元|¥|\$|€|円)?\s*$`)
	dateTextPattern  = regexp.MustCompile(`^([0-9]{4})[-/.年]([0-9]{1,2})[-/.月]([0-9]{1,2})(?:日)?$`)
	instantPattern   = regexp.MustCompile(`^([0-9]{4})[-/.年]([0-9]{1,2})[-/.月]([0-9]{1,2})(?:日)?[ T]([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?$`)
	decimalPattern   = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

// Map assembles a complete internal Claim snapshot from exact visible strings.
// The model only selects a business field and copies {text,page}; this function
// performs deterministic formatting, currency conversion and evidence binding.
// It never calculates a missing business value or repairs an OCR character.
func Map(source domain.BillVisibleTextEnvelope) (domain.ClaimEnvelope, error) {
	documentType := domain.DocumentType(source.DocumentType)
	if source.SchemaVersion != ExtractionSchemaVersion || !documentType.Valid() {
		return domain.ClaimEnvelope{}, errors.New("bill visible-text envelope identity is invalid")
	}

	claim := domain.ClaimEnvelope{
		SchemaVersion: ClaimSchemaVersion,
		DocumentType:  source.DocumentType,
	}
	switch documentType {
	case domain.DocumentPayment:
		if !nullish(source.Invoice) {
			claim.DocumentIssues = append(claim.DocumentIssues, "conflicting_business_sections")
		}
		fields, issues := mapPayment(source.Payment)
		claim.Fields = fields
		claim.DocumentIssues = append(claim.DocumentIssues, issues...)
	case domain.DocumentInvoice:
		if !nullish(source.Payment) {
			claim.DocumentIssues = append(claim.DocumentIssues, "conflicting_business_sections")
		}
		fields, issues := mapInvoice(source.Invoice)
		claim.Fields = fields
		claim.DocumentIssues = append(claim.DocumentIssues, issues...)
	case domain.DocumentUnknown:
		if !nullish(source.Payment) || !nullish(source.Invoice) {
			claim.DocumentIssues = append(claim.DocumentIssues, "conflicting_business_sections")
		}
	}
	claim.DocumentIssues = uniqueStrings(claim.DocumentIssues)
	return claim, nil
}

func mapPayment(raw json.RawMessage) ([]domain.FieldCandidate, []string) {
	section, issue := businessSection(raw)
	var issues []string
	if issue != "" {
		issues = append(issues, issue)
	}

	currencyField, currency, hasCurrency := mapCurrencyField(section["currency"])
	if currencyField.Presence == "absent" {
		if inferred, ok := inferCurrencyField(section["amount"]); ok {
			currencyField = inferred
			currency = domain.Currency(rawStringValue(inferred.Value))
			hasCurrency = true
		}
	}
	timezoneField, location, hasLocation := mapTimezoneField(section["timezone"])
	fields := []domain.FieldCandidate{
		mapMoneyField("amount_minor", section["amount"], currency, hasCurrency),
		currencyField,
		mapVisibleField("merchant", "string", section["merchant"], normalizeLiteral),
		mapInstantField(section["transaction_time"], location, hasLocation),
		timezoneField,
		mapVisibleField("payment_method", "string", section["payment_method"], normalizeLiteral),
		mapVisibleField("order_number", "string", section["order_number"], normalizeLiteral),
		mapVisibleField("category", "string", section["category"], normalizeLiteral),
	}
	known := stringSet(
		"amount", "currency", "merchant", "transaction_time", "timezone",
		"payment_method", "order_number", "category",
	)
	supplementary := collectObjectExtras("payment", section, known)
	fields = append(fields, supplementaryField(supplementary))
	return fields, issues
}

func mapInvoice(raw json.RawMessage) ([]domain.FieldCandidate, []string) {
	section, issue := businessSection(raw)
	var issues []string
	if issue != "" {
		issues = append(issues, issue)
	}

	currencyField, currency, hasCurrency := mapCurrencyField(section["currency"])
	if currencyField.Presence == "absent" {
		inferred, conflict, ok := inferCurrencyFromMany(
			section["amount_with_tax"],
			section["tax_amount"],
			section["amount_without_tax"],
		)
		if conflict {
			issues = append(issues, "conflicting_values")
		} else if ok {
			currencyField = inferred
			currency = domain.Currency(rawStringValue(inferred.Value))
			hasCurrency = true
		}
	}
	fields := []domain.FieldCandidate{
		mapVisibleField("invoice_number", "string", section["invoice_number"], normalizeLiteral),
		mapVisibleField("invoice_date", "date", section["invoice_date"], normalizeDate),
		mapMoneyField("total_minor", section["amount_with_tax"], currency, hasCurrency),
		mapMoneyField("tax_minor", section["tax_amount"], currency, hasCurrency),
		currencyField,
		mapVisibleField("seller_name", "string", section["seller_name"], normalizeLiteral),
		mapVisibleField("buyer_name", "string", section["buyer_name"], normalizeLiteral),
	}

	known := stringSet(
		"invoice_number", "invoice_date", "amount_without_tax", "tax_amount",
		"amount_with_tax", "currency", "seller_name", "buyer_name", "items",
	)
	supplementary := collectObjectExtras("invoice", section, known)
	if value, ok := supplementaryFromVisible(
		"invoice.amount_without_tax",
		"不含税金额",
		section["amount_without_tax"],
	); ok {
		supplementary = append(supplementary, value)
	}
	itemFields, itemSupplementary, itemIssues := mapInvoiceItems(section["items"], currency, hasCurrency)
	fields = append(fields, itemFields...)
	supplementary = append(supplementary, itemSupplementary...)
	issues = append(issues, itemIssues...)
	fields = append(fields, supplementaryField(supplementary))
	return fields, issues
}

func mapInvoiceItems(
	raw json.RawMessage,
	currency domain.Currency,
	hasCurrency bool,
) ([]domain.FieldCandidate, []supplementaryValue, []string) {
	if nullish(raw) {
		return nil, nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, nil, []string{"invalid_invoice_items"}
	}
	fields := make([]domain.FieldCandidate, 0, len(items)*7)
	var supplementary []supplementaryValue
	var issues []string
	known := stringSet("name", "quantity", "unit", "unit_price", "amount", "tax")
	validIndex := 0
	for sourceIndex, rawItem := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil || item == nil {
			issues = append(issues, "invalid_invoice_item")
			continue
		}
		prefix := fmt.Sprintf("items[%d].", validIndex)
		fields = append(fields,
			mapVisibleField(prefix+"name", "string", item["name"], normalizeLiteral),
			mapVisibleField(prefix+"quantity", "decimal", item["quantity"], normalizeDecimal),
			mapVisibleField(prefix+"unit", "string", item["unit"], normalizeLiteral),
			mapMoneyField(prefix+"unit_price_minor", item["unit_price"], currency, hasCurrency),
			mapMoneyField(prefix+"amount_minor", item["amount"], currency, hasCurrency),
			mapMoneyField(prefix+"tax_minor", item["tax"], currency, hasCurrency),
			domain.FieldCandidate{
				Path:      prefix + "sort_order",
				ValueType: "integer",
				Presence:  "present",
				Value:     mustJSON(validIndex),
				Issues:    []string{},
			},
		)
		modelPrefix := fmt.Sprintf("invoice.items[%d]", sourceIndex)
		supplementary = append(supplementary, collectObjectExtras(modelPrefix, item, known)...)
		validIndex++
	}
	return fields, supplementary, uniqueStrings(issues)
}

func businessSection(raw json.RawMessage) (map[string]json.RawMessage, string) {
	if nullish(raw) {
		return nil, "missing_business_section"
	}
	var section map[string]json.RawMessage
	if err := json.Unmarshal(raw, &section); err != nil || section == nil {
		return nil, "invalid_business_section"
	}
	return section, ""
}

func mapVisibleField(path, valueType string, raw json.RawMessage, normalize valueNormalizer) domain.FieldCandidate {
	source, present := decodeVisibleValue(raw)
	if !present {
		return absentField(path, valueType)
	}
	field := domain.FieldCandidate{
		Path:      path,
		ValueType: valueType,
		Presence:  "present",
		Value:     rawCopy(raw),
		Issues:    append([]string(nil), source.Issues...),
	}
	if !source.HasText {
		return field
	}
	trimmed := strings.TrimSpace(source.Text)
	field.Value = mustJSON(trimmed)
	field.Evidence = []domain.CandidateEvidence{{Page: source.Page, Quote: source.Text}}
	if normalize == nil {
		return field
	}
	value, issue := normalize(trimmed)
	if issue != "" {
		field.Issues = uniqueStrings(append(field.Issues, issue))
		return field
	}
	field.Value = mustJSON(value)
	return field
}

func mapCurrencyField(raw json.RawMessage) (domain.FieldCandidate, domain.Currency, bool) {
	field := mapVisibleField("currency", "string", raw, normalizeCurrency)
	if field.Presence != "present" {
		return field, "", false
	}
	currency := domain.Currency(rawStringValue(field.Value))
	if _, ok := currency.Exponent(); !ok {
		return field, "", false
	}
	return field, currency, true
}

func mapMoneyField(path string, raw json.RawMessage, currency domain.Currency, hasCurrency bool) domain.FieldCandidate {
	return mapVisibleField(path, "money_minor", raw, func(text string) (any, string) {
		if !hasCurrency {
			return nil, "money_currency_unavailable"
		}
		return normalizeMoney(text, currency)
	})
}

func mapTimezoneField(raw json.RawMessage) (domain.FieldCandidate, *time.Location, bool) {
	if nullish(raw) {
		location, err := time.LoadLocation(defaultSourceTimezone)
		if err != nil {
			return domain.FieldCandidate{
				Path:      "source_timezone",
				ValueType: "string",
				Presence:  "present",
				Value:     mustJSON(defaultSourceTimezone),
				Issues:    []string{"invalid_timezone_value"},
			}, nil, false
		}
		return domain.FieldCandidate{
			Path:      "source_timezone",
			ValueType: "string",
			Presence:  "present",
			Value:     mustJSON(defaultSourceTimezone),
			Issues:    []string{},
		}, location, true
	}
	field := mapVisibleField("source_timezone", "string", raw, normalizeTimezone)
	canonical := rawStringValue(field.Value)
	location, err := time.LoadLocation(canonical)
	if err != nil {
		return field, nil, false
	}
	return field, location, true
}

func mapInstantField(raw json.RawMessage, location *time.Location, hasLocation bool) domain.FieldCandidate {
	return mapVisibleField("transaction_time", "instant", raw, func(text string) (any, string) {
		if !hasLocation {
			return nil, "timezone_unavailable"
		}
		return normalizeInstant(text, location)
	})
}

func decodeVisibleValue(raw json.RawMessage) (visibleValue, bool) {
	if nullish(raw) {
		return visibleValue{}, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return visibleValue{Issues: []string{"invalid_visible_text_shape"}}, true
	}
	value := visibleValue{}
	if len(object) != 2 {
		value.Issues = append(value.Issues, "invalid_visible_text_shape")
	}
	for key := range object {
		if key != "text" && key != "page" {
			value.Issues = append(value.Issues, "invalid_visible_text_shape")
		}
	}
	if err := json.Unmarshal(object["text"], &value.Text); err != nil ||
		strings.TrimSpace(value.Text) == "" ||
		len([]rune(value.Text)) > 500 {
		value.Issues = append(value.Issues, "invalid_visible_text_value")
	} else {
		value.HasText = true
	}
	page, err := rawInt(object["page"])
	if err != nil || page < 1 || page > 20 {
		value.Issues = append(value.Issues, "invalid_visible_text_page")
	} else {
		value.Page = int(page)
	}
	value.Issues = uniqueStrings(value.Issues)
	return value, true
}

func normalizeLiteral(text string) (any, string) {
	value := strings.TrimSpace(text)
	if value == "" {
		return nil, "invalid_visible_text_value"
	}
	return value, ""
}

func normalizeCurrency(text string) (any, string) {
	value, ok := currencyToken(text)
	if !ok {
		return nil, "invalid_currency_value"
	}
	return string(value), ""
}

func currencyToken(text string) (domain.Currency, bool) {
	value := strings.ToUpper(strings.Join(strings.Fields(norm.NFKC.String(text)), ""))
	switch value {
	case "CNY", "RMB", "人民币", "人民币元", "元", "¥":
		return domain.CurrencyCNY, true
	case "USD", "美元", "$":
		return domain.CurrencyUSD, true
	case "EUR", "欧元", "€":
		return domain.CurrencyEUR, true
	case "JPY", "日元", "円":
		return domain.CurrencyJPY, true
	default:
		return "", false
	}
}

func normalizeMoney(text string, currency domain.Currency) (any, string) {
	number, explicitCurrency, hasExplicitCurrency, issue := moneyComponents(text)
	if issue != "" {
		return nil, issue
	}
	if hasExplicitCurrency && explicitCurrency != currency {
		return nil, "money_currency_conflict"
	}
	exponent, ok := currency.Exponent()
	if !ok {
		return nil, "money_currency_unavailable"
	}
	decimal, ok := normalizeMoneyNumber(number, exponent)
	if !ok {
		return nil, "invalid_money_value"
	}
	money, err := domain.ParseMoney(decimal, currency)
	if err != nil {
		return nil, "invalid_money_value"
	}
	return money.MinorUnits, ""
}

func moneyComponents(text string) (string, domain.Currency, bool, string) {
	match := moneyTextPattern.FindStringSubmatch(norm.NFKC.String(strings.TrimSpace(text)))
	if match == nil {
		return "", "", false, "invalid_money_value"
	}
	var currencies []domain.Currency
	for _, token := range []string{match[1], match[3]} {
		if token == "" {
			continue
		}
		currency, ok := currencyToken(token)
		if !ok {
			return "", "", false, "invalid_money_value"
		}
		currencies = append(currencies, currency)
	}
	if len(currencies) == 2 && currencies[0] != currencies[1] {
		return "", "", false, "money_currency_conflict"
	}
	if len(currencies) == 0 {
		return match[2], "", false, ""
	}
	return match[2], currencies[0], true, ""
}

func normalizeMoneyNumber(value string, exponent int) (string, bool) {
	if value == "" {
		return "", false
	}
	dotCount := strings.Count(value, ".")
	commaCount := strings.Count(value, ",")
	if dotCount > 0 && commaCount > 0 {
		decimalSeparator := "."
		thousandsSeparator := ","
		if strings.LastIndex(value, ",") > strings.LastIndex(value, ".") {
			decimalSeparator, thousandsSeparator = ",", "."
		}
		if exponent == 0 || strings.Count(value, decimalSeparator) != 1 {
			return "", false
		}
		parts := strings.Split(value, decimalSeparator)
		if len(parts) != 2 || len(parts[1]) < 1 || len(parts[1]) > exponent || !digits(parts[1]) {
			return "", false
		}
		whole, ok := normalizeGroupedWhole(parts[0], thousandsSeparator)
		if !ok {
			return "", false
		}
		return whole + "." + parts[1], true
	}

	separator := ""
	count := 0
	if dotCount > 0 {
		separator, count = ".", dotCount
	} else if commaCount > 0 {
		separator, count = ",", commaCount
	}
	if separator == "" {
		if !digits(value) {
			return "", false
		}
		return canonicalWhole(value), true
	}
	if count > 1 {
		return normalizeGroupedWhole(value, separator)
	}
	parts := strings.Split(value, separator)
	if len(parts) != 2 || !digits(parts[0]) || !digits(parts[1]) {
		return "", false
	}
	if exponent > 0 && len(parts[1]) <= exponent {
		return canonicalWhole(parts[0]) + "." + parts[1], true
	}
	if len(parts[1]) == 3 && len(parts[0]) >= 1 && len(parts[0]) <= 3 {
		return canonicalWhole(parts[0] + parts[1]), true
	}
	return "", false
}

func normalizeGroupedWhole(value, separator string) (string, bool) {
	parts := strings.Split(value, separator)
	if len(parts) < 2 || len(parts[0]) < 1 || len(parts[0]) > 3 || !digits(parts[0]) {
		return "", false
	}
	for _, part := range parts[1:] {
		if len(part) != 3 || !digits(part) {
			return "", false
		}
	}
	return canonicalWhole(strings.Join(parts, "")), true
}

func canonicalWhole(value string) string {
	trimmed := strings.TrimLeft(value, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

func normalizeDate(text string) (any, string) {
	match := dateTextPattern.FindStringSubmatch(norm.NFKC.String(strings.TrimSpace(text)))
	if match == nil {
		return nil, "invalid_date_value"
	}
	year, _ := strconv.Atoi(match[1])
	month, _ := strconv.Atoi(match[2])
	day, _ := strconv.Atoi(match[3])
	value := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if value.Year() != year || int(value.Month()) != month || value.Day() != day {
		return nil, "invalid_date_value"
	}
	return value.Format("2006-01-02"), ""
}

func normalizeTimezone(text string) (any, string) {
	value := strings.TrimSpace(norm.NFKC.String(text))
	switch strings.ToUpper(strings.Join(strings.Fields(value), "")) {
	case "北京时间", "中国标准时间", "UTC+8", "UTC+08:00", "GMT+8", "GMT+08:00", "+08:00":
		value = defaultSourceTimezone
	}
	if _, err := time.LoadLocation(value); err != nil {
		return nil, "invalid_timezone_value"
	}
	return value, ""
}

func normalizeInstant(text string, location *time.Location) (any, string) {
	value := strings.TrimSpace(norm.NFKC.String(text))
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		_, observedOffset := parsed.Zone()
		_, expectedOffset := parsed.In(location).Zone()
		if observedOffset != expectedOffset {
			return nil, "timezone_conflict"
		}
		return parsed.Format(time.RFC3339), ""
	}
	match := instantPattern.FindStringSubmatch(value)
	if match == nil {
		return nil, "invalid_instant_value"
	}
	parts := make([]int, 6)
	for index := range parts {
		if index+1 >= len(match) || match[index+1] == "" {
			continue
		}
		parts[index], _ = strconv.Atoi(match[index+1])
	}
	parsed := time.Date(parts[0], time.Month(parts[1]), parts[2], parts[3], parts[4], parts[5], 0, location)
	if parsed.Year() != parts[0] || int(parsed.Month()) != parts[1] || parsed.Day() != parts[2] ||
		parsed.Hour() != parts[3] || parsed.Minute() != parts[4] || parsed.Second() != parts[5] {
		return nil, "invalid_instant_value"
	}
	return parsed.Format(time.RFC3339), ""
}

func normalizeDecimal(text string) (any, string) {
	value := strings.TrimSpace(norm.NFKC.String(text))
	if !decimalPattern.MatchString(value) {
		return nil, "invalid_decimal_value"
	}
	parts := strings.Split(value, ".")
	whole := canonicalWhole(parts[0])
	if len(parts) == 1 {
		return whole, ""
	}
	fraction := strings.TrimRight(parts[1], "0")
	if fraction == "" {
		return whole, ""
	}
	return whole + "." + fraction, ""
}

func inferCurrencyField(raw json.RawMessage) (domain.FieldCandidate, bool) {
	source, present := decodeVisibleValue(raw)
	if !present || !source.HasText {
		return domain.FieldCandidate{}, false
	}
	_, currency, hasCurrency, issue := moneyComponents(source.Text)
	if issue != "" || !hasCurrency {
		return domain.FieldCandidate{}, false
	}
	return domain.FieldCandidate{
		Path:      "currency",
		ValueType: "string",
		Presence:  "present",
		Value:     mustJSON(string(currency)),
		Evidence:  []domain.CandidateEvidence{{Page: source.Page, Quote: source.Text}},
		Issues:    append([]string(nil), source.Issues...),
	}, true
}

func inferCurrencyFromMany(rawValues ...json.RawMessage) (domain.FieldCandidate, bool, bool) {
	var selected domain.FieldCandidate
	var selectedCurrency string
	found := false
	for _, raw := range rawValues {
		field, ok := inferCurrencyField(raw)
		if !ok {
			continue
		}
		currency := rawStringValue(field.Value)
		if found && currency != selectedCurrency {
			return domain.FieldCandidate{}, true, false
		}
		if !found {
			selected = field
			selectedCurrency = currency
			found = true
		}
	}
	return selected, false, found
}

func supplementaryFromVisible(path, label string, raw json.RawMessage) (supplementaryValue, bool) {
	if nullish(raw) {
		return supplementaryValue{}, false
	}
	source, _ := decodeVisibleValue(raw)
	value := rawCopy(raw)
	var evidence []domain.CandidateEvidence
	if source.HasText {
		value = mustJSON(strings.TrimSpace(source.Text))
		evidence = []domain.CandidateEvidence{{Page: source.Page, Quote: source.Text}}
	}
	return supplementaryValue{
		Entry:    supplementaryEntry{Path: path, Label: label, Value: value},
		Evidence: evidence,
	}, true
}

func collectObjectExtras(
	prefix string,
	values map[string]json.RawMessage,
	known map[string]struct{},
) []supplementaryValue {
	keys := sortedKeys(values)
	var result []supplementaryValue
	for _, key := range keys {
		if _, exists := known[key]; exists {
			continue
		}
		if value, ok := supplementaryFromVisible(prefix+"."+key, key, values[key]); ok {
			result = append(result, value)
		}
	}
	return result
}

func supplementaryField(values []supplementaryValue) domain.FieldCandidate {
	if len(values) == 0 {
		return absentField("supplementary_fields", "supplementary")
	}
	entries := make([]supplementaryEntry, 0, len(values))
	var evidence []domain.CandidateEvidence
	for _, value := range values {
		entries = append(entries, value.Entry)
		evidence = append(evidence, value.Evidence...)
	}
	return domain.FieldCandidate{
		Path:      "supplementary_fields",
		ValueType: "supplementary",
		Presence:  "present",
		Value:     mustJSON(entries),
		Evidence:  evidence,
		Issues:    []string{},
	}
}

func absentField(path, valueType string) domain.FieldCandidate {
	return domain.FieldCandidate{Path: path, ValueType: valueType, Presence: "absent", Issues: []string{}}
}

func nullish(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func rawInt(raw json.RawMessage) (int64, error) {
	if nullish(raw) {
		return 0, errors.New("missing integer")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return 0, err
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, errors.New("not an integer")
	}
	return strconv.ParseInt(number.String(), 10, 64)
}

func rawStringValue(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func rawCopy(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func sortedKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func digits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
