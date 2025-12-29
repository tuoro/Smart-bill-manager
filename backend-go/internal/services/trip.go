package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"smart-bill-manager/internal/models"
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

func NewTripService(uploadsDir string) *TripService {
	return &TripService{
		repo:        repository.NewTripRepository(),
		paymentRepo: repository.NewPaymentRepository(),
		uploadsDir:  uploadsDir,
	}
}

type CreateTripInput struct {
	Name      string  `json:"name" binding:"required"`
	StartTime string  `json:"start_time" binding:"required"`
	EndTime   string  `json:"end_time" binding:"required"`
	// unreimbursed|reimbursed (optional; defaults to unreimbursed)
	ReimburseStatus *string `json:"reimburse_status"`
	Note      *string `json:"note"`
}

func (s *TripService) Create(input CreateTripInput) (*models.Trip, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	if err := validateRFC3339Range(input.StartTime, input.EndTime); err != nil {
		return nil, err
	}

	reimburseStatus := "unreimbursed"
	if input.ReimburseStatus != nil {
		reimburseStatus = strings.TrimSpace(*input.ReimburseStatus)
	}
	if reimburseStatus == "" {
		reimburseStatus = "unreimbursed"
	}
	if reimburseStatus != "unreimbursed" && reimburseStatus != "reimbursed" {
		return nil, fmt.Errorf("invalid reimburse_status")
	}

	trip := &models.Trip{
		ID:              utils.GenerateUUID(),
		Name:            strings.TrimSpace(input.Name),
		StartTime:        strings.TrimSpace(input.StartTime),
		EndTime:          strings.TrimSpace(input.EndTime),
		ReimburseStatus: reimburseStatus,
		Note:            input.Note,
	}

	if err := s.repo.Create(trip); err != nil {
		return nil, err
	}
	return trip, nil
}

func (s *TripService) GetAll() ([]models.Trip, error) {
	return s.repo.FindAll()
}

func (s *TripService) GetByID(id string) (*models.Trip, error) {
	return s.repo.FindByID(id)
}

type UpdateTripInput struct {
	Name      *string `json:"name"`
	StartTime *string `json:"start_time"`
	EndTime   *string `json:"end_time"`
	// unreimbursed|reimbursed
	ReimburseStatus *string `json:"reimburse_status"`
	Note      *string `json:"note"`
}

func (s *TripService) Update(id string, input UpdateTripInput) error {
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
		data["start_time"] = start
	}
	if input.EndTime != nil {
		end = strings.TrimSpace(*input.EndTime)
		data["end_time"] = end
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

	if (input.StartTime != nil || input.EndTime != nil) && (start == "" || end == "") {
		trip, err := s.repo.FindByID(id)
		if err != nil {
			return err
		}
		if start == "" {
			start = trip.StartTime
		}
		if end == "" {
			end = trip.EndTime
		}
	}
	if start != "" || end != "" {
		if err := validateRFC3339Range(start, end); err != nil {
			return err
		}
	}

	if len(data) == 0 {
		return nil
	}
	return s.repo.Update(id, data)
}

type TripSummary struct {
	TripID         string  `json:"trip_id"`
	PaymentCount   int     `json:"payment_count"`
	TotalAmount    float64 `json:"total_amount"`
	LinkedInvoices int     `json:"linked_invoices"`
	UnlinkedPays   int     `json:"unlinked_payments"`
}

