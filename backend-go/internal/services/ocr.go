package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/liyue201/goqr"
)

// OCRService provides OCR functionality
type OCRService struct{}

var (
	rapidOCRModulesOnce sync.Once
	rapidOCRModulesOK   bool
)

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
	// Negative amounts in screenshots usually include decimals (e.g. "-400.00", "-13,897.00").
	// Avoid matching non-money ids like "ZZHK-0007-..." where "-0007" is not an amount.
	negativeAmountRegex   = regexp.MustCompile("[-\u2212]\\s*(?:[\u00A5￥]\\s*)?(\\d+(?:,\\d{3})*(?:\\.\\d{1,2}))")
	merchantFullNameRegex = regexp.MustCompile(`商户全称[：:]?[\s]*([^\n收单机构支付方式]+?)[\s]*(?:收单机构|支付方式|\n|$)`)
	merchantGenericRegex  = regexp.MustCompile(`([^\n]+(?:店|行|公司|商户|超市|餐厅|饭店|有限公司))`)

	// Pattern to insert space between Chinese date and time when 日 is directly followed by digits
	chineseDateTimePattern = regexp.MustCompile(`日(\d)`)

	// Pattern to insert space between ISO date and time when "YYYY-MM-DD" is directly followed by time.
	isoDateTimePattern = regexp.MustCompile(`(\d{4}-\d{1,2}-\d{1,2})(\d{1,2}:\d{2}(?::\d{2})?)`)

	// Alipay bill detail often contains an order id like "Alipay251116...".
	alipayOrderNumberRegex = regexp.MustCompile(`(?i)alipay\d{6,}`)

	// Amount detection patterns for merging OCR results
	// Note: First pattern uses \d{3,} to prioritize large amounts (e.g., 1700.00)
	// which are more likely to be the main transaction amount in payment screenshots
	amountDetectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`-?\d{3,}\.?\d{0,2}`), // Large amounts like 1700.00 or -1700.00
		regexp.MustCompile(`[¥￥]-?\d+\.?\d*`),    // Currency symbol with amount (any size)
	}
)

var paymentInvisibleSpaceReplacer = strings.NewReplacer(
	"\u00a0", " ", // nbsp
	"\u200b", " ", // zwsp
	"\u200c", " ", // zwnj
	"\u200d", " ", // zwj
	"\ufeff", " ", // bom
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
	Amount                    *float64 `json:"amount"`
	AmountSource              string   `json:"amount_source,omitempty"`
	AmountConfidence          float64  `json:"amount_confidence,omitempty"`
	Merchant                  *string  `json:"merchant"`
	MerchantSource            string   `json:"merchant_source,omitempty"`
	MerchantConfidence        float64  `json:"merchant_confidence,omitempty"`
	TransactionTime           *string  `json:"transaction_time"`
	TransactionTimeSource     string   `json:"transaction_time_source,omitempty"`
	TransactionTimeConfidence float64  `json:"transaction_time_confidence,omitempty"`
	PaymentMethod             *string  `json:"payment_method"`
	PaymentMethodSource       string   `json:"payment_method_source,omitempty"`
	PaymentMethodConfidence   float64  `json:"payment_method_confidence,omitempty"`
	OrderNumber               *string  `json:"order_number"`
	OrderNumberSource         string   `json:"order_number_source,omitempty"`
	OrderNumberConfidence     float64  `json:"order_number_confidence,omitempty"`
	RawText                   string   `json:"raw_text"`
	PrettyText                string   `json:"pretty_text,omitempty"`
}

