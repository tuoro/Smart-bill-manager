package domain

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

	"golang.org/x/text/unicode/norm"
)

type ClaimValidation struct {
	FieldPath   string
	RuleCode    string
	Severity    string
	Status      string
	SafeMessage string
}

type ValidatedClaim struct {
	DocumentType DocumentType
	Fields       []FieldCandidate
	Validations  []ClaimValidation
	Status       ClaimStatus
}

type ClaimFieldSpec struct {
	Path      string
	ValueType string
	Required  bool
	Normalize bool
}

var paymentFieldSpecs = map[string]ClaimFieldSpec{
	"amount_minor":         {ValueType: "money_minor", Required: true},
	"currency":             {ValueType: "string", Required: true},
	"merchant":             {ValueType: "string", Required: true, Normalize: true},
	"transaction_time":     {ValueType: "instant", Required: true},
	"source_timezone":      {ValueType: "string", Required: true},
	"payment_method":       {ValueType: "string", Normalize: true},
	"order_number":         {ValueType: "string", Normalize: true},
	"category":             {ValueType: "string", Normalize: true},
	"supplementary_fields": {ValueType: "supplementary"},
}

var invoiceFieldSpecs = map[string]ClaimFieldSpec{
	"invoice_number":       {ValueType: "string", Required: true, Normalize: true},
	"invoice_date":         {ValueType: "date", Required: true},
	"total_minor":          {ValueType: "money_minor", Required: true},
	"tax_minor":            {ValueType: "money_minor"},
	"currency":             {ValueType: "string", Required: true},
	"seller_name":          {ValueType: "string", Required: true, Normalize: true},
	"buyer_name":           {ValueType: "string", Required: true, Normalize: true},
	"supplementary_fields": {ValueType: "supplementary"},
}

var tripFieldSpecs = map[string]ClaimFieldSpec{
	"origin":               {ValueType: "string", Normalize: true},
	"destination":          {ValueType: "string", Required: true, Normalize: true},
	"start_date":           {ValueType: "date", Required: true},
	"end_date":             {ValueType: "date", Required: true},
	"traveler_name":        {ValueType: "string", Normalize: true},
	"transport_type":       {ValueType: "string", Normalize: true},
	"booking_reference":    {ValueType: "string", Normalize: true},
	"supplementary_fields": {ValueType: "supplementary"},
}

var invoiceItemFieldSpecs = map[string]ClaimFieldSpec{
	"name":             {ValueType: "string", Required: true, Normalize: true},
	"quantity":         {ValueType: "decimal"},
	"unit":             {ValueType: "string", Normalize: true},
	"unit_price_minor": {ValueType: "money_minor"},
	"amount_minor":     {ValueType: "money_minor", Required: true},
	"tax_minor":        {ValueType: "money_minor"},
	"sort_order":       {ValueType: "integer", Required: true},
}

var (
	temporaryItemPath = regexp.MustCompile(`^items\[([0-9]+)\]\.([a-z][a-z0-9_]*)$`)
	stableItemPath    = regexp.MustCompile(`^items\[([a-f0-9-]{36})\]\.([a-z][a-z0-9_]*)$`)
	decimalPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
)

func StabilizeItemPaths(envelope ClaimEnvelope, newID func() (string, error)) (ClaimEnvelope, error) {
	result := envelope
	result.Fields = append([]FieldCandidate(nil), envelope.Fields...)
	keys := make(map[int]string)
	indexes := make(map[int]struct{})
	for _, field := range result.Fields {
		match := temporaryItemPath.FindStringSubmatch(field.Path)
		if match == nil {
			continue
		}
		index, _ := strconv.Atoi(match[1])
		indexes[index] = struct{}{}
	}
	for index := 0; index < len(indexes); index++ {
		if _, ok := indexes[index]; !ok {
			return ClaimEnvelope{}, NewRuleError("invalid_item_order", "发票明细下标必须从 0 连续排列", ErrInvalidInput)
		}
		key, err := newID()
		if err != nil {
			return ClaimEnvelope{}, fmt.Errorf("generate item key: %w", err)
		}
		keys[index] = key
	}
	for index := range result.Fields {
		match := temporaryItemPath.FindStringSubmatch(result.Fields[index].Path)
		if match == nil {
			continue
		}
		itemIndex, _ := strconv.Atoi(match[1])
		result.Fields[index].Path = "items[" + keys[itemIndex] + "]." + match[2]
	}
	return result, nil
}

