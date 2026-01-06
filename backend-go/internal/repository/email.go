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

	query := database.GetDB().WithContext(ctx).Model(&models.EmailLog{}).Where("owner_user_id = ?", ownerUserID).Order("created_at DESC")

	if configID != "" {
		query = query.Where("email_config_id = ?", strings.TrimSpace(configID))
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&logs).Error
	return logs, err
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
