package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ledongthuc/pdf"
	"github.com/otiai10/gosseract/v2"
)

// OCRService provides OCR functionality
type OCRService struct{}

const (
	// taxIDPattern matches Chinese unified social credit codes (15-20 alphanumeric characters)
	taxIDPattern = `[A-Z0-9]{15,20}`

	// amountPattern matches monetary amounts with ¥ or ￥ symbol
	// Pattern explanation: (?:,\d{3})* allows comma-separated thousands, (?:\.\d{1,2})? makes decimals optional
	amountPattern = `[¥￥]\s*\d+(?:,\d{3})*(?:\.\d{1,2})?`

	// taxIDPatternWithBoundary for structured extraction (word boundaries for better matching)
	taxIDPatternWithBoundary = `\b[A-Z0-9]{15,20}\b`

	// MinValidAmount is the minimum amount threshold for payment extraction
	MinValidAmount = 1.0

	// MaxMerchantNameLength is the maximum allowed length for merchant names
	MaxMerchantNameLength = 50

	// digitsWhitelist defines characters allowed for digit-only OCR
	digitsWhitelist = "0123456789.-¥￥,"

	// PaddleOCR CLI configuration
	paddleOCRTimeout = 60 * time.Second
)

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

// PaddleOCRResponse represents the response from PaddleOCR service
type PaddleOCRResponse struct {
	Success   bool            `json:"success"`
	Text      string          `json:"text"`
	Lines     []PaddleOCRLine `json:"lines"`
	LineCount int             `json:"line_count"`
	Error     string          `json:"error,omitempty"`
}

// PaddleOCRLine represents a single line of OCR result
type PaddleOCRLine struct {
	Text       string      `json:"text"`
	Confidence float64     `json:"confidence"`
	Box        [][]float64 `json:"box"`
}

// RecognizeImage performs OCR on an image file with enhanced configuration
func (s *OCRService) RecognizeImage(imagePath string) (string, error) {
	client := gosseract.NewClient()
	defer client.Close()

	// Set language to Chinese simplified and English
	client.SetLanguage("chi_sim", "eng")

	// Set page segmentation mode for better recognition of mixed layouts
	// PSM_AUTO (3): Fully automatic page segmentation, but no OSD
	client.SetPageSegMode(gosseract.PSM_AUTO)

	if err := client.SetImage(imagePath); err != nil {
		return "", fmt.Errorf("failed to set image: %w", err)
	}

	text, err := client.Text()
	if err != nil {
		return "", fmt.Errorf("failed to recognize text: %w", err)
	}

	return text, nil
}

// RecognizeImageEnhanced performs OCR with image preprocessing for better accuracy
func (s *OCRService) RecognizeImageEnhanced(imagePath string) (string, error) {
	fmt.Printf("[OCR] Starting enhanced image recognition for: %s\n", imagePath)

	// Create temporary directory for preprocessed image
	tempDir, err := os.MkdirTemp("", "ocr-enhanced-*")
	if err != nil {
		// Fallback to basic recognition
		fmt.Printf("[OCR] Failed to create temp dir, falling back to basic recognition: %v\n", err)
		return s.RecognizeImage(imagePath)
	}
	defer os.RemoveAll(tempDir)

	// Preprocess image
	processedPath := s.preprocessImage(imagePath, tempDir, 0)

	client := gosseract.NewClient()
	defer client.Close()

	client.SetLanguage("chi_sim", "eng")
	client.SetPageSegMode(gosseract.PSM_AUTO)

	if err := client.SetImage(processedPath); err != nil {
		return "", fmt.Errorf("failed to set image: %w", err)
	}

	text, err := client.Text()
	if err != nil {
		return "", fmt.Errorf("failed to recognize text: %w", err)
	}

	fmt.Printf("[OCR] Enhanced recognition extracted %d characters\n", len(text))
	return text, nil
}