func (s *TripService) GetSummary(tripID string) (*TripSummary, error) {
	db := database.GetDB()

	var payments []models.Payment
	if err := db.Model(&models.Payment{}).Where("trip_id = ?", tripID).Find(&payments).Error; err != nil {
		return nil, err
	}

	paymentIDs := make([]string, 0, len(payments))
	out := &TripSummary{TripID: tripID}
	for _, p := range payments {
		out.PaymentCount++
		out.TotalAmount += p.Amount
		paymentIDs = append(paymentIDs, p.ID)
	}
	if len(paymentIDs) == 0 {
		return out, nil
	}

	// Count distinct invoices linked to these payments.
	var invoiceCount int64
	if err := db.
		Table("invoice_payment_links").
		Where("payment_id IN ?", paymentIDs).
		Distinct("invoice_id").
		Count(&invoiceCount).Error; err != nil {
		return nil, err
	}
	out.LinkedInvoices = int(invoiceCount)

	// Count payments with no linked invoices.
	type row struct {
		PaymentID string
		Cnt       int64
	}
	var rows []row
	if err := db.
		Table("invoice_payment_links").
		Select("payment_id as payment_id, COUNT(*) as cnt").
		Where("payment_id IN ?", paymentIDs).
		Group("payment_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	hasLink := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if r.Cnt > 0 {
			hasLink[r.PaymentID] = struct{}{}
		}
	}
	for _, pid := range paymentIDs {
		if _, ok := hasLink[pid]; !ok {
			out.UnlinkedPays++
		}
	}
	return out, nil
}

type AssignByTimePreview struct {
	TripID              string   `json:"trip_id"`
	MatchedPayments     int      `json:"matched_payments"`
	WillAssign          int      `json:"will_assign"`
	AlreadyInThisTrip   int      `json:"already_in_this_trip"`
	AssignedOtherTrip   int      `json:"assigned_other_trip"`
	SkippedOtherTripIDs []string `json:"skipped_other_trip_ids,omitempty"`
}

type AssignByTimeInput struct {
	DryRun bool `json:"dry_run"`
}

