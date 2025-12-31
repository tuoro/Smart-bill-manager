package services

import (
	"errors"
	"fmt"
	"log"
	"math"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrMissingTransactionTime = errors.New("missing transaction time")

type PaymentService struct {
	repo        *repository.PaymentRepository
	invoiceRepo *repository.InvoiceRepository
	ocrService  *OCRService
}

func NewPaymentService() *PaymentService {
	return &PaymentService{
		repo:        repository.NewPaymentRepository(),
		invoiceRepo: repository.NewInvoiceRepository(),
		ocrService:  NewOCRService(),
	}
}

type CreatePaymentInput struct {
	Amount          float64 `json:"amount" binding:"required"`
	Merchant        *string `json:"merchant"`
	Category        *string `json:"category"`
	PaymentMethod   *string `json:"payment_method"`
	Description     *string `json:"description"`
	TransactionTime string  `json:"transaction_time" binding:"required"`
	ScreenshotPath  *string `json:"screenshot_path"`
	ExtractedData   *string `json:"extracted_data"`
}

func (s *PaymentService) Create(input CreatePaymentInput) (*models.Payment, error) {
	t, err := parseRFC3339ToUTC(input.TransactionTime)
	if err != nil {
		return nil, fmt.Errorf("transaction_time must be RFC3339: %w", err)
	}

	var screenshotPath *string
	if input.ScreenshotPath != nil {
		p := strings.TrimSpace(*input.ScreenshotPath)
		if p != "" {
			screenshotPath = &p
		}
	}
	var extractedData *string
	if input.ExtractedData != nil {
		d := strings.TrimSpace(*input.ExtractedData)
		if d != "" {
			extractedData = &d
		}
	}

	payment := &models.Payment{
		ID:                utils.GenerateUUID(),
		Amount:            input.Amount,
		Merchant:          input.Merchant,
		Category:          input.Category,
		PaymentMethod:     input.PaymentMethod,
		Description:       input.Description,
		TransactionTime:   t.Format(time.RFC3339),
		TransactionTimeTs: unixMilli(t),
		ScreenshotPath:    screenshotPath,
		ExtractedData:     extractedData,
		TripAssignSrc:     assignSrcAuto,
		TripAssignState:   assignStateNoMatch,
	}

	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(payment).Error; err != nil {
			return err
		}
		return autoAssignPaymentTx(tx, payment)
	}); err != nil {
		return nil, err
	}

	return payment, nil
}

type PaymentFilterInput struct {
	Limit     int    `form:"limit"`
	Offset    int    `form:"offset"`
	StartDate string `form:"startDate"`
	EndDate   string `form:"endDate"`
	Category  string `form:"category"`
	// IncludeDraft controls whether draft records are included in listing/stats.
	// Default is false.
	IncludeDraft bool `form:"includeDraft"`
}

func (s *PaymentService) GetAll(filter PaymentFilterInput) ([]models.Payment, error) {
	startTs := int64(0)
	endTs := int64(0)
	if strings.TrimSpace(filter.StartDate) != "" {
		if t, err := parseRFC3339ToUTC(filter.StartDate); err == nil {
			startTs = unixMilli(t)
		} else {
			return nil, fmt.Errorf("invalid startDate: %w", err)
		}
	}
	if strings.TrimSpace(filter.EndDate) != "" {
		if t, err := parseRFC3339ToUTC(filter.EndDate); err == nil {
			endTs = unixMilli(t)
		} else {
			return nil, fmt.Errorf("invalid endDate: %w", err)
		}
	}

	return s.repo.FindAll(repository.PaymentFilter{
		Limit:     filter.Limit,
		Offset:    filter.Offset,
		StartDate: filter.StartDate,
		EndDate:   filter.EndDate,
		StartTs:   startTs,
		EndTs:     endTs,
		Category:  filter.Category,
		IncludeDraft: filter.IncludeDraft,
	})
}

type PaymentListItem struct {
	models.Payment
	InvoiceCount int `json:"invoiceCount"`
}

