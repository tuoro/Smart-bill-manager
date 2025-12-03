package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/otiai10/gosseract/v2"
)

// OCRService provides OCR functionality
type OCRService struct{}

const (
	// taxIDPattern matches Chinese unified social credit codes (15-20 alphanumeric characters)
	taxIDPattern = `[A-Z0-9]{15,20}`
)

func NewOCRService() *OCRService {
	return &OCRService{}
}

// PaymentExtractedData represents extracted payment information
type PaymentExtractedData struct {
	Amount          *float64 `json:"amount"`
	Merchant        *string  `json:"merchant"`
	TransactionTime *string  `json:"transaction_time"`
	PaymentMethod   *string  `json:"payment_method"`
	OrderNumber     *string  `json:"order_number"`
	RawText         string   `json:"raw_text"`
}

// InvoiceExtractedData represents extracted invoice information
type InvoiceExtractedData struct {
	InvoiceNumber *string  `json:"invoice_number"`
	InvoiceDate   *string  `json:"invoice_date"`
	Amount        *float64 `json:"amount"`
	TaxAmount     *float64 `json:"tax_amount"`
	SellerName    *string  `json:"seller_name"`
	BuyerName     *string  `json:"buyer_name"`
	RawText       string   `json:"raw_text"`
}

// RecognizeImage performs OCR on an image file
func (s *OCRService) RecognizeImage(imagePath string) (string, error) {
	client := gosseract.NewClient()
	defer client.Close()

	// Set language to Chinese simplified and English
	client.SetLanguage("chi_sim", "eng")

	if err := client.SetImage(imagePath); err != nil {
		return "", fmt.Errorf("failed to set image: %w", err)
	}

	text, err := client.Text()
	if err != nil {
		return "", fmt.Errorf("failed to recognize text: %w", err)
	}

	return text, nil
}

// RecognizePDF extracts text from PDF, using OCR if necessary
func (s *OCRService) RecognizePDF(pdfPath string) (string, error) {
	fmt.Printf("[OCR] Starting PDF recognition for: %s\n", pdfPath)
	
	// First try to extract text directly from PDF
	text, err := s.extractTextFromPDF(pdfPath)
	if err != nil {
		fmt.Printf("[OCR] Direct text extraction failed: %v\n", err)
		// If direct extraction fails, convert PDF to images and use OCR
		return s.pdfToImageOCR(pdfPath)
	}
	
	if strings.TrimSpace(text) == "" {
		fmt.Printf("[OCR] No text found in PDF, attempting OCR conversion\n")
		return s.pdfToImageOCR(pdfPath)
	}
	
	fmt.Printf("[OCR] Successfully extracted %d characters from PDF\n", len(text))
	return text, nil
}

