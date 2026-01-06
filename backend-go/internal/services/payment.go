package services

import (
	"errors"
	"fmt"
	"log"
	"math"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrMissingTransactionTime = errors.New("missing transaction time")

type PaymentService struct {
	repo        *repository.PaymentRepository
	invoiceRepo *repository.InvoiceRepository
	blobRepo    *repository.OCRBlobRepository
	ocrService  *OCRService
	uploadsDir  string
}

func NewPaymentService(uploadsDir string) *PaymentService {
	return &PaymentService{
		repo:        repository.NewPaymentRepository(),
		invoiceRepo: repository.NewInvoiceRepository(),
		blobRepo:    repository.NewOCRBlobRepository(),
		ocrService:  NewOCRService(),
		uploadsDir:  uploadsDir,
	}
}

func (s *PaymentService) CreateDraftFromScreenshotUpload(ownerUserID string, screenshotPath string, fileSHA256 *string) (*models.Payment, error) {
	now := time.Now().UTC()
	utcTimeStr := now.Format(time.RFC3339)

	p := &models.Payment{
		ID:                utils.GenerateUUID(),
		OwnerUserID:       strings.TrimSpace(ownerUserID),
		IsDraft:           true,
		Amount:            0.0,
		TransactionTime:   utcTimeStr,
		TransactionTimeTs: unixMilli(now),
		ScreenshotPath:    &screenshotPath,
		FileSHA256:        fileSHA256,
		TripAssignSrc:     assignSrcAuto,
		TripAssignState:   assignStateNoMatch,
		DedupStatus:       DedupStatusOK,
	}

	db := database.GetDB()
	if err := db.Create(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PaymentService) DeleteDraftByScreenshotPath(ownerUserID string, screenshotPath string) error {
	screenshotPath = strings.TrimSpace(screenshotPath)
	if screenshotPath == "" {
		return nil
	}
	return database.GetDB().
		Where("owner_user_id = ? AND is_draft = ? AND screenshot_path = ?", strings.TrimSpace(ownerUserID), true, screenshotPath).
		Delete(&models.Payment{}).
		Error
}

func (s *PaymentService) UpdateDraftScreenshotPath(ownerUserID string, paymentID string, screenshotPath string, fileSHA256 *string) (*models.Payment, error) {
	paymentID = strings.TrimSpace(paymentID)
	screenshotPath = strings.TrimSpace(screenshotPath)
	if paymentID == "" {
		return nil, fmt.Errorf("missing payment id")
	}
	if screenshotPath == "" {
		return nil, fmt.Errorf("missing screenshot path")
	}

	update := map[string]any{
		"screenshot_path": screenshotPath,
	}
	if fileSHA256 != nil {
		h := strings.TrimSpace(*fileSHA256)
		if h != "" {
			update["file_sha256"] = h
		}
	}

	if err := database.GetDB().
		Model(&models.Payment{}).
		Where("id = ? AND owner_user_id = ? AND is_draft = 1", paymentID, strings.TrimSpace(ownerUserID)).
		Updates(update).Error; err != nil {
		return nil, err
	}
	return s.repo.FindByIDForOwner(strings.TrimSpace(ownerUserID), paymentID)
}

type paymentOCRTaskResult struct {
	Payment        *models.Payment       `json:"payment"`
	Extracted      *PaymentExtractedData `json:"extracted"`
	ScreenshotPath string                `json:"screenshot_path"`
	OCRError       string                `json:"ocr_error,omitempty"`
	Dedup          any                   `json:"dedup,omitempty"`
}

func (s *PaymentService) ProcessPaymentOCRTask(paymentID string) (any, error) {
	paymentID = strings.TrimSpace(paymentID)
	if paymentID == "" {
		return nil, fmt.Errorf("missing payment id")
	}

	payment, err := s.repo.FindByID(paymentID)
	if err != nil {
		return nil, err
	}
	if payment.ScreenshotPath == nil || strings.TrimSpace(*payment.ScreenshotPath) == "" {
		return nil, fmt.Errorf("payment has no screenshot")
	}

	text, err := s.ocrService.RecognizePaymentScreenshot(*payment.ScreenshotPath)
	if err != nil {
		return nil, err
	}

	extracted, parseErr := s.ocrService.ParsePaymentScreenshot(text)
	if parseErr != nil {
		return nil, parseErr
	}

	warn := ""
	if extracted.TransactionTime == nil || strings.TrimSpace(*extracted.TransactionTime) == "" {
		warn = "missing transaction time"
		extracted.TransactionTime = nil
	} else {
		shanghai := loadLocationOrUTC("Asia/Shanghai")
		if payTime, err := parsePaymentTimeToUTC(*extracted.TransactionTime, shanghai); err == nil {
			utcTimeStr := payTime.Format(time.RFC3339)
			extracted.TransactionTime = &utcTimeStr
		} else {
			warn = "missing transaction time"
			extracted.TransactionTime = nil
		}
	}

	extractedDataJSON, err := ExtractedDataToJSON(extracted)
	if err != nil {
		extractedDataJSON = nil
	}

	updateData := map[string]any{}

	if extracted.Amount != nil {
		absAmount := math.Abs(*extracted.Amount)
		if absAmount > 0 {
			updateData["amount"] = absAmount
			extracted.Amount = &absAmount
		}
	}
	if extracted.Merchant != nil {
		updateData["merchant"] = *extracted.Merchant
	}
	if extracted.PaymentMethod != nil {
		updateData["payment_method"] = *extracted.PaymentMethod
	}
	if extracted.TransactionTime != nil && strings.TrimSpace(*extracted.TransactionTime) != "" {
		if t, err := parseRFC3339ToUTC(*extracted.TransactionTime); err == nil {
			updateData["transaction_time"] = t.Format(time.RFC3339)
			updateData["transaction_time_ts"] = unixMilli(t)
		}
	}

	db := database.GetDB()
	ownerUserID := strings.TrimSpace(payment.OwnerUserID)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.Update(paymentID, updateData); err != nil {
			return err
		}
		return s.blobRepo.UpsertPaymentBlob(tx, ownerUserID, paymentID, extractedDataJSON)
	}); err != nil {
		return nil, err
	}

	updated, _ := s.repo.FindByID(paymentID)
	dedup := any(nil)
	if updated != nil {
		blob, _ := s.blobRepo.FindPaymentBlob(ownerUserID, updated.ID)
		if blob != nil {
			updated.ExtractedData = blob.ExtractedData
		}
	}

	// If we have a meaningful amount+time, compute suspected duplicates for UI.
	if updated != nil && updated.Amount > 0 && updated.TransactionTimeTs > 0 {
		if cands, derr := FindPaymentCandidatesByAmountTimeForOwner(strings.TrimSpace(updated.OwnerUserID), updated.Amount, updated.TransactionTimeTs, updated.ID, 5*time.Minute, 5); derr == nil && len(cands) > 0 {
			updated.DedupStatus = DedupStatusSuspected
			ref := cands[0].ID
			updated.DedupRefID = &ref
			_ = database.GetDB().Model(&models.Payment{}).Where("id = ?", updated.ID).Updates(map[string]any{
				"dedup_status": DedupStatusSuspected,
				"dedup_ref_id": ref,
			}).Error
			dedup = map[string]any{
				"kind":       "suspected_duplicate",
				"reason":     "amount_time",
				"candidates": cands,
			}
		}
	}

	out := &paymentOCRTaskResult{
		Payment:        updated,
		Extracted:      extracted,
		ScreenshotPath: strings.TrimSpace(*payment.ScreenshotPath),
		OCRError:       warn,
		Dedup:          dedup,
	}
	if warn == "" {
		out.OCRError = ""
	}
	return out, nil
}

