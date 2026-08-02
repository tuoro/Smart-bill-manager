package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

type sampleOut struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	RawText  string `json:"raw_text"`
	Expected any    `json:"expected"`
}

type paymentExpected struct {
	Amount          *float64 `json:"amount,omitempty"`
	Merchant        *string  `json:"merchant,omitempty"`
	TransactionTime *string  `json:"transaction_time,omitempty"`
	PaymentMethod   *string  `json:"payment_method,omitempty"`
	OrderNumber     *string  `json:"order_number,omitempty"`
}

type invoiceExpected struct {
	InvoiceNumber *string  `json:"invoice_number,omitempty"`
	InvoiceDate   *string  `json:"invoice_date,omitempty"`
	Amount        *float64 `json:"amount,omitempty"`
	TaxAmount     *float64 `json:"tax_amount,omitempty"`
	SellerName    *string  `json:"seller_name,omitempty"`
	BuyerName     *string  `json:"buyer_name,omitempty"`
}

func main() {
	kind := flag.String("kind", "", "payment|invoice")
	id := flag.String("id", "", "record id")
	name := flag.String("name", "", "sample name (default: <kind>_<id>)")
	dataDir := flag.String("data-dir", "", "DATA_DIR for bills.db (default: env DATA_DIR or ./data)")
	outPath := flag.String("out", "", "output json file path (default: internal/services/testdata/regression/...)")
	overwrite := flag.Bool("overwrite", false, "overwrite output if exists")
	flag.Parse()

	k := strings.ToLower(strings.TrimSpace(*kind))
	recID := strings.TrimSpace(*id)
	if k == "" || recID == "" {
		fmt.Fprintln(os.Stderr, "missing --kind and/or --id")
		os.Exit(2)
	}
	if k != "payment" && k != "invoice" {
		fmt.Fprintln(os.Stderr, "invalid --kind, expected payment|invoice")
		os.Exit(2)
	}

	sampleName := strings.TrimSpace(*name)
	if sampleName == "" {
		sampleName = fmt.Sprintf("%s_%s", k, recID)
	}

	dir := strings.TrimSpace(*dataDir)
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("DATA_DIR"))
	}
	if dir == "" {
		dir = "./data"
	}

	db, err := database.Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	ocrSvc := services.NewOCRService()

	var out sampleOut
	switch k {
	case "payment":
		rawText, exp, err := exportPaymentSample(db, ocrSvc, recID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		out = sampleOut{
			Kind:     "payment_screenshot",
			Name:     sampleName,
			RawText:  rawText,
			Expected: exp,
		}
	case "invoice":
		rawText, exp, err := exportInvoiceSample(db, ocrSvc, recID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		out = sampleOut{
			Kind:     "invoice",
			Name:     sampleName,
			RawText:  rawText,
			Expected: exp,
		}
	}

	dest := strings.TrimSpace(*outPath)
	if dest == "" {
		base := filepath.Join("internal", "services", "testdata", "regression")
		if out.Kind == "payment_screenshot" {
			dest = filepath.Join(base, "payments", sampleName+".json")
		} else {
			dest = filepath.Join(base, "invoices", sampleName+".json")
		}
	}

	if !*overwrite {
		if _, err := os.Stat(dest); err == nil {
			fmt.Fprintf(os.Stderr, "output exists: %s (use --overwrite)\n", dest)
			os.Exit(1)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	b = append(b, '\n')

	if err := os.WriteFile(dest, b, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	fmt.Println(dest)
}

func exportPaymentSample(db *gorm.DB, ocrSvc *services.OCRService, id string) (string, *paymentExpected, error) {
	var p models.Payment
	res := db.Where("id = ?", id).Limit(1).Find(&p)
	if res.Error != nil {
		return "", nil, fmt.Errorf("query payment: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return "", nil, fmt.Errorf("payment not found: %s", id)
	}
	if p.ExtractedData == nil || strings.TrimSpace(*p.ExtractedData) == "" {
		return "", nil, fmt.Errorf("payment has no extracted_data: %s", id)
	}

	var ed services.PaymentExtractedData
	if err := json.Unmarshal([]byte(*p.ExtractedData), &ed); err != nil {
		return "", nil, fmt.Errorf("parse payment extracted_data: %w", err)
	}
	raw := strings.TrimSpace(ed.RawText)
	if raw == "" {
		return "", nil, fmt.Errorf("payment extracted_data has empty raw_text: %s", id)
	}

	parsed, err := ocrSvc.ParsePaymentScreenshot(raw)
	if err != nil {
		return "", nil, fmt.Errorf("ParsePaymentScreenshot failed: %w", err)
	}

	exp := &paymentExpected{
		Amount:          parsed.Amount,
		Merchant:        parsed.Merchant,
		TransactionTime: parsed.TransactionTime,
		PaymentMethod:   parsed.PaymentMethod,
		OrderNumber:     parsed.OrderNumber,
	}
	return raw, exp, nil
}

func exportInvoiceSample(db *gorm.DB, ocrSvc *services.OCRService, id string) (string, *invoiceExpected, error) {
	var inv models.Invoice
	res := db.Where("id = ?", id).Limit(1).Find(&inv)
	if res.Error != nil {
		return "", nil, fmt.Errorf("query invoice: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return "", nil, fmt.Errorf("invoice not found: %s", id)
	}

	raw := ""
	if inv.RawText != nil {
		raw = strings.TrimSpace(*inv.RawText)
	}
	if raw == "" && inv.ExtractedData != nil && strings.TrimSpace(*inv.ExtractedData) != "" {
		var ed services.InvoiceExtractedData
		if err := json.Unmarshal([]byte(*inv.ExtractedData), &ed); err == nil {
			raw = strings.TrimSpace(ed.RawText)
		}
	}
	if raw == "" {
		return "", nil, fmt.Errorf("invoice has no raw_text/extracted_data raw_text: %s", id)
	}

	parsed, err := ocrSvc.ParseInvoiceData(raw)
	if err != nil {
		return "", nil, fmt.Errorf("ParseInvoiceData failed: %w", err)
	}

	exp := &invoiceExpected{
		InvoiceNumber: parsed.InvoiceNumber,
		InvoiceDate:   parsed.InvoiceDate,
		Amount:        parsed.Amount,
		TaxAmount:     parsed.TaxAmount,
		SellerName:    parsed.SellerName,
		BuyerName:     parsed.BuyerName,
	}
	return raw, exp, nil
}
