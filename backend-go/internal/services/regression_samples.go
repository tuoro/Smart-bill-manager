package services

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

var ErrSampleNotFound = errors.New("regression sample not found")
var ErrRepoSampleDirNotFound = errors.New("repo sample dir not found")

type RegressionSampleService struct{}

func NewRegressionSampleService() *RegressionSampleService { return &RegressionSampleService{} }

type SampleQualityIssue struct {
	Level   string `json:"level"`   // error | warn
	Code    string `json:"code"`    // stable identifier
	Message string `json:"message"` // human readable
}

type SampleQualityError struct {
	Issues []SampleQualityIssue
}

func (e *SampleQualityError) Error() string { return "sample quality check failed" }

func hasQualityErrors(issues []SampleQualityIssue) bool {
	for _, it := range issues {
		if strings.EqualFold(strings.TrimSpace(it.Level), "error") {
			return true
		}
	}
	return false
}

type regressionSampleExpectedPayment struct {
	Amount          *float64 `json:"amount,omitempty"`
	Merchant        *string  `json:"merchant,omitempty"`
	TransactionTime *string  `json:"transaction_time,omitempty"`
	PaymentMethod   *string  `json:"payment_method,omitempty"`
	OrderNumber     *string  `json:"order_number,omitempty"`
}

type regressionSampleExpectedInvoice struct {
	InvoiceNumber *string  `json:"invoice_number,omitempty"`
	InvoiceDate   *string  `json:"invoice_date,omitempty"`
	Amount        *float64 `json:"amount,omitempty"`
	TaxAmount     *float64 `json:"tax_amount,omitempty"`
	SellerName    *string  `json:"seller_name,omitempty"`
	BuyerName     *string  `json:"buyer_name,omitempty"`
}

func normalizeSampleName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = regexp.MustCompile(`[^a-z0-9_\-]+`).ReplaceAllString(s, "_")
	s = strings.Trim(s, "_-")
	if s == "" {
		return ""
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:])
}

var (
	piiPhoneCNRegex = regexp.MustCompile(`\b1[3-9]\d{9}\b`)
	piiIDCNRegex    = regexp.MustCompile(`\b\d{17}[\dXx]\b`)
	piiEmailRegex   = regexp.MustCompile(`[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}`)

	invoiceDatePrefixRegex = regexp.MustCompile(`(\d{4})\D+(\d{1,2})\D+(\d{1,2})`)
	sampleTokenRegex       = regexp.MustCompile(`[^a-zA-Z0-9]+`)
	issuerNameLineRegex    = regexp.MustCompile(`^\s*(开票人|收款人|复核|经办人|出票人)\s*([:：])\s*(.+?)\s*$`)
)

func normalizeInvoiceDatePrefix(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	if len(s) >= 10 && s[4] == '/' && s[7] == '/' {
		return strings.ReplaceAll(s[:10], "/", "-")
	}
	if m := invoiceDatePrefixRegex.FindStringSubmatch(s); len(m) == 4 {
		month, err1 := strconv.Atoi(m[2])
		day, err2 := strconv.Atoi(m[3])
		if err1 != nil || err2 != nil || month < 1 || month > 12 || day < 1 || day > 31 {
			return ""
		}
		return fmt.Sprintf("%s-%02d-%02d", m[1], month, day)
	}
	return ""
}