type CreatePaymentInput struct {
	Amount          float64 `json:"amount" binding:"required"`
	Merchant        *string `json:"merchant"`
	Category        *string `json:"category"`
	PaymentMethod   *string `json:"payment_method"`
	Description     *string `json:"description"`
	TransactionTime string  `json:"transaction_time" binding:"required"`
	ScreenshotPath  *string `json:"screenshot_path"`
	ExtractedData   *string `json:"extracted_data"`
}

func (s *PaymentService) Create(ownerUserID string, input CreatePaymentInput) (*models.Payment, error) {
	t, err := parseRFC3339ToUTC(input.TransactionTime)
	if err != nil {
		return nil, fmt.Errorf("transaction_time must be RFC3339: %w", err)
	}

	var screenshotPath *string
	if input.ScreenshotPath != nil {
		p := strings.TrimSpace(*input.ScreenshotPath)
		if p != "" {
			screenshotPath = &p
		}
	}
	var extractedData *string
	if input.ExtractedData != nil {
		d := strings.TrimSpace(*input.ExtractedData)
		if d != "" {
			extractedData = &d
		}
	}

	payment := &models.Payment{
		ID:                utils.GenerateUUID(),
		OwnerUserID:       strings.TrimSpace(ownerUserID),
		Amount:            input.Amount,
		Merchant:          input.Merchant,
		Category:          input.Category,
		PaymentMethod:     input.PaymentMethod,
		Description:       input.Description,
		TransactionTime:   t.Format(time.RFC3339),
		TransactionTimeTs: unixMilli(t),
		ScreenshotPath:    screenshotPath,
		ExtractedData:     nil, // stored in payment_ocr_blobs
		DedupStatus:       DedupStatusOK,
		TripAssignSrc:     assignSrcAuto,
		TripAssignState:   assignStateNoMatch,
	}

	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(payment).Error; err != nil {
			return err
		}
		if extractedData != nil {
			if err := s.blobRepo.UpsertPaymentBlob(tx, strings.TrimSpace(ownerUserID), payment.ID, extractedData); err != nil {
				return err
			}
		}
		return autoAssignPaymentTx(tx, strings.TrimSpace(ownerUserID), payment)
	}); err != nil {
		return nil, err
	}

	// Include OCR payload in response.
	if extractedData != nil {
		payment.ExtractedData = extractedData
	}
	return payment, nil
}

