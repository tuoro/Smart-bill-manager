package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/liyue201/goqr"
)

// OCRService provides OCR functionality
type OCRService struct{}

const (
	// taxIDPattern matches common Chinese taxpayer identifiers:
	// - 15-digit taxpayer ID (legacy)
	// - 18-char unified social credit code
	// Keeping this tight avoids misclassifying invoice/order/check codes (often 19-20 digits) as tax IDs.
	taxIDPattern = `(?:[A-Z0-9]{15}|[A-Z0-9]{18})`

	// amountPattern matches monetary amounts with ¥ or ￥ symbol
	// Pattern explanation: (?:,\d{3})* allows comma-separated thousands, (?:\.\d{1,2})? makes decimals optional
	amountPattern = `[¥￥]\s*\d+(?:,\d{3})*(?:\.\d{1,2})?`

	// taxIDPatternWithBoundary for structured extraction (word boundaries for better matching)
	taxIDPatternWithBoundary = `\b(?:[A-Z0-9]{15}|[A-Z0-9]{18})\b`

	// MinValidAmount is the minimum amount threshold for payment extraction
	MinValidAmount = 1.0

	// MaxMerchantNameLength is the maximum allowed length for merchant names
	MaxMerchantNameLength = 50

	// digitsWhitelist defines characters allowed for digit-only OCR
	digitsWhitelist = "0123456789.-¥￥,"

	// RapidOCR CLI configuration
	rapidOCRTimeout = 60 * time.Second

	// pdfOCRDPI controls PDF->image rendering resolution; lower is faster.
	// Key header fields can be recovered via QR and/or ROI fallback.
	pdfOCRDPI = 220
)

func getPDFOCRDPI() int {
	if v := strings.TrimSpace(os.Getenv("SBM_PDF_OCR_DPI")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 120 && n <= 450 {
			return n
		}
	}
	return pdfOCRDPI
}

var (
	// Compiled regex patterns for better performance
	amountRegex = regexp.MustCompile(amountPattern)
	taxIDRegex  = regexp.MustCompile(taxIDPatternWithBoundary)

	// Name pattern for position-based extraction
	namePositionPattern = regexp.MustCompile(`名\s*称[：:]\s*([^\n\r]+?)(?:\s{3,}|[\n\r]|$)`)

	// Space-delimited date pattern
	spaceDelimitedDatePattern = regexp.MustCompile(`开票日期[：:]?\s*(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日`)

	// Date patterns - compiled once for performance
	datePatterns = []*regexp.Regexp{
		regexp.MustCompile(`\d{4}[/年\-]\s*\d{1,2}[/月\-]\s*\d{1,2}[日]?`),
		regexp.MustCompile(`\d{4}\s+\d{2}\s+\d{2}`),
	}

	// Payment parsing - compiled regex patterns for reuse
	negativeAmountRegex   = regexp.MustCompile(`[-−][\s]*[¥￥]?[\s]*([\d,]+\.?\d*)`)
	merchantFullNameRegex = regexp.MustCompile(`商户全称[：:]?[\s]*([^\n收单机构支付方式]+?)[\s]*(?:收单机构|支付方式|\n|$)`)
	merchantGenericRegex  = regexp.MustCompile(`([^\n]+(?:店|行|公司|商户|超市|餐厅|饭店|有限公司))`)

	// Pattern to insert space between Chinese date and time when 日 is directly followed by digits
	chineseDateTimePattern = regexp.MustCompile(`日(\d)`)

	// Amount detection patterns for merging OCR results
	// Note: First pattern uses \d{3,} to prioritize large amounts (e.g., 1700.00)
	// which are more likely to be the main transaction amount in payment screenshots
	amountDetectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`-?\d{3,}\.?\d{0,2}`), // Large amounts like 1700.00 or -1700.00
		regexp.MustCompile(`[¥￥]-?\d+\.?\d*`),    // Currency symbol with amount (any size)
	}
)

func NewOCRService() *OCRService {
	return &OCRService{}
}

func getOCREngine() string {
	engine := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_OCR_ENGINE")))
	// This project ships with RapidOCR v3 (onnxruntime CPU) as the default/recommended engine.
	// Other engines are intentionally not supported in the default Docker image.
	if engine != "rapidocr" {
		return "rapidocr"
	}
	return "rapidocr"
}

func ocrEngineInstallHint(engine string) string {
	return fmt.Sprintf("install rapidocr==3.* and onnxruntime (engine=%s)", engine)
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

// OCRCLIResponse represents the response from the Python OCR CLI script.
type OCRCLIResponse struct {
	Success   bool            `json:"success"`
	Text      string          `json:"text"`
	Lines     []OCRCLILine    `json:"lines"`
	LineCount int             `json:"line_count"`
	Engine    string          `json:"engine,omitempty"`
	Profile   string          `json:"profile,omitempty"`
	Variant   string          `json:"variant,omitempty"`
	Variants  []OCRCLIVariant `json:"variants,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type OCRCLIVariant struct {
	Variant string  `json:"variant"`
	Score   float64 `json:"score"`
	Lines   int     `json:"lines"`
	Chars   int     `json:"chars"`
}

// OCRCLILine represents a single line of OCR result
type OCRCLILine struct {
	Text       string      `json:"text"`
	Confidence float64     `json:"confidence"`
	Box        [][]float64 `json:"box"`
}

// RecognizeImage performs OCR on an image file (RapidOCR v3 only).
func (s *OCRService) RecognizeImage(imagePath string) (string, error) {
	return s.RecognizeWithRapidOCR(imagePath)
}

// RecognizeImageEnhanced performs OCR without any local image preprocessing (RapidOCR v3 only).
func (s *OCRService) RecognizeImageEnhanced(imagePath string) (string, error) {
	return s.RecognizeWithRapidOCR(imagePath)
}

// RecognizeWithRapidOCR executes the paddleocr_cli.py script for OCR recognition (RapidOCR only).
func (s *OCRService) RecognizeWithRapidOCR(imagePath string) (string, error) {
	return s.recognizeWithRapidOCRArgs(imagePath, nil)
}

func (s *OCRService) RecognizeWithRapidOCRProfile(imagePath, profile string) (string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" || profile == "default" {
		return s.recognizeWithRapidOCRArgs(imagePath, nil)
	}
	return s.recognizeWithRapidOCRArgs(imagePath, []string{"--profile", profile})
}

func (s *OCRService) recognizeWithRapidOCRArgs(imagePath string, extraArgs []string) (string, error) {
	fmt.Printf("[OCR] Running OCR CLI for: %s (engine=%s)\n", imagePath, getOCREngine())

	// Find the paddleocr_cli.py script
	scriptPath := s.findPaddleOCRScript()
	if scriptPath == "" {
		return "", fmt.Errorf("paddleocr_cli.py script not found")
	}

	// Execute Python script
	ctx, cancel := context.WithTimeout(context.Background(), rapidOCRTimeout)
	defer cancel()

	run := func(python string) ([]byte, error) {
		args := []string{scriptPath}
		args = append(args, extraArgs...)
		args = append(args, imagePath)
		cmd := exec.CommandContext(ctx, python, args...)
		return cmd.CombinedOutput()
	}

	output, execErr := run("python3")
	if execErr != nil {
		// Try with "python" if "python3" fails
		if altOut, altErr := run("python"); altErr == nil || len(altOut) > 0 {
			output = altOut
			execErr = altErr
		}
	}

	// Parse JSON output
	var result OCRCLIResponse
	if err := unmarshalPossiblyNoisyJSON(output, &result); err != nil {
		if execErr != nil {
			return "", fmt.Errorf("failed to execute RapidOCR CLI: %w (output: %s)", execErr, string(output))
		}
		return "", fmt.Errorf("failed to parse OCR CLI output: %w (output: %s)", err, string(output))
	}

	if !result.Success {
		return "", fmt.Errorf("OCR error: %s", result.Error)
	}

	engine := result.Engine
	if engine == "" {
		engine = "rapidocr"
	}
	profile := result.Profile
	if profile == "" {
		profile = "default"
	}
	if result.Variant != "" {
		fmt.Printf("[OCR] OCR extracted %d lines, %d characters (engine=%s profile=%s variant=%s)\n", result.LineCount, len(result.Text), engine, profile, result.Variant)
	} else {
		fmt.Printf("[OCR] OCR extracted %d lines, %d characters (engine=%s profile=%s)\n", result.LineCount, len(result.Text), engine, profile)
	}
	return result.Text, nil
}

func stripANSIEscapes(s string) string {
	// Best-effort removal of ANSI escape sequences such as "\x1b[32m".
	ansi := regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	return ansi.ReplaceAllString(s, "")
}

func unmarshalPossiblyNoisyJSON(output []byte, v any) error {
	// 1) direct
	if err := json.Unmarshal(output, v); err == nil {
		return nil
	}
	// 2) strip ANSI and retry
	cleaned := strings.TrimSpace(stripANSIEscapes(string(output)))
	if cleaned != "" {
		if err := json.Unmarshal([]byte(cleaned), v); err == nil {
			return nil
		}
		// 3) try last JSON object (in case logs precede it)
		if i := strings.LastIndex(cleaned, "{"); i >= 0 {
			tail := strings.TrimSpace(cleaned[i:])
			if tail != "" {
				if err := json.Unmarshal([]byte(tail), v); err == nil {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("invalid JSON")
}

// findPaddleOCRScript locates the paddleocr_cli.py script
func (s *OCRService) findPaddleOCRScript() string {
	// Check common locations
	locations := []string{
		"scripts/paddleocr_cli.py",
		"../scripts/paddleocr_cli.py",
		"/app/scripts/paddleocr_cli.py",
		"./paddleocr_cli.py",
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return ""
}

// findPDFTextScript locates the pdf_text_cli.py script (PyMuPDF PDF text extraction).
func (s *OCRService) findPDFTextScript() string {
	locations := []string{
		"scripts/pdf_text_cli.py",
		"../scripts/pdf_text_cli.py",
		"/app/scripts/pdf_text_cli.py",
		"./pdf_text_cli.py",
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return ""
}

// checkPythonModule checks if a Python module is available using both python3 and python
func (s *OCRService) checkPythonModule(moduleName string) bool {
	// Try with python3 first
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", "-c", fmt.Sprintf("import %s; print('ok')", moduleName))
	if output, err := cmd.CombinedOutput(); err == nil {
		fmt.Printf("[OCR] %s is available (python3)\n", moduleName)
		return true
	} else {
		fmt.Printf("[OCR] %s check failed (python3): %v, output: %s\n", moduleName, err, string(output))
	}

	// Try with python
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	cmd = exec.CommandContext(ctx2, "python", "-c", fmt.Sprintf("import %s; print('ok')", moduleName))
	if output, err := cmd.CombinedOutput(); err == nil {
		fmt.Printf("[OCR] %s is available (python)\n", moduleName)
		return true
	} else {
		fmt.Printf("[OCR] %s check failed (python): %v, output: %s\n", moduleName, err, string(output))
	}

	return false
}

// isRapidOCRAvailable checks if RapidOCR is available (Python module).
func (s *OCRService) isRapidOCRAvailable() bool {
	// Check if script exists
	scriptPath := s.findPaddleOCRScript()
	if scriptPath == "" {
		fmt.Printf("[OCR] paddleocr_cli.py script not found\n")
		return false
	}

	// RapidOCR v3 requires both rapidocr and onnxruntime.
	if s.checkPythonModule("rapidocr") && s.checkPythonModule("onnxruntime") {
		return true
	}
	fmt.Printf("[OCR] RapidOCR v3 is not available\n")
	return false
}

// RecognizePaymentScreenshot performs OCR for payment screenshots (RapidOCR v3 only).
func (s *OCRService) RecognizePaymentScreenshot(imagePath string) (string, error) {
	fmt.Printf("[OCR] Starting payment screenshot recognition for: %s\n", imagePath)

	if !s.isRapidOCRAvailable() {
		engine := getOCREngine()
		return "", fmt.Errorf("OCR engine is not available (%s: %s)", engine, ocrEngineInstallHint(engine))
	}

	text, err := s.RecognizeWithRapidOCR(imagePath)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("RapidOCR returned empty text")
	}
	return text, nil
}

// isGarbledText checks if extracted text contains mostly garbled/unrecognizable characters
func (s *OCRService) isGarbledText(text string) bool {
	if text == "" {
		return true
	}

	// Count valid characters (Chinese, English, digits)
	// We're strict about what we consider valid to catch garbled text
	validChars := 0
	totalChars := 0

	for _, r := range text {
		// Skip whitespace in the count
		if unicode.IsSpace(r) {
			continue
		}

		totalChars++

		// Only count clearly valid characters: Chinese, letters, and digits
		// Common punctuation like ￥¥@#$% are also considered valid
		if unicode.Is(unicode.Han, r) || // Chinese characters
			(unicode.IsLetter(r) && r < 128) || // ASCII letters only (not garbage high unicode)
			unicode.IsDigit(r) || // Numbers
			r == '，' || r == '。' || r == '、' || r == '：' || r == '；' || // Chinese punctuation
			r == '\u201c' || r == '\u201d' || r == '\u2018' || r == '\u2019' || // Chinese quotes (using unicode escape)
			r == '（' || r == '）' || r == '【' || r == '】' || // Chinese brackets
			r == '￥' || r == '¥' || r == '@' || r == '#' || r == '$' || r == '%' || // Symbols
			r == '&' || r == '*' || r == '+' || r == '-' || r == '=' || r == '/' { // Math symbols
			validChars++
		}
	}

	if totalChars == 0 {
		return true
	}

	// If valid character ratio is less than 50%, consider it garbled
	validRatio := float64(validChars) / float64(totalChars)
	fmt.Printf("[OCR] Text validity check: %d/%d valid chars (%.2f%%)\n", validChars, totalChars, validRatio*100)

	return validRatio < 0.5
}

type PDFTextCLIResponse struct {
	Success   bool   `json:"success"`
	Text      string `json:"text"`
	PageCount int    `json:"page_count,omitempty"`
	Extractor string `json:"extractor,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (s *OCRService) extractTextWithPyMuPDF(pdfPath string) (string, error) {
	fmt.Printf("[OCR] Attempting PDF text extraction with PyMuPDF: %s\n", pdfPath)

	scriptPath := s.findPDFTextScript()
	if scriptPath == "" {
		return "", fmt.Errorf("pdf_text_cli.py script not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	run := func(python string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, python, scriptPath, pdfPath)
		return cmd.CombinedOutput()
	}

	output, execErr := run("python3")
	if execErr != nil {
		if altOut, altErr := run("python"); altErr == nil || len(altOut) > 0 {
			output = altOut
			execErr = altErr
		}
	}

	var result PDFTextCLIResponse
	if err := unmarshalPossiblyNoisyJSON(output, &result); err != nil {
		if execErr != nil {
			return "", fmt.Errorf("failed to execute PyMuPDF CLI: %w (output: %s)", execErr, string(output))
		}
		return "", fmt.Errorf("failed to parse PyMuPDF CLI output: %w (output: %s)", err, string(output))
	}

	if !result.Success {
		return "", fmt.Errorf("PyMuPDF error: %s", result.Error)
	}

	fmt.Printf("[OCR] PyMuPDF extracted %d characters from %d pages (%s)\n", len(result.Text), result.PageCount, result.Extractor)
	return result.Text, nil
}

func (s *OCRService) isLikelyUsefulInvoicePDFText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if s.isGarbledText(text) {
		return false
	}

	minChars := 160
	if v := strings.TrimSpace(os.Getenv("SBM_PDF_TEXT_MIN_CHARS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			minChars = n
		}
	}

	minHanRatio := 0.01
	if v := strings.TrimSpace(os.Getenv("SBM_PDF_TEXT_MIN_HAN_RATIO")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			minHanRatio = f
		}
	}

	if len([]rune(text)) >= minChars && s.getChineseCharRatio(text) >= minHanRatio {
		return true
	}

	keywords := []string{
		"发票代码", "发票号码", "开票日期", "校验码", "价税合计", "合计金额", "购买方", "销售方",
	}
	for _, k := range keywords {
		if strings.Contains(text, k) {
			return true
		}
	}

	return false
}