func validateSampleRawText(raw string) []SampleQualityIssue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []SampleQualityIssue{{Level: "error", Code: "raw_text_empty", Message: "raw_text 为空"}}
	}

	var issues []SampleQualityIssue
	if len([]rune(raw)) < 20 {
		issues = append(issues, SampleQualityIssue{Level: "warn", Code: "raw_text_too_short", Message: "raw_text 过短，可能不利于回归"})
	}
	if lines := len(strings.Split(raw, "\n")); lines < 2 {
		issues = append(issues, SampleQualityIssue{Level: "warn", Code: "raw_text_too_few_lines", Message: "raw_text 行数太少，可能不利于回归"})
	}

	if piiPhoneCNRegex.MatchString(raw) {
		issues = append(issues, SampleQualityIssue{Level: "warn", Code: "pii_phone", Message: "raw_text 疑似包含手机号"})
	}
	if piiIDCNRegex.MatchString(raw) {
		issues = append(issues, SampleQualityIssue{Level: "warn", Code: "pii_id", Message: "raw_text 疑似包含身份证号"})
	}
	if piiEmailRegex.MatchString(raw) {
		issues = append(issues, SampleQualityIssue{Level: "warn", Code: "pii_email", Message: "raw_text 疑似包含邮箱"})
	}

	return issues
}

func redactSampleRawText(raw string) (string, []string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw, nil
	}

	rules := make([]string, 0, 4)
	out := raw

	// Email: keep first character of local-part.
	if piiEmailRegex.MatchString(out) {
		rules = append(rules, "email")
		out = piiEmailRegex.ReplaceAllStringFunc(out, func(s string) string {
			parts := strings.SplitN(s, "@", 2)
			if len(parts) != 2 {
				return "***"
			}
			local := parts[0]
			domain := parts[1]
			first := ""
			if len(local) > 0 {
				first = local[:1]
			}
			return first + "***@" + domain
		})
	}

	// CN ID: keep first 3 + last 3 chars.
	idRegex := regexp.MustCompile(`(^|[^0-9])(\d{17}[\dXx])([^0-9]|$)`)
	if idRegex.MatchString(out) {
		rules = append(rules, "cn_id")
		out = idRegex.ReplaceAllStringFunc(out, func(m string) string {
			sub := idRegex.FindStringSubmatch(m)
			if len(sub) != 4 {
				return m
			}
			prefix, id, suffix := sub[1], sub[2], sub[3]
			if len(id) < 6 {
				return prefix + "***" + suffix
			}
			return prefix + id[:3] + strings.Repeat("*", len(id)-6) + id[len(id)-3:] + suffix
		})
	}

	// CN mobile: keep first 3 + last 2 digits.
	phoneRegex := regexp.MustCompile(`(^|[^0-9])(1[3-9]\d{9})([^0-9]|$)`)
	if phoneRegex.MatchString(out) {
		rules = append(rules, "cn_mobile")
		out = phoneRegex.ReplaceAllStringFunc(out, func(m string) string {
			sub := phoneRegex.FindStringSubmatch(m)
			if len(sub) != 4 {
				return m
			}
			prefix, phone, suffix := sub[1], sub[2], sub[3]
			if len(phone) != 11 {
				return prefix + "***" + suffix
			}
			return prefix + phone[:3] + strings.Repeat("*", 6) + phone[9:] + suffix
		})
	}

	// Common invoice roles (often personal names) - keep first rune.
	lines := strings.Split(out, "\n")
	issuerChanged := false
	for i, line := range lines {
		m := issuerNameLineRegex.FindStringSubmatch(line)
		if len(m) != 4 {
			continue
		}
		label, colon, value := m[1], m[2], strings.TrimSpace(m[3])
		if value == "" {
			continue
		}
		runes := []rune(value)
		first := string(runes[0])
		lines[i] = label + colon + " " + first + "***"
		issuerChanged = true
	}
	if issuerChanged {
		rules = append(rules, "issuer_name")
		out = strings.Join(lines, "\n")
	}

	if len(rules) == 0 {
		return raw, nil
	}
	return out, rules
}

