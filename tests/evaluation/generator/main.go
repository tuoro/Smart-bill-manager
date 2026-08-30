package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const datasetVersion = "m1-synthetic-v1"

type manifest struct {
	DatasetVersion string       `json:"dataset_version"`
	FrozenAt       string       `json:"frozen_at"`
	SyntheticOnly  bool         `json:"synthetic_only"`
	Generator      string       `json:"generator"`
	Samples        []sampleSpec `json:"samples"`
}

type sampleSpec struct {
	SampleID                string                  `json:"sample_id"`
	File                    string                  `json:"file"`
	SHA256                  string                  `json:"sha256"`
	OriginalName            string                  `json:"original_name"`
	DeclaredMIME            string                  `json:"declared_mime"`
	DocumentType            string                  `json:"document_type"`
	ModelStageEligible      bool                    `json:"model_stage_eligible"`
	ScenarioTags            []string                `json:"scenario_tags"`
	ExpectedFields          map[string]any          `json:"expected_fields"`
	ExpectedMissingFields   []string                `json:"expected_missing_fields"`
	ExpectedEvidence        map[string]evidenceSpec `json:"expected_evidence"`
	AllowedNormalizations   map[string][]string     `json:"allowed_normalizations"`
	ExpectedEvents          []string                `json:"expected_events"`
	ExpectedFailureCategory string                  `json:"expected_failure_category,omitempty"`
	ExpectedReviewState     string                  `json:"expected_review_state"`
	ExpectedItems           []map[string]any        `json:"expected_items,omitempty"`
}

type evidenceSpec struct {
	Page  int    `json:"page"`
	Quote string `json:"quote"`
}