// extractTextWithPdftotext uses poppler's pdftotext for better CID font support
func (s *OCRService) extractTextWithPdftotext(pdfPath string) (string, error) {
	fmt.Printf("[OCR] Attempting text extraction with pdftotext: %s\n", pdfPath)

	// Check if pdftotext is available
	_, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", fmt.Errorf("pdftotext not found in PATH: %w", err)
	}

	// Validate the PDF file exists before running the command
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to access PDF file: %w", err)
	}
	if !fileInfo.Mode().IsRegular() {
		return "", fmt.Errorf("PDF path is not a regular file")
	}

	// Run pdftotext with -layout flag to preserve layout
	// Output to stdout using "-" as output file
	// Note: exec.Command properly escapes arguments, preventing command injection
	cmd := exec.Command("pdftotext", "-layout", "-enc", "UTF-8", pdfPath, "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		// Include stderr in error message for better diagnostics
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return "", fmt.Errorf("pdftotext execution failed: %w (stderr: %s)", err, stderrStr)
		}
		return "", fmt.Errorf("pdftotext execution failed: %w", err)
	}

	text := string(output)
	fmt.Printf("[OCR] pdftotext extracted %d characters\n", len(text))
	return text, nil
}

// RecognizePDF extracts invoice text from PDF with PyMuPDF as a fast pre-step, falling back to RapidOCR.
func (s *OCRService) RecognizePDF(pdfPath string) (string, error) {
	fmt.Printf("[OCR] Starting PDF recognition for: %s\n", pdfPath)

	if strings.TrimSpace(pdfPath) == "" {
		return "", fmt.Errorf("empty PDF path")
	}

	// Validate PDF file exists and is a regular file
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to access PDF file: %w", err)
	}
	if !fileInfo.Mode().IsRegular() {
		return "", fmt.Errorf("PDF path is not a regular file")
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_PDF_TEXT_EXTRACTOR")))
	if mode == "" {
		mode = "pymupdf"
	}

	if mode != "off" && mode != "false" && mode != "0" {
		if text, err := s.extractTextWithPyMuPDF(pdfPath); err == nil {
			if s.isLikelyUsefulInvoicePDFText(text) {
				return text, nil
			}
			fmt.Printf("[OCR] PyMuPDF text looks incomplete; falling back to RapidOCR image OCR\n")
		} else {
			fmt.Printf("[OCR] PyMuPDF extraction failed; falling back to RapidOCR image OCR: %v\n", err)
		}
	}

	fmt.Printf("[OCR] Using OCR CLI for PDF pages\n")
	return s.pdfToImageOCR(pdfPath)
}

// getChineseCharRatio calculates the ratio of Chinese characters in the text
func (s *OCRService) getChineseCharRatio(text string) float64 {
	if text == "" {
		return 0
	}

	totalChars := 0
	chineseChars := 0

	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		totalChars++
		if unicode.Is(unicode.Han, r) {
			chineseChars++
		}
	}

	if totalChars == 0 {
		return 0
	}
	return float64(chineseChars) / float64(totalChars)
}

// mergeExtractionResults combines pdftotext and OCR results
func (s *OCRService) mergeExtractionResults(pdftotextResult, ocrResult string) string {
	// Strategy:
	// 1. pdftotext is better for: numbers, dates, amounts, tax IDs
	// 2. OCR is better for: Chinese characters, text labels

	// If OCR result has more Chinese content, prefer OCR as base
	ocrChineseRatio := s.getChineseCharRatio(ocrResult)
	pdfChineseRatio := s.getChineseCharRatio(pdftotextResult)

	fmt.Printf("[OCR] Merge: pdftotext Chinese ratio: %.2f%%, OCR Chinese ratio: %.2f%%\n",
		pdfChineseRatio*100, ocrChineseRatio*100)

	// Use OCR as base if it has more Chinese content
	if ocrChineseRatio > pdfChineseRatio {
		// OCR result contains more Chinese text, which is what we need
		// The structured data extraction from pdftotext could be used
		// for validation/correction in future enhancements
		return ocrResult
	}

	// Otherwise, use pdftotext result (it already has good Chinese content)
	return pdftotextResult
}

// extractAmounts finds monetary amounts in text
func (s *OCRService) extractAmounts(text string) []string {
	// Match amounts with ¥ or ￥ symbol, supporting:
	// - Amounts with or without decimals: ¥100 or ¥100.00
	// - Amounts with commas: ¥1,234.56
	// - Amounts must have at least one digit
	return amountRegex.FindAllString(text, -1)
}

// extractTaxIDs finds Chinese unified social credit codes
// Note: This pattern is intentionally broad to catch variations in OCR output
// Real validation should be done in the parsing layer with checksum verification
func (s *OCRService) extractTaxIDs(text string) []string {
	return taxIDRegex.FindAllString(text, -1)
}