func validatePaymentSampleQuality(p *models.Payment, raw string) []SampleQualityIssue {
	var issues []SampleQualityIssue
	issues = append(issues, validateSampleRawText(raw)...)

	if p == nil {
		return append(issues, SampleQualityIssue{Level: "error", Code: "payment_missing", Message: "支付记录不存在"})
	}
	if p.Amount <= 0 {
		issues = append(issues, SampleQualityIssue{Level: "error", Code: "amount_missing", Message: "支付金额为空或无效"})
	}
	if strings.TrimSpace(p.TransactionTime) == "" {
		issues = append(issues, SampleQualityIssue{Level: "error", Code: "transaction_time_missing", Message: "交易时间为空"})
	} else if _, err := parseRFC3339ToUTC(p.TransactionTime); err != nil {
		issues = append(issues, SampleQualityIssue{Level: "error", Code: "transaction_time_invalid", Message: "交易时间格式不合法（需要 RFC3339）"})
	}

	if p.Merchant == nil || strings.TrimSpace(*p.Merchant) == "" {
		issues = append(issues, SampleQualityIssue{Level: "warn", Code: "merchant_missing", Message: "商家为空（可选，但建议补全）"})
	}
	if p.PaymentMethod == nil || strings.TrimSpace(*p.PaymentMethod) == "" {
		issues = append(issues, SampleQualityIssue{Level: "warn", Code: "payment_method_missing", Message: "支付方式为空（可选，但建议补全）"})
	}

	return issues
}

func validateInvoiceSampleQuality(inv *models.Invoice, raw string) []SampleQualityIssue {
	var issues []SampleQualityIssue
	issues = append(issues, validateSampleRawText(raw)...)

	if inv == nil {
		return append(issues, SampleQualityIssue{Level: "error", Code: "invoice_missing", Message: "发票不存在"})
	}
	if inv.InvoiceNumber == nil || strings.TrimSpace(*inv.InvoiceNumber) == "" {
		issues = append(issues, SampleQualityIssue{Level: "error", Code: "invoice_number_missing", Message: "发票号码为空"})
	}
	if inv.Amount == nil || *inv.Amount <= 0 {
		issues = append(issues, SampleQualityIssue{Level: "error", Code: "amount_missing", Message: "发票金额为空或无效"})
	}
	if inv.InvoiceDate == nil || strings.TrimSpace(*inv.InvoiceDate) == "" {
		issues = append(issues, SampleQualityIssue{Level: "error", Code: "invoice_date_missing", Message: "开票日期为空"})
	} else if normalizeInvoiceDatePrefix(*inv.InvoiceDate) == "" {
		issues = append(issues, SampleQualityIssue{Level: "error", Code: "invoice_date_invalid", Message: "开票日期格式不合法（需要 YYYY-MM-DD 或可识别日期前缀）"})
	}

	if inv.SellerName == nil || strings.TrimSpace(*inv.SellerName) == "" {
		issues = append(issues, SampleQualityIssue{Level: "warn", Code: "seller_name_missing", Message: "销售方名称为空（可选，但建议补全）"})
	}

	return issues
}

func shortToken(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = sampleTokenRegex.ReplaceAllString(s, "")
	if s == "" {
		return ""
	}
	if max <= 0 {
		max = 8
	}
	if len(s) > max {
		s = s[:max]
	}
	return s
}

func exportSampleZipPath(base string, r models.RegressionSample) string {
	dir := "misc"
	switch r.Kind {
	case "payment_screenshot":
		dir = "payments"
	case "invoice":
		dir = "invoices"
	}

	date := "00000000"
	if !r.CreatedAt.IsZero() {
		date = r.CreatedAt.UTC().Format("20060102")
	}

	source := shortToken(r.SourceID, 8)
	if source == "" {
		source = shortToken(r.ID, 8)
	}

	h := strings.TrimSpace(r.RawHash)
	if h == "" {
		h = sha256Hex(strings.TrimSpace(r.RawText))
	}
	h = shortToken(h, 12)
	if h == "" {
		h = "nohash"
	}

	filename := fmt.Sprintf("%s_%s_%s", date, source, h)
	path := filepath.ToSlash(filepath.Join(base, dir, filename+".json"))
	return path
}