func (s *TripService) AssignPaymentsByTime(tripID string, input AssignByTimeInput) (*AssignByTimePreview, error) {
	trip, err := s.repo.FindByID(tripID)
	if err != nil {
		return nil, err
	}
	if err := validateRFC3339Range(trip.StartTime, trip.EndTime); err != nil {
		return nil, err
	}

	db := database.GetDB()
	var payments []models.Payment
	if err := db.
		Model(&models.Payment{}).
		Where("transaction_time >= ? AND transaction_time <= ?", trip.StartTime, trip.EndTime).
		Find(&payments).Error; err != nil {
		return nil, err
	}

	preview := &AssignByTimePreview{TripID: tripID}
	skippedOther := make(map[string]struct{})

	for _, p := range payments {
		preview.MatchedPayments++
		if p.TripID == nil || strings.TrimSpace(*p.TripID) == "" {
			preview.WillAssign++
			continue
		}
		if strings.TrimSpace(*p.TripID) == tripID {
			preview.AlreadyInThisTrip++
			continue
		}
		preview.AssignedOtherTrip++
		skippedOther[strings.TrimSpace(*p.TripID)] = struct{}{}
	}
	for id := range skippedOther {
		preview.SkippedOtherTripIDs = append(preview.SkippedOtherTripIDs, id)
	}

	if input.DryRun || preview.WillAssign == 0 {
		return preview, nil
	}

	// Assign only currently unassigned payments (safe default).
	if err := db.
		Model(&models.Payment{}).
		Where("(trip_id IS NULL OR TRIM(trip_id) = '') AND transaction_time >= ? AND transaction_time <= ?", trip.StartTime, trip.EndTime).
		Updates(map[string]interface{}{"trip_id": tripID}).Error; err != nil {
		return nil, err
	}

	if err := recalcTripBadDebtLocked(tripID); err != nil {
		return nil, err
	}

	return preview, nil
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

func (s *TripService) GetPayments(tripID string, includeInvoices bool) ([]TripPaymentWithInvoices, error) {
	db := database.GetDB()

	var payments []models.Payment
	if err := db.Model(&models.Payment{}).Where("trip_id = ?", tripID).Order("transaction_time DESC").Find(&payments).Error; err != nil {
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
	if err := db.Model(&models.Invoice{}).Where("id IN ?", invoiceIDs).Find(&invoices).Error; err != nil {
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

func (s *TripService) GetCascadePreview(tripID string) (*CascadePreview, []string, []string, error) {
	db := database.GetDB()

	var payments []models.Payment
	if err := db.Model(&models.Payment{}).Where("trip_id = ?", tripID).Find(&payments).Error; err != nil {
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
	toDelete := make([]string, 0)
	for _, invID := range invoiceIDs {
		var count int64
		if err := db.
			Table("invoice_payment_links").
			Where("invoice_id = ? AND payment_id NOT IN ?", invID, paymentIDs).
			Count(&count).Error; err != nil {
			return nil, nil, nil, err
		}
		if count == 0 {
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
		if err := db.Model(&models.Invoice{}).Select("file_path").Where("id IN ?", toDelete).Scan(&rows).Error; err != nil {
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

func (s *TripService) DeleteCascade(tripID string) (*CascadePreview, error) {
	// Build preview and file delete lists first.
	preview, screenshotPaths, invoicePaths, err := s.GetCascadePreview(tripID)
	if err != nil {
		return nil, err
	}

	db := database.GetDB()

	// Transaction for DB operations.
	err = db.Transaction(func(tx *gorm.DB) error {
		// Ensure trip exists.
		var trip models.Trip
		if err := tx.Where("id = ?", tripID).First(&trip).Error; err != nil {
			return err
		}

		// Collect payment IDs.
		var paymentIDs []string
		if err := tx.Model(&models.Payment{}).Where("trip_id = ?", tripID).Pluck("id", &paymentIDs).Error; err != nil {
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
			for _, invID := range invoiceIDs {
				var count int64
				if err := tx.Table("invoice_payment_links").
					Where("invoice_id = ? AND payment_id NOT IN ?", invID, paymentIDs).
					Count(&count).Error; err != nil {
					return err
				}
				if count == 0 {
					toDelete[invID] = struct{}{}
				}
			}

			// Unlink kept invoices from payments being deleted.
			if len(invoiceIDs) > 0 {
				keep := make([]string, 0)
				for _, invID := range invoiceIDs {
					if _, ok := toDelete[invID]; !ok {
						keep = append(keep, invID)
					}
				}
				if len(keep) > 0 {
					if err := tx.
						Table("invoice_payment_links").
						Where("invoice_id IN ? AND payment_id IN ?", keep, paymentIDs).
						Delete(&models.InvoicePaymentLink{}).Error; err != nil {
						return err
					}
					// Clear legacy payment_id pointers if they reference deleted payments.
					if err := tx.Model(&models.Invoice{}).
						Where("id IN ? AND payment_id IN ?", keep, paymentIDs).
						Update("payment_id", nil).Error; err != nil {
						return err
					}
				}
			}

			// Delete invoices that become unlinked.
			if len(toDelete) > 0 {
				toDeleteIDs := make([]string, 0, len(toDelete))
				for id := range toDelete {
					toDeleteIDs = append(toDeleteIDs, id)
				}
				if err := tx.Table("invoice_payment_links").
					Where("invoice_id IN ?", toDeleteIDs).
					Delete(&models.InvoicePaymentLink{}).Error; err != nil {
					return err
				}
				if err := tx.Where("id IN ?", toDeleteIDs).Delete(&models.Invoice{}).Error; err != nil {
					return err
				}
			}

			// Delete payments.
			if err := tx.Where("id IN ?", paymentIDs).Delete(&models.Payment{}).Error; err != nil {
				return err
			}
		}

		// Delete trip itself.
		if err := tx.Where("id = ?", tripID).Delete(&models.Trip{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

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
	st, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return fmt.Errorf("start_time must be RFC3339: %w", err)
	}
	et, err := time.Parse(time.RFC3339, end)
	if err != nil {
		return fmt.Errorf("end_time must be RFC3339: %w", err)
	}
	if et.Before(st) {
		return fmt.Errorf("end_time must be >= start_time")
	}
	return nil
}