func (s *PaymentService) GetAllWithInvoiceCounts(filter PaymentFilterInput) ([]PaymentListItem, error) {
	payments, err := s.GetAll(filter)
	if err != nil {
		return nil, err
	}
	if len(payments) == 0 {
		return []PaymentListItem{}, nil
	}

	ids := make([]string, 0, len(payments))
	for _, p := range payments {
		ids = append(ids, p.ID)
	}

	type row struct {
		PaymentID string `gorm:"column:payment_id"`
		Cnt       int    `gorm:"column:cnt"`
	}
	var rows []row
	_ = database.GetDB().
		Table("invoice_payment_links").
		Select("payment_id, COUNT(*) AS cnt").
		Where("payment_id IN ?", ids).
		Group("payment_id").
		Scan(&rows).Error

	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[strings.TrimSpace(r.PaymentID)] = r.Cnt
	}

	out := make([]PaymentListItem, 0, len(payments))
	for _, p := range payments {
		out = append(out, PaymentListItem{
			Payment:      p,
			InvoiceCount: counts[p.ID],
		})
	}
	return out, nil
}

func (s *PaymentService) GetByID(id string) (*models.Payment, error) {
	return s.repo.FindByID(id)
}

type UpdatePaymentInput struct {
	Amount          *float64 `json:"amount"`
	Merchant        *string  `json:"merchant"`
	Category        *string  `json:"category"`
	PaymentMethod   *string  `json:"payment_method"`
	Description     *string  `json:"description"`
	TransactionTime *string  `json:"transaction_time"`
	TripID          *string  `json:"trip_id"`
	TripAssignSrc   *string  `json:"trip_assignment_source"`
	BadDebt         *bool    `json:"bad_debt"`
	Confirm         *bool    `json:"confirm"`
}

func (s *PaymentService) Update(id string, input UpdatePaymentInput) error {
	var before *models.Payment
	needsRecalc := input.TripID != nil || input.TripAssignSrc != nil || input.BadDebt != nil
	if needsRecalc {
		p, err := s.repo.FindByID(id)
		if err != nil {
			return err
		}
		before = p
	}

	data := make(map[string]interface{})

	if input.Amount != nil {
		data["amount"] = *input.Amount
	}
	if input.Merchant != nil {
		data["merchant"] = *input.Merchant
	}
	if input.Category != nil {
		data["category"] = *input.Category
	}
	if input.PaymentMethod != nil {
		data["payment_method"] = *input.PaymentMethod
	}
	if input.Description != nil {
		data["description"] = *input.Description
	}
	if input.TransactionTime != nil {
		t, err := parseRFC3339ToUTC(*input.TransactionTime)
		if err != nil {
			return fmt.Errorf("transaction_time must be RFC3339: %w", err)
		}
		data["transaction_time"] = t.Format(time.RFC3339)
		data["transaction_time_ts"] = unixMilli(t)
	}

	normalizedTripID := ""
	if input.TripID != nil {
		trimmed := strings.TrimSpace(*input.TripID)
		if trimmed == "" {
			data["trip_id"] = nil
		} else {
			data["trip_id"] = trimmed
			normalizedTripID = trimmed
		}
	} else if before != nil && before.TripID != nil {
		normalizedTripID = strings.TrimSpace(*before.TripID)
	}

	if input.TripID != nil || input.TripAssignSrc != nil {
		src := ""
		if input.TripAssignSrc != nil {
			src = strings.TrimSpace(*input.TripAssignSrc)
		}
		// Backward-compatible default: if caller changes trip_id without specifying source,
		// treat non-empty trip_id as manual and empty as blocked (explicit user intent).
		if src == "" {
			if input.TripID != nil && strings.TrimSpace(*input.TripID) == "" {
				src = assignSrcBlocked
			} else if input.TripID != nil {
				src = assignSrcManual
			}
		}
		if src != "" {
			if src != assignSrcAuto && src != assignSrcManual && src != assignSrcBlocked {
				return fmt.Errorf("invalid trip_assignment_source")
			}
			data["trip_assignment_source"] = src
			if src == assignSrcBlocked {
				data["trip_assignment_state"] = assignStateBlocked
				data["trip_id"] = nil
				normalizedTripID = ""
			} else if src == assignSrcManual && normalizedTripID != "" {
				data["trip_assignment_state"] = assignStateAssigned
			} else if src == assignSrcAuto {
				// state will be recomputed by automatic logic elsewhere
			}
		}
	}
	if input.BadDebt != nil {
		data["bad_debt"] = *input.BadDebt
	}
	if input.Confirm != nil && *input.Confirm {
		data["is_draft"] = false
	}

	if len(data) == 0 {
		return nil
	}

	if err := s.repo.Update(id, data); err != nil {
		return err
	}

	if !needsRecalc {
		return nil
	}

	affected := make([]string, 0, 2)
	if before != nil && before.TripID != nil && strings.TrimSpace(*before.TripID) != "" {
		affected = append(affected, strings.TrimSpace(*before.TripID))
	}

	afterTripID := ""
	if v, ok := data["trip_id"]; ok {
		if v == nil {
			afterTripID = ""
		} else if s, ok := v.(string); ok {
			afterTripID = strings.TrimSpace(s)
		}
	} else if before != nil && before.TripID != nil {
		afterTripID = strings.TrimSpace(*before.TripID)
	}
	if afterTripID != "" {
		affected = append(affected, afterTripID)
	}

	return recalcTripBadDebtLockedForTripIDs(affected)
}