type PaymentFilterInput struct {
	Limit     int    `form:"limit"`
	Offset    int    `form:"offset"`
	Cursor    string `form:"cursor"`
	StartDate string `form:"startDate"`
	EndDate   string `form:"endDate"`
	Category  string `form:"category"`
	// IncludeDraft controls whether draft records are included in listing/stats.
	// Default is false.
	IncludeDraft bool `form:"includeDraft"`
}

func normalizeLimitOffset(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *PaymentService) GetAll(ownerUserID string, filter PaymentFilterInput) ([]models.Payment, error) {
	startTs := int64(0)
	endTs := int64(0)
	if strings.TrimSpace(filter.StartDate) != "" {
		if t, err := parseRFC3339ToUTC(filter.StartDate); err == nil {
			startTs = unixMilli(t)
		} else {
			return nil, fmt.Errorf("invalid startDate: %w", err)
		}
	}
	if strings.TrimSpace(filter.EndDate) != "" {
		if t, err := parseRFC3339ToUTC(filter.EndDate); err == nil {
			endTs = unixMilli(t)
		} else {
			return nil, fmt.Errorf("invalid endDate: %w", err)
		}
	}

	return s.repo.FindAll(repository.PaymentFilter{
		OwnerUserID:  strings.TrimSpace(ownerUserID),
		Limit:        filter.Limit,
		Offset:       filter.Offset,
		StartDate:    filter.StartDate,
		EndDate:      filter.EndDate,
		StartTs:      startTs,
		EndTs:        endTs,
		Category:     filter.Category,
		IncludeDraft: filter.IncludeDraft,
	})
}

type PaymentListItem struct {
	models.Payment
	InvoiceCount int `json:"invoiceCount"`
}

func (s *PaymentService) GetAllWithInvoiceCounts(ownerUserID string, filter PaymentFilterInput) ([]PaymentListItem, error) {
	payments, err := s.GetAll(strings.TrimSpace(ownerUserID), filter)
	if err != nil {
		return nil, err
	}
	if len(payments) == 0 {
		return []PaymentListItem{}, nil
	}

	ids := make([]string, 0, len(payments))
	for _, p := range payments {
		ids = append(ids, p.ID)
	}

	type row struct {
		PaymentID string `gorm:"column:payment_id"`
		Cnt       int    `gorm:"column:cnt"`
	}
	var rows []row
	_ = database.GetDB().
		Table("invoice_payment_links").
		Select("payment_id, COUNT(*) AS cnt").
		Where("payment_id IN ?", ids).
		Group("payment_id").
		Scan(&rows).Error

	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[strings.TrimSpace(r.PaymentID)] = r.Cnt
	}

	out := make([]PaymentListItem, 0, len(payments))
	for _, p := range payments {
		out = append(out, PaymentListItem{
			Payment:      p,
			InvoiceCount: counts[p.ID],
		})
	}
	return out, nil
}

