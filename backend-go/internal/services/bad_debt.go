package services

import (
	"errors"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

var ErrTripBadDebtLocked = errors.New("trip is bad debt locked")

func isTripBadDebtLockedTx(tx *gorm.DB, tripID string) (bool, error) {
	tripID = strings.TrimSpace(tripID)
	if tripID == "" {
		return false, nil
	}
	if tx == nil {
		return false, errors.New("tx is required")
	}

	var paymentBadDebt int64
	if err := tx.Model(&models.Payment{}).
		Where("trip_id = ? AND bad_debt = ? AND is_draft = 0", tripID, true).
		Count(&paymentBadDebt).Error; err != nil {
		return false, err
	}
	if paymentBadDebt > 0 {
		return true, nil
	}

	var invoiceBadDebtViaLinks int64
	if err := tx.
		Table("invoices").
		Joins("JOIN invoice_payment_links ON invoice_payment_links.invoice_id = invoices.id").
		Joins("JOIN payments ON payments.id = invoice_payment_links.payment_id").
		Where("payments.trip_id = ? AND invoices.bad_debt = ? AND payments.is_draft = 0 AND invoices.is_draft = 0", tripID, true).
		Distinct("invoices.id").
		Count(&invoiceBadDebtViaLinks).Error; err != nil {
		return false, err
	}
	if invoiceBadDebtViaLinks > 0 {
		return true, nil
	}

	var invoiceBadDebtViaLegacy int64
	if err := tx.
		Table("invoices").
		Joins("JOIN payments ON payments.id = invoices.payment_id").
		Where("payments.trip_id = ? AND invoices.bad_debt = ? AND payments.is_draft = 0 AND invoices.is_draft = 0", tripID, true).
		Distinct("invoices.id").
		Count(&invoiceBadDebtViaLegacy).Error; err != nil {
		return false, err
	}

	return invoiceBadDebtViaLegacy > 0, nil
}

func recalcTripBadDebtLocked(tripID string) error {
	tripID = strings.TrimSpace(tripID)
	if tripID == "" {
		return nil
	}

	db := database.GetDB()

	var paymentBadDebt int64
	if err := db.Model(&models.Payment{}).
		Where("trip_id = ? AND bad_debt = ? AND is_draft = 0", tripID, true).
		Count(&paymentBadDebt).Error; err != nil {
		return err
	}

	var invoiceBadDebtViaLinks int64
	if err := db.
		Table("invoices").
		Joins("JOIN invoice_payment_links ON invoice_payment_links.invoice_id = invoices.id").
		Joins("JOIN payments ON payments.id = invoice_payment_links.payment_id").
		Where("payments.trip_id = ? AND invoices.bad_debt = ? AND payments.is_draft = 0 AND invoices.is_draft = 0", tripID, true).
		Distinct("invoices.id").
		Count(&invoiceBadDebtViaLinks).Error; err != nil {
		return err
	}

	var invoiceBadDebtViaLegacy int64
	if err := db.
		Table("invoices").
		Joins("JOIN payments ON payments.id = invoices.payment_id").
		Where("payments.trip_id = ? AND invoices.bad_debt = ? AND payments.is_draft = 0 AND invoices.is_draft = 0", tripID, true).
		Distinct("invoices.id").
		Count(&invoiceBadDebtViaLegacy).Error; err != nil {
		return err
	}

	locked := paymentBadDebt > 0 || invoiceBadDebtViaLinks > 0 || invoiceBadDebtViaLegacy > 0

	return db.Model(&models.Trip{}).Where("id = ?", tripID).Update("bad_debt_locked", locked).Error
}

func recalcTripBadDebtLockedForTripIDs(tripIDs []string) error {
	unique := make(map[string]struct{}, len(tripIDs))
	for _, id := range tripIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		unique[id] = struct{}{}
	}

	for id := range unique {
		if err := recalcTripBadDebtLocked(id); err != nil {
			return err
		}
	}
	return nil
}

func getTripIDForPayment(paymentID string) (string, error) {
	paymentID = strings.TrimSpace(paymentID)
	if paymentID == "" {
		return "", nil
	}

	db := database.GetDB()
	var payment models.Payment
	if err := db.Select("trip_id").Where("id = ?", paymentID).First(&payment).Error; err != nil {
		return "", err
	}
	if payment.TripID == nil || strings.TrimSpace(*payment.TripID) == "" {
		return "", nil
	}
	return strings.TrimSpace(*payment.TripID), nil
}