// extractTextFromPDF extracts text from a PDF file
func (s *OCRService) extractTextFromPDF(pdfPath string) (string, error) {
	fmt.Printf("[OCR] Opening PDF file: %s\n", pdfPath)
	
	f, r, err := pdf.Open(pdfPath)
	if err != nil {
		fmt.Printf("[OCR] Failed to open PDF: %v\n", err)
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	totalPage := r.NumPage()
	fmt.Printf("[OCR] PDF has %d pages\n", totalPage)

	for pageIndex := 1; pageIndex <= totalPage; pageIndex++ {
		p := r.Page(pageIndex)
		if p.V.IsNull() {
			fmt.Printf("[OCR] Page %d is null, skipping\n", pageIndex)
			continue
		}

		text, err := p.GetPlainText(nil)
		if err != nil {
			fmt.Printf("[OCR] Failed to extract text from page %d: %v\n", pageIndex, err)
			continue
		}
		fmt.Printf("[OCR] Extracted %d characters from page %d\n", len(text), pageIndex)
		buf.WriteString(text)
		buf.WriteString("\n")
	}

	result := buf.String()
	fmt.Printf("[OCR] Total extracted text length: %d characters\n", len(result))
	return result, nil
}

// pdfToImageOCR converts PDF pages to images and performs OCR
func (s *OCRService) pdfToImageOCR(pdfPath string) (string, error) {
	// For scanned PDFs without text, we would need to convert pages to images
	// and then perform OCR. This is a placeholder for that functionality.
	// In a production system, you might use tools like pdfcpu to extract images
	// or convert PDF pages to images using ImageMagick or similar.
	
	return "", fmt.Errorf("scanned PDF OCR is not yet fully implemented - please use text-based PDFs or convert scanned PDFs to image files (JPG/PNG) and upload those instead")
}

// ParsePaymentScreenshot extracts payment information from OCR text
func (s *OCRService) ParsePaymentScreenshot(text string) (*PaymentExtractedData, error) {
	data := &PaymentExtractedData{
		RawText: text,
	}

	// Normalize text for better matching - remove extra spaces but keep structure
	text = strings.TrimSpace(text)
	// Replace multiple spaces with single space
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")

	// Try to detect payment platform and extract accordingly
	if s.isWeChatPay(text) {
		s.parseWeChatPay(text, data)
	} else if s.isAlipay(text) {
		s.parseAlipay(text, data)
	} else if s.isBankTransfer(text) {
		s.parseBankTransfer(text, data)
	}

	// Generic amount extraction if not found
	if data.Amount == nil {
		s.extractAmount(text, data)
	}

	return data, nil
}

// isWeChatPay checks if text is from WeChat Pay
func (s *OCRService) isWeChatPay(text string) bool {
	keywords := []string{"微信支付", "微信", "WeChat", "支付成功", "转账成功"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// isAlipay checks if text is from Alipay
func (s *OCRService) isAlipay(text string) bool {
	keywords := []string{"支付宝", "Alipay", "付款成功"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// isBankTransfer checks if text is from bank transfer
func (s *OCRService) isBankTransfer(text string) bool {
	keywords := []string{"银行", "转账", "交易成功", "电子回单"}
	count := 0
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			count++
		}
	}
	return count >= 2
}

// parseWeChatPay extracts WeChat Pay information
func (s *OCRService) parseWeChatPay(text string, data *PaymentExtractedData) {
	method := "微信支付"
	data.PaymentMethod = &method

	// Extract amount: ¥123.45 or 金额¥123.45
	amountRegexes := []*regexp.Regexp{
		regexp.MustCompile(`[¥￥][\s]*([\d,]+\.?\d*)`),
		regexp.MustCompile(`金额[：:]?[¥￥]?([\d,]+\.?\d*)`),
		regexp.MustCompile(`支付金额[：:]?[¥￥]?([\d,]+\.?\d*)`),
		regexp.MustCompile(`转账金额[：:]?[¥￥]?([\d,]+\.?\d*)`),
	}
	for _, re := range amountRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil {
				data.Amount = amount
				break
			}
		}
	}

	// Extract merchant/receiver
	merchantRegexes := []*regexp.Regexp{
		regexp.MustCompile(`收款方[：:]?([^\s¥￥]+)`),
		regexp.MustCompile(`收款人[：:]?([^\s¥￥]+)`),
		regexp.MustCompile(`转账给([^\s¥￥]+)`),
	}
	for _, re := range merchantRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			merchant := strings.TrimSpace(match[1])
			if merchant != "" {
				data.Merchant = &merchant
				break
			}
		}
	}

	// Extract transaction time
	timeRegexes := []*regexp.Regexp{
		regexp.MustCompile(`支付时间[：:]?([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`转账时间[：:]?([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日\s[\d]{2}:[\d]{2}:[\d]{2})`),
	}
	for _, re := range timeRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			timeStr := match[1]
			data.TransactionTime = &timeStr
			break
		}
	}

	// Extract order number
	orderRegexes := []*regexp.Regexp{
		regexp.MustCompile(`交易单号[：:]?([\d]+)`),
		regexp.MustCompile(`订单号[：:]?([\d]+)`),
		regexp.MustCompile(`流水号[：:]?([\d]+)`),
	}
	for _, re := range orderRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			orderNum := match[1]
			data.OrderNumber = &orderNum
			break
		}
	}
}

// parseAlipay extracts Alipay information
func (s *OCRService) parseAlipay(text string, data *PaymentExtractedData) {
	method := "支付宝"
	data.PaymentMethod = &method

	// Extract amount
	amountRegexes := []*regexp.Regexp{
		regexp.MustCompile(`[¥￥][\s]*([\d,]+\.?\d*)`),
		regexp.MustCompile(`金额[：:]?[¥￥]?([\d,]+\.?\d*)`),
		regexp.MustCompile(`付款金额[：:]?[¥￥]?([\d,]+\.?\d*)`),
	}
	for _, re := range amountRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil {
				data.Amount = amount
				break
			}
		}
	}

	// Extract merchant
	merchantRegexes := []*regexp.Regexp{
		regexp.MustCompile(`商家[：:]?([^\s¥￥]+)`),
		regexp.MustCompile(`收款方[：:]?([^\s¥￥]+)`),
		regexp.MustCompile(`付款给([^\s¥￥]+)`),
	}
	for _, re := range merchantRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			merchant := strings.TrimSpace(match[1])
			if merchant != "" {
				data.Merchant = &merchant
				break
			}
		}
	}

	// Extract transaction time
	timeRegexes := []*regexp.Regexp{
		regexp.MustCompile(`创建时间[：:]?([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`付款时间[：:]?([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日\s[\d]{2}:[\d]{2})`),
	}
	for _, re := range timeRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			timeStr := match[1]
			data.TransactionTime = &timeStr
			break
		}
	}

	// Extract order number
	orderRegexes := []*regexp.Regexp{
		regexp.MustCompile(`订单号[：:]?([\d]+)`),
		regexp.MustCompile(`交易号[：:]?([\d]+)`),
		regexp.MustCompile(`流水号[：:]?([\d]+)`),
	}
	for _, re := range orderRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			orderNum := match[1]
			data.OrderNumber = &orderNum
			break
		}
	}
}

