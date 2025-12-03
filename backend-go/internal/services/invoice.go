package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
)

type InvoiceService struct {
	repo       *repository.InvoiceRepository
	ocrService *OCRService
	uploadsDir string
}

func NewInvoiceService(uploadsDir string) *InvoiceService {
	return &InvoiceService{
		repo:       repository.NewInvoiceRepository(),
		ocrService: NewOCRService(),
		uploadsDir: uploadsDir,
	}
}

type CreateInvoiceInput struct {
	PaymentID    *string `json:"payment_id"`
	Filename     string  `json:"filename"`
	OriginalName string  `json:"original_name"`
	FilePath     string  `json:"file_path"`
	FileSize     int64   `json:"file_size"`
	Source       string  `json:"source"`
}

func (s *InvoiceService) Create(input CreateInvoiceInput) (*models.Invoice, error) {
	id := utils.GenerateUUID()

	// Build absolute file path
	filePath := input.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(s.uploadsDir, "..", filePath)
	}

	// Parse the invoice PDF
	invoiceNumber, invoiceDate, sellerName, buyerName,
		amount, taxAmount,
		extractedData, rawText,
		parseStatus, parseError := s.parseInvoicePDF(filePath, input.Filename)

	source := input.Source
	if source == "" {
		source = "upload"
	}

	invoice := &models.Invoice{
		ID:            id,
		PaymentID:     input.PaymentID,
		Filename:      input.Filename,
		OriginalName:  input.OriginalName,
		FilePath:      input.FilePath,
		FileSize:      &input.FileSize,
		InvoiceNumber: invoiceNumber,
		InvoiceDate:   invoiceDate,
		Amount:        amount,
		TaxAmount:     taxAmount,
		SellerName:    sellerName,
		BuyerName:     buyerName,
		ExtractedData: extractedData,
		ParseStatus:   parseStatus,
		ParseError:    parseError,
		RawText:       rawText,
		Source:        source,
	}

	if err := s.repo.Create(invoice); err != nil {
		return nil, err
	}

	return invoice, nil
}

type InvoiceFilterInput struct {
	Limit  int `form:"limit"`
	Offset int `form:"offset"`
}

