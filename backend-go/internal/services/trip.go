package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/money"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

type TripService struct {
	repo        *repository.TripRepository
	paymentRepo *repository.PaymentRepository
	uploadsDir  string
}

func NewTripService(db *gorm.DB, uploadsDir string) *TripService {
	return &TripService{
		repo:        repository.NewTripRepository(),
		paymentRepo: repository.NewPaymentRepository(db),
		uploadsDir:  uploadsDir,
	}
}

type CreateTripInput struct {
	Name      string  `json:"name" binding:"required"`
	StartTime string  `json:"start_time" binding:"required"`
	EndTime   string  `json:"end_time" binding:"required"`
	Timezone  *string `json:"timezone"`
	// unreimbursed|reimbursed (optional; defaults to unreimbursed)
	ReimburseStatus *string `json:"reimburse_status"`
	Note            *string `json:"note"`
}

func (s *TripService) Create(ownerUserID string, input CreateTripInput) (*models.Trip, *AssignmentChangeSummary, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	if err := validateRFC3339Range(input.StartTime, input.EndTime); err != nil {
		return nil, nil, err
	}

	reimburseStatus := "unreimbursed"
	if input.ReimburseStatus != nil {
		reimburseStatus = strings.TrimSpace(*input.ReimburseStatus)
	}
	if reimburseStatus == "" {
		reimburseStatus = "unreimbursed"
	}
	if reimburseStatus != "unreimbursed" && reimburseStatus != "reimbursed" {
		return nil, nil, fmt.Errorf("invalid reimburse_status")
	}

	timezone := "Asia/Shanghai"
	if input.Timezone != nil && strings.TrimSpace(*input.Timezone) != "" {
		timezone = strings.TrimSpace(*input.Timezone)
	}

	st, err := parseRFC3339ToUTC(input.StartTime)
	if err != nil {
		return nil, nil, fmt.Errorf("start_time must be RFC3339: %w", err)
	}
	et, err := parseRFC3339ToUTC(input.EndTime)
	if err != nil {
		return nil, nil, fmt.Errorf("end_time must be RFC3339: %w", err)
	}

	trip := &models.Trip{
		ID:              utils.GenerateUUID(),
		OwnerUserID:     strings.TrimSpace(ownerUserID),
		Name:            strings.TrimSpace(input.Name),
		StartTime:       st.Format(time.RFC3339),
		EndTime:         et.Format(time.RFC3339),
		StartTimeTs:     unixMilli(st),
		EndTimeTs:       unixMilli(et),
		Timezone:        timezone,
		ReimburseStatus: reimburseStatus,
		Note:            input.Note,
	}

	var changes *AssignmentChangeSummary
	var affectedTripIDs []string
	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(trip).Error; err != nil {
			return err
		}
		c, tripIDs, err := recomputeAutoAssignmentsForRangeTx(tx, strings.TrimSpace(ownerUserID), trip.StartTimeTs, trip.EndTimeTs)
		if err != nil {
			return err
		}
		changes = c
		affectedTripIDs = tripIDs
		return nil
	}); err != nil {
		return nil, nil, err
	}

	_ = recalcTripBadDebtLockedForTripIDs(affectedTripIDs)
	return trip, changes, nil
}

func (s *TripService) GetAll(ownerUserID string) ([]models.Trip, error) {
	return s.GetAllCtx(context.Background(), ownerUserID)
}

func (s *TripService) GetAllCtx(ctx context.Context, ownerUserID string) ([]models.Trip, error) {
	return s.repo.FindAllCtx(ctx, strings.TrimSpace(ownerUserID))
}

func (s *TripService) GetByID(ownerUserID string, id string) (*models.Trip, error) {
	return s.GetByIDCtx(context.Background(), ownerUserID, id)
}

func (s *TripService) GetByIDCtx(ctx context.Context, ownerUserID string, id string) (*models.Trip, error) {
	return s.repo.FindByIDForOwnerCtx(ctx, strings.TrimSpace(ownerUserID), id)
}

type UpdateTripInput struct {
	Name      *string `json:"name"`
	StartTime *string `json:"start_time"`
	EndTime   *string `json:"end_time"`
	Timezone  *string `json:"timezone"`
	// unreimbursed|reimbursed
	ReimburseStatus *string `json:"reimburse_status"`
	Note            *string `json:"note"`
}