func backfillRegressionSampleRawHashes(db *gorm.DB) {
	if db == nil {
		return
	}
	// Best-effort backfill for older rows created before raw_hash existed.
	var rows []models.RegressionSample
	if err := db.Where("raw_hash = '' OR raw_hash IS NULL").Limit(500).Find(&rows).Error; err != nil {
		return
	}
	for _, r := range rows {
		raw := strings.TrimSpace(r.RawText)
		if raw == "" {
			continue
		}
		h := sha256Hex(raw)
		_ = db.Model(&models.RegressionSample{}).Where("id = ?", r.ID).Updates(map[string]any{
			"raw_hash": h,
		}).Error
	}
}

func (s *RegressionSampleService) CreateOrUpdateFromPayment(paymentID string, createdBy string, name string, force bool) (*models.RegressionSample, []SampleQualityIssue, error) {
	paymentID = strings.TrimSpace(paymentID)
	createdBy = strings.TrimSpace(createdBy)
	name = normalizeSampleName(name)
	if paymentID == "" || createdBy == "" {
		return nil, nil, fmt.Errorf("missing fields")
	}

	db := database.GetDB()
	backfillRegressionSampleRawHashes(db)
	var p models.Payment
	res := db.Where("id = ?", paymentID).Limit(1).Find(&p)
	if res.Error != nil {
		return nil, nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil, ErrNotFound
	}
	if p.IsDraft {
		return nil, nil, fmt.Errorf("cannot create regression sample from draft payment")
	}
	if p.ExtractedData == nil || strings.TrimSpace(*p.ExtractedData) == "" {
		return nil, nil, fmt.Errorf("payment has no extracted_data")
	}

	var ed PaymentExtractedData
	if err := json.Unmarshal([]byte(*p.ExtractedData), &ed); err != nil {
		return nil, nil, fmt.Errorf("invalid payment extracted_data: %w", err)
	}
	raw := strings.TrimSpace(ed.RawText)
	if raw == "" {
		return nil, nil, fmt.Errorf("payment extracted_data has empty raw_text")
	}

	issues := validatePaymentSampleQuality(&p, raw)
	if !force && hasQualityErrors(issues) {
		return nil, issues, &SampleQualityError{Issues: issues}
	}

	exp := regressionSampleExpectedPayment{
		Merchant:      p.Merchant,
		PaymentMethod: p.PaymentMethod,
	}
	if p.Amount > 0 {
		v := p.Amount
		exp.Amount = &v
	}
	if strings.TrimSpace(p.TransactionTime) != "" {
		v := strings.TrimSpace(p.TransactionTime)
		exp.TransactionTime = &v
	}

	expB, _ := json.Marshal(exp)

	if name == "" {
		short := paymentID
		if len(short) > 8 {
			short = short[:8]
		}
		name = "payment_" + short
	}

	sample := &models.RegressionSample{
		ID:           utils.GenerateUUID(),
		Kind:         "payment_screenshot",
		Name:         name,
		Origin:       "ui",
		SourceType:   "payment",
		SourceID:     paymentID,
		CreatedBy:    createdBy,
		RawText:      raw,
		RawHash:      sha256Hex(raw),
		ExpectedJSON: string(expB),
	}

	// Upsert by (source_type, source_id, kind).
	var existing models.RegressionSample
	existingRes := db.Where("source_type = ? AND source_id = ? AND kind = ?", sample.SourceType, sample.SourceID, sample.Kind).
		Limit(1).
		Find(&existing)
	if existingRes.Error != nil {
		return nil, nil, existingRes.Error
	}
	if existingRes.RowsAffected > 0 {
		update := map[string]any{
			"name":          sample.Name,
			"raw_text":      sample.RawText,
			"raw_hash":      sample.RawHash,
			"expected_json": sample.ExpectedJSON,
			"created_by":    sample.CreatedBy,
			"updated_at":    time.Now(),
		}
		if err := db.Model(&models.RegressionSample{}).Where("id = ?", existing.ID).Updates(update).Error; err != nil {
			return nil, nil, err
		}
		out := existing
		out.Name = sample.Name
		out.RawText = sample.RawText
		out.RawHash = sample.RawHash
		out.ExpectedJSON = sample.ExpectedJSON
		out.CreatedBy = sample.CreatedBy
		return &out, issues, nil
	}

	if err := db.Create(sample).Error; err != nil {
		return nil, nil, err
	}
	return sample, issues, nil
}

