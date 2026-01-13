package repository

import (
	"context"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

type InvoiceAttachmentRepository struct{}

func NewInvoiceAttachmentRepository() *InvoiceAttachmentRepository {
	return &InvoiceAttachmentRepository{}
}

func (r *InvoiceAttachmentRepository) CreateCtx(ctx context.Context, a *models.InvoiceAttachment) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil {
		return gorm.ErrInvalidData
	}
	return database.GetDB().WithContext(ctx).Create(a).Error
}

func (r *InvoiceAttachmentRepository) FindByInvoiceIDForOwnerCtx(ctx context.Context, ownerUserID string, invoiceID string) ([]models.InvoiceAttachment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	invoiceID = strings.TrimSpace(invoiceID)
	if ownerUserID == "" || invoiceID == "" {
		return []models.InvoiceAttachment{}, nil
	}
	var rows []models.InvoiceAttachment
	err := database.GetDB().WithContext(ctx).
		Model(&models.InvoiceAttachment{}).
		Where("owner_user_id = ? AND invoice_id = ?", ownerUserID, invoiceID).
		Order("created_at ASC, id ASC").
		Find(&rows).Error
	return rows, err
}

func (r *InvoiceAttachmentRepository) FindByIDForOwnerCtx(ctx context.Context, ownerUserID string, id string) (*models.InvoiceAttachment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	id = strings.TrimSpace(id)
	if ownerUserID == "" || id == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var row models.InvoiceAttachment
	if err := database.GetDB().WithContext(ctx).
		Where("owner_user_id = ? AND id = ?", ownerUserID, id).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *InvoiceAttachmentRepository) DeleteByInvoiceIDForOwnerCtx(ctx context.Context, ownerUserID string, invoiceID string) (deleted int64, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	invoiceID = strings.TrimSpace(invoiceID)
	if ownerUserID == "" || invoiceID == "" {
		return 0, nil
	}
	res := database.GetDB().WithContext(ctx).
		Where("owner_user_id = ? AND invoice_id = ?", ownerUserID, invoiceID).
		Delete(&models.InvoiceAttachment{})
	return res.RowsAffected, res.Error
}

func (r *InvoiceAttachmentRepository) DeleteByIDForOwnerCtx(ctx context.Context, ownerUserID string, id string) (deleted int64, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	id = strings.TrimSpace(id)
	if ownerUserID == "" || id == "" {
		return 0, nil
	}
	res := database.GetDB().WithContext(ctx).
		Where("owner_user_id = ? AND id = ?", ownerUserID, id).
		Delete(&models.InvoiceAttachment{})
	return res.RowsAffected, res.Error
}