func ValidateClaim(envelope ClaimEnvelope, pageCount int) ValidatedClaim {
	validated := ValidatedClaim{DocumentType: DocumentType(envelope.DocumentType), Status: ClaimReadyForReview}
	if envelope.SchemaVersion != "document-claim/3" || !validated.DocumentType.Valid() {
		validated.add("", "invalid_claim_envelope", "blocked", "blocked", "Claim 版本或文档类型不受支持")
		validated.Status = ClaimBlocked
		return validated
	}
	documentTypeValue, _ := json.Marshal(envelope.DocumentType)
	fields := make([]FieldCandidate, 0, len(envelope.Fields)+1)
	fields = append(fields, FieldCandidate{
		Path:            "document_type",
		ValueType:       "document_type",
		Presence:        "present",
		Value:           documentTypeValue,
		NormalizedValue: documentTypeValue,
		Issues:          []string{},
	})
	fields = append(fields, envelope.Fields...)
	specs := expectedSpecs(validated.DocumentType, fields)
	seen := make(map[string]struct{}, len(fields))
	uniqueFields := make([]FieldCandidate, 0, len(fields))
	for index := range fields {
		field := &fields[index]
		if _, exists := seen[field.Path]; exists {
			validated.add(field.Path, "duplicate_field_path", "blocked", "blocked", "同一路径出现了多个候选字段")
			continue
		}
		seen[field.Path] = struct{}{}
		spec, expected := specs[field.Path]
		if field.Path == "document_type" {
			spec = ClaimFieldSpec{ValueType: "document_type", Required: true, Normalize: true}
			expected = true
		}
		if !expected {
			validated.add(field.Path, "unknown_field_path", "blocked", "blocked", "候选字段不属于当前文档类型")
			uniqueFields = append(uniqueFields, *field)
			continue
		}
		validateField(&validated, field, spec, pageCount)
		uniqueFields = append(uniqueFields, *field)
	}
	for path, spec := range specs {
		if _, exists := seen[path]; !exists {
			validated.add(path, "incomplete_claim_snapshot", "blocked", "blocked", "完整 Claim 快照缺少字段路径")
			continue
		}
		if spec.Required {
			for _, field := range uniqueFields {
				if field.Path == path && field.Presence != "present" {
					validated.add(path, "required_field_absent", "blocked", "blocked", "关键字段缺失，必须人工修订")
				}
			}
		}
	}
	validateDocumentIssues(&validated, envelope.DocumentIssues)
	if validated.DocumentType == DocumentInvoice {
		validateInvoiceTotals(&validated, uniqueFields)
		_, pageValidations := analyzeClaimPagePlan(validated.DocumentType, uniqueFields, pageCount)
		validated.Validations = append(validated.Validations, pageValidations...)
	}
	if validated.DocumentType == DocumentTrip {
		validateTripDates(&validated, uniqueFields)
	}
	if validated.DocumentType == DocumentUnknown {
		validated.add("document_type", "unknown_document_type", "blocked", "blocked", "文档无法归类，不能创建 Fact")
	}
	sort.Slice(uniqueFields, func(left, right int) bool { return uniqueFields[left].Path < uniqueFields[right].Path })
	validated.Fields = uniqueFields
	if validated.blocked() {
		validated.Status = ClaimBlocked
	} else {
		validated.add("", "claim_snapshot_complete", "info", "passed", "完整 Claim 快照、字段类型和证据检查通过")
	}
	return validated
}

func expectedSpecs(documentType DocumentType, fields []FieldCandidate) map[string]ClaimFieldSpec {
	result := make(map[string]ClaimFieldSpec)
	switch documentType {
	case DocumentPayment:
		copySpecs(result, paymentFieldSpecs, "")
	case DocumentInvoice:
		copySpecs(result, invoiceFieldSpecs, "")
		keys := make(map[string]struct{})
		for _, field := range fields {
			match := stableItemPath.FindStringSubmatch(field.Path)
			if match != nil {
				keys[match[1]] = struct{}{}
			}
		}
		for key := range keys {
			copySpecs(result, invoiceItemFieldSpecs, "items["+key+"].")
		}
	case DocumentTrip:
		copySpecs(result, tripFieldSpecs, "")
	}
	return result
}