func (s *RegressionSampleService) CreateOrUpdateFromInvoice(invoiceID string, createdBy string, name string, force bool) (*models.RegressionSample, []SampleQualityIssue, error) {
	invoiceID = strings.TrimSpace(invoiceID)
	createdBy = strings.TrimSpace(createdBy)
	nameProvided := strings.TrimSpace(name) != ""
	name = normalizeSampleName(name)
	if invoiceID == "" || createdBy == "" {
		return nil, nil, fmt.Errorf("missing fields")
	}

	db := database.GetDB()
	backfillRegressionSampleRawHashes(db)
	var inv models.Invoice
	res := db.Where("id = ?", invoiceID).Limit(1).Find(&inv)
	if res.Error != nil {
		return nil, nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil, ErrNotFound
	}
	if inv.IsDraft {
		return nil, nil, fmt.Errorf("cannot create regression sample from draft invoice")
	}

	raw := ""
	if inv.RawText != nil {
		raw = strings.TrimSpace(*inv.RawText)
	}
	if raw == "" && inv.ExtractedData != nil && strings.TrimSpace(*inv.ExtractedData) != "" {
		var ed InvoiceExtractedData
		if err := json.Unmarshal([]byte(*inv.ExtractedData), &ed); err == nil {
			raw = strings.TrimSpace(ed.RawText)
		}
	}
	if raw == "" {
		return nil, nil, fmt.Errorf("invoice has no raw_text")
	}

	issues := validateInvoiceSampleQuality(&inv, raw)
	if !force && hasQualityErrors(issues) {
		return nil, issues, &SampleQualityError{Issues: issues}
	}

	exp := regressionSampleExpectedInvoice{
		InvoiceNumber: inv.InvoiceNumber,
		InvoiceDate:   inv.InvoiceDate,
		Amount:        inv.Amount,
		TaxAmount:     inv.TaxAmount,
		SellerName:    inv.SellerName,
		BuyerName:     inv.BuyerName,
	}
	expB, _ := json.Marshal(exp)

	if name == "" {
		short := invoiceID
		if len(short) > 8 {
			short = short[:8]
		}
		name = "invoice_" + short
	}

	sample := &models.RegressionSample{
		ID:           utils.GenerateUUID(),
		Kind:         "invoice",
		Name:         name,
		Origin:       "ui",
		SourceType:   "invoice",
		SourceID:     invoiceID,
		CreatedBy:    createdBy,
		RawText:      raw,
		RawHash:      sha256Hex(raw),
		ExpectedJSON: string(expB),
	}

	// Prefer upserting by (kind, raw_hash) so already-synced "云端" samples (origin=repo)
	// can be updated from the UI without needing the original source_id/path.
	{
		var byHash models.RegressionSample
		hashRes := db.Where("kind = ? AND raw_hash = ?", sample.Kind, sample.RawHash).Limit(1).Find(&byHash)
		if hashRes.Error != nil {
			return nil, nil, hashRes.Error
		}
		if hashRes.RowsAffected > 0 {
			updateName := sample.Name
			if !nameProvided && strings.TrimSpace(byHash.Name) != "" {
				updateName = byHash.Name
			}
			update := map[string]any{
				"name":          updateName,
				"raw_text":      sample.RawText,
				"raw_hash":      sample.RawHash,
				"expected_json": sample.ExpectedJSON,
				"created_by":    sample.CreatedBy,
				"updated_at":    time.Now(),
			}
			if err := db.Model(&models.RegressionSample{}).Where("id = ?", byHash.ID).Updates(update).Error; err != nil {
				return nil, nil, err
			}
			out := byHash
			out.Name = updateName
			out.RawText = sample.RawText
			out.RawHash = sample.RawHash
			out.ExpectedJSON = sample.ExpectedJSON
			out.CreatedBy = sample.CreatedBy
			out.UpdatedAt = time.Now()
			return &out, issues, nil
		}
	}

	var existing models.RegressionSample
	existingRes := db.Where("source_type = ? AND source_id = ? AND kind = ?", sample.SourceType, sample.SourceID, sample.Kind).
		Limit(1).
		Find(&existing)
	if existingRes.Error != nil {
		return nil, nil, existingRes.Error
	}
	if existingRes.RowsAffected > 0 {
		update := map[string]any{
			"name":          sample.Name,
			"raw_text":      sample.RawText,
			"raw_hash":      sample.RawHash,
			"expected_json": sample.ExpectedJSON,
			"created_by":    sample.CreatedBy,
			"updated_at":    time.Now(),
		}
		if err := db.Model(&models.RegressionSample{}).Where("id = ?", existing.ID).Updates(update).Error; err != nil {
			return nil, nil, err
		}
		out := existing
		out.Name = sample.Name
		out.RawText = sample.RawText
		out.RawHash = sample.RawHash
		out.ExpectedJSON = sample.ExpectedJSON
		out.CreatedBy = sample.CreatedBy
		return &out, issues, nil
	}

	if err := db.Create(sample).Error; err != nil {
		return nil, nil, err
	}
	return sample, issues, nil
}