type InvoiceLineItem struct {
	Name     string   `json:"name"`
	Spec     string   `json:"spec,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	Quantity *float64 `json:"quantity,omitempty"`
}

// InvoiceExtractedData represents extracted invoice information
type InvoiceExtractedData struct {
	InvoiceNumber           *string           `json:"invoice_number"`
	InvoiceNumberSource     string            `json:"invoice_number_source,omitempty"`
	InvoiceNumberConfidence float64           `json:"invoice_number_confidence,omitempty"`
	InvoiceDate             *string           `json:"invoice_date"`
	InvoiceDateSource       string            `json:"invoice_date_source,omitempty"`
	InvoiceDateConfidence   float64           `json:"invoice_date_confidence,omitempty"`
	Amount                  *float64          `json:"amount"`
	AmountSource            string            `json:"amount_source,omitempty"`
	AmountConfidence        float64           `json:"amount_confidence,omitempty"`
	TaxAmount               *float64          `json:"tax_amount"`
	TaxAmountSource         string            `json:"tax_amount_source,omitempty"`
	TaxAmountConfidence     float64           `json:"tax_amount_confidence,omitempty"`
	SellerName              *string           `json:"seller_name"`
	SellerNameSource        string            `json:"seller_name_source,omitempty"`
	SellerNameConfidence    float64           `json:"seller_name_confidence,omitempty"`
	BuyerName               *string           `json:"buyer_name"`
	BuyerNameSource         string            `json:"buyer_name_source,omitempty"`
	BuyerNameConfidence     float64           `json:"buyer_name_confidence,omitempty"`
	Items                   []InvoiceLineItem `json:"items,omitempty"`
	RawText                 string            `json:"raw_text"`
	RawTextSource           string            `json:"raw_text_source,omitempty"` // pymupdf/rapidocr
	PrettyText              string            `json:"pretty_text,omitempty"`
	PDFZones                []PDFTextZonesPage `json:"pdf_zones,omitempty"`
}

// OCRCLIResponse represents the response from the Python OCR CLI script.
type OCRCLIResponse struct {
	Success       bool            `json:"success"`
	Text          string          `json:"text"`
	Lines         []OCRCLILine    `json:"lines"`
	LineCount     int             `json:"line_count"`
	Engine        string          `json:"engine,omitempty"`
	Profile       string          `json:"profile,omitempty"`
	Variant       string          `json:"variant,omitempty"`
	Backend       string          `json:"backend,omitempty"`
	BackendErrors []string        `json:"backend_errors,omitempty"`
	Params        map[string]any  `json:"params,omitempty"`
	Variants      []OCRCLIVariant `json:"variants,omitempty"`
	Error         string          `json:"error,omitempty"`
}

type OCRCLIVariant struct {
	Variant       string         `json:"variant"`
	Score         float64        `json:"score"`
	Lines         int            `json:"lines"`
	Chars         int            `json:"chars"`
	Backend       string         `json:"backend,omitempty"`
	BackendErrors []string       `json:"backend_errors,omitempty"`
	Params        map[string]any `json:"params,omitempty"`
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

// RecognizeWithRapidOCR executes the ocr_cli.py script for OCR recognition (RapidOCR only).
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
	// One OCR job can be CPU-heavy (and may spawn external processes).
	// Limit concurrent OCR to keep the server responsive.
	ctx, cancel := context.WithTimeout(context.Background(), rapidOCRTimeout)
	defer cancel()
	release, err := acquireWithTimeout(ctx, limitOCR, rapidOCRTimeout, "ocr")
	if err != nil {
		return "", err
	}
	defer release()

	// Prefer a persistent worker to avoid per-request Python startup overhead.
	// Falls back to the CLI when the worker is disabled or unavailable.
	reqProfile := parseOCRProfileFromArgs(extraArgs)
	if ocrWorkerEnabled() {
		workerScript := s.findOCRWorkerScript()
		if workerScript == "" {
			fmt.Printf("[OCR] OCR worker enabled but scripts/ocr_worker.py not found; falling back to CLI\n")
		} else {
			fmt.Printf("[OCR] Running OCR worker for: %s (engine=%s profile=%s)\n", imagePath, getOCREngine(), reqProfile)
			out, err := recognizeWithRapidOCRWorker(workerScript, imagePath, reqProfile)
			if err == nil {
				var result OCRCLIResponse
				parseErr := unmarshalPossiblyNoisyJSON(out, &result)
				if parseErr == nil && result.Success {
					engine := result.Engine
					if strings.TrimSpace(engine) == "" {
						engine = "rapidocr"
					}
					usedProfile := strings.TrimSpace(result.Profile)
					if usedProfile == "" {
						usedProfile = reqProfile
					}
					be := strings.TrimSpace(result.Backend)
					if be == "" {
						be = "custom"
					}
					if result.Variant != "" {
						fmt.Printf("[OCR] OCR(worker) extracted %d lines, %d characters (engine=%s profile=%s variant=%s backend=%s)\n", result.LineCount, len(result.Text), engine, usedProfile, result.Variant, be)
					} else {
						fmt.Printf("[OCR] OCR(worker) extracted %d lines, %d characters (engine=%s profile=%s backend=%s)\n", result.LineCount, len(result.Text), engine, usedProfile, be)
					}
					return result.Text, nil
				}
				if parseErr != nil {
					fmt.Printf("[OCR] OCR worker JSON parse failed: %v\n", parseErr)
				} else if !result.Success {
					fmt.Printf("[OCR] OCR worker returned error: %s\n", result.Error)
				}
			} else {
				fmt.Printf("[OCR] OCR worker failed: %v; falling back to CLI\n", err)
			}
		}
	}

	fmt.Printf("[OCR] Running OCR CLI for: %s (engine=%s)\n", imagePath, getOCREngine())

	// Find the OCR CLI script
	scriptPath := s.findOCRCLIScript()
	if scriptPath == "" {
		return "", fmt.Errorf("ocr_cli.py script not found")
	}

	// Execute Python script
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
			fmt.Printf("[OCR] RapidOCR CLI exec error: %v, output=%s\n", execErr, stripANSIEscapes(string(output)))
			return "", fmt.Errorf("failed to execute RapidOCR CLI: %w (output: %s)", execErr, string(output))
		}
		fmt.Printf("[OCR] RapidOCR CLI JSON parse失败: %v, output=%s\n", err, stripANSIEscapes(string(output)))
		return "", fmt.Errorf("failed to parse OCR CLI output: %w (output: %s)", err, string(output))
	}

	if !result.Success {
		fmt.Printf("[OCR] RapidOCR CLI returned error: %s, output=%s\n", result.Error, stripANSIEscapes(string(output)))
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
	be := strings.TrimSpace(result.Backend)
	if be == "" {
		be = "custom"
	}
	if len(result.BackendErrors) > 0 {
		fmt.Printf("[OCR] backend errors: %v\n", result.BackendErrors)
	}
	if len(result.Params) > 0 {
		fmt.Printf("[OCR] backend params: det=%v, rec=%v, dict=%v, cls=%v\n", result.Params["det"], result.Params["rec"], result.Params["dict"], result.Params["cls"])
		if v, ok := result.Params["model_dir"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				fmt.Printf("[OCR] model cache dir: %s\n", strings.TrimSpace(s))
			}
		}
	}
	if result.Variant != "" {
		fmt.Printf("[OCR] OCR extracted %d lines, %d characters (engine=%s profile=%s variant=%s backend=%s)\n", result.LineCount, len(result.Text), engine, profile, result.Variant, be)
	} else {
		fmt.Printf("[OCR] OCR extracted %d lines, %d characters (engine=%s profile=%s backend=%s)\n", result.LineCount, len(result.Text), engine, profile, be)
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

// findOCRCLIScript locates the OCR CLI script (RapidOCR v3).
func (s *OCRService) findOCRCLIScript() string {
	// Check common locations
	locations := []string{
		"scripts/ocr_cli.py",
		"../scripts/ocr_cli.py",
		"/app/scripts/ocr_cli.py",
		"./ocr_cli.py",
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
	// Check if script exists (worker preferred when enabled).
	if ocrWorkerEnabled() {
		workerPath := s.findOCRWorkerScript()
		if workerPath == "" {
			fmt.Printf("[OCR] ocr_worker.py script not found\n")
			return false
		}
	} else {
		scriptPath := s.findOCRCLIScript()
		if scriptPath == "" {
			fmt.Printf("[OCR] ocr_cli.py script not found\n")
			return false
		}
	}

	rapidOCRModulesOnce.Do(func() {
		// RapidOCR v3 requires both rapidocr and onnxruntime.
		rapidOCRModulesOK = s.checkPythonModule("rapidocr") && s.checkPythonModule("onnxruntime")
	})
	if rapidOCRModulesOK {
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
	RawText   string `json:"raw_text,omitempty"`
	ZonedText string `json:"zoned_text,omitempty"`
	Zones     []PDFTextZonesPage `json:"zones,omitempty"`
	Layout    string `json:"layout,omitempty"` // zones|ordered|raw
	Ordered   bool   `json:"ordered,omitempty"`
	PageCount int    `json:"page_count,omitempty"`
	Extractor string `json:"extractor,omitempty"`
	Error     string `json:"error,omitempty"`
}

type PDFTextZonesSpan struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	T  string  `json:"t"`
}

type PDFTextZonesRow struct {
	Region string            `json:"region"`
	Y0     float64           `json:"y0"`
	Y1     float64           `json:"y1"`
	Text   string            `json:"text"`
	Spans  []PDFTextZonesSpan `json:"spans,omitempty"`
}

type PDFTextZonesPage struct {
	Page   int             `json:"page"`
	Width  float64         `json:"width"`
	Height float64         `json:"height"`
	Rows   []PDFTextZonesRow `json:"rows,omitempty"`
}

func (s *OCRService) extractTextWithPyMuPDF(pdfPath string) (string, string, error) {
	text, source, _, err := s.extractTextWithPyMuPDFMeta(pdfPath)
	return text, source, err
}

func (s *OCRService) extractTextWithPyMuPDFMeta(pdfPath string) (string, string, *PDFTextCLIResponse, error) {
	fmt.Printf("[OCR] Attempting PDF text extraction with PyMuPDF: %s\n", pdfPath)

	scriptPath := s.findPDFTextScript()
	if scriptPath == "" {
		return "", "", nil, fmt.Errorf("pdf_text_cli.py script not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	run := func(python string) ([]byte, error) {
		layout := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_PDF_TEXT_LAYOUT")))
		if layout == "" {
			layout = "zones"
		}
		if layout != "zones" && layout != "ordered" && layout != "raw" {
			layout = "zones"
		}
		includeZones := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_PDF_TEXT_INCLUDE_ZONES")))
		if includeZones == "" {
			includeZones = "true"
		}
		zonesPages := strings.TrimSpace(os.Getenv("SBM_PDF_TEXT_ZONES_PAGES"))
		if zonesPages == "" {
			zonesPages = "1"
		}
		cmd := exec.CommandContext(ctx, python, scriptPath, pdfPath, "--layout", layout, "--include-zones", includeZones, "--zones-pages", zonesPages)
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
			return "", "", nil, fmt.Errorf("failed to execute PyMuPDF CLI: %w (output: %s)", execErr, string(output))
		}
		return "", "", nil, fmt.Errorf("failed to parse PyMuPDF CLI output: %w (output: %s)", err, string(output))
	}

	if !result.Success {
		return "", "", nil, fmt.Errorf("PyMuPDF error: %s", result.Error)
	}

	text := result.Text
	if strings.TrimSpace(text) == "" && strings.TrimSpace(result.RawText) != "" {
		text = result.RawText
	}

	layout := strings.ToLower(strings.TrimSpace(result.Layout))
	if layout == "" {
		layout = "raw"
	}
	source := "pymupdf"
	if layout == "zones" {
		source = "pymupdf_zones"
	}

	fmt.Printf("[OCR] PyMuPDF extracted %d characters from %d pages (%s, layout=%s, ordered=%v)\n", len(text), result.PageCount, result.Extractor, layout, result.Ordered)
	return text, source, &result, nil
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
	text, _, err := s.RecognizePDFWithSource(pdfPath)
	return text, err
}

// RecognizePDFWithSource is like RecognizePDF but also returns the text source.
// source values: "pymupdf" | "rapidocr".
func (s *OCRService) RecognizePDFWithSource(pdfPath string) (text string, source string, err error) {
	text, source, _, err = s.RecognizePDFWithSourceAndMeta(pdfPath)
	return text, source, err
}

// RecognizePDFWithSourceAndMeta returns the extracted text, its source, and optional PyMuPDF layout metadata.
// The metadata is only available when PyMuPDF extraction is used.
func (s *OCRService) RecognizePDFWithSourceAndMeta(pdfPath string) (text string, source string, meta *PDFTextCLIResponse, err error) {
	fmt.Printf("[OCR] Starting PDF recognition for: %s\n", pdfPath)

	if strings.TrimSpace(pdfPath) == "" {
		return "", "", nil, fmt.Errorf("empty PDF path")
	}

	// Validate PDF file exists and is a regular file
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to access PDF file: %w", err)
	}
	if !fileInfo.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("PDF path is not a regular file")
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_PDF_TEXT_EXTRACTOR")))
	if mode == "" {
		mode = "pymupdf"
	}

	if mode != "off" && mode != "false" && mode != "0" {
		if text, src, res, err := s.extractTextWithPyMuPDFMeta(pdfPath); err == nil {
			if s.isLikelyUsefulInvoicePDFText(text) {
				if strings.TrimSpace(src) == "" {
					src = "pymupdf"
				}
				return text, src, res, nil
			}
			fmt.Printf("[OCR] PyMuPDF text looks incomplete; falling back to RapidOCR image OCR\n")
		} else {
			fmt.Printf("[OCR] PyMuPDF extraction failed; falling back to RapidOCR image OCR: %v\n", err)
		}
	}

	fmt.Printf("[OCR] Using OCR CLI for PDF pages\n")
	text, err = s.pdfToImageOCR(pdfPath)
	if err != nil {
		return "", "", nil, err
	}
	return text, "rapidocr", nil, nil
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
			if !needROI {
				needROI = isBadPartyNameCandidate(*buyer) || isBadPartyNameCandidate(*seller)
			}
		}
		if needROI {
			partyInjected, _, _ := s.injectInvoicePartiesFromRegions(tempDir, imgPath)
			if partyInjected != "" {
				text = partyInjected + "\n" + text
			}
		}

		// Totals (amount/tax) ROI fallback:
		// - true/1/yes: always try ROI injection
		// - auto (default): try ROI only when amount/tax seems missing from the OCR text
		totalMode := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_INVOICE_TOTAL_ROI")))
		if totalMode == "" {
			totalMode = "auto"
		}
		needTotalsROI := false
		switch totalMode {
		case "1", "true", "yes":
			needTotalsROI = true
		case "auto":
			parsed, _ := s.ParseInvoiceData(text)
			needTotalsROI = parsed == nil || parsed.Amount == nil || parsed.TaxAmount == nil
		}
		if needTotalsROI {
			totalsInjected, _, _ := s.injectInvoiceTotalsFromRegions(tempDir, imgPath)
			if totalsInjected != "" {
				text = totalsInjected + "\n" + text
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

func (s *OCRService) injectInvoiceTotalsFromRegions(tempDir, imgPath string) (injected string, totalOK bool, taxOK bool) {
	// Heuristic crop (A4 invoice layouts):
	// - Totals: bottom-right area near "价税合计(小写)" and "税额".
	text, err := s.ocrInvoiceRegion(tempDir, imgPath, "totals", 0.50, 0.66, 0.98, 0.94)
	if err != nil {
		fmt.Printf("[OCR] Totals ROI OCR failed: %v\n", err)
		return "", false, false
	}

	total, tax := extractTotalsFromROICandidate(text)

	var parts []string
	if total != nil {
		parts = append(parts, "\u4ef7\u7a0e\u5408\u8ba1(\u5c0f\u5199)\uff1a\u00a5"+formatFloat2(total))
		totalOK = true
	}
	if tax != nil {
		parts = append(parts, "\u7a0e\u989d\uff1a\u00a5"+formatFloat2(tax))
		taxOK = true
	}

	injected = strings.Join(parts, "\n")
	return injected, totalOK, taxOK
}

func extractTotalsFromROICandidate(text string) (total *float64, tax *float64) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}

	amountToken := `([0-9][0-9,]*(?:\.[0-9]{1,2})?)`
	ccyChars := "\u00a5\uFFE5" // ¥ and ￥
	ccy := fmt.Sprintf(`(?:[%s]\s*)?`, ccyChars)

	taxTotalKey := "\u4ef7\u7a0e\u5408\u8ba1"
	sumAmountKey := "\u5408\u8ba1\u91d1\u989d"
	totalKey := "\u603b\u8ba1"
	sumKey := "\u5408\u8ba1"

	totalRe := regexp.MustCompile(fmt.Sprintf(`(?s)(?:%s|%s|%s|%s).*?%s%s`, taxTotalKey, sumAmountKey, totalKey, sumKey, ccy, amountToken))
	totalMatches := totalRe.FindAllStringSubmatch(text, -1)
	if len(totalMatches) > 0 {
		last := totalMatches[len(totalMatches)-1]
		if len(last) > 1 {
			total = parseAmount(last[len(last)-1])
		}
	}

	taxKey := "\u7a0e\u989d"
	taxMoneyKey := "\u7a0e\u91d1"
	taxRe := regexp.MustCompile(fmt.Sprintf(`(?s)(?:%s|%s).*?%s%s`, taxKey, taxMoneyKey, ccy, amountToken))
	taxMatches := taxRe.FindAllStringSubmatch(text, -1)
	if len(taxMatches) > 0 {
		last := taxMatches[len(taxMatches)-1]
		if len(last) > 1 {
			tax = parseAmount(last[len(last)-1])
		}
	}

	return total, tax
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

	// Some OCR outputs place the actual name at the end of a "multi-label" line,
	// e.g. "名称: ... 纳税人识别号: 中国移动通信集团上海有限公司". Prefer the last label value.
	if name == "" {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if !strings.ContainsAny(line, ":：") {
				continue
			}
			if !(strings.Contains(line, "名称") || strings.Contains(line, "纳税人识别号") || strings.Contains(line, "统一社会信用代码")) {
				continue
			}
			candidate := extractNameFromTaxIDLabelLine(line)
			if candidate == "" {
				// Fallback to last label value if the tax-id label extraction didn't apply.
				idx := strings.LastIndexAny(line, ":：")
				if idx < 0 || idx+1 >= len(line) {
					continue
				}
				candidate = cleanupName(strings.TrimSpace(line[idx+1:]))
			}
			if candidate == "" {
				continue
			}
			if taxIDRegex.MatchString(candidate) || isBadPartyNameCandidate(candidate) {
				continue
			}
			if len([]rune(candidate)) > MaxMerchantNameLength {
				continue
			}
			name = candidate
			break
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
		if isBadPartyNameCandidate(s) {
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

func binarizeImage(src image.Image, thr int) (*image.Gray, error) {
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("empty image")
	}
	if thr < 0 {
		thr = 0
	}
	if thr > 255 {
		thr = 255
	}

	dst := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bb, _ := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			l := uint8((299*r + 587*g + 114*bb + 500) / 1000 >> 8)
			if int(l) > thr {
				dst.SetGray(x, y, color.Gray{Y: 255})
			} else {
				dst.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}
	return dst, nil
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

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}

	cropImage := func(src image.Image, r image.Rectangle) image.Image {
		r = r.Intersect(src.Bounds())
		if r.Empty() {
			return src
		}
		if si, ok := src.(subImager); ok {
			return si.SubImage(r)
		}
		dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		draw.Draw(dst, dst.Bounds(), src, r.Min, draw.Src)
		return dst
	}

	tryRecognize := func(src image.Image) ([]*goqr.QRData, error) {
		codes, err := goqr.Recognize(src)
		if err == nil && len(codes) > 0 {
			return codes, nil
		}
		return nil, err
	}

	var codes []*goqr.QRData

	// Pass 1: full image.
	if c, e := tryRecognize(img); e == nil && len(c) > 0 {
		codes = c
	}

	// Pass 2: crop top-left region (common invoice QR placement).
	if len(codes) == 0 {
		b := img.Bounds()
		r := image.Rect(b.Min.X, b.Min.Y, b.Min.X+int(float64(b.Dx())*0.45), b.Min.Y+int(float64(b.Dy())*0.45))
		cropped := cropImage(img, r)
		if c, e := tryRecognize(cropped); e == nil && len(c) > 0 {
			codes = c
		}
	}

	// Pass 3: binarize (different thresholds) to improve QR decoding on faint scans.
	if len(codes) == 0 {
		thresholds := []int{110, 130, 150, 170, 190, 210}
		for _, thr := range thresholds {
			bin, binErr := binarizeImage(img, thr)
			if binErr == nil {
				if c, e := tryRecognize(bin); e == nil && len(c) > 0 {
					codes = c
					break
				}
			}
			b := img.Bounds()
			r := image.Rect(b.Min.X, b.Min.Y, b.Min.X+int(float64(b.Dx())*0.45), b.Min.Y+int(float64(b.Dy())*0.45))
			cropped := cropImage(img, r)
			binCrop, binErr2 := binarizeImage(cropped, thr)
			if binErr2 == nil {
				if c, e := tryRecognize(binCrop); e == nil && len(c) > 0 {
					codes = c
					break
				}
			}
		}
	}

	if len(codes) == 0 {
		return nil, nil
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
	// Only treat "inline" spaces as removable. OCR output often uses newlines for structure,
	// and removing them breaks downstream parsing (e.g. payment bill-detail blocks).
	isInlineSpace := func(r rune) bool {
		return r == ' ' || r == '\u3000' || r == '\u00a0'
	}

	var result strings.Builder
	runes := []rune(text)

	i := 0
	for i < len(runes) {
		r := runes[i]

		// If not a space, directly add
		if !isInlineSpace(r) {
			result.WriteRune(r)
			i++
			continue
		}

		// Is a space, check previous and next characters
		// Find previous non-space character
		prevIdx := i - 1
		for prevIdx >= 0 && isInlineSpace(runes[prevIdx]) {
			prevIdx--
		}

		// Find next non-space character
		nextIdx := i + 1
		for nextIdx < len(runes) && isInlineSpace(runes[nextIdx]) {
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
	// Fix missing space between ISO date and time: "2025-11-2812:57" -> "2025-11-28 12:57"
	dateStr = isoDateTimePattern.ReplaceAllString(dateStr, "$1 $2")
	// If 日 is directly followed by a digit (time), insert a space
	// This handles cases like "2025年10月23日14:59:46" -> "2025年10月23日 14:59:46"
	dateStr = chineseDateTimePattern.ReplaceAllString(dateStr, "日 $1")

	// Replace Chinese date separators with dashes
	dateStr = strings.ReplaceAll(dateStr, "年", "-")
	dateStr = strings.ReplaceAll(dateStr, "月", "-")
	dateStr = strings.ReplaceAll(dateStr, "日", "")
	// Common OCR output uses "/" as date separator.
	dateStr = strings.ReplaceAll(dateStr, "/", "-")
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

	// Preprocess for robust keyword matching/parsing:
	// - normalize newlines (some OCR outputs use \r\n/\r)
	// - remove invisible spaces that break keyword matching
	// - remove spaces between Chinese characters (e.g. "支 付 时 间" -> "支付时间")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = paymentInvisibleSpaceReplacer.Replace(text)
	text = removeChineseSpaces(text)
	text = strings.TrimSpace(text)

	// Try to detect payment platform and extract accordingly
	if s.isJDBillDetail(text) {
		s.parseJDBillDetail(text, data)
	} else if s.isUnionPayBillDetail(text) {
		s.parseUnionPayBillDetail(text, data)
	} else if s.isWeChatPay(text) {
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

	// Final cleanup for user-facing fields (keep RawText untouched)
	if data.Merchant != nil {
		clean := sanitizePaymentField(*data.Merchant)
		if clean == "" || clean == "说明" {
			data.Merchant = nil
		} else {
			data.Merchant = &clean
		}
	}
	if data.PaymentMethod != nil {
		clean := sanitizePaymentMethod(*data.PaymentMethod)
		if clean == "" {
			data.PaymentMethod = nil
		} else {
			data.PaymentMethod = &clean
		}
	}

	data.PrettyText = formatPaymentPrettyText(data.RawText, data)
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
	// Most reliable: Alipay order id in the screenshot.
	if alipayOrderNumberRegex.MatchString(text) {
		return true
	}
	// Alipay bill-detail UI labels (some screenshots may not include "支付宝" explicitly).
	if strings.Contains(text, "账单详情") &&
		(strings.Contains(text, "付款方式") || strings.Contains(text, "收单机构") || strings.Contains(text, "商品说明")) {
		return true
	}

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
	hasStrongSignal := false
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			count++
			if keyword == "转账" || keyword == "电子回单" {
				hasStrongSignal = true
			}
		}
	}
	// "银行" + "交易成功" is too generic (may match JD/other bill-detail UIs),
	// require at least one strong signal (转账 / 电子回单).
	return hasStrongSignal && count >= 2
}

// isJDBillDetail checks if text looks like JD Pay bill detail UI.
func (s *OCRService) isJDBillDetail(text string) bool {
	// JD bill detail often contains: 账单详情 + 创建时间 + (总订单编号/商户单号) and does not necessarily include "支付宝/微信".
	if !strings.Contains(text, "账单详情") {
		return false
	}
	if strings.Contains(text, "京东") || strings.Contains(text, "JD") {
		return true
	}
	// WeChat bill detail also has "商户单号" - do NOT use that as a JD signal.
	if strings.Contains(text, "总订单编号") {
		return true
	}
	return false
}

// isUnionPayBillDetail checks if text looks like UnionPay (云闪付) bill detail UI.
func (s *OCRService) isUnionPayBillDetail(text string) bool {
	if !strings.Contains(text, "账单详情") {
		return false
	}
	// Avoid misclassifying JD.
	if s.isJDBillDetail(text) {
		return false
	}
	// Strong signals for UnionPay bill detail.
	if strings.Contains(text, "云闪付") {
		return true
	}
	if strings.Contains(text, "订单金额") && strings.Contains(text, "订单时间") {
		return true
	}
	if strings.Contains(text, "商户订单号") && strings.Contains(text, "订单编号") {
		return true
	}
	// UnionPay often shows "在此商户的交易" entry.
	if strings.Contains(text, "在此商户的交易") && strings.Contains(text, "订单时间") {
		return true
	}
	return false
}

func extractInlineValueForLabel(line, label string) (string, bool) {
	line = strings.TrimSpace(line)
	label = strings.TrimSpace(label)
	if line == "" || label == "" {
		return "", false
	}
	if !strings.HasPrefix(line, label) {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, label))
	rest = strings.TrimLeft(rest, "：:\t ")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
}

func indexOfExactLine(lines []string, needle string) int {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return -1
	}
	for i, raw := range lines {
		if strings.TrimSpace(raw) == needle {
			return i
		}
	}
	return -1
}

func scanForwardValue(lines []string, labelIdx int, maxLookahead int, isBad func(string) bool) (string, bool) {
	for j := labelIdx + 1; j < len(lines) && j <= labelIdx+maxLookahead; j++ {
		cand := sanitizePaymentField(lines[j])
		if cand == "" {
			continue
		}
		if isBad != nil && isBad(cand) {
			continue
		}
		return cand, true
	}
	return "", false
}

func extractValueByLabel(lines []string, label string, maxLookahead int, isBad func(string) bool) (string, bool) {
	label = strings.TrimSpace(label)
	if label == "" || len(lines) == 0 {
		return "", false
	}
	if maxLookahead <= 0 {
		maxLookahead = 3
	}

	for i, raw := range lines {
		line := sanitizePaymentField(raw)
		if line == "" {
			continue
		}
		if v, ok := extractInlineValueForLabel(line, label); ok {
			v = sanitizePaymentField(v)
			if v != "" && (isBad == nil || !isBad(v)) {
				return v, true
			}
		}
		if line == label {
			if v, ok := scanForwardValue(lines, i, maxLookahead, isBad); ok {
				return v, true
			}
		}
	}
	return "", false
}

// parseWeChatPay extracts WeChat Pay information
func (s *OCRService) parseWeChatPay(text string, data *PaymentExtractedData) {
	lines := strings.Split(text, "\n")

	isWeChatBillDetailLabel := func(v string) bool {
		v = sanitizePaymentField(v)
		if v == "" {
			return true
		}
		labels := map[string]struct{}{
			"交易单号": {}, "商品": {}, "支付方式": {}, "付款方式": {}, "当前状态": {}, "支付时间": {}, "转账时间": {},
			"商户全称": {}, "收单机构": {}, "商户单号": {}, "服务": {}, "账单服务": {}, "可在支持的商户扫码退款": {},
			"全部账单": {}, "已支付": {}, "支付成功": {}, "转账成功": {},
		}
		_, ok := labels[v]
		return ok
	}
	isLikelyBankInstitution := func(v string) bool {
		v = sanitizePaymentField(v)
		if v == "" {
			return false
		}
		return strings.Contains(v, "银行") || strings.Contains(v, "清算") || strings.Contains(v, "收款清算") || strings.Contains(v, "收单机构")
	}
	extractBankCardPaymentMethod := func(t string) *string {
		lines := strings.Split(t, "\n")
		cardLikeRe := regexp.MustCompile(`(?m)([\p{Han}A-Za-z]{2,}(?:银行)?(?:信用卡|储蓄卡|借记卡|银行卡)\(\d{3,4}\))`)
		for _, line := range lines {
			line = sanitizePaymentMethod(strings.TrimSpace(line))
			if line == "" || isWeChatBillDetailLabel(line) {
				continue
			}
			if m := cardLikeRe.FindStringSubmatch(line); len(m) > 1 {
				method := sanitizePaymentMethod(m[1])
				if method != "" && !isWeChatBillDetailLabel(method) {
					return &method
				}
			}
		}
		cardParenRe := regexp.MustCompile(`(?m)(?:信用卡|储蓄卡|借记卡|银行卡)\(\d{3,4}\)`)
		for _, line := range lines {
			line = sanitizePaymentMethod(strings.TrimSpace(line))
			if line == "" || isWeChatBillDetailLabel(line) {
				continue
			}
			if cardParenRe.MatchString(line) {
				method := sanitizePaymentMethod(line)
				if method != "" && !isWeChatBillDetailLabel(method) {
					return &method
				}
			}
		}
		return nil
	}

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
				data.AmountSource = "wechat_amount_label"
				data.AmountConfidence = 0.9
				break
			}
		}
	}

	// Extract merchant/receiver (layout-aware, avoid capturing labels as values).
	// Priority: QR-pay title payee > 收款方/收款人/转账给 > 商户全称 > 商品（仅看起来像商户时才优先） > 通用兜底
	merchantIsBad := func(v string) bool {
		v = sanitizePaymentField(v)
		if v == "" || v == "备注" || v == "说明" {
			return true
		}
		// Generic/non-merchant phrases often appearing as "商品" values in WeChat bill detail.
		switch v {
		case "商户收款", "二维码收款", "商户消费", "收款", "付款", "转账", "转账收款":
			return true
		}
		if isWeChatBillDetailLabel(v) {
			return true
		}
		if isLikelyBankInstitution(v) {
			return true
		}
		return false
	}
	containsDigit := func(s string) bool {
		for _, r := range s {
			if unicode.IsDigit(r) {
				return true
			}
		}
		return false
	}

	extractWeChatTitleMerchant := func(lines []string) (string, bool) {
		// Heuristic for WeChat bill detail:
		// merchant/store name is usually the last non-empty line before the main amount line (e.g. "-3420.00").
		amountLineRe := regexp.MustCompile(`^\s*[-−]\s*[¥￥]?\s*[\d,]+(?:\.\d{1,2})?\s*$`)
		timeOnlyRe := regexp.MustCompile(`^\d{1,2}:\d{2}$`)
		isBadTitle := func(v string) bool {
			v = sanitizePaymentField(v)
			if merchantIsBad(v) {
				return true
			}
			if timeOnlyRe.MatchString(v) {
				return true
			}
			// Avoid picking obvious UI headers.
			switch v {
			case "全部账单", "已支付", "微信支付":
				return true
			}
			// Avoid picking a pure amount line or a pure numeric id.
			if amountLineRe.MatchString(v) {
				return true
			}
			digits := 0
			nonDigits := 0
			for _, r := range v {
				if unicode.IsDigit(r) {
					digits++
				} else if !unicode.IsSpace(r) {
					nonDigits++
				}
			}
			if nonDigits == 0 && digits >= 8 {
				return true
			}
			return false
		}

		amountIdx := -1
		for i, raw := range lines {
			line := sanitizePaymentField(raw)
			if amountLineRe.MatchString(line) {
				amountIdx = i
				break
			}
		}
		if amountIdx <= 0 {
			return "", false
		}

		for j := amountIdx - 1; j >= 0 && j >= amountIdx-6; j-- {
			cand := sanitizePaymentField(lines[j])
			if cand == "" {
				continue
			}
			if isBadTitle(cand) {
				continue
			}
			// Prefer names that look like a shop/store.
			if merchantGenericRegex.MatchString(cand) || strings.Contains(cand, "店") || strings.Contains(cand, "超市") || strings.Contains(cand, "酒行") {
				return cand, true
			}
			// Otherwise still accept short CJK merchant-like strings.
			if len([]rune(cand)) >= 2 && len([]rune(cand)) <= 30 && !containsDigit(cand) {
				return cand, true
			}
		}
		return "", false
	}

	// WeChat QR transfer style: "扫二维码付款-给XXXX"
	// OCR may misread "二维码" as "维码/二 维 码" etc, so match loosely.
	if m := regexp.MustCompile(`扫.{0,4}码付款[-—－]?\s*给([^\n\r]+)`).FindStringSubmatch(text); len(m) > 1 {
		merchant := sanitizePaymentField(m[1])
		if !merchantIsBad(merchant) {
			data.Merchant = &merchant
			data.MerchantSource = "wechat_qr_payee"
			if len([]rune(merchant)) <= 1 {
				data.MerchantConfidence = 0.3
			} else {
				data.MerchantConfidence = 0.85
			}
		}
	}

	// 收款方 / 收款人 / 转账给
	if data.Merchant == nil {
		for _, label := range []string{"收款方", "收款人", "转账给"} {
			if v, ok := extractValueByLabel(lines, label, 4, merchantIsBad); ok {
				merchant := sanitizePaymentField(v)
				if !merchantIsBad(merchant) {
					data.Merchant = &merchant
					data.MerchantSource = "wechat_label"
					data.MerchantConfidence = 0.9
					break
				}
			}
		}
	}

	// Title/store name near the amount line (WeChat bill detail).
	if data.Merchant == nil {
		if m, ok := extractWeChatTitleMerchant(lines); ok {
			merchant := sanitizePaymentField(m)
			if !merchantIsBad(merchant) {
				data.Merchant = &merchant
				data.MerchantSource = "wechat_title"
				data.MerchantConfidence = 0.92
			}
		}
	}

	// 商户全称：只接受“同一行内带值”的形式；如果是 label/value 分列或 label 先出现，交给后面的长扫描处理。
	var fullNameCandidate string
	for _, raw := range lines {
		line := sanitizePaymentField(raw)
		if v, ok := extractInlineValueForLabel(line, "商户全称"); ok {
			fullNameCandidate = sanitizePaymentField(v)
			if data.Merchant == nil && !merchantIsBad(fullNameCandidate) {
				data.Merchant = &fullNameCandidate
				data.MerchantSource = "wechat_fullname_label"
				data.MerchantConfidence = 0.95
			}
			break
		}
	}

	// 商品（低置信度兜底；如果看起来像商户名称，可以覆盖“商户全称”的超长公司名）
	var itemCandidate string
	itemIsBad := func(v string) bool {
		v = sanitizePaymentField(v)
		if merchantIsBad(v) {
			return true
		}
		// Avoid "商品说明" -> "说明" style partial matches.
		if v == "说明" || strings.HasPrefix(v, "说明") {
			return true
		}
		return false
	}
	if v, ok := extractValueByLabel(lines, "商品", 3, itemIsBad); ok {
		itemCandidate = sanitizePaymentField(v)
	}
	if itemCandidate != "" && !itemIsBad(itemCandidate) {
		looksLikeMerchant := merchantGenericRegex.MatchString(itemCandidate) ||
			(len([]rune(itemCandidate)) >= 2 && len([]rune(itemCandidate)) <= 12 && !containsDigit(itemCandidate))

		if looksLikeMerchant {
			// Prefer short merchant-like item over very long "商户全称".
			if data.Merchant == nil || (fullNameCandidate != "" && len([]rune(fullNameCandidate)) > len([]rune(itemCandidate))+6) {
				m := itemCandidate
				data.Merchant = &m
				data.MerchantSource = "wechat_item"
				data.MerchantConfidence = 0.8
			}
		} else if data.Merchant == nil {
			m := itemCandidate
			data.Merchant = &m
			data.MerchantSource = "wechat_item"
			data.MerchantConfidence = 0.4
		}
	}
	// If we still don't have merchant, try a line-based fallback for QR-pay title lines.
	if data.Merchant == nil {
		lines := strings.Split(text, "\n")
		titleRe := regexp.MustCompile(`扫.{0,6}码付款.*?给\s*([^\s¥￥\n\r]{1,20})`)
		titleOnlyRe := regexp.MustCompile(`扫.{0,6}码付款.*?给\s*$`)
		cjkNameRe := regexp.MustCompile(`^[\p{Han}]{2,10}$`)

		for i := 0; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			if !strings.Contains(line, "扫") || !strings.Contains(line, "付款") || !strings.Contains(line, "给") {
				continue
			}

			var merchant string
			if m := titleRe.FindStringSubmatch(line); len(m) > 1 {
				merchant = sanitizePaymentField(m[1])
			} else if titleOnlyRe.MatchString(line) && i+1 < len(lines) {
				merchant = sanitizePaymentField(lines[i+1])
			}

			if merchant == "" || isWeChatBillDetailLabel(merchant) || isLikelyBankInstitution(merchant) {
				continue
			}

			// Some OCR outputs split the payee name into multiple short lines. If we only captured 1 rune,
			// try to look at the next 1-2 lines and prefer the longer plausible CJK name.
			if len([]rune(merchant)) <= 1 {
				for j := i + 1; j < len(lines) && j <= i+2; j++ {
					cand := sanitizePaymentField(lines[j])
					if cand == "" || isWeChatBillDetailLabel(cand) || isLikelyBankInstitution(cand) {
						continue
					}
					if cjkNameRe.MatchString(cand) && len([]rune(cand)) > len([]rune(merchant)) {
						merchant = cand
					} else if len([]rune(cand)) == 1 && cjkNameRe.MatchString(merchant+cand) {
						merchant = merchant + cand
					}
				}
			}

			data.Merchant = &merchant
			data.MerchantSource = "wechat_qr_payee_line"
			if len([]rune(merchant)) <= 1 {
				data.MerchantConfidence = 0.3
			} else {
				data.MerchantConfidence = 0.8
			}
			break
		}
	}

	// Some OCR outputs may place all labels first, then values.
	// When that happens, inline label parsing can still fail. Try a long label-guided scan.
	if data.Merchant == nil {
		for i, line := range lines {
			if strings.TrimSpace(line) != "商户全称" {
				continue
			}
			best := ""
			bestScore := -1
			for j := i + 1; j < len(lines) && j <= i+40; j++ {
				cand := sanitizePaymentField(lines[j])
				if merchantIsBad(cand) {
					continue
				}
				if !merchantGenericRegex.MatchString(cand) {
					continue
				}
				score := len([]rune(cand))
				if strings.Contains(cand, "有限公司") {
					score += 20
				}
				if strings.Contains(cand, "市") || strings.Contains(cand, "区") || strings.Contains(cand, "县") {
					score += 10
				}
				if strings.Contains(cand, "店") || strings.Contains(cand, "超市") || strings.Contains(cand, "餐饮") || strings.Contains(cand, "饭店") {
					score += 8
				}
				if score > bestScore {
					bestScore = score
					best = cand
				}
			}
			if best != "" {
				m := sanitizePaymentField(best)
				if m != "" && !merchantIsBad(m) {
					data.Merchant = &m
					data.MerchantSource = "wechat_fullname_label_scan"
					data.MerchantConfidence = 0.9
				}
			}
			break
		}
	}

	// Extract transaction time with support for various formats
	// Prefer label-based extraction first to avoid "label next label" mis-binding.
	timeIsBad := func(v string) bool {
		v = sanitizePaymentField(v)
		return v == "" || isWeChatBillDetailLabel(v)
	}
	if data.TransactionTime == nil {
		for _, label := range []string{"支付时间", "转账时间", "交易时间"} {
			if v, ok := extractValueByLabel(lines, label, 6, timeIsBad); ok {
				timeStr := convertChineseDateToISO(v)
				data.TransactionTime = &timeStr
				data.TransactionTimeSource = "wechat_time_label"
				data.TransactionTimeConfidence = 0.9
				break
			}
		}
	}
	timeRegexes := []*regexp.Regexp{
		// Standard format: 2024-01-01 12:00:00
		regexp.MustCompile(`支付时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2}\s[\d]{1,2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`转账时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2}\s[\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Chinese format with optional spaces after 日: matches "2025年10月23日14:59:46" and "2025年10月23日 14:59:46"
		regexp.MustCompile(`支付时间[：:]?[\s]*([\d]{4}年[\d]{1,2}月[\d]{1,2}日\s*[\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Generic Chinese date-time format with space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)\s+([\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Generic Chinese date-time format without space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)([\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Date only format
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)`),
	}
	if data.TransactionTime == nil {
		for _, re := range timeRegexes {
			if match := re.FindStringSubmatch(text); len(match) > 1 {
				timeStr := extractTimeFromMatch(match)
				data.TransactionTime = &timeStr
				data.TransactionTimeSource = "wechat_time_label"
				data.TransactionTimeConfidence = 0.9
				break
			}
		}
	}

	// Extract order number - handle both transaction and merchant order numbers
	// Prefer label-based extraction first (covers "label column then values column" layouts better).
	orderIsBad := func(v string) bool {
		v = sanitizePaymentField(v)
		if v == "" || isWeChatBillDetailLabel(v) {
			return true
		}
		// Avoid binding a date/time line as an order id.
		if strings.ContainsRune(v, '年') || strings.ContainsRune(v, '月') || strings.ContainsRune(v, '日') || strings.ContainsRune(v, ':') {
			return true
		}
		digits := 0
		for _, r := range v {
			if unicode.IsDigit(r) {
				digits++
			}
		}
		// WeChat ids are typically long numeric strings.
		return digits < 12
	}
	if data.OrderNumber == nil {
		nonDigit := regexp.MustCompile(`\D`)
		for _, label := range []string{"交易单号", "转账单号", "商户单号", "订单号", "流水号"} {
			if v, ok := extractValueByLabel(lines, label, 6, orderIsBad); ok {
				clean := strings.ReplaceAll(v, " ", "")
				clean = strings.TrimLeft(clean, "：:")
				digitsOnly := nonDigit.ReplaceAllString(clean, "")
				if len(digitsOnly) >= 12 {
					orderNum := digitsOnly
					data.OrderNumber = &orderNum
					data.OrderNumberSource = "wechat_order"
					data.OrderNumberConfidence = 0.9
					break
				}
			}
		}
	}
	// If labels exist but values are far away (labels-first layout), do a longer scan for digit-like ids.
	if data.OrderNumber == nil {
		nonDigit := regexp.MustCompile(`\D`)
		best := ""
		bestScore := -1

		scoreCandidate := func(s string) int {
			score := len(s)
			// Prefer WeChat transaction ids (often 28 digits, sometimes starting with 42).
			if len(s) >= 26 && len(s) <= 32 {
				score += 40
			}
			if strings.HasPrefix(s, "42") {
				score += 15
			}
			return score
		}

		for _, label := range []string{"交易单号", "转账单号", "商户单号", "订单号", "流水号"} {
			for i, line := range lines {
				if strings.TrimSpace(line) != label {
					continue
				}
				for j := i + 1; j < len(lines) && j <= i+60; j++ {
					cand := sanitizePaymentField(lines[j])
					if cand == "" || isWeChatBillDetailLabel(cand) {
						continue
					}
					// Avoid mistaking time/date for an order id (e.g. "2025年11月15日23:02:47").
					if strings.ContainsRune(cand, '年') || strings.ContainsRune(cand, '月') || strings.ContainsRune(cand, '日') || strings.ContainsRune(cand, ':') {
						continue
					}
					cand = strings.TrimLeft(cand, "：:")
					cand = strings.ReplaceAll(cand, " ", "")
					digits := nonDigit.ReplaceAllString(cand, "")
					if len(digits) < 16 {
						continue
					}
					if sc := scoreCandidate(digits); sc > bestScore {
						bestScore = sc
						best = digits
					}
				}
				// Keep scanning other occurrences of the same label; stop only after trying earlier labels.
			}
			if best != "" {
				orderNum := best
				data.OrderNumber = &orderNum
				data.OrderNumberSource = "wechat_order_label_scan"
				data.OrderNumberConfidence = 0.8
				break
			}
		}
	}
	orderRegexes := []*regexp.Regexp{
		regexp.MustCompile(`交易单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`商户单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`订单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`流水号[：:]?[\s]*([\d]+)`),
		// Transfer receipts sometimes wrap lines or insert spaces
		regexp.MustCompile(`转账单号[：:]?[\s]*([\d][\d\s]+)`),
	}
	if data.OrderNumber == nil {
		for _, re := range orderRegexes {
			if match := re.FindStringSubmatch(text); len(match) > 1 {
				orderNum := strings.ReplaceAll(match[1], " ", "")
				data.OrderNumber = &orderNum
				data.OrderNumberSource = "wechat_order"
				data.OrderNumberConfidence = 0.9
				break
			}
		}
	}

	// Extract actual payment method from text
	// Prefer label-based extraction first to avoid "支付方式\n当前状态" etc.
	methodIsBad := func(v string) bool {
		v = sanitizePaymentMethod(v)
		if v == "" || isWeChatBillDetailLabel(v) {
			return true
		}
		// Payment method should not be a long numeric id (barcode / transaction id).
		digits := 0
		nonDigits := 0
		for _, r := range v {
			if unicode.IsDigit(r) {
				digits++
			} else if !unicode.IsSpace(r) {
				nonDigits++
			}
		}
		if nonDigits == 0 && digits >= 12 {
			return true
		}
		return false
	}
	if data.PaymentMethod == nil {
		for _, label := range []string{"支付方式", "付款方式"} {
			if v, ok := extractValueByLabel(lines, label, 6, methodIsBad); ok {
				method := sanitizePaymentMethod(v)
				if method != "" && !isWeChatBillDetailLabel(method) {
					data.PaymentMethod = &method
					data.PaymentMethodSource = "wechat_method_label"
					data.PaymentMethodConfidence = 0.9
					break
				}
			}
		}
	}
	paymentMethodRegexes := []*regexp.Regexp{
		// 支付方式：<换行>招商银行信用卡(2506)
		regexp.MustCompile(`支付方式[：:]?\s*(?:\r?\n\s*)?([^\n\r]+?)(?:\s*由|$)`),
	}
	if data.PaymentMethod == nil {
		for _, re := range paymentMethodRegexes {
			if match := re.FindStringSubmatch(text); len(match) > 1 {
				method := sanitizePaymentMethod(strings.TrimSpace(match[1]))
				if method != "" && !isWeChatBillDetailLabel(method) {
					data.PaymentMethod = &method
					data.PaymentMethodSource = "wechat_method_label"
					data.PaymentMethodConfidence = 0.9
					break
				}
			}
		}
	}
	if data.PaymentMethod == nil {
		if m := extractBankCardPaymentMethod(text); m != nil {
			data.PaymentMethod = m
			data.PaymentMethodSource = "wechat_method_scan"
			data.PaymentMethodConfidence = 0.9
		}
	}
	// If we got a suspicious method via label binding, prefer card-like scan result.
	if data.PaymentMethod != nil {
		cur := sanitizePaymentMethod(*data.PaymentMethod)
		if methodIsBad(cur) || (!strings.Contains(cur, "卡") && !strings.Contains(cur, "银行")) {
			if m := extractBankCardPaymentMethod(text); m != nil {
				data.PaymentMethod = m
				data.PaymentMethodSource = "wechat_method_scan"
				data.PaymentMethodConfidence = 0.9
			}
		}
	}
	// If no specific payment method found, use default
	if data.PaymentMethod == nil {
		data.PaymentMethod = inferPaymentMethodFromText(text)
		if data.PaymentMethod != nil && data.PaymentMethodSource == "" {
			data.PaymentMethodSource = "wechat_infer"
			data.PaymentMethodConfidence = 0.5
		}
	}
	// If推断出支付方式但文本明确包含“支付方式”标签，则视为标签匹配提升置信度
	if data.PaymentMethod != nil && data.PaymentMethodSource == "wechat_infer" && strings.Contains(text, "支付方式") {
		data.PaymentMethodSource = "wechat_method_label"
		if data.PaymentMethodConfidence < 0.8 {
			data.PaymentMethodConfidence = 0.9
		}
	}

	// Default confidences if not set but values present
	if data.Merchant != nil && data.MerchantConfidence == 0 {
		data.MerchantConfidence = 0.6
	}
	if data.Amount != nil && data.AmountConfidence == 0 {
		data.AmountConfidence = 0.6
	}
	if data.TransactionTime != nil && data.TransactionTimeConfidence == 0 {
		data.TransactionTimeConfidence = 0.7
	}
	if data.OrderNumber != nil && data.OrderNumberConfidence == 0 {
		data.OrderNumberConfidence = 0.7
	}
	if data.PaymentMethod != nil && data.PaymentMethodConfidence == 0 {
		data.PaymentMethodConfidence = 0.6
	}
}

// parseJDBillDetail extracts fields from JD Pay bill detail screenshots.
func (s *OCRService) parseJDBillDetail(text string, data *PaymentExtractedData) {
	lines := strings.Split(text, "\n")

	// Amount: prefer negative amount shown on the page.
	if data.Amount == nil {
		if m := negativeAmountRegex.FindStringSubmatch(text); len(m) > 1 {
			if amount := parseAmount(m[1]); amount != nil && *amount >= MinValidAmount {
				data.Amount = amount
				data.AmountSource = "jd_amount"
				data.AmountConfidence = 0.9
			}
		}
	}

	// Merchant: first meaningful line near "账单详情" and before amount.
	if data.Merchant == nil {
		block := map[string]struct{}{
			"账单详情": {}, "交易成功": {}, "支付成功": {}, "更多": {}, "服务详情": {}, "其他服务": {}, "账单分类": {},
			"查看常见问题": {}, "对此账单有疑问": {},
		}
		amountLineRe := regexp.MustCompile(`^\s*[-−]\s*[¥￥]?\s*[\d,]+(?:\.\d{1,2})?\s*$`)
		foundDetail := false
		for i := 0; i < len(lines); i++ {
			line := sanitizePaymentField(lines[i])
			if line == "" {
				continue
			}
			if line == "账单详情" {
				foundDetail = true
				continue
			}
			if !foundDetail {
				continue
			}
			if amountLineRe.MatchString(line) {
				break
			}
			if _, ok := block[line]; ok {
				continue
			}
			// Skip badge-like fragments such as "5+"
			if len([]rune(line)) <= 2 && strings.ContainsAny(line, "+·•") {
				continue
			}
			if len([]rune(line)) >= 2 && len([]rune(line)) <= MaxMerchantNameLength {
				m := line
				data.Merchant = &m
				data.MerchantSource = "jd_title"
				data.MerchantConfidence = 0.85
				break
			}
		}
	}

	// Time: JD uses "创建时间" (also accept 交易时间/支付时间 if present).
	if data.TransactionTime == nil {
		timeIsBad := func(v string) bool {
			v = sanitizePaymentField(v)
			return v == "" || v == "交易成功"
		}
		for _, label := range []string{"交易时间", "支付时间", "创建时间"} {
			if v, ok := extractValueByLabel(lines, label, 6, timeIsBad); ok {
				t := convertChineseDateToISO(v)
				data.TransactionTime = &t
				data.TransactionTimeSource = "jd_time"
				data.TransactionTimeConfidence = 0.85
				break
			}
		}
	}

	// Order/transaction id: prefer 交易单号/交易号, otherwise use 商户单号, then 总订单编号.
	if data.OrderNumber == nil {
		nonDigit := regexp.MustCompile(`\D`)
		orderIsBad := func(v string) bool {
			v = sanitizePaymentField(v)
			if v == "" || strings.ContainsRune(v, '年') || strings.ContainsRune(v, ':') {
				return true
			}
			digits := nonDigit.ReplaceAllString(v, "")
			return len(digits) < 8
		}

		type candidate struct {
			label string
			src   string
		}
		cands := []candidate{
			{label: "交易单号", src: "jd_trade_no"},
			{label: "交易号", src: "jd_trade_no"},
			{label: "商户单号", src: "jd_merchant_order"},
			{label: "总订单编号", src: "jd_total_order"},
			{label: "订单编号", src: "jd_order"},
		}

		best := ""
		bestSrc := ""
		bestScore := -1
		for _, c := range cands {
			if v, ok := extractValueByLabel(lines, c.label, 6, orderIsBad); ok {
				digits := nonDigit.ReplaceAllString(v, "")
				if digits == "" {
					continue
				}
				score := len(digits)
				// Prefer longer ids (more likely to be unique transaction/merchant ids).
				if score > bestScore {
					bestScore = score
					best = digits
					bestSrc = c.src
				}
			}
		}
		if best != "" {
			order := best
			data.OrderNumber = &order
			data.OrderNumberSource = bestSrc
			data.OrderNumberConfidence = 0.85
		}
	}

	// Payment method: label-based.
	if data.PaymentMethod == nil {
		methodIsBad := func(v string) bool {
			v = sanitizePaymentMethod(v)
			return v == "" || strings.Contains(v, "账单详情") || strings.Contains(v, "交易成功")
		}
		for _, label := range []string{"支付方式", "付款方式"} {
			if v, ok := extractValueByLabel(lines, label, 6, methodIsBad); ok {
				method := sanitizePaymentMethod(v)
				if method != "" {
					data.PaymentMethod = &method
					data.PaymentMethodSource = "jd_method"
					data.PaymentMethodConfidence = 0.85
					break
				}
			}
		}
	}
}

// parseUnionPayBillDetail extracts fields from UnionPay (云闪付) bill detail screenshots.
func (s *OCRService) parseUnionPayBillDetail(text string, data *PaymentExtractedData) {
	lines := strings.Split(text, "\n")

	// Amount: prefer "订单金额", fallback to negative amount line.
	if data.Amount == nil {
		if v, ok := extractValueByLabel(lines, "订单金额", 4, nil); ok {
			if amount := parseAmount(v); amount != nil && *amount >= MinValidAmount {
				data.Amount = amount
				data.AmountSource = "unionpay_amount_label"
				data.AmountConfidence = 0.9
			}
		}
	}
	if data.Amount == nil {
		if m := negativeAmountRegex.FindStringSubmatch(text); len(m) > 1 {
			if amount := parseAmount(m[1]); amount != nil && *amount >= MinValidAmount {
				data.Amount = amount
				data.AmountSource = "unionpay_amount"
				data.AmountConfidence = 0.85
			}
		}
	}

	// Merchant: first meaningful line after "账单详情" and before the amount line.
	if data.Merchant == nil {
		block := map[string]struct{}{
			"账单详情": {}, "当前状态": {}, "交易成功": {}, "支付成功": {},
			"订单金额": {}, "付款方式": {}, "支付方式": {}, "订单时间": {}, "订单编号": {}, "商户订单号": {},
			"点击查看": {}, "点击查看>": {}, "在此商户的交易": {},
		}
		amountLineRe := regexp.MustCompile(`^\s*[-−]\s*[¥￥]?\s*[\d,]+(?:\.\d{1,2})?\s*$`)
		foundDetail := false
		for i := 0; i < len(lines); i++ {
			line := sanitizePaymentField(lines[i])
			if line == "" {
				continue
			}
			if line == "账单详情" {
				foundDetail = true
				continue
			}
			if !foundDetail {
				continue
			}
			if amountLineRe.MatchString(line) {
				break
			}
			if _, ok := block[line]; ok {
				continue
			}
			// Skip badge-like fragments.
			if len([]rune(line)) <= 2 && strings.ContainsAny(line, "+·•") {
				continue
			}
			if len([]rune(line)) >= 2 && len([]rune(line)) <= MaxMerchantNameLength {
				m := line
				data.Merchant = &m
				data.MerchantSource = "unionpay_bill_detail"
				data.MerchantConfidence = 0.9
				break
			}
		}
	}

	// Time: "订单时间" (fallback to 交易时间/支付时间).
	if data.TransactionTime == nil {
		timeIsBad := func(v string) bool {
			v = sanitizePaymentField(v)
			return v == "" || v == "交易成功" || v == "当前状态"
		}
		for _, label := range []string{"订单时间", "交易时间", "支付时间"} {
			if v, ok := extractValueByLabel(lines, label, 6, timeIsBad); ok {
				t := convertChineseDateToISO(v)
				data.TransactionTime = &t
				data.TransactionTimeSource = "unionpay_time_label"
				data.TransactionTimeConfidence = 0.9
				break
			}
		}
	}

	// Order/transaction id: prefer 商户订单号 (more useful unique id), fallback to 订单编号.
	if data.OrderNumber == nil {
		nonDigit := regexp.MustCompile(`\D`)
		orderIsBad := func(v string) bool {
			v = sanitizePaymentField(v)
			if v == "" || strings.ContainsRune(v, '年') || strings.ContainsRune(v, ':') {
				return true
			}
			digits := nonDigit.ReplaceAllString(v, "")
			return len(digits) < 8
		}
		for _, label := range []string{"商户订单号", "订单编号"} {
			if v, ok := extractValueByLabel(lines, label, 6, orderIsBad); ok {
				digits := nonDigit.ReplaceAllString(v, "")
				if digits == "" {
					continue
				}
				order := digits
				data.OrderNumber = &order
				if label == "商户订单号" {
					data.OrderNumberSource = "unionpay_merchant_order"
				} else {
					data.OrderNumberSource = "unionpay_order"
				}
				data.OrderNumberConfidence = 0.9
				break
			}
		}
	}

	// Payment method: "付款方式" (fallback to 支付方式).
	if data.PaymentMethod == nil {
		methodIsBad := func(v string) bool {
			v = sanitizePaymentMethod(v)
			return v == "" || strings.Contains(v, "账单详情") || strings.Contains(v, "交易成功")
		}
		for _, label := range []string{"付款方式", "支付方式"} {
			if v, ok := extractValueByLabel(lines, label, 6, methodIsBad); ok {
				method := sanitizePaymentMethod(v)
				if method != "" {
					data.PaymentMethod = &method
					data.PaymentMethodSource = "unionpay_method_label"
					data.PaymentMethodConfidence = 0.9
					break
				}
			}
		}
	}
}

// parseAlipay extracts Alipay information
func (s *OCRService) parseAlipay(text string, data *PaymentExtractedData) {
	lines := strings.Split(text, "\n")

	// Alipay transfer voucher ("转账凭证") is a distinct layout and should not reuse bill-detail heuristics.
	if strings.Contains(text, "转账凭证") {
		s.parseAlipayTransferVoucher(text, data)
		return
	}

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
				data.AmountSource = "alipay_amount_label"
				data.AmountConfidence = 0.9
				break
			}
		}
	}

	// Extract merchant - prioritize short names
	alipayMerchantIsBad := func(v string) bool {
		v = sanitizePaymentField(v)
		if v == "" || v == "说明" || v == "详情" {
			return true
		}
		switch v {
		case "账单详情", "交易成功", "付款成功",
			"支付时间", "付款时间", "创建时间", "交易时间",
			"支付方式", "付款方式",
			"商品说明", "查看购物详情", "服务详情", "进入小程序",
			"收单机构", "清算机构":
			return true
		}
		// Avoid "商品说明" -> "说明" style partial matches.
		if v == "商品说明" || strings.HasPrefix(v, "说明") {
			return true
		}
		return false
	}

	if data.Merchant == nil {
		if m := extractAlipayMerchantFromBillDetail(text); m != "" {
			data.Merchant = &m
			data.MerchantSource = "alipay_bill_detail"
			data.MerchantConfidence = 0.9
		}
	}

	// Labels commonly seen in Alipay receipts.
	if data.Merchant == nil {
		for _, label := range []string{"商家", "收款方"} {
			if v, ok := extractValueByLabel(lines, label, 6, alipayMerchantIsBad); ok {
				merchant := sanitizePaymentField(v)
				if !alipayMerchantIsBad(merchant) {
					data.Merchant = &merchant
					data.MerchantSource = "alipay_label"
					data.MerchantConfidence = 0.85
					break
				}
			}
		}
	}

	// 商品（仅在看起来像“商户简称/门店名”时才作为兜底）
	if data.Merchant == nil {
		itemIsBad := func(v string) bool {
			v = sanitizePaymentField(v)
			if alipayMerchantIsBad(v) {
				return true
			}
			if v == "说明" || strings.HasPrefix(v, "说明") {
				return true
			}
			return false
		}
		if v, ok := extractValueByLabel(lines, "商品", 3, itemIsBad); ok {
			item := sanitizePaymentField(v)
			if item != "" && !itemIsBad(item) {
				looksLikeMerchant := merchantGenericRegex.MatchString(item) ||
					(len([]rune(item)) >= 2 && len([]rune(item)) <= 12 && !strings.ContainsAny(item, "0123456789"))
				if looksLikeMerchant {
					data.Merchant = &item
					data.MerchantSource = "alipay_item"
					data.MerchantConfidence = 0.7
				}
			}
		}
	}

	// Lower priority: full merchant name
	if data.Merchant == nil {
		if match := merchantFullNameRegex.FindStringSubmatch(text); len(match) > 1 {
			merchant := sanitizePaymentField(match[1])
			if merchant != "" && !alipayMerchantIsBad(merchant) {
				data.Merchant = &merchant
				data.MerchantSource = "alipay_fullname"
				data.MerchantConfidence = 0.7
			}
		}
	}

	// Extract transaction time
	// Prefer label-based extraction first.
	timeIsBad := func(v string) bool {
		v = sanitizePaymentField(v)
		return v == "" || v == "账单详情"
	}
	if data.TransactionTime == nil {
		for _, label := range []string{"支付时间", "付款时间", "创建时间", "交易时间"} {
			if v, ok := extractValueByLabel(lines, label, 6, timeIsBad); ok {
				timeStr := convertChineseDateToISO(v)
				data.TransactionTime = &timeStr
				data.TransactionTimeSource = "alipay_time_label"
				data.TransactionTimeConfidence = 0.85
				break
			}
		}
	}
	timeRegexes := []*regexp.Regexp{
		// Alipay often prints "支付时间" and sometimes omits the space between date and time.
		regexp.MustCompile(`支付时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2})[\s]*([\d]{1,2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`付款时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2})[\s]*([\d]{1,2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`创建时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2})[\s]*([\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Standard format: 2024-01-01 12:00:00
		regexp.MustCompile(`支付时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2}\s[\d]{1,2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`付款时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2}\s[\d]{1,2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`创建时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2}\s[\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Chinese format with space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)\s+([\d]{1,2}:[\d]{2}:[\d]{2})`),
		// Chinese format without space
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)([\d]{1,2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`([\d]{4}年[\d]{1,2}月[\d]{1,2}日)`),
	}
	if data.TransactionTime == nil {
		for _, re := range timeRegexes {
			if match := re.FindStringSubmatch(text); len(match) > 1 {
				timeStr := extractTimeFromMatch(match)
				data.TransactionTime = &timeStr
				data.TransactionTimeSource = "alipay_time_label"
				data.TransactionTimeConfidence = 0.85
				break
			}
		}
	}

	// Extract order number
	// Prefer label-based extraction first.
	if data.OrderNumber == nil {
		orderIsBad := func(v string) bool {
			v = sanitizePaymentField(v)
			return v == "" || v == "账单详情"
		}
		for _, label := range []string{"交易单号", "交易号", "订单号", "商户单号", "流水号"} {
			if v, ok := extractValueByLabel(lines, label, 6, orderIsBad); ok {
				orderNum := strings.ReplaceAll(v, " ", "")
				orderNum = strings.TrimLeft(orderNum, "：:")
				orderNum = sanitizePaymentField(orderNum)
				if orderNum != "" {
					data.OrderNumber = &orderNum
					data.OrderNumberSource = "alipay_order"
					data.OrderNumberConfidence = 0.9
					break
				}
			}
		}
	}
	orderRegexes := []*regexp.Regexp{
		regexp.MustCompile(`交易单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`商户单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`订单号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`交易号[：:]?[\s]*([\d]+)`),
		regexp.MustCompile(`流水号[：:]?[\s]*([\d]+)`),
	}
	if data.OrderNumber == nil {
		for _, re := range orderRegexes {
			if match := re.FindStringSubmatch(text); len(match) > 1 {
				orderNum := strings.ReplaceAll(match[1], " ", "")
				data.OrderNumber = &orderNum
				data.OrderNumberSource = "alipay_order"
				data.OrderNumberConfidence = 0.9
				break
			}
		}
	}

	// Extract payment method
	// Prefer label-based extraction first.
	if data.PaymentMethod == nil {
		methodIsBad := func(v string) bool {
			v = sanitizePaymentMethod(v)
			return v == ""
		}
		for _, label := range []string{"支付方式", "付款方式"} {
			if v, ok := extractValueByLabel(lines, label, 6, methodIsBad); ok {
				method := sanitizePaymentMethod(v)
				if method != "" {
					data.PaymentMethod = &method
					data.PaymentMethodSource = "alipay_method_label"
					data.PaymentMethodConfidence = 0.85
					break
				}
			}
		}
	}
	paymentMethodRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?:支付方式|付款方式)[：:]?\s*(?:\r?\n\s*)?([^\n\r]+?)(?:\s*由|$)`),
	}
	if data.PaymentMethod == nil {
		for _, re := range paymentMethodRegexes {
			if match := re.FindStringSubmatch(text); len(match) > 1 {
				method := strings.TrimSpace(match[1])
				if method != "" {
					method = sanitizePaymentMethod(method)
					if method != "" {
						data.PaymentMethod = &method
						data.PaymentMethodSource = "alipay_method_label"
						data.PaymentMethodConfidence = 0.8
					}
					break
				}
			}
		}
	}
	// If no specific payment method found, use default
	if data.PaymentMethod == nil {
		data.PaymentMethod = inferPaymentMethodFromText(text)
		if data.PaymentMethod != nil && data.PaymentMethodSource == "" {
			data.PaymentMethodSource = "alipay_infer"
			data.PaymentMethodConfidence = 0.5
		}
	}
	if data.PaymentMethod != nil && data.PaymentMethodSource == "alipay_infer" && strings.Contains(text, "支付方式") {
		data.PaymentMethodSource = "alipay_method_label"
		if data.PaymentMethodConfidence < 0.8 {
			data.PaymentMethodConfidence = 0.9
		}
	}

	// Default confidences if missing
	if data.Merchant != nil && data.MerchantConfidence == 0 {
		data.MerchantConfidence = 0.6
	}
	if data.Amount != nil && data.AmountConfidence == 0 {
		data.AmountConfidence = 0.6
	}
	if data.TransactionTime != nil && data.TransactionTimeConfidence == 0 {
		data.TransactionTimeConfidence = 0.7
	}
	if data.OrderNumber != nil && data.OrderNumberConfidence == 0 {
		data.OrderNumberConfidence = 0.7
	}
	if data.PaymentMethod != nil && data.PaymentMethodConfidence == 0 {
		data.PaymentMethodConfidence = 0.6
	}
}

func (s *OCRService) parseAlipayTransferVoucher(text string, data *PaymentExtractedData) {
	lines := strings.Split(text, "\n")

	isVoucherLabel := func(v string) bool {
		v = sanitizePaymentField(v)
		if v == "" {
			return true
		}
		labels := map[string]struct{}{
			"转账凭证": {}, "转账凭证专用章": {}, "支付宝（中国）": {}, "支付宝(中国)": {}, "支付宝": {},
			"收款方姓名": {}, "收款方账号": {}, "收款方银行": {},
			"付款方姓名": {}, "付款方账号": {},
			"转账时间": {}, "凭证编号": {}, "转账附言": {},
			"款项已经转出成功，凭证仅供参考，请以收方账户": {}, "实际到账为准。": {},
		}
		_, ok := labels[v]
		return ok
	}

	// Amount: prefer "￥6000" near the top, fallback to generic patterns later.
	if data.Amount == nil {
		amountRegexes := []*regexp.Regexp{
			regexp.MustCompile(`[¥￥]\s*([\d,]+(?:\.\d{1,2})?)`),
			regexp.MustCompile(`([\d,]+(?:\.\d{1,2})?)元`),
		}
		for _, re := range amountRegexes {
			if m := re.FindStringSubmatch(text); len(m) > 1 {
				if amount := parseAmount(m[1]); amount != nil && *amount >= MinValidAmount {
					data.Amount = amount
					data.AmountSource = "alipay_amount_label"
					data.AmountConfidence = 0.9
					break
				}
			}
		}
	}

	// Merchant: payee name (收款方姓名)
	if data.Merchant == nil {
		bad := func(v string) bool {
			v = sanitizePaymentField(v)
			return v == "" || isVoucherLabel(v) || v == "姓名" || v == "账号" || v == "银行"
		}
		if v, ok := extractValueByLabel(lines, "收款方姓名", 10, bad); ok {
			m := sanitizePaymentField(v)
			if m != "" && !bad(m) {
				data.Merchant = &m
				data.MerchantSource = "alipay_transfer_payee"
				data.MerchantConfidence = 0.9
			}
		}
	}

	// Time: 转账时间 (may miss space between date and time)
	if data.TransactionTime == nil {
		timeIsBad := func(v string) bool {
			v = sanitizePaymentField(v)
			return v == "" || isVoucherLabel(v)
		}
		if v, ok := extractValueByLabel(lines, "转账时间", 20, timeIsBad); ok {
			t := convertChineseDateToISO(v)
			data.TransactionTime = &t
			data.TransactionTimeSource = "alipay_transfer_time"
			data.TransactionTimeConfidence = 0.9
		}
	}

	// Order number: 凭证编号 (can be split into multiple lines)
	if data.OrderNumber == nil {
		idx := indexOfExactLine(lines, "凭证编号")
		if idx >= 0 {
			parts := make([]string, 0, 3)
			partRe := regexp.MustCompile(`^[0-9]{6,}$`)
			for j := idx + 1; j < len(lines) && j <= idx+6; j++ {
				cand := sanitizePaymentField(lines[j])
				if cand == "" || isVoucherLabel(cand) {
					continue
				}
				cand = strings.ReplaceAll(cand, " ", "")
				if partRe.MatchString(cand) {
					parts = append(parts, cand)
					continue
				}
				// stop if a non-digit line is found after we started collecting
				if len(parts) > 0 {
					break
				}
			}
			if len(parts) > 0 {
				order := strings.Join(parts, "")
				data.OrderNumber = &order
				data.OrderNumberSource = "alipay_transfer_voucher_no"
				data.OrderNumberConfidence = 0.9
			}
		}
	}

	// Payment method: this is an Alipay transfer voucher.
	if data.PaymentMethod == nil {
		m := "支付宝转账"
		data.PaymentMethod = &m
		data.PaymentMethodSource = "alipay_transfer"
		data.PaymentMethodConfidence = 0.8
	}
	if data.PaymentMethod != nil && data.PaymentMethodSource == "alipay_infer" {
		// Upgrade low-confidence inference for vouchers.
		m := "支付宝转账"
		data.PaymentMethod = &m
		data.PaymentMethodSource = "alipay_transfer"
		data.PaymentMethodConfidence = 0.8
	}

	// Default confidences if missing
	if data.Merchant != nil && data.MerchantConfidence == 0 {
		data.MerchantConfidence = 0.6
	}
	if data.Amount != nil && data.AmountConfidence == 0 {
		data.AmountConfidence = 0.6
	}
	if data.TransactionTime != nil && data.TransactionTimeConfidence == 0 {
		data.TransactionTimeConfidence = 0.7
	}
	if data.OrderNumber != nil && data.OrderNumberConfidence == 0 {
		data.OrderNumberConfidence = 0.7
	}
	if data.PaymentMethod != nil && data.PaymentMethodConfidence == 0 {
		data.PaymentMethodConfidence = 0.6
	}
}

// parseBankTransfer extracts bank transfer information
func (s *OCRService) parseBankTransfer(text string, data *PaymentExtractedData) {
	isBankReceipt := strings.Contains(text, "电子回单") || strings.Contains(text, "汇款电子回单") || strings.Contains(text, "境内汇款电子回单")
	if isBankReceipt {
		lines := strings.Split(text, "\n")

		isReceiptLabel := func(v string) bool {
			v = sanitizePaymentField(v)
			if v == "" {
				return true
			}
			labels := map[string]struct{}{
				"ICBC": {}, "中国工商银行": {}, "境内汇款电子回单": {}, "电子回单": {}, "来自中国工商银行手机银行": {},
				"收款银行": {}, "收款户名": {}, "收款卡号": {}, "收款金额": {}, "手续费": {}, "合计": {},
				"付款户名": {}, "付款卡号": {}, "付款银行": {},
				"指令序号": {}, "回单编号": {}, "交易时间": {}, "附言": {},
				"重要提示": {}, "专用章": {}, "手机银行跨行汇款、跨行信用卡还款免收手续费": {},
			}
			_, ok := labels[v]
			return ok
		}

		findAfterLabel := func(label string, maxLookahead int, pred func(string) bool) (string, bool) {
			idx := indexOfExactLine(lines, label)
			if idx < 0 {
				return "", false
			}
			if maxLookahead <= 0 {
				maxLookahead = 30
			}
			for j := idx + 1; j < len(lines) && j <= idx+maxLookahead; j++ {
				cand := sanitizePaymentField(lines[j])
				if cand == "" || isReceiptLabel(cand) {
					continue
				}
				if pred == nil || pred(cand) {
					return cand, true
				}
			}
			return "", false
		}

		// 收款金额：可能被拆成多行（收款金额/手续费/合计/免费/中文大写/数字金额）。
		if data.Amount == nil {
			if idx := indexOfExactLine(lines, "收款金额"); idx >= 0 {
				best := ""
				bestVal := 0.0
				moneyDigitsRe := regexp.MustCompile(`([\d,]+(?:\.\d{1,2})?)`)
				for j := idx + 1; j < len(lines) && j <= idx+20; j++ {
					cand := sanitizePaymentField(lines[j])
					if cand == "" || isReceiptLabel(cand) {
						continue
					}
					if strings.Contains(cand, "元") || strings.Contains(cand, "人民币") || strings.ContainsRune(cand, '.') {
						if m := moneyDigitsRe.FindStringSubmatch(cand); len(m) > 1 {
							if v := parseAmount(m[1]); v != nil && *v >= MinValidAmount {
								if *v > bestVal {
									bestVal = *v
									best = m[1]
								}
							}
						}
					}
					// Stop scanning when we reach payer section.
					if strings.TrimSpace(cand) == "付款户名" {
						break
					}
				}
				if best != "" {
					if v := parseAmount(best); v != nil {
						data.Amount = v
						data.AmountSource = "bank_amount_label"
						data.AmountConfidence = 0.9
					}
				}
			}
		}

		// 收款户名（商家/收款方）
		if data.Merchant == nil {
			if idx := indexOfExactLine(lines, "收款户名"); idx >= 0 {
				best := ""
				bestScore := -1
				for j := idx + 1; j < len(lines) && j <= idx+25; j++ {
					cand := sanitizePaymentField(lines[j])
					if cand == "" || isReceiptLabel(cand) {
						continue
					}
					// Skip banks and masked numbers.
					if strings.Contains(cand, "银行") {
						continue
					}
					if strings.Contains(cand, "****") || strings.Contains(cand, "***") {
						continue
					}
					// Prefer longer plausible names (公司/中心/店/行...).
					score := len([]rune(cand))
					if strings.Contains(cand, "公司") || strings.Contains(cand, "中心") || strings.Contains(cand, "店") || strings.Contains(cand, "行") {
						score += 20
					}
					if score > bestScore {
						bestScore = score
						best = cand
					}
					// Stop if we reach amount section.
					if strings.TrimSpace(cand) == "收款金额" {
						break
					}
				}
				if best != "" {
					m := best
					data.Merchant = &m
					data.MerchantSource = "bank_label"
					data.MerchantConfidence = 0.85
				}
			}
		}

		// 交易时间
		if data.TransactionTime == nil {
			dateTimeRe := regexp.MustCompile(`\d{4}[-/]\d{1,2}[-/]\d{1,2}\s+\d{1,2}:\d{2}(?::\d{2})?`)
			if v, ok := findAfterLabel("交易时间", 50, func(s string) bool { return dateTimeRe.MatchString(s) }); ok {
				t := convertChineseDateToISO(v)
				data.TransactionTime = &t
				data.TransactionTimeSource = "bank_time_label"
				data.TransactionTimeConfidence = 0.9
			}
		}

		// 回单编号/指令序号
		if data.OrderNumber == nil {
			receiptNoRe := regexp.MustCompile(`^[A-Za-z]{2,}[A-Za-z0-9-]{6,}$`)
			if v, ok := findAfterLabel("回单编号", 60, func(s string) bool { return receiptNoRe.MatchString(strings.ReplaceAll(s, " ", "")) }); ok {
				order := sanitizePaymentField(strings.ReplaceAll(v, " ", ""))
				if order != "" {
					data.OrderNumber = &order
					data.OrderNumberSource = "bank_order_label"
					data.OrderNumberConfidence = 0.9
				}
			}
		}
		if data.OrderNumber == nil {
			digitsRe := regexp.MustCompile(`^\d{12,}$`)
			if v, ok := findAfterLabel("指令序号", 60, func(s string) bool { return digitsRe.MatchString(strings.ReplaceAll(s, " ", "")) }); ok {
				order := sanitizePaymentField(strings.ReplaceAll(v, " ", ""))
				if order != "" {
					data.OrderNumber = &order
					data.OrderNumberSource = "bank_order_label"
					data.OrderNumberConfidence = 0.85
				}
			}
		}

		// 付款银行 + 尾号（付款卡号）
		if data.PaymentMethod == nil {
			cardTail := ""
			tailRe := regexp.MustCompile(`(\d{4})$`)
			// Prefer payer card tail ("付款卡号") instead of payee card tail.
			if idx := indexOfExactLine(lines, "付款卡号"); idx >= 0 {
				for j := idx + 1; j < len(lines) && j <= idx+10; j++ {
					s := sanitizePaymentField(lines[j])
					if s == "" || isReceiptLabel(s) {
						continue
					}
					if strings.Contains(s, "****") {
						if m := tailRe.FindStringSubmatch(s); len(m) > 1 {
							cardTail = m[1]
							break
						}
					}
				}
			}
			if cardTail == "" {
				for _, raw := range lines {
					s := sanitizePaymentField(raw)
					if strings.Contains(s, "****") {
						if m := tailRe.FindStringSubmatch(s); len(m) > 1 {
							cardTail = m[1]
							break
						}
					}
				}
			}
			bankName := ""
			// Prefer explicit "付款银行" nearby.
			if idx := indexOfExactLine(lines, "付款银行"); idx >= 0 {
				for j := idx + 1; j < len(lines) && j <= idx+10; j++ {
					cand := sanitizePaymentField(lines[j])
					if cand == "" || isReceiptLabel(cand) {
						continue
					}
					if strings.Contains(cand, "银行") {
						bankName = cand
						break
					}
				}
			}
			if bankName == "" {
				for _, raw := range lines {
					cand := sanitizePaymentField(raw)
					if strings.Contains(cand, "银行") && !strings.Contains(cand, "收款银行") {
						bankName = cand
						break
					}
				}
			}
			if bankName != "" && cardTail != "" {
				method := fmt.Sprintf("%s(%s)", bankName, cardTail)
				method = sanitizePaymentMethod(method)
				data.PaymentMethod = &method
				data.PaymentMethodSource = "bank_method_label"
				data.PaymentMethodConfidence = 0.8
			}
		}

		// Do not fall through to generic bank transfer parsing (it may mis-bind amounts from ids).
		return
	}

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
				data.AmountSource = "bank_amount_label"
				data.AmountConfidence = 0.85
				break
			}
		}
	}

	// Extract receiver
	merchantRegexes := []*regexp.Regexp{
		// NOTE: Require delimiter to avoid matching "商品说明" -> "说明".
		regexp.MustCompile(`商品[：:][\s]*([^\s(（\n]+)`),
		regexp.MustCompile(`商品[\s]+([^\s(（\n]+)`),
		regexp.MustCompile(`收款人[：:]?[\s]*([^\s¥￥\n]+)`),
		regexp.MustCompile(`收款账户[：:]?[\s]*([^\s¥￥\n]+)`),
		merchantFullNameRegex,
	}
	for _, re := range merchantRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			merchant := sanitizePaymentField(match[1])
			if merchant == "" || merchant == "说明" {
				continue
			}
			data.Merchant = &merchant
			data.MerchantSource = "bank_label"
			break
		}
	}

	// Extract transaction time
	timeRegexes := []*regexp.Regexp{
		regexp.MustCompile(`转账时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2}\s[\d]{1,2}:[\d]{2}:[\d]{2})`),
		regexp.MustCompile(`交易时间[：:]?[\s]*([\d]{4}-[\d]{1,2}-[\d]{1,2}\s[\d]{1,2}:[\d]{2}:[\d]{2})`),
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
			data.TransactionTimeSource = "bank_time_label"
			data.TransactionTimeConfidence = 0.85
			break
		}
	}

	// Extract payment method
	paymentMethodRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?:支付方式|付款方式)[：:]?\s*(?:\r?\n\s*)?([^\n\r]+?)(?:\s*由|$)`),
	}
	for _, re := range paymentMethodRegexes {
		if match := re.FindStringSubmatch(text); len(match) > 1 {
			method := strings.TrimSpace(match[1])
			if method != "" {
				method = sanitizePaymentMethod(method)
				if method != "" {
					data.PaymentMethod = &method
					data.PaymentMethodSource = "bank_method_label"
					data.PaymentMethodConfidence = 0.7
				}
				break
			}
		}
	}
	// If no specific payment method found, use default
	if data.PaymentMethod == nil {
		data.PaymentMethod = inferPaymentMethodFromText(text)
		if data.PaymentMethod != nil && data.PaymentMethodSource == "" {
			data.PaymentMethodSource = "bank_infer"
			data.PaymentMethodConfidence = 0.5
		}
	}
	if data.PaymentMethod != nil && data.PaymentMethodSource == "bank_infer" && strings.Contains(text, "支付方式") {
		data.PaymentMethodSource = "bank_method_label"
		if data.PaymentMethodConfidence < 0.7 {
			data.PaymentMethodConfidence = 0.9
		}
	}

	// Default confidences if missing
	if data.Merchant != nil && data.MerchantConfidence == 0 {
		data.MerchantConfidence = 0.6
	}
	if data.Amount != nil && data.AmountConfidence == 0 {
		data.AmountConfidence = 0.6
	}
	if data.TransactionTime != nil && data.TransactionTimeConfidence == 0 {
		data.TransactionTimeConfidence = 0.7
	}
	if data.OrderNumber != nil && data.OrderNumberConfidence == 0 {
		data.OrderNumberConfidence = 0.7
	}
	if data.PaymentMethod != nil && data.PaymentMethodConfidence == 0 {
		data.PaymentMethodConfidence = 0.6
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
			m = sanitizePaymentMethod(m)
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

func sanitizePaymentMethod(s string) string {
	s = sanitizePaymentField(s)
	// Normalize fullwidth parentheses commonly seen in Chinese UIs.
	s = strings.NewReplacer("（", "(", "）", ")").Replace(s)
	// Alipay/WeChat UI arrows often appear as trailing ">" or similar.
	s = strings.TrimRight(s, ">›»〉》→")
	s = strings.TrimSpace(s)
	return s
}

func sanitizePaymentField(s string) string {
	s = paymentInvisibleSpaceReplacer.Replace(s)
	s = strings.TrimSpace(s)
	// Normalize whitespace
	s = strings.Join(strings.Fields(s), " ")
	// Trim common trailing UI artifacts
	s = strings.TrimRight(s, ">›»〉》→")
	s = strings.TrimSpace(s)
	return s
}

func extractAlipayMerchantFromBillDetail(text string) string {
	lines := strings.Split(text, "\n")

	blocklist := []string{
		"账单详情",
		"交易成功",
		"支付时间",
		"付款时间",
		"付款方式",
		"支付方式",
		"商品说明",
		"查看购物详情",
		"收单机构",
		"清算机构",
		"服务详情",
		"进入小程序",
		"推荐服务",
		"账单管理",
		"账单分类",
	}

	timeOnlyRe := regexp.MustCompile(`^\d{1,2}:\d{2}$`)
	datePrefixRe := regexp.MustCompile(`^\d{4}[-/年]\d{1,2}[-/月]\d{1,2}`)
	amountOnlyRe := regexp.MustCompile(`^[-−]?\s*[¥￥]?\s*\d+(?:,\d{3})*(?:\.\d{1,2})?$`)

	for i, line := range lines {
		if !strings.Contains(line, "账单详情") {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+12; j++ {
			s := strings.TrimSpace(lines[j])
			if s == "" {
				continue
			}
			if timeOnlyRe.MatchString(s) || datePrefixRe.MatchString(s) {
				continue
			}
			s = sanitizePaymentField(s)
			if amountOnlyRe.MatchString(s) {
				continue
			}
			if s == "" || s == "说明" || s == "详情" {
				continue
			}
			skip := false
			for _, b := range blocklist {
				if strings.Contains(s, b) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			if len([]rune(s)) >= 2 && len([]rune(s)) <= MaxMerchantNameLength {
				return s
			}
		}
	}
	return ""
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
				if data.AmountSource == "" {
					data.AmountSource = "generic_amount"
				}
				if data.AmountConfidence == 0 {
					data.AmountConfidence = 0.4
				}
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
			if data.MerchantSource == "" {
				data.MerchantSource = "generic_merchant_suffix"
				data.MerchantConfidence = 0.4
			}
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

	// Strip common leading labels that occasionally get included in the captured "name".
	// Examples:
	// - "购买方名称：XXX" / "销售方名称：XXX"
	// - "销售方:(章) XXX"
	// - "购买方：XXX" / "销售方：XXX"
	name = regexp.MustCompile(`^(?:购买方|销售方)\s*[:：]?\s*(?:(?:\(\s*章\s*\))|(?:（\s*章\s*）))?\s*`).ReplaceAllString(name, "")
	name = regexp.MustCompile(`^(?:购买方名称|销售方名称)\s*[:：]?\s*`).ReplaceAllString(name, "")
	name = strings.TrimSpace(name)

	// Split by multiple spaces (2 or more) and take the first part
	// This handles cases where the name is followed by other content with significant spacing
	parts := regexp.MustCompile(`\s{2,}`).Split(name, 2)
	if len(parts) > 0 {
		name = parts[0]
	}

	// Strip leading personal name when a city-prefixed company name follows (e.g. "杜洪亮上海滴滴畅行科技有限公司").
	// Keep this conservative to avoid damaging legitimate company names like "中国移动通信集团..." or address-prefixed sellers.
	if regexp.MustCompile(`(?:有限责任公司|有限公司|公司|集团|商店|企业|中心|厂|店|行|社|院|局)`).MatchString(name) {
		if m := regexp.MustCompile(`^([\p{Han}]{2,3})\s*([\p{Han}].+)$`).FindStringSubmatch(name); len(m) > 2 {
			rest := strings.TrimSpace(m[2])
			if regexp.MustCompile(`^(?:上海|北京|天津|重庆|广州|深圳|杭州|南京|苏州|成都|武汉|西安|郑州|长沙|青岛|厦门|宁波|无锡|沈阳|大连|佛山|东莞|昆明|合肥|福州|济南|南昌|南宁|石家庄|太原|哈尔滨|长春)`).MatchString(rest) {
				name = rest
			}
		}
	}

	// If a tax ID appears in the same token stream, keep only the part before it.
	if loc := taxIDRegex.FindStringIndex(name); loc != nil && loc[0] > 0 {
		name = strings.TrimSpace(name[:loc[0]])
	}

	// Strip trailing stamp labels like "销售方:(章)" / "销售方:（章）".
	name = regexp.MustCompile(`\s*销售方\s*[:：]?\s*(?:(?:\(\s*章\s*\))|(?:（\s*章\s*）))\s*$`).ReplaceAllString(name, "")
	name = strings.TrimSpace(name)

	// If an 11-digit phone appears right after a Chinese name, keep only the name.
	if m := regexp.MustCompile(`^([\p{Han}]{2,20})\s*\d{11}\b`).FindStringSubmatch(name); len(m) > 1 {
		name = strings.TrimSpace(m[1])
	}
	// Masked phone patterns like "135******11" sometimes appear in redacted regression samples.
	if m := regexp.MustCompile(`^([\p{Han}]{2,20})\s*\d{3}\*+\d{2,8}\b`).FindStringSubmatch(name); len(m) > 1 {
		name = strings.TrimSpace(m[1])
	}
	if m := regexp.MustCompile(`^([\p{Han}]{2,20})\d{3}\*+\d{2,8}\b`).FindStringSubmatch(name); len(m) > 1 {
		name = strings.TrimSpace(m[1])
	}

	// Remove trailing single characters that are likely markers
	trailingPatterns := []string{"销", "售", "购", "买", "方", "信", "息", "密", "码", "区"}
	for _, pattern := range trailingPatterns {
		name = strings.TrimSuffix(name, pattern)
		name = strings.TrimSpace(name)
	}

	// Remove trailing whitespace and newlines
	name = strings.TrimRight(name, " \t\n\r")

	// Trim common trailing separators that appear in OCR (e.g. "乌洪军 /").
	name = strings.TrimRightFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || r == '/' || r == '／' || r == '|' || r == '｜'
	})
	name = strings.TrimSpace(name)

	return name
}

func isGarbagePartyName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	// Common merged-label artifacts in PDF text extraction.
	if strings.Contains(name, "销售方信息") || strings.Contains(name, "购买方信息") {
		return true
	}
	if strings.Contains(name, "信息名称") || strings.Contains(name, "名称：") || strings.Contains(name, "名称:") {
		return true
	}
	// Mojibake/replacement characters.
	if strings.ContainsRune(name, '\uFFFD') {
		return true
	}
	return false
}