func (s *PaymentService) Delete(id string) error {
	payment, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	var tripID string
	if payment.TripID != nil {
		tripID = strings.TrimSpace(*payment.TripID)
	}

	if err := s.repo.Delete(id); err != nil {
		return err
	}

	if tripID == "" {
		return nil
	}
	return recalcTripBadDebtLocked(tripID)
}

func (s *PaymentService) GetStats(startDate, endDate string) (*models.PaymentStats, error) {
	startTs := int64(0)
	endTs := int64(0)
	if strings.TrimSpace(startDate) != "" {
		if t, err := parseRFC3339ToUTC(startDate); err == nil {
			startTs = unixMilli(t)
		} else {
			return nil, fmt.Errorf("invalid startDate: %w", err)
		}
	}
	if strings.TrimSpace(endDate) != "" {
		if t, err := parseRFC3339ToUTC(endDate); err == nil {
			endTs = unixMilli(t)
		} else {
			return nil, fmt.Errorf("invalid endDate: %w", err)
		}
	}
	return s.repo.GetStatsByTs(startTs, endTs)
}

// CreateFromScreenshot creates a payment from a screenshot with OCR
type CreateFromScreenshotInput struct {
	ScreenshotPath string
}

// CreateFromScreenshot creates a payment from a screenshot with OCR.
// If OCR cannot extract a valid transaction time, this returns an error (policy A).
func (s *PaymentService) CreateFromScreenshot(input CreateFromScreenshotInput) (*models.Payment, *PaymentExtractedData, error) {

	// Perform OCR on the screenshot with specialized payment screenshot recognition
	text, err := s.ocrService.RecognizePaymentScreenshot(input.ScreenshotPath)
	if err != nil {
		return nil, nil, err
	}

	// Parse payment data from OCR text
	extracted, parseErr := s.ocrService.ParsePaymentScreenshot(text)
	if parseErr != nil {
		return nil, nil, parseErr
	}

	if extracted.TransactionTime == nil || strings.TrimSpace(*extracted.TransactionTime) == "" {
		return nil, extracted, fmt.Errorf("%w", ErrMissingTransactionTime)
	}

	shanghai := loadLocationOrUTC("Asia/Shanghai")
	payTime, err := parsePaymentTimeToUTC(*extracted.TransactionTime, shanghai)
	if err != nil {
		return nil, extracted, fmt.Errorf("%w: %v", ErrMissingTransactionTime, err)
	}
	utcTimeStr := payTime.Format(time.RFC3339)
	extracted.TransactionTime = &utcTimeStr

	// Store extracted data as JSON
	extractedDataJSON, err := ExtractedDataToJSON(extracted)
	if err != nil {
		// Log the error but continue - extracted data is optional
		extractedDataJSON = nil
	}

	// Create payment record with extracted data
	payment := &models.Payment{
		ID:                utils.GenerateUUID(),
		IsDraft:           true,
		Amount:            0.0, // Default to 0.0, will be updated if amount is extracted
		Merchant:          extracted.Merchant,
		PaymentMethod:     extracted.PaymentMethod,
		TransactionTime:   utcTimeStr,
		TransactionTimeTs: unixMilli(payTime),
		ScreenshotPath:    &input.ScreenshotPath,
		ExtractedData:     extractedDataJSON,
		TripAssignSrc:     assignSrcAuto,
		TripAssignState:   assignStateNoMatch,
	}

	// Set amount if extracted
	if extracted.Amount != nil {
		absAmount := math.Abs(*extracted.Amount)
		if absAmount > 0 {
			payment.Amount = absAmount
			// Normalize for UI/matching: store expenses as positive amounts.
			extracted.Amount = &absAmount
		}
	}

	// Set transaction time if extracted
	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(payment).Error; err != nil {
			return err
		}
		return autoAssignPaymentTx(tx, payment)
	}); err != nil {
		return nil, nil, err
	}

	return payment, extracted, nil
}