func validateTripDates(validated *ValidatedClaim, fields []FieldCandidate) {
	values := make(map[string]string, 2)
	for _, field := range fields {
		if field.Presence != "present" || (field.Path != "start_date" && field.Path != "end_date") {
			continue
		}
		if value, ok := rawString(field.Value); ok {
			values[field.Path] = value
		}
	}
	start, startErr := time.Parse("2006-01-02", values["start_date"])
	end, endErr := time.Parse("2006-01-02", values["end_date"])
	if startErr == nil && endErr == nil && end.Before(start) {
		validated.add("end_date", "trip_date_range_invalid", "blocked", "blocked", "行程结束日期不能早于开始日期")
	}
}

func copySpecs(destination map[string]ClaimFieldSpec, source map[string]ClaimFieldSpec, prefix string) {
	for path, spec := range source {
		destination[prefix+path] = spec
	}
}

func validateField(validated *ValidatedClaim, field *FieldCandidate, spec ClaimFieldSpec, pageCount int) {
	if field.ValueType != spec.ValueType {
		validated.add(field.Path, "field_type_mismatch", "blocked", "blocked", "字段类型与契约不一致")
	}
	if field.Presence != "present" && field.Presence != "absent" {
		validated.add(field.Path, "invalid_presence", "blocked", "blocked", "字段存在性不合法")
		return
	}
	if field.Presence == "absent" {
		if len(field.Value) != 0 || len(field.NormalizedValue) != 0 || len(field.Evidence) != 0 {
			validated.add(field.Path, "absent_field_payload", "blocked", "blocked", "缺失墓碑不能携带值或证据")
		}
		return
	}
	validateFieldIssues(validated, field)
	if len(field.Value) == 0 || bytes.Equal(field.Value, []byte("null")) {
		validated.add(field.Path, "present_field_without_value", "blocked", "blocked", "存在字段必须携带确定类型的值")
		return
	}
	if err := validateTypedValue(field); err != nil {
		validated.add(field.Path, "invalid_typed_value", "blocked", "blocked", err.Error())
	}
	if spec.Normalize {
		if value, ok := rawString(field.Value); ok {
			normalized, _ := json.Marshal(NormalizeExact(value))
			field.NormalizedValue = normalized
		}
	}
	if field.Path != "document_type" {
		requiresEvidence := field.Path != "source_timezone" && !strings.HasSuffix(field.Path, "].sort_order")
		if len(field.Evidence) == 0 && requiresEvidence {
			if field.ValueType == "supplementary" {
				validated.add(field.Path, "missing_supplementary_evidence", "warning", "warning", "补充识别字段没有逐项证据，仅供人工复核")
			} else {
				validated.add(field.Path, "missing_field_evidence", "blocked", "blocked", "字段缺少原始证据")
			}
		}
		if len(field.Evidence) > 8 {
			validated.add(field.Path, "too_many_field_evidence", "blocked", "blocked", "单个字段最多绑定 8 条证据")
		}
		for _, evidence := range field.Evidence {
			if evidence.Page < 1 || evidence.Page > pageCount {
				validated.add(field.Path, "invalid_evidence_page", "blocked", "blocked", "证据页码超出 Document 范围")
			}
			if strings.TrimSpace(evidence.Quote) == "" && len(evidence.Region) == 0 {
				validated.add(field.Path, "empty_field_evidence", "blocked", "blocked", "证据必须包含摘录或区域")
			}
			if len([]rune(evidence.Quote)) > 500 {
				validated.add(field.Path, "evidence_quote_too_long", "blocked", "blocked", "证据摘录不能超过 500 个字符")
			}
			if len(evidence.Region) != 0 && !validEvidenceRegion(evidence.Region) {
				validated.add(field.Path, "invalid_evidence_region", "blocked", "blocked", "证据区域必须是页面内的归一化矩形")
			}
		}
	}
}