func (s *PaymentService) ListWithInvoiceCounts(ownerUserID string, filter PaymentFilterInput) ([]PaymentListItem, int64, error) {
	startTs := int64(0)
	endTs := int64(0)
	if strings.TrimSpace(filter.StartDate) != "" {
		if t, err := parseRFC3339ToUTC(filter.StartDate); err == nil {
			startTs = unixMilli(t)
		} else {
			return nil, 0, fmt.Errorf("invalid startDate: %w", err)
		}
	}
	if strings.TrimSpace(filter.EndDate) != "" {
		if t, err := parseRFC3339ToUTC(filter.EndDate); err == nil {
			endTs = unixMilli(t)
		} else {
			return nil, 0, fmt.Errorf("invalid endDate: %w", err)
		}
	}
	filter.Limit, filter.Offset = normalizeLimitOffset(filter.Limit, filter.Offset)
	afterTs := int64(0)
	afterID := ""
	if strings.TrimSpace(filter.Cursor) != "" {
		ts, id, err := decodePaymentCursor(filter.Cursor)
		if err != nil {
			return nil, 0, err
		}
		afterTs = ts
		afterID = id
		filter.Offset = 0
	}

	selectCols := []string{
		"id",
		"owner_user_id",
		"is_draft",
		"trip_id",
		"trip_assignment_source",
		"trip_assignment_state",
		"bad_debt",
		"amount",
		"merchant",
		"category",
		"payment_method",
		"description",
		"transaction_time",
		"transaction_time_ts",
		"screenshot_path",
		"dedup_status",
		"dedup_ref_id",
		"created_at",
	}

	payments, total, err := s.repo.FindAllPaged(repository.PaymentFilter{
		OwnerUserID:  strings.TrimSpace(ownerUserID),
		Limit:        filter.Limit,
		Offset:       filter.Offset,
		AfterTs:      afterTs,
		AfterID:      afterID,
		StartDate:    filter.StartDate,
		EndDate:      filter.EndDate,
		StartTs:      startTs,
		EndTs:        endTs,
		Category:     filter.Category,
		IncludeDraft: filter.IncludeDraft,
	}, selectCols)
	if err != nil {
		return nil, 0, err
	}
	if len(payments) == 0 {
		return []PaymentListItem{}, total, nil
	}

	ids := make([]string, 0, len(payments))
	for _, p := range payments {
		ids = append(ids, p.ID)
	}

	type row struct {
		PaymentID string `gorm:"column:payment_id"`
		Cnt       int    `gorm:"column:cnt"`
	}
	var rows []row
	_ = database.GetDB().
		Table("invoice_payment_links").
		Select("payment_id, COUNT(*) AS cnt").
		Where("payment_id IN ?", ids).
		Group("payment_id").
		Scan(&rows).Error

	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[strings.TrimSpace(r.PaymentID)] = r.Cnt
	}

	out := make([]PaymentListItem, 0, len(payments))
	for _, p := range payments {
		out = append(out, PaymentListItem{
			Payment:      p,
			InvoiceCount: counts[p.ID],
		})
	}
	return out, total, nil
}

func (s *PaymentService) GetByID(ownerUserID string, id string) (*models.Payment, error) {
	p, err := s.repo.FindByIDForOwner(strings.TrimSpace(ownerUserID), id)
	if err != nil {
		return nil, err
	}
	blob, err := s.blobRepo.FindPaymentBlob(strings.TrimSpace(ownerUserID), p.ID)
	if err == nil && blob != nil {
		p.ExtractedData = blob.ExtractedData
	}
	return p, nil
}

type UpdatePaymentInput struct {
	Amount             *float64 `json:"amount"`
	Merchant           *string  `json:"merchant"`
	Category           *string  `json:"category"`
	PaymentMethod      *string  `json:"payment_method"`
	Description        *string  `json:"description"`
	TransactionTime    *string  `json:"transaction_time"`
	TripID             *string  `json:"trip_id"`
	TripAssignSrc      *string  `json:"trip_assignment_source"`
	BadDebt            *bool    `json:"bad_debt"`
	Confirm            *bool    `json:"confirm"`
	ForceDuplicateSave *bool    `json:"force_duplicate_save"`
}