// extractDates finds date patterns
func (s *OCRService) extractDates(text string) []string {
	var dates []string
	for _, re := range datePatterns {
		dates = append(dates, re.FindAllString(text, -1)...)
	}
	return dates
}

// pdfToImageOCR converts PDF pages to images and performs OCR
func (s *OCRService) pdfToImageOCR(pdfPath string) (string, error) {
	fmt.Printf("[OCR] Converting PDF to images for OCR: %s\n", pdfPath)

	if !s.isRapidOCRAvailable() {
		engine := getOCREngine()
		return "", fmt.Errorf("OCR engine is not available (%s: %s)", engine, ocrEngineInstallHint(engine))
	}

	// Check if pdftoppm is available
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return "", fmt.Errorf("pdftoppm not found in PATH: %w", err)
	}

	// Validate PDF file exists and is a regular file
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to access PDF file: %w", err)
	}
	if !fileInfo.Mode().IsRegular() {
		return "", fmt.Errorf("PDF path is not a regular file")
	}

	// Create temporary directory for images
	tempDir, err := os.MkdirTemp("", "pdf-ocr-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Use pdftoppm to convert PDF to PNG images
	// pdftoppm -png -r 300 input.pdf outputPrefix
	// Note: exec.Command properly escapes arguments, preventing shell injection
	// pdftoppm outputs files with pattern: outputPrefix-N.png where N is page number
	outputPrefix := filepath.Join(tempDir, "page")
	// Use grayscale output to improve OCR recall on colored text (common in invoices).
	cmd := exec.Command("pdftoppm", "-png", "-gray", "-r", strconv.Itoa(getPDFOCRDPI()), pdfPath, outputPrefix)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to convert PDF to images with pdftoppm: %w (output: %s)", err, string(output))
	}

	// Find generated image files
	files, err := filepath.Glob(filepath.Join(tempDir, "page-*.png"))
	if err != nil {
		return "", fmt.Errorf("failed to glob image files: %w", err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no images generated from PDF")
	}

	// Sort files to ensure page order
	sort.Strings(files)

	fmt.Printf("[OCR] PDF converted to %d images\n", len(files))

	var allText strings.Builder

	// Process each image
	for i, imgPath := range files {
		fmt.Printf("[OCR] Processing page %d/%d\n", i+1, len(files))

		qrInjected, qrHasHeader := s.injectInvoiceFieldsFromQRCode(imgPath)

		var parts []string
		if qrInjected != "" {
			parts = append(parts, qrInjected)
		}

		text, err := s.RecognizeWithRapidOCRProfile(imgPath, "pdf")
		if err != nil {
			fmt.Printf("[OCR] RapidOCR v3 failed for page %d: %v\n", i+1, err)
			continue
		}
		if strings.TrimSpace(text) == "" {
			fmt.Printf("[OCR] RapidOCR v3 returned empty text for page %d\n", i+1)
			continue
		}

		if len(parts) > 0 {
			text = strings.Join(append(parts, text), "\n")
		}

		// If key header fields are missing (invoice code/number/date), run a second pass on the
		// top-right region with binarization. This improves recall for small, colored text.
		if !qrHasHeader && (!strings.Contains(text, "发票代码") || !strings.Contains(text, "发票号码") || !strings.Contains(text, "开票日期")) {
			if extra, extraErr := s.ocrInvoiceTopRightRegion(tempDir, imgPath); extraErr == nil && strings.TrimSpace(extra) != "" {
				fmt.Printf("[OCR] Extra header OCR extracted %d characters from page %d\n", len(extra), i+1)
				text = text + "\n" + extra
			} else if extraErr != nil {
				fmt.Printf("[OCR] Extra header OCR failed for page %d: %v\n", i+1, extraErr)
			}
		}

		// Buyer/seller ROI fallback:
		// - true/1/yes: always try ROI injection
		// - auto (default): try ROI only when buyer/seller seems missing from main OCR text
		roiMode := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_INVOICE_PARTY_ROI")))
		if roiMode == "" {
			roiMode = "auto"
		}
		needROI := false
		switch roiMode {
		case "1", "true", "yes":
			needROI = true
		case "auto":
			buyer, seller := s.extractBuyerAndSellerByPosition(text)
			needROI = buyer == nil || seller == nil
		}
		if needROI {
			partyInjected, _, _ := s.injectInvoicePartiesFromRegions(tempDir, imgPath)
			if partyInjected != "" {
				text = partyInjected + "\n" + text
			}
		}

		fmt.Printf("[OCR] Extracted %d characters from page %d\n", len(text), i+1)
		allText.WriteString(text)
		allText.WriteString("\n")
	}

	result := allText.String()
	fmt.Printf("[OCR] Total OCR text extracted: %d characters from %d pages\n", len(result), len(files))

	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("no text could be extracted from PDF images")
	}

	return result, nil
}

func (s *OCRService) injectInvoicePartiesFromRegions(tempDir, imgPath string) (injected string, buyerOK bool, sellerOK bool) {
	// Heuristic crops (A4 invoice layouts):
	// - Buyer: upper-left block under the QR code
	// - Seller: bottom-left block
	// Widened slightly to better cover “名称/纳税人识别号/地址电话/开户行账号” lines.
	buyerText, err1 := s.ocrInvoiceRegion(tempDir, imgPath, "buyer", 0.02, 0.10, 0.86, 0.55)
	sellerText, err2 := s.ocrInvoiceRegion(tempDir, imgPath, "seller", 0.02, 0.54, 0.90, 0.92)

	if err1 != nil {
		fmt.Printf("[OCR] Buyer ROI OCR failed: %v\n", err1)
	}
	if err2 != nil {
		fmt.Printf("[OCR] Seller ROI OCR failed: %v\n", err2)
	}

	buyerName, buyerTax := extractPartyFromROICandidate(buyerText, "buyer")
	sellerName, sellerTax := extractPartyFromROICandidate(sellerText, "seller")

	var parts []string
	if buyerName != "" {
		parts = append(parts, "购买方名称："+buyerName)
		buyerOK = true
	}
	if buyerTax != "" {
		parts = append(parts, "购买方纳税人识别号："+buyerTax)
	}
	if sellerName != "" {
		parts = append(parts, "销售方名称："+sellerName)
		sellerOK = true
	}
	if sellerTax != "" {
		parts = append(parts, "销售方纳税人识别号："+sellerTax)
	}

	injected = strings.Join(parts, "\n")
	return injected, buyerOK, sellerOK
}

func extractPartyFromROICandidate(text string, role string) (name string, taxID string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}

	// Tax ID: prefer unified social credit code if present.
	if m := taxIDRegex.FindString(text); m != "" {
		taxID = m
	}

	// Name: handle patterns like:
	// - 名称: XXX / 名 称 XXX
	// - 购买方名称: XXX / 销售方名称: XXX
	nameRe := regexp.MustCompile(`(?m)^(?:(?:购买方|销售方)\s*)?(?:名\s*称|名称)\s*[:：]?\s*([^\n\r]+)$`)
	if match := nameRe.FindStringSubmatch(text); len(match) > 1 {
		candidate := strings.TrimSpace(match[1])
		// Filter obvious non-names.
		if candidate != "" && !strings.Contains(candidate, "地址") && !strings.Contains(candidate, "电话") && len([]rune(candidate)) <= MaxMerchantNameLength {
			name = candidate
		}
	}

	// Fallback: sometimes "名称" is on its own line and the value is on the next line.
	if name == "" {
		lines := strings.Split(text, "\n")
		for i := 0; i < len(lines); i++ {
			l := strings.TrimSpace(lines[i])
			compact := strings.ReplaceAll(l, " ", "")
			if compact == "名称" || compact == "名称:" || compact == "名称：" ||
				compact == "购买方名称" || compact == "购买方名称:" || compact == "购买方名称：" ||
				compact == "销售方名称" || compact == "销售方名称:" || compact == "销售方名称：" ||
				compact == "名" || compact == "称" {
				if i+1 < len(lines) {
					candidate := strings.TrimSpace(lines[i+1])
					if candidate != "" && !strings.Contains(candidate, "地址") && !strings.Contains(candidate, "电话") && len([]rune(candidate)) <= MaxMerchantNameLength {
						name = candidate
						break
					}
				}
			}
		}
	}

	// Heuristic fallback: pick the best Chinese-looking line(s) as name.
	if name == "" {
		name = pickBestPartyNameHeuristic(text, role)
	}

	return name, taxID
}

func pickBestPartyNameHeuristic(text string, role string) string {
	lines := make([]string, 0, 32)
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		lines = append(lines, l)
	}

	if len(lines) == 0 {
		return ""
	}

	badContains := []string{
		"国家税务总局", "税务局", "密码区", "校验码", "发票代码", "发票号码", "开票日期",
		"地址", "电话", "开户行", "账号", "纳税人识别号", "统一社会信用代码",
	}

	sellerBonus := []string{"公司", "有限", "集团", "商贸", "商业", "零售", "超市", "沃尔玛", "门店"}
	buyerBonus := []string{"先生", "女士", "个人"}

	containsAny := func(s string, arr []string) bool {
		for _, k := range arr {
			if strings.Contains(s, k) {
				return true
			}
		}
		return false
	}

	countHan := func(s string) int {
		n := 0
		for _, r := range s {
			if unicode.Is(unicode.Han, r) {
				n++
			}
		}
		return n
	}

	cleanLine := func(s string) string {
		s = strings.TrimSpace(s)
		// Remove leading labels like “购买方/销售方”.
		s = regexp.MustCompile(`^(购买方|销售方)\s*`).ReplaceAllString(s, "")
		s = regexp.MustCompile(`^(名\s*称|名称)\s*`).ReplaceAllString(s, "")
		s = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, ":"), "："))
		s = strings.TrimSpace(strings.Trim(s, "（）()【】[]<>《》“”\"'"))
		return strings.TrimSpace(s)
	}

	best := ""
	bestScore := -1

	scoreCandidate := func(s string) int {
		s = cleanLine(s)
		if s == "" {
			return -1
		}
		if containsAny(s, badContains) {
			return -1
		}
		han := countHan(s)
		if han < 2 {
			return -1
		}
		// Avoid “名称/纳税人识别号” itself.
		if s == "名称" || s == "名" || s == "称" {
			return -1
		}
		score := han * 10
		// Penalize too many digits/symbols.
		digits := 0
		for _, r := range s {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		score -= digits * 5
		if len([]rune(s)) > MaxMerchantNameLength {
			score -= 30
		}
		if role == "seller" && containsAny(s, sellerBonus) {
			score += 40
		}
		if role == "buyer" && containsAny(s, buyerBonus) {
			score += 30
		}
		// Penalize overly-short seller names without typical company/store keywords.
		if role == "seller" && han < 4 && !containsAny(s, sellerBonus) {
			score -= 20
		}
		// Buyer names are often short; seller names usually longer.
		if role == "buyer" && han <= 6 {
			score += 15
		}
		return score
	}

	// Evaluate single lines.
	for _, l := range lines {
		if sc := scoreCandidate(l); sc > bestScore {
			bestScore = sc
			best = cleanLine(l)
		}
	}

	// Evaluate concatenation of up to 3 consecutive lines (handles broken long company names).
	for i := 0; i < len(lines); i++ {
		joined := ""
		for j := i; j < len(lines) && j < i+3; j++ {
			part := cleanLine(lines[j])
			if part == "" || containsAny(part, badContains) {
				break
			}
			// If the part is mostly non-Chinese and very short, stop concatenating.
			if countHan(part) == 0 && len([]rune(part)) <= 2 {
				break
			}
			if joined == "" {
				joined = part
			} else {
				joined = joined + part
			}
			if sc := scoreCandidate(joined); sc > bestScore {
				bestScore = sc
				best = joined
			}
		}
	}

	// Extra buyer fallback: look for “X先生/女士”.
	if role == "buyer" && best != "" && !containsAny(best, buyerBonus) {
		re := regexp.MustCompile(`([\p{Han}]{1,4}(先生|女士))`)
		if m := re.FindStringSubmatch(text); len(m) > 1 {
			return m[1]
		}
	}

	return best
}