// parseBankTransfer extracts bank transfer information
func (s *OCRService) parseBankTransfer(text string, data *PaymentExtractedData) {
	method := "银行转账"
	data.PaymentMethod = &method

	// Extract amount
	amountRegexes := []*regexp.Regexp{
		regexp.MustCompile(`金额[：:]?[¥￥]?([\d,]+\.?\d*)`),
		regexp.MustCompile(`转账金额[：:]?[¥￥]?([\d,]+\.?\d*)`),
		regexp.MustCompile(`交易金额[：:]?[¥￥]?([\d,]+\.?\d*)`),
	}
	for _, re := range amountRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil {
				data.Amount = amount
				break
			}
		}
	}

	// Extract receiver
	merchantRegexes := []*regexp.Regexp{
		regexp.MustCompile(`收款人[：:]?([^\s¥￥]+)`),
		regexp.MustCompile(`收款账户[：:]?([^\s¥￥]+)`),
	}
	for _, re := range merchantRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			merchant := strings.TrimSpace(match[1])
			if merchant != "" {
				data.Merchant = &merchant
				break
			}
		}
	}

	// Extract transaction time
	timeRegexes := []*regexp.Regexp{
		regexp.MustCompile(`转账时间[：:]?([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`交易时间[：:]?([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日[\d]{2}:[\d]{2})`),
	}
	for _, re := range timeRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			timeStr := match[1]
			data.TransactionTime = &timeStr
			break
		}
	}
}

// extractAmount extracts amount from text using generic patterns
func (s *OCRService) extractAmount(text string, data *PaymentExtractedData) {
	// Try various amount patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`[¥￥][\s]*([\d,]+\.?\d*)`),
		regexp.MustCompile(`([\d,]+\.?\d*)元`),
	}

	for _, re := range patterns {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil {
				data.Amount = amount
				return
			}
		}
	}
}

// ParseInvoiceData extracts invoice information from OCR text
func (s *OCRService) ParseInvoiceData(text string) (*InvoiceExtractedData, error) {
	data := &InvoiceExtractedData{
		RawText: text,
	}

	// Extract invoice number - support both same-line and newline-separated formats
	invoiceNumRegexes := []*regexp.Regexp{
		regexp.MustCompile(`发票号码[：:]?\s*[\n\r]?\s*(\d+)`),
		regexp.MustCompile(`发票代码[：:]?\s*[\n\r]?\s*(\d+)`),
		regexp.MustCompile(`No[\.:]?\s*[\n\r]?\s*(\d+)`),
	}
	for _, re := range invoiceNumRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			invoiceNum := match[1]
			data.InvoiceNumber = &invoiceNum
			break
		}
	}
	
	// If not found, try to match standalone 20+ digit invoice numbers (electronic invoice format)
	// Limit to 25 digits to avoid matching unintended long sequences
	if data.InvoiceNumber == nil {
		standaloneNumRegex := regexp.MustCompile(`\b(\d{20,25})\b`)
		if match := standaloneNumRegex.FindStringSubmatch(text); len(match) > 1 {
			invoiceNum := match[1]
			data.InvoiceNumber = &invoiceNum
		}
	}

	// Extract invoice date - support both same-line and newline-separated formats
	dateRegexes := []*regexp.Regexp{
		regexp.MustCompile(`开票日期[：:]?\s*[\n\r]?\s*(\d{4}年\d{1,2}月\d{1,2}日)`),
		regexp.MustCompile(`开票日期[：:]?\s*[\n\r]?\s*(\d{4}-\d{2}-\d{2})`),
		regexp.MustCompile(`日期[：:]?\s*[\n\r]?\s*(\d{4}年\d{1,2}月\d{1,2}日)`),
	}
	for _, re := range dateRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			date := match[1]
			data.InvoiceDate = &date
			break
		}
	}
	
	// If not found, try to match standalone date format (YYYY年M月D日 or YYYY年MM月DD日)
	if data.InvoiceDate == nil {
		standaloneDateRegex := regexp.MustCompile(`\b(\d{4}年\d{1,2}月\d{1,2}日)\b`)
		if match := standaloneDateRegex.FindStringSubmatch(text); len(match) > 1 {
			date := match[1]
			data.InvoiceDate = &date
		}
	}

	// Extract amount - support newline-separated formats like "（小写）\n¥\n3080.00"
	amountRegexes := []*regexp.Regexp{
		regexp.MustCompile(`合计金额[（(]小写[)）][：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
		regexp.MustCompile(`价税合计[（(]小写[)）][：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
		regexp.MustCompile(`[（(]小写[)）][：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
		regexp.MustCompile(`总计[：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
		regexp.MustCompile(`金额[：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
	}
	for _, re := range amountRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil {
				data.Amount = amount
				break
			}
		}
	}
	
	// If not found, try to find amount after Chinese character amount (e.g., "叁仟零捌拾圆整" followed by "¥3080.00")
	// This handles the electronic invoice format where the amount appears after the Chinese text
	// Include both simplified (万) and traditional (萬) characters
	if data.Amount == nil {
		chineseAmountRegex := regexp.MustCompile(`[零壹贰叁肆伍陆柒捌玖拾佰仟万萬亿]+圆整[\s\n\r]*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`)
		if match := chineseAmountRegex.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil {
				data.Amount = amount
			}
		}
	}

	// Extract tax amount
	taxRegexes := []*regexp.Regexp{
		regexp.MustCompile(`税额[：:]?\s*[¥￥]?([\d,.]+)`),
		regexp.MustCompile(`税金[：:]?\s*[¥￥]?([\d,.]+)`),
	}
	for _, re := range taxRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if tax := parseAmount(match[1]); tax != nil {
				data.TaxAmount = tax
				break
			}
		}
	}

	// Extract seller name - handle both inline and newline-separated formats
	// First try patterns with explicit "销售方" prefix
	sellerRegexes := []*regexp.Regexp{
		regexp.MustCompile(`销售方[：:]?\s*名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
		regexp.MustCompile(`销售方名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
		regexp.MustCompile(`出票方[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
	}
	for _, re := range sellerRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			seller := strings.TrimSpace(match[1])
			// Filter out section headers like "信息" (information) that might be captured
			if seller != "" && seller != "信" && seller != "息" {
				data.SellerName = &seller
				break
			}
		}
	}
	
	// If not found, try to find in seller section context
	// Look for seller section and extract tax ID followed by name
	// Format: "销售方信息 统一社会信用代码/纳税人识别号：92310109MA1KMFLM1K 名称：上海市虹口区鹏侠百货商店"
	if data.SellerName == nil {
		// Match tax ID followed by company name
		sellerSectionRegex := regexp.MustCompile(fmt.Sprintf(`(?s)销.*?售.*?方.*?信.*?息.*?统一社会信用代码/纳税人识别号[：:]?\s*[\n\r]?\s*(%s)[\s\n\r]+名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`, taxIDPattern))
		if match := sellerSectionRegex.FindStringSubmatch(text); len(match) > 2 {
			seller := strings.TrimSpace(match[2])
			if seller != "" && seller != "购" && seller != "买" && seller != "方" {
				data.SellerName = &seller
			}
		}
	}
	
	// If still not found, try a more flexible pattern looking for tax ID followed by name
	// This handles cases where the seller info appears without explicit section markers
	if data.SellerName == nil {
		// Look for patterns like: tax ID on one line, then "名称：" followed by name
		flexibleSellerRegex := regexp.MustCompile(fmt.Sprintf(`\b(%s)\b[\s\n\r]+名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`, taxIDPattern))
		if match := flexibleSellerRegex.FindStringSubmatch(text); len(match) > 2 {
			seller := strings.TrimSpace(match[2])
			// Additional validation: check if this looks like a company name
			// Sellers should not be "个人" (individual) - that would be a buyer
			if seller != "" && len(seller) > 2 && seller != "个人" {
				data.SellerName = &seller
			}
		}
	}

	// Extract buyer name - handle both inline and newline-separated formats
	// First try patterns with explicit "购买方" prefix
	buyerRegexes := []*regexp.Regexp{
		regexp.MustCompile(`购买方[：:]?\s*名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
		regexp.MustCompile(`购买方名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
		regexp.MustCompile(`购货方[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
	}
	for _, re := range buyerRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			buyer := strings.TrimSpace(match[1])
			// Filter out section headers like "信息" (information) that might be captured
			if buyer != "" && buyer != "信" && buyer != "息" {
				data.BuyerName = &buyer
				break
			}
		}
	}
	
	// If not found, try to find in buyer section context
	// Look for buyer section and extract tax ID followed by name or just name
	// Format: "购买方信息 统一社会信用代码/纳税人识别号： 名称：个人"
	if data.BuyerName == nil {
		// Match tax ID (optional, may be empty for individuals) followed by name
		buyerSectionRegex := regexp.MustCompile(`(?s)购.*?买.*?方.*?信.*?息.*?统一社会信用代码/纳税人识别号[：:]?\s*[\n\r]?\s*([A-Z0-9]*)[\s\n\r]+名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`)
		if match := buyerSectionRegex.FindStringSubmatch(text); len(match) > 2 {
			buyer := strings.TrimSpace(match[2])
			if buyer != "" && buyer != "销" && buyer != "售" && buyer != "方" {
				data.BuyerName = &buyer
			}
		}
	}
	
	// If still not found, try to match "个人" (individual) as a standalone buyer
	if data.BuyerName == nil {
		individualRegex := regexp.MustCompile(`\b(个人)\b`)
		if match := individualRegex.FindStringSubmatch(text); len(match) > 1 {
			buyer := match[1]
			data.BuyerName = &buyer
		}
	}

	return data, nil
}

// parseAmount parses amount string to float64
func parseAmount(s string) *float64 {
	// Remove commas and spaces
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.TrimSpace(s)

	if s == "" {
		return nil
	}

	amount, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}

	return &amount
}

// ExtractedDataToJSON converts extracted data to JSON string
func ExtractedDataToJSON(data interface{}) (*string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	jsonStr := string(jsonData)
	return &jsonStr, nil
}