func (s *PaymentService) Update(ownerUserID string, id string, input UpdatePaymentInput) error {
	needsRecalc := input.TripID != nil || input.TripAssignSrc != nil || input.BadDebt != nil
	timeChanged := input.TransactionTime != nil
	confirming := input.Confirm != nil && *input.Confirm
	moveScreenshot := false

	var before *models.Payment
	if needsRecalc || timeChanged || confirming || moveScreenshot {
		p, err := s.repo.FindByIDForOwner(strings.TrimSpace(ownerUserID), id)
		if err != nil {
			return err
		}
		before = p
	}

	// On confirm, enforce dedup rules:
	// - hash duplicate: hard block (no override)
	// - amount+time duplicate: allow only with force_duplicate_save
	if confirming && before != nil {
		force := input.ForceDuplicateSave != nil && *input.ForceDuplicateSave

		hash := ""
		if before.FileSHA256 != nil {
			hash = strings.TrimSpace(*before.FileSHA256)
		}
		if hash != "" {
			if existing, err := FindPaymentByFileSHA256ForOwner(strings.TrimSpace(ownerUserID), hash, id); err != nil {
				return err
			} else if existing != nil {
				return &DuplicateError{
					Kind:            "hash_duplicate",
					Reason:          "file_sha256",
					Entity:          "payment",
					ExistingID:      existing.ID,
					ExistingIsDraft: existing.IsDraft,
				}
			}
		}

		nextAmount := before.Amount
		if input.Amount != nil {
			nextAmount = *input.Amount
		}
		nextTs := before.TransactionTimeTs
		if input.TransactionTime != nil {
			if t, err := parseRFC3339ToUTC(*input.TransactionTime); err == nil {
				nextTs = unixMilli(t)
			}
		}

		cands, err := FindPaymentCandidatesByAmountTimeForOwner(strings.TrimSpace(ownerUserID), nextAmount, nextTs, id, 5*time.Minute, 5)
		if err != nil {
			return err
		}
		if len(cands) > 0 && !force {
			return &DuplicateError{
				Kind:       "suspected_duplicate",
				Reason:     "amount_time",
				Entity:     "payment",
				Candidates: cands,
			}
		}
	}

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
		t, err := parseRFC3339ToUTC(*input.TransactionTime)
		if err != nil {
			return fmt.Errorf("transaction_time must be RFC3339: %w", err)
		}
		data["transaction_time"] = t.Format(time.RFC3339)
		data["transaction_time_ts"] = unixMilli(t)
	}

	normalizedTripID := ""
	if input.TripID != nil {
		trimmed := strings.TrimSpace(*input.TripID)
		if trimmed == "" {
			data["trip_id"] = nil
		} else {
			data["trip_id"] = trimmed
			normalizedTripID = trimmed
		}
	} else if before != nil && before.TripID != nil {
		normalizedTripID = strings.TrimSpace(*before.TripID)
	}

	if input.TripID != nil || input.TripAssignSrc != nil {
		src := ""
		if input.TripAssignSrc != nil {
			src = strings.TrimSpace(*input.TripAssignSrc)
		}
		// Backward-compatible default: if caller changes trip_id without specifying source,
		// treat non-empty trip_id as manual and empty as blocked (explicit user intent).
		if src == "" {
			if input.TripID != nil && strings.TrimSpace(*input.TripID) == "" {
				src = assignSrcBlocked
			} else if input.TripID != nil {
				src = assignSrcManual
			}
		}
		if src != "" {
			if src != assignSrcAuto && src != assignSrcManual && src != assignSrcBlocked {
				return fmt.Errorf("invalid trip_assignment_source")
			}
			data["trip_assignment_source"] = src
			if src == assignSrcBlocked {
				data["trip_assignment_state"] = assignStateBlocked
				data["trip_id"] = nil
				normalizedTripID = ""
			} else if src == assignSrcManual && normalizedTripID != "" {
				data["trip_assignment_state"] = assignStateAssigned
			} else if src == assignSrcManual && normalizedTripID == "" {
				// Manual but unassigned: keep it in a reviewable state so UI can route it to "pending".
				data["trip_assignment_state"] = assignStateNoMatch
			} else if src == assignSrcAuto {
				// state will be recomputed by automatic logic elsewhere
			}
		}
	}
	if input.BadDebt != nil {
		data["bad_debt"] = *input.BadDebt
	}
	if input.Confirm != nil && *input.Confirm {
		data["is_draft"] = false
	}

	// Persist dedup status changes on confirm.
	if confirming && before != nil {
		force := input.ForceDuplicateSave != nil && *input.ForceDuplicateSave

		nextAmount := before.Amount
		if input.Amount != nil {
			nextAmount = *input.Amount
		}
		nextTs := before.TransactionTimeTs
		if input.TransactionTime != nil {
			if t, err := parseRFC3339ToUTC(*input.TransactionTime); err == nil {
				nextTs = unixMilli(t)
			}
		}

		cands, err := FindPaymentCandidatesByAmountTimeForOwner(strings.TrimSpace(ownerUserID), nextAmount, nextTs, id, 5*time.Minute, 5)
		if err == nil && len(cands) > 0 {
			if force {
				data["dedup_status"] = DedupStatusForced
				data["dedup_ref_id"] = cands[0].ID
			} else {
				// Should have been blocked above, but keep safe default.
				data["dedup_status"] = DedupStatusSuspected
				data["dedup_ref_id"] = cands[0].ID
			}
		} else {
			data["dedup_status"] = DedupStatusOK
			data["dedup_ref_id"] = nil
		}
	}

	if len(data) == 0 {
		return nil
	}

	// No file move/rename on confirm. The draft flag alone controls visibility/lifecycle.

	if err := s.repo.UpdateForOwner(strings.TrimSpace(ownerUserID), id, data); err != nil {
		return err
	}

	after, _ := s.repo.FindByIDForOwner(strings.TrimSpace(ownerUserID), id)

	// If transaction time changed (common during OCR confirm), recompute auto trip assignment.
	if (timeChanged || confirming) && after != nil && strings.TrimSpace(after.TripAssignSrc) == assignSrcAuto {
		db := database.GetDB()
		_ = db.Transaction(func(tx *gorm.DB) error {
			return autoAssignPaymentTx(tx, strings.TrimSpace(ownerUserID), after)
		})
		after, _ = s.repo.FindByIDForOwner(strings.TrimSpace(ownerUserID), id)
	}

	if !needsRecalc && !(timeChanged || confirming) {
		return nil
	}

	affected := make([]string, 0, 4)
	if before != nil && before.BadDebt && before.TripID != nil && strings.TrimSpace(*before.TripID) != "" {
		affected = append(affected, strings.TrimSpace(*before.TripID))
	}
	if after != nil && after.BadDebt && after.TripID != nil && strings.TrimSpace(*after.TripID) != "" {
		affected = append(affected, strings.TrimSpace(*after.TripID))
	}
	return recalcTripBadDebtLockedForTripIDs(affected)
}