func validateFieldIssues(validated *ValidatedClaim, field *FieldCandidate) {
	type issueDescription struct {
		RuleCode string
		Message  string
	}
	descriptions := map[string]issueDescription{
		"invalid_visible_text_shape": {RuleCode: "model_field_invalid_visible_text_shape", Message: "模型字段不是约定的票面文字与页码对象"},
		"invalid_visible_text_value": {RuleCode: "model_field_invalid_visible_text_value", Message: "模型没有返回可用的票面原文"},
		"invalid_visible_text_page":  {RuleCode: "model_field_invalid_visible_text_page", Message: "模型返回的票面页码不合法"},
		"invalid_currency_value":     {RuleCode: "model_field_invalid_currency_value", Message: "票面币种不能确定性规范化"},
		"invalid_money_value":        {RuleCode: "model_field_invalid_money_value", Message: "票面金额不能按币种精确换算"},
		"money_currency_unavailable": {RuleCode: "model_field_money_currency_unavailable", Message: "金额缺少有效币种，不能换算为最小单位"},
		"money_currency_conflict":    {RuleCode: "model_field_money_currency_conflict", Message: "金额文字与币种字段互相冲突"},
		"invalid_date_value":         {RuleCode: "model_field_invalid_date_value", Message: "票面日期不能确定性规范化"},
		"invalid_instant_value":      {RuleCode: "model_field_invalid_instant_value", Message: "票面交易时间不能确定性规范化"},
		"invalid_timezone_value":     {RuleCode: "model_field_invalid_timezone_value", Message: "票面时区不是受支持的明确时区"},
		"timezone_unavailable":       {RuleCode: "model_field_timezone_unavailable", Message: "交易时间缺少可用来源时区"},
		"timezone_conflict":          {RuleCode: "model_field_timezone_conflict", Message: "交易时间中的偏移与来源时区冲突"},
		"invalid_decimal_value":      {RuleCode: "model_field_invalid_decimal_value", Message: "票面数量不能确定性规范化"},
	}
	for _, issue := range field.Issues {
		description, exists := descriptions[issue]
		if !exists {
			description = issueDescription{RuleCode: "model_field_invalid", Message: "模型字段存在局部格式问题，必须人工修订"}
		}
		validated.add(field.Path, description.RuleCode, "blocked", "blocked", description.Message)
	}
}

func validateTypedValue(field *FieldCandidate) error {
	switch field.ValueType {
	case "string", "document_type":
		value, ok := rawString(field.Value)
		if !ok || strings.TrimSpace(value) == "" || len([]rune(value)) > 500 {
			return errors.New("文本字段为空或超出 500 个字符")
		}
		if field.ValueType == "document_type" && !DocumentType(value).Valid() {
			return errors.New("文档类型不受支持")
		}
		if field.Path == "currency" {
			if _, ok := Currency(value).Exponent(); !ok {
				return errors.New("币种仅支持 CNY、USD、EUR 和 JPY")
			}
		}
		if field.Path == "source_timezone" {
			if _, err := time.LoadLocation(value); err != nil {
				return errors.New("来源时区不是有效的 IANA 时区")
			}
		}
	case "money_minor":
		value, err := rawInteger(field.Value)
		if err != nil || value < 0 || value > MaxSafeMinorUnits {
			return errors.New("金额必须是浏览器与 JSON 可精确表示的非负整数")
		}
	case "integer":
		value, err := rawInteger(field.Value)
		if err != nil || value < 0 || value > MaxSafeMinorUnits {
			return errors.New("整数值必须在浏览器与 JSON 可精确表示的非负范围内")
		}
	case "date":
		value, ok := rawString(field.Value)
		if !ok {
			return errors.New("日期必须是 YYYY-MM-DD 字符串")
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return errors.New("日期必须是有效的 YYYY-MM-DD")
		}
	case "instant":
		value, ok := rawString(field.Value)
		if !ok {
			return errors.New("时间必须是 RFC3339 字符串")
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return errors.New("时间必须是有效的 RFC3339")
		}
	case "decimal":
		value, ok := rawString(field.Value)
		if !ok || !decimalPattern.MatchString(value) {
			return errors.New("十进制数量格式不正确")
		}
	case "supplementary":
		if err := validateSupplementaryValue(field.Value); err != nil {
			return err
		}
	default:
		return errors.New("字段类型不受支持")
	}
	return nil
}

func validateDocumentIssues(validated *ValidatedClaim, issues []string) {
	blockedIssues := map[string]string{
		"ambiguous_document_type":       "文档类型无法安全确定",
		"cross_page_continuation":       "检测到跨页续行，M1 不进行猜测拼接",
		"ambiguous_repeated_header":     "重复表头归属不明确",
		"cross_page_total_conflict":     "跨页合计存在冲突",
		"uncertain_page_order":          "PDF 页序无法确定",
		"conflicting_values":            "关键字段存在冲突",
		"incomplete_document":           "单据内容不完整，无法安全形成事实",
		"missing_required_field":        "关键字段缺失",
		"conflicting_business_sections": "文档类型与业务区段相互冲突",
		"invalid_business_section":      "当前业务区段不是可审核的 JSON 对象",
		"missing_business_section":      "当前文档类型缺少对应业务区段",
		"invalid_evidence_collection":   "Evidence 不是可审核的数组",
		"invalid_model_evidence":        "模型返回了局部格式错误的 Evidence",
		"invalid_invoice_item":          "发票包含格式错误的明细行",
		"invalid_invoice_items":         "发票明细不是可审核的数组",
		"invalid_other_fields":          "补充识别字段格式不完整",
		"invalid_model_issues":          "模型问题列表格式不合法",
	}
	for _, issue := range issues {
		if message, blocked := blockedIssues[issue]; blocked {
			validated.add("", issue, "blocked", "blocked", message)
			continue
		}
		validated.add("", "model_issue_"+issue, "warning", "warning", "模型报告需要人工判断的问题："+issue)
	}
}