// RecognizeWithPaddleOCR executes PaddleOCR CLI script for recognition
func (s *OCRService) RecognizeWithPaddleOCR(imagePath string) (string, error) {
	fmt.Printf("[OCR] Running PaddleOCR CLI for: %s\n", imagePath)

	// Find the paddleocr_cli.py script
	scriptPath := s.findPaddleOCRScript()
	if scriptPath == "" {
		return "", fmt.Errorf("paddleocr_cli.py script not found")
	}

	// Execute Python script
	ctx, cancel := context.WithTimeout(context.Background(), paddleOCRTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", scriptPath, imagePath)
	output, err := cmd.Output()
	if err != nil {
		// Try with "python" if "python3" fails
		cmd = exec.CommandContext(ctx, "python", scriptPath, imagePath)
		output, err = cmd.Output()
		if err != nil {
			return "", fmt.Errorf("failed to execute PaddleOCR: %w", err)
		}
	}

	// Parse JSON output
	var result PaddleOCRResponse
	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("failed to parse PaddleOCR output: %w", err)
	}

	if !result.Success {
		return "", fmt.Errorf("PaddleOCR error: %s", result.Error)
	}

	fmt.Printf("[OCR] PaddleOCR extracted %d lines, %d characters\n", result.LineCount, len(result.Text))
	return result.Text, nil
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

// isPaddleOCRAvailable checks if RapidOCR or PaddleOCR CLI is available
func (s *OCRService) isPaddleOCRAvailable() bool {
	// Check if script exists
	scriptPath := s.findPaddleOCRScript()
	if scriptPath == "" {
		fmt.Printf("[OCR] paddleocr_cli.py script not found\n")
		return false
	}

	// Check if Python is available
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First check for RapidOCR (preferred, lighter weight)
	cmd := exec.CommandContext(ctx, "python3", "-c", "import rapidocr_onnxruntime; print('ok')")
	if output, err := cmd.CombinedOutput(); err == nil {
		fmt.Printf("[OCR] RapidOCR is available\n")
		return true
	} else {
		fmt.Printf("[OCR] RapidOCR check failed: %v, output: %s\n", err, string(output))
	}

	// Try with "python" instead of "python3"
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	cmd = exec.CommandContext(ctx2, "python", "-c", "import rapidocr_onnxruntime; print('ok')")
	if output, err := cmd.CombinedOutput(); err == nil {
		fmt.Printf("[OCR] RapidOCR is available (python)\n")
		return true
	} else {
		fmt.Printf("[OCR] RapidOCR check with python failed: %v, output: %s\n", err, string(output))
	}

	// Fall back to check PaddleOCR
	ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel3()
	cmd = exec.CommandContext(ctx3, "python3", "-c", "import paddleocr; print('ok')")
	if output, err := cmd.CombinedOutput(); err == nil {
		fmt.Printf("[OCR] PaddleOCR is available\n")
		return true
	} else {
		fmt.Printf("[OCR] PaddleOCR check failed: %v, output: %s\n", err, string(output))
	}

	// Try with "python"
	ctx4, cancel4 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel4()
	cmd = exec.CommandContext(ctx4, "python", "-c", "import paddleocr; print('ok')")
	if output, err := cmd.CombinedOutput(); err == nil {
		fmt.Printf("[OCR] PaddleOCR is available (python)\n")
		return true
	} else {
		fmt.Printf("[OCR] PaddleOCR check with python failed: %v, output: %s\n", err, string(output))
	}

	fmt.Printf("[OCR] Neither RapidOCR nor PaddleOCR is available\n")
	return false
}

// RecognizePaymentScreenshot performs OCR optimized for payment screenshots
// It prioritizes PaddleOCR/RapidOCR if available, falls back to Tesseract with multiple strategies
func (s *OCRService) RecognizePaymentScreenshot(imagePath string) (string, error) {
	fmt.Printf("[OCR] Starting payment screenshot recognition for: %s\n", imagePath)

	// Log script path
	scriptPath := s.findPaddleOCRScript()
	fmt.Printf("[OCR] Script path: %s\n", scriptPath)

	// Strategy 1: Try PaddleOCR/RapidOCR first (best for Chinese payment screenshots)
	if s.isPaddleOCRAvailable() {
		fmt.Printf("[OCR] RapidOCR/PaddleOCR available, attempting to use it\n")
		text, err := s.RecognizeWithPaddleOCR(imagePath)
		if err == nil && strings.TrimSpace(text) != "" {
			fmt.Printf("[OCR] RapidOCR/PaddleOCR succeeded with %d characters\n", len(text))
			return text, nil
		}
		fmt.Printf("[OCR] RapidOCR/PaddleOCR failed or returned empty: %v, falling back to Tesseract\n", err)
	} else {
		fmt.Printf("[OCR] RapidOCR/PaddleOCR not available, using Tesseract\n")
	}

	// Fallback: Use Tesseract with multiple strategies (existing code)
	fmt.Printf("[OCR] Using Tesseract fallback strategies\n")

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "payment-ocr-*")
	if err != nil {
		return s.RecognizeImageEnhanced(imagePath)
	}
	defer os.RemoveAll(tempDir)

	var results []string

	// Strategy 1: Original image with PSM_SINGLE_BLOCK (good for large text)
	if text := s.ocrWithConfig(imagePath, gosseract.PSM_SINGLE_BLOCK); text != "" {
		results = append(results, text)
		fmt.Printf("[OCR] Strategy 1 (PSM_SINGLE_BLOCK) extracted %d chars\n", len(text))
	}

	// Strategy 2: Payment-specific preprocessing with PSM_AUTO
	processedPath := s.preprocessPaymentScreenshot(imagePath, tempDir)
	if text := s.ocrWithConfig(processedPath, gosseract.PSM_AUTO); text != "" {
		results = append(results, text)
		fmt.Printf("[OCR] Strategy 2 (preprocessed + PSM_AUTO) extracted %d chars\n", len(text))
	}

	// Strategy 3: High contrast binary image with PSM_SINGLE_BLOCK
	binaryPath := s.createBinaryImage(imagePath, tempDir)
	if binaryPath != imagePath {
		if text := s.ocrWithConfig(binaryPath, gosseract.PSM_SINGLE_BLOCK); text != "" {
			results = append(results, text)
			fmt.Printf("[OCR] Strategy 3 (binary + PSM_SINGLE_BLOCK) extracted %d chars\n", len(text))
		}
	}

	// Strategy 4: Inverted image (white text on black background issues)
	invertedPath := s.createInvertedImage(imagePath, tempDir)
	if invertedPath != imagePath {
		if text := s.ocrWithConfig(invertedPath, gosseract.PSM_AUTO); text != "" {
			results = append(results, text)
			fmt.Printf("[OCR] Strategy 4 (inverted) extracted %d chars\n", len(text))
		}
	}

	// Merge all results - combine unique content
	mergedText := s.mergeOCRResults(results)
	fmt.Printf("[OCR] Merged result: %d characters from %d strategies\n", len(mergedText), len(results))

	if mergedText == "" {
		// Fallback to enhanced recognition
		return s.RecognizeImageEnhanced(imagePath)
	}

	return mergedText, nil
}