func ptrToString(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func cleanPartyNameFromInlineValue(val string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return ""
	}
	// Strip replacement characters that sometimes appear in PDF->text.
	val = strings.ReplaceAll(val, "\uFFFD", "")
	val = strings.TrimSpace(val)

	// Cut at common labels that may be merged into the same line.
	cutMarkers := []string{
		"销售方信息", "销售方", "购买方信息", "购买方",
		"统一社会信用代码", "纳税人识别号",
		"货物或应税劳务", "服务名称",
		"地址", "电话", "开户地址", "开户行", "开户行及账号", "账号",
		"项目名称", "规格型号", "单位", "数量", "单价", "金额", "税率", "税额",
		"下载次数",
	}
	for _, m := range cutMarkers {
		if idx := strings.Index(val, m); idx > 0 {
			val = strings.TrimSpace(val[:idx])
		}
	}
	val = cleanupName(val)
	if val == "" {
		return ""
	}
	// Some PDFs end the party line with "电话: 个人" etc; treat "个人" as a valid buyer name.
	if val == "个人" {
		return val
	}
	if isBadPartyNameCandidate(val) {
		return ""
	}
	return val
}

func extractCompanyNameNearTaxID(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// We intentionally do not use word boundaries here because PDF text may glue tokens together.
	taxLoose := regexp.MustCompile(taxIDPattern)
	companyLike := regexp.MustCompile(`([\p{Han}（）()·]{2,80}(?:有限责任公司|有限公司|公司|集团|银行|商店|企业|中心|厂|店|行|社|院|局)[\p{Han}（）()·]{0,30})`)

	best := ""
	bestLen := 0
	bestTaxPos := -1
	for _, loc := range taxLoose.FindAllStringIndex(text, -1) {
		if len(loc) < 2 {
			continue
		}
		// Look back a bit; seller name is usually close to the tax-id.
		// Use a rune-based window to avoid cutting multi-byte runes mid-sequence.
		prefix := text[:loc[0]]
		runes := []rune(prefix)
		if len(runes) > 520 {
			runes = runes[len(runes)-520:]
		}
		window := string(runes)

		matches := companyLike.FindAllStringSubmatch(window, -1)
		if len(matches) == 0 {
			continue
		}
		// Prefer the longest company-like match in the window.
		// Some strings contain nested matches (e.g. "北京易行出行旅游有限公司" also matches "旅游有限公司"),
		// and picking the "last" occurrence would incorrectly truncate the company name.
		cand := ""
		candLen := 0
		for _, mm := range matches {
			if len(mm) < 2 {
				continue
			}
			v := cleanupName(strings.TrimSpace(mm[1]))
			v = strings.TrimSpace(v)
			if v == "" || v == "个人" || isBadPartyNameCandidate(v) {
				continue
			}
			l := len([]rune(v))
			if l > candLen {
				cand = v
				candLen = l
			}
		}
		if cand == "" {
			continue
		}
		// Strip common table/header prefixes that may be adjacent in PDF text.
		cand = regexp.MustCompile(`^(?:单价|金额|税率/征收率|税率|税额|下载次数)+`).ReplaceAllString(cand, "")
		cand = strings.TrimSpace(cand)
		if cand == "" || cand == "个人" || isBadPartyNameCandidate(cand) {
			continue
		}
		l := len([]rune(cand))
		// Prefer the company name near the later tax-id (seller is usually later in the document),
		// then prefer longer names.
		if loc[0] > bestTaxPos || (loc[0] == bestTaxPos && l > bestLen) {
			best = cand
			bestLen = l
			bestTaxPos = loc[0]
		}
	}
	if best != "" {
		return best
	}

	// Fallback: line-based search (more robust for heavily segmented/zoned text).
	// Walk from bottom to top so we prefer seller-side tax IDs that usually appear later.
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if !taxLoose.MatchString(line) {
			continue
		}
		for j := i - 1; j >= 0 && j >= i-8; j-- {
			candLine := strings.TrimSpace(lines[j])
			if candLine == "" {
				continue
			}
			if strings.Contains(candLine, "密码区") || strings.Contains(candLine, "明细") || strings.Contains(candLine, "购买方") || strings.Contains(candLine, "销售方") {
				continue
			}
			if m := companyLike.FindStringSubmatch(candLine); len(m) > 1 {
				cand := cleanupName(strings.TrimSpace(m[1]))
				if cand != "" && cand != "个人" && !isBadPartyNameCandidate(cand) {
					return cand
				}
			}
		}
	}
	return ""
}

func extractNameFromTaxIDLabelLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	idx := strings.LastIndex(line, "纳税人识别号")
	if idx == -1 {
		idx = strings.LastIndex(line, "统一社会信用代码")
	}
	if idx == -1 {
		return ""
	}
	rest := line[idx:]
	col := strings.IndexAny(rest, ":：")
	if col == -1 || col+1 >= len(rest) {
		return ""
	}
	val := strings.TrimSpace(rest[col+1:])
	if val == "" {
		return ""
	}
	// Prefer cutting at a tax ID if present.
	if loc := taxIDRegex.FindStringIndex(val); loc != nil && loc[0] > 0 {
		val = strings.TrimSpace(val[:loc[0]])
	}
	// Or cut at a phone number.
	if m := regexp.MustCompile(`^([\p{Han}]{2,20})\s*\d{11}\b`).FindStringSubmatch(val); len(m) > 1 {
		val = strings.TrimSpace(m[1])
	}
	// Or cut at masked phones like "135******11".
	if m := regexp.MustCompile(`^([\p{Han}]{2,20})\s*\d{3}\*+\d{2,8}\b`).FindStringSubmatch(val); len(m) > 1 {
		val = strings.TrimSpace(m[1])
	}
	if m := regexp.MustCompile(`^([\p{Han}]{2,20})\d{3}\*+\d{2,8}\b`).FindStringSubmatch(val); len(m) > 1 {
		val = strings.TrimSpace(m[1])
	}
	// Cut at merged table header fragments if present.
	if idx := strings.Index(val, "数量"); idx > 0 {
		val = strings.TrimSpace(val[:idx])
	}
	if idx := strings.Index(val, "单价"); idx > 0 {
		val = strings.TrimSpace(val[:idx])
	}
	// If still contains long digit runs (accounts/phones), cut before them.
	if loc := regexp.MustCompile(`\d{6,}`).FindStringIndex(val); loc != nil && loc[0] > 0 {
		val = strings.TrimSpace(val[:loc[0]])
	}
	return cleanupName(val)
}

func extractSellerNameFromPDFZones(pages []PDFTextZonesPage) string {
	best := ""
	bestScore := -1

	for _, p := range pages {
		h := p.Height
		if h <= 0 {
			h = 1
		}
		for _, row := range p.Rows {
			rowText := strings.TrimSpace(row.Text)
			if rowText == "" && len(row.Spans) > 0 {
				parts := make([]string, 0, len(row.Spans))
				for _, sp := range row.Spans {
					if t := strings.TrimSpace(sp.T); t != "" {
						parts = append(parts, t)
					}
				}
				rowText = strings.TrimSpace(strings.Join(parts, " "))
			}
			if rowText == "" {
				continue
			}

			cand := ""
			if taxIDRegex.MatchString(rowText) {
				cand = extractCompanyNameNearTaxID(rowText)
			}
			if cand == "" {
				continue
			}
			cand = cleanupName(strings.TrimSpace(cand))
			if cand == "" || cand == "个人" || isBadPartyNameCandidate(cand) {
				continue
			}

			score := len([]rune(cand))
			score += 60 // tax-id context
			switch strings.ToLower(strings.TrimSpace(row.Region)) {
			case "password", "seller", "header_right":
				score += 10
			}
			if row.Y0/h < 0.55 {
				score += 6
			}
			if score > bestScore {
				best = cand
				bestScore = score
			}
		}
	}

	return best
}

