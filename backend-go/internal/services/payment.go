package services

import (
	"fmt"
	"log"
	"math"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"
	"sort"
	"strings"
)

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
}

func (s *PaymentService) Create(input CreatePaymentInput) (*models.Payment, error) {
	payment := &models.Payment{
		ID:              utils.GenerateUUID(),
		Amount:          input.Amount,
		Merchant:        input.Merchant,
		Category:        input.Category,
		PaymentMethod:   input.PaymentMethod,
		Description:     input.Description,
		TransactionTime: input.TransactionTime,
	}

	if err := s.repo.Create(payment); err != nil {
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
}

func (s *PaymentService) GetAll(filter PaymentFilterInput) ([]models.Payment, error) {
	return s.repo.FindAll(repository.PaymentFilter{
		Limit:     filter.Limit,
		Offset:    filter.Offset,
		StartDate: filter.StartDate,
		EndDate:   filter.EndDate,
		Category:  filter.Category,
	})
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
}

func (s *PaymentService) Update(id string, input UpdatePaymentInput) error {
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
		data["transaction_time"] = *input.TransactionTime
	}
	if input.TripID != nil {
		trimmed := strings.TrimSpace(*input.TripID)
		if trimmed == "" {
			data["trip_id"] = nil
		} else {
			data["trip_id"] = trimmed
		}
	}

	if len(data) == 0 {
		return nil
	}

	return s.repo.Update(id, data)
}

func (s *PaymentService) Delete(id string) error {
	return s.repo.Delete(id)
}

func (s *PaymentService) GetStats(startDate, endDate string) (*models.PaymentStats, error) {
	return s.repo.GetStats(startDate, endDate)
}

// CreateFromScreenshot creates a payment from a screenshot with OCR
type CreateFromScreenshotInput struct {
	ScreenshotPath string
}

// CreateFromScreenshotBestEffort creates a payment from a screenshot and tries OCR,
// but will still create the payment record even if OCR fails.
func (s *PaymentService) CreateFromScreenshotBestEffort(input CreateFromScreenshotInput) (*models.Payment, *PaymentExtractedData, *string, error) {
	var ocrError *string

	// Perform OCR on the screenshot with specialized payment screenshot recognition
	text, err := s.ocrService.RecognizePaymentScreenshot(input.ScreenshotPath)
	if err != nil {
		msg := err.Error()
		ocrError = &msg
		text = ""
	}

	// Parse payment data from OCR text
	extracted, parseErr := s.ocrService.ParsePaymentScreenshot(text)
	if parseErr != nil {
		msg := parseErr.Error()
		ocrError = &msg
		extracted = &PaymentExtractedData{RawText: text}
		extracted.PrettyText = formatPaymentPrettyText(text, extracted)
	}

	// Store extracted data as JSON
	extractedDataJSON, err := ExtractedDataToJSON(extracted)
	if err != nil {
		// Log the error but continue - extracted data is optional
		extractedDataJSON = nil
	}

	// Create payment record with extracted data
	payment := &models.Payment{
		ID:              utils.GenerateUUID(),
		Amount:          0.0, // Default to 0.0, will be updated if amount is extracted
		Merchant:        extracted.Merchant,
		PaymentMethod:   extracted.PaymentMethod,
		TransactionTime: "",
		ScreenshotPath:  &input.ScreenshotPath,
		ExtractedData:   extractedDataJSON,
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
	if extracted.TransactionTime != nil {
		payment.TransactionTime = *extracted.TransactionTime
	}

	// If no transaction time extracted, use current time
	if payment.TransactionTime == "" {
		payment.TransactionTime = utils.CurrentTimeString()
	}

	if err := s.repo.Create(payment); err != nil {
		return nil, nil, ocrError, err
	}

	return payment, extracted, ocrError, nil
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
		_ = database.GetDB().Model(&models.Invoice{}).Count(&total).Error
		if debug {
			log.Printf("[MATCH] payment=%s repo candidates=0, fallback to recent invoices (total=%d)", paymentID, total)
		}
		if total > 0 {
			var recent []models.Invoice
			_ = database.GetDB().
				Model(&models.Invoice{}).
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
	if extracted.Amount != nil && *extracted.Amount > 0 {
		updateData["amount"] = *extracted.Amount
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
	if extracted.TransactionTime != nil {
		updateData["transaction_time"] = *extracted.TransactionTime
	}

	// Update the payment record
	if err := s.repo.Update(paymentID, updateData); err != nil {
		return nil, fmt.Errorf("failed to update payment: %w", err)
	}

	return extracted, nil
}