// preprocessPaymentScreenshot applies specialized preprocessing for payment screenshots
// Payment screenshots typically have large amount text that needs special handling
func (s *OCRService) preprocessPaymentScreenshot(inputPath, tempDir string) string {
	outputPath := filepath.Join(tempDir, "payment-processed.png")

	// Check if ImageMagick is available
	_, err := exec.LookPath("convert")
	if err != nil {
		fmt.Printf("[OCR] ImageMagick not found, skipping preprocessing\n")
		return inputPath
	}

	// For payment screenshots, we need more aggressive preprocessing:
	// 1. Resize to larger size for better recognition of large fonts
	// 2. Convert to grayscale
	// 3. High contrast with thresholding to make text stand out
	// 4. Sharpen edges
	// 5. Remove noise
	cmd := exec.Command("convert", inputPath,
		"-resize", "200%", // Scale up for better recognition
		"-colorspace", "Gray", // Convert to grayscale
		"-sigmoidal-contrast", "10,50%", // Increase contrast (S-curve)
		"-threshold", "50%", // Binary threshold for clean text
		"-morphology", "Close", "Square:1", // Close small gaps in characters
		"-sharpen", "0x2", // Sharpen edges
		outputPath)

	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("[OCR] Payment screenshot preprocessing failed: %v (output: %s)\n", err, string(output))
		// Try alternative preprocessing
		return s.preprocessPaymentScreenshotAlt(inputPath, tempDir)
	}

	fmt.Printf("[OCR] Payment screenshot preprocessed: %s\n", outputPath)
	return outputPath
}