func (s *TripService) Update(ownerUserID string, id string, input UpdateTripInput) (*AssignmentChangeSummary, error) {
	db := database.GetDB()
	var changes *AssignmentChangeSummary

	var affectedTripIDs []string
	err := db.Transaction(func(tx *gorm.DB) error {
		var existing models.Trip
		if err := tx.Model(&models.Trip{}).Where("id = ? AND owner_user_id = ?", id, strings.TrimSpace(ownerUserID)).First(&existing).Error; err != nil {
			return err
		}
		oldStartTs := existing.StartTimeTs
		oldEndTs := existing.EndTimeTs
		newStartTs := oldStartTs
		newEndTs := oldEndTs

		data := make(map[string]interface{})

		if input.Name != nil {
			name := strings.TrimSpace(*input.Name)
			if name == "" {
				return fmt.Errorf("name is required")
			}
			data["name"] = name
		}

		start := ""
		end := ""
		if input.StartTime != nil {
			start = strings.TrimSpace(*input.StartTime)
		} else {
			start = existing.StartTime
		}
		if input.EndTime != nil {
			end = strings.TrimSpace(*input.EndTime)
		} else {
			end = existing.EndTime
		}

		if input.StartTime != nil || input.EndTime != nil {
			if err := validateRFC3339Range(start, end); err != nil {
				return err
			}
			st, err := parseRFC3339ToUTC(start)
			if err != nil {
				return fmt.Errorf("start_time must be RFC3339: %w", err)
			}
			et, err := parseRFC3339ToUTC(end)
			if err != nil {
				return fmt.Errorf("end_time must be RFC3339: %w", err)
			}
			data["start_time"] = st.Format(time.RFC3339)
			data["end_time"] = et.Format(time.RFC3339)
			data["start_time_ts"] = unixMilli(st)
			data["end_time_ts"] = unixMilli(et)
			newStartTs = unixMilli(st)
			newEndTs = unixMilli(et)
		}

		if input.Timezone != nil {
			tz := strings.TrimSpace(*input.Timezone)
			if tz == "" {
				tz = "Asia/Shanghai"
			}
			data["timezone"] = tz
		}

		if input.Note != nil {
			data["note"] = *input.Note
		}
		if input.ReimburseStatus != nil {
			status := strings.TrimSpace(*input.ReimburseStatus)
			if status != "unreimbursed" && status != "reimbursed" {
				return fmt.Errorf("invalid reimburse_status")
			}
			data["reimburse_status"] = status
		}

		if len(data) > 0 {
			if err := tx.Model(&models.Trip{}).Where("id = ? AND owner_user_id = ?", id, strings.TrimSpace(ownerUserID)).Updates(data).Error; err != nil {
				return err
			}
		}

		unionStart := oldStartTs
		if newStartTs < unionStart {
			unionStart = newStartTs
		}
		unionEnd := oldEndTs
		if newEndTs > unionEnd {
			unionEnd = newEndTs
		}

		c, tripIDs, err := recomputeAutoAssignmentsForRangeTx(tx, strings.TrimSpace(ownerUserID), unionStart, unionEnd)
		if err != nil {
			return err
		}
		changes = c
		affectedTripIDs = tripIDs
		return nil
	})
	if err != nil {
		return nil, err
	}

	_ = recalcTripBadDebtLockedForTripIDs(affectedTripIDs)
	return changes, nil
}

type TripSummary struct {
	TripID         string  `json:"trip_id"`
	PaymentCount   int     `json:"payment_count"`
	TotalAmount    float64 `json:"total_amount"`
	LinkedInvoices int     `json:"linked_invoices"`
	UnlinkedPays   int     `json:"unlinked_payments"`
}

func (s *TripService) GetSummary(ownerUserID string, tripID string) (*TripSummary, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	return s.GetSummaryCtx(context.Background(), ownerUserID, tripID)
}