func (s *OCRService) ocrInvoiceRegion(tempDir, imgPath, tag string, x0p, y0p, x1p, y1p float64) (string, error) {
	f, err := os.Open(imgPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return "", fmt.Errorf("invalid image bounds")
	}

	x0 := b.Min.X + int(float64(w)*x0p)
	y0 := b.Min.Y + int(float64(h)*y0p)
	x1 := b.Min.X + int(float64(w)*x1p)
	y1 := b.Min.Y + int(float64(h)*y1p)

	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	if x1-x0 < 50 || y1-y0 < 50 {
		return "", fmt.Errorf("crop region too small")
	}

	rect := image.Rect(x0, y0, x1, y1)
	bin, err := binarizeRegion(src, rect)
	if err != nil {
		return "", err
	}

	// Upscale ROI a bit for small fonts; cheap and improves recall.
	if bin.Bounds().Dx() < 1300 && bin.Bounds().Dy() < 900 {
		bin = scaleGrayNearest(bin, 2, 1800, 1200)
	}

	outPath := filepath.Join(tempDir, fmt.Sprintf("roi-%s-%s.png", tag, filepath.Base(imgPath)))
	of, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	if err := png.Encode(of, bin); err != nil {
		_ = of.Close()
		return "", err
	}
	_ = of.Close()

	// Lower thresholds for small text in ROI.
	return s.recognizeWithRapidOCRArgs(outPath, []string{"--profile", "pdf", "--min-height", "5", "--text-score", "0.25"})
}

func scaleGrayNearest(src *image.Gray, scale int, maxW, maxH int) *image.Gray {
	if scale <= 1 {
		return src
	}
	w := src.Bounds().Dx() * scale
	h := src.Bounds().Dy() * scale
	if w > maxW || h > maxH {
		// Compute a smaller scale that still upscales a bit.
		scale = 2
		w = src.Bounds().Dx() * scale
		h = src.Bounds().Dy() * scale
		if w > maxW || h > maxH {
			return src
		}
	}

	dst := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := y / scale
		for x := 0; x < w; x++ {
			sx := x / scale
			dst.SetGray(x, y, src.GrayAt(sx, sy))
		}
	}
	return dst
}

func binarizeRegion(src image.Image, rect image.Rectangle) (*image.Gray, error) {
	roi := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			roi.Set(x, y, src.At(rect.Min.X+x, rect.Min.Y+y))
		}
	}

	gray := image.NewGray(roi.Bounds())
	hist := make([]int, 256)
	for y := 0; y < gray.Bounds().Dy(); y++ {
		for x := 0; x < gray.Bounds().Dx(); x++ {
			r, g, b2, _ := roi.At(x, y).RGBA()
			l := uint8((299*r + 587*g + 114*b2 + 500) / 1000 >> 8)
			gray.SetGray(x, y, color.Gray{Y: l})
			hist[int(l)]++
		}
	}

	total := gray.Bounds().Dx() * gray.Bounds().Dy()
	if total <= 0 {
		return nil, fmt.Errorf("empty roi")
	}
	var sum int
	for i := 0; i < 256; i++ {
		sum += i * hist[i]
	}
	var (
		sumB   int
		wB     int
		varMax float64
		thr    int
	)
	for t := 0; t < 256; t++ {
		wB += hist[t]
		if wB == 0 {
			continue
		}
		wF := total - wB
		if wF == 0 {
			break
		}
		sumB += t * hist[t]
		mB := float64(sumB) / float64(wB)
		mF := float64(sum-sumB) / float64(wF)
		v := float64(wB) * float64(wF) * (mB - mF) * (mB - mF)
		if v > varMax {
			varMax = v
			thr = t
		}
	}

	bin := image.NewGray(gray.Bounds())
	for y := 0; y < bin.Bounds().Dy(); y++ {
		for x := 0; x < bin.Bounds().Dx(); x++ {
			if gray.GrayAt(x, y).Y > uint8(thr) {
				bin.SetGray(x, y, color.Gray{Y: 255})
			} else {
				bin.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}
	return bin, nil
}

type invoiceQRFields struct {
	InvoiceCode   string
	InvoiceNumber string
	InvoiceDate   string // YYYY年M月D日
	Amount        string // 123.45
	CheckCode     string // digits
}

func (f invoiceQRFields) hasHeader() bool {
	return f.InvoiceCode != "" || f.InvoiceNumber != "" || f.InvoiceDate != "" || f.Amount != ""
}

func (f invoiceQRFields) injectText() string {
	var parts []string
	if f.InvoiceCode != "" {
		parts = append(parts, "发票代码："+f.InvoiceCode)
	}
	if f.InvoiceNumber != "" {
		parts = append(parts, "发票号码："+f.InvoiceNumber)
	}
	if f.InvoiceDate != "" {
		parts = append(parts, "开票日期："+f.InvoiceDate)
	}
	if f.CheckCode != "" {
		parts = append(parts, "校验码："+f.CheckCode)
	}
	if f.Amount != "" {
		parts = append(parts, "价税合计(小写)："+f.Amount)
	}
	return strings.Join(parts, "\n")
}

func (s *OCRService) injectInvoiceFieldsFromQRCode(imgPath string) (injected string, hasHeader bool) {
	fields, err := s.decodeInvoiceQRCode(imgPath)
	if err != nil || fields == nil || !fields.hasHeader() {
		return "", false
	}
	injected = fields.injectText()
	hasHeader = true
	fmt.Printf("[OCR] QR extracted header fields (code=%s number=%s date=%s amount=%s)\n",
		fields.InvoiceCode, fields.InvoiceNumber, fields.InvoiceDate, fields.Amount)
	return injected, hasHeader
}

func (s *OCRService) decodeInvoiceQRCode(imgPath string) (*invoiceQRFields, error) {
	f, err := os.Open(imgPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}

	codes, err := goqr.Recognize(img)
	if err != nil || len(codes) == 0 {
		return nil, err
	}

	var best invoiceQRFields
	bestScore := 0
	for _, c := range codes {
		payload := strings.TrimSpace(string(c.Payload))
		if payload == "" {
			continue
		}
		parsed := parseInvoiceQRPayload(payload)
		score := 0
		if parsed.InvoiceCode != "" {
			score++
		}
		if parsed.InvoiceNumber != "" {
			score++
		}
		if parsed.InvoiceDate != "" {
			score++
		}
		if parsed.Amount != "" {
			score++
		}
		if parsed.CheckCode != "" {
			score++
		}
		if score > bestScore {
			bestScore = score
			best = parsed
		}
	}

	if bestScore == 0 {
		return nil, nil
	}
	return &best, nil
}

func parseInvoiceQRPayload(payload string) invoiceQRFields {
	fields := invoiceQRFields{}

	tokens := splitInvoiceQRPayload(payload)
	if len(tokens) >= 6 {
		if isDigitsLen(tokens[2], 10, 12) {
			fields.InvoiceCode = onlyDigits(tokens[2])
		}
		if isDigitsLen(tokens[3], 8, 8) {
			fields.InvoiceNumber = onlyDigits(tokens[3])
		}
		if looksLikeAmount(tokens[4]) {
			fields.Amount = normalizeAmount(tokens[4])
		}
		if isDigitsLen(tokens[5], 8, 8) && strings.HasPrefix(onlyDigits(tokens[5]), "20") {
			fields.InvoiceDate = formatDateYYYYMMDD(onlyDigits(tokens[5]))
		}
		if len(tokens) >= 7 && isDigitsLen(tokens[6], 16, 24) {
			fields.CheckCode = onlyDigits(tokens[6])
		}
	}

	allDigits := regexp.MustCompile(`\d+`).FindAllString(payload, -1)
	for _, d := range allDigits {
		if fields.CheckCode == "" && len(d) == 20 {
			fields.CheckCode = d
			continue
		}
		if fields.InvoiceDate == "" && len(d) == 8 && strings.HasPrefix(d, "20") {
			fields.InvoiceDate = formatDateYYYYMMDD(d)
			continue
		}
		if fields.InvoiceNumber == "" && len(d) == 8 && !strings.HasPrefix(d, "20") {
			fields.InvoiceNumber = d
			continue
		}
		if fields.InvoiceCode == "" && (len(d) == 10 || len(d) == 12) && !strings.HasPrefix(d, "20") {
			fields.InvoiceCode = d
			continue
		}
	}

	if fields.Amount == "" {
		amt := regexp.MustCompile(`\d+\.\d{2}`).FindString(payload)
		if amt != "" {
			fields.Amount = normalizeAmount(amt)
		}
	}

	return fields
}

func splitInvoiceQRPayload(payload string) []string {
	payload = strings.TrimSpace(payload)
	payload = strings.TrimPrefix(payload, "\ufeff")
	sep := ","
	if strings.Count(payload, ",") == 0 && strings.Count(payload, "|") > 0 {
		sep = "|"
	} else if strings.Count(payload, ",") == 0 && strings.Count(payload, ";") > 0 {
		sep = ";"
	}
	raw := strings.Split(payload, sep)
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		out = append(out, x)
	}
	return out
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isDigitsLen(s string, min, max int) bool {
	s = onlyDigits(s)
	return len(s) >= min && len(s) <= max
}

func looksLikeAmount(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "¥")
	s = strings.TrimPrefix(s, "￥")
	s = strings.ReplaceAll(s, ",", "")
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func normalizeAmount(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "¥")
	s = strings.TrimPrefix(s, "￥")
	s = strings.ReplaceAll(s, ",", "")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%.2f", f)
}

