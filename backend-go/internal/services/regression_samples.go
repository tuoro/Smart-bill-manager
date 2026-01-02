package services

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

func (s *RegressionSampleService) CreateOrUpdateFromPayment(paymentID string, createdBy string, name string) (*models.RegressionSample, error) {
	paymentID = strings.TrimSpace(paymentID)
	createdBy = strings.TrimSpace(createdBy)
	name = normalizeSampleName(name)
	if paymentID == "" || createdBy == "" {
		return nil, fmt.Errorf("missing fields")
	}

	db := database.GetDB()
	backfillRegressionSampleRawHashes(db)
	var p models.Payment
	res := db.Where("id = ?", paymentID).Limit(1).Find(&p)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	if p.IsDraft {
		return nil, fmt.Errorf("cannot create regression sample from draft payment")
	}
	if p.ExtractedData == nil || strings.TrimSpace(*p.ExtractedData) == "" {
		return nil, fmt.Errorf("payment has no extracted_data")
	}

	var ed PaymentExtractedData
	if err := json.Unmarshal([]byte(*p.ExtractedData), &ed); err != nil {
		return nil, fmt.Errorf("invalid payment extracted_data: %w", err)
	}
	raw := strings.TrimSpace(ed.RawText)
	if raw == "" {
		return nil, fmt.Errorf("payment extracted_data has empty raw_text")
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
		return nil, existingRes.Error
	}
	if existingRes.RowsAffected > 0 {
		update := map[string]any{
			"name":          sample.Name,
			"raw_text":      sample.RawText,
			"expected_json": sample.ExpectedJSON,
			"created_by":    sample.CreatedBy,
			"updated_at":    time.Now(),
		}
		if err := db.Model(&models.RegressionSample{}).Where("id = ?", existing.ID).Updates(update).Error; err != nil {
			return nil, err
		}
		out := existing
		out.Name = sample.Name
		out.RawText = sample.RawText
		out.ExpectedJSON = sample.ExpectedJSON
		out.CreatedBy = sample.CreatedBy
		return &out, nil
	}

	if err := db.Create(sample).Error; err != nil {
		return nil, err
	}
	return sample, nil
}

func (s *RegressionSampleService) CreateOrUpdateFromInvoice(invoiceID string, createdBy string, name string) (*models.RegressionSample, error) {
	invoiceID = strings.TrimSpace(invoiceID)
	createdBy = strings.TrimSpace(createdBy)
	name = normalizeSampleName(name)
	if invoiceID == "" || createdBy == "" {
		return nil, fmt.Errorf("missing fields")
	}

	db := database.GetDB()
	backfillRegressionSampleRawHashes(db)
	var inv models.Invoice
	res := db.Where("id = ?", invoiceID).Limit(1).Find(&inv)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	if inv.IsDraft {
		return nil, fmt.Errorf("cannot create regression sample from draft invoice")
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
		return nil, fmt.Errorf("invoice has no raw_text")
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

	var existing models.RegressionSample
	existingRes := db.Where("source_type = ? AND source_id = ? AND kind = ?", sample.SourceType, sample.SourceID, sample.Kind).
		Limit(1).
		Find(&existing)
	if existingRes.Error != nil {
		return nil, existingRes.Error
	}
	if existingRes.RowsAffected > 0 {
		update := map[string]any{
			"name":          sample.Name,
			"raw_text":      sample.RawText,
			"expected_json": sample.ExpectedJSON,
			"created_by":    sample.CreatedBy,
			"updated_at":    time.Now(),
		}
		if err := db.Model(&models.RegressionSample{}).Where("id = ?", existing.ID).Updates(update).Error; err != nil {
			return nil, err
		}
		out := existing
		out.Name = sample.Name
		out.RawText = sample.RawText
		out.ExpectedJSON = sample.ExpectedJSON
		out.CreatedBy = sample.CreatedBy
		return &out, nil
	}

	if err := db.Create(sample).Error; err != nil {
		return nil, err
	}
	return sample, nil
}

type ListRegressionSamplesParams struct {
	Kind   string
	Origin string // ui | repo
	Search string
	Limit  int
	Offset int
}

func (s *RegressionSampleService) List(params ListRegressionSamplesParams) ([]models.RegressionSample, int64, error) {
	db := database.GetDB()
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
}

func (s *RegressionSampleService) ExportZip(params ExportRegressionSamplesParams) ([]byte, string, error) {
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
			return nil, "", fmt.Errorf("no samples to export")
		}
		q = q.Where("id IN ?", clean)
	}

	var rows []models.RegressionSample
	if err := q.Find(&rows).Error; err != nil {
		return nil, "", err
	}
	if len(rows) == 0 {
		return nil, "", fmt.Errorf("no samples to export")
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

	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	now := time.Now().UTC().Format("20060102_150405")
	zipName := "regression_samples_" + now + ".zip"
	base := filepath.Join("backend-go", "internal", "services", "testdata", "regression")

	for _, r := range rows {
		dir := "misc"
		switch r.Kind {
		case "payment_screenshot":
			dir = "payments"
		case "invoice":
			dir = "invoices"
		}

		filename := normalizeSampleName(r.Name)
		if filename == "" {
			filename = r.SourceType + "_" + r.SourceID
		}
		path := filepath.ToSlash(filepath.Join(base, dir, filename+".json"))

		var expected any
		_ = json.Unmarshal([]byte(r.ExpectedJSON), &expected)
		payload := map[string]any{
			"kind":     r.Kind,
			"name":     r.Name,
			"raw_text": r.RawText,
			"expected": expected,
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

	_ = zw.Close()
	return buf.Bytes(), zipName, nil
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
	Kind     string          `json:"kind"`
	Name     string          `json:"name"`
	RawText  string          `json:"raw_text"`
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

		rawHash := sha256Hex(raw)

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
