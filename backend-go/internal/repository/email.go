package repository

import (
	"context"
	"fmt"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

type EmailRepository struct{}

func NewEmailRepository() *EmailRepository {
	return &EmailRepository{}
}

// Email Config methods
func (r *EmailRepository) CreateConfig(config *models.EmailConfig) error {
	return database.GetDB().Create(config).Error
}

func (r *EmailRepository) FindConfigByID(id string) (*models.EmailConfig, error) {
	return r.FindConfigByIDCtx(context.Background(), id)
}

func (r *EmailRepository) FindConfigByIDCtx(ctx context.Context, id string) (*models.EmailConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var config models.EmailConfig
	err := database.GetDB().WithContext(ctx).Where("id = ?", id).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *EmailRepository) FindConfigByIDForOwner(ownerUserID string, id string) (*models.EmailConfig, error) {
	return r.FindConfigByIDForOwnerCtx(context.Background(), ownerUserID, id)
}

func (r *EmailRepository) FindConfigByIDForOwnerCtx(ctx context.Context, ownerUserID string, id string) (*models.EmailConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	id = strings.TrimSpace(id)
	if ownerUserID == "" || id == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var config models.EmailConfig
	err := database.GetDB().WithContext(ctx).Where("id = ? AND owner_user_id = ?", id, ownerUserID).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *EmailRepository) FindAllConfigs(ownerUserID string) ([]models.EmailConfig, error) {
	return r.FindAllConfigsCtx(context.Background(), ownerUserID)
}

func (r *EmailRepository) FindAllConfigsCtx(ctx context.Context, ownerUserID string) ([]models.EmailConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	var configs []models.EmailConfig
	err := database.GetDB().WithContext(ctx).Where("owner_user_id = ?", ownerUserID).Order("created_at DESC").Find(&configs).Error
	return configs, err
}

func (r *EmailRepository) UpdateConfig(id string, data map[string]interface{}) error {
	result := database.GetDB().Model(&models.EmailConfig{}).Where("id = ?", id).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *EmailRepository) UpdateConfigForOwner(ownerUserID string, id string, data map[string]interface{}) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	id = strings.TrimSpace(id)
	result := database.GetDB().Model(&models.EmailConfig{}).Where("id = ? AND owner_user_id = ?", id, ownerUserID).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *EmailRepository) DeleteConfig(id string) error {
	result := database.GetDB().Where("id = ?", id).Delete(&models.EmailConfig{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *EmailRepository) DeleteConfigForOwner(ownerUserID string, id string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	id = strings.TrimSpace(id)
	result := database.GetDB().Where("id = ? AND owner_user_id = ?", id, ownerUserID).Delete(&models.EmailConfig{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *EmailRepository) DeleteLogsByConfigID(configID string) (deleted int64, err error) {
	configID = strings.TrimSpace(configID)
	if configID == "" {
		return 0, fmt.Errorf("missing config id")
	}
	res := database.GetDB().Where("email_config_id = ?", configID).Delete(&models.EmailLog{})
	return res.RowsAffected, res.Error
}

func (r *EmailRepository) DeleteLogsByConfigIDForOwner(ownerUserID string, configID string) (deleted int64, err error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	if ownerUserID == "" || configID == "" {
		return 0, fmt.Errorf("missing fields")
	}
	res := database.GetDB().
		Where("owner_user_id = ? AND email_config_id = ?", ownerUserID, configID).
		Delete(&models.EmailLog{})
	return res.RowsAffected, res.Error
}

func (r *EmailRepository) DeleteConfigForOwnerCascade(ownerUserID string, id string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	id = strings.TrimSpace(id)
	if ownerUserID == "" || id == "" {
		return gorm.ErrRecordNotFound
	}

	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		// Ensure config belongs to owner first.
		var cfg models.EmailConfig
		if err := tx.Select("id").Where("id = ? AND owner_user_id = ?", id, ownerUserID).First(&cfg).Error; err != nil {
			return err
		}

		if err := tx.Where("email_config_id = ?", cfg.ID).Delete(&models.EmailLog{}).Error; err != nil {
			return err
		}

		res := tx.Where("id = ? AND owner_user_id = ?", cfg.ID, ownerUserID).Delete(&models.EmailConfig{})
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return res.Error
	})
}

func (r *EmailRepository) UpdateLastCheck(id, lastCheck string) error {
	return database.GetDB().Model(&models.EmailConfig{}).Where("id = ?", id).Update("last_check", lastCheck).Error
}

func (r *EmailRepository) UpdateLastCheckForOwner(ownerUserID string, id, lastCheck string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	id = strings.TrimSpace(id)
	return database.GetDB().Model(&models.EmailConfig{}).Where("id = ? AND owner_user_id = ?", id, ownerUserID).Update("last_check", lastCheck).Error
}

func (r *EmailRepository) GetConfigIDs(ownerUserID string) ([]string, error) {
	return r.GetConfigIDsCtx(context.Background(), ownerUserID)
}

func (r *EmailRepository) GetConfigIDsCtx(ctx context.Context, ownerUserID string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	var ids []string
	err := database.GetDB().WithContext(ctx).Model(&models.EmailConfig{}).Where("owner_user_id = ?", ownerUserID).Pluck("id", &ids).Error
	return ids, err
}

// Email Log methods
func (r *EmailRepository) CreateLog(log *models.EmailLog) error {
	return database.GetDB().Create(log).Error
}

func (r *EmailRepository) FindLogs(ownerUserID string, configID string, limit int) ([]models.EmailLog, error) {
	return r.FindLogsCtx(context.Background(), ownerUserID, configID, limit)
}

func (r *EmailRepository) FindLogsCtx(ctx context.Context, ownerUserID string, configID string, limit int) ([]models.EmailLog, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	var logs []models.EmailLog

	query := database.GetDB().
		WithContext(ctx).
		Model(&models.EmailLog{}).
		Where("owner_user_id = ?", ownerUserID).
		Where("status <> ?", "deleted").
		// received_date is stored as an ISO/RFC3339 string; use SQLite datetime() to sort correctly
		// even when timezone offsets differ.
		Order("datetime(received_date) DESC").
		Order("created_at DESC")

	if configID != "" {
		query = query.Where("email_config_id = ?", strings.TrimSpace(configID))
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&logs).Error
	return logs, err
}

func (r *EmailRepository) FindLogsForMailboxReconcileCtx(ctx context.Context, ownerUserID string, configID string, mailbox string) ([]models.EmailLog, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if ownerUserID == "" || configID == "" {
		return []models.EmailLog{}, nil
	}

	var logs []models.EmailLog
	err := database.GetDB().
		WithContext(ctx).
		Model(&models.EmailLog{}).
		Select("id, message_uid, status").
		Where("owner_user_id = ? AND email_config_id = ? AND mailbox = ?", ownerUserID, configID, mailbox).
		Where("status <> ?", "deleted").
		Find(&logs).Error
	return logs, err
}

func (r *EmailRepository) GetMaxUIDForMailboxCtx(ctx context.Context, ownerUserID string, configID string, mailbox string) (maxUID uint32, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if ownerUserID == "" || configID == "" {
		return 0, nil
	}

	var v *uint32
	if err := database.GetDB().WithContext(ctx).
		Model(&models.EmailLog{}).
		Select("MAX(message_uid)").
		Where("owner_user_id = ? AND email_config_id = ? AND mailbox = ?", ownerUserID, configID, mailbox).
		Scan(&v).Error; err != nil {
		return 0, err
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

func (r *EmailRepository) GetMinUIDForMailboxCtx(ctx context.Context, ownerUserID string, configID string, mailbox string) (minUID uint32, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if ownerUserID == "" || configID == "" {
		return 0, nil
	}

	var v *uint32
	if err := database.GetDB().WithContext(ctx).
		Model(&models.EmailLog{}).
		Select("MIN(message_uid)").
		Where("owner_user_id = ? AND email_config_id = ? AND mailbox = ?", ownerUserID, configID, mailbox).
		Where("status <> ?", "deleted").
		Where("message_uid > 0").
		Scan(&v).Error; err != nil {
		return 0, err
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

func (r *EmailRepository) MarkLogsDeletedByIDs(ids []string) (int64, error) {
	var total int64
	if len(ids) == 0 {
		return 0, nil
	}

	const chunkSize = 200
	for i := 0; i < len(ids); i += chunkSize {
		end := i + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		if len(chunk) == 0 {
			continue
		}
		res := database.GetDB().
			Model(&models.EmailLog{}).
			Where("id IN ?", chunk).
			Where("status <> ?", "deleted").
			Update("status", "deleted")
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
	}

	return total, nil
}

func (r *EmailRepository) FindLogByID(id string) (*models.EmailLog, error) {
	return r.FindLogByIDCtx(context.Background(), id)
}

func (r *EmailRepository) FindLogByIDCtx(ctx context.Context, id string) (*models.EmailLog, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var logRow models.EmailLog
	if err := database.GetDB().WithContext(ctx).Where("id = ?", id).First(&logRow).Error; err != nil {
		return nil, err
	}
	return &logRow, nil
}

func (r *EmailRepository) FindLogByUIDCtx(ctx context.Context, ownerUserID string, configID string, mailbox string, messageUID uint32) (*models.EmailLog, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if ownerUserID == "" || configID == "" || messageUID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var logRow models.EmailLog
	if err := database.GetDB().WithContext(ctx).
		Select("id, owner_user_id, email_config_id, mailbox, message_uid, has_attachment, attachment_count, invoice_xml_url, invoice_pdf_url, parsed_invoice_id, status").
		Where("owner_user_id = ? AND email_config_id = ? AND mailbox = ? AND message_uid = ?", ownerUserID, configID, mailbox, messageUID).
		First(&logRow).Error; err != nil {
		return nil, err
	}
	return &logRow, nil
}

func (r *EmailRepository) LogExists(ownerUserID string, configID string, mailbox string, messageUID uint32) (bool, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	configID = strings.TrimSpace(configID)
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if ownerUserID == "" || configID == "" || messageUID == 0 {
		return false, fmt.Errorf("missing fields")
	}

	var cnt int64
	if err := database.GetDB().
		Model(&models.EmailLog{}).
		Where("owner_user_id = ? AND email_config_id = ? AND mailbox = ? AND message_uid = ?", ownerUserID, configID, mailbox, messageUID).
		Limit(1).
		Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func (r *EmailRepository) UpdateLog(id string, data map[string]interface{}) error {
	result := database.GetDB().Model(&models.EmailLog{}).Where("id = ?", id).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}