// preprocessPaymentScreenshotAlt provides alternative preprocessing for payment screenshots
func (s *OCRService) preprocessPaymentScreenshotAlt(inputPath, tempDir string) string {
	outputPath := filepath.Join(tempDir, "payment-processed-alt.png")

	// Alternative preprocessing without threshold (for colored text)
	cmd := exec.Command("convert", inputPath,
		"-resize", "150%",
		"-colorspace", "Gray",
		"-contrast-stretch", "2%x2%", // Aggressive contrast stretch
		"-unsharp", "0x5", // Unsharp mask for edge enhancement
		"-despeckle", // Remove noise
		outputPath)

	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("[OCR] Alternative preprocessing failed: %v (output: %s), using original\n", err, string(output))
		return inputPath
	}

	fmt.Printf("[OCR] Alternative preprocessing successful: %s\n", outputPath)
	return outputPath
}

// ocrWithConfig performs OCR with specific page segmentation mode
func (s *OCRService) ocrWithConfig(imagePath string, psm gosseract.PageSegMode) string {
	client := gosseract.NewClient()
	defer client.Close()

	client.SetLanguage("chi_sim", "eng")
	client.SetPageSegMode(psm)

	if err := client.SetImage(imagePath); err != nil {
		return ""
	}

	text, err := client.Text()
	if err != nil {
		return ""
	}

	return text
}

// createBinaryImage creates a high-contrast binary image
func (s *OCRService) createBinaryImage(inputPath, tempDir string) string {
	outputPath := filepath.Join(tempDir, "binary.png")

	// Double negation technique: invert -> threshold -> invert back
	// This ensures dark text on light background gets properly thresholded
	// First negate makes dark text light, threshold creates clean binary,
	// second negate restores proper polarity for Tesseract OCR
	cmd := exec.Command("convert", inputPath,
		"-colorspace", "Gray",
		"-negate",           // Invert colors (dark text becomes light)
		"-threshold", "40%", // Aggressive threshold for clean binary
		"-negate", // Invert back (restore dark text on light background)
		outputPath)

	if _, err := cmd.CombinedOutput(); err != nil {
		return inputPath
	}
	return outputPath
}

// createInvertedImage creates an inverted version of the image
func (s *OCRService) createInvertedImage(inputPath, tempDir string) string {
	outputPath := filepath.Join(tempDir, "inverted.png")

	cmd := exec.Command("convert", inputPath,
		"-negate",
		outputPath)

	if _, err := cmd.CombinedOutput(); err != nil {
		return inputPath
	}
	return outputPath
}

// mergeOCRResults merges multiple OCR results, prioritizing amount extraction
func (s *OCRService) mergeOCRResults(results []string) string {
	if len(results) == 0 {
		return ""
	}

	var bestResult string
	var bestScore int

	for _, result := range results {
		score := 0
		for _, pattern := range amountDetectionPatterns {
			matches := pattern.FindAllString(result, -1)
			score += len(matches)
		}
		// Also score based on Chinese character count (we want good text recognition)
		for _, r := range result {
			if unicode.Is(unicode.Han, r) {
				score++
			}
		}

		if score > bestScore {
			bestScore = score
			bestResult = result
		}
	}

	// If best result has amount, use it as base
	// Otherwise, concatenate all unique lines
	if bestScore > 0 {
		return bestResult
	}

	// Merge unique lines from all results
	seen := make(map[string]bool)
	var merged strings.Builder
	for _, result := range results {
		lines := strings.Split(result, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !seen[line] {
				seen[line] = true
				merged.WriteString(line)
				merged.WriteString("\n")
			}
		}
	}

	return merged.String()
}