// GetLinkedInvoices returns all invoices linked to a payment
func (s *PaymentService) GetLinkedInvoices(paymentID string) ([]models.Invoice, error) {
	return s.repo.GetLinkedInvoices(paymentID)
}

// SuggestInvoices suggests invoices that might match this payment using amount/seller/date signals.
func (s *PaymentService) SuggestInvoices(paymentID string, limit int, debug bool) ([]models.Invoice, error) {
	payment, err := s.repo.FindByID(paymentID)
	if err != nil {
		return nil, err
	}

	if debug {
		log.Printf(
			"[MATCH] payment=%s amount=%.2f merchant=%q time=%q",
			paymentID,
			payment.Amount,
			strPtrVal(payment.Merchant),
			payment.TransactionTime,
		)
	}

	linked, _ := s.repo.GetLinkedInvoices(paymentID)
	linkedIDs := make(map[string]struct{}, len(linked))
	for _, inv := range linked {
		linkedIDs[inv.ID] = struct{}{}
	}

	if limit <= 0 {
		limit = 10
	}
	maxCandidates := limit * 50
	if maxCandidates < 200 {
		maxCandidates = 200
	}

	candidates, err := s.invoiceRepo.SuggestInvoices(payment, maxCandidates)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		var total int64
		_ = database.GetDB().Model(&models.Invoice{}).Where("is_draft = 0").Count(&total).Error
		if debug {
			log.Printf("[MATCH] payment=%s repo candidates=0, fallback to recent invoices (total=%d)", paymentID, total)
		}
		if total > 0 {
			var recent []models.Invoice
			_ = database.GetDB().
				Model(&models.Invoice{}).
				Where("is_draft = 0").
				Order("created_at DESC").
				Limit(maxCandidates).
				Find(&recent).Error
			candidates = recent

			if debug {
				sampleN := 5
				if len(recent) < sampleN {
					sampleN = len(recent)
				}
				for i := 0; i < sampleN; i++ {
					inv := recent[i]
					log.Printf("[MATCH] payment=%s recent invoice sample=%d id=%s amount=%v seller=%v invoice_date=%v",
						paymentID, i+1, inv.ID, valueOrNil(inv.Amount), strValueOrNil(inv.SellerName), strValueOrNil(inv.InvoiceDate))
				}
			}
		}
	}

	if debug {
		log.Printf("[MATCH] payment=%s linked=%d candidates=%d", paymentID, len(linkedIDs), len(candidates))
	}

	type scored struct {
		invoice models.Invoice
		score   float64
		aScore  float64
		dScore  float64
		mScore  float64
	}
	scoredAll := make([]scored, 0, len(candidates))
	for _, inv := range candidates {
		if _, ok := linkedIDs[inv.ID]; ok {
			continue
		}
		score, aScore, dScore, mScore := computeInvoicePaymentScoreBreakdown(&inv, payment)
		scoredAll = append(scoredAll, scored{invoice: inv, score: score, aScore: aScore, dScore: dScore, mScore: mScore})
	}

	sort.Slice(scoredAll, func(i, j int) bool {
		if scoredAll[i].score == scoredAll[j].score {
			return scoredAll[i].invoice.CreatedAt.After(scoredAll[j].invoice.CreatedAt)
		}
		return scoredAll[i].score > scoredAll[j].score
	})

	minScore := 0.15
	if payment.Amount == 0 {
		minScore = 0.05
	}

	out := make([]models.Invoice, 0, limit)
	for _, s := range scoredAll {
		if s.score < minScore {
			continue
		}
		out = append(out, s.invoice)
		if len(out) >= limit {
			break
		}
	}

	if len(out) == 0 {
		for _, s := range scoredAll {
			out = append(out, s.invoice)
			if len(out) >= limit {
				break
			}
		}
	}

	if debug {
		top := 10
		if len(scoredAll) < top {
			top = len(scoredAll)
		}
		for i := 0; i < top; i++ {
			inv := scoredAll[i].invoice
			log.Printf(
				"[MATCH] payment=%s rank=%d invoice=%s score=%.3f amount=%v seller=%v invoice_date=%v parts(a=%.3f d=%.3f m=%.3f)",
				paymentID,
				i+1,
				inv.ID,
				scoredAll[i].score,
				valueOrNil(inv.Amount),
				strValueOrNil(inv.SellerName),
				strValueOrNil(inv.InvoiceDate),
				scoredAll[i].aScore,
				scoredAll[i].dScore,
				scoredAll[i].mScore,
			)
		}
	}

	return out, nil
}