func main() {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate evaluation generator")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), ".."))
	assets := filepath.Join(root, "assets", datasetVersion)
	if err := os.MkdirAll(assets, 0o700); err != nil {
		panic(err)
	}
	result := manifest{
		DatasetVersion: datasetVersion,
		FrozenAt:       "2026-08-28T00:00:00Z",
		SyntheticOnly:  true,
		Generator:      "tests/evaluation/generator/main.go",
	}
	for index := 1; index <= 40; index++ {
		result.Samples = append(result.Samples, paymentSample(index, assets))
	}
	for index := 1; index <= 40; index++ {
		result.Samples = append(result.Samples, invoiceSample(index, assets))
	}
	for index := 1; index <= 20; index++ {
		result.Samples = append(result.Samples, edgeSample(index, assets))
	}
	validateManifest(result)
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		panic(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(filepath.Join(root, "manifest-v1.json"), encoded, 0o600); err != nil {
		panic(err)
	}
	fmt.Printf("generated %d frozen synthetic samples in %s\n", len(result.Samples), assets)
}

func paymentSample(index int, assets string) sampleSpec {
	sampleID := fmt.Sprintf("PAY-%03d", index)
	name := strings.ToLower(sampleID) + ".png"
	currencies := []string{"CNY", "USD", "EUR", "JPY"}
	currency := currencies[(index-1)%len(currencies)]
	amountMinor := int64(10_000 + index*137)
	merchant := fmt.Sprintf("Synthetic Merchant %02d", index)
	day := 1 + (index-1)%28
	hour := 8 + index%10
	transactionTime := fmt.Sprintf("2026-07-%02dT%02d:15:00+08:00", day, hour)
	amountText := formatMoney(amountMinor, currency)
	fields := map[string]any{
		"amount_minor":     amountMinor,
		"currency":         currency,
		"merchant":         merchant,
		"transaction_time": transactionTime,
		"source_timezone":  "Asia/Shanghai",
	}
	evidence := map[string]evidenceSpec{
		"amount_minor":     {Page: 1, Quote: amountText},
		"currency":         {Page: 1, Quote: currency},
		"merchant":         {Page: 1, Quote: merchant},
		"transaction_time": {Page: 1, Quote: transactionTime},
		"source_timezone":  {Page: 1, Quote: "Asia/Shanghai"},
	}
	tags := []string{"payment_screenshot"}
	events := []string{}
	state := "needs_review"
	lines := []string{
		"SYNTHETIC PAYMENT RECEIPT",
		"MERCHANT: " + merchant,
		"TOTAL: " + amountText,
		"TRANSACTION_TIME: " + transactionTime,
		"SOURCE_TIMEZONE: Asia/Shanghai",
		fmt.Sprintf("ORDER: SYN-PAY-%04d", index),
	}
	if index >= 31 {
		tags = append(tags, "low_quality_conflict")
		state = "blocked"
		if index <= 35 {
			delete(fields, "amount_minor")
			delete(evidence, "amount_minor")
			lines[2] = "TOTAL: [CROPPED / MISSING]"
			events = append(events, "missing:amount_minor")
		} else {
			delete(fields, "amount_minor")
			delete(evidence, "amount_minor")
			lines[2] = "TOTAL A: " + amountText + "  TOTAL B: " + formatMoney(amountMinor+500, currency)
			events = append(events, "conflict:amount_minor")
		}
	}
	path := filepath.Join(assets, name)
	writePNG(path, lines, index >= 31)
	return sampleSpec{
		SampleID: sampleID, File: filepath.ToSlash(filepath.Join("assets", datasetVersion, name)),
		SHA256: fileHash(path), OriginalName: name, DeclaredMIME: "image/png",
		DocumentType: "payment", ModelStageEligible: true, ScenarioTags: tags,
		ExpectedFields: fields, ExpectedMissingFields: missingPaymentFields(fields), ExpectedEvidence: evidence,
		AllowedNormalizations: nameNormalizations(fields, "merchant"), ExpectedEvents: events,
		ExpectedReviewState: state,
	}
}

func invoiceSample(index int, assets string) sampleSpec {
	sampleNumber := 40 + index
	sampleID := fmt.Sprintf("INV-%03d", sampleNumber)
	name := strings.ToLower(sampleID) + ".png"
	number := fmt.Sprintf("SYN-INV-2026-%04d", index)
	day := 1 + (index-1)%28
	date := fmt.Sprintf("2026-06-%02d", day)
	currencies := []string{"CNY", "USD", "EUR", "JPY"}
	currency := currencies[(index-1)%len(currencies)]
	totalMinor := int64(20_000 + index*211)
	taxMinor := int64(0)
	if currency != "JPY" {
		taxMinor = totalMinor / 13
	}
	seller := fmt.Sprintf("Synthetic Seller %02d", index)
	buyer := fmt.Sprintf("Synthetic Buyer %02d", index)
	fields := map[string]any{
		"invoice_number": number,
		"invoice_date":   date,
		"total_minor":    totalMinor,
		"currency":       currency,
		"seller_name":    seller,
		"buyer_name":     buyer,
	}
	if taxMinor > 0 {
		fields["tax_minor"] = taxMinor
	}
	totalText := formatMoney(totalMinor, currency)
	evidence := map[string]evidenceSpec{
		"invoice_number": {Page: 1, Quote: number},
		"invoice_date":   {Page: 1, Quote: date},
		"total_minor":    {Page: 1, Quote: totalText},
		"currency":       {Page: 1, Quote: currency},
		"seller_name":    {Page: 1, Quote: seller},
		"buyer_name":     {Page: 1, Quote: buyer},
	}
	if taxMinor > 0 {
		evidence["tax_minor"] = evidenceSpec{Page: 1, Quote: formatMoney(taxMinor, currency)}
	}
	tags := []string{"single_item_invoice"}
	itemCount := 1
	if index > 20 {
		tags = []string{"multi_item_invoice"}
		itemCount = 3
	}
	items := make([]map[string]any, 0, itemCount)
	remaining := totalMinor
	lines := []string{
		"SYNTHETIC TAX INVOICE",
		"INVOICE_NUMBER: " + number,
		"INVOICE_DATE: " + date,
		"SELLER: " + seller,
		"BUYER: " + buyer,
		"TOTAL: " + totalText,
	}
	if taxMinor > 0 {
		lines = append(lines, "TAX: "+formatMoney(taxMinor, currency))
	}
	for item := 0; item < itemCount; item++ {
		amount := remaining / int64(itemCount-item)
		remaining -= amount
		entry := map[string]any{
			"name":         fmt.Sprintf("Synthetic Item %d", item+1),
			"amount_minor": amount,
			"sort_order":   item,
		}
		items = append(items, entry)
		lines = append(lines, fmt.Sprintf("ITEM %d: Synthetic Item %d | %s", item+1, item+1, formatMoney(amount, currency)))
	}
	events := []string{}
	state := "needs_review"
	if index >= 31 && index <= 35 {
		tags = append(tags, "low_quality_conflict")
		state = "blocked"
		delete(fields, "total_minor")
		delete(evidence, "total_minor")
		lines[5] = "TOTAL A: " + totalText + "  TOTAL B: " + formatMoney(totalMinor+700, currency)
		events = append(events, "conflict:total_minor")
	}
	path := filepath.Join(assets, name)
	writePNG(path, lines, index >= 31 && index <= 35)
	return sampleSpec{
		SampleID: sampleID, File: filepath.ToSlash(filepath.Join("assets", datasetVersion, name)),
		SHA256: fileHash(path), OriginalName: name, DeclaredMIME: "image/png",
		DocumentType: "invoice", ModelStageEligible: true, ScenarioTags: tags,
		ExpectedFields: fields, ExpectedMissingFields: missingInvoiceFields(fields), ExpectedEvidence: evidence,
		AllowedNormalizations: nameNormalizations(fields, "seller_name", "buyer_name"),
		ExpectedEvents:        events, ExpectedReviewState: state, ExpectedItems: items,
	}
}

func edgeSample(index int, assets string) sampleSpec {
	sampleNumber := 80 + index
	sampleID := fmt.Sprintf("EDGE-%03d", sampleNumber)
	base := sampleSpec{
		SampleID: sampleID, DocumentType: "unknown", ScenarioTags: []string{"invalid_unsupported"},
		ExpectedFields: map[string]any{}, ExpectedMissingFields: []string{},
		ExpectedEvidence: map[string]evidenceSpec{}, AllowedNormalizations: map[string][]string{},
		ExpectedEvents: []string{}, ExpectedReviewState: "rejected_before_model",
	}
	var content []byte
	switch {
	case index <= 5:
		base.OriginalName = strings.ToLower(sampleID) + ".txt"
		base.DeclaredMIME = "text/plain"
		base.ExpectedFailureCategory = "unsupported_document"
		content = []byte("SYNTHETIC UNSUPPORTED DOCUMENT " + sampleID + "\n")
	case index <= 10:
		base.OriginalName = strings.ToLower(sampleID) + ".jpg"
		base.DeclaredMIME = "image/jpeg"
		base.ExpectedFailureCategory = "document_signature_mismatch"
		temporary := filepath.Join(assets, "."+strings.ToLower(sampleID)+".png")
		writePNG(temporary, []string{"SYNTHETIC MIME MISMATCH", sampleID}, false)
		var err error
		content, err = os.ReadFile(temporary)
		if err != nil {
			panic(err)
		}
		if err := os.Remove(temporary); err != nil {
			panic(err)
		}
	default:
		if index <= 15 {
			base.OriginalName = strings.ToLower(sampleID) + ".pdf"
			base.DeclaredMIME = "application/pdf"
			base.ExpectedFailureCategory = "corrupt_pdf"
			content = []byte("%PDF-1.7\nSYNTHETIC TRUNCATED PDF " + sampleID)
		} else {
			base.OriginalName = strings.ToLower(sampleID) + ".png"
			base.DeclaredMIME = "image/png"
			base.ModelStageEligible = true
			base.ScenarioTags = []string{"low_quality_conflict"}
			base.ExpectedEvents = []string{"unknown_document_type"}
			base.ExpectedReviewState = "blocked"
			path := filepath.Join(assets, base.OriginalName)
			writePNG(path, []string{"SYNTHETIC UNCLASSIFIED DOCUMENT", "REFERENCE: " + sampleID, "NO PAYMENT OR INVOICE FIELDS"}, true)
			base.File = filepath.ToSlash(filepath.Join("assets", datasetVersion, base.OriginalName))
			base.SHA256 = fileHash(path)
			return base
		}
	}
	path := filepath.Join(assets, base.OriginalName)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		panic(err)
	}
	base.File = filepath.ToSlash(filepath.Join("assets", datasetVersion, base.OriginalName))
	base.SHA256 = fileHash(path)
	return base
}