func (s *TripService) GetSummaryCtx(ctx context.Context, ownerUserID string, tripID string) (*TripSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	db := database.GetDB().WithContext(ctx)
	ownerUserID = strings.TrimSpace(ownerUserID)
	tripID = strings.TrimSpace(tripID)
	if ownerUserID == "" || tripID == "" {
		return nil, gorm.ErrRecordNotFound
	}

	out := &TripSummary{TripID: tripID}
	type payAgg struct {
		PaymentCount int64 `gorm:"column:payment_count"`
		TotalCents   int64 `gorm:"column:total_cents"`
	}
	var pa payAgg
	if err := db.
		Model(&models.Payment{}).
		Select("COUNT(*) AS payment_count, COALESCE(SUM(amount_cents), 0) AS total_cents").
		Where("owner_user_id = ?", ownerUserID).
		Where("trip_id = ?", tripID).
		Where("is_draft = 0").
		Scan(&pa).Error; err != nil {
		return nil, err
	}
	out.PaymentCount = int(pa.PaymentCount)
	out.TotalAmount = money.ToMajor(pa.TotalCents)
	if pa.PaymentCount == 0 {
		return out, nil
	}

	// Count distinct invoices linked to these payments.
	var invoiceCount int64
	if err := db.
		Table("invoice_payment_links AS l").
		Joins("JOIN payments AS p ON p.id = l.payment_id").
		Where("p.owner_user_id = ?", ownerUserID).
		Where("p.trip_id = ?", tripID).
		Where("p.is_draft = 0").
		Distinct("l.invoice_id").
		Count(&invoiceCount).Error; err != nil {
		return nil, err
	}
	out.LinkedInvoices = int(invoiceCount)

	// Count payments with no linked invoices.
	var unlinked int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM payments p
		LEFT JOIN invoice_payment_links l ON l.payment_id = p.id
		WHERE p.owner_user_id = ?
		  AND p.trip_id = ?
		  AND p.is_draft = 0
		  AND l.payment_id IS NULL
	`, ownerUserID, tripID).Scan(&unlinked).Error; err != nil {
		return nil, err
	}
	out.UnlinkedPays = int(unlinked)
	return out, nil
}

func (s *TripService) GetAllSummaries(ownerUserID string) ([]TripSummary, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	return s.GetAllSummariesCtx(context.Background(), ownerUserID)
}

func (s *TripService) GetAllSummariesCtx(ctx context.Context, ownerUserID string) ([]TripSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	db := database.GetDB().WithContext(ctx)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, fmt.Errorf("missing owner_user_id")
	}

	var out []TripSummary
	err := db.Raw(`
		SELECT
			t.id AS trip_id,
			COALESCE(p.payment_count, 0) AS payment_count,
			COALESCE(p.total_cents, 0) / 100.0 AS total_amount,
			COALESCE(li.linked_invoices, 0) AS linked_invoices,
			COALESCE(p.unlinked_pays, 0) AS unlinked_pays
		FROM trips t
		LEFT JOIN (
			SELECT
				trip_id,
				owner_user_id,
				COUNT(*) AS payment_count,
				COALESCE(SUM(amount_cents), 0) AS total_cents,
				COALESCE(SUM(CASE
					WHEN NOT EXISTS (SELECT 1 FROM invoice_payment_links l WHERE l.payment_id = payments.id) THEN 1
					ELSE 0
				END), 0) AS unlinked_pays
			FROM payments
			WHERE owner_user_id = ? AND is_draft = 0
			GROUP BY owner_user_id, trip_id
		) p ON p.trip_id = t.id AND p.owner_user_id = t.owner_user_id
		LEFT JOIN (
			SELECT
				p.trip_id AS trip_id,
				p.owner_user_id AS owner_user_id,
				COUNT(DISTINCT l.invoice_id) AS linked_invoices
			FROM payments p
			JOIN invoice_payment_links l ON l.payment_id = p.id
			WHERE p.owner_user_id = ? AND p.is_draft = 0
			GROUP BY p.owner_user_id, p.trip_id
		) li ON li.trip_id = t.id AND li.owner_user_id = t.owner_user_id
		WHERE t.owner_user_id = ?
		ORDER BY t.start_time_ts DESC
	`, ownerUserID, ownerUserID, ownerUserID).Scan(&out).Error
	return out, err
}

type TripPaymentInvoice struct {
	ID            string   `json:"id"`
	InvoiceNumber *string  `json:"invoice_number"`
	InvoiceDate   *string  `json:"invoice_date"`
	Amount        *float64 `json:"amount"`
	SellerName    *string  `json:"seller_name"`
	BadDebt       bool     `json:"bad_debt"`
}

type TripPaymentWithInvoices struct {
	models.Payment
	Invoices []TripPaymentInvoice `json:"invoices"`
}

func (s *TripService) GetPayments(ownerUserID string, tripID string, includeInvoices bool) ([]TripPaymentWithInvoices, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	return s.GetPaymentsCtx(context.Background(), ownerUserID, tripID, includeInvoices)
}

func (s *TripService) GetPaymentsCtx(ctx context.Context, ownerUserID string, tripID string, includeInvoices bool) ([]TripPaymentWithInvoices, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	db := database.GetDB().WithContext(ctx)
	ownerUserID = strings.TrimSpace(ownerUserID)
	tripID = strings.TrimSpace(tripID)
	if ownerUserID == "" || tripID == "" {
		return nil, gorm.ErrRecordNotFound
	}

	var payments []models.Payment
	if err := db.Model(&models.Payment{}).
		Select([]string{
			"id",
			"owner_user_id",
			"is_draft",
			"trip_id",
			"trip_assignment_source",
			"trip_assignment_state",
			"bad_debt",
			"amount",
			"amount_cents",
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
		}).
		Where("owner_user_id = ?", ownerUserID).
		Where("trip_id = ?", tripID).
		Where("is_draft = 0").
		Order("transaction_time_ts DESC").
		Find(&payments).Error; err != nil {
		return nil, err
	}
	if len(payments) == 0 {
		return []TripPaymentWithInvoices{}, nil
	}

	out := make([]TripPaymentWithInvoices, 0, len(payments))
	paymentIDs := make([]string, 0, len(payments))
	for _, p := range payments {
		paymentIDs = append(paymentIDs, p.ID)
		out = append(out, TripPaymentWithInvoices{Payment: p})
	}
	if !includeInvoices {
		return out, nil
	}

	type linkRow struct {
		PaymentID string
		InvoiceID string
	}
	var links []linkRow
	if err := db.
		Table("invoice_payment_links").
		Select("payment_id, invoice_id").
		Where("payment_id IN ?", paymentIDs).
		Scan(&links).Error; err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return out, nil
	}

	invoiceIDsSet := make(map[string]struct{}, len(links))
	byPayment := make(map[string][]string)
	for _, l := range links {
		invoiceIDsSet[l.InvoiceID] = struct{}{}
		byPayment[l.PaymentID] = append(byPayment[l.PaymentID], l.InvoiceID)
	}
	invoiceIDs := make([]string, 0, len(invoiceIDsSet))
	for id := range invoiceIDsSet {
		invoiceIDs = append(invoiceIDs, id)
	}

	var invoices []models.Invoice
	if err := db.Model(&models.Invoice{}).
		Select([]string{
			"id",
			"invoice_number",
			"invoice_date",
			"amount",
			"amount_cents",
			"seller_name",
			"bad_debt",
		}).
		Where("owner_user_id = ?", ownerUserID).
		Where("id IN ?", invoiceIDs).
		Where("is_draft = 0").
		Find(&invoices).Error; err != nil {
		return nil, err
	}
	invByID := make(map[string]models.Invoice, len(invoices))
	for _, inv := range invoices {
		invByID[inv.ID] = inv
	}

	for i := range out {
		pid := out[i].ID
		for _, invID := range byPayment[pid] {
			if inv, ok := invByID[invID]; ok {
				out[i].Invoices = append(out[i].Invoices, TripPaymentInvoice{
					ID:            inv.ID,
					InvoiceNumber: inv.InvoiceNumber,
					InvoiceDate:   inv.InvoiceDate,
					Amount:        inv.Amount,
					SellerName:    inv.SellerName,
					BadDebt:       inv.BadDebt,
				})
			}
		}
	}

	return out, nil
}

type CascadePreview struct {
	TripID       string `json:"trip_id"`
	Payments     int    `json:"payments"`
	Invoices     int    `json:"invoices"`
	UnlinkedOnly int    `json:"unlinked_only"`
}

type DeleteTripOptions struct {
	DeletePayments bool
}

func (s *TripService) GetCascadePreview(ownerUserID string, tripID string) (*CascadePreview, []string, []string, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	return s.GetCascadePreviewCtx(context.Background(), ownerUserID, tripID)
}

func (s *TripService) GetCascadePreviewCtx(ctx context.Context, ownerUserID string, tripID string) (*CascadePreview, []string, []string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	db := database.GetDB().WithContext(ctx)
	ownerUserID = strings.TrimSpace(ownerUserID)
	tripID = strings.TrimSpace(tripID)
	if ownerUserID == "" || tripID == "" {
		return nil, nil, nil, gorm.ErrRecordNotFound
	}

	var payments []models.Payment
	if err := db.Model(&models.Payment{}).
		Select([]string{"id", "screenshot_path"}).
		Where("owner_user_id = ?", ownerUserID).
		Where("trip_id = ?", tripID).
		Where("is_draft = 0").
		Find(&payments).Error; err != nil {
		return nil, nil, nil, err
	}
	paymentIDs := make([]string, 0, len(payments))
	var screenshotPaths []string
	for _, p := range payments {
		paymentIDs = append(paymentIDs, p.ID)
		if p.ScreenshotPath != nil && strings.TrimSpace(*p.ScreenshotPath) != "" {
			screenshotPaths = append(screenshotPaths, strings.TrimSpace(*p.ScreenshotPath))
		}
	}

	preview := &CascadePreview{TripID: tripID, Payments: len(payments)}
	if len(paymentIDs) == 0 {
		return preview, screenshotPaths, nil, nil
	}

	var invoiceIDs []string
	if err := db.
		Table("invoice_payment_links").
		Distinct("invoice_id").
		Where("payment_id IN ?", paymentIDs).
		Pluck("invoice_id", &invoiceIDs).Error; err != nil {
		return nil, nil, nil, err
	}
	preview.Invoices = len(invoiceIDs)

	// Determine which invoices become unlinked after removing these payments.
	remaining := make(map[string]struct{})
	if len(invoiceIDs) > 0 {
		var stillLinked []string
		if err := db.
			Table("invoice_payment_links").
			Distinct("invoice_id").
			Where("invoice_id IN ? AND payment_id NOT IN ?", invoiceIDs, paymentIDs).
			Pluck("invoice_id", &stillLinked).Error; err != nil {
			return nil, nil, nil, err
		}
		for _, id := range stillLinked {
			remaining[id] = struct{}{}
		}
	}

	toDelete := make([]string, 0, len(invoiceIDs))
	for _, invID := range invoiceIDs {
		if _, ok := remaining[invID]; !ok {
			toDelete = append(toDelete, invID)
		}
	}
	preview.UnlinkedOnly = len(toDelete)

	var invoicePaths []string
	if len(toDelete) > 0 {
		type invRow struct {
			FilePath string
		}
		var rows []invRow
		if err := db.Model(&models.Invoice{}).Select("file_path").
			Where("owner_user_id = ?", ownerUserID).
			Where("id IN ?", toDelete).
			Scan(&rows).Error; err != nil {
			return nil, nil, nil, err
		}
		for _, r := range rows {
			if strings.TrimSpace(r.FilePath) != "" {
				invoicePaths = append(invoicePaths, strings.TrimSpace(r.FilePath))
			}
		}
	}
	return preview, screenshotPaths, invoicePaths, nil
}

func (s *TripService) DeleteCascade(ownerUserID string, tripID string) (*CascadePreview, error) {
	return s.DeleteWithOptions(ownerUserID, tripID, DeleteTripOptions{
		DeletePayments: true,
	})
}

func (s *TripService) DeleteWithOptions(ownerUserID string, tripID string, opts DeleteTripOptions) (*CascadePreview, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	// Build preview and file delete lists first.
	preview, screenshotPaths, invoicePaths, err := s.GetCascadePreview(ownerUserID, tripID)
	if err != nil {
		return nil, err
	}
	if !opts.DeletePayments {
		screenshotPaths = nil
		invoicePaths = nil
	}

	db := database.GetDB()
	var rangeStartTs int64
	var rangeEndTs int64
	var affectedTripIDs []string

	// Transaction for DB operations.
	err = db.Transaction(func(tx *gorm.DB) error {
		// Ensure trip exists.
		var trip models.Trip
		if err := tx.Where("id = ? AND owner_user_id = ?", tripID, ownerUserID).First(&trip).Error; err != nil {
			return err
		}
		if locked, err := isTripBadDebtLockedTx(tx, tripID); err != nil {
			return err
		} else if locked {
			return ErrTripBadDebtLocked
		}
		rangeStartTs = trip.StartTimeTs
		rangeEndTs = trip.EndTimeTs

		if opts.DeletePayments {
			// Collect payment IDs.
			var paymentIDs []string
			if err := tx.Model(&models.Payment{}).
				Where("owner_user_id = ?", ownerUserID).
				Where("trip_id = ?", tripID).
				Where("is_draft = 0").
				Pluck("id", &paymentIDs).Error; err != nil {
				return err
			}

			if len(paymentIDs) > 0 {
				// Invoices linked to these payments.
				var invoiceIDs []string
				if err := tx.Table("invoice_payment_links").
					Distinct("invoice_id").
					Where("payment_id IN ?", paymentIDs).
					Pluck("invoice_id", &invoiceIDs).Error; err != nil {
					return err
				}

				toDelete := make(map[string]struct{})
				if len(invoiceIDs) > 0 {
					remaining := make(map[string]struct{})
					var stillLinked []string
					if err := tx.
						Table("invoice_payment_links").
						Distinct("invoice_id").
						Where("invoice_id IN ? AND payment_id NOT IN ?", invoiceIDs, paymentIDs).
						Pluck("invoice_id", &stillLinked).Error; err != nil {
						return err
					}
					for _, id := range stillLinked {
						remaining[id] = struct{}{}
					}
					for _, invID := range invoiceIDs {
						if _, ok := remaining[invID]; !ok {
							toDelete[invID] = struct{}{}
						}
					}
				}

				if len(invoiceIDs) > 0 {
					// Unlink invoices from payments being deleted.
					if err := tx.
						Table("invoice_payment_links").
						Where("invoice_id IN ? AND payment_id IN ?", invoiceIDs, paymentIDs).
						Delete(&models.InvoicePaymentLink{}).Error; err != nil {
						return err
					}
					// Clear legacy payment_id pointers if they reference deleted payments.
					if err := tx.Model(&models.Invoice{}).
						Where("id IN ? AND payment_id IN ?", invoiceIDs, paymentIDs).
						Update("payment_id", nil).Error; err != nil {
						return err
					}
				}

				// Optionally delete invoices that become unlinked.
				if len(toDelete) > 0 {
					toDeleteIDs := make([]string, 0, len(toDelete))
					for id := range toDelete {
						toDeleteIDs = append(toDeleteIDs, id)
					}
					if err := tx.Where("owner_user_id = ? AND id IN ?", ownerUserID, toDeleteIDs).Delete(&models.Invoice{}).Error; err != nil {
						return err
					}
				}

				// Delete payments.
				if err := tx.Where("owner_user_id = ? AND id IN ?", ownerUserID, paymentIDs).Delete(&models.Payment{}).Error; err != nil {
					return err
				}
			}
		} else {
			// Keep payments: move them into a reviewable unassigned state (so UI routes them to "pending").
			if err := tx.Model(&models.Payment{}).
				Where("owner_user_id = ?", ownerUserID).
				Where("trip_id = ?", tripID).
				Where("is_draft = 0").
				Updates(map[string]interface{}{
					"trip_id":                nil,
					"trip_assignment_source": assignSrcManual,
					"trip_assignment_state":  assignStateNoMatch,
				}).Error; err != nil {
				return err
			}
		}

		// Delete trip itself.
		if err := tx.Where("id = ? AND owner_user_id = ?", tripID, ownerUserID).Delete(&models.Trip{}).Error; err != nil {
			return err
		}

		if opts.DeletePayments {
			// Trip removal may resolve overlaps; re-evaluate auto assignments in this range.
			if rangeEndTs > rangeStartTs {
				if _, tripIDs, err := recomputeAutoAssignmentsForRangeTx(tx, strings.TrimSpace(ownerUserID), rangeStartTs, rangeEndTs); err != nil {
					return err
				} else {
					affectedTripIDs = tripIDs
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	_ = recalcTripBadDebtLockedForTripIDs(affectedTripIDs)

	// Best-effort file cleanup after DB commit.
	for _, p := range screenshotPaths {
		_ = os.Remove(resolveUploadsPath(s.uploadsDir, p))
	}
	for _, p := range invoicePaths {
		_ = os.Remove(resolveUploadsPath(s.uploadsDir, p))
	}

	return preview, nil
}

func resolveUploadsPath(uploadsDir, storedPath string) string {
	uploadsDir = strings.TrimSpace(uploadsDir)
	p := strings.TrimSpace(storedPath)
	if p == "" {
		return ""
	}
	// storedPath typically is "uploads/<file>".
	p = strings.TrimPrefix(p, "uploads/")
	p = strings.TrimPrefix(p, "/uploads/")
	p = strings.TrimPrefix(p, "uploads\\")
	return filepath.Join(uploadsDir, filepath.FromSlash(p))
}

func validateRFC3339Range(start, end string) error {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start == "" || end == "" {
		return fmt.Errorf("start_time and end_time are required")
	}
	st, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return fmt.Errorf("start_time must be RFC3339: %w", err)
	}
	et, err := time.Parse(time.RFC3339Nano, end)
	if err != nil {
		return fmt.Errorf("end_time must be RFC3339: %w", err)
	}
	if et.Before(st) {
		return fmt.Errorf("end_time must be >= start_time")
	}
	return nil
}