func (s *PaymentService) Delete(ownerUserID string, id string) error {
	payment, err := s.repo.FindByIDForOwner(strings.TrimSpace(ownerUserID), id)
	if err != nil {
		return err
	}

	var tripID string
	if payment.TripID != nil {
		tripID = strings.TrimSpace(*payment.TripID)
	}

	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		_ = tx.Where("payment_id = ?", id).Delete(&models.InvoicePaymentLink{}).Error
		_ = s.blobRepo.DeletePaymentBlob(tx, strings.TrimSpace(ownerUserID), id)
		return tx.Where("id = ? AND owner_user_id = ?", id, strings.TrimSpace(ownerUserID)).Delete(&models.Payment{}).Error
	}); err != nil {
		return err
	}

	if tripID == "" {
		return nil
	}
	return recalcTripBadDebtLocked(tripID)
}

func (s *PaymentService) GetStats(ownerUserID string, startDate, endDate string) (*models.PaymentStats, error) {
	startTs := int64(0)
	endTs := int64(0)
	if strings.TrimSpace(startDate) != "" {
		if t, err := parseRFC3339ToUTC(startDate); err == nil {
			startTs = unixMilli(t)
		} else {
			return nil, fmt.Errorf("invalid startDate: %w", err)
		}
	}
	if strings.TrimSpace(endDate) != "" {
		if t, err := parseRFC3339ToUTC(endDate); err == nil {
			endTs = unixMilli(t)
		} else {
			return nil, fmt.Errorf("invalid endDate: %w", err)
		}
	}
	return s.repo.GetStatsByTs(strings.TrimSpace(ownerUserID), startTs, endTs)
}

// CreateFromScreenshot creates a payment from a screenshot with OCR
type CreateFromScreenshotInput struct {
	ScreenshotPath string
	FileSHA256     *string
}

// CreateFromScreenshot creates a payment from a screenshot with OCR.
// If OCR cannot extract a valid transaction time, this returns an error (policy A).
func (s *PaymentService) CreateFromScreenshot(ownerUserID string, input CreateFromScreenshotInput) (*models.Payment, *PaymentExtractedData, error) {

	// Perform OCR on the screenshot with specialized payment screenshot recognition
	text, err := s.ocrService.RecognizePaymentScreenshot(input.ScreenshotPath)
	if err != nil {
		return nil, nil, err
	}

	// Parse payment data from OCR text
	extracted, parseErr := s.ocrService.ParsePaymentScreenshot(text)
	if parseErr != nil {
		return nil, nil, parseErr
	}

	warn := error(nil)
	utcTimeStr := ""
	payTime := time.Time{}
	if extracted.TransactionTime == nil || strings.TrimSpace(*extracted.TransactionTime) == "" {
		warn = fmt.Errorf("%w", ErrMissingTransactionTime)
	} else {
		shanghai := loadLocationOrUTC("Asia/Shanghai")
		t, err := parsePaymentTimeToUTC(*extracted.TransactionTime, shanghai)
		if err != nil {
			warn = fmt.Errorf("%w: %v", ErrMissingTransactionTime, err)
		} else {
			payTime = t
			utcTimeStr = payTime.Format(time.RFC3339)
			extracted.TransactionTime = &utcTimeStr
		}
	}
	if warn != nil {
		// Force UI to manually choose transaction time.
		extracted.TransactionTime = nil
		now := time.Now().UTC()
		payTime = now
		utcTimeStr = now.Format(time.RFC3339)
	}

	// Store extracted data as JSON
	extractedDataJSON, err := ExtractedDataToJSON(extracted)
	if err != nil {
		// Log the error but continue - extracted data is optional
		extractedDataJSON = nil
	}

	// Create draft payment record with extracted data (confirmed on user save).
	payment := &models.Payment{
		ID:                utils.GenerateUUID(),
		OwnerUserID:       strings.TrimSpace(ownerUserID),
		IsDraft:           true,
		Amount:            0.0, // Default to 0.0, will be updated if amount is extracted
		Merchant:          extracted.Merchant,
		PaymentMethod:     extracted.PaymentMethod,
		TransactionTime:   utcTimeStr,
		TransactionTimeTs: unixMilli(payTime),
		ScreenshotPath:    &input.ScreenshotPath,
		FileSHA256:        input.FileSHA256,
		ExtractedData:     nil, // stored in payment_ocr_blobs
		TripAssignSrc:     assignSrcAuto,
		TripAssignState:   assignStateNoMatch,
		DedupStatus:       DedupStatusOK,
	}

	// Set amount if extracted
	if extracted.Amount != nil {
		absAmount := math.Abs(*extracted.Amount)
		if absAmount > 0 {
			payment.Amount = absAmount
			// Normalize for UI/matching: store expenses as positive amounts.
			extracted.Amount = &absAmount
		}
	}

	// Set transaction time if extracted
	db := database.GetDB()
	if err := db.Create(payment).Error; err != nil {
		return nil, nil, err
	}
	_ = s.blobRepo.UpsertPaymentBlob(nil, strings.TrimSpace(ownerUserID), payment.ID, extractedDataJSON)
	payment.ExtractedData = extractedDataJSON

	// Mark suspected duplicates for UI (amount+time) if we have a meaningful timestamp.
	if payment.Amount > 0 && payment.TransactionTimeTs > 0 {
		if cands, err := FindPaymentCandidatesByAmountTimeForOwner(strings.TrimSpace(payment.OwnerUserID), payment.Amount, payment.TransactionTimeTs, payment.ID, 5*time.Minute, 5); err == nil && len(cands) > 0 {
			payment.DedupStatus = DedupStatusSuspected
			ref := cands[0].ID
			payment.DedupRefID = &ref
			_ = db.Model(&models.Payment{}).Where("id = ?", payment.ID).Updates(map[string]interface{}{
				"dedup_status": DedupStatusSuspected,
				"dedup_ref_id": ref,
			}).Error
		}
	}

	if warn != nil {
		return payment, extracted, warn
	}
	return payment, extracted, nil
}