type ListRegressionSamplesParams struct {
	Kind   string
	Origin string // ui | repo
	Search string
	Limit  int
	Offset int
}

func (s *RegressionSampleService) List(params ListRegressionSamplesParams) ([]models.RegressionSample, int64, error) {
	return s.ListCtx(context.Background(), params)
}

func (s *RegressionSampleService) ListCtx(ctx context.Context, params ListRegressionSamplesParams) ([]models.RegressionSample, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	db := database.GetDB().WithContext(ctx)
	q := db.Model(&models.RegressionSample{})

	kind := strings.TrimSpace(params.Kind)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	origin := strings.TrimSpace(params.Origin)
	if origin != "" {
		q = q.Where("origin = ?", origin)
	}
	search := strings.TrimSpace(params.Search)
	if search != "" {
		like := "%" + search + "%"
		q = q.Where("name LIKE ? OR source_id LIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	var rows []models.RegressionSample
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *RegressionSampleService) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrSampleNotFound
	}
	db := database.GetDB()
	res := db.Where("id = ?", id).Delete(&models.RegressionSample{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrSampleNotFound
	}
	return nil
}

func (s *RegressionSampleService) BulkDelete(ids []string) (deleted int, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	clean := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return 0, nil
	}
	db := database.GetDB()
	res := db.Where("id IN ?", clean).Delete(&models.RegressionSample{})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

type ExportRegressionSamplesParams struct {
	Kind   string
	Origin string   // ui | repo
	IDs    []string // optional
	Redact bool     // redact raw_text for export
}

func (s *RegressionSampleService) PrepareExportZip(ctx context.Context, params ExportRegressionSamplesParams) (*ZipStream, error) {
	db := database.GetDB()
	backfillRegressionSampleRawHashes(db)
	q := db.Model(&models.RegressionSample{})
	kind := strings.TrimSpace(params.Kind)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	origin := strings.TrimSpace(params.Origin)
	if origin != "" {
		q = q.Where("origin = ?", origin)
	}
	if len(params.IDs) > 0 {
		clean := make([]string, 0, len(params.IDs))
		seen := map[string]struct{}{}
		for _, id := range params.IDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			clean = append(clean, id)
		}
		if len(clean) == 0 {
			return nil, fmt.Errorf("no samples to export")
		}
		q = q.Where("id IN ?", clean)
	}

	var rows []models.RegressionSample
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no samples to export")
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].ID < rows[j].ID
	})

	now := time.Now().UTC().Format("20060102_150405")
	zipName := "regression_samples_" + now + ".zip"
	base := filepath.Join("backend-go", "internal", "services", "testdata", "regression")
	return &ZipStream{
		Filename: zipName,
		Write: func(w io.Writer) error {
			if ctx == nil {
				ctx = context.Background()
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			zw := zip.NewWriter(w)
			seenPaths := map[string]int{}

			for _, r := range rows {
				if err := ctx.Err(); err != nil {
					return err
				}
				path := exportSampleZipPath(base, r)
				if n := seenPaths[path]; n > 0 {
					ext := filepath.Ext(path)
					noExt := strings.TrimSuffix(path, ext)
					path = fmt.Sprintf("%s_%d%s", noExt, n+1, ext)
				}
				seenPaths[path]++

				rawText := r.RawText
				redacted := false
				redactionRules := []string(nil)
				if params.Redact {
					redactedText, rules := redactSampleRawText(rawText)
					rawText = redactedText
					redactionRules = rules
					redacted = len(rules) > 0
				}

				var expected any
				_ = json.Unmarshal([]byte(r.ExpectedJSON), &expected)
				payload := map[string]any{
					"schema":   1,
					"raw_hash": strings.TrimSpace(r.RawHash),
					"kind":     r.Kind,
					"name":     r.Name,
					"raw_text": rawText,
					"expected": expected,
				}
				if redacted {
					payload["redacted"] = true
					payload["redaction_rules"] = redactionRules
				}

				b, err := json.MarshalIndent(payload, "", "  ")
				if err != nil {
					continue
				}
				b = append(b, '\n')

				f, err := zw.Create(path)
				if err != nil {
					continue
				}
				_, _ = f.Write(b)
			}

			return zw.Close()
		},
	}, nil
}