func extractInvoiceLineItemsFromPDFZones(pages []PDFTextZonesPage) []InvoiceLineItem {
	type spanItem struct {
		PDFTextZonesSpan
		xc float64
		yc float64
		h  float64
	}

	flattenItemsSpans := func() (out []spanItem, pageW float64) {
		for _, p := range pages {
			if p.Page != 1 {
				continue
			}
			pageW = p.Width
			pageH := p.Height
			if pageH <= 0 {
				pageH = 1
			}
			for _, r := range p.Rows {
				region := strings.ToLower(strings.TrimSpace(r.Region))
				// Some templates place the first line item (or its prefix) close to the buyer block,
				// so include both buyer+items and later filter by table/header detection.
				if region != "items" && region != "buyer" {
					continue
				}
				for _, sp := range r.Spans {
					t := strings.TrimSpace(sp.T)
					if t == "" {
						continue
					}
					yr := ((sp.Y0 + sp.Y1) / 2.0) / pageH
					// Exclude header and footer blocks that commonly contain party info / stamps.
					if yr < 0.20 || yr > 0.86 {
						continue
					}
					h := sp.Y1 - sp.Y0
					out = append(out, spanItem{
						PDFTextZonesSpan: sp,
						xc:               (sp.X0 + sp.X1) / 2.0,
						yc:               (sp.Y0 + sp.Y1) / 2.0,
						h:                h,
					})
				}
			}
			break
		}
		return out, pageW
	}

	spans, pageW := flattenItemsSpans()
	if len(spans) == 0 {
		return nil
	}
	if pageW <= 0 {
		pageW = 1
	}

	// Cluster spans into rows (more reliable than the python-side row clustering for tightly packed tables).
	hs := make([]float64, 0, len(spans))
	for _, s := range spans {
		if s.h > 0 {
			hs = append(hs, s.h)
		}
	}
	sort.Float64s(hs)
	medianH := 10.0
	if len(hs) > 0 {
		mid := len(hs) / 2
		if len(hs)%2 == 1 {
			medianH = hs[mid]
		} else if mid > 0 {
			medianH = (hs[mid-1] + hs[mid]) / 2.0
		}
	}
	rowTol := math.Max(3.0, medianH*0.45)

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Y0 == spans[j].Y0 {
			return spans[i].X0 < spans[j].X0
		}
		return spans[i].Y0 < spans[j].Y0
	})

	rows := make([][]spanItem, 0, 32)
	var cur []spanItem
	curMaxY := 0.0
	for _, s := range spans {
		if len(cur) == 0 {
			cur = []spanItem{s}
			curMaxY = s.Y1
			continue
		}
		// Start a new row when the next span is clearly below the current row.
		if s.Y0 <= curMaxY+rowTol {
			cur = append(cur, s)
			if s.Y1 > curMaxY {
				curMaxY = s.Y1
			}
			continue
		}
		rows = append(rows, cur)
		cur = []spanItem{s}
		curMaxY = s.Y1
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	if len(rows) == 0 {
		return nil
	}

	rowText := func(r []spanItem) string {
		sort.Slice(r, func(i, j int) bool { return r[i].X0 < r[j].X0 })
		parts := make([]string, 0, len(r))
		for _, sp := range r {
			t := strings.TrimSpace(sp.T)
			if t != "" {
				parts = append(parts, t)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	}

	// Detect header row and column boundaries.
	type col struct {
		key    string
		center float64
	}
	headerLabels := map[string][]string{
		"name": {"项目名称", "货物或应税劳务", "服务名称"},
		"spec": {"规格型号"},
		"unit": {"单位"},
		"qty":  {"数量"},
	}
	headerIdx := -1
	bestHits := -1
	for i := 0; i < len(rows) && i < 18; i++ {
		txt := rowText(rows[i])
		if txt == "" {
			continue
		}
		hits := 0
		for _, labels := range headerLabels {
			for _, l := range labels {
				if strings.Contains(txt, l) {
					hits++
					break
				}
			}
		}
		if hits > bestHits {
			bestHits = hits
			headerIdx = i
		}
	}

	cols := make([]col, 0, 4)
	if headerIdx >= 0 && bestHits >= 2 {
		r := rows[headerIdx]
		for key, labels := range headerLabels {
			bestX := -1.0
			for _, sp := range r {
				t := strings.TrimSpace(sp.T)
				if t == "" {
					continue
				}
				for _, l := range labels {
					if strings.Contains(t, l) {
						bestX = sp.xc
						break
					}
				}
				if bestX >= 0 {
					break
				}
			}
			if bestX >= 0 {
				cols = append(cols, col{key: key, center: bestX})
			}
		}
		sort.Slice(cols, func(i, j int) bool { return cols[i].center < cols[j].center })
	}

	// Build boundaries: either from header centers, or fallback to a typical VAT invoice table layout.
	type boundaries struct {
		nameEnd float64
		specEnd float64
		unitEnd float64
		qtyEnd  float64
	}
	bounds := boundaries{
		nameEnd: 0.47 * pageW,
		specEnd: 0.62 * pageW,
		unitEnd: 0.70 * pageW,
		qtyEnd:  0.79 * pageW,
	}
	if len(cols) >= 2 {
		// Use midpoints between detected columns.
		// Determine name->spec->unit->qty in order if present.
		colPos := map[string]float64{}
		for _, c := range cols {
			colPos[c.key] = c.center
		}
		if a, okA := colPos["name"]; okA {
			if b, okB := colPos["spec"]; okB {
				bounds.nameEnd = (a + b) / 2
			}
		}
		if a, okA := colPos["spec"]; okA {
			if b, okB := colPos["unit"]; okB {
				bounds.specEnd = (a + b) / 2
			}
		}
		if a, okA := colPos["unit"]; okA {
			if b, okB := colPos["qty"]; okB {
				bounds.unitEnd = (a + b) / 2
			}
		}
		if q, okQ := colPos["qty"]; okQ {
			bounds.qtyEnd = math.Min(pageW, q+0.08*pageW)
		}
	}

	assignCol := func(x float64) string {
		switch {
		case x <= bounds.nameEnd:
			return "name"
		case x <= bounds.specEnd:
			return "spec"
		case x <= bounds.unitEnd:
			return "unit"
		case x <= bounds.qtyEnd:
			return "qty"
		default:
			return "other"
		}
	}

	parseQty := func(s string) *float64 {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		m := regexp.MustCompile(`-?\d+(?:\.\d+)?`).FindString(s)
		if m == "" {
			return nil
		}
		if f, err := strconv.ParseFloat(m, 64); err == nil {
			return &f
		}
		return nil
	}

	isStop := func(s string) bool {
		s = strings.TrimSpace(s)
		if s == "" {
			return false
		}
		if strings.Contains(s, "价税合计") {
			return true
		}
		if strings.HasPrefix(s, "合计") || strings.Contains(s, " 合计") {
			return true
		}
		if strings.Contains(s, "销售方") || strings.Contains(s, "购买方") {
			return true
		}
		return false
	}

	looksLikeItemRow := func(r []spanItem) bool {
		qtyRe := regexp.MustCompile(`^\d+(?:\.\d+)?$`)
		unitSet := map[string]bool{
			"\u4e2a": true, // 个
			"\u74f6": true, // 瓶
			"\u5305": true, // 包
			"\u6b21": true, // 次
			"\u5957": true, // 套
			"\u7ec4": true, // 组
			"\u5f20": true, // 张
			"\u4ef6": true, // 件
			"\u76d2": true, // 盒
			"\u4efd": true, // 份
			"\u53f0": true, // 台
			"\u8f86": true, // 辆
		}
		hasQty := false
		hasUnit := false
		for _, sp := range r {
			t := strings.TrimSpace(sp.T)
			if t == "" {
				continue
			}
			if len([]rune(t)) <= 2 && unitSet[t] && sp.X0 >= 0.55*pageW {
				hasUnit = true
			}
			if qtyRe.MatchString(t) && sp.X0 >= 0.62*pageW {
				hasQty = true
			}
		}
		return hasQty && hasUnit
	}

	start := 0
	if headerIdx >= 0 && bestHits >= 2 {
		start = headerIdx + 1
	}
	if start == 0 && (headerIdx < 0 || bestHits < 2) {
		// If the header row is missing, skip leading buyer/header clutter until we see a row that looks
		// like a real line item (unit+quantity columns present).
		for i := 0; i < len(rows); i++ {
			if looksLikeItemRow(rows[i]) {
				start = i
				break
			}
		}
	}

	out := make([]InvoiceLineItem, 0, 8)
	for i := start; i < len(rows); i++ {
		txt := rowText(rows[i])
		if txt == "" {
			continue
		}
		if isStop(txt) {
			break
		}

		cells := map[string][]string{
			"name": {},
			"spec": {},
			"unit": {},
			"qty":  {},
		}
		for _, sp := range rows[i] {
			t := strings.TrimSpace(sp.T)
			if t == "" {
				continue
			}
			key := assignCol(sp.X0)
			if key == "other" {
				continue
			}
			cells[key] = append(cells[key], t)
		}

		name := strings.TrimSpace(strings.Join(cells["name"], " "))
		spec := strings.TrimSpace(strings.Join(cells["spec"], " "))
		unit := strings.TrimSpace(strings.Join(cells["unit"], " "))
		qty := parseQty(strings.Join(cells["qty"], " "))

		normalizeCell := func(s string) string {
			s = strings.TrimSpace(s)
			if s == "" {
				return ""
			}
			return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
		}
		name = normalizeCell(name)
		spec = normalizeCell(spec)
		unit = normalizeCell(unit)

		if name == "" {
			continue
		}
		if strings.Contains(name, "项目名称") || strings.Contains(name, "货物或应税劳务") || strings.Contains(name, "服务名称") {
			continue
		}

		// Handle continuation rows: no numeric columns, just a wrapped name/spec line.
		if len(out) > 0 && qty == nil && strings.TrimSpace(unit) == "" {
			// If spec is empty and this looks like a model code, treat as spec.
			if out[len(out)-1].Spec == "" && regexp.MustCompile(`(?i)[A-Z]{1,3}[-/A-Z0-9]{3,}`).MatchString(name) {
				out[len(out)-1].Spec = name
				continue
			}
			out[len(out)-1].Name = strings.TrimSpace(out[len(out)-1].Name + " " + name)
			if out[len(out)-1].Spec == "" && spec != "" {
				out[len(out)-1].Spec = spec
			}
			continue
		}

		item := InvoiceLineItem{
			Name:     strings.TrimSpace(name),
			Spec:     strings.TrimSpace(spec),
			Unit:     strings.TrimSpace(unit),
			Quantity: qty,
		}
		out = append(out, item)
	}

	// Final cleanup: drop obviously invalid rows.
	clean := make([]InvoiceLineItem, 0, len(out))
	for _, it := range out {
		n := strings.TrimSpace(it.Name)
		if n == "" || isStop(n) {
			continue
		}
		clean = append(clean, it)
	}
	return clean
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
	// Single-character picks are almost always OCR/segmentation leftovers (e.g. "地").
	if len([]rune(name)) <= 1 {
		return true
	}
	// Common numeric/table labels that may be mistakenly picked as a party name.
	// Keep this conservative: only reject short, pure-label values to avoid blocking real company names
	// that contain these words as prefixes (we often strip them in later heuristics).
	switch name {
	case "金额", "单价", "税额", "税率", "征收率", "税率/征收率":
		return true
	}
	if len([]rune(name)) <= 6 {
		if strings.Contains(name, "金额") || strings.Contains(name, "单价") || strings.Contains(name, "税额") || strings.Contains(name, "税率") || strings.Contains(name, "征收率") {
			return true
		}
		if strings.Contains(name, "%") && (strings.Contains(name, "税") || strings.Contains(name, "征收")) {
			return true
		}
	}
	// Footer/stamp fields often get concatenated into a "name" by PDF text extraction.
	if strings.Contains(name, "销售方:(章)") || strings.Contains(name, "销售方:（章）") || strings.Contains(name, "销售方：（章）") ||
		strings.Contains(name, "开票人") || strings.Contains(name, "复核") || strings.Contains(name, "收款人") || strings.Contains(name, "备注") {
		return true
	}
	// Any embedded buyer/seller labels indicate this isn't a pure party name.
	if strings.Contains(name, "销售方") || strings.Contains(name, "购买方") {
		return true
	}
	// Truncated label fragments (OCR may drop the last character like "统一社会信用代").
	if strings.Contains(name, "统一社会信用") || strings.Contains(name, "纳税人识别") {
		return true
	}
	// Pure punctuation/ellipsis-like placeholders.
	if strings.Trim(name, ".·•…") == "" {
		return true
	}
	// Common label fragments that sometimes get captured as a "name".
	bad := []string{
		"名称", "名称:", "名称：",
		"项目名称", "货物或应税劳务、服务名称",
		"购买方", "销售方", "购买", "销售",
		"纳税人识别号", "纳税人识别号:", "纳税人识别号：",
		"地址", "地址、电话", "电话",
		"开户行", "开户行及账号", "账号", "支行", "分行", "营业部",
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
	if strings.Contains(name, "支行") || strings.Contains(name, "分行") || strings.Contains(name, "营业部") {
		return true
	}
	// Dates are not party names; avoid confusing invoice date with seller/buyer.
	if regexp.MustCompile(`\d{4}年\d{1,2}月\d{1,2}日`).MatchString(name) {
		return true
	}
	// Chinese amount-in-words (e.g. "贰佰圆整") are not party names.
	if strings.Contains(name, "圆整") || strings.Contains(name, "元整") {
		return true
	}
	if regexp.MustCompile(`^[零壹贰叁肆伍陆柒捌玖拾佰仟万亿圆元整]+$`).MatchString(name) &&
		(strings.Contains(name, "圆") || strings.Contains(name, "元")) {
		return true
	}
	// Bank/account/address-like lines are not party names.
	if regexp.MustCompile(`\d{10,}`).MatchString(name) {
		return true
	}
	if strings.Contains(name, "银行") || strings.Contains(name, "流水号") || strings.Contains(name, "业务") {
		return true
	}
	if regexp.MustCompile(`(?:路|街|道|弄|巷)\d+号`).MatchString(name) || regexp.MustCompile(`\d+号`).MatchString(name) {
		return true
	}
	return false
}

func isLikelyBuyerName(name string) bool {
	name = cleanupName(strings.TrimSpace(name))
	if name == "" || isBadPartyNameCandidate(name) {
		return false
	}
	if name == "个人" {
		return true
	}
	han := 0
	for _, r := range name {
		if unicode.Is(unicode.Han, r) {
			han++
		}
	}
	return han >= 2
}

func isLikelySellerName(name string) bool {
	name = cleanupName(strings.TrimSpace(name))
	if name == "" || isBadPartyNameCandidate(name) {
		return false
	}
	// A seller should not be "个人" for invoices we handle.
	if name == "个人" {
		return false
	}
	han := 0
	for _, r := range name {
		if unicode.Is(unicode.Han, r) {
			han++
		}
	}
	return han >= 2
}

func normalizeInvoiceTextForParsing(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = removeChineseInlineSpaces(text)

	// If PyMuPDF zoned layout is used, keep buyer/seller markers but drop other section titles.
	// This prevents section headers like "【明细】" from polluting line-item parsing.
	text = regexp.MustCompile(`(?m)^【第\d+页-分区】\s*$`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`(?m)^【购买方】\s*$`).ReplaceAllString(text, "购买方")
	text = regexp.MustCompile(`(?m)^【销售方】\s*$`).ReplaceAllString(text, "销售方")
	// Keep a few important section markers as plain tokens for parsing.
	text = regexp.MustCompile(`(?m)^【密码区】\s*$`).ReplaceAllString(text, "密码区")
	text = regexp.MustCompile(`(?m)^【明细】\s*$`).ReplaceAllString(text, "明细")
	text = regexp.MustCompile(`(?m)^【备注/其他】\s*$`).ReplaceAllString(text, "备注")
	text = regexp.MustCompile(`(?m)^【[^】]{1,20}】\s*$`).ReplaceAllString(text, "")

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
		{regexp.MustCompile(`合\s*计`), "合计"},
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

func normalizeInvoiceTextForPretty(text string, data *InvoiceExtractedData) string {
	// Pretty output should preserve PyMuPDF "分区" headers (e.g. "【发票信息】/【明细】"),
	// while still normalizing common spacing/currency issues.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = removeChineseInlineSpaces(text)

	// Normalize "vertical" or whitespace-separated labels often found in PDF text extraction.
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
		{regexp.MustCompile(`合\s*计`), "合计"},
	}
	for _, r := range replacements {
		text = r.re.ReplaceAllString(text, r.repl)
	}

	// Normalize currency symbols / mojibake variants to the full-width RMB symbol (U+FFE5).
	text = strings.ReplaceAll(text, "\u00ef\u00bf\u00a5", "\uffe5")
	text = strings.ReplaceAll(text, "\u00c2\u00a5", "\uffe5")
	text = strings.ReplaceAll(text, "\u00a5", "\uffe5")

	text = paymentInvisibleSpaceReplacer.Replace(text)

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		out = append(out, s)
	}

	// Fix PyMuPDF zoned output when an invoice template has no password area.
	// Some providers/templates omit the "密码区", but the fixed region split still emits a "【密码区】" block,
	// and buyer info may be pushed into it, making the pretty zoned text misleading (even if the final
	// extracted fields are correct).
	out = fixInvoiceZonesForPretty(out, data)

	return strings.TrimSpace(strings.Join(out, "\n"))
}

func fixInvoiceZonesForPretty(lines []string, data *InvoiceExtractedData) []string {
	isZoneHeader := func(s string) bool {
		s = strings.TrimSpace(s)
		if s == "" {
			return false
		}
		if regexp.MustCompile(`^【第\d+页-分区】$`).MatchString(s) {
			return true
		}
		return regexp.MustCompile(`^【[^】]{1,20}】$`).MatchString(s)
	}
	isPageHeader := func(s string) bool {
		return regexp.MustCompile(`^【第\d+页-分区】$`).MatchString(strings.TrimSpace(s))
	}
	isLikelyPasswordBlock := func(block []string) bool {
		if len(block) == 0 {
			return false
		}
		s := strings.TrimSpace(strings.Join(block, " "))
		if s == "" {
			return false
		}
		// Explicit marker or typical encrypted patterns.
		if strings.Contains(s, "密码区") || strings.Contains(s, "密 码 区") || strings.ContainsAny(s, "<>") {
			return true
		}
		// Strong symbol density is a good hint for the encrypted password area.
		// Avoid using "/" as a signal because many non-password invoices contain "税率/征收率".
		if strings.Count(s, "*") >= 3 || strings.Count(s, "+") >= 2 {
			return true
		}
		// If it contains typical buyer/seller fields, it's not a password area.
		for _, badPwd := range []string{"纳税人识别号", "统一社会信用代码", "地址", "电话", "开户行", "项目名称", "规格型号", "单位", "数量", "单价", "金额", "税率", "税额"} {
			if strings.Contains(s, badPwd) {
				return false
			}
		}
		// Heuristic: password blocks have very low Han ratio and high digit/symbol ratio.
		total := 0
		han := 0
		digitOrSym := 0
		for _, r := range s {
			if unicode.IsSpace(r) {
				continue
			}
			total++
			if unicode.Is(unicode.Han, r) {
				han++
				continue
			}
			if unicode.IsDigit(r) {
				digitOrSym++
				continue
			}
			switch r {
			case '*', '/', '+', '-', '=', '_', '.', ',', ':', '：', '￥', '¥', '<', '>':
				digitOrSym++
			}
		}
		if total == 0 {
			return false
		}
		hanRatio := float64(han) / float64(total)
		dsRatio := float64(digitOrSym) / float64(total)
		return dsRatio >= 0.45 && hanRatio <= 0.15
	}
	shouldMergeFakePasswordBlock := func(block []string) bool {
		if len(block) == 0 {
			return false
		}
		s := strings.TrimSpace(strings.Join(block, " "))
		if s == "" {
			return false
		}
		// Only merge when we are confident the "【密码区】" block is actually a mis-split buyer/seller/table area.
		// Some invoice templates (or PDFs extracted by zones) can place seller/company lines or numeric columns
		// near the password region; merging those away makes the pretty text misleading.
		if data != nil {
			if buyerName := ptrToString(data.BuyerName); buyerName != "" && strings.Contains(s, buyerName) {
				return true
			}
		}
		for _, hint := range []string{
			"购买方", "买方", "购方", "购买方信息", "销售方信息",
			"货物或应税劳务", "项目名称", "规格型号", "单位", "数量",
			"地址", "电话", "开户行", "账号",
		} {
			if strings.Contains(s, hint) {
				return true
			}
		}
		return false
	}

	type seg struct {
		header  string
		content []string
		removed bool
	}

	// If there are no zone headers, do nothing.
	hasZone := false
	for _, l := range lines {
		if isZoneHeader(l) {
			hasZone = true
			break
		}
	}
	if !hasZone {
		return lines
	}

	segs := make([]seg, 0, 16)
	cur := seg{}
	flush := func() {
		if cur.header == "" && len(cur.content) == 0 {
			return
		}
		segs = append(segs, cur)
		cur = seg{}
	}
	for _, l := range lines {
		if isZoneHeader(l) {
			flush()
			cur.header = l
			continue
		}
		cur.content = append(cur.content, l)
	}
	flush()

	// Merge non-password "【密码区】" into the most likely logical zone.
	// This happens when a template has no password area but we still split by fixed regions.
	for i := 0; i < len(segs); i++ {
		if strings.TrimSpace(segs[i].header) != "【密码区】" {
			continue
		}
		if isLikelyPasswordBlock(segs[i].content) {
			continue
		}
		if !shouldMergeFakePasswordBlock(segs[i].content) {
			continue
		}

		// Find the nearest buyer/seller/detail segment within the same page.
		buyerIdx := -1
		sellerIdx := -1
		detailIdx := -1
		for j := i - 1; j >= 0; j-- {
			if isPageHeader(segs[j].header) {
				break
			}
			if strings.TrimSpace(segs[j].header) == "【购买方】" {
				buyerIdx = j
				break
			}
		}
		for j := i + 1; j < len(segs); j++ {
			if isPageHeader(segs[j].header) {
				break
			}
			if strings.TrimSpace(segs[j].header) == "【销售方】" {
				sellerIdx = j
				break
			}
		}
		for j := i + 1; j < len(segs); j++ {
			if isPageHeader(segs[j].header) {
				break
			}
			if strings.TrimSpace(segs[j].header) == "【明细】" {
				detailIdx = j
				break
			}
		}
		if buyerIdx == -1 {
			continue
		}

		// Distribute lines instead of moving the whole block:
		// - company+taxid -> seller
		// - unit price / amount / tax columns -> detail
		// - others -> buyer
		buyerName := ""
		sellerName := ""
		if data != nil {
			buyerName = ptrToString(data.BuyerName)
			sellerName = ptrToString(data.SellerName)
		}
		for _, line := range segs[i].content {
			l := strings.TrimSpace(line)
			if l == "" {
				continue
			}

			target := buyerIdx

			// Table numeric columns should belong to "明细" rather than "销售方".
			if detailIdx != -1 && (strings.Contains(l, "单价") || strings.Contains(l, "金额") || strings.Contains(l, "税率") || strings.Contains(l, "征收率") || strings.Contains(l, "税额")) {
				target = detailIdx
			}

			// Tax-id/company lines should belong to seller when we have a company seller (common when buyer is "个人").
			if sellerIdx != -1 && sellerName != "" && sellerName != "个人" {
				joined := l
				if strings.Contains(joined, sellerName) || extractCompanyNameNearTaxID(joined) == sellerName {
					target = sellerIdx
				} else if taxIDRegex.MatchString(joined) && regexp.MustCompile(`(?:有限责任公司|有限公司|公司|集团|商店|企业|中心|厂|店|行|社|院|局)`).MatchString(joined) {
					target = sellerIdx
				}
			}

			// If buyer name appears, keep it in buyer zone.
			if buyerName != "" && strings.Contains(l, buyerName) {
				target = buyerIdx
			}

			segs[target].content = append(segs[target].content, l)
		}
		segs[i].removed = true
	}

	// Clean merged header rows: buyer zone lines sometimes include both buyer and seller labels due to 2-column PDFs.
	if data != nil {
		buyerName := ptrToString(data.BuyerName)
		sellerName := ptrToString(data.SellerName)
		if buyerName != "" || sellerName != "" {
			for i := range segs {
				h := strings.TrimSpace(segs[i].header)
				if h != "【购买方】" && h != "【销售方】" {
					continue
				}
				cleaned := make([]string, 0, len(segs[i].content))
				for _, line := range segs[i].content {
					l := strings.TrimSpace(line)
					if l == "" {
						continue
					}
					// Split "…个人 销售方信息名称：…" into buyer/seller respective segments.
					if strings.Contains(l, "销售方信息名称") && h == "【购买方】" {
						if idx := strings.Index(l, "销售方信息名称"); idx > 0 {
							left := strings.TrimSpace(l[:idx])
							if left != "" {
								cleaned = append(cleaned, left)
							}
							// Drop the trailing seller label fragment (seller details are handled via other blocks).
							continue
						}
					}
					if strings.Contains(l, "购买方信息名称") && h == "【销售方】" {
						if idx := strings.Index(l, "购买方信息名称"); idx > 0 {
							right := strings.TrimSpace(l[idx:])
							_ = right
							// Prefer dropping merged buyer fragments from seller zone.
							l = strings.TrimSpace(l[:idx])
							if l == "" {
								continue
							}
						}
					}
					// If we have a known party name, remove obvious mismatched label-only lines.
					if h == "【购买方】" && buyerName == "个人" && strings.Contains(l, "纳税人识别号") && taxIDRegex.MatchString(l) {
						// A tax ID line in buyer zone is usually a seller-side column leak when buyer is individual.
						continue
					}
					if h == "【销售方】" && sellerName != "" && sellerName != "个人" {
						// If a line contains buyer name but not seller name, it's likely leaked from buyer column.
						if buyerName != "" && strings.Contains(l, buyerName) && !strings.Contains(l, sellerName) && !taxIDRegex.MatchString(l) {
							continue
						}
					}
					cleaned = append(cleaned, l)
				}
				segs[i].content = cleaned
			}
		}
	}

	rebuilt := make([]string, 0, len(lines))
	for _, s := range segs {
		if s.removed {
			continue
		}
		if strings.TrimSpace(s.header) != "" {
			rebuilt = append(rebuilt, strings.TrimSpace(s.header))
		}
		for _, l := range s.content {
			v := strings.TrimSpace(l)
			if v == "" {
				continue
			}
			rebuilt = append(rebuilt, v)
		}
	}
	return rebuilt
}

func formatFloat2(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *v)
}

func formatInvoicePrettyText(raw string, data *InvoiceExtractedData) string {
	clean := normalizeInvoiceTextForPretty(raw, data)

	var b strings.Builder
	b.WriteString("【整理摘要】\n")
	if data.InvoiceNumber != nil && strings.TrimSpace(*data.InvoiceNumber) != "" {
		b.WriteString("发票号码：")
		b.WriteString(strings.TrimSpace(*data.InvoiceNumber))
		b.WriteString("\n")
	}
	if data.InvoiceDate != nil && strings.TrimSpace(*data.InvoiceDate) != "" {
		b.WriteString("开票日期：")
		b.WriteString(strings.TrimSpace(*data.InvoiceDate))
		b.WriteString("\n")
	}
	if amt := formatFloat2(data.Amount); amt != "" {
		b.WriteString("价税合计(小写)：￥")
		b.WriteString(amt)
		b.WriteString("\n")
	}
	if tax := formatFloat2(data.TaxAmount); tax != "" {
		b.WriteString("税额：￥")
		b.WriteString(tax)
		b.WriteString("\n")
	}
	if data.BuyerName != nil && strings.TrimSpace(*data.BuyerName) != "" {
		b.WriteString("购买方：")
		b.WriteString(strings.TrimSpace(*data.BuyerName))
		b.WriteString("\n")
	}
	if data.SellerName != nil && strings.TrimSpace(*data.SellerName) != "" {
		b.WriteString("销售方：")
		b.WriteString(strings.TrimSpace(*data.SellerName))
		b.WriteString("\n")
	}

	if len(data.Items) > 0 {
		b.WriteString("\n【商品明细(解析)】\n")
		b.WriteString("商品名称\t规格型号\t单位\t数量\n")
		for _, it := range data.Items {
			name := strings.TrimSpace(it.Name)
			if name == "" {
				continue
			}
			spec := strings.TrimSpace(it.Spec)
			if spec == "" {
				spec = "-"
			}
			unit := strings.TrimSpace(it.Unit)
			if unit == "" {
				unit = "-"
			}
			qty := "-"
			if it.Quantity != nil {
				if *it.Quantity == float64(int64(*it.Quantity)) {
					qty = fmt.Sprintf("%d", int64(*it.Quantity))
				} else {
					qty = fmt.Sprintf("%g", *it.Quantity)
				}
			}
			b.WriteString(name)
			b.WriteString("\t")
			b.WriteString(spec)
			b.WriteString("\t")
			b.WriteString(unit)
			b.WriteString("\t")
			b.WriteString(qty)
			b.WriteString("\n")
		}
	}

	if clean != "" {
		b.WriteString("\n【整理后的OCR文本】\n")
		b.WriteString(clean)
	}

	return strings.TrimSpace(b.String())
}

func normalizePaymentTextForPretty(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = paymentInvisibleSpaceReplacer.Replace(text)
	text = removeChineseSpaces(text)

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func formatPaymentPrettyText(raw string, data *PaymentExtractedData) string {
	clean := normalizePaymentTextForPretty(raw)

	var b strings.Builder
	b.WriteString("【整理摘要】\n")
	if data.Amount != nil {
		b.WriteString("金额：￥")
		b.WriteString(formatFloat2(data.Amount))
		b.WriteString("\n")
	}
	if data.Merchant != nil && strings.TrimSpace(*data.Merchant) != "" {
		b.WriteString("商家：")
		b.WriteString(strings.TrimSpace(*data.Merchant))
		b.WriteString("\n")
	}
	if data.PaymentMethod != nil && strings.TrimSpace(*data.PaymentMethod) != "" {
		b.WriteString("支付方式：")
		b.WriteString(strings.TrimSpace(*data.PaymentMethod))
		b.WriteString("\n")
	}
	if data.TransactionTime != nil && strings.TrimSpace(*data.TransactionTime) != "" {
		b.WriteString("交易时间：")
		b.WriteString(strings.TrimSpace(*data.TransactionTime))
		b.WriteString("\n")
	}
	if data.OrderNumber != nil && strings.TrimSpace(*data.OrderNumber) != "" {
		b.WriteString("订单号：")
		b.WriteString(strings.TrimSpace(*data.OrderNumber))
		b.WriteString("\n")
	}

	if clean != "" {
		b.WriteString("\n【整理后的OCR文本】\n")
		b.WriteString(clean)
	}

	return strings.TrimSpace(b.String())
}

func extractInvoiceLineItems(text string) []InvoiceLineItem {
	text = normalizeInvoiceTextForParsing(text)
	lines := strings.Split(text, "\n")

	headerMarkers := []string{
		"货物或应税劳务", "服务名称", "项目名称",
		"规格型号", "单位", "数量", "单价", "金额", "税率", "税额",
	}
	isPrimaryHeaderLine := func(s string) bool {
		return strings.Contains(s, "货物或应税劳务") || strings.Contains(s, "服务名称") || strings.Contains(s, "项目名称")
	}
	normLine := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}
		s = strings.Join(strings.Fields(s), " ")
		return removeChineseInlineSpaces(s)
	}

	// In PDF text extraction, the header columns are frequently broken across multiple lines
	// (e.g. each column on its own line). Detect a header *region* using a windowed score.
	bestStart := -1
	bestScore := -1
	winSize := 10
	for i := 0; i < len(lines); i++ {
		score := 0
		for j := i; j < len(lines) && j < i+winSize; j++ {
			s := normLine(lines[j])
			if s == "" {
				continue
			}
			if isPrimaryHeaderLine(s) {
				score += 4
			}
			for _, m := range headerMarkers[3:] { // non-primary columns
				if strings.Contains(s, m) {
					score++
				}
			}
		}
		if score > bestScore {
			bestScore = score
			bestStart = i
		}
	}
	if bestStart < 0 || bestScore < 4 {
		return nil
	}

	headerIdx := -1
	for j := bestStart; j < len(lines) && j < bestStart+winSize; j++ {
		s := normLine(lines[j])
		if s == "" {
			continue
		}
		if isPrimaryHeaderLine(s) {
			headerIdx = j
			break
		}
	}
	if headerIdx < 0 {
		headerIdx = bestStart
	}

	headerEnd := headerIdx
	for j := headerIdx + 1; j < len(lines) && j <= headerIdx+winSize; j++ {
		s := normLine(lines[j])
		if s == "" {
			continue
		}
		// Extend while header markers continue.
		has := false
		for _, m := range headerMarkers {
			if strings.Contains(s, m) {
				has = true
				break
			}
		}
		if has {
			headerEnd = j
			continue
		}
		break
	}

	isStopLine := func(s string) bool {
		stopMarkers := []string{
			// Total/summary + footer/person fields.
			"价税合计",
			"合计",
			"收款人",
			"复核",
			"开票人",
			"备注",
			// Party sections (avoid treating these as items).
			"销售方",
			"购买方",
			// Invoice meta (avoid treating these as items; may appear anywhere in PDF text).
			"校验码",
			"发票代码",
			"发票号码",
			"开票日期",
			"机器编号",
		}
		for _, m := range stopMarkers {
			if strings.Contains(s, m) {
				return true
			}
		}
		return false
	}

	isBlockEndLine := func(s string) bool {
		// For PDF text extraction, invoice meta (code/date/check) can appear between the header and
		// actual rows. Only stop when we reach totals/summary, otherwise we might cut off items.
		if strings.Contains(s, "价税合计") && (strings.ContainsRune(s, '￥') || regexp.MustCompile(`\d+\.\d{2}`).MatchString(s)) {
			return true
		}
		// Sometimes totals are broken into multiple lines; treat a "合计" line that also has currency as end.
		if strings.Contains(s, "合计") && strings.ContainsRune(s, '￥') {
			return true
		}
		// Totals may also appear as a "(小写) ¥123.45" line without the "价税合计" token (common in PDF text).
		if (strings.Contains(s, "小写") || strings.Contains(s, "大写")) && (strings.ContainsRune(s, '￥') || regexp.MustCompile(`\d+\.\d{2}`).MatchString(s)) {
			return true
		}
		return false
	}

	isHeaderLine := func(s string) bool {
		for _, m := range headerMarkers {
			if strings.Contains(s, m) {
				return true
			}
		}
		return false
	}

	unitTokenRe := regexp.MustCompile(`^[\p{Han}]{1,2}$`)
	isUnitToken := func(s string) bool {
		if !unitTokenRe.MatchString(s) {
			return false
		}
		// Avoid treating common unit-like tokens as item names/continuations.
		switch s {
		case "件", "个", "箱", "袋", "包", "瓶", "罐", "盒", "组", "台", "次", "张", "套", "份", "支", "双", "只", "米", "公斤", "千克", "克", "元":
			return true
		default:
			return false
		}
	}

	numOnlyRe := regexp.MustCompile(`^\d+(?:\.\d+)?$`)
	moneyLikeRe := regexp.MustCompile(`^\d+\.\d{2}$`)
	decimalLikeRe := regexp.MustCompile(`^\d+\.\d+$`)
	dimensionLikeRe := regexp.MustCompile(`(?i)^\d+(?:\.\d+)?\s*[x\x{00d7}]\s*\d+(?:\.\d+)?(?:\s*[x\x{00d7}]\s*\d+(?:\.\d+)?)?\s*(?:mm|cm|m|g|kg|ml|l)?$`)
	spacedDim2Re := regexp.MustCompile(`(?i)^(\d{2,4})\s+(\d{2,4})(mm|cm|m)$`)
	integerOnlyRe := regexp.MustCompile(`^\d+$`)
	taxRateRe := regexp.MustCompile(`^\d{1,2}%$`)
	quantityLabelRe := regexp.MustCompile(`数量\s*[:：]?\s*(\d+(?:\.\d+)?)`)
	// Common "spec/model" tokens that are not item names, such as:
	// - 3X410g / 3×410g
	// - 410g×3
	// - 750ml×6
	// - 53°×6
	specTokenRe := regexp.MustCompile(`(?i)^(?:\d+\s*[x×]\s*\d+(?:\.\d+)?\s*(?:g|kg|ml|l)?|\d+(?:\.\d+)?\s*(?:g|kg|ml|l)?\s*[x×]\s*\d+(?:\.\d+)?|\d+(?:\.\d+)?[a-z]{1,4}\s*[x×]\s*\d+(?:\.\d+)?|\d+(?:\.\d+)?\s*°\s*[x×]\s*\d+(?:\.\d+)?\s*(?:kg|ml|mm|cm|m|g|l)?)$`)
	// Simple measure specs that often appear as the right-most token in the item name when the
	// "规格型号" column is missing, e.g. "500ml", "1.23kg", "190mm".
	simpleMeasureSpecRe := regexp.MustCompile(`(?i)^\d+(?:\.\d+)?(?:ml|l|g|kg|mm|cm|m)$`)
	simpleMeasureSpecSplitRe := regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)(ml|l|g|kg|mm|cm|m)$`)
	simpleMeasureUnitRe := regexp.MustCompile(`(?i)^(ml|l|g|kg|mm|cm|m)$`)
	labelLineRe := regexp.MustCompile(`^(?:名称|名\s*称|纳税人识别号|地址|地址、电话|地址,电话|电话|开户行|开户行及账号|账号)[:：]?$`)
	// Some OCR results merge labels and values on the same line (e.g. "名称：沃尔玛…").
	labelPrefixRe := regexp.MustCompile(`^(?:名称|名\s*称|纳税人识别号|统一社会信用代码/纳税人识别号|地址|地址、电话|地址,电话|电话|开户行|开户行及账号|开户行|账号)\s*[:：]\s*\S+`)

	categoryPrefixRe := regexp.MustCompile(`^\*([^*]+)\*`)
	latinCamelSplitRe := regexp.MustCompile(`([a-z])([A-Z])`)
	latinHanBoundaryRe := regexp.MustCompile(`([A-Za-z])([\p{Han}])`)
	hanLatinBoundaryRe := regexp.MustCompile(`([\p{Han}])([A-Za-z])`)
	normalizeName := func(s string) string {
		s = strings.TrimSpace(s)
		// Convert "*分类*" prefix to "分类 ".
		s = categoryPrefixRe.ReplaceAllString(s, "$1 ")
		// Remaining '*' are usually multipliers like "410g*3".
		s = strings.ReplaceAll(s, "*", "×")
		// Normalize common punctuation variants for consistent display.
		s = strings.NewReplacer("（", "(", "）", ")", "，", ",").Replace(s)
		// Improve readability for OCR that merges English tokens (e.g. "Member'sMark希腊式...").
		s = latinCamelSplitRe.ReplaceAllString(s, "$1 $2")
		s = latinHanBoundaryRe.ReplaceAllString(s, "$1 $2")
		s = hanLatinBoundaryRe.ReplaceAllString(s, "$1 $2")
		s = strings.Join(strings.Fields(s), " ")
		s = removeChineseInlineSpaces(s)
		return strings.TrimSpace(s)
	}

	// Peel trailing model codes from an item name, e.g.:
	// "... KFR-72LW/NhBa1BAj KFR-72LW/NhBa1BAj" -> (name="...", spec="KFR-72LW/NhBa1BAj").
	peelTrailingModelCodes := func(name string) (cleanName, spec string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", ""
		}
		// Match codes that contain both letters+digits and a separator (- or /), like "KFR-72LW/NhBa1BAj".
		// Some OCR split the tail code into multiple tokens, e.g. "KFR-72LW/Nh Ba1BAj", so we also
		// try concatenating the last two tokens.
		modelRe := regexp.MustCompile(`(?i)^[A-Z]{2,}[A-Z0-9]*[-/][A-Z0-9][A-Z0-9/.-]{3,}$`)
		digitRe := regexp.MustCompile(`\d`)
		trimModelToken := func(s string) string {
			return strings.TrimRight(strings.TrimSpace(s), "，,.;。:：")
		}
		parts := strings.Fields(name)
		if len(parts) == 0 {
			return name, ""
		}
		var models []string
		for len(parts) > 0 {
			last := trimModelToken(parts[len(parts)-1])
			// 1-token model
			if modelRe.MatchString(last) && digitRe.MatchString(last) {
				models = append(models, last)
				parts = parts[:len(parts)-1]
				continue
			}
			// 2-token model (OCR split inside code)
			if len(parts) >= 2 {
				combined := trimModelToken(parts[len(parts)-2] + parts[len(parts)-1])
				if modelRe.MatchString(combined) && digitRe.MatchString(combined) {
					models = append(models, combined)
					parts = parts[:len(parts)-2]
					continue
				}
			}
			break
		}
		if len(models) == 0 {
			return name, ""
		}
		// Keep only the first unique model (these often repeat twice).
		uniq := make(map[string]struct{}, len(models))
		out := make([]string, 0, 2)
		for i := len(models) - 1; i >= 0; i-- {
			m := models[i]
			if _, ok := uniq[m]; ok {
				continue
			}
			uniq[m] = struct{}{}
			out = append(out, m)
		}
		clean := strings.TrimSpace(strings.Join(parts, " "))
		return clean, strings.TrimSpace(strings.Join(out, " "))
	}

	isLikelyItemNameLine := func(s string) bool {
		if s == "" || isHeaderLine(s) || isStopLine(s) {
			return false
		}
		// Password area is never a line item; it often contains dense symbols like "<>".
		if strings.Contains(s, "密码区") || strings.ContainsAny(s, "<>") {
			return false
		}
		if labelLineRe.MatchString(s) || labelPrefixRe.MatchString(s) {
			return false
		}
		if strings.Contains(s, "订单号") || strings.Contains(s, "发票专用章") {
			return false
		}
		if strings.Contains(s, "下载次数") {
			return false
		}
		// Label-like rows (often from PDF extraction) shouldn't be treated as item names.
		if strings.HasSuffix(s, ":") || strings.HasSuffix(s, "：") {
			return false
		}
		if taxRateRe.MatchString(s) || numOnlyRe.MatchString(s) {
			return false
		}
		if isUnitToken(s) {
			return false
		}
		tn := strings.ReplaceAll(s, "*", "\u00d7")
		tnCompact := strings.ReplaceAll(tn, " ", "")
		if specTokenRe.MatchString(tnCompact) || dimensionLikeRe.MatchString(tnCompact) || spacedDim2Re.MatchString(strings.TrimSpace(s)) {
			return false
		}
		if strings.Contains(s, "价税合计") || strings.Contains(s, "合计") {
			return false
		}
		if (strings.Contains(s, "小写") || strings.Contains(s, "大写")) && (strings.ContainsRune(s, '￥') || moneyLikeRe.MatchString(s)) {
			return false
		}
		// Require at least one letter or Han character.
		hasText := false
		hasHan := false
		asciiLetters := 0
		for _, r := range s {
			if unicode.Is(unicode.Han, r) {
				hasText = true
				hasHan = true
				break
			}
			if unicode.IsLetter(r) && r < 128 {
				hasText = true
				asciiLetters++
			}
		}
		if !hasText {
			return false
		}
		// Purely ASCII "spec-like" tokens are common in invoices; avoid treating them as item names.
		if !hasHan && asciiLetters > 0 {
			// Heuristic: require at least 3 ASCII letters for English-only names.
			if asciiLetters < 3 {
				return false
			}
		}
		n := len([]rune(s))
		return n >= 2 && n <= 120
	}

	// Some PDF text extractions merge unit+qty into the same token as the item name,
	// e.g. "*电信服务*话费充值元1" or "... 元 1". Peel them off early so we don't lose them.
	unitQtySuffixRe := regexp.MustCompile(`^(.*?)(元|件|个|箱|袋|包|瓶|罐|盒|组|台|次|张|套|份|支|双|只|米|公斤|千克|克)\s*(\d+(?:\.\d+)?)$`)
	unitQtyInlineRe := regexp.MustCompile(`(元|件|个|箱|袋|包|瓶|罐|盒|组|台|次|张|套|份|支|双|只|米|公斤|千克|克)\s*(\d+(?:\.\d+)?)`)
	specInlineRe := regexp.MustCompile(`(?:\d+(?:\.\d+)?\s*°\s*(?:[×x\*])\s*\d+(?:\.\d+)?\s*(?:kg|ml|mm|cm|m|g|l)?|\d+(?:\.\d+)?\s*(?:kg|ml|mm|cm|m|g|l)\s*(?:[×x\*])\s*\d+(?:\.\d+)?)`)
	categoryAnyRe := regexp.MustCompile(`\*[^*\n\r]{1,20}\*`)
	longDecimalRe := regexp.MustCompile(`\d+\.\d{4,}`)
	decimalRe := regexp.MustCompile(`\d+\.\d+`)
	percentRe := regexp.MustCompile(`\d{1,3}%`)
	intRe := regexp.MustCompile(`\b\d+\b`)

	var parseQuantityWithUnit func(s, unit string) *float64

	parseMultiPackedItems := func(line string) []InvoiceLineItem {
		line = strings.TrimSpace(line)
		if line == "" {
			return nil
		}
		cats := categoryAnyRe.FindAllStringIndex(line, -1)
		if len(cats) < 2 {
			return nil
		}
		// Filter out false "*...*" matches caused by dimension specs like "360*380*190mm" (e.g. "*380*").
		// Keep only markers that look like real categories (contain Han or at least 2 ASCII letters).
		{
			onlyDigits := regexp.MustCompile(`^\d+$`)
			dimUnit := regexp.MustCompile(`(?i)^\d+(?:mm|cm|m|g|kg|ml|l)$`)
			filtered := make([][]int, 0, len(cats))
			for _, c := range cats {
				if len(c) != 2 || c[1]-c[0] < 3 {
					continue
				}
				token := strings.TrimSpace(line[c[0]+1 : c[1]-1])
				if token == "" || onlyDigits.MatchString(token) || dimUnit.MatchString(token) {
					continue
				}
				hasHan := false
				asciiLetters := 0
				for _, r := range token {
					if unicode.Is(unicode.Han, r) {
						hasHan = true
						break
					}
					if r < 128 && unicode.IsLetter(r) {
						asciiLetters++
					}
				}
				if !hasHan && asciiLetters < 2 {
					continue
				}
				filtered = append(filtered, c)
			}
			cats = filtered
			if len(cats) < 2 {
				return nil
			}
		}

		// Split into [namesPart | tailPart] at the first spec occurrence.
		splitAt := -1
		if loc := specInlineRe.FindStringIndex(line); loc != nil {
			splitAt = loc[0]
		}
		// Some packed rows have no inline spec; instead, they list unit/qty/prices after all names,
		// sometimes with duplicated units like "包包2 2 ...". In that case, split at the first
		// unit+quantity marker and backtrack to include any duplicated unit token.
		if splitAt < 0 {
			if ms := unitQtyInlineRe.FindAllStringSubmatchIndex(line, -1); len(ms) > 0 {
				m := ms[0]
				if len(m) >= 4 {
					splitAt = m[0]
					// Backtrack to include repeated unit tokens just before the first "unit+qty" match.
					if len(m) >= 6 && m[2] >= 0 && m[3] > m[2] {
						unitToken := line[m[2]:m[3]]
						back := splitAt
						for back > 0 {
							// Trim spaces between duplicated unit tokens.
							r, size := utf8.DecodeLastRuneInString(line[:back])
							if size <= 0 {
								break
							}
							if unicode.IsSpace(r) {
								back -= size
								splitAt = back
								continue
							}
							if back >= len(unitToken) && strings.HasSuffix(line[:back], unitToken) {
								back -= len(unitToken)
								splitAt = back
								continue
							}
							break
						}
					}
					// Ensure we still have multiple category segments in the names part.
					if splitAt > 0 {
						if len(categoryAnyRe.FindAllStringIndex(strings.TrimSpace(line[:splitAt]), -1)) < 2 {
							splitAt = -1
						}
					}
				}
			}
		}
		namePart := line
		tailPart := ""
		if splitAt > 0 {
			namePart = strings.TrimSpace(line[:splitAt])
			tailPart = strings.TrimSpace(line[splitAt:])
		}

		// Extract name segments (each starts with a "*类别*").
		segStarts := categoryAnyRe.FindAllStringIndex(namePart, -1)
		if len(segStarts) < 2 {
			return nil
		}
		names := make([]string, 0, len(segStarts))
		for i := 0; i < len(segStarts); i++ {
			start := segStarts[i][0]
			end := len(namePart)
			if i+1 < len(segStarts) {
				end = segStarts[i+1][0]
			}
			seg := strings.TrimSpace(namePart[start:end])
			if seg != "" {
				names = append(names, seg)
			}
		}
		if len(names) < 2 {
			return nil
		}

		// Extract specs in order (e.g. "53°×6", "750ml×6").
		specs := make([]string, 0, len(names))
		for _, sidx := range specInlineRe.FindAllStringIndex(tailPart, -1) {
			if sidx[0] >= 0 && sidx[1] > sidx[0] {
				raw := tailPart[sidx[0]:sidx[1]]
				specs = append(specs, strings.NewReplacer("*", "×", "x", "×", "X", "×").Replace(raw))
			}
		}

		// Units: if a single unit repeats N times, assume it's for each row (common for invoices).
		units := make([]string, 0, len(names))
		for _, u := range []string{"瓶", "件", "个", "箱", "袋", "包", "罐", "盒", "组", "台", "次", "张", "套", "份", "支", "双", "只", "米", "公斤", "千克", "克", "元"} {
			c := strings.Count(tailPart, u)
			if c >= len(names) {
				for i := 0; i < len(names); i++ {
					units = append(units, u)
				}
				break
			}
		}

		// Quantities: remove specs, decimals/prices and percents, then take the last N integers.
		cleanTail := tailPart
		cleanTail = specInlineRe.ReplaceAllString(cleanTail, " ")
		cleanTail = percentRe.ReplaceAllString(cleanTail, " ")
		cleanTail = longDecimalRe.ReplaceAllString(cleanTail, " ")
		cleanTail = decimalRe.ReplaceAllString(cleanTail, " ")
		ints := intRe.FindAllString(cleanTail, -1)
		qtys := make([]*float64, 0, len(names))
		if len(ints) >= len(names) {
			ints = ints[len(ints)-len(names):]
		}
		for _, q := range ints {
			v := parseQuantityWithUnit(q, "")
			if v == nil {
				qtys = append(qtys, nil)
				continue
			}
			qtys = append(qtys, v)
		}

		if len(specs) == 0 && len(units) == 0 && len(qtys) == 0 {
			return nil
		}

		out := make([]InvoiceLineItem, 0, len(names))
		for i := 0; i < len(names); i++ {
			name := normalizeName(names[i])
			spec := ""
			if i < len(specs) {
				spec = strings.TrimSpace(specs[i])
			}
			unit := ""
			if i < len(units) {
				unit = strings.TrimSpace(units[i])
			}
			var qty *float64
			if i < len(qtys) {
				qty = qtys[i]
			}
			if name == "" {
				continue
			}
			out = append(out, InvoiceLineItem{Name: name, Spec: spec, Unit: unit, Quantity: qty})
		}
		if len(out) >= 2 {
			return out
		}
		return nil
	}

	parsePackedRow := func(line string) (name, spec, unit string, qty *float64, ok bool) {
		line = strings.TrimSpace(line)
		if line == "" {
			return "", "", "", nil, false
		}

		// Keep category prefix like "*酒*" if present so normalizeName can format it.
		cat := ""
		rest := line
		if loc := categoryPrefixRe.FindStringSubmatchIndex(line); len(loc) >= 2 {
			// Group 1: category text.
			if len(loc) >= 4 && loc[2] >= 0 && loc[3] > loc[2] {
				cat = strings.TrimSpace(line[loc[2]:loc[3]])
			}
			rest = strings.TrimSpace(line[loc[1]:])
		}

		// Drop trailing long-decimal prices (common in PDF text) so they don't pollute the name.
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			last := fields[len(fields)-1]
			if decimalLikeRe.MatchString(last) {
				if parts := strings.SplitN(last, ".", 2); len(parts) == 2 && len(parts[1]) > 3 {
					fields = fields[:len(fields)-1]
				}
			}
			if len(fields) > 0 && moneyLikeRe.MatchString(fields[len(fields)-1]) {
				fields = fields[:len(fields)-1]
			}
		}
		rest = strings.TrimSpace(strings.Join(fields, " "))

		// Extract inline spec like "53°*6" / "750ml*6" and normalize separator to "×".
		if loc := specInlineRe.FindStringIndex(rest); loc != nil && loc[0] >= 0 && loc[1] > loc[0] {
			rawSpec := rest[loc[0]:loc[1]]
			spec = strings.NewReplacer("*", "×", "x", "×", "X", "×").Replace(rawSpec)
			rest = strings.TrimSpace(rest[:loc[0]] + " " + rest[loc[1]:])
		}

		// Extract inline unit+quantity like "瓶2" or "瓶 2".
		if ms := unitQtyInlineRe.FindAllStringSubmatchIndex(rest, -1); len(ms) > 0 {
			m := ms[len(ms)-1]
			if len(m) >= 6 {
				unit = strings.TrimSpace(rest[m[2]:m[3]])
				q := parseQuantityWithUnit(rest[m[4]:m[5]], unit)
				if q != nil {
					qty = q
					rest = strings.TrimSpace(rest[:m[0]] + " " + rest[m[1]:])
				}
			}
		}

		// Extract a simple measure spec like "500ml" / "1.23kg" that sits at the end of the name
		// when PDF text extraction does not include the spec column.
		if spec == "" {
			fields := strings.Fields(rest)
			if len(fields) >= 2 && simpleMeasureUnitRe.MatchString(fields[len(fields)-1]) && numOnlyRe.MatchString(fields[len(fields)-2]) {
				spec = fields[len(fields)-2] + strings.ToLower(fields[len(fields)-1])
				fields = fields[:len(fields)-2]
				rest = strings.TrimSpace(strings.Join(fields, " "))
			} else if len(fields) >= 1 {
				last := strings.TrimSpace(fields[len(fields)-1])
				last = strings.ReplaceAll(last, " ", "")
				if simpleMeasureSpecRe.MatchString(last) {
					if m := simpleMeasureSpecSplitRe.FindStringSubmatch(last); len(m) == 3 {
						spec = m[1] + strings.ToLower(m[2])
						fields = fields[:len(fields)-1]
						rest = strings.TrimSpace(strings.Join(fields, " "))
					}
				}
			}
		}

		// Extract trailing dimension specs like "360*380*190mm" when spec column is missing.
		if spec == "" {
			dimTailRe := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?(?:\s*[*x×]\s*\d+(?:\.\d+)?){1,2}\s*(?:mm|cm|m|g|kg|ml|l)?)$`)
			if loc := dimTailRe.FindStringSubmatchIndex(rest); len(loc) >= 4 {
				raw := strings.TrimSpace(rest[loc[2]:loc[3]])
				raw = strings.NewReplacer("*", "×", "x", "×", "X", "×").Replace(raw)
				raw = strings.ReplaceAll(raw, " ", "")
				if raw != "" && dimensionLikeRe.MatchString(raw) {
					spec = raw
					rest = strings.TrimSpace(rest[:loc[0]])
				}
			}
		}

		rest = strings.TrimSpace(rest)
		if rest == "" {
			return "", "", "", nil, false
		}
		// Only treat as "packed row" if we managed to extract at least one structured field.
		if spec == "" && (unit == "" || qty == nil) {
			return "", "", "", nil, false
		}

		if cat != "" {
			name = "*" + cat + "*" + rest
		} else {
			name = rest
		}

		// If the OCR merged a trailing model code into the name (common when spec column is missing),
		// peel it out so it doesn't pollute the human-readable item name.
		if spec == "" {
			clean, model := peelTrailingModelCodes(name)
			if strings.TrimSpace(clean) != "" && model != "" {
				name = clean
				spec = model
			}
		}
		return name, spec, unit, qty, true
	}

	parseQuantityWithUnit = func(s, unit string) *float64 {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		if m := quantityLabelRe.FindStringSubmatch(s); len(m) > 1 {
			return parseAmount(m[1])
		}
		// If token looks like money (2 decimal places), do not treat it as quantity.
		if moneyLikeRe.MatchString(s) {
			return nil
		}
		// Prefer integers for quantity.
		if integerOnlyRe.MatchString(s) {
			q := parseAmount(s)
			if q == nil {
				return nil
			}
			if *q <= 0 || *q > 9999 {
				return nil
			}
			return q
		}
		// Reject long decimals (common for unit price in PDF text).
		if decimalLikeRe.MatchString(s) {
			parts := strings.SplitN(s, ".", 2)
			if len(parts) == 2 {
				frac := parts[1]
				// More than 3 decimals is almost certainly unit price noise.
				if len(frac) > 3 {
					return nil
				}
				// If unit is a countable unit, require integer quantity.
				if isUnitToken(unit) && unit != "公斤" && unit != "千克" && unit != "克" && unit != "米" {
					return nil
				}
			}
		}
		if !numOnlyRe.MatchString(s) {
			return nil
		}
		q := parseAmount(s)
		if q == nil {
			return nil
		}
		if *q <= 0 || *q > 9999 {
			return nil
		}
		// Prefer "smallish" integers (most invoice quantities are <= 999).
		if strings.Contains(s, ".") && *q > 999 {
			return nil
		}
		return q
	}

	block := make([]string, 0, 64)
	for i := headerEnd + 1; i < len(lines); i++ {
		s := strings.TrimSpace(lines[i])
		if s == "" {
			continue
		}
		if isBlockEndLine(s) {
			break
		}
		block = append(block, normLine(s))
	}
	if len(block) == 0 {
		return nil
	}

	// If the OCR output has an explicit "明细" section marker, prefer it over header heuristics.
	// This avoids treating "密码区" rows (which contain lots of "*" and numbers) as line items.
	forcedStart := -1
	for i, s := range block {
		if strings.TrimSpace(s) == "明细" {
			if i+1 < len(block) {
				forcedStart = i + 1
			}
			break
		}
	}

	// Some PyMuPDF zoned extractions leak the left-most "项目名称" column into the buyer block
	// (before "【明细】"), while the actual "【明细】" row only contains the tail part + model code.
	// Capture the last category-prefixed product prefix above "明细" so we can merge it into the
	// first line item name later.
	pendingFirstItemPrefix := ""
	if forcedStart > 0 {
		cleanPrefixLine := func(s string) string {
			s = strings.TrimSpace(s)
			if s == "" {
				return ""
			}
			stops := []string{
				"购买方信息", "购买方", "销售方信息", "销售方",
				"统一社会信用代码", "纳税人识别号",
				"项目名称", "规格型号", "单位", "数量",
			}
			for _, stop := range stops {
				if idx := strings.Index(s, stop); idx > 0 {
					s = strings.TrimSpace(s[:idx])
				}
			}
			return strings.TrimSpace(s)
		}

		// Prefer scanning the full text (not just the post-header block), since the leaked prefix may be
		// on the same physical line as the table header and thus excluded from "block".
		{
			marker := strings.TrimSpace(block[forcedStart-1])
			detailIdx := -1
			if marker != "" {
				for i, s := range lines {
					if strings.TrimSpace(normLine(s)) == marker {
						detailIdx = i
						break
					}
				}
			}
			if detailIdx > 0 {
				low := detailIdx - 30
				if low < 0 {
					low = 0
				}
				for i := detailIdx - 1; i >= low; i-- {
					cand := cleanPrefixLine(normLine(lines[i]))
					if cand == "" || !strings.HasPrefix(cand, "*") {
						continue
					}
					han := 0
					for _, r := range cand {
						if unicode.Is(unicode.Han, r) {
							han++
						}
					}
					if han < 6 || len([]rune(cand)) < 12 {
						continue
					}
					if isStopLine(cand) {
						continue
					}
					pendingFirstItemPrefix = cand
					break
				}
			}
		}

		if pendingFirstItemPrefix == "" {
			for i := forcedStart - 1; i >= 0; i-- {
				cand := cleanPrefixLine(block[i])
				if cand == "" {
					continue
				}
				if !strings.HasPrefix(cand, "*") {
					continue
				}
				han := 0
				for _, r := range cand {
					if unicode.Is(unicode.Han, r) {
						han++
					}
				}
				if han < 6 || len([]rune(cand)) < 12 {
					continue
				}
				if isHeaderLine(cand) || isStopLine(cand) {
					continue
				}
				pendingFirstItemPrefix = cand
				break
			}
		}
	}

	rowScore := func(s string) int {
		s = strings.TrimSpace(s)
		if s == "" {
			return -10
		}
		score := 0
		// Penalize common totals/footer lines so we don't start in the wrong place (PNG OCR often includes them).
		if strings.Contains(s, "价税合计") || strings.Contains(s, "合计") {
			score -= 6
		}
		if (strings.Contains(s, "小写") || strings.Contains(s, "大写")) && (strings.ContainsRune(s, '￥') || moneyLikeRe.MatchString(s)) {
			score -= 6
		}
		if strings.ContainsAny(s, "圆元角分") && regexp.MustCompile(`[零壹贰叁肆伍陆柒捌玖拾佰仟万萬亿]+`).MatchString(s) {
			score -= 8
		}
		if strings.HasPrefix(s, "*") {
			score += 3
		}
		if taxRateRe.MatchString(s) {
			score += 3
		}
		if strings.ContainsRune(s, '￥') || moneyLikeRe.MatchString(s) {
			score += 2
		}
		if isUnitToken(s) {
			score++
		}
		if labelLineRe.MatchString(s) || labelPrefixRe.MatchString(s) || isStopLine(s) || strings.Contains(s, "下载次数") {
			score -= 6
		}
		if specTokenRe.MatchString(s) {
			score -= 2
		}
		if isLikelyItemNameLine(s) {
			score++
		}
		return score
	}

	startIdx := 0
	{
		bestWinScore := -1 << 30
		bestWinStart := 0
		rowWin := 14
		for i := 0; i < len(block); i++ {
			sum := 0
			for j := i; j < len(block) && j < i+rowWin; j++ {
				sum += rowScore(block[j])
			}
			if sum > bestWinScore {
				bestWinScore = sum
				bestWinStart = i
			}
		}
		startIdx = bestWinStart
		for j := bestWinStart; j < len(block) && j < bestWinStart+rowWin; j++ {
			s := strings.TrimSpace(block[j])
			if s == "" {
				continue
			}
			if strings.HasPrefix(s, "*") || isLikelyItemNameLine(s) {
				startIdx = j
				break
			}
		}
	}

	// PDFs extracted via text often include a lot of non-table content between the header
	// and the first real line item. Anchor on the first tax-rate row, then backtrack to
	// the nearest likely name row to avoid treating invoice meta (e.g. "增值税电子普通发票")
	// as a line item.
	if forcedStart >= 0 {
		startIdx = forcedStart
	} else {
		firstTaxRate := -1
		for i, s := range block {
			if taxRateRe.MatchString(strings.TrimSpace(s)) {
				firstTaxRate = i
				break
			}
		}
		if firstTaxRate >= 0 {
			start := firstTaxRate
			low := firstTaxRate - 18
			if low < 0 {
				low = 0
			}
			starStart := -1
			nameStart := -1
			for j := firstTaxRate; j >= low; j-- {
				cand := strings.TrimSpace(block[j])
				if cand == "" || isHeaderLine(cand) || isStopLine(cand) {
					continue
				}
				// Prefer category-prefixed rows as they are the most stable OCR signal.
				if strings.HasPrefix(cand, "*") {
					starStart = j
					break
				}
				if nameStart == -1 && isLikelyItemNameLine(cand) {
					nameStart = j
				}
			}
			if starStart >= 0 {
				start = starStart
			} else if nameStart >= 0 {
				start = nameStart
			}
			// Override window-based start when we managed to anchor to a likely name line.
			if start >= 0 && start < len(block) {
				cand := strings.TrimSpace(block[start])
				if strings.HasPrefix(cand, "*") || isLikelyItemNameLine(cand) {
					startIdx = start
				}
			}
		}
	}
	if startIdx > 0 && startIdx < len(block) {
		block = block[startIdx:]
	}

	var (
		items           []InvoiceLineItem
		currentName     string
		currentSpec     string
		currentUnit     string
		currentQty      *float64
		currentSawMoney bool
		preserveDup     bool
	)
	flush := func() {
		if pendingFirstItemPrefix != "" && len(items) == 0 && strings.TrimSpace(currentName) != "" {
			cur := strings.TrimSpace(currentName)
			if !strings.HasPrefix(cur, "*") {
				prefixNorm := normalizeName(pendingFirstItemPrefix)
				curNorm := normalizeName(cur)
				if prefixNorm != "" && curNorm != "" &&
					len([]rune(prefixNorm)) > len([]rune(curNorm))+3 &&
					!strings.Contains(curNorm, strings.TrimSpace(prefixNorm)) &&
					!strings.Contains(prefixNorm, strings.TrimSpace(curNorm)) {
					currentName = strings.TrimSpace(pendingFirstItemPrefix + " " + cur)
					pendingFirstItemPrefix = ""
				}
			}
		}
		name := normalizeName(currentName)
		if name == "" {
			currentName = ""
			currentSpec = ""
			currentUnit = ""
			currentQty = nil
			currentSawMoney = false
			return
		}
		qty := currentQty
		if qty == nil && currentSawMoney {
			one := 1.0
			qty = &one
		}
		items = append(items, InvoiceLineItem{
			Name:     name,
			Spec:     strings.TrimSpace(currentSpec),
			Unit:     strings.TrimSpace(currentUnit),
			Quantity: qty,
		})
		currentName = ""
		currentSpec = ""
		currentUnit = ""
		currentQty = nil
		currentSawMoney = false
	}

	isTableExitLine := func(s string) bool {
		// After items, PDF text often continues with buyer/seller blocks and footer.
		if labelLineRe.MatchString(s) || labelPrefixRe.MatchString(s) {
			return true
		}
		if strings.Contains(s, "价税合计") || strings.Contains(s, "合计") {
			return true
		}
		if (strings.Contains(s, "小写") || strings.Contains(s, "大写")) && (strings.ContainsRune(s, '￥') || moneyLikeRe.MatchString(s)) {
			return true
		}
		exitMarkers := []string{
			"销售方", "购买方",
			"收款人", "复核", "开票人",
			"发票专用章",
			"订单号",
			"下载次数",
		}
		for _, m := range exitMarkers {
			if strings.Contains(s, m) {
				return true
			}
		}
		// Chinese uppercase total line often appears after items.
		if strings.ContainsAny(s, "圆元角分") && regexp.MustCompile(`[零壹贰叁肆伍陆柒捌玖拾佰仟万萬亿]+`).MatchString(s) {
			return true
		}
		return false
	}

	for idx := 0; idx < len(block); idx++ {
		s := strings.TrimSpace(block[idx])
		if s == "" || isHeaderLine(s) {
			continue
		}

		// If we already started collecting items (or have a pending current item), stop once we hit non-table sections.
		if (len(items) > 0 || currentName != "") && isTableExitLine(s) {
			if currentName != "" {
				flush()
			}
			break
		}

		// Within an active row, absorb spec/unit/qty tokens as we see them (PDF text often wraps names).
		if currentName != "" && currentQty == nil {
			tn := strings.TrimSpace(strings.ReplaceAll(s, "*", "\u00d7"))
			tnCompact := strings.ReplaceAll(tn, " ", "")
			if currentSpec == "" {
				if m := spacedDim2Re.FindStringSubmatch(strings.TrimSpace(s)); len(m) > 3 {
					currentSpec = fmt.Sprintf("%s×%s%s", m[1], m[2], m[3])
					continue
				}
				if specTokenRe.MatchString(tnCompact) || dimensionLikeRe.MatchString(tnCompact) {
					currentSpec = tnCompact
					continue
				}
			}
			if currentUnit == "" && isUnitToken(s) {
				currentUnit = s
				continue
			}
			// Only parse quantity once we have a unit (prevents misclassifying dimension numbers as qty).
			if currentUnit != "" {
				if q := parseQuantityWithUnit(s, currentUnit); q != nil {
					currentQty = q
					continue
				}
			}
			if moneyLikeRe.MatchString(s) || strings.ContainsRune(s, '\uFFE5') {
				currentSawMoney = true
				continue
			}
			if taxRateRe.MatchString(s) {
				continue
			}
		}

		if isLikelyItemNameLine(s) {
			// Treat subsequent likely-name lines as continuation for wrapped rows (e.g. "*分类*长名称" 下一行续写).
			if currentName != "" && currentQty == nil && currentSpec == "" && currentUnit == "" {
				openASCII := strings.Count(currentName, "(") > strings.Count(currentName, ")")
				openFull := strings.Count(currentName, "\uFF08") > strings.Count(currentName, "\uFF09")
				if (strings.HasPrefix(currentName, "*") && !strings.HasPrefix(s, "*")) || ((openASCII || openFull) && !strings.HasPrefix(s, "*")) {
					currentName = strings.TrimSpace(currentName + " " + s)
					continue
				}
			}
			if currentName != "" {
				flush()
			}
			// Didi-style merged rows: repeated service names in one line.
			if strings.Contains(s, "运输服务") && strings.Contains(s, "客运服务费") &&
				(strings.Count(s, "运输服务") >= 2 || strings.Count(s, "客运服务费") >= 2) {
				clean := strings.NewReplacer("*", "", " ", "").Replace(s)
				spec := ""
				if strings.Contains(clean, "无") {
					spec = "无"
				}
				unit := ""
				qty := (*float64)(nil)
				if m := regexp.MustCompile(`(次)\s*(\d+(?:\.\d+)?)$`).FindStringSubmatch(s); len(m) > 2 {
					unit = m[1]
					qty = parseQuantityWithUnit(m[2], unit)
				} else if m := regexp.MustCompile(`(次)(\d+(?:\.\d+)?)$`).FindStringSubmatch(clean); len(m) > 2 {
					unit = m[1]
					qty = parseQuantityWithUnit(m[2], unit)
				}
				name := "运输服务 客运服务费"
				if spec != "" || unit != "" || qty != nil {
					items = append(items, InvoiceLineItem{Name: normalizeName(name), Spec: spec, Unit: unit, Quantity: qty})
					items = append(items, InvoiceLineItem{Name: normalizeName(name), Spec: spec, Unit: unit, Quantity: qty})
					preserveDup = true
					continue
				}
			}
			// Multi-row packed into one line (PDF text): repeated "*分类*" segments with shared tail columns.
			if multi := parseMultiPackedItems(s); len(multi) >= 2 {
				items = append(items, multi...)
				preserveDup = true
				continue
			}
			// Packed single-line rows (common in PDF text): name/spec/unit/qty may be in one line.
			if n, sp, un, q, ok := parsePackedRow(s); ok {
				currentName = n
				currentSpec = sp
				currentUnit = un
				currentQty = q
				currentSawMoney = false
				continue
			}
			// Category-prefixed rows sometimes end with a bare quantity without a unit
			// (e.g. "*日用杂品*包装费配送费1"). Only apply when the name part contains no other digits.
			if strings.HasPrefix(s, "*") {
				trimmed := strings.TrimSpace(s)
				cat := ""
				rest := trimmed
				if loc := categoryPrefixRe.FindStringSubmatchIndex(trimmed); len(loc) >= 2 {
					if len(loc) >= 4 && loc[2] >= 0 && loc[3] > loc[2] {
						cat = strings.TrimSpace(trimmed[loc[2]:loc[3]])
					}
					rest = strings.TrimSpace(trimmed[loc[1]:])
				}
				// Only accept single-digit suffix quantities to avoid corrupting product names like "青花30".
				if m := regexp.MustCompile(`^([^\d]{2,200})(\d)$`).FindStringSubmatch(rest); len(m) > 2 {
					if q := parseAmount(m[2]); q != nil {
						namePart := strings.TrimSpace(m[1])
						if cat != "" {
							namePart = "*" + cat + "*" + namePart
						}
						currentName = namePart
						currentSpec = ""
						currentUnit = ""
						currentQty = q
						currentSawMoney = false
						continue
					}
				}
			}
			// If unit+qty is attached to the name, split them here.
			if m := unitQtySuffixRe.FindStringSubmatch(s); len(m) > 3 {
				namePart := strings.TrimSpace(m[1])
				unitPart := strings.TrimSpace(m[2])
				if namePart != "" {
					if q := parseQuantityWithUnit(m[3], unitPart); q != nil {
						// If model codes were merged into the name, peel them into spec.
						cleanName, model := peelTrailingModelCodes(namePart)
						currentName = namePart
						if strings.TrimSpace(cleanName) != "" {
							currentName = cleanName
						}
						currentSpec = ""
						if model != "" {
							currentSpec = model
						}
						currentUnit = unitPart
						currentQty = q
						currentSawMoney = false
						continue
					}
				}
			}
			currentName = s
			currentSpec = ""
			currentUnit = ""
			currentQty = nil
			currentSawMoney = false
			continue
		}

		// Merge continuation lines for long item names (before quantity is found).
		if currentName != "" && currentQty == nil {
			tn := strings.TrimSpace(strings.ReplaceAll(s, "*", "\u00d7"))
			tnCompact := strings.ReplaceAll(tn, " ", "")

			// If unit and quantity are split across lines (common in PDF text), capture them.
			if isUnitToken(strings.TrimSpace(s)) {
				if currentUnit == "" {
					currentUnit = strings.TrimSpace(s)
				}
				continue
			}
			if currentUnit != "" && integerOnlyRe.MatchString(strings.TrimSpace(s)) {
				if q := parseQuantityWithUnit(strings.TrimSpace(s), currentUnit); q != nil {
					currentQty = q
					currentSawMoney = false
					continue
				}
			}

			// Capture dimension/spec fragments instead of dropping them or treating as a new item.
			if m := spacedDim2Re.FindStringSubmatch(strings.TrimSpace(s)); len(m) > 3 {
				part := fmt.Sprintf("%s×%s%s", m[1], m[2], m[3])
				part = strings.ReplaceAll(part, " ", "")
				if currentSpec == "" {
					currentSpec = part
				} else if !strings.Contains(currentSpec, part) {
					currentSpec = strings.TrimSpace(currentSpec + "×" + part)
				}
				continue
			}
			if dimensionLikeRe.MatchString(tnCompact) {
				if currentSpec == "" {
					currentSpec = tnCompact
				} else if !strings.Contains(currentSpec, tnCompact) {
					currentSpec = strings.TrimSpace(currentSpec + "×" + tnCompact)
				}
				continue
			}
			if specTokenRe.MatchString(tnCompact) {
				// Treat as spec token, not a name continuation.
				if currentSpec == "" {
					currentSpec = tnCompact
				}
				continue
			}
			if numOnlyRe.MatchString(strings.TrimSpace(s)) {
				// A standalone number (e.g. "360") is often a spec fragment when followed by dimension units.
				if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && v >= 10 && v <= 9999 {
					if currentSpec == "" {
						currentSpec = fmt.Sprintf("%d", v)
					} else if regexp.MustCompile(`^\\d{2,4}$`).MatchString(currentSpec) {
						currentSpec = strings.TrimSpace(currentSpec + "×" + fmt.Sprintf("%d", v))
					}
				}
				continue
			}
			if taxRateRe.MatchString(s) || moneyLikeRe.MatchString(s) {
				continue
			}
			if len([]rune(s)) >= 2 && len([]rune(s)) <= 80 {
				currentName = strings.TrimSpace(currentName + " " + s)
			}
		}
	}

	if currentName != "" {
		flush()
	}

	if preserveDup {
		return items
	}

	// De-duplicate by name (keep the first occurrence).
	seen := make(map[string]struct{}, len(items))
	out := make([]InvoiceLineItem, 0, len(items))
	for _, it := range items {
		if it.Name == "" {
			continue
		}
		if _, ok := seen[it.Name]; ok {
			continue
		}
		seen[it.Name] = struct{}{}
		out = append(out, it)
	}
	return out
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
		searchFrom := 0
		for {
			idx := strings.Index(text[searchFrom:], pattern)
			if idx == -1 {
				break
			}
			idx += searchFrom
			after := idx + len(pattern)
			// Ignore merged-label fragments like "销售方信息" that appear inside buyer lines.
			if after < len(text) && strings.HasPrefix(text[after:], "信息") {
				searchFrom = after
				continue
			}
			if sellerMarkerIndex == -1 || idx < sellerMarkerIndex {
				sellerMarkerIndex = idx
			}
			break
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

	// Extract embedded personal names that appear inside noisy lines, e.g. "武亚峰13512168111"
	// where the line may also contain password symbols and be rejected by line-level heuristics.
	phoneNameRe := regexp.MustCompile(`([\p{Han}]{2,6})\s*\d{11}`)
	phoneNameMatches := phoneNameRe.FindAllStringSubmatchIndex(text, -1)
	for _, m := range phoneNameMatches {
		if len(m) < 4 {
			continue
		}
		addName(text[m[2]:m[3]], m[0])
	}

	// Extract company/person names that appear at the tail of multi-label lines like:
	// "名称: 开户行及账号: 地址、电话: 纳税人识别号: 中国移动通信集团上海有限公司"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !(strings.Contains(line, "纳税人识别号") || strings.Contains(line, "统一社会信用代码")) {
			continue
		}
		candidate := extractNameFromTaxIDLabelLine(line)
		if candidate == "" || isBadPartyNameCandidate(candidate) {
			continue
		}
		pos := strings.Index(text, line)
		if pos < 0 {
			pos = 0
		}
		addName(candidate, pos)
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

	pickClosest := func(markerIdx int, used map[string]bool, minPos, maxPos int) *string {
		pickFrom := func(filterRange bool) *string {
			bestDist := int(^uint(0) >> 1)
			bestName := ""
			for _, entry := range names {
				if used[entry.name] {
					continue
				}
				if filterRange && (entry.position < minPos || entry.position > maxPos) {
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

		// Prefer names within the relevant section window to avoid picking footer names
		// (e.g. "复核: 张唯...") as the buyer.
		if cand := pickFrom(true); cand != nil {
			return cand
		}
		return pickFrom(false)
	}

	used := make(map[string]bool)
	if buyerMarkerIndex != -1 {
		// Buyer name may appear slightly above the "购/购买方" marker in top-bottom layouts,
		// so allow a small lookback window while still stopping before the seller section.
		buyerMin := buyerMarkerIndex - 800
		if buyerMin < 0 {
			buyerMin = 0
		}
		buyerMax := len(text)
		if sellerMarkerIndex != -1 && sellerMarkerIndex > buyerMarkerIndex {
			buyerMax = sellerMarkerIndex
		}
		// When buyer/seller sections are far apart (top-bottom layout), avoid picking names
		// that are very close to the seller marker (often footer fields like "复核: 张唯").
		if sellerMarkerIndex != -1 && sellerMarkerIndex > buyerMarkerIndex && sellerMarkerIndex-buyerMarkerIndex > 500 {
			cut := sellerMarkerIndex - 220
			if cut > buyerMin {
				buyerMax = cut
			}
		}
		buyer = pickClosest(buyerMarkerIndex, used, buyerMin, buyerMax)
		if buyer != nil {
			used[*buyer] = true
		}
	}
	if sellerMarkerIndex != -1 {
		// Seller name may also appear slightly above the "销/销售方" marker; keep a lookback
		// but don't drift into the buyer header too much.
		sellerMin := sellerMarkerIndex - 800
		if sellerMin < 0 {
			sellerMin = 0
		}
		if buyerMarkerIndex != -1 && sellerMin < buyerMarkerIndex {
			sellerMin = buyerMarkerIndex
		}
		seller = pickClosest(sellerMarkerIndex, used, sellerMin, len(text))
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
	return s.ParseInvoiceDataWithMeta(text, nil)
}

// ParseInvoiceDataWithMeta is like ParseInvoiceData but also accepts optional PDF text layout metadata.
// When metadata is present (PyMuPDF zoned extraction), it can be used as a fallback to fix ambiguous fields.
func (s *OCRService) ParseInvoiceDataWithMeta(text string, meta *PDFTextCLIResponse) (*InvoiceExtractedData, error) {
	data := &InvoiceExtractedData{RawText: text}

	parsedText := normalizeInvoiceTextForParsing(text)

	// Special invoice type: 航空运输电子客票行程单 (air ticket itinerary).
	// The layout is very different from VAT invoices, and fields like seller/buyer/amount do not
	// follow the "购买方/销售方/价税合计" table patterns.
	isAirTicketItinerary := func(s string) bool {
		if strings.Contains(s, "航空运输电子客票行程单") {
			return true
		}
		// Some OCR results miss "行程单" but still contain these strong markers.
		if strings.Contains(s, "航空运输电子客票") && strings.Contains(s, "电子客票号码") {
			return true
		}
		return false
	}

	airTicketDetected := isAirTicketItinerary(parsedText)

	if airTicketDetected {
		// Invoice date: 填开日期
		if m := regexp.MustCompile(`填开日期[:：]?\s*(\d{4}年\d{1,2}月\d{1,2}日)`).FindStringSubmatch(parsedText); len(m) > 1 {
			setStringWithSourceAndConfidence(&data.InvoiceDate, &data.InvoiceDateSource, &data.InvoiceDateConfidence, m[1], "air_ticket_fill_date", 0.95)
		}

		// Buyer: passenger name (旅客姓名)
		passenger := ""
		// Prefer raw OCR text for this field because normalization may move/merge lines.
		if m := regexp.MustCompile(`(?m)^([\p{Han}·]{2,20})\s*[\r]?\n\s*旅客姓名\s*$`).FindStringSubmatch(text); len(m) > 1 {
			passenger = cleanupName(strings.TrimSpace(m[1]))
		}
		if passenger == "" {
			if m := regexp.MustCompile(`旅客姓名[:：]?\s*([^\n\r]+)`).FindStringSubmatch(text); len(m) > 1 {
				passenger = cleanupName(strings.TrimSpace(m[1]))
			}
		}
		if passenger == "" {
			// Fallback: try a best-effort neighbor line lookup.
			if m := regexp.MustCompile(`旅客姓名[:：]?\s*([^\n\r]+)`).FindStringSubmatch(parsedText); len(m) > 1 {
				passenger = cleanupName(strings.TrimSpace(m[1]))
			}
		}
		if passenger == "" {
			// Common OCR layout: "<姓名>\n旅客姓名"
			lines := strings.Split(text, "\n")
			for i, line := range lines {
				l := strings.TrimSpace(strings.TrimRight(line, "\r"))
				if l == "" {
					continue
				}
				if l == "旅客姓名" || strings.HasPrefix(l, "旅客姓名") {
					// Prefer previous non-empty line; if not plausible, try next.
					for j := i - 1; j >= 0; j-- {
						cand := strings.TrimSpace(strings.TrimRight(lines[j], "\r"))
						if cand == "" {
							continue
						}
						passenger = cleanupName(cand)
						break
					}
					if passenger == "" && i+1 < len(lines) {
						cand := strings.TrimSpace(strings.TrimRight(lines[i+1], "\r"))
						passenger = cleanupName(cand)
					}
					break
				}
			}
		}
		if passenger != "" && passenger != "个人" && !isBadPartyNameCandidate(passenger) {
			setStringWithSourceAndConfidence(&data.BuyerName, &data.BuyerNameSource, &data.BuyerNameConfidence, passenger, "air_ticket_passenger", 0.95)
		}

		// Seller: 填开单位
		if m := regexp.MustCompile(`填开单位[:：]?\s*([^\n\r]+)`).FindStringSubmatch(parsedText); len(m) > 1 {
			val := cleanupName(strings.TrimSpace(m[1]))
			if val != "" && !isBadPartyNameCandidate(val) {
				setStringWithSourceAndConfidence(&data.SellerName, &data.SellerNameSource, &data.SellerNameConfidence, val, "air_ticket_issuer", 0.95)
			}
		}

		// Total amount: 合计 CNY 2220.00
		if m := regexp.MustCompile(`(?m)^合计\s*(?:CNY\s*)?([\d,.]+)\s*$`).FindStringSubmatch(parsedText); len(m) > 1 {
			if amt := parseAmount(m[1]); amt != nil {
				setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, amt, "air_ticket_total", 0.95)
			}
		}
		if data.Amount == nil {
			// Two-line variant:
			// 合计
			// CNY 2220.00
			if m := regexp.MustCompile(`(?s)合计\s*[\n\r]+\s*(?:CNY\s*)?([\d,.]+)`).FindStringSubmatch(parsedText); len(m) > 1 {
				if amt := parseAmount(m[1]); amt != nil {
					setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, amt, "air_ticket_total", 0.95)
				}
			}
		}

		// VAT tax amount: 增值税税额 CNY 179.17
		if m := regexp.MustCompile(`增值税税额[:：]?\s*(?:CNY\s*)?([\d,.]+)`).FindStringSubmatch(parsedText); len(m) > 1 {
			if tax := parseAmount(m[1]); tax != nil {
				setAmountWithSourceAndConfidence(&data.TaxAmount, &data.TaxAmountSource, &data.TaxAmountConfidence, tax, "air_ticket_vat_tax", 0.9)
			}
		}

		// Build a simple 1-line item for better UX (route + flight number + date/time).
		if len(data.Items) == 0 {
			lines := strings.Split(parsedText, "\n")
			findAfterPrefix := func(prefixes ...string) string {
				for _, line := range lines {
					l := strings.TrimSpace(line)
					for _, p := range prefixes {
						if strings.HasPrefix(l, p) {
							v := strings.TrimSpace(strings.TrimPrefix(l, p))
							v = strings.TrimSpace(strings.TrimPrefix(v, "："))
							v = strings.TrimSpace(strings.TrimPrefix(v, ":"))
							if v != "" {
								return removeChineseInlineSpaces(v)
							}
						}
					}
				}
				return ""
			}

			origin := findAfterPrefix("自：", "自:")
			dest := ""
			// There may be multiple "至：" placeholders; prefer the first non-empty.
			for _, line := range lines {
				l := strings.TrimSpace(line)
				if strings.HasPrefix(l, "至：") || strings.HasPrefix(l, "至:") {
					v := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(l, "至："), "至:"))
					v = strings.TrimSpace(strings.TrimPrefix(v, "："))
					v = strings.TrimSpace(strings.TrimPrefix(v, ":"))
					v = removeChineseInlineSpaces(v)
					if v != "" {
						dest = v
						break
					}
				}
			}

			flightNo := ""
			if m := regexp.MustCompile(`\b([A-Z]{2}\d{3,4})\b`).FindStringSubmatch(parsedText); len(m) > 1 {
				flightNo = m[1]
			}
			flightDate := ""
			flightTime := ""
			// Prefer the first date after the flight number if possible.
			if flightNo != "" {
				if m := regexp.MustCompile(regexp.QuoteMeta(flightNo) + `(?s).{0,120}?(\d{4}年\d{1,2}月\d{1,2}日)`).FindStringSubmatch(parsedText); len(m) > 1 {
					flightDate = m[1]
				}
			}
			if flightDate == "" {
				if m := regexp.MustCompile(`(\d{4}年\d{1,2}月\d{1,2}日)`).FindStringSubmatch(parsedText); len(m) > 1 {
					flightDate = m[1]
				}
			}
			if m := regexp.MustCompile(`\b(\d{1,2}:\d{2})\b`).FindStringSubmatch(parsedText); len(m) > 1 {
				flightTime = m[1]
			}

			itemNameParts := make([]string, 0, 6)
			itemNameParts = append(itemNameParts, "航空运输服务")
			if origin != "" && dest != "" {
				itemNameParts = append(itemNameParts, fmt.Sprintf("%s→%s", origin, dest))
			} else if origin != "" {
				itemNameParts = append(itemNameParts, origin)
			}
			if flightNo != "" {
				itemNameParts = append(itemNameParts, flightNo)
			}
			if flightDate != "" {
				itemNameParts = append(itemNameParts, flightDate)
			}
			if flightTime != "" {
				itemNameParts = append(itemNameParts, flightTime)
			}

			name := strings.TrimSpace(strings.Join(itemNameParts, " "))
			if name != "" {
				q := 1.0
				data.Items = []InvoiceLineItem{{
					Name:     name,
					Unit:     "次",
					Quantity: &q,
				}}
			}
		}
	}

	// Special invoice type: railway e-ticket (电子发票（铁路电子客票）).
	// The seller block is often missing a real company name; OCR may mistakenly pick the tax bureau header.
	// Build a stable seller + one line item for better UX.
	isRailwayETicket := func(s string) bool {
		if strings.Contains(s, "\u94c1\u8def\u7535\u5b50\u5ba2\u7968") {
			return true
		}
		// Some PDFs omit brackets but still contain strong markers.
		if strings.Contains(s, "\u7535\u5b50\u53d1\u7968") && strings.Contains(s, "\u94c1\u8def") && strings.Contains(s, "\u5ba2\u7968") {
			return true
		}
		return false
	}

		railTicketDetected := isRailwayETicket(parsedText) || isRailwayETicket(text)
		if railTicketDetected {
			// Invoice date: 开票日期 / 章开票日期
			if m := regexp.MustCompile(`(?:开票日期|章开票日期)[:：]?\s*(\d{4}年\d{1,2}月\d{1,2}日)`).FindStringSubmatch(parsedText); len(m) > 1 {
				setStringWithSourceAndConfidence(&data.InvoiceDate, &data.InvoiceDateSource, &data.InvoiceDateConfidence, m[1], "rail_ticket_date", 0.9)
			}

			// Buyer: 购买方名称
			if m := regexp.MustCompile(`购买方名称[:：]?\s*([^\n\r]+)`).FindStringSubmatch(parsedText); len(m) > 1 {
				val := cleanupName(strings.TrimSpace(m[1]))
				if val != "" && val != "\u4e2a\u4eba" && !isBadPartyNameCandidate(val) {
					setStringWithSourceAndConfidence(&data.BuyerName, &data.BuyerNameSource, &data.BuyerNameConfidence, val, "rail_ticket_buyer", 0.9)
				}
		}

		// Seller: railway e-tickets are issued by China Railway; OCR may mistakenly pick tax-bureau headers as "seller".
		setStringWithSourceAndConfidence(&data.SellerName, &data.SellerNameSource, &data.SellerNameConfidence, "\u4e2d\u56fd\u94c1\u8def", "rail_ticket_seller", 0.85)

			// Amount: 票价
			if m := regexp.MustCompile(`票价[:：]?\s*(?:￥|¥)?\s*([\d,.]+)`).FindStringSubmatch(parsedText); len(m) > 1 {
				if amt := parseAmount(m[1]); amt != nil {
					setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, amt, "rail_ticket_price", 0.9)
				}
			}

		// Build a simple 1-line item (route + train + seat).
		if len(data.Items) == 0 {
			origin := ""
			dest := ""
			if matches := regexp.MustCompile(`(?m)^([\p{Han}]{2,10})\s*[A-Za-z]*\s*站`).FindAllStringSubmatch(parsedText, -1); len(matches) > 0 {
				origin = strings.TrimSpace(matches[0][1])
				if len(matches) > 1 {
					dest = strings.TrimSpace(matches[1][1])
				}
			}

			trainNo := ""
			if m := regexp.MustCompile(`\b([GDCKTZYLSP]\d{1,4})\b`).FindStringSubmatch(parsedText); len(m) > 1 {
				trainNo = m[1]
			}
			seat := ""
			if m := regexp.MustCompile(`(商务座|特等座|一等座|二等座|三等座|软卧|硬卧|软座|硬座|无座)`).FindStringSubmatch(parsedText); len(m) > 1 {
				seat = m[1]
			}

			itemNameParts := make([]string, 0, 6)
			itemNameParts = append(itemNameParts, "\u94c1\u8def\u5ba2\u8fd0\u670d\u52a1")
			if origin != "" && dest != "" {
				itemNameParts = append(itemNameParts, origin+"->"+dest)
			} else if origin != "" {
				itemNameParts = append(itemNameParts, origin)
			}
			if trainNo != "" {
				itemNameParts = append(itemNameParts, trainNo)
			}
			if seat != "" {
				itemNameParts = append(itemNameParts, seat)
			}

			name := strings.TrimSpace(strings.Join(itemNameParts, " "))
			if name != "" {
				q := 1.0
				data.Items = []InvoiceLineItem{{
					Name:     name,
					Unit:     "\u6b21",
					Quantity: &q,
				}}
			}
		}
	}

	isPlausibleInvoiceYear := func(y int) bool {
		nowY := time.Now().Year()
		return y >= 2000 && y <= nowY+2
	}
	extractYMD := func(s string) (y, m, d int, ok bool) {
		mm := regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日`).FindStringSubmatch(s)
		if len(mm) != 4 {
			return 0, 0, 0, false
		}
		yy, err1 := strconv.Atoi(mm[1])
		mo, err2 := strconv.Atoi(mm[2])
		dd, err3 := strconv.Atoi(mm[3])
		if err1 != nil || err2 != nil || err3 != nil || mo < 1 || mo > 12 || dd < 1 || dd > 31 {
			return 0, 0, 0, false
		}
		return yy, mo, dd, true
	}

	// Prefer the actual invoice number (usually 8 digits). Some OCR outputs place code/number
	// in a swapped order like "发票号码: 发票代码: <code> <number>".
	pickInvoiceNumberFromPair := func(a, b string) string {
		a = onlyDigits(a)
		b = onlyDigits(b)
		if len(a) >= 20 {
			return a
		}
		if len(b) >= 20 {
			return b
		}
		if len(a) == 8 && len(b) != 8 {
			return a
		}
		if len(b) == 8 && len(a) != 8 {
			return b
		}
		if len(a) == 8 && len(b) == 8 {
			return a
		}
		return ""
	}
	pairRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?s)发票号码[：:]?.{0,60}?发票代码[：:]?.{0,60}?(\d{8,25}).{0,20}?(\d{8,25})`),
		regexp.MustCompile(`(?s)发票代码[：:]?.{0,60}?发票号码[：:]?.{0,60}?(\d{8,25}).{0,20}?(\d{8,25})`),
	}
	for _, re := range pairRegexes {
		if match := re.FindStringSubmatch(parsedText); len(match) > 2 {
			if inv := pickInvoiceNumberFromPair(match[1], match[2]); inv != "" {
				setStringWithSourceAndConfidence(&data.InvoiceNumber, &data.InvoiceNumberSource, &data.InvoiceNumberConfidence, inv, "label_pair", 0.95)
				break
			}
		}
	}

	invoiceNumRegexes := []*regexp.Regexp{
		regexp.MustCompile(`发票号码[：:]?\s*[\n\r]?\s*(\d{20,25}|\d{8})`),
		regexp.MustCompile(`发票代码[：:]?\s*[\n\r]?\s*(\d{10,12})`),
		regexp.MustCompile(`No[\.:]?\s*[\n\r]?\s*(\d+)`),
	}
	for _, re := range invoiceNumRegexes {
		if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
			setStringWithSourceAndConfidence(&data.InvoiceNumber, &data.InvoiceNumberSource, &data.InvoiceNumberConfidence, match[1], "label", 0.9)
			break
		}
	}
	if data.InvoiceNumber == nil {
		standaloneNumRegex := regexp.MustCompile(`(?m)^(\d{8}|\d{20,25})$`)
		if match := standaloneNumRegex.FindStringSubmatch(parsedText); len(match) > 1 {
			setStringWithSourceAndConfidence(&data.InvoiceNumber, &data.InvoiceNumberSource, &data.InvoiceNumberConfidence, match[1], "standalone", 0.7)
		}
	}

	dateRegexes := []*regexp.Regexp{
		regexp.MustCompile(`开票日期[：:]?\s*[\n\r]?\s*(\d{4}年\d{1,2}月\d{1,2}日)`),
		regexp.MustCompile(`开票日期[：:]?\s*[\n\r]?\s*(\d{4}-\d{2}-\d{2})`),
		regexp.MustCompile(`日期[：:]?\s*[\n\r]?\s*(\d{4}年\d{1,2}月\d{1,2}日)`),
	}
	for _, re := range dateRegexes {
		if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
			setStringWithSourceAndConfidence(&data.InvoiceDate, &data.InvoiceDateSource, &data.InvoiceDateConfidence, match[1], "label", 0.9)
			break
		}
	}
	// Handle header dates printed as spaced digits: "2025  07  02" without 年/月/日 units.
	if data.InvoiceDate == nil {
		headerSpacedDigits := regexp.MustCompile(`(?s)(?:开票日期|日期)[：:]?.{0,120}?(\d{4})\s+(\d{1,2})\s+(\d{1,2})`)
		if match := headerSpacedDigits.FindStringSubmatch(parsedText); len(match) > 3 {
			mi, _ := strconv.Atoi(match[2])
			di, _ := strconv.Atoi(match[3])
			date := fmt.Sprintf("%s年%02d月%02d日", match[1], mi, di)
			setStringWithSourceAndConfidence(&data.InvoiceDate, &data.InvoiceDateSource, &data.InvoiceDateConfidence, date, "spaced_digits", 0.85)
		}
	}
	if data.InvoiceDate == nil {
		if match := spaceDelimitedDatePattern.FindStringSubmatch(parsedText); len(match) > 3 {
			date := fmt.Sprintf("%s年%s月%s日", match[1], match[2], match[3])
			setStringWithSourceAndConfidence(&data.InvoiceDate, &data.InvoiceDateSource, &data.InvoiceDateConfidence, date, "spaced_label", 0.8)
		}
	}
	if data.InvoiceDate == nil {
		standaloneDateRegex := regexp.MustCompile(`(\d{4}年\d{1,2}月\d{1,2}日)`)
		if match := standaloneDateRegex.FindStringSubmatch(parsedText); len(match) > 1 {
			setStringWithSourceAndConfidence(&data.InvoiceDate, &data.InvoiceDateSource, &data.InvoiceDateConfidence, match[1], "standalone", 0.7)
		}
	}
	// Repair implausible years (common in Didi invoices where check code digits stick to the year).
	if data.InvoiceDate != nil {
		if yy, mo, dd, ok := extractYMD(*data.InvoiceDate); ok && !isPlausibleInvoiceYear(yy) {
			// Find a plausible 4-digit year in the "开票日期" header line.
			headerLine := ""
			for _, line := range strings.Split(parsedText, "\n") {
				if strings.Contains(line, "开票日期") {
					headerLine = line
					break
				}
			}
			yearCand := 0
			if headerLine != "" {
				for _, m := range regexp.MustCompile(`\b(\d{4})\b`).FindAllStringSubmatch(headerLine, -1) {
					if len(m) < 2 {
						continue
					}
					y, _ := strconv.Atoi(m[1])
					if isPlausibleInvoiceYear(y) {
						yearCand = y
						break
					}
				}
			}
			// Also try anywhere near the header if not found.
			if yearCand == 0 {
				for _, m := range regexp.MustCompile(`\b(\d{4})\b`).FindAllStringSubmatch(parsedText, -1) {
					if len(m) < 2 {
						continue
					}
					y, _ := strconv.Atoi(m[1])
					if isPlausibleInvoiceYear(y) {
						yearCand = y
						break
					}
				}
			}
			if yearCand != 0 {
				fixed := fmt.Sprintf("%04d年%02d月%02d日", yearCand, mo, dd)
				v := fixed
				data.InvoiceDate = &v
				data.InvoiceDateSource = "year_repair"
				data.InvoiceDateConfidence = 0.9
			}
		}
	}

	// Some invoices print a "价税合计(小写)" line that includes multiple numeric amounts after 小写,
	// e.g. "... (小写) 396.04 ￥ 400.00 ￥ 3.96". Prefer the largest as total, and when possible
	// derive tax from (total - net) by matching another extracted number.
	if data.Amount == nil || data.TaxAmount == nil {
		decimal2Re := regexp.MustCompile(`\d+\.\d{2}`)
		for _, line := range strings.Split(parsedText, "\n") {
			if !strings.Contains(line, "价税合计") || !strings.Contains(line, "小写") {
				continue
			}
			rawNums := decimal2Re.FindAllString(line, -1)
			if len(rawNums) < 2 {
				continue
			}
			vals := make([]float64, 0, len(rawNums))
			for _, s := range rawNums {
				if v := parseAmount(s); v != nil && *v >= MinValidAmount && *v <= 1e7 {
					vals = append(vals, *v)
				}
			}
			if len(vals) < 2 {
				continue
			}
			total := vals[0]
			for _, v := range vals[1:] {
				if v > total {
					total = v
				}
			}
			totalPtr := total
			setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, &totalPtr, "tax_total_xiaoxie_multi", 0.95)

			if data.TaxAmount == nil && len(vals) >= 3 {
				const eps = 0.02
				bestTax := 0.0
				for _, net := range vals {
					if net <= 0 || net >= total {
						continue
					}
					// Net amount should be close to total; ignore very small values that are likely tax.
					if net < total*0.5 {
						continue
					}
					diff := total - net
					if diff <= 0 || diff >= total {
						continue
					}
					// Tax is usually much smaller than total; ignore implausibly large diffs.
					if diff > total*0.5 {
						continue
					}
					for _, cand := range vals {
						if cand <= 0 || cand >= total {
							continue
						}
						if cand > total*0.5 {
							continue
						}
						if math.Abs(cand-diff) <= eps {
							if bestTax == 0 || cand > bestTax {
								bestTax = cand
							}
						}
					}
				}
				if bestTax > 0 && bestTax < total {
					taxPtr := bestTax
					setAmountWithSourceAndConfidence(&data.TaxAmount, &data.TaxAmountSource, &data.TaxAmountConfidence, &taxPtr, "tax_total_xiaoxie_multi", 0.95)
				}
			}
			if data.Amount != nil && (data.TaxAmount != nil || len(vals) >= 2) {
				break
			}
		}
	}

	// Prefer totals line parsing that contains both net amount, total amount, and tax amount, e.g.:
	// "价税合计（大写） ... ¥ 3049.51 （小写） ¥ 3080.00 ¥ 30.49"
	// Some PDF text merges multiple item rows and also glues "（小写）" without a colon, so we handle it explicitly.
	taxTotalThreeRe := regexp.MustCompile(`(?s)价税合计.{0,200}?(?:[¥￥]\s*([\d,.]+)).{0,200}?(?:[（(]?\s*小写\s*[）)])\s*(?:[¥￥]\s*([\d,.]+)).{0,80}?(?:[¥￥]\s*([\d,.]+))`)
	if m := taxTotalThreeRe.FindStringSubmatch(parsedText); len(m) > 3 {
		if total := parseAmount(m[2]); total != nil {
			setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, total, "tax_total_three", 0.95)
		}
		if tax := parseAmount(m[3]); tax != nil {
			setAmountWithSourceAndConfidence(&data.TaxAmount, &data.TaxAmountSource, &data.TaxAmountConfidence, tax, "tax_total_three", 0.95)
		}
	}
	// Two-amount variant: compute tax = total - net.
	if data.Amount == nil || data.TaxAmount == nil {
		taxTotalTwoRe := regexp.MustCompile(`(?s)价税合计.{0,200}?(?:[¥￥]\s*([\d,.]+)).{0,200}?(?:[（(]?\s*小写\s*[）)])\s*(?:[¥￥]\s*([\d,.]+))`)
		if m := taxTotalTwoRe.FindStringSubmatch(parsedText); len(m) > 2 {
			net := parseAmount(m[1])
			total := parseAmount(m[2])
			if total != nil && (data.Amount == nil || *data.Amount == 0) {
				setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, total, "tax_total_two", 0.9)
			}
			if data.TaxAmount == nil && net != nil && total != nil {
				diff := *total - *net
				if diff > 0 && diff < *total {
					v := diff
					setAmountWithSourceAndConfidence(&data.TaxAmount, &data.TaxAmountSource, &data.TaxAmountConfidence, &v, "tax_total_two_diff", 0.85)
				}
			}
		}
	}

	// Some invoices have a dedicated net/tax summary line like:
	// "合计 ￥107.79 ￥14.01"
	// Prefer this over per-line item "税额" matches.
	if data.TaxAmount == nil {
		netTaxLineRe := regexp.MustCompile(`(?m)^合计\s*[¥￥]?\s*([\d,.]+)\s*[¥￥]?\s*([\d,.]+)\s*$`)
		if m := netTaxLineRe.FindStringSubmatch(parsedText); len(m) > 2 {
			tax := parseAmount(m[2])
			if tax != nil {
				setAmountWithSourceAndConfidence(&data.TaxAmount, &data.TaxAmountSource, &data.TaxAmountConfidence, tax, "sum_net_tax_line", 0.9)
			}
			// Keep amount parsing anchored on explicit totals (价税合计/小写) when present.
			// Some invoices omit the 价税合计 line though; for those, we can derive total as net+tax.
			if data.Amount == nil && !strings.Contains(parsedText, "价税合计") {
				net := parseAmount(m[1])
				if net != nil && tax != nil {
					v := *net + *tax
					setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, &v, "sum_net_tax_line_total", 0.7)
				}
			}
		}
	}

	amountRegexes := []struct {
		re   *regexp.Regexp
		src  string
		conf float64
	}{
		{regexp.MustCompile(`价税合计\s*[（(]?小写[）)]?\s*[:：]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`), "tax_total_label", 0.9},
		{regexp.MustCompile(`(?s)价税合计[（(]?大写[）)]?.{0,240}?(?:[（(]?\s*小写\s*[）)])\s*[¥￥]?\s*([\d,.]+)`), "tax_total_label_daxie_then_xiaoxie", 0.9},
		{regexp.MustCompile(`总计\s*[:：]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`), "total_label", 0.85},
		{regexp.MustCompile(`合计\s*[:：]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`), "sum_label", 0.8},
		{regexp.MustCompile(`合计金额[（(]?小写[）)]?\s*[:：]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`), "sum_amount_label", 0.8},
		{regexp.MustCompile(`(?:[（(]?\s*小写\s*[）)])\s*[:：]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`), "xiaoxie_label", 0.7},
	}
	for _, cfg := range amountRegexes {
		if match := cfg.re.FindStringSubmatch(parsedText); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil {
				setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, amount, cfg.src, cfg.conf)
				break
			}
		}
	}
	// Very last resort: "金额: 123.45" (guard against tax-id like "金额92310109MA...").
	if data.Amount == nil {
		amountLabelRe := regexp.MustCompile(`金额\s*[:：]?\s*[\n\r]?\s*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`)
		matches := amountLabelRe.FindAllStringSubmatchIndex(parsedText, -1)
		for _, m := range matches {
			if len(m) < 4 {
				continue
			}
			numStart, numEnd := m[2], m[3]
			if numStart < 0 || numEnd <= numStart || numEnd > len(parsedText) {
				continue
			}
			// Skip if immediately followed by ASCII letters (likely part of a tax-id).
			if numEnd < len(parsedText) {
				r, _ := utf8.DecodeRuneInString(parsedText[numEnd:])
				if r != utf8.RuneError && unicode.IsLetter(r) && r < 128 {
					continue
				}
			}
			// Skip if the surrounding context indicates taxpayer ID.
			ctxStart := m[0] - 40
			if ctxStart < 0 {
				ctxStart = 0
			}
			ctx := parsedText[ctxStart:m[1]]
			if strings.Contains(ctx, "纳税人识别号") || strings.Contains(ctx, "统一社会信用代码") {
				continue
			}
			if amount := parseAmount(parsedText[numStart:numEnd]); amount != nil && *amount >= MinValidAmount {
				// Heuristic: reject absurdly large amounts for invoices.
				if *amount > 1e7 {
					continue
				}
				setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, amount, "amount_label_guarded", 0.5)
				break
			}
		}
	}
	if data.Amount == nil {
		chineseAmountRegex := regexp.MustCompile(`[零壹贰叁肆伍陆柒捌玖拾佰仟万亿]+圆整[\s\n\r]*[¥￥]?\s*[\n\r]?\s*([\d,.]+)`)
		if match := chineseAmountRegex.FindStringSubmatch(parsedText); len(match) > 1 {
			if amount := parseAmount(match[1]); amount != nil {
				setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, amount, "chinese_amount", 0.7)
			}
		}
	}
	if data.Amount == nil {
		standaloneAmountRegex := regexp.MustCompile(`[¥￥]\s*([\d]+(?:\.[\d]{1,2})?)(?:\s*$|\s*\n|$)`)
		matches := standaloneAmountRegex.FindAllStringSubmatch(parsedText, -1)
		if len(matches) > 0 {
			last := matches[len(matches)-1]
			if len(last) > 1 {
				if amount := parseAmount(last[1]); amount != nil {
					setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, amount, "standalone_amount", 0.6)
				}
			}
		}
	}
	if data.Amount == nil {
		currencyAmountRegex := regexp.MustCompile(`[¥￥]\s*([\d]+(?:\.[\d]{1,2})?)`)
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
			setAmountWithSourceAndConfidence(&data.Amount, &data.AmountSource, &data.AmountConfidence, maxAmt, "max_currency", 0.5)
		}
	}

	taxRegexes := []struct {
		re   *regexp.Regexp
		conf float64
	}{
		{regexp.MustCompile(`税额[:：]?\s*[¥￥]?([\d,.]+)`), 0.8},
		{regexp.MustCompile(`税金[:：]?\s*[¥￥]?([\d,.]+)`), 0.8},
	}
	for _, cfg := range taxRegexes {
		if match := cfg.re.FindStringSubmatch(parsedText); len(match) > 1 {
			if tax := parseAmount(match[1]); tax != nil {
				setAmountWithSourceAndConfidence(&data.TaxAmount, &data.TaxAmountSource, &data.TaxAmountConfidence, tax, "tax_label", cfg.conf)
				break
			}
		}
	}

	if buyer, seller := s.extractBuyerAndSellerByPosition(parsedText); buyer != nil || seller != nil {
		if buyer != nil && isLikelyBuyerName(*buyer) {
			setStringWithSourceAndConfidence(&data.BuyerName, &data.BuyerNameSource, &data.BuyerNameConfidence, cleanupName(*buyer), "position", 0.7)
		}
		if seller != nil && isLikelySellerName(*seller) {
			setStringWithSourceAndConfidence(&data.SellerName, &data.SellerNameSource, &data.SellerNameConfidence, cleanupName(*seller), "position", 0.7)
		}
	}

	if data.BuyerName == nil {
		buyerRegexes := []*regexp.Regexp{
			regexp.MustCompile(`购买方[：:]?\s*名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
			regexp.MustCompile(`购买方名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
			regexp.MustCompile(`购货方[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
		}
		for _, re := range buyerRegexes {
			if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
				val := cleanPartyNameFromInlineValue(match[1])
				if val != "" && val != "信息" && val != "名称" && !isBadPartyNameCandidate(val) {
					setStringWithSourceAndConfidence(&data.BuyerName, &data.BuyerNameSource, &data.BuyerNameConfidence, val, "buyer_label", 0.8)
					break
				}
			}
		}
		if data.BuyerName == nil {
			buyerSectionRegex := regexp.MustCompile(`(?s)购买方信息.*?纳税人识别号[：:]?\s*[\n\r]?\s*([A-Z0-9]*)[\s\n\r]+名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`)
			if match := buyerSectionRegex.FindStringSubmatch(parsedText); len(match) > 2 {
				val := cleanupName(strings.TrimSpace(match[2]))
				if val != "" && val != "名称" && !isBadPartyNameCandidate(val) {
					setStringWithSourceAndConfidence(&data.BuyerName, &data.BuyerNameSource, &data.BuyerNameConfidence, val, "buyer_section", 0.8)
				}
			}
		}
		if data.BuyerName == nil {
			if match := regexp.MustCompile(`(个人)`).FindStringSubmatch(parsedText); len(match) > 1 {
				setStringWithSourceAndConfidence(&data.BuyerName, &data.BuyerNameSource, &data.BuyerNameConfidence, match[1], "buyer_individual", 0.6)
			}
		}
	}

	if data.SellerName == nil {
		sellerRegexes := []*regexp.Regexp{
			regexp.MustCompile(`销售方[：:]?\s*名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
			regexp.MustCompile(`销售方名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
			regexp.MustCompile(`出票方[：:]?\s*[\n\r]?\s*([^\n\r]+)`),
		}
		for _, re := range sellerRegexes {
			if match := re.FindStringSubmatch(parsedText); len(match) > 1 {
				val := cleanPartyNameFromInlineValue(match[1])
				if val != "" && val != "信息" && val != "名称" && val != "个人" && !isBadPartyNameCandidate(val) {
					setStringWithSourceAndConfidence(&data.SellerName, &data.SellerNameSource, &data.SellerNameConfidence, val, "seller_label", 0.8)
					break
				}
			}
		}
		if data.SellerName == nil {
			sellerSectionRegex := regexp.MustCompile(fmt.Sprintf(`(?s)销.*?纳税人识别号[：:]?\s*[\n\r]?\s*(%s)[\s\n\r]+名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`, taxIDPattern))
			if match := sellerSectionRegex.FindStringSubmatch(parsedText); len(match) > 2 {
				val := cleanupName(strings.TrimSpace(match[2]))
				if val != "" && val != "名称" && val != "个人" && !isBadPartyNameCandidate(val) {
					setStringWithSourceAndConfidence(&data.SellerName, &data.SellerNameSource, &data.SellerNameConfidence, val, "seller_section", 0.8)
				}
			}
		}
		if data.SellerName == nil {
			taxThenName := regexp.MustCompile(fmt.Sprintf(`\b(%s)\b[\s\n\r]+名称[：:]?\s*[\n\r]?\s*([^\n\r]+)`, taxIDPattern))
			if match := taxThenName.FindStringSubmatch(parsedText); len(match) > 2 {
				val := cleanupName(strings.TrimSpace(match[2]))
				if val != "" && val != "个人" && len(val) > 2 && !isBadPartyNameCandidate(val) {
					setStringWithSourceAndConfidence(&data.SellerName, &data.SellerNameSource, &data.SellerNameConfidence, val, "seller_taxid_name", 0.7)
				}
			}
		}
		if data.SellerName == nil {
			companyBeforeTaxID := regexp.MustCompile(fmt.Sprintf(`([^\n\r]*(?:公司|商店|企业|中心|厂|店|行|社|院|局)[^\n\r]*)[\s\n\r]+(%s)`, taxIDPattern))
			if match := companyBeforeTaxID.FindStringSubmatch(parsedText); len(match) > 2 {
				val := cleanPartyNameFromInlineValue(match[1])
				if val != "" && val != "个人" && len(val) > 3 && !isBadPartyNameCandidate(val) {
					setStringWithSourceAndConfidence(&data.SellerName, &data.SellerNameSource, &data.SellerNameConfidence, val, "seller_company_before_taxid", 0.6)
				}
			}
		}
	}

	// Recover buyer/seller names from merged-label PDF text (e.g. "名称：个人 销售方信息 名称：").
	// This happens with PyMuPDF text extraction where buyer/seller blocks are on the same line.
	if !airTicketDetected {
		// Buyer: extract from buyer block. Even if a plausible name was picked by position (e.g. footer "张唯"),
		// override when the buyer block explicitly indicates "电话: 个人".
		buyerText := parsedText
		if loc := regexp.MustCompile(`(?m)^购买方\s*$`).FindStringIndex(parsedText); loc != nil {
			// In some top-bottom layouts, the buyer name line appears slightly above the "购买方" marker.
			// Keep a small lookback window so we can still capture it.
			start := loc[0] - 800
			if start < 0 {
				start = 0
			}
			buyerText = parsedText[start:]
		}
		for _, stop := range []string{"密码区", "明细", "销售方"} {
			if idx := strings.Index(buyerText, stop); idx > 0 {
				buyerText = buyerText[:idx]
			}
		}

		val := ""
		if m := regexp.MustCompile(`名称\s*[:：]\s*([^\n\r]{1,80})`).FindStringSubmatch(buyerText); len(m) > 1 {
			val = cleanPartyNameFromInlineValue(m[1])
		}
		if val == "" || len([]rune(val)) <= 1 || isGarbagePartyName(val) || isBadPartyNameCandidate(val) {
			if mm := regexp.MustCompile(`电话\s*[:：]\s*([^\s\n\r]{1,20})`).FindStringSubmatch(buyerText); len(mm) > 1 {
				val = cleanPartyNameFromInlineValue(mm[1])
			}
		}
		// Some buyer blocks are columnar: "名称:" is empty, and the actual name may appear after "开户行及账号:".
		if val == "" || len([]rune(val)) <= 1 || isGarbagePartyName(val) || isBadPartyNameCandidate(val) {
			if ms := regexp.MustCompile(`(?m)开户行及账号\s*[:：]\s*([^\n\r]{1,60})`).FindAllStringSubmatch(buyerText, -1); len(ms) > 0 {
				cand := cleanPartyNameFromInlineValue(ms[len(ms)-1][1])
				if cand != "" && len([]rune(cand)) > 1 && !isBadPartyNameCandidate(cand) {
					val = cand
				}
			}
		}
		if val == "" || isGarbagePartyName(val) || isBadPartyNameCandidate(val) {
			if regexp.MustCompile(`(?s)购买方.{0,240}?电话\s*[:：]\s*个人`).MatchString(buyerText) {
				val = "个人"
			}
		}

		if val != "" && val != "个人销售方信息名称" && !isBadPartyNameCandidate(val) {
			old := ptrToString(data.BuyerName)
			shouldOverride := false
			if val == "个人" && old != "个人" {
				shouldOverride = true
			}
			if data.BuyerName == nil || strings.TrimSpace(old) == "" || isGarbagePartyName(old) || isBadPartyNameCandidate(old) {
				shouldOverride = true
			}
			if !shouldOverride && data.BuyerNameSource == "position" && data.BuyerNameConfidence <= 0.75 {
				shouldOverride = true
			}
			if shouldOverride {
				v := val
				data.BuyerName = &v
				data.BuyerNameSource = "buyer_name_inline_block"
				data.BuyerNameConfidence = 0.9
			}
		}

		// Seller: try to recover company name near tax-id (even if there are numbers/amounts between them).
		needSellerFix := data.SellerName == nil || isGarbagePartyName(ptrToString(data.SellerName)) || isBadPartyNameCandidate(ptrToString(data.SellerName)) || ptrToString(data.SellerName) == ptrToString(data.BuyerName)
		if needSellerFix {
			if cand := extractCompanyNameNearTaxID(parsedText); cand != "" && cand != "个人" && !isBadPartyNameCandidate(cand) {
				v := cand
				data.SellerName = &v
				data.SellerNameSource = "seller_company_taxid_context"
				data.SellerNameConfidence = 0.9
			}
		}
	}

	// Strong fallback for PDF text: extract company name adjacent to a tax ID in the seller block.
	// This avoids relying on fragile label ordering ("名称:" sometimes has no value, while the tax-id line carries the name).
	{
		companyTaxIDLoose := regexp.MustCompile(fmt.Sprintf(`([\p{Han}（）()·]{2,140}(?:有限责任公司|有限公司|公司|集团|银行|商店|企业|中心|厂|店|行|社|院|局)[\p{Han}（）()·]{0,40})\s*(%s)`, taxIDPattern))
		sellerText := parsedText
		if loc := regexp.MustCompile(`(?m)^销售方\s*$`).FindStringIndex(parsedText); loc != nil {
			sellerText = parsedText[loc[0]:]
		}

		best := ""
		bestLen := 0
		for _, m := range companyTaxIDLoose.FindAllStringSubmatch(sellerText, -1) {
			cand := cleanupName(strings.TrimSpace(m[1]))
			if cand == "" || cand == "个人" || isBadPartyNameCandidate(cand) {
				continue
			}
			l := len([]rune(cand))
			if l > bestLen {
				best = cand
				bestLen = l
			}
		}

		// If seller section is empty (common with zoned PDF text), fall back to the last company+taxid match
		// in the whole invoice text.
		if best == "" {
			if matches := companyTaxIDLoose.FindAllStringSubmatch(parsedText, -1); len(matches) > 0 {
				cand := cleanupName(strings.TrimSpace(matches[len(matches)-1][1]))
				if cand != "" && cand != "个人" && !isBadPartyNameCandidate(cand) {
					best = cand
				}
			}
		}

		if best != "" {
			shouldOverride := false
			if data.SellerName == nil || strings.TrimSpace(*data.SellerName) == "" {
				shouldOverride = true
			} else {
				old := strings.TrimSpace(*data.SellerName)
				oldLen := len([]rune(old))
				newLen := len([]rune(best))
				// Override short/low-confidence positional picks or garbage merged-label picks when we find a company name tied to a tax ID.
				if (data.SellerNameSource == "position" && data.SellerNameConfidence <= 0.75 && newLen > oldLen+1) ||
					isGarbagePartyName(old) ||
					(newLen > oldLen+4) {
					shouldOverride = true
				}
			}
			if shouldOverride {
				v := best
				data.SellerName = &v
				data.SellerNameSource = "seller_company_taxid_loose"
				data.SellerNameConfidence = 0.9
			}
		}
	}

	if data.InvoiceNumber != nil && data.InvoiceNumberConfidence == 0 {
		data.InvoiceNumberConfidence = 0.6
	}
	if data.InvoiceDate != nil && data.InvoiceDateConfidence == 0 {
		data.InvoiceDateConfidence = 0.7
	}
	if data.Amount != nil && data.AmountConfidence == 0 {
		data.AmountConfidence = 0.6
	}
	if data.TaxAmount != nil && data.TaxAmountConfidence == 0 {
		data.TaxAmountConfidence = 0.6
	}
	if data.SellerName != nil && data.SellerNameConfidence == 0 {
		data.SellerNameConfidence = 0.6
	}
	if data.BuyerName != nil && data.BuyerNameConfidence == 0 {
		data.BuyerNameConfidence = 0.6
	}

	// Attach zones for debugging/UI if provided.
	if meta != nil && len(meta.Zones) > 0 {
		data.PDFZones = meta.Zones
	}

	// Items: prefer coordinate-based extraction when available; it is more robust for tight columns
	// (name/spec close together) and for templates where text normalization merges columns.
	if meta != nil && len(meta.Zones) > 0 {
		zonedItems := extractInvoiceLineItemsFromPDFZones(meta.Zones)
		score := func(items []InvoiceLineItem) int {
			s := 0
			for _, it := range items {
				if strings.TrimSpace(it.Name) != "" {
					s++
				}
				if strings.TrimSpace(it.Spec) != "" {
					s++
				}
				if strings.TrimSpace(it.Unit) != "" {
					s++
				}
				if it.Quantity != nil && *it.Quantity != 0 {
					s++
				}
			}
			return s
		}
		if len(zonedItems) > 0 {
			curScore := score(data.Items)
			zScore := score(zonedItems)
			if len(data.Items) == 0 || len(zonedItems) > len(data.Items) || zScore > curScore+1 {
				data.Items = zonedItems
			}
		}
	}

	if len(data.Items) == 0 {
		data.Items = extractInvoiceLineItems(parsedText)
	}

	// When PDF zones are available, prefer coordinate-based fixes for ambiguous party names and totals.
	if meta != nil && len(meta.Zones) > 0 {
		// Buyer: recover from the buyer block when OCR text/normalization merged columns.
		{
			curBuyer := ptrToString(data.BuyerName)
			needBuyer := data.BuyerName == nil ||
				isGarbagePartyName(curBuyer) ||
				isBadPartyNameCandidate(curBuyer) ||
				curBuyer == ptrToString(data.SellerName)

			if cand, ok := extractBuyerNameFromPDFZones(meta.Zones); ok {
				shouldOverride := needBuyer
				if !shouldOverride && curBuyer == "个人" && cand.val != "个人" {
					// "个人" is often a placeholder; override only when our current pick is weak.
					if data.BuyerNameConfidence <= 0.75 || data.BuyerNameSource == "position" {
						shouldOverride = cand.conf >= 0.9
					}
				}
				if shouldOverride && cand.val != "" && !isBadPartyNameCandidate(cand.val) {
					v := cand.val
					data.BuyerName = &v
					data.BuyerNameSource = cand.src
					data.BuyerNameConfidence = cand.conf
				}
			}
		}

		// Totals: avoid picking huge numbers from tax IDs (e.g. 92310109...) by reading the totals row.
		{
			total, totalSrc, totalConf, tax, taxSrc, taxConf := extractInvoiceTotalsFromPDFZones(meta.Zones)
			if total != nil && totalSrc != "" && *total > 0 {
				cur := data.Amount
				need := cur == nil || data.AmountConfidence < 0.75
				if !need && cur != nil {
					if *cur > 1000000 && *total < 1000000 {
						need = true
					} else if math.Abs(*cur-*total) > 0.01 && (data.AmountSource == "standalone_amount" || data.AmountSource == "max_currency") {
						need = true
					}
				}
				if need {
					v := *total
					data.Amount = &v
					data.AmountSource = totalSrc
					data.AmountConfidence = totalConf
				}
			}
			if tax != nil && taxSrc != "" && *tax >= 0 {
				if total != nil && *total > 0 && *tax > *total {
					// Ignore obviously invalid tax candidates.
				} else {
					cur := data.TaxAmount
					need := cur == nil || data.TaxAmountConfidence < 0.75
					if !need && cur != nil {
						if *cur > 1000000 && *tax < 1000000 {
							need = true
						} else if math.Abs(*cur-*tax) > 0.01 && (data.TaxAmountSource == "tax_label" || data.TaxAmountSource == "sum_net_tax_line") {
							need = true
						}
					}
					if need {
						v := *tax
						data.TaxAmount = &v
						data.TaxAmountSource = taxSrc
						data.TaxAmountConfidence = taxConf
					}
				}
			}
		}
	}

	// If seller name is still suspicious, try to recover from coordinate-zoned rows (when available).
	if meta != nil && len(meta.Zones) > 0 {
		need := data.SellerName == nil ||
			isGarbagePartyName(ptrToString(data.SellerName)) ||
			isBadPartyNameCandidate(ptrToString(data.SellerName)) ||
			ptrToString(data.SellerName) == ptrToString(data.BuyerName)
		if need {
			if cand := extractSellerNameFromPDFZones(meta.Zones); cand != "" && cand != "个人" && !isBadPartyNameCandidate(cand) {
				setStringWithSourceAndConfidence(&data.SellerName, &data.SellerNameSource, &data.SellerNameConfidence, cand, "pymupdf_zones_coords", 0.92)
			}
		}
	}

	data.PrettyText = formatInvoicePrettyText(text, data)
	return data, nil
}

// parseAmount parses amount string to float64
func parseAmount(s string) *float64 {
	// Remove currency symbols, commas and spaces
	s = strings.NewReplacer("￥", "", "¥", "", "CNY", "", "cny", "", "RMB", "", "rmb", "", "人民币", "").Replace(s)
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

func setPtrIfEmpty(target **string, val string) {
	if target == nil {
		return
	}
	if strings.TrimSpace(val) == "" {
		return
	}
	if *target == nil || strings.TrimSpace(**target) == "" {
		v := strings.TrimSpace(val)
		*target = &v
	}
}

func setStringWithSource(target **string, source *string, val, src string) {
	if strings.TrimSpace(val) == "" {
		return
	}
	if *target == nil || strings.TrimSpace(**target) == "" {
		v := strings.TrimSpace(val)
		*target = &v
		if source != nil && src != "" {
			*source = src
		}
	}
}

func setAmountWithSource(target **float64, source *string, val *float64, src string) {
	if val == nil {
		return
	}
	if *target == nil || **target == 0 {
		*target = val
		if source != nil && src != "" {
			*source = src
		}
	}
}

func setStringWithSourceAndConfidence(target **string, source *string, confidence *float64, val, src string, conf float64) {
	if strings.TrimSpace(val) == "" {
		return
	}
	if *target == nil || strings.TrimSpace(**target) == "" {
		v := strings.TrimSpace(val)
		*target = &v
		if source != nil && src != "" {
			*source = src
		}
		if confidence != nil && conf > 0 {
			*confidence = conf
		}
	}
}

func setAmountWithSourceAndConfidence(target **float64, source *string, confidence *float64, val *float64, src string, conf float64) {
	if val == nil {
		return
	}
	if *target == nil || **target == 0 {
		*target = val
		if source != nil && src != "" {
			*source = src
		}
		if confidence != nil && conf > 0 {
			*confidence = conf
		}
	}
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
