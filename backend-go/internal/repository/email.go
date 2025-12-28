package repository

import (
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
	var config models.EmailConfig
	err := database.GetDB().Where("id = ?", id).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *EmailRepository) FindAllConfigs() ([]models.EmailConfig, error) {
	var configs []models.EmailConfig
	err := database.GetDB().Find(&configs).Error
	return configs, err
}

func (r *EmailRepository) UpdateConfig(id string, data map[string]interface{}) error {
	result := database.GetDB().Model(&models.EmailConfig{}).Where("id = ?", id).Updates(data)
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

func (r *EmailRepository) UpdateLastCheck(id, lastCheck string) error {
	return database.GetDB().Model(&models.EmailConfig{}).Where("id = ?", id).Update("last_check", lastCheck).Error
}

func (r *EmailRepository) GetConfigIDs() ([]string, error) {
	var ids []string
	err := database.GetDB().Model(&models.EmailConfig{}).Pluck("id", &ids).Error
	return ids, err
}

// Email Log methods
func (r *EmailRepository) CreateLog(log *models.EmailLog) error {
	return database.GetDB().Create(log).Error
}

func (r *EmailRepository) FindLogs(configID string, limit int) ([]models.EmailLog, error) {
	var logs []models.EmailLog

	query := database.GetDB().Model(&models.EmailLog{}).Order("created_at DESC")

	if configID != "" {
		query = query.Where("email_config_id = ?", configID)
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&logs).Error
	return logs, err
}