// ReparseScreenshot re-parses the screenshot for a payment record
func (s *PaymentService) ReparseScreenshot(paymentID string) (*PaymentExtractedData, error) {
	// Get the payment record
	payment, err := s.repo.FindByID(paymentID)
	if err != nil {
		return nil, err
	}

	// Check if payment has a screenshot
	if payment.ScreenshotPath == nil || *payment.ScreenshotPath == "" {
		return nil, fmt.Errorf("payment has no screenshot")
	}

	// Perform OCR on the screenshot with specialized payment screenshot recognition
	text, err := s.ocrService.RecognizePaymentScreenshot(*payment.ScreenshotPath)
	if err != nil {
		return nil, fmt.Errorf("OCR recognition failed: %w", err)
	}

	// Parse payment data from OCR text
	extracted, err := s.ocrService.ParsePaymentScreenshot(text)
	if err != nil {
		return nil, fmt.Errorf("OCR parsing failed: %w", err)
	}

	// Store extracted data as JSON
	extractedDataJSON, err := ExtractedDataToJSON(extracted)
	if err != nil {
		extractedDataJSON = nil
	}

	// Update payment record with new extracted data
	updateData := make(map[string]interface{})
	updateData["extracted_data"] = extractedDataJSON

	// Update amount if extracted
	if extracted.Amount != nil {
		absAmount := math.Abs(*extracted.Amount)
		if absAmount > 0 {
			updateData["amount"] = absAmount
			extracted.Amount = &absAmount
		}
	}

	// Update merchant if extracted
	if extracted.Merchant != nil {
		updateData["merchant"] = *extracted.Merchant
	}

	// Update payment method if extracted
	if extracted.PaymentMethod != nil {
		updateData["payment_method"] = *extracted.PaymentMethod
	}

	// Update transaction time if extracted
	if extracted.TransactionTime != nil && strings.TrimSpace(*extracted.TransactionTime) != "" {
		shanghai := loadLocationOrUTC("Asia/Shanghai")
		if payTime, err := parsePaymentTimeToUTC(*extracted.TransactionTime, shanghai); err == nil {
			utcTimeStr := payTime.Format(time.RFC3339)
			extracted.TransactionTime = &utcTimeStr
			updateData["transaction_time"] = utcTimeStr
			updateData["transaction_time_ts"] = unixMilli(payTime)
		}
	}

	// Update the payment record
	if err := s.repo.Update(paymentID, updateData); err != nil {
		return nil, fmt.Errorf("failed to update payment: %w", err)
	}

	return extracted, nil
}
