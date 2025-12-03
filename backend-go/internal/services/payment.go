package services

import (
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
	// Perform OCR on the screenshot
	text, err := s.ocrService.RecognizeImage(input.ScreenshotPath)
	if err != nil {
		return nil, nil, err
	}

	// Parse payment data from OCR text
	extracted, err := s.ocrService.ParsePaymentScreenshot(text)
	if err != nil {
		return nil, nil, err
	}

	// Store extracted data as JSON
	extractedDataJSON, _ := ExtractedDataToJSON(extracted)

	// Create payment record with extracted data
	payment := &models.Payment{
		ID:              utils.GenerateUUID(),
		Amount:          0, // Will be set from extracted data
		Merchant:        extracted.Merchant,
		PaymentMethod:   extracted.PaymentMethod,
		TransactionTime: "",
		ScreenshotPath:  &input.ScreenshotPath,
		ExtractedData:   extractedDataJSON,
	}

	// Set amount if extracted
	if extracted.Amount != nil {
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
