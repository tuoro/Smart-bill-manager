package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
)

type InvoiceService struct {
	repo       *repository.InvoiceRepository
	uploadsDir string
}

func NewInvoiceService(uploadsDir string) *InvoiceService {
	return &InvoiceService{
		repo:       repository.NewInvoiceRepository(),
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

	// Try to extract data from PDF
	var invoiceNumber, invoiceDate, sellerName, buyerName *string
	var amount *float64
	var extractedData *string

	filePath := input.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(s.uploadsDir, "..", filePath)
	}

	if strings.HasSuffix(strings.ToLower(input.Filename), ".pdf") {
		if extracted := s.extractPDFData(filePath); extracted != nil {
			invoiceNumber = extracted.InvoiceNumber
			invoiceDate = extracted.InvoiceDate
			amount = extracted.Amount
			sellerName = extracted.SellerName
			buyerName = extracted.BuyerName
			if extracted.RawData != nil {
				jsonData, _ := json.Marshal(extracted.RawData)
				jsonStr := string(jsonData)
				extractedData = &jsonStr
			}
		}
	}

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
		SellerName:    sellerName,
		BuyerName:     buyerName,
		ExtractedData: extractedData,
		Source:        source,
	}

	if err := s.repo.Create(invoice); err != nil {
		return nil, err
	}

	return invoice, nil
}

type ExtractedPDFData struct {
	InvoiceNumber *string
	InvoiceDate   *string
	Amount        *float64
	SellerName    *string
	BuyerName     *string
	RawData       map[string]interface{}
}

func (s *InvoiceService) extractPDFData(filePath string) *ExtractedPDFData {
	// Read PDF file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	// Simple text extraction from PDF (basic implementation)
	// For a production system, use a proper PDF parsing library
	text := extractTextFromPDF(content)
	if text == "" {
		return nil
	}

	extracted := &ExtractedPDFData{
		RawData: map[string]interface{}{
			"text": truncateString(text, 2000),
		},
	}

	// Extract invoice information using regex patterns (Chinese invoice format)
	invoiceNumberRe := regexp.MustCompile(`发票号码[：:]\s*(\d+)`)
	invoiceDateRe := regexp.MustCompile(`开票日期[：:]\s*(\d{4}年\d{1,2}月\d{1,2}日|\d{4}-\d{2}-\d{2})`)
	amountRe := regexp.MustCompile(`合计金额[（(]小写[)）][：:]\s*[¥￥]?([\d.]+)|价税合计[（(]大写[)）].*?[¥￥]([\d.]+)`)
	sellerRe := regexp.MustCompile(`销售方[：:]?\s*名称[：:]\s*([^\n]+)|销售方名称[：:]\s*([^\n]+)`)
	buyerRe := regexp.MustCompile(`购买方[：:]?\s*名称[：:]\s*([^\n]+)|购买方名称[：:]\s*([^\n]+)`)

	if match := invoiceNumberRe.FindStringSubmatch(text); len(match) > 1 {
		extracted.InvoiceNumber = &match[1]
	}

	if match := invoiceDateRe.FindStringSubmatch(text); len(match) > 1 {
		extracted.InvoiceDate = &match[1]
	}

	if match := amountRe.FindStringSubmatch(text); len(match) > 1 {
		amountStr := match[1]
		if amountStr == "" && len(match) > 2 {
			amountStr = match[2]
		}
		if amountStr != "" {
			var amt float64
			if _, err := parseFloat(amountStr, &amt); err == nil {
				extracted.Amount = &amt
			}
		}
	}

	if match := sellerRe.FindStringSubmatch(text); len(match) > 1 {
		seller := match[1]
		if seller == "" && len(match) > 2 {
			seller = match[2]
		}
		if seller != "" {
			extracted.SellerName = &seller
		}
	}

	if match := buyerRe.FindStringSubmatch(text); len(match) > 1 {
		buyer := match[1]
		if buyer == "" && len(match) > 2 {
			buyer = match[2]
		}
		if buyer != "" {
			extracted.BuyerName = &buyer
		}
	}

	return extracted
}

// extractTextFromPDF is a simple PDF text extractor
// For production, use a proper library like pdfcpu or ledongthuc/pdf
func extractTextFromPDF(content []byte) string {
	// This is a basic implementation that looks for text streams in PDF
	// A proper implementation would use a PDF parsing library
	
	// Simple extraction - look for readable ASCII text
	var result strings.Builder
	inText := false
	
	for i := 0; i < len(content); i++ {
		if i+1 < len(content) && content[i] == 'B' && content[i+1] == 'T' {
			inText = true
			continue
		}
		if i+1 < len(content) && content[i] == 'E' && content[i+1] == 'T' {
			inText = false
			result.WriteByte(' ')
			continue
		}
		if inText {
			// Try to extract text between parentheses (PDF string objects)
			if content[i] == '(' {
				j := i + 1
				for j < len(content) && content[j] != ')' {
					if content[j] >= 32 && content[j] < 127 {
						result.WriteByte(content[j])
					}
					j++
				}
				i = j
			}
		}
	}
	
	// If basic extraction failed, try to find any readable text
	if result.Len() == 0 {
		for _, b := range content {
			if b >= 32 && b < 127 {
				result.WriteByte(b)
			} else if b == '\n' || b == '\r' {
				result.WriteByte(' ')
			}
		}
	}
	
	return strings.TrimSpace(result.String())
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func parseFloat(s string, result *float64) (bool, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false, err
	}
	
	*result = val
	return true, nil
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