func writePNG(path string, lines []string, lowQuality bool) {
	canvas := image.NewRGBA(image.Rect(0, 0, 600, 400))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{R: 250, G: 251, B: 253, A: 255}}, image.Point{}, draw.Src)
	ink := color.RGBA{R: 20, G: 34, B: 54, A: 255}
	if lowQuality {
		ink = color.RGBA{R: 112, G: 118, B: 128, A: 255}
	}
	drawer := &font.Drawer{Dst: canvas, Src: image.NewUniform(ink), Face: basicfont.Face7x13}
	for index, line := range lines {
		drawer.Dot = fixed.P(32, 45+index*38)
		drawer.DrawString(line)
	}
	if lowQuality {
		for x := 0; x < 600; x += 37 {
			draw.Draw(canvas, image.Rect(x, 0, x+1, 400), &image.Uniform{C: color.RGBA{R: 235, G: 237, B: 240, A: 255}}, image.Point{}, draw.Src)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		panic(err)
	}
	if err := png.Encode(file, canvas); err != nil {
		_ = file.Close()
		panic(err)
	}
	if err := file.Close(); err != nil {
		panic(err)
	}
}

func formatMoney(minor int64, currency string) string {
	if currency == "JPY" {
		return fmt.Sprintf("JPY %d", minor)
	}
	return fmt.Sprintf("%s %d.%02d", currency, minor/100, minor%100)
}