// GetLinkedInvoices returns all invoices linked to a payment
func (s *PaymentService) GetLinkedInvoices(ownerUserID string, paymentID string) ([]models.Invoice, error) {
	return s.repo.GetLinkedInvoices(strings.TrimSpace(ownerUserID), paymentID)
}

type scoredInvoiceCandidate struct {
	invoice models.Invoice
	score   float64
	aScore  float64
	dScore  float64
	mScore  float64
}

func scoreInvoiceCandidates(payment *models.Payment, candidates []models.Invoice, linkedIDs map[string]struct{}) []scoredInvoiceCandidate {
	scoredAll := make([]scoredInvoiceCandidate, 0, len(candidates))
	for _, inv := range candidates {
		if _, ok := linkedIDs[inv.ID]; ok {
			continue
		}
		score, aScore, dScore, mScore := computeInvoicePaymentScoreBreakdown(&inv, payment)
		scoredAll = append(scoredAll, scoredInvoiceCandidate{invoice: inv, score: score, aScore: aScore, dScore: dScore, mScore: mScore})
	}

	sort.Slice(scoredAll, func(i, j int) bool {
		if scoredAll[i].score == scoredAll[j].score {
			return scoredAll[i].invoice.CreatedAt.After(scoredAll[j].invoice.CreatedAt)
		}
		return scoredAll[i].score > scoredAll[j].score
	})

	return scoredAll
}

