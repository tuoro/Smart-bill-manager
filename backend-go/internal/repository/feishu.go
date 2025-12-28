package repository

import (
	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

type FeishuRepository struct{}

func NewFeishuRepository() *FeishuRepository {
	return &FeishuRepository{}
}

// Config methods
func (r *FeishuRepository) CreateConfig(config *models.FeishuConfig) error {
	return database.GetDB().Create(config).Error
}

func (r *FeishuRepository) FindConfigByID(id string) (*models.FeishuConfig, error) {
	var config models.FeishuConfig
	err := database.GetDB().Where("id = ?", id).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *FeishuRepository) FindAllConfigs() ([]models.FeishuConfig, error) {
	var configs []models.FeishuConfig
	err := database.GetDB().Find(&configs).Error
	return configs, err
}

func (r *FeishuRepository) FindActiveConfig() (*models.FeishuConfig, error) {
	var config models.FeishuConfig
	err := database.GetDB().Where("is_active = 1").First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *FeishuRepository) UpdateConfig(id string, data map[string]interface{}) error {
	result := database.GetDB().Model(&models.FeishuConfig{}).Where("id = ?", id).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *FeishuRepository) DeleteConfig(id string) error {
	result := database.GetDB().Where("id = ?", id).Delete(&models.FeishuConfig{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

// Log methods
func (r *FeishuRepository) CreateLog(log *models.FeishuLog) error {
	return database.GetDB().Create(log).Error
}

func (r *FeishuRepository) FindLogs(configID string, limit int) ([]models.FeishuLog, error) {
	var logs []models.FeishuLog

	query := database.GetDB().Model(&models.FeishuLog{}).Order("created_at DESC")
	if configID != "" {
		query = query.Where("config_id = ?", configID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&logs).Error
	return logs, err
}