func formatDateYYYYMMDD(s string) string {
	if len(s) != 8 {
		return s
	}
	y, _ := strconv.Atoi(s[:4])
	m, _ := strconv.Atoi(s[4:6])
	d, _ := strconv.Atoi(s[6:8])
	if y == 0 || m == 0 || d == 0 {
		return s
	}
	return fmt.Sprintf("%d年%d月%d日", y, m, d)
}

func (s *OCRService) ocrInvoiceTopRightRegion(tempDir, imgPath string) (string, error) {
	f, err := os.Open(imgPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return "", fmt.Errorf("invalid image bounds")
	}

	// Heuristic crop: invoice header block is on the top-right of A4 invoices.
	x0 := b.Min.X + int(float64(w)*0.55)
	y0 := b.Min.Y + int(float64(h)*0.02)
	x1 := b.Min.X + w
	y1 := b.Min.Y + int(float64(h)*0.30)
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	if x1-x0 < 50 || y1-y0 < 50 {
		return "", fmt.Errorf("crop region too small")
	}

	rect := image.Rect(x0, y0, x1, y1)

	// Copy into a concrete image so SubImage implementations don't surprise us.
	roi := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			roi.Set(x, y, src.At(rect.Min.X+x, rect.Min.Y+y))
		}
	}

	// Convert to grayscale.
	gray := image.NewGray(roi.Bounds())
	hist := make([]int, 256)
	for y := 0; y < gray.Bounds().Dy(); y++ {
		for x := 0; x < gray.Bounds().Dx(); x++ {
			r, g, b2, _ := roi.At(x, y).RGBA()
			// Convert to 8-bit luma.
			l := uint8((299*r + 587*g + 114*b2 + 500) / 1000 >> 8)
			gray.SetGray(x, y, color.Gray{Y: l})
			hist[int(l)]++
		}
	}

	// Otsu binarization threshold.
	total := gray.Bounds().Dx() * gray.Bounds().Dy()
	if total <= 0 {
		return "", fmt.Errorf("empty roi")
	}
	var sum int
	for i := 0; i < 256; i++ {
		sum += i * hist[i]
	}
	var (
		sumB   int
		wB     int
		varMax float64
		thr    int
	)
	for t := 0; t < 256; t++ {
		wB += hist[t]
		if wB == 0 {
			continue
		}
		wF := total - wB
		if wF == 0 {
			break
		}
		sumB += t * hist[t]
		mB := float64(sumB) / float64(wB)
		mF := float64(sum-sumB) / float64(wF)
		v := float64(wB) * float64(wF) * (mB - mF) * (mB - mF)
		if v > varMax {
			varMax = v
			thr = t
		}
	}

	// Produce binarized image (text -> black).
	bin := image.NewGray(gray.Bounds())
	for y := 0; y < bin.Bounds().Dy(); y++ {
		for x := 0; x < bin.Bounds().Dx(); x++ {
			if gray.GrayAt(x, y).Y > uint8(thr) {
				bin.SetGray(x, y, color.Gray{Y: 255})
			} else {
				bin.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}

	outPath := filepath.Join(tempDir, fmt.Sprintf("roi-tr-%s.png", filepath.Base(imgPath)))
	of, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	if err := png.Encode(of, bin); err != nil {
		_ = of.Close()
		return "", err
	}
	_ = of.Close()

	// Lower thresholds a bit for this ROI.
	return s.recognizeWithRapidOCRArgs(outPath, []string{"--profile", "pdf", "--min-height", "5", "--text-score", "0.25"})
}

// removeChineseSpaces removes spaces between Chinese characters in OCR text
// This helps normalize text like "支 付 时 间" to "支付时间"
// Also handles spaces between numbers and Chinese date units like "2025 年 10 月 23 日"
func removeChineseSpaces(text string) string {
	var result strings.Builder
	runes := []rune(text)

	i := 0
	for i < len(runes) {
		r := runes[i]

		// If not a space, directly add
		if !unicode.IsSpace(r) {
			result.WriteRune(r)
			i++
			continue
		}

		// Is a space, check previous and next characters
		// Find previous non-space character
		prevIdx := i - 1
		for prevIdx >= 0 && unicode.IsSpace(runes[prevIdx]) {
			prevIdx--
		}

		// Find next non-space character
		nextIdx := i + 1
		for nextIdx < len(runes) && unicode.IsSpace(runes[nextIdx]) {
			nextIdx++
		}

		// Determine if we should skip this space
		skipSpace := false
		if prevIdx >= 0 && nextIdx < len(runes) {
			prev := runes[prevIdx]
			next := runes[nextIdx]

			// Skip space if both neighbors are Chinese characters
			if unicode.Is(unicode.Han, prev) && unicode.Is(unicode.Han, next) {
				skipSpace = true
			}
			// Skip space between Chinese and digits (e.g. "支付时间 2025")
			// BUT preserve space after '日' when followed by a digit (likely time)
			if prev != '日' && unicode.Is(unicode.Han, prev) && unicode.IsDigit(next) {
				skipSpace = true
			}
			// Skip space if previous is digit and next is date unit (年/月/日)
			if unicode.IsDigit(prev) && (next == '年' || next == '月' || next == '日' || next == '时' || next == '分' || next == '秒') {
				skipSpace = true
			}
			// Skip space between digits and Chinese (e.g. "1700 元")
			if unicode.IsDigit(prev) && unicode.Is(unicode.Han, next) {
				skipSpace = true
			}
			// Skip space if previous is date unit (年/月) and next is digit
			// BUT preserve space after '日' when followed by a digit (likely time)
			if (prev == '年' || prev == '月') && unicode.IsDigit(next) {
				skipSpace = true
			}
		}

		if !skipSpace {
			result.WriteRune(r)
		}

		i++
	}

	return result.String()
}

// convertChineseDateToISO converts Chinese date format to ISO format
// Example: "2025年10月23日 14:59:46" -> "2025-10-23 14:59:46"
// Example: "2025年10月23日14:59:46" -> "2025-10-23 14:59:46"
// Example: "2025年10月23日" -> "2025-10-23"
func convertChineseDateToISO(dateStr string) string {
	// If 日 is directly followed by a digit (time), insert a space
	// This handles cases like "2025年10月23日14:59:46" -> "2025年10月23日 14:59:46"
	dateStr = chineseDateTimePattern.ReplaceAllString(dateStr, "日 $1")

	// Replace Chinese date separators with dashes
	dateStr = strings.ReplaceAll(dateStr, "年", "-")
	dateStr = strings.ReplaceAll(dateStr, "月", "-")
	dateStr = strings.ReplaceAll(dateStr, "日", "")
	return strings.TrimSpace(dateStr)
}

// extractTimeFromMatch extracts time string from regex match groups
// Handles both single capture group and separate date/time capture groups
func extractTimeFromMatch(match []string) string {
	var timeStr string
	if len(match) > 2 && match[2] != "" {
		// Date and time captured separately, join with space
		timeStr = match[1] + " " + match[2]
	} else {
		timeStr = match[1]
	}
	return convertChineseDateToISO(timeStr)
}

// ParsePaymentScreenshot extracts payment information from OCR text
func (s *OCRService) ParsePaymentScreenshot(text string) (*PaymentExtractedData, error) {
	data := &PaymentExtractedData{
		RawText: text,
	}

	// Preprocess: remove spaces between Chinese characters
	// This normalizes OCR text like "支 付 时 间" to "支付时间"
	text = removeChineseSpaces(text)

	// Trim leading/trailing whitespace
	text = strings.TrimSpace(text)

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

	// Generic merchant extraction if not found
	if data.Merchant == nil {
		s.extractGenericMerchant(text, data)
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
	// Extract amount with support for negative numbers and large amounts (4+ digits)
	// Priority: negative numbers > amounts with ¥ symbol > large amounts (4+ digits)
	amountRegexes := []*regexp.Regexp{
		// Negative amount with optional currency symbol: -1700.00 or -¥1700.00
		negativeAmountRegex,
		// Standard format with currency symbol: ¥123.45
		regexp.MustCompile(`[¥￥][\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
		// Large amounts (4+ digits with decimals): 1700.00
		regexp.MustCompile(`([\d]{4,}\.[\d]{2})`),
		regexp.MustCompile(`金额[：:]?[\s]*[¥￥]?[\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
		regexp.MustCompile(`支付金额[：:]?[\s]*[¥￥]?[\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
		regexp.MustCompile(`转账金额[：:]?[\s]*[¥￥]?[\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
	}
	for _, re := range amountRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil && *amount >= MinValidAmount {
				data.Amount = amount
				break
			}
		}
	}

	// Extract merchant/receiver with additional patterns
	// Priority: 商品 (short name) > 收款方/收款人 > 商户全称 (full company name)
	merchantRegexes := []*regexp.Regexp{
		// Highest priority: short merchant name after "商品"
		regexp.MustCompile(`商品[：:]?[\s]*([^\s(（\n]+)`),
		regexp.MustCompile(`收款方[：:]?[\s]*([^\s¥￥\n]+)`),
		regexp.MustCompile(`收款人[：:]?[\s]*([^\s¥￥\n]+)`),
		regexp.MustCompile(`转账给([^\s¥￥\n]+)`),
		// Lower priority: full merchant name
		merchantFullNameRegex,
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

	// Extract transaction time with support for various formats
	timeRegexes := []*regexp.Regexp{
		// Standard format: 2024-01-01 12:00:00
		regexp.MustCompile(`支付时间[：:]?[\s]*([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`转账时间[：:]?[\s]*([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		// Chinese format with optional spaces after 日: matches "2025年10月23日14:59:46" and "2025年10月23日 14:59:46"
		regexp.MustCompile(`支付时间[：:]?[\s]*([\d]{4}年[\d]{1,2}月[\d]{1,2}日\s*[\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Generic Chinese date-time format with space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)\s+([\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Generic Chinese date-time format without space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)([\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Date only format
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)`),
	}
	for _, re := range timeRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			timeStr := extractTimeFromMatch(match)
			data.TransactionTime = &timeStr
			break
		}
	}

	// Extract order number - handle both transaction and merchant order numbers
	orderRegexes := []*regexp.Regexp{
		regexp.MustCompile(`交易单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`商户单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`订单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`流水号[：:]?[\s]*([\d]+)`),
	}
	for _, re := range orderRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			orderNum := match[1]
			data.OrderNumber = &orderNum
			break
		}
	}

	// Extract actual payment method from text
	paymentMethodRegexes := []*regexp.Regexp{
		regexp.MustCompile(`支付方式[：:]?[\s]*([^\n]+?)(?:\s*由|$)`),
	}
	for _, re := range paymentMethodRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			method := strings.TrimSpace(match[1])
			if method != "" {
				data.PaymentMethod = &method
				break
			}
		}
	}
	// If no specific payment method found, use default
	if data.PaymentMethod == nil {
		data.PaymentMethod = inferPaymentMethodFromText(text)
	}
}

// parseAlipay extracts Alipay information
func (s *OCRService) parseAlipay(text string, data *PaymentExtractedData) {
	// Extract amount with support for negative numbers and large amounts
	amountRegexes := []*regexp.Regexp{
		// Negative amount with optional currency symbol
		negativeAmountRegex,
		regexp.MustCompile(`[¥￥][\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
		// Large amounts (4+ digits with decimals)
		regexp.MustCompile(`([\d]{4,}\.[\d]{2})`),
		regexp.MustCompile(`金额[：:]?[\s]*[¥￥]?[\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
		regexp.MustCompile(`付款金额[：:]?[\s]*[¥￥]?[\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
	}
	for _, re := range amountRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil && *amount >= MinValidAmount {
				data.Amount = amount
				break
			}
		}
	}

	// Extract merchant - prioritize short names
	merchantRegexes := []*regexp.Regexp{
		regexp.MustCompile(`商品[：:]?[\s]*([^\s(（\n]+)`),
		regexp.MustCompile(`商家[：:]?[\s]*([^\s¥￥\n]+)`),
		regexp.MustCompile(`收款方[：:]?[\s]*([^\s¥￥\n]+)`),
		regexp.MustCompile(`付款给([^\s¥￥\n]+)`),
		merchantFullNameRegex,
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
		regexp.MustCompile(`创建时间[：:]?[\s]*([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`付款时间[：:]?[\s]*([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		// Chinese format with space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)\s+([\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Chinese format without space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)([\d]{1,2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)`),
	}
	for _, re := range timeRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			timeStr := extractTimeFromMatch(match)
			data.TransactionTime = &timeStr
			break
		}
	}

	// Extract order number
	orderRegexes := []*regexp.Regexp{
		regexp.MustCompile(`交易单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`商户单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`订单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`交易号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`流水号[：:]?[\s]*([\d]+)`),
	}
	for _, re := range orderRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			orderNum := match[1]
			data.OrderNumber = &orderNum
			break
		}
	}

	// Extract payment method
	paymentMethodRegexes := []*regexp.Regexp{
		regexp.MustCompile(`支付方式[：:]?[\s]*([^\n]+?)(?:\s*由|$)`),
	}
	for _, re := range paymentMethodRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			method := strings.TrimSpace(match[1])
			if method != "" {
				data.PaymentMethod = &method
				break
			}
		}
	}
	// If no specific payment method found, use default
	if data.PaymentMethod == nil {
		data.PaymentMethod = inferPaymentMethodFromText(text)
	}
}

// parseBankTransfer extracts bank transfer information
func (s *OCRService) parseBankTransfer(text string, data *PaymentExtractedData) {
	// Extract amount with support for negative numbers and large amounts
	amountRegexes := []*regexp.Regexp{
		negativeAmountRegex,
		regexp.MustCompile(`[¥￥][\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
		regexp.MustCompile(`([\d]{4,}\.[\d]{2})`),
		regexp.MustCompile(`金额[：:]?[\s]*[¥￥]?[\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
		regexp.MustCompile(`转账金额[：:]?[\s]*[¥￥]?[\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
		regexp.MustCompile(`交易金额[：:]?[\s]*[¥￥]?[\s]*[-−]?[\s]*([\d,]+\.?\d*)`),
	}
	for _, re := range amountRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil && *amount >= MinValidAmount {
				data.Amount = amount
				break
			}
		}
	}

	// Extract receiver
	merchantRegexes := []*regexp.Regexp{
		regexp.MustCompile(`商品[：:]?[\s]*([^\s(（\n]+)`),
		regexp.MustCompile(`收款人[：:]?[\s]*([^\s¥￥\n]+)`),
		regexp.MustCompile(`收款账户[：:]?[\s]*([^\s¥￥\n]+)`),
		merchantFullNameRegex,
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
		regexp.MustCompile(`转账时间[：:]?[\s]*([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`交易时间[：:]?[\s]*([\d]{4}-[\d]{2}-[\d]{2}\s[\d]{2}:[\d]{2}:[\d]{2})`),
		// Chinese format with space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)\s+([\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Chinese format without space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)([\d]{1,2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)`),
	}
	for _, re := range timeRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			timeStr := extractTimeFromMatch(match)
			data.TransactionTime = &timeStr
			break
		}
	}

	// Extract payment method
	paymentMethodRegexes := []*regexp.Regexp{
		regexp.MustCompile(`支付方式[：:]?[\s]*([^\n]+?)(?:\s*由|$)`),
	}
	for _, re := range paymentMethodRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			method := strings.TrimSpace(match[1])
			if method != "" {
				data.PaymentMethod = &method
				break
			}
		}
	}
	// If no specific payment method found, use default
	if data.PaymentMethod == nil {
		data.PaymentMethod = inferPaymentMethodFromText(text)
	}
}

func inferPaymentMethodFromText(text string) *string {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}

	// Try extracting from common labels first.
	labelRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?m)(?:支付方式|付款方式|支付工具|支付渠道|支付类型)\s*[:：]?\s*([^\n\r]+)`),
	}
	for _, re := range labelRegexes {
		if match := re.FindStringSubmatch(t); len(match) > 1 {
			m := strings.TrimSpace(match[1])
			if idx := strings.Index(m, "由"); idx >= 0 {
				m = strings.TrimSpace(m[:idx])
			}
			m = strings.Trim(m, " \t-—:：")
			if m != "" {
				return &m
			}
		}
	}

	// Then infer from keywords (prefer longer keywords first).
	keywords := []string{
		"微信零钱通",
		"微信零钱",
		"微信支付",
		"支付宝余额",
		"余额宝",
		"花呗",
		"借呗",
		"支付宝",
		"云闪付",
		"银联",
		"信用卡",
		"借记卡",
		"银行卡",
		"现金",
		"Apple Pay",
		"Google Pay",
		"PayPal",
	}
	for _, kw := range keywords {
		if strings.Contains(t, kw) {
			m := kw
			return &m
		}
	}
	return nil
}

// extractAmount extracts amount from text using generic patterns
func (s *OCRService) extractAmount(text string, data *PaymentExtractedData) {
	// Try various amount patterns
	patterns := []*regexp.Regexp{
		// Negative amounts: -1700.00
		negativeAmountRegex,
		// Large amounts with decimals (4+ digits): 1700.00
		regexp.MustCompile(`([\d]{4,}\.[\d]{2})`),
		// Amounts with currency symbols
		regexp.MustCompile(`[¥￥][\s]*([\d,]+\.?\d*)`),
		// Amounts followed by 元
		regexp.MustCompile(`([\d,]+\.?\d*)元`),
	}

	for _, re := range patterns {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil && *amount >= MinValidAmount {
				data.Amount = amount
				return
			}
		}
	}
}

// extractGenericMerchant attempts to extract merchant name using generic patterns
func (s *OCRService) extractGenericMerchant(text string, data *PaymentExtractedData) {
	// Try to find merchant names that contain common business suffixes
	if match := merchantGenericRegex.FindStringSubmatch(text); len(match) > 1 {
		merchant := strings.TrimSpace(match[1])
		if merchant != "" && len(merchant) < MaxMerchantNameLength {
			data.Merchant = &merchant
		}
	}
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// cleanupName removes trailing noise from extracted names
func cleanupName(name string) string {
	// Remove common trailing patterns
	// e.g., "上海市虹口区鹏侠百货商店\n售" -> "上海市虹口区鹏侠百货商店"
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ":：")

	// Split by multiple spaces (2 or more) and take the first part
	// This handles cases where the name is followed by other content with significant spacing
	parts := regexp.MustCompile(`\s{2,}`).Split(name, 2)
	if len(parts) > 0 {
		name = parts[0]
	}

	// Remove trailing single characters that are likely markers
	trailingPatterns := []string{"销", "售", "购", "买", "方", "信", "息", "密", "码", "区"}
	for _, pattern := range trailingPatterns {
		name = strings.TrimSuffix(name, pattern)
		name = strings.TrimSpace(name)
	}

	// Remove trailing whitespace and newlines
	name = strings.TrimRight(name, " \t\n\r")

	return name
}

// removeChineseInlineSpaces removes *inline* spaces between Chinese characters in OCR/PDF text,
// but keeps newlines intact (important for invoice section parsing).
func removeChineseInlineSpaces(text string) string {
	var result strings.Builder
	runes := []rune(text)

	isInlineSpace := func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\u3000'
	}

	i := 0
	for i < len(runes) {
		r := runes[i]

		// Preserve newlines as-is for invoice parsing.
		if r == '\n' || r == '\r' {
			result.WriteRune(r)
			i++
			continue
		}

		// Only treat plain/ideographic spaces and tabs as removable inline whitespace.
		if !isInlineSpace(r) {
			result.WriteRune(r)
			i++
			continue
		}

		// Preserve aligned layouts (e.g. left-right buyer/seller columns) by keeping
		// runs of 2+ inline spaces intact.
		runEnd := i + 1
		for runEnd < len(runes) && isInlineSpace(runes[runEnd]) {
			runEnd++
		}
		if runEnd-i >= 2 {
			for ; i < runEnd; i++ {
				result.WriteRune(runes[i])
			}
			continue
		}

		// Single inline space: decide whether to drop it.
		prevIdx := i - 1
		nextIdx := i + 1

		skipSpace := false
		if prevIdx >= 0 && nextIdx < len(runes) {
			prev := runes[prevIdx]
			next := runes[nextIdx]

			// Skip space if both neighbors are Chinese characters
			if unicode.Is(unicode.Han, prev) && unicode.Is(unicode.Han, next) {
				skipSpace = true
			}
			// Skip space between Chinese and digits (e.g. "开票日期 2025")
			if prev != '日' && unicode.Is(unicode.Han, prev) && unicode.IsDigit(next) {
				skipSpace = true
			}
			// Skip space if previous is digit and next is date unit (年/月/日)
			if unicode.IsDigit(prev) && (next == '年' || next == '月' || next == '日' || next == '时' || next == '分' || next == '秒') {
				skipSpace = true
			}
			// Skip space between digits and Chinese (e.g. "1700 元")
			if unicode.IsDigit(prev) && unicode.Is(unicode.Han, next) {
				skipSpace = true
			}
			// Skip space if previous is date unit (年/月) and next is digit
			if prev != '日' && (prev == '年' || prev == '月') && unicode.IsDigit(next) {
				skipSpace = true
			}
		}

		if !skipSpace {
			result.WriteRune(r)
		}
		i++
	}

	return result.String()
}

func isBadPartyNameCandidate(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	// Common label fragments that sometimes get captured as a "name".
	bad := []string{
		"名称", "名称:", "名称：",
		"项目名称", "货物或应税劳务、服务名称",
		"购买方", "销售方", "购买", "销售",
		"纳税人识别号", "纳税人识别号:", "纳税人识别号：",
		"地址", "地址、电话", "电话",
		"开户行", "开户行及账号", "账号",
	}
	for _, b := range bad {
		if name == b {
			return true
		}
	}
	if strings.HasSuffix(name, "识别号:") || strings.HasSuffix(name, "识别号：") {
		return true
	}
	if strings.Contains(name, "小写") || strings.Contains(name, "大写") {
		return true
	}
	if strings.Contains(name, "纳税人识别号") || strings.Contains(name, "统一社会信用代码") || strings.Contains(name, "地址") || strings.Contains(name, "开户行") {
		return true
	}
	// Dates are not party names; avoid confusing invoice date with seller/buyer.
	if regexp.MustCompile(`\d{4}年\d{1,2}月\d{1,2}日`).MatchString(name) {
		return true
	}
	return false
}

func normalizeInvoiceTextForParsing(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = removeChineseInlineSpaces(text)

	// Normalize "vertical" or whitespace-separated section markers often found in PDF text extraction.
	replacements := []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`购\s*买\s*方`), "购买方"},
		{regexp.MustCompile(`销\s*售\s*方`), "销售方"},
		{regexp.MustCompile(`价\s*税\s*合\s*计`), "价税合计"},
		{regexp.MustCompile(`开\s*票\s*日\s*期`), "开票日期"},
		{regexp.MustCompile(`发\s*票\s*代\s*码`), "发票代码"},
		{regexp.MustCompile(`发\s*票\s*号\s*码`), "发票号码"},
		{regexp.MustCompile(`校\s*验\s*码`), "校验码"},
		{regexp.MustCompile(`纳\s*税\s*人\s*识\s*别\s*号`), "纳税人识别号"},
		{regexp.MustCompile(`名\s*称`), "名称"},
	}
	for _, r := range replacements {
		text = r.re.ReplaceAllString(text, r.repl)
	}

	// Normalize currency symbols / mojibake variants to the full-width RMB symbol (U+FFE5).
	// - U+00A5: '¥' (Yen sign; common in PDF text)
	// - "\u00c2\u00a5": "Â¥" (UTF-8 bytes mis-decoded as Latin-1)
	// - "\u00ef\u00bf\u00a5": "ï¿¥" (UTF-8 bytes of U+FFE5 mis-decoded as Latin-1)
	text = strings.ReplaceAll(text, "\u00ef\u00bf\u00a5", "\uffe5")
	text = strings.ReplaceAll(text, "\u00c2\u00a5", "\uffe5")
	text = strings.ReplaceAll(text, "\u00a5", "\uffe5")

	return text
}

// extractBuyerAndSellerByPosition extracts buyer and seller names based on text position
func (s *OCRService) extractBuyerAndSellerByPosition(text string) (buyer, seller *string) {
	text = normalizeInvoiceTextForParsing(text)

	// Step 1: Find positions of "购" and "销" markers
	buyerMarkerIndex := -1
	sellerMarkerIndex := -1

	findStandaloneMarker := func(marker string) int {
		// Match marker as a standalone token (line start/end or surrounded by whitespace).
		re := regexp.MustCompile(fmt.Sprintf(`(?m)(^|[\s])%s($|[\s])`, regexp.QuoteMeta(marker)))
		loc := re.FindStringSubmatchIndex(text)
		if len(loc) >= 2 {
			// FindStringSubmatchIndex returns overall match span; locate the marker within it.
			span := text[loc[0]:loc[1]]
			off := strings.Index(span, marker)
			if off >= 0 {
				return loc[0] + off
			}
			return loc[0]
		}
		return -1
	}

	// Find "购" marker (购买方)
	buyerPatterns := []string{"购买方", "购方"}
	for _, pattern := range buyerPatterns {
		if idx := strings.Index(text, pattern); idx != -1 {
			if buyerMarkerIndex == -1 || idx < buyerMarkerIndex {
				buyerMarkerIndex = idx
			}
		}
	}
	if buyerMarkerIndex == -1 {
		buyerMarkerIndex = findStandaloneMarker("购")
	}
	if buyerMarkerIndex == -1 {
		// "购 名称：xxx" may become "购名称：xxx" after inline-space normalization.
		if loc := regexp.MustCompile(`购\s*名称`).FindStringIndex(text); loc != nil {
			buyerMarkerIndex = loc[0]
		}
	}

	// Find "销" marker (销售方)
	sellerPatterns := []string{"销售方", "销方"}
	for _, pattern := range sellerPatterns {
		if idx := strings.Index(text, pattern); idx != -1 {
			if sellerMarkerIndex == -1 || idx < sellerMarkerIndex {
				sellerMarkerIndex = idx
			}
		}
	}
	if sellerMarkerIndex == -1 {
		sellerMarkerIndex = findStandaloneMarker("销")
	}
	if sellerMarkerIndex == -1 {
		// "销 名称：xxx" may become "销名称：xxx" after inline-space normalization.
		if loc := regexp.MustCompile(`销\s*名称`).FindStringIndex(text); loc != nil {
			sellerMarkerIndex = loc[0]
		}
	}

	type nameEntry struct {
		name     string
		position int
	}
	var names []nameEntry
	seenNames := make(map[string]bool)
	addName := func(name string, pos int) bool {
		name = cleanupName(strings.TrimSpace(name))
		if name == "" || len([]rune(name)) <= 1 || isBadPartyNameCandidate(name) {
			return false
		}
		if seenNames[name] {
			return false
		}
		seenNames[name] = true
		names = append(names, nameEntry{name: name, position: pos})
		return true
	}

	// Step 2: Find all "名称：XXX" patterns with their positions.
	// Use non-greedy match and stop at 3+ spaces, newline, or end of string.
	nameMatches := namePositionPattern.FindAllStringSubmatchIndex(text, -1)
	for _, match := range nameMatches {
		if len(match) >= 4 {
			addName(text[match[2]:match[3]], match[0])
		}
	}

	// Additional patterns: handle inline forms like "名称: XXX" (but avoid the naive
	// "next line" capture, which often grabs "纳税人识别号:" when the value is empty).
	nameInlineRe := regexp.MustCompile(`(?m)(?:名称)[:：]\s*([^\n\r]+)`)
	matches := nameInlineRe.FindAllStringSubmatchIndex(text, -1)
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		addName(text[m[2]:m[3]], m[0])
	}

	// Handle empty "名称:" lines where the value appears later (common in PDF text extraction).
	lines := strings.Split(text, "\n")
	lineStarts := make([]int, 0, len(lines))
	offset := 0
	for _, line := range lines {
		lineStarts = append(lineStarts, offset)
		offset += len(line) + 1
	}
	nameLineRe := regexp.MustCompile(`名称\s*[:：]\s*(.*)$`)
	looksLikePartyName := func(s string) bool {
		s = strings.TrimSpace(s)
		if s == "" {
			return false
		}
		if len([]rune(s)) > 80 {
			return false
		}
		if strings.ContainsAny(s, "*<>") {
			return false
		}
		if regexp.MustCompile(`^\d+$`).MatchString(s) {
			return false
		}
		// Skip common table headers and invoice labels.
		blocklist := []string{
			"项目名称", "货物或应税劳务、服务名称", "规格型号", "单位", "单 位", "数量", "数 量", "单价", "单 价", "金额", "金 额",
			"税率", "税率/征收率", "税额", "税 额", "合计", "价税合计", "备注", "开票人", "收款人", "复核",
		}
		for _, b := range blocklist {
			if strings.Contains(s, b) {
				return false
			}
		}
		hasHan := false
		for _, r := range s {
			if unicode.Is(unicode.Han, r) {
				hasHan = true
				break
			}
		}
		return hasHan && !isBadPartyNameCandidate(s)
	}
	for i, line := range lines {
		loc := nameLineRe.FindStringSubmatchIndex(line)
		if len(loc) < 4 {
			continue
		}
		value := strings.TrimSpace(line[loc[2]:loc[3]])
		if value != "" && !isBadPartyNameCandidate(value) {
			addName(value, lineStarts[i]+loc[2])
			continue
		}

		// Look ahead for the first non-label line; PDF text extraction often prints labels
		// (纳税人识别号/地址/开户行...) before the actual name.
		for j := i + 1; j < len(lines) && j <= i+200; j++ {
			cand := strings.TrimSpace(lines[j])
			if cand == "" {
				continue
			}
			if !looksLikePartyName(cand) {
				continue
			}
			// If this candidate was already added (e.g. buyer and seller labels are close),
			// keep scanning to find the next distinct party name.
			if addName(cand, lineStarts[j]) {
				break
			}
		}
	}

	// Step 4: Associate names with buyer/seller based on proximity to markers
	if len(names) == 0 {
		return nil, nil
	}

	if buyerMarkerIndex == -1 && sellerMarkerIndex == -1 {
		return nil, nil
	}

	sort.Slice(names, func(i, j int) bool { return names[i].position < names[j].position })

	pickClosest := func(markerIdx int, used map[string]bool) *string {
		bestDist := int(^uint(0) >> 1)
		bestName := ""
		for _, entry := range names {
			if used[entry.name] {
				continue
			}
			d := abs(entry.position - markerIdx)
			if d < bestDist {
				bestDist = d
				bestName = entry.name
			}
		}
		if bestName == "" {
			return nil
		}
		nameCopy := bestName
		return &nameCopy
	}

	used := make(map[string]bool)
	if buyerMarkerIndex != -1 {
		buyer = pickClosest(buyerMarkerIndex, used)
		if buyer != nil {
			used[*buyer] = true
		}
	}
	if sellerMarkerIndex != -1 {
		seller = pickClosest(sellerMarkerIndex, used)
		if seller != nil {
			used[*seller] = true
		}
	}

	// Fallback ordering (first buyer, last seller) if one side is still missing.
	if (buyer == nil || seller == nil) && len(names) >= 2 {
		first := names[0].name
		last := names[len(names)-1].name
		if buyer == nil {
			buyer = &first
		}
		if seller == nil && last != first {
			seller = &last
		}
	}

	return buyer, seller
}

// ParseInvoiceData extracts invoice information from OCR text
func (s *OCRService) ParseInvoiceData(text string) (*InvoiceExtractedData, error) {
	data := &InvoiceExtractedData{
		RawText: text,
	}

	parsedText := normalizeInvoiceTextForParsing(text)

	// Extract invoice number - support both same-line and newline-separated formats
	invoiceNumRegexes := []*regexp.Regexp{
		regexp.MustCompile(`发票号码[：:]?\s*[\n\r]?\s*(\d+)`),
		regexp.MustCompile(`发票代码[：:]?\s*[\n\r]?\s*(\d+)`),
		regexp.MustCompile(`No[\.:]?\s*[\n\r]?\s*(\d+)`),
	}
	for _, re := range invoiceNumRegexes {
		if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
			invoiceNum := match[1]
			data.InvoiceNumber = &invoiceNum
			break
		}
	}

	// If not found, try to match standalone invoice numbers (8-25 digits)
	// This handles old format invoices (8 digits) and electronic invoices (20+ digits)
	if data.InvoiceNumber == nil {
		// Match 8-digit numbers on their own line (old invoice format)
		// or 20-25 digit numbers (electronic invoice format)
		standaloneNumRegex := regexp.MustCompile(`(?m)^(\d{8}|\d{20,25})$`)
		if match := standaloneNumRegex.FindStringSubmatch(parsedText); len(match) > 1 {
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
		if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
			date := match[1]
			data.InvoiceDate = &date
			break
		}
	}

	// If not found, try to match space-separated date format: "2025 年07 月02 日"
	if data.InvoiceDate == nil {
		if match := spaceDelimitedDatePattern.FindStringSubmatch(parsedText); len(match) > 3 {
			// Reconstruct date: "2025年07月02日"
			date := fmt.Sprintf("%s年%s月%s日", match[1], match[2], match[3])
			data.InvoiceDate = &date
		}
	}

	// If not found, try to match standalone date format (YYYY年M月D日 or YYYY年MM月DD日)
	// This is common in electronic invoices where the date appears on its own line
	if data.InvoiceDate == nil {
		standaloneDateRegex := regexp.MustCompile(`(\d{4}年\d{1,2}月\d{1,2}日)`)
		if match := standaloneDateRegex.FindStringSubmatch(parsedText); len(match) > 1 {
			date := match[1]
			data.InvoiceDate = &date
		}
	}

	// Extract amount - support newline-separated formats like "（小写）\n¥\n3080.00"
	amountRegexes := []*regexp.Regexp{
		// Prefer tax-inclusive total (价税合计).
		regexp.MustCompile(`价税合计\s*[（(]?\s*小写\s*[)）]?\s*[：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
		regexp.MustCompile(`总计\s*[：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
		// "合计金额(小写)" is usually tax-exclusive; keep as fallback.
		regexp.MustCompile(`合计金额\s*[（(]?\s*小写\s*[)）]?\s*[：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
		regexp.MustCompile(`[（(]?\s*小写\s*[)）]?\s*[：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
		regexp.MustCompile(`金额\s*[：:]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`),
	}
	for _, re := range amountRegexes {
		if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
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
		if match := chineseAmountRegex.FindStringSubmatch(parsedText); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil {
				data.Amount = amount
			}
		}
	}

	// If still not found, try to match standalone amount at the end of text
	// This handles cases where the amount appears as a final value like "￥19.58" or "￥100"
	if data.Amount == nil {
		// Match amount with ￥ or ¥ symbol, possibly on its own line
		// Support amounts with or without decimal places
		standaloneAmountRegex := regexp.MustCompile(`[¥￥]\s*([\d]+(?:\.[\d]{1,2})?)(?:\s*$|\s*\n|$)`)
		// Find all matches and take the last one (most likely to be the total)
		matches := standaloneAmountRegex.FindAllStringSubmatch(parsedText, -1)
		if len(matches) > 0 {
			lastMatch := matches[len(matches)-1]
			if len(lastMatch) > 1 {
				if amount := parseAmount(lastMatch[1]); amount != nil {
					data.Amount = amount
				}
			}
		}
	}

	// If we still failed (or picked an obviously too-small value), choose the max currency amount.
	// This handles common invoice layouts where totals are listed as multiple currency values:
	//   ¥121.80 (价税合计), ¥107.79 (不含税), ¥14.01 (税额)
	currencyAmountRegex := regexp.MustCompile(`[\x{00A5}\x{FFE5}]\s*([\d]+(?:\.[\d]{1,2})?)`)
	curMatches := currencyAmountRegex.FindAllStringSubmatch(parsedText, -1)
	var maxAmt *float64
	for _, m := range curMatches {
		if len(m) < 2 {
			continue
		}
		if a := parseAmount(m[1]); a != nil && *a >= MinValidAmount {
			if maxAmt == nil || *a > *maxAmt {
				maxAmt = a
			}
		}
	}
	if maxAmt != nil {
		if data.Amount == nil || *data.Amount < *maxAmt {
			data.Amount = maxAmt
		}
	}

	// Extract tax amount
	taxRegexes := []*regexp.Regexp{
		regexp.MustCompile(`税额[：:]?\s*[¥￥]?([\d,.]+)`),
		regexp.MustCompile(`税金[：:]?\s*[¥￥]?([\d,.]+)`),
	}
	for _, re := range taxRegexes {
		if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
			if tax := parseAmount(match[1]); tax != nil {
				data.TaxAmount = tax
				break
			}
		}
	}

	// Use position-based method to extract buyer and seller names
	buyer, seller := s.extractBuyerAndSellerByPosition(parsedText)
	data.BuyerName = buyer
	data.SellerName = seller

	// If position-based method didn't find buyer, try fallback regex methods
	if data.BuyerName == nil {
		// Extract buyer name - handle both inline and newline-separated formats
		// First try patterns with explicit "购买方" prefix
		buyerRegexes := []*regexp.Regexp{
			regexp.MustCompile(`购买方[：:]?\s*名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
			regexp.MustCompile(`购买方名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
			regexp.MustCompile(`购货方[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
		}
		for _, re := range buyerRegexes {
			if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
				buyer := strings.TrimSpace(match[1])
				// Filter out section headers like "信息" (information) that might be captured
				// Also filter out labels like "名称：" or "名称:"
				if buyer != "" && buyer != "信" && buyer != "息" && buyer != "名称：" && buyer != "名称:" {
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
			if match := buyerSectionRegex.FindStringSubmatch(parsedText); len(match) > 2 {
				buyer := strings.TrimSpace(match[2])
				// Filter out labels and section markers
				if buyer != "" && buyer != "销" && buyer != "售" && buyer != "方" && buyer != "名称：" && buyer != "名称:" {
					data.BuyerName = &buyer
				}
			}
		}

		// If still not found, try to match "个人" (individual) as a standalone buyer
		if data.BuyerName == nil {
			individualRegex := regexp.MustCompile(`(个人)`)
			if match := individualRegex.FindStringSubmatch(parsedText); len(match) > 1 {
				buyer := match[1]
				data.BuyerName = &buyer
			}
		}

		// Final cleanup: if buyer name was set to a label by mistake, clear it
		if data.BuyerName != nil && (*data.BuyerName == "名称：" || *data.BuyerName == "名称:") {
			data.BuyerName = nil
		}
	}

	// If position-based method didn't find seller, try fallback regex methods
	if data.SellerName == nil {
		// Extract seller name - handle both inline and newline-separated formats
		// First try patterns with explicit "销售方" prefix
		sellerRegexes := []*regexp.Regexp{
			regexp.MustCompile(`销售方[：:]?\s*名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
			regexp.MustCompile(`销售方名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
			regexp.MustCompile(`出票方[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
		}
		for _, re := range sellerRegexes {
			if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
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
			if match := sellerSectionRegex.FindStringSubmatch(parsedText); len(match) > 2 {
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
			if match := flexibleSellerRegex.FindStringSubmatch(parsedText); len(match) > 2 {
				seller := strings.TrimSpace(match[2])
				// Additional validation: check if this looks like a company name
				// Sellers should not be "个人" (individual) - that would be a buyer
				if seller != "" && len(seller) > 2 && seller != "个人" {
					data.SellerName = &seller
				}
			}
		}

		// If still not found, try to find company name appearing BEFORE tax ID
		// This is common in OCR output where data sequence differs from labels
		// Pattern: company name (containing 公司/商店/企业/中心/etc.) on one line, followed by tax ID
		if data.SellerName == nil {
			// Look for company/store name followed by tax ID on next line
			// Company indicators: 公司, 商店, 企业, 中心, 厂, 店, etc.
			companyBeforeTaxIDRegex := regexp.MustCompile(fmt.Sprintf(`([^\n\r]*(?:公司|商店|企业|中心|厂|店|行|社|院|局|部)[^\n\r]*)[\s\n\r]+(%s)`, taxIDPattern))
			if match := companyBeforeTaxIDRegex.FindStringSubmatch(parsedText); len(match) > 2 {
				seller := strings.TrimSpace(match[1])
				// Validate it's not too short and doesn't contain obvious non-name content
				if len(seller) > 3 && seller != "个人" {
					data.SellerName = &seller
				}
			}
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