func validateSupplementaryValue(raw json.RawMessage) error {
	if len(raw) > 64*1024 {
		return errors.New("补充识别字段总大小不能超过 64 KiB")
	}
	var entries []struct {
		Path  string          `json:"path"`
		Label string          `json:"label"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 || len(entries) > 100 {
		return errors.New("补充识别字段必须包含 1 到 100 个条目")
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Path) == "" || len([]rune(entry.Path)) > 240 || len([]rune(entry.Label)) > 160 || len(entry.Value) == 0 {
			return errors.New("补充识别字段的路径、标签或值不合法")
		}
		var value any
		if err := json.Unmarshal(entry.Value, &value); err != nil {
			return errors.New("补充识别字段包含无效 JSON 值")
		}
	}
	return nil
}

func validEvidenceRegion(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || len(object) != 4 {
		return false
	}
	values := make(map[string]float64, 4)
	for _, key := range []string{"x", "y", "width", "height"} {
		var value float64
		if err := json.Unmarshal(object[key], &value); err != nil {
			return false
		}
		values[key] = value
	}
	return values["x"] >= 0 && values["x"] <= 1 &&
		values["y"] >= 0 && values["y"] <= 1 &&
		values["width"] > 0 && values["width"] <= 1 &&
		values["height"] > 0 && values["height"] <= 1 &&
		values["x"]+values["width"] <= 1 && values["y"]+values["height"] <= 1
}

func validateInvoiceTotals(validated *ValidatedClaim, fields []FieldCandidate) {
	values := make(map[string]int64)
	var itemTotal int64
	itemCount := 0
	itemTotalOverflow := false
	for _, field := range fields {
		if field.Presence != "present" || field.ValueType != "money_minor" {
			continue
		}
		value, err := rawInteger(field.Value)
		if err != nil {
			continue
		}
		values[field.Path] = value
		if strings.HasPrefix(field.Path, "items[") && strings.HasSuffix(field.Path, "].amount_minor") {
			if value > MaxSafeMinorUnits-itemTotal {
				itemTotalOverflow = true
			} else {
				itemTotal += value
			}
			itemCount++
		}
	}
	total, hasTotal := values["total_minor"]
	if tax, hasTax := values["tax_minor"]; hasTax && hasTotal && tax > total {
		validated.add("tax_minor", "tax_exceeds_total", "blocked", "blocked", "税额不能大于价税合计")
	}
	if itemCount > 0 && hasTotal {
		tax := values["tax_minor"]
		totalWithTaxMatches := tax <= MaxSafeMinorUnits-itemTotal && itemTotal+tax == total
		if itemTotalOverflow || (itemTotal != total && !totalWithTaxMatches) {
			validated.add("total_minor", "invoice_item_total_conflict", "blocked", "blocked", "明细金额与发票合计不一致")
		}
	}
}

func NormalizeExact(value string) string {
	normalized := []rune(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
	for index, character := range normalized {
		if character >= 'A' && character <= 'Z' {
			normalized[index] = character + ('a' - 'A')
		}
	}
	return string(normalized)
}

func rawString(value json.RawMessage) (string, bool) {
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		return "", false
	}
	return decoded, true
}

func rawInteger(value json.RawMessage) (int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return 0, err
	}
	number, ok := decoded.(json.Number)
	if !ok {
		return 0, errors.New("not an integer")
	}
	return strconv.ParseInt(number.String(), 10, 64)
}

func (v *ValidatedClaim) add(fieldPath, ruleCode, severity, status, safeMessage string) {
	v.Validations = append(v.Validations, ClaimValidation{
		FieldPath:   fieldPath,
		RuleCode:    ruleCode,
		Severity:    severity,
		Status:      status,
		SafeMessage: safeMessage,
	})
}

func (v ValidatedClaim) blocked() bool {
	for _, validation := range v.Validations {
		if validation.Status == "error" || validation.Status == "blocked" {
			return true
		}
	}
	return false
}
