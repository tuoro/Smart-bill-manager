package repository

import (
	"context"
	"fmt"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OCRBlobRepository struct{}

func NewOCRBlobRepository() *OCRBlobRepository {
	return &OCRBlobRepository{}
}

func (r *OCRBlobRepository) UpsertInvoiceBlob(tx *gorm.DB, ownerUserID, invoiceID string, extractedData, rawText *string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	invoiceID = strings.TrimSpace(invoiceID)
	if ownerUserID == "" || invoiceID == "" {
		return fmt.Errorf("missing fields")
	}
	db := tx
	if db == nil {
		db = database.GetDB()
	}

	row := &models.InvoiceOCRBlob{
		InvoiceID:     invoiceID,
		OwnerUserID:   ownerUserID,
		ExtractedData: extractedData,
		RawText:       rawText,
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "invoice_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"owner_user_id", "extracted_data", "raw_text", "updated_at"}),
	}).Create(row).Error
}

func (r *OCRBlobRepository) FindInvoiceBlob(ownerUserID, invoiceID string) (*models.InvoiceOCRBlob, error) {
	return r.FindInvoiceBlobCtx(context.Background(), ownerUserID, invoiceID)
}

func (r *OCRBlobRepository) FindInvoiceBlobCtx(ctx context.Context, ownerUserID, invoiceID string) (*models.InvoiceOCRBlob, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	invoiceID = strings.TrimSpace(invoiceID)
	if ownerUserID == "" || invoiceID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var row models.InvoiceOCRBlob
	if ctx == nil {
		ctx = context.Background()
	}
	if err := database.GetDB().WithContext(ctx).
		Where("invoice_id = ? AND owner_user_id = ?", invoiceID, ownerUserID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *OCRBlobRepository) DeleteInvoiceBlob(tx *gorm.DB, ownerUserID, invoiceID string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	invoiceID = strings.TrimSpace(invoiceID)
	if ownerUserID == "" || invoiceID == "" {
		return gorm.ErrRecordNotFound
	}
	db := tx
	if db == nil {
		db = database.GetDB()
	}
	return db.Where("invoice_id = ? AND owner_user_id = ?", invoiceID, ownerUserID).Delete(&models.InvoiceOCRBlob{}).Error
}

func (r *OCRBlobRepository) UpsertPaymentBlob(tx *gorm.DB, ownerUserID, paymentID string, extractedData *string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	paymentID = strings.TrimSpace(paymentID)
	if ownerUserID == "" || paymentID == "" {
		return fmt.Errorf("missing fields")
	}
	db := tx
	if db == nil {
		db = database.GetDB()
	}

	row := &models.PaymentOCRBlob{
		PaymentID:     paymentID,
		OwnerUserID:   ownerUserID,
		ExtractedData: extractedData,
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "payment_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"owner_user_id", "extracted_data", "updated_at"}),
	}).Create(row).Error
}

func (r *OCRBlobRepository) FindPaymentBlob(ownerUserID, paymentID string) (*models.PaymentOCRBlob, error) {
	return r.FindPaymentBlobCtx(context.Background(), ownerUserID, paymentID)
}

func (r *OCRBlobRepository) FindPaymentBlobCtx(ctx context.Context, ownerUserID, paymentID string) (*models.PaymentOCRBlob, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	paymentID = strings.TrimSpace(paymentID)
	if ownerUserID == "" || paymentID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var row models.PaymentOCRBlob
	if ctx == nil {
		ctx = context.Background()
	}
	if err := database.GetDB().WithContext(ctx).
		Where("payment_id = ? AND owner_user_id = ?", paymentID, ownerUserID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *OCRBlobRepository) DeletePaymentBlob(tx *gorm.DB, ownerUserID, paymentID string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	paymentID = strings.TrimSpace(paymentID)
	if ownerUserID == "" || paymentID == "" {
		return gorm.ErrRecordNotFound
	}
	db := tx
	if db == nil {
		db = database.GetDB()
	}
	return db.Where("payment_id = ? AND owner_user_id = ?", paymentID, ownerUserID).Delete(&models.PaymentOCRBlob{}).Error
}
