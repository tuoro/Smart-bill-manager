package migrations

import (
	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

// migrateSchema 负责创建当前版本所需的表和列，数据变换由版本化迁移处理。
func migrateSchema(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.User{},
		&models.Invite{},
		&models.Task{},
		&models.Payment{},
		&models.Trip{},
		&models.Invoice{},
		&models.InvoiceAttachment{},
		&models.InvoiceOCRBlob{},
		&models.PaymentOCRBlob{},
		&models.InvoicePaymentLink{},
		&models.EmailConfig{},
		&models.EmailLog{},
	)
}
