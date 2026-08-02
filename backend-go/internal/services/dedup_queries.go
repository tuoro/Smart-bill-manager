package services

import (
	"math"
	"strings"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/money"

	"gorm.io/gorm"
)

func (s *PaymentService) FindByFileSHA256ForOwner(ownerUserID string, hash string, excludeID string) (*models.Payment, error) {
	return findPaymentByFileSHA256ForOwner(s.db, ownerUserID, hash, excludeID)
}

func findPaymentByFileSHA256ForOwner(db *gorm.DB, ownerUserID string, hash string, excludeID string) (*models.Payment, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}

	q := db.Model(&models.Payment{}).
		Where("file_sha256 = ?", hash)
	if ownerUserID != "" {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	if strings.TrimSpace(excludeID) != "" {
		q = q.Where("id <> ?", strings.TrimSpace(excludeID))
	}

	var p models.Payment
	res := q.Order("is_draft ASC, created_at DESC, id DESC").Limit(1).Find(&p)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &p, nil
}

func (s *InvoiceService) FindByFileSHA256ForOwner(ownerUserID string, hash string, excludeID string) (*models.Invoice, error) {
	return findInvoiceByFileSHA256ForOwner(s.db, ownerUserID, hash, excludeID)
}

func findInvoiceByFileSHA256ForOwner(db *gorm.DB, ownerUserID string, hash string, excludeID string) (*models.Invoice, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}

	q := db.Model(&models.Invoice{}).
		Where("file_sha256 = ?", hash)
	if ownerUserID != "" {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	if strings.TrimSpace(excludeID) != "" {
		q = q.Where("id <> ?", strings.TrimSpace(excludeID))
	}

	var inv models.Invoice
	res := q.Order("is_draft ASC, created_at DESC, id DESC").Limit(1).Find(&inv)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &inv, nil
}

func (s *PaymentService) FindCandidatesByAmountTimeForOwner(ownerUserID string, amount float64, transactionTimeTs int64, excludeID string, window time.Duration, limit int) ([]DedupCandidate, error) {
	return findPaymentCandidatesByAmountTimeForOwner(s.db, ownerUserID, amount, transactionTimeTs, excludeID, window, limit)
}

func findPaymentCandidatesByAmountTimeForOwner(db *gorm.DB, ownerUserID string, amount float64, transactionTimeTs int64, excludeID string, window time.Duration, limit int) ([]DedupCandidate, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if amount <= 0 || transactionTimeTs <= 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	amountCents, err := money.FromMajor(amount)
	if err != nil {
		return nil, err
	}

	deltaMs := int64(window / time.Millisecond)
	startTs := transactionTimeTs - deltaMs
	endTs := transactionTimeTs + deltaMs

	var rows []models.Payment
	q := db.Model(&models.Payment{}).
		Where("is_draft = 0").
		Where("transaction_time_ts BETWEEN ? AND ?", startTs, endTs).
		Where("amount_cents = ?", amountCents)
	if ownerUserID != "" {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	if strings.TrimSpace(excludeID) != "" {
		q = q.Where("id <> ?", strings.TrimSpace(excludeID))
	}
	if err := q.Order("transaction_time_ts DESC, created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]DedupCandidate, 0, len(rows))
	for _, p := range rows {
		amt := math.Abs(p.Amount)
		ts := p.TransactionTime
		out = append(out, DedupCandidate{
			ID:              p.ID,
			IsDraft:         p.IsDraft,
			Amount:          &amt,
			TransactionTime: &ts,
			Merchant:        p.Merchant,
			CreatedAt:       p.CreatedAt,
		})
	}
	return out, nil
}

func (s *InvoiceService) FindCandidatesByInvoiceNumberForOwner(ownerUserID string, invoiceNumber string, excludeID string, limit int) ([]DedupCandidate, error) {
	return findInvoiceCandidatesByInvoiceNumberForOwner(s.db, ownerUserID, invoiceNumber, excludeID, limit)
}

func findInvoiceCandidatesByInvoiceNumberForOwner(db *gorm.DB, ownerUserID string, invoiceNumber string, excludeID string, limit int) ([]DedupCandidate, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	invoiceNumber = strings.TrimSpace(invoiceNumber)
	if invoiceNumber == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	var rows []models.Invoice
	q := db.Model(&models.Invoice{}).
		Where("is_draft = 0").
		Where("invoice_number = ?", invoiceNumber)
	if ownerUserID != "" {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	if strings.TrimSpace(excludeID) != "" {
		q = q.Where("id <> ?", strings.TrimSpace(excludeID))
	}
	if err := q.Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]DedupCandidate, 0, len(rows))
	for _, inv := range rows {
		out = append(out, DedupCandidate{
			ID:            inv.ID,
			IsDraft:       inv.IsDraft,
			Amount:        inv.Amount,
			InvoiceNumber: inv.InvoiceNumber,
			InvoiceDate:   inv.InvoiceDate,
			SellerName:    inv.SellerName,
			CreatedAt:     inv.CreatedAt,
		})
	}
	return out, nil
}
