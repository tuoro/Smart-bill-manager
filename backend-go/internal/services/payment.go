package services

import (
	"fmt"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
)

type PaymentService struct {
	repo       *repository.PaymentRepository
	ocrService *OCRService
}

func NewPaymentService() *PaymentService {
	return &PaymentService{
		repo:       repository.NewPaymentRepository(),
		ocrService: NewOCRService(),
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

func (s *PaymentService) CreateFromScreenshot(input CreateFromScreenshotInput) (*models.Payment, *PaymentExtractedData, error) {
	// Perform OCR on the screenshot with specialized payment screenshot recognition
	text, err := s.ocrService.RecognizePaymentScreenshot(input.ScreenshotPath)
	if err != nil {
		return nil, nil, err
	}

	// Parse payment data from OCR text
	extracted, err := s.ocrService.ParsePaymentScreenshot(text)
	if err != nil {
		return nil, nil, err
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
	if extracted.Amount != nil && *extracted.Amount > 0 {
		payment.Amount = *extracted.Amount
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
		return nil, nil, err
	}

	return payment, extracted, nil
}

// GetLinkedInvoices returns all invoices linked to a payment
func (s *PaymentService) GetLinkedInvoices(paymentID string) ([]models.Invoice, error) {
	return s.repo.GetLinkedInvoices(paymentID)
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