func (s *InvoiceService) GetAll(filter InvoiceFilterInput) ([]models.Invoice, error) {
	return s.repo.FindAll(repository.InvoiceFilter{
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

func (s *InvoiceService) GetByID(id string) (*models.Invoice, error) {
	return s.repo.FindByID(id)
}

func (s *InvoiceService) GetByPaymentID(paymentID string) ([]models.Invoice, error) {
	return s.repo.FindByPaymentID(paymentID)
}

type UpdateInvoiceInput struct {
	PaymentID     *string  `json:"payment_id"`
	InvoiceNumber *string  `json:"invoice_number"`
	InvoiceDate   *string  `json:"invoice_date"`
	Amount        *float64 `json:"amount"`
	TaxAmount     *float64 `json:"tax_amount"`
	SellerName    *string  `json:"seller_name"`
	BuyerName     *string  `json:"buyer_name"`
}

func (s *InvoiceService) Update(id string, input UpdateInvoiceInput) error {
	data := make(map[string]interface{})

	if input.PaymentID != nil {
		data["payment_id"] = *input.PaymentID
	}
	if input.InvoiceNumber != nil {
		data["invoice_number"] = *input.InvoiceNumber
	}
	if input.InvoiceDate != nil {
		data["invoice_date"] = *input.InvoiceDate
	}
	if input.Amount != nil {
		data["amount"] = *input.Amount
	}
	if input.TaxAmount != nil {
		data["tax_amount"] = *input.TaxAmount
	}
	if input.SellerName != nil {
		data["seller_name"] = *input.SellerName
	}
	if input.BuyerName != nil {
		data["buyer_name"] = *input.BuyerName
	}

	if len(data) == 0 {
		return nil
	}

	return s.repo.Update(id, data)
}

func (s *InvoiceService) Delete(id string) error {
	// Get invoice first to delete file
	invoice, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	// Delete file
	filePath := invoice.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(s.uploadsDir, "..", filePath)
	}
	_ = os.Remove(filePath) // Ignore error if file doesn't exist

	return s.repo.Delete(id)
}

func (s *InvoiceService) GetStats() (*models.InvoiceStats, error) {
	return s.repo.GetStats()
}

// LinkPayment links an invoice to a payment
func (s *InvoiceService) LinkPayment(invoiceID, paymentID string) error {
	return s.repo.LinkPayment(invoiceID, paymentID)
}

// UnlinkPayment removes the link between an invoice and a payment
func (s *InvoiceService) UnlinkPayment(invoiceID, paymentID string) error {
	return s.repo.UnlinkPayment(invoiceID, paymentID)
}

// GetLinkedPayments returns all payments linked to an invoice
func (s *InvoiceService) GetLinkedPayments(invoiceID string) ([]models.Payment, error) {
	return s.repo.GetLinkedPayments(invoiceID)
}

// SuggestPayments suggests payments that might match this invoice based on amount and date
func (s *InvoiceService) SuggestPayments(invoiceID string, limit int) ([]models.Payment, error) {
	invoice, err := s.repo.FindByID(invoiceID)
	if err != nil {
		return nil, err
	}

	// Get suggestions based on amount and date proximity
	return s.repo.SuggestPayments(invoice, limit)
}

// parseInvoicePDF is a helper method that parses a PDF invoice and returns the extracted data
func (s *InvoiceService) parseInvoicePDF(filePath, filename string) (
	invoiceNumber, invoiceDate, sellerName, buyerName *string,
	amount, taxAmount *float64,
	extractedData, rawText *string,
	parseStatus string,
	parseError *string,
) {
	parseStatus = "parsing"
	
	if !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
		parseStatus = "failed"
		errMsg := "Only PDF files can be parsed"
		parseError = &errMsg
		return
	}

	// Use OCR service to extract text
	text, err := s.ocrService.RecognizePDF(filePath)
	if err != nil {
		parseStatus = "failed"
		errMsg := fmt.Sprintf("OCR recognition failed: %v", err)
		parseError = &errMsg
		return
	}
	
	if text == "" {
		parseStatus = "failed"
		errMsg := "No text extracted from PDF"
		parseError = &errMsg
		return
	}
	
	// Save raw text for frontend display
	rawText = &text
	
	// Parse the extracted text
	extracted, err := s.ocrService.ParseInvoiceData(text)
	if err != nil {
		parseStatus = "failed"
		errMsg := fmt.Sprintf("Failed to parse invoice data: %v", err)
		parseError = &errMsg
		return
	}
	
	invoiceNumber = extracted.InvoiceNumber
	invoiceDate = extracted.InvoiceDate
	amount = extracted.Amount
	taxAmount = extracted.TaxAmount
	sellerName = extracted.SellerName
	buyerName = extracted.BuyerName
	
	// Store extracted data as JSON
	if jsonStr, err := ExtractedDataToJSON(extracted); err == nil {
		extractedData = jsonStr
	}
	parseStatus = "success"
	return
}

// Reparse re-triggers parsing for an invoice
func (s *InvoiceService) Reparse(id string) (*models.Invoice, error) {
	// Get the invoice
	invoice, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	// Build absolute file path
	filePath := invoice.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(s.uploadsDir, "..", filePath)
	}

	// Parse the invoice PDF
	invoiceNumber, invoiceDate, sellerName, buyerName,
		amount, taxAmount,
		extractedData, rawText,
		parseStatus, parseError := s.parseInvoicePDF(filePath, invoice.Filename)

	// Update the invoice with parsed data
	updateData := map[string]interface{}{
		"parse_status": parseStatus,
		"parse_error":  parseError,
		"raw_text":     rawText,
	}
	
	if invoiceNumber != nil {
		updateData["invoice_number"] = *invoiceNumber
	}
	if invoiceDate != nil {
		updateData["invoice_date"] = *invoiceDate
	}
	if amount != nil {
		updateData["amount"] = *amount
	}
	if taxAmount != nil {
		updateData["tax_amount"] = *taxAmount
	}
	if sellerName != nil {
		updateData["seller_name"] = *sellerName
	}
	if buyerName != nil {
		updateData["buyer_name"] = *buyerName
	}
	if extractedData != nil {
		updateData["extracted_data"] = *extractedData
	}

	if err := s.repo.Update(id, updateData); err != nil {
		return nil, err
	}

	// Return updated invoice
	return s.repo.FindByID(id)
}
