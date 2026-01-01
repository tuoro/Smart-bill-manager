package services

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
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

func (s *InvoiceService) CreateDraftFromUpload(input CreateInvoiceInput) (*models.Invoice, error) {
	id := utils.GenerateUUID()

	if input.PaymentID != nil {
		pid := strings.TrimSpace(*input.PaymentID)
		if pid == "" {
			input.PaymentID = nil
		} else {
			input.PaymentID = &pid
		}
	}

	source := input.Source
	if source == "" {
		source = "upload"
	}

	inv := &models.Invoice{
		ID:           id,
		IsDraft:      true,
		PaymentID:    input.PaymentID,
		Filename:     input.Filename,
		OriginalName: input.OriginalName,
		FilePath:     input.FilePath,
		FileSize:     &input.FileSize,
		FileSHA256:   input.FileSHA256,
		ParseStatus:  "pending",
		ParseError:   nil,
		Source:       source,
		DedupStatus:  DedupStatusOK,
	}

	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(inv).Error; err != nil {
			return err
		}
		if input.PaymentID != nil {
			pid := strings.TrimSpace(*input.PaymentID)
			if pid != "" {
				if err := tx.Table("invoice_payment_links").Create(&models.InvoicePaymentLink{
					InvoiceID: inv.ID,
					PaymentID: pid,
				}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return inv, nil
}

type invoiceOCRTaskResult struct {
	Invoice *models.Invoice `json:"invoice"`
	Dedup   any            `json:"dedup,omitempty"`
}

func (s *InvoiceService) ProcessInvoiceOCRTask(invoiceID string) (any, error) {
	invoiceID = strings.TrimSpace(invoiceID)
	if invoiceID == "" {
		return nil, fmt.Errorf("missing invoice id")
	}

	inv, err := s.repo.FindByID(invoiceID)
	if err != nil {
		return nil, err
	}

	// Build absolute file path
	filePath := inv.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(s.uploadsDir, "..", filePath)
	}

	// Parse file (never returns Go error; failures are represented via parseStatus/parseError)
	invoiceNumber, invoiceDate, sellerName, buyerName,
		amount, taxAmount,
		extractedData, rawText,
		parseStatus, parseError := s.parseInvoiceFile(filePath, inv.Filename)

	updateData := map[string]any{
		"parse_status": parseStatus,
		"parse_error":  parseError,
		"raw_text":     rawText,
		"extracted_data": extractedData,
	}
	if invoiceNumber != nil {
		updateData["invoice_number"] = *invoiceNumber
	} else {
		updateData["invoice_number"] = nil
	}
	if invoiceDate != nil {
		updateData["invoice_date"] = *invoiceDate
	} else {
		updateData["invoice_date"] = nil
	}
	if amount != nil {
		updateData["amount"] = *amount
	} else {
		updateData["amount"] = nil
	}
	if taxAmount != nil {
		updateData["tax_amount"] = *taxAmount
	} else {
		updateData["tax_amount"] = nil
	}
	if sellerName != nil {
		updateData["seller_name"] = *sellerName
	} else {
		updateData["seller_name"] = nil
	}
	if buyerName != nil {
		updateData["buyer_name"] = *buyerName
	} else {
		updateData["buyer_name"] = nil
	}

	if err := s.repo.Update(inv.ID, updateData); err != nil {
		return nil, err
	}

	updated, _ := s.repo.FindByID(inv.ID)
	dedup := any(nil)

	// Mark suspected duplicates based on invoice_number.
	if updated != nil && updated.InvoiceNumber != nil {
		no := strings.TrimSpace(*updated.InvoiceNumber)
		if no != "" {
			if cands, derr := FindInvoiceCandidatesByInvoiceNumber(no, updated.ID, 5); derr == nil && len(cands) > 0 {
				updated.DedupStatus = DedupStatusSuspected
				ref := cands[0].ID
				updated.DedupRefID = &ref
				_ = database.GetDB().Model(&models.Invoice{}).Where("id = ?", updated.ID).Updates(map[string]any{
					"dedup_status": DedupStatusSuspected,
					"dedup_ref_id": ref,
				}).Error
				dedup = map[string]any{
					"kind":       "suspected_duplicate",
					"reason":     "invoice_number",
					"candidates": cands,
				}
			} else {
				_ = database.GetDB().Model(&models.Invoice{}).Where("id = ?", updated.ID).Updates(map[string]any{
					"dedup_status": DedupStatusOK,
					"dedup_ref_id": nil,
				}).Error
				updated.DedupStatus = DedupStatusOK
				updated.DedupRefID = nil
			}
		}
	}

	return &invoiceOCRTaskResult{Invoice: updated, Dedup: dedup}, nil
}

type CreateInvoiceInput struct {
	PaymentID    *string `json:"payment_id"`
	Filename     string  `json:"filename"`
	OriginalName string  `json:"original_name"`
	FilePath     string  `json:"file_path"`
	FileSize     int64   `json:"file_size"`
	FileSHA256   *string `json:"file_sha256"`
	Source       string  `json:"source"`
	IsDraft      bool    `json:"is_draft"`
}

func (s *InvoiceService) Create(input CreateInvoiceInput) (*models.Invoice, error) {
	id := utils.GenerateUUID()

	if input.PaymentID != nil {
		pid := strings.TrimSpace(*input.PaymentID)
		if pid == "" {
			input.PaymentID = nil
		} else {
			input.PaymentID = &pid
		}
	}

	// Build absolute file path
	filePath := input.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(s.uploadsDir, "..", filePath)
	}

	// Parse the invoice PDF
	invoiceNumber, invoiceDate, sellerName, buyerName,
		amount, taxAmount,
		extractedData, rawText,
		parseStatus, parseError := s.parseInvoiceFile(filePath, input.Filename)

	source := input.Source
	if source == "" {
		source = "upload"
	}

	invoice := &models.Invoice{
		ID:            id,
		IsDraft:       input.IsDraft,
		PaymentID:     input.PaymentID,
		Filename:      input.Filename,
		OriginalName:  input.OriginalName,
		FilePath:      input.FilePath,
		FileSize:      &input.FileSize,
		FileSHA256:    input.FileSHA256,
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
		DedupStatus:   DedupStatusOK,
	}

	// Create invoice (and optional 1:1 payment link) atomically.
	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(invoice).Error; err != nil {
			return err
		}
		if input.PaymentID != nil {
			pid := strings.TrimSpace(*input.PaymentID)
			if pid != "" {
				if err := tx.Table("invoice_payment_links").Create(&models.InvoicePaymentLink{
					InvoiceID: invoice.ID,
					PaymentID: pid,
				}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Mark suspected duplicates for UI/confirm step (invoice_number).
	if invoice.InvoiceNumber != nil {
		n := strings.TrimSpace(*invoice.InvoiceNumber)
		if n != "" {
			if cands, err := FindInvoiceCandidatesByInvoiceNumber(n, invoice.ID, 5); err == nil && len(cands) > 0 {
				invoice.DedupStatus = DedupStatusSuspected
				ref := cands[0].ID
				invoice.DedupRefID = &ref
				_ = db.Model(&models.Invoice{}).Where("id = ?", invoice.ID).Updates(map[string]interface{}{
					"dedup_status": DedupStatusSuspected,
					"dedup_ref_id": ref,
				}).Error
			}
		}
	}

	return invoice, nil
}

type InvoiceFilterInput struct {
	Limit        int  `form:"limit"`
	Offset       int  `form:"offset"`
	IncludeDraft bool `form:"includeDraft"`
}

func (s *InvoiceService) GetAll(filter InvoiceFilterInput) ([]models.Invoice, error) {
	return s.repo.FindAll(repository.InvoiceFilter{
		Limit:        filter.Limit,
		Offset:       filter.Offset,
		IncludeDraft: filter.IncludeDraft,
	})
}

func (s *InvoiceService) GetByID(id string) (*models.Invoice, error) {
	return s.repo.FindByID(id)
}

func (s *InvoiceService) GetByPaymentID(paymentID string) ([]models.Invoice, error) {
	return s.repo.FindByPaymentID(paymentID)
}

type UpdateInvoiceInput struct {
	PaymentID          *string  `json:"payment_id"`
	InvoiceNumber      *string  `json:"invoice_number"`
	InvoiceDate        *string  `json:"invoice_date"`
	Amount             *float64 `json:"amount"`
	TaxAmount          *float64 `json:"tax_amount"`
	BadDebt            *bool    `json:"bad_debt"`
	SellerName         *string  `json:"seller_name"`
	BuyerName          *string  `json:"buyer_name"`
	Confirm            *bool    `json:"confirm"`
	ForceDuplicateSave *bool    `json:"force_duplicate_save"`
}

func (s *InvoiceService) Update(id string, input UpdateInvoiceInput) error {
	confirming := input.Confirm != nil && *input.Confirm
	needsRecalc := input.BadDebt != nil || input.PaymentID != nil
	var affectedTrips []string
	if needsRecalc || confirming {
		linked, err := s.repo.GetLinkedPayments(id)
		if err != nil {
			return err
		}
		for _, p := range linked {
			if p.TripID != nil && strings.TrimSpace(*p.TripID) != "" {
				affectedTrips = append(affectedTrips, strings.TrimSpace(*p.TripID))
			}
		}

		// Legacy: invoices.payment_id -> payments.trip_id
		before, err := s.repo.FindByID(id)
		if err != nil {
			return err
		}
		if before.PaymentID != nil && strings.TrimSpace(*before.PaymentID) != "" {
			if tripID, err := getTripIDForPayment(strings.TrimSpace(*before.PaymentID)); err == nil && tripID != "" {
				affectedTrips = append(affectedTrips, tripID)
			}
		}
	}

	data := make(map[string]interface{})

	// On confirm, enforce dedup rules:
	// - hash duplicate: hard block (no override)
	// - invoice_number duplicate: allow only with force_duplicate_save
	var before *models.Invoice
	if confirming {
		inv, err := s.repo.FindByID(id)
		if err != nil {
			return err
		}
		before = inv

		force := input.ForceDuplicateSave != nil && *input.ForceDuplicateSave

		hash := ""
		if inv.FileSHA256 != nil {
			hash = strings.TrimSpace(*inv.FileSHA256)
		}
		if hash != "" {
			if existing, err := FindInvoiceByFileSHA256(hash, id); err != nil {
				return err
			} else if existing != nil {
				return &DuplicateError{
					Kind:            "hash_duplicate",
					Reason:          "file_sha256",
					Entity:          "invoice",
					ExistingID:      existing.ID,
					ExistingIsDraft: existing.IsDraft,
				}
			}
		}

		nextNo := ""
		if input.InvoiceNumber != nil {
			nextNo = strings.TrimSpace(*input.InvoiceNumber)
		} else if inv.InvoiceNumber != nil {
			nextNo = strings.TrimSpace(*inv.InvoiceNumber)
		}
		if nextNo != "" {
			cands, err := FindInvoiceCandidatesByInvoiceNumber(nextNo, id, 5)
			if err != nil {
				return err
			}
			if len(cands) > 0 && !force {
				return &DuplicateError{
					Kind:       "suspected_duplicate",
					Reason:     "invoice_number",
					Entity:     "invoice",
					Candidates: cands,
				}
			}
		}
	}

	if input.PaymentID != nil {
		trimmed := strings.TrimSpace(*input.PaymentID)
		if trimmed == "" {
			data["payment_id"] = nil
		} else {
			data["payment_id"] = trimmed
			if needsRecalc {
				if tripID, err := getTripIDForPayment(trimmed); err == nil && tripID != "" {
					affectedTrips = append(affectedTrips, tripID)
				}
			}
		}
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
	if input.BadDebt != nil {
		data["bad_debt"] = *input.BadDebt
	}
	if input.SellerName != nil {
		data["seller_name"] = *input.SellerName
	}
	if input.BuyerName != nil {
		data["buyer_name"] = *input.BuyerName
	}
	if input.Confirm != nil && *input.Confirm {
		data["is_draft"] = false
	}

	// Persist dedup status changes on confirm.
	if confirming && before != nil {
		force := input.ForceDuplicateSave != nil && *input.ForceDuplicateSave

		nextNo := ""
		if input.InvoiceNumber != nil {
			nextNo = strings.TrimSpace(*input.InvoiceNumber)
		} else if before.InvoiceNumber != nil {
			nextNo = strings.TrimSpace(*before.InvoiceNumber)
		}
		if nextNo != "" {
			if cands, err := FindInvoiceCandidatesByInvoiceNumber(nextNo, id, 5); err == nil && len(cands) > 0 {
				if force {
					data["dedup_status"] = DedupStatusForced
					data["dedup_ref_id"] = cands[0].ID
				} else {
					data["dedup_status"] = DedupStatusSuspected
					data["dedup_ref_id"] = cands[0].ID
				}
			} else {
				data["dedup_status"] = DedupStatusOK
				data["dedup_ref_id"] = nil
			}
		} else {
			data["dedup_status"] = DedupStatusOK
			data["dedup_ref_id"] = nil
		}
	}

	if len(data) == 0 {
		return nil
	}

	if err := s.repo.Update(id, data); err != nil {
		return err
	}

	if !needsRecalc {
		return nil
	}
	return recalcTripBadDebtLockedForTripIDs(affectedTrips)
}

func (s *InvoiceService) Delete(id string) error {
	// Get invoice first to delete file
	invoice, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	affectedTrips := make([]string, 0, 4)
	linked, err := s.repo.GetLinkedPayments(id)
	if err != nil {
		return err
	}
	for _, p := range linked {
		if p.TripID != nil && strings.TrimSpace(*p.TripID) != "" {
			affectedTrips = append(affectedTrips, strings.TrimSpace(*p.TripID))
		}
	}
	if invoice.PaymentID != nil && strings.TrimSpace(*invoice.PaymentID) != "" {
		if tripID, err := getTripIDForPayment(strings.TrimSpace(*invoice.PaymentID)); err == nil && tripID != "" {
			affectedTrips = append(affectedTrips, tripID)
		}
	}

	// Delete file
	filePath := invoice.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(s.uploadsDir, "..", filePath)
	}
	_ = os.Remove(filePath) // Ignore error if file doesn't exist

	// Delete invoice + links atomically.
	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		_ = tx.Where("invoice_id = ?", id).Delete(&models.InvoicePaymentLink{}).Error
		if err := tx.Where("id = ?", id).Delete(&models.Invoice{}).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	return recalcTripBadDebtLockedForTripIDs(affectedTrips)
}

func (s *InvoiceService) GetStats() (*models.InvoiceStats, error) {
	return s.repo.GetStats()
}

// LinkPayment links an invoice to a payment
func (s *InvoiceService) LinkPayment(invoiceID, paymentID string) error {
	paymentID = strings.TrimSpace(paymentID)
	if err := s.repo.LinkPayment(invoiceID, paymentID); err != nil {
		return err
	}
	// Keep legacy pointer in sync (1:1).
	_ = s.repo.Update(invoiceID, map[string]interface{}{"payment_id": paymentID})

	inv, err := s.repo.FindByID(invoiceID)
	if err != nil {
		return nil
	}
	if !inv.BadDebt {
		return nil
	}
	if tripID, err := getTripIDForPayment(strings.TrimSpace(paymentID)); err == nil && tripID != "" {
		return recalcTripBadDebtLocked(tripID)
	}
	return nil
}

// UnlinkPayment removes the link between an invoice and a payment
func (s *InvoiceService) UnlinkPayment(invoiceID, paymentID string) error {
	paymentID = strings.TrimSpace(paymentID)
	if err := s.repo.UnlinkPayment(invoiceID, paymentID); err != nil {
		return err
	}
	// Keep legacy pointer in sync (1:1).
	_ = s.repo.Update(invoiceID, map[string]interface{}{"payment_id": nil})

	inv, err := s.repo.FindByID(invoiceID)
	if err != nil {
		return nil
	}
	if !inv.BadDebt {
		return nil
	}
	if tripID, err := getTripIDForPayment(strings.TrimSpace(paymentID)); err == nil && tripID != "" {
		return recalcTripBadDebtLocked(tripID)
	}
	return nil
}

// GetLinkedPayments returns all payments linked to an invoice
func (s *InvoiceService) GetLinkedPayments(invoiceID string) ([]models.Payment, error) {
	return s.repo.GetLinkedPayments(invoiceID)
}

// SuggestPayments suggests payments that might match this invoice based on amount and date
func (s *InvoiceService) SuggestPayments(invoiceID string, limit int, debug bool) ([]models.Payment, error) {
	invoice, err := s.repo.FindByID(invoiceID)
	if err != nil {
		return nil, err
	}

	if debug {
		log.Printf(
			"[MATCH] invoice=%s amount=%v invoice_date=%v seller=%v",
			invoiceID,
			valueOrNil(invoice.Amount),
			strValueOrNil(invoice.InvoiceDate),
			strValueOrNil(invoice.SellerName),
		)
	}

	linked, _ := s.repo.GetLinkedPayments(invoiceID)
	linkedIDs := make(map[string]struct{}, len(linked))
	for _, p := range linked {
		linkedIDs[p.ID] = struct{}{}
	}

	if limit <= 0 {
		limit = 10
	}
	maxCandidates := limit * 50
	if maxCandidates < 200 {
		maxCandidates = 200
	}

	candidates, err := s.repo.SuggestPayments(invoice, maxCandidates)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		// Safety net: if repository-side filters are too strict (or data is missing),
		// fall back to the most recent payments so scoring still has something to rank.
		var total int64
		_ = database.GetDB().Model(&models.Payment{}).Where("is_draft = 0").Count(&total).Error
		if debug {
			log.Printf("[MATCH] invoice=%s repo candidates=0, fallback to recent payments (total=%d)", invoiceID, total)
		}
		if total > 0 {
			var recent []models.Payment
			_ = database.GetDB().
				Model(&models.Payment{}).
				Where("is_draft = 0").
				Order("transaction_time DESC").
				Limit(maxCandidates).
				Find(&recent).Error
			candidates = recent

			if debug {
				sampleN := 5
				if len(recent) < sampleN {
					sampleN = len(recent)
				}
				for i := 0; i < sampleN; i++ {
					p := recent[i]
					log.Printf("[MATCH] invoice=%s recent payment sample=%d id=%s amount=%.2f merchant=%q time=%q",
						invoiceID, i+1, p.ID, p.Amount, strPtrVal(p.Merchant), p.TransactionTime)
				}
			}
		}
	}

	if debug {
		log.Printf("[MATCH] invoice=%s linked=%d candidates=%d", invoiceID, len(linkedIDs), len(candidates))
	}

	type scored struct {
		payment models.Payment
		score   float64
		aScore  float64
		dScore  float64
		mScore  float64
	}
	scoredAll := make([]scored, 0, len(candidates))
	for _, p := range candidates {
		if _, ok := linkedIDs[p.ID]; ok {
			continue
		}
		score, aScore, dScore, mScore := computeInvoicePaymentScoreBreakdown(invoice, &p)
		scoredAll = append(scoredAll, scored{payment: p, score: score, aScore: aScore, dScore: dScore, mScore: mScore})
	}

	sort.Slice(scoredAll, func(i, j int) bool {
		if scoredAll[i].score == scoredAll[j].score {
			return scoredAll[i].payment.TransactionTime > scoredAll[j].payment.TransactionTime
		}
		return scoredAll[i].score > scoredAll[j].score
	})

	minScore := 0.15
	if invoice.Amount == nil || (invoice.Amount != nil && *invoice.Amount <= 0) {
		minScore = 0.05
	}

	out := make([]models.Payment, 0, limit)
	for _, s := range scoredAll {
		if s.score < minScore {
			continue
		}
		out = append(out, s.payment)
		if len(out) >= limit {
			break
		}
	}

	// If thresholding produced no matches, return the best-scoring candidates anyway (better UX for debugging).
	if len(out) == 0 {
		for _, s := range scoredAll {
			out = append(out, s.payment)
			if len(out) >= limit {
				break
			}
		}
	}

	if debug {
		top := 10
		if len(scoredAll) < top {
			top = len(scoredAll)
		}
		for i := 0; i < top; i++ {
			p := scoredAll[i].payment
			log.Printf(
				"[MATCH] invoice=%s rank=%d payment=%s score=%.3f amount=%.2f merchant=%q time=%q parts(a=%.3f d=%.3f m=%.3f)",
				invoiceID,
				i+1,
				p.ID,
				scoredAll[i].score,
				p.Amount,
				strPtrVal(p.Merchant),
				p.TransactionTime,
				scoredAll[i].aScore,
				scoredAll[i].dScore,
				scoredAll[i].mScore,
			)
		}
	}

	return out, nil
}

func strPtrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func valueOrNil(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func strValueOrNil(v *string) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

// parseInvoiceFile parses an invoice file and returns the extracted data.
// - PDF: PyMuPDF fast-path (with RapidOCR fallback) via OCRService.RecognizePDF
// - Images: RapidOCR v3 via OCRService.RecognizeImage
func (s *InvoiceService) parseInvoiceFile(filePath, filename string) (
	invoiceNumber, invoiceDate, sellerName, buyerName *string,
	amount, taxAmount *float64,
	extractedData, rawText *string,
	parseStatus string,
	parseError *string,
) {
	parseStatus = "parsing"

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".pdf" && ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		parseStatus = "failed"
		errMsg := "Only PDF/PNG/JPG files can be parsed"
		parseError = &errMsg
		return
	}

	// Use OCR service to extract text
	var (
		text string
		err  error
	)
	if ext == ".pdf" {
		text, err = s.ocrService.RecognizePDF(filePath)
	} else {
		text, err = s.ocrService.RecognizeImage(filePath)
	}
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
		parseStatus, parseError := s.parseInvoiceFile(filePath, invoice.Filename)

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