func missingPaymentFields(fields map[string]any) []string {
	return missingFields(fields, []string{"amount_minor", "currency", "merchant", "transaction_time", "source_timezone", "payment_method", "order_number", "category"})
}

func missingInvoiceFields(fields map[string]any) []string {
	return missingFields(fields, []string{"invoice_number", "invoice_date", "total_minor", "tax_minor", "currency", "seller_name", "buyer_name"})
}

func missingFields(fields map[string]any, paths []string) []string {
	result := make([]string, 0)
	for _, path := range paths {
		if _, exists := fields[path]; !exists {
			result = append(result, path)
		}
	}
	return result
}

func nameNormalizations(fields map[string]any, paths ...string) map[string][]string {
	result := make(map[string][]string)
	for _, path := range paths {
		if value, exists := fields[path].(string); exists {
			result[path] = []string{value, strings.ToUpper(value), "  " + value + "  "}
		}
	}
	return result
}

func fileHash(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func validateManifest(value manifest) {
	if len(value.Samples) != 100 {
		panic(fmt.Sprintf("sample count = %d, want 100", len(value.Samples)))
	}
	ids := make(map[string]struct{}, len(value.Samples))
	types := make(map[string]int)
	tags := make(map[string]int)
	for _, sample := range value.Samples {
		if _, exists := ids[sample.SampleID]; exists {
			panic("duplicate sample id: " + sample.SampleID)
		}
		ids[sample.SampleID] = struct{}{}
		types[sample.DocumentType]++
		for _, tag := range sample.ScenarioTags {
			tags[tag]++
		}
	}
	if types["payment"] < 40 || types["invoice"] < 40 {
		panic(fmt.Sprintf("type distribution = %#v", types))
	}
	for _, tag := range []string{"payment_screenshot", "single_item_invoice", "multi_item_invoice", "low_quality_conflict", "invalid_unsupported"} {
		if tags[tag] < 15 {
			panic(fmt.Sprintf("scenario %s has %d samples", tag, tags[tag]))
		}
	}
	keys := make([]string, 0, len(tags))
	for tag := range tags {
		keys = append(keys, tag)
	}
	sort.Strings(keys)
}