type RepoImportResult struct {
	Files     int      `json:"files"`
	Inserted  int      `json:"inserted"`
	Updated   int      `json:"updated"`
	Promoted  int      `json:"promoted"` // ui -> repo
	Errors    int      `json:"errors"`
	ErrorList []string `json:"error_list,omitempty"`
}

type repoSampleFile struct {
	Schema   int             `json:"schema"`
	Kind     string          `json:"kind"`
	Name     string          `json:"name"`
	RawText  string          `json:"raw_text"`
	RawHash  string          `json:"raw_hash"`
	Expected json.RawMessage `json:"expected"`
}

func candidateRepoSampleDirs() []string {
	if v := strings.TrimSpace(os.Getenv("SBM_REGRESSION_SAMPLES_DIR")); v != "" {
		return []string{v}
	}
	return []string{
		filepath.Join("internal", "services", "testdata", "regression"),
		filepath.Join("backend-go", "internal", "services", "testdata", "regression"),
		filepath.Join("testdata", "regression"),
	}
}

func resolveRepoSampleDir() (string, error) {
	for _, p := range candidateRepoSampleDirs() {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w (set SBM_REGRESSION_SAMPLES_DIR)", ErrRepoSampleDirNotFound)
}

// ImportRepoSamples imports repo (built-in) regression samples into DB.
// Behavior (mode B): if a matching (kind, raw_hash) exists, it is promoted/overwritten as origin=repo.
func (s *RegressionSampleService) ImportRepoSamples() (*RepoImportResult, error) {
	baseDir, err := resolveRepoSampleDir()
	if err != nil {
		return nil, err
	}

	db := database.GetDB()
	backfillRegressionSampleRawHashes(db)
	result := &RepoImportResult{}

	walkErr := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			result.Errors++
			if len(result.ErrorList) < 20 {
				result.ErrorList = append(result.ErrorList, err.Error())
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		result.Files++

		b, rerr := os.ReadFile(path)
		if rerr != nil {
			result.Errors++
			if len(result.ErrorList) < 20 {
				result.ErrorList = append(result.ErrorList, fmt.Sprintf("%s: %v", path, rerr))
			}
			return nil
		}
		var f repoSampleFile
		if uerr := json.Unmarshal(b, &f); uerr != nil {
			result.Errors++
			if len(result.ErrorList) < 20 {
				result.ErrorList = append(result.ErrorList, fmt.Sprintf("%s: %v", path, uerr))
			}
			return nil
		}

		kind := strings.TrimSpace(f.Kind)
		raw := strings.TrimSpace(f.RawText)
		if kind == "" || raw == "" {
			result.Errors++
			if len(result.ErrorList) < 20 {
				result.ErrorList = append(result.ErrorList, fmt.Sprintf("%s: missing kind/raw_text", path))
			}
			return nil
		}
		if kind != "payment_screenshot" && kind != "invoice" {
			// Ignore unknown kinds for now.
			return nil
		}

		name := normalizeSampleName(f.Name)
		if name == "" {
			name = normalizeSampleName(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())))
		}
		if name == "" {
			name = kind + "_" + sha256Hex(raw)[:8]
		}

		var expected any
		if len(f.Expected) > 0 {
			_ = json.Unmarshal(f.Expected, &expected)
		}
		expB, _ := json.Marshal(expected)

		rawHash := strings.TrimSpace(f.RawHash)
		if rawHash == "" {
			rawHash = sha256Hex(raw)
		}

		var existing models.RegressionSample
		exRes := db.Where("kind = ? AND raw_hash = ?", kind, rawHash).Limit(1).Find(&existing)
		if exRes.Error != nil {
			result.Errors++
			if len(result.ErrorList) < 20 {
				result.ErrorList = append(result.ErrorList, fmt.Sprintf("%s: %v", path, exRes.Error))
			}
			return nil
		}

		if exRes.RowsAffected == 0 {
			sample := &models.RegressionSample{
				ID:           utils.GenerateUUID(),
				Kind:         kind,
				Name:         name,
				Origin:       "repo",
				SourceType:   "repo",
				SourceID:     filepath.ToSlash(path),
				CreatedBy:    "repo_sync",
				RawText:      raw,
				RawHash:      rawHash,
				ExpectedJSON: string(expB),
			}
			if err := db.Create(sample).Error; err != nil {
				result.Errors++
				if len(result.ErrorList) < 20 {
					result.ErrorList = append(result.ErrorList, fmt.Sprintf("%s: %v", path, err))
				}
				return nil
			}
			result.Inserted++
			return nil
		}

		update := map[string]any{
			"name":          name,
			"raw_text":      raw,
			"expected_json": string(expB),
			"raw_hash":      rawHash,
			"source_type":   "repo",
			"source_id":     filepath.ToSlash(path),
			"origin":        "repo",
		}
		if strings.TrimSpace(existing.Origin) != "repo" {
			result.Promoted++
		}
		shouldUpdate := strings.TrimSpace(existing.Origin) != "repo" ||
			strings.TrimSpace(existing.Name) != name ||
			strings.TrimSpace(existing.SourceType) != "repo" ||
			strings.TrimSpace(existing.SourceID) != filepath.ToSlash(path) ||
			strings.TrimSpace(existing.RawText) != raw ||
			strings.TrimSpace(existing.ExpectedJSON) != string(expB) ||
			strings.TrimSpace(existing.RawHash) != rawHash
		if !shouldUpdate {
			return nil
		}
		update["updated_at"] = time.Now()
		if err := db.Model(&models.RegressionSample{}).Where("id = ?", existing.ID).Updates(update).Error; err != nil {
			result.Errors++
			if len(result.ErrorList) < 20 {
				result.ErrorList = append(result.ErrorList, fmt.Sprintf("%s: %v", path, err))
			}
			return nil
		}
		result.Updated++
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return result, nil
}
