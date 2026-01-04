package services

import (
	"math"
	"strings"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
)

func FindPaymentByFileSHA256ForOwner(ownerUserID string, hash string, excludeID string) (*models.Payment, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}

	q := database.GetDB().Model(&models.Payment{}).
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

func FindPaymentByFileSHA256(hash string, excludeID string) (*models.Payment, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}
	q := database.GetDB().Model(&models.Payment{}).
		Where("file_sha256 = ?", hash)
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

func FindInvoiceByFileSHA256ForOwner(ownerUserID string, hash string, excludeID string) (*models.Invoice, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}

	q := database.GetDB().Model(&models.Invoice{}).
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

func FindInvoiceByFileSHA256(hash string, excludeID string) (*models.Invoice, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}
	q := database.GetDB().Model(&models.Invoice{}).
		Where("file_sha256 = ?", hash)
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

func FindPaymentCandidatesByAmountTimeForOwner(ownerUserID string, amount float64, transactionTimeTs int64, excludeID string, window time.Duration, limit int) ([]DedupCandidate, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if amount <= 0 || transactionTimeTs <= 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	eps := 0.01
	minAmount := amount - eps
	maxAmount := amount + eps
	if minAmount < 0 {
		minAmount = 0
	}

	deltaMs := int64(window / time.Millisecond)
	startTs := transactionTimeTs - deltaMs
	endTs := transactionTimeTs + deltaMs

	var rows []models.Payment
	q := database.GetDB().Model(&models.Payment{}).
		Where("is_draft = 0").
		Where("transaction_time_ts BETWEEN ? AND ?", startTs, endTs).
		Where("amount BETWEEN ? AND ?", minAmount, maxAmount)
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

func FindPaymentCandidatesByAmountTime(amount float64, transactionTimeTs int64, excludeID string, window time.Duration, limit int) ([]DedupCandidate, error) {
	if amount <= 0 || transactionTimeTs <= 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	// Amount is stored as float64; use a small epsilon around 2-decimal money values.
	eps := 0.01
	minAmount := amount - eps
	maxAmount := amount + eps
	if minAmount < 0 {
		minAmount = 0
	}

	deltaMs := int64(window / time.Millisecond)
	startTs := transactionTimeTs - deltaMs
	endTs := transactionTimeTs + deltaMs

	var rows []models.Payment
	q := database.GetDB().Model(&models.Payment{}).
		Where("is_draft = 0").
		Where("transaction_time_ts BETWEEN ? AND ?", startTs, endTs).
		Where("amount BETWEEN ? AND ?", minAmount, maxAmount)
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

func FindInvoiceCandidatesByInvoiceNumberForOwner(ownerUserID string, invoiceNumber string, excludeID string, limit int) ([]DedupCandidate, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	invoiceNumber = strings.TrimSpace(invoiceNumber)
	if invoiceNumber == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	var rows []models.Invoice
	q := database.GetDB().Model(&models.Invoice{}).
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

func FindInvoiceCandidatesByInvoiceNumber(invoiceNumber string, excludeID string, limit int) ([]DedupCandidate, error) {
	invoiceNumber = strings.TrimSpace(invoiceNumber)
	if invoiceNumber == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	var rows []models.Invoice
	q := database.GetDB().Model(&models.Invoice{}).
		Where("is_draft = 0").
		Where("invoice_number = ?", invoiceNumber)
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