func pickSuggestedInvoices(payment *models.Payment, scoredAll []scoredInvoiceCandidate, limit int) []models.Invoice {
	minScore := 0.15
	if payment != nil && payment.Amount == 0 {
		minScore = 0.05
	}

	out := make([]models.Invoice, 0, limit)
	for _, s := range scoredAll {
		if s.score < minScore {
			continue
		}
		out = append(out, s.invoice)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// SuggestInvoices suggests invoices that might match this payment using amount/seller/date signals.
func (s *PaymentService) SuggestInvoices(ownerUserID string, paymentID string, limit int, debug bool) ([]models.Invoice, error) {
	payment, err := s.repo.FindByIDForOwner(strings.TrimSpace(ownerUserID), paymentID)
	if err != nil {
		return nil, err
	}

	if debug {
		log.Printf(
			"[MATCH] payment=%s amount=%.2f merchant=%q time=%q",
			paymentID,
			payment.Amount,
			strPtrVal(payment.Merchant),
			payment.TransactionTime,
		)
	}

	linked, _ := s.repo.GetLinkedInvoices(strings.TrimSpace(ownerUserID), paymentID)
	linkedIDs := make(map[string]struct{}, len(linked))
	for _, inv := range linked {
		linkedIDs[inv.ID] = struct{}{}
	}

	if limit <= 0 {
		limit = 10
	}
	maxCandidates := limit * 50
	if maxCandidates < 200 {
		maxCandidates = 200
	}

	candidates, err := s.invoiceRepo.SuggestInvoices(payment, maxCandidates)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		// When payment amount is available, prefer returning empty rather than guessing from unrelated recent invoices.
		// Users can still manually pick from the "unlinked invoices" list in the UI.
		if payment.Amount > 0 {
			if debug {
				log.Printf("[MATCH] payment=%s repo candidates=0 (amount=%.2f), return empty suggestions", paymentID, payment.Amount)
			}
			return []models.Invoice{}, nil
		}

		// If amount is missing (0), we can fallback to recent unlinked invoices so merchant/date signals still work.
		if recent, _, err := s.invoiceRepo.FindUnlinked(strings.TrimSpace(payment.OwnerUserID), maxCandidates, 0); err == nil {
			candidates = recent
			if debug {
				log.Printf("[MATCH] payment=%s repo candidates=0 (amount=0), fallback to recent unlinked invoices=%d", paymentID, len(recent))
			}
		}
	}

	if debug {
		log.Printf("[MATCH] payment=%s linked=%d candidates=%d", paymentID, len(linkedIDs), len(candidates))
	}

	scoredAll := scoreInvoiceCandidates(payment, candidates, linkedIDs)
	out := pickSuggestedInvoices(payment, scoredAll, limit)

	if debug {
		top := 10
		if len(scoredAll) < top {
			top = len(scoredAll)
		}
		for i := 0; i < top; i++ {
			inv := scoredAll[i].invoice
			log.Printf(
				"[MATCH] payment=%s rank=%d invoice=%s score=%.3f amount=%v seller=%v invoice_date=%v parts(a=%.3f d=%.3f m=%.3f)",
				paymentID,
				i+1,
				inv.ID,
				scoredAll[i].score,
				valueOrNil(inv.Amount),
				strValueOrNil(inv.SellerName),
				strValueOrNil(inv.InvoiceDate),
				scoredAll[i].aScore,
				scoredAll[i].dScore,
				scoredAll[i].mScore,
			)
		}
	}

	return out, nil
}

// ReparseScreenshot re-parses the screenshot for a payment record
func (s *PaymentService) ReparseScreenshot(paymentID string) (*PaymentExtractedData, error) {
	// Get the payment record
	payment, err := s.repo.FindByID(paymentID)
	if err != nil {
		return nil, err
	}

	// Check if payment has a screenshot
	if payment.ScreenshotPath == nil || *payment.ScreenshotPath == "" {
		return nil, fmt.Errorf("payment has no screenshot")
	}

	// Perform OCR on the screenshot with specialized payment screenshot recognition
	text, err := s.ocrService.RecognizePaymentScreenshot(*payment.ScreenshotPath)
	if err != nil {
		return nil, fmt.Errorf("OCR recognition failed: %w", err)
	}

	// Parse payment data from OCR text
	extracted, err := s.ocrService.ParsePaymentScreenshot(text)
	if err != nil {
		return nil, fmt.Errorf("OCR parsing failed: %w", err)
	}

	// Store extracted data as JSON
	extractedDataJSON, err := ExtractedDataToJSON(extracted)
	if err != nil {
		extractedDataJSON = nil
	}

	// Update payment record with new extracted data
	updateData := make(map[string]interface{})

	// Update amount if extracted
	if extracted.Amount != nil {
		absAmount := math.Abs(*extracted.Amount)
		if absAmount > 0 {
			updateData["amount"] = absAmount
			extracted.Amount = &absAmount
		}
	}

	// Update merchant if extracted
	if extracted.Merchant != nil {
		updateData["merchant"] = *extracted.Merchant
	}

	// Update payment method if extracted
	if extracted.PaymentMethod != nil {
		updateData["payment_method"] = *extracted.PaymentMethod
	}

	// Update transaction time if extracted
	if extracted.TransactionTime != nil && strings.TrimSpace(*extracted.TransactionTime) != "" {
		shanghai := loadLocationOrUTC("Asia/Shanghai")
		if payTime, err := parsePaymentTimeToUTC(*extracted.TransactionTime, shanghai); err == nil {
			utcTimeStr := payTime.Format(time.RFC3339)
			extracted.TransactionTime = &utcTimeStr
			updateData["transaction_time"] = utcTimeStr
			updateData["transaction_time_ts"] = unixMilli(payTime)
		}
	}

	// Update the payment record
	if err := s.repo.Update(paymentID, updateData); err != nil {
		return nil, fmt.Errorf("failed to update payment: %w", err)
	}
	_ = s.blobRepo.UpsertPaymentBlob(nil, strings.TrimSpace(payment.OwnerUserID), paymentID, extractedDataJSON)

	return extracted, nil
}