// ocrDigitsOnly performs OCR configured specifically for digit recognition
func (s *OCRService) ocrDigitsOnly(imagePath string) string {
	client := gosseract.NewClient()
	defer client.Close()

	// Use only English for digit recognition
	client.SetLanguage("eng")
	client.SetPageSegMode(gosseract.PSM_SINGLE_LINE)
	// Whitelist only digits and common amount characters
	client.SetVariable("tessedit_char_whitelist", digitsWhitelist)

	if err := client.SetImage(imagePath); err != nil {
		return ""
	}

	text, err := client.Text()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(text)
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

// RecognizePDF extracts text from PDF using hybrid approach
func (s *OCRService) RecognizePDF(pdfPath string) (string, error) {
	fmt.Printf("[OCR] Starting PDF recognition for: %s\n", pdfPath)

	var pdftotextResult, ocrResult string
	var pdftotextErr, ocrErr error

	// Method 1: Try pdftotext first (good for numbers, amounts, tax IDs)
	pdftotextResult, pdftotextErr = s.extractTextWithPdftotext(pdfPath)
	if pdftotextErr != nil {
		fmt.Printf("[OCR] pdftotext extraction failed: %v\n", pdftotextErr)
	} else {
		fmt.Printf("[OCR] pdftotext extracted %d characters\n", len(pdftotextResult))
	}

	// Check if pdftotext result has enough Chinese characters
	chineseRatio := s.getChineseCharRatio(pdftotextResult)
	fmt.Printf("[OCR] pdftotext Chinese character ratio: %.2f%%\n", chineseRatio*100)

	// If pdftotext result is good enough (has sufficient Chinese), use it directly
	if pdftotextErr == nil && chineseRatio > 0.1 && !s.isGarbledText(pdftotextResult) {
		fmt.Printf("[OCR] pdftotext result is sufficient, using it directly\n")
		return pdftotextResult, nil
	}

	// Method 2: Use enhanced OCR to get Chinese characters
	fmt.Printf("[OCR] pdftotext result lacks Chinese content, performing enhanced OCR\n")
	ocrResult, ocrErr = s.enhancedPdfToImageOCR(pdfPath)
	if ocrErr != nil {
		fmt.Printf("[OCR] Enhanced OCR failed: %v\n", ocrErr)
		// If OCR also failed, return pdftotext result if available
		if pdftotextErr == nil && strings.TrimSpace(pdftotextResult) != "" {
			return pdftotextResult, nil
		}
		return "", fmt.Errorf("both pdftotext and OCR failed: pdftotext: %v, OCR: %v", pdftotextErr, ocrErr)
	}

	// Method 3: Merge results - combine the best of both
	if pdftotextErr == nil && strings.TrimSpace(pdftotextResult) != "" {
		mergedResult := s.mergeExtractionResults(pdftotextResult, ocrResult)
		fmt.Printf("[OCR] Merged result: %d characters\n", len(mergedResult))
		return mergedResult, nil
	}

	return ocrResult, nil
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

// enhancedPdfToImageOCR converts PDF to images with preprocessing and performs OCR
func (s *OCRService) enhancedPdfToImageOCR(pdfPath string) (string, error) {
	fmt.Printf("[OCR] Starting enhanced PDF to image OCR: %s\n", pdfPath)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "pdf-ocr-enhanced-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Step 1: Convert PDF to high-resolution images (400 DPI)
	outputPrefix := filepath.Join(tempDir, "page")
	cmd := exec.Command("pdftoppm", "-png", "-r", "400", pdfPath, outputPrefix)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("pdftoppm failed: %w (output: %s)", err, string(output))
	}

	// Find generated images
	files, err := filepath.Glob(filepath.Join(tempDir, "page-*.png"))
	if err != nil || len(files) == 0 {
		return "", fmt.Errorf("no images generated from PDF")
	}
	sort.Strings(files)

	// Step 2: Preprocess images and perform OCR
	client := gosseract.NewClient()
	defer client.Close()

	// Configure Tesseract for better Chinese recognition
	// PSM_AUTO: Automatic page segmentation (best for most invoices with mixed layouts)
	// Alternative: PSM_SINGLE_BLOCK can be used for simple single-column invoices
	client.SetLanguage("chi_sim", "eng")
	client.SetPageSegMode(gosseract.PSM_AUTO)

	var allText strings.Builder

	for i, imgPath := range files {
		fmt.Printf("[OCR] Processing page %d/%d\n", i+1, len(files))

		// Preprocess image using ImageMagick (if available)
		processedPath := s.preprocessImage(imgPath, tempDir, i)

		// Perform OCR
		client.SetImage(processedPath)
		text, err := client.Text()
		if err != nil {
			fmt.Printf("[OCR] OCR failed for page %d: %v\n", i+1, err)
			continue
		}

		fmt.Printf("[OCR] Extracted %d characters from page %d\n", len(text), i+1)
		allText.WriteString(text)
		allText.WriteString("\n")
	}

	return allText.String(), nil
}

// preprocessImage applies image enhancements using ImageMagick
func (s *OCRService) preprocessImage(inputPath, tempDir string, pageNum int) string {
	outputPath := filepath.Join(tempDir, fmt.Sprintf("processed-%d.png", pageNum))

	// Check if ImageMagick is available
	_, err := exec.LookPath("convert")
	if err != nil {
		fmt.Printf("[OCR] ImageMagick not found, skipping preprocessing\n")
		return inputPath
	}

	// Apply preprocessing: grayscale, contrast enhancement, adaptive sharpening, denoise, normalize
	// Command: convert input.png -colorspace Gray -contrast-stretch 0.1x0.1% -adaptive-sharpen 0x1 -median 1 -normalize output.png
	cmd := exec.Command("convert", inputPath,
		"-colorspace", "Gray", // Convert to grayscale
		"-contrast-stretch", "0.1x0.1%", // Enhance contrast
		"-adaptive-sharpen", "0x1", // Sharpen edges
		"-median", "1", // Denoise
		"-normalize", // Normalize histogram
		outputPath)

	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("[OCR] Image preprocessing failed: %v (output: %s), using original\n", err, string(output))
		return inputPath
	}

	fmt.Printf("[OCR] Image preprocessed successfully: %s\n", outputPath)
	return outputPath
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
	fmt.Printf("[OCR] Converting PDF to images for OCR: %s\n", pdfPath)

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
	cmd := exec.Command("pdftoppm", "-png", "-r", "300", pdfPath, outputPrefix)
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

	// Initialize gosseract client for OCR
	client := gosseract.NewClient()
	defer client.Close()
	client.SetLanguage("chi_sim", "eng")

	var allText strings.Builder

	// Process each image
	for i, imgPath := range files {
		fmt.Printf("[OCR] Processing page %d/%d\n", i+1, len(files))

		// Perform OCR on the image
		client.SetImage(imgPath)
		text, err := client.Text()

		if err != nil {
			fmt.Printf("[OCR] OCR failed for page %d: %v\n", i+1, err)
			continue
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
			// Skip space if previous is digit and next is date unit (年/月/日)
			if unicode.IsDigit(prev) && (next == '年' || next == '月' || next == '日' || next == '时' || next == '分' || next == '秒') {
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
		method := "微信支付"
		data.PaymentMethod = &method
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
		method := "支付宝"
		data.PaymentMethod = &method
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
		method := "银行转账"
		data.PaymentMethod = &method
	}
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

// extractBuyerAndSellerByPosition extracts buyer and seller names based on text position
func (s *OCRService) extractBuyerAndSellerByPosition(text string) (buyer, seller *string) {
	// Step 1: Find positions of "购" and "销" markers
	buyerMarkerIndex := -1
	sellerMarkerIndex := -1

	// Find "购" marker (购买方)
	buyerPatterns := []string{"购买方", "购方", "购"}
	for _, pattern := range buyerPatterns {
		if idx := strings.Index(text, pattern); idx != -1 {
			if buyerMarkerIndex == -1 || idx < buyerMarkerIndex {
				buyerMarkerIndex = idx
			}
		}
	}

	// Find "销" marker (销售方)
	sellerPatterns := []string{"销售方", "销方", "销"}
	for _, pattern := range sellerPatterns {
		if idx := strings.Index(text, pattern); idx != -1 {
			if sellerMarkerIndex == -1 || idx < sellerMarkerIndex {
				sellerMarkerIndex = idx
			}
		}
	}

	// Step 2: Find all "名称：XXX" or "名    称:XXX" patterns with their positions
	// Support formats: "名称：", "名称:", "名   称：", "名   称:"
	// Use non-greedy match and stop at 3+ spaces, newline, or end of string
	nameMatches := namePositionPattern.FindAllStringSubmatchIndex(text, -1)

	// Step 3: Extract names and associate with buyer/seller based on position
	type nameEntry struct {
		name     string
		position int
	}
	var names []nameEntry

	for _, match := range nameMatches {
		if len(match) >= 4 {
			name := strings.TrimSpace(text[match[2]:match[3]])
			// Clean up the name - remove trailing markers
			name = cleanupName(name)
			// Filter out invalid names: empty, single character, or just markers/labels
			if name != "" && name != "信" && name != "息" && name != "名称：" && name != "名称:" && len(name) > 1 {
				names = append(names, nameEntry{name: name, position: match[0]})
			}
		}
	}

	// Step 4: Associate names with buyer/seller based on proximity to markers
	if len(names) == 0 {
		return nil, nil
	}

	// If we have both markers, use smart positioning logic
	if buyerMarkerIndex != -1 && sellerMarkerIndex != -1 {
		// Strategy: For each name, find which marker it's closest to, considering direction
		// Names can appear either before or after their associated markers depending on invoice format
		type preference struct {
			nameIdx    int
			markerType string
			score      int
		}

		var prefs []preference

		for idx, entry := range names {
			distToBuyer := abs(entry.position - buyerMarkerIndex)
			distToSeller := abs(entry.position - sellerMarkerIndex)

			// Check which markers come before/after the name
			buyerBefore := buyerMarkerIndex < entry.position
			sellerBefore := sellerMarkerIndex < entry.position
			buyerAfter := buyerMarkerIndex > entry.position
			sellerAfter := sellerMarkerIndex > entry.position

			if buyerBefore && sellerBefore {
				// Both markers come before the name - name is after both sections
				// Pick the closer one
				if distToBuyer < distToSeller {
					prefs = append(prefs, preference{idx, "buyer", distToBuyer})
				} else {
					prefs = append(prefs, preference{idx, "seller", distToSeller})
				}
			} else if buyerBefore && sellerAfter {
				// Buyer before, seller after - name is between markers
				// Prefer the first marker (buyer in this case) as names in structured sections
				// belong to the section they appear in
				if buyerMarkerIndex < sellerMarkerIndex {
					// Buyer comes first - prefer buyer
					prefs = append(prefs, preference{idx, "buyer", distToBuyer})
					// Also add seller with penalty to allow fallback if needed
					prefs = append(prefs, preference{idx, "seller", distToSeller + 1000})
				} else {
					// Seller comes first - prefer seller
					prefs = append(prefs, preference{idx, "seller", distToSeller})
					prefs = append(prefs, preference{idx, "buyer", distToBuyer + 1000})
				}
			} else if sellerBefore && buyerAfter {
				// Seller before, buyer after - name is between markers
				if sellerMarkerIndex < buyerMarkerIndex {
					// Seller comes first - prefer seller
					prefs = append(prefs, preference{idx, "seller", distToSeller})
					prefs = append(prefs, preference{idx, "buyer", distToBuyer + 1000})
				} else {
					// Buyer comes first - prefer buyer
					prefs = append(prefs, preference{idx, "buyer", distToBuyer})
					prefs = append(prefs, preference{idx, "seller", distToSeller + 1000})
				}
			} else if buyerAfter && sellerAfter {
				// Both markers come after the name - name is before both sections
				// This is the top-bottom layout case where names precede markers
				// Assign to closer marker
				prefs = append(prefs, preference{idx, "buyer", distToBuyer})
				prefs = append(prefs, preference{idx, "seller", distToSeller})
			} else {
				// Name is before markers or between them - use distance to both
				prefs = append(prefs, preference{idx, "buyer", distToBuyer})
				prefs = append(prefs, preference{idx, "seller", distToSeller})
			}
		}

		// Sort by distance - closest pairs first
		sort.Slice(prefs, func(i, j int) bool {
			return prefs[i].score < prefs[j].score
		})

		// Greedy assignment
		assignedNames := make(map[int]bool)

		for _, pref := range prefs {
			if assignedNames[pref.nameIdx] {
				continue
			}

			if pref.markerType == "buyer" && buyer == nil {
				nameCopy := names[pref.nameIdx].name
				buyer = &nameCopy
				assignedNames[pref.nameIdx] = true
			} else if pref.markerType == "seller" && seller == nil {
				nameCopy := names[pref.nameIdx].name
				seller = &nameCopy
				assignedNames[pref.nameIdx] = true
			}

			if buyer != nil && seller != nil {
				break
			}
		}
	} else if buyerMarkerIndex != -1 || sellerMarkerIndex != -1 {
		// Only one marker found - use position relative to that marker
		markerIndex := buyerMarkerIndex
		if sellerMarkerIndex != -1 {
			markerIndex = sellerMarkerIndex
		}

		var beforeNames, afterNames []nameEntry
		for _, entry := range names {
			if entry.position < markerIndex {
				beforeNames = append(beforeNames, entry)
			} else {
				afterNames = append(afterNames, entry)
			}
		}

		// The name after the marker belongs to that party
		// The name before belongs to the other party
		if buyerMarkerIndex != -1 {
			if len(afterNames) > 0 {
				buyer = &afterNames[0].name
			}
			if len(beforeNames) > 0 {
				seller = &beforeNames[len(beforeNames)-1].name
			}
		} else {
			if len(afterNames) > 0 {
				seller = &afterNames[0].name
			}
			if len(beforeNames) > 0 {
				buyer = &beforeNames[len(beforeNames)-1].name
			}
		}
	}

	return buyer, seller
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

	// If not found, try to match standalone invoice numbers (8-25 digits)
	// This handles old format invoices (8 digits) and electronic invoices (20+ digits)
	if data.InvoiceNumber == nil {
		// Match 8-digit numbers on their own line (old invoice format)
		// or 20-25 digit numbers (electronic invoice format)
		standaloneNumRegex := regexp.MustCompile(`(?m)^(\d{8}|\d{20,25})$`)
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

	// If not found, try to match space-separated date format: "2025 年07 月02 日"
	if data.InvoiceDate == nil {
		if match := spaceDelimitedDatePattern.FindStringSubmatch(text); len(match) > 3 {
			// Reconstruct date: "2025年07月02日"
			date := fmt.Sprintf("%s年%s月%s日", match[1], match[2], match[3])
			data.InvoiceDate = &date
		}
	}

	// If not found, try to match standalone date format (YYYY年M月D日 or YYYY年MM月DD日)
	// This is common in electronic invoices where the date appears on its own line
	if data.InvoiceDate == nil {
		standaloneDateRegex := regexp.MustCompile(`(\d{4}年\d{1,2}月\d{1,2}日)`)
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

	// If still not found, try to match standalone amount at the end of text
	// This handles cases where the amount appears as a final value like "￥19.58" or "￥100"
	if data.Amount == nil {
		// Match amount with ￥ or ¥ symbol, possibly on its own line
		// Support amounts with or without decimal places
		standaloneAmountRegex := regexp.MustCompile(`[¥￥]\s*([\d]+(?:\.[\d]{1,2})?)(?:\s*$|\s*\n|$)`)
		// Find all matches and take the last one (most likely to be the total)
		matches := standaloneAmountRegex.FindAllStringSubmatch(text, -1)
		if len(matches) > 0 {
			lastMatch := matches[len(matches)-1]
			if len(lastMatch) > 1 {
				if amount := parseAmount(lastMatch[1]); amount != nil {
					data.Amount = amount
				}
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

	// Use position-based method to extract buyer and seller names
	buyer, seller := s.extractBuyerAndSellerByPosition(text)
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
			if match := re.FindStringSubmatch(text); len(match) > 1 {
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
			if match := buyerSectionRegex.FindStringSubmatch(text); len(match) > 2 {
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
			if match := individualRegex.FindStringSubmatch(text); len(match) > 1 {
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

		// If still not found, try to find company name appearing BEFORE tax ID
		// This is common in OCR output where data sequence differs from labels
		// Pattern: company name (containing 公司/商店/企业/中心/etc.) on one line, followed by tax ID
		if data.SellerName == nil {
			// Look for company/store name followed by tax ID on next line
			// Company indicators: 公司, 商店, 企业, 中心, 厂, 店, etc.
			companyBeforeTaxIDRegex := regexp.MustCompile(fmt.Sprintf(`([^\n\r]*(?:公司|商店|企业|中心|厂|店|行|社|院|局|部)[^\n\r]*)[\s\n\r]+(%s)`, taxIDPattern))
			if match := companyBeforeTaxIDRegex.FindStringSubmatch(text); len(match) > 2 {
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
