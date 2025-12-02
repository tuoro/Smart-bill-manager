package repository

import (
	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

type DingtalkRepository struct{}

func NewDingtalkRepository() *DingtalkRepository {
	return &DingtalkRepository{}
}

// Config methods
func (r *DingtalkRepository) CreateConfig(config *models.DingtalkConfig) error {
	return database.GetDB().Create(config).Error
}

func (r *DingtalkRepository) FindConfigByID(id string) (*models.DingtalkConfig, error) {
	var config models.DingtalkConfig
	err := database.GetDB().Where("id = ?", id).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *DingtalkRepository) FindAllConfigs() ([]models.DingtalkConfig, error) {
	var configs []models.DingtalkConfig
	err := database.GetDB().Find(&configs).Error
	return configs, err
}

func (r *DingtalkRepository) FindActiveConfig() (*models.DingtalkConfig, error) {
	var config models.DingtalkConfig
	err := database.GetDB().Where("is_active = 1").First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *DingtalkRepository) UpdateConfig(id string, data map[string]interface{}) error {
	result := database.GetDB().Model(&models.DingtalkConfig{}).Where("id = ?", id).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *DingtalkRepository) DeleteConfig(id string) error {
	result := database.GetDB().Where("id = ?", id).Delete(&models.DingtalkConfig{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

// Log methods
func (r *DingtalkRepository) CreateLog(log *models.DingtalkLog) error {
	return database.GetDB().Create(log).Error
}

func (r *DingtalkRepository) FindLogs(configID string, limit int) ([]models.DingtalkLog, error) {
	var logs []models.DingtalkLog
	
	query := database.GetDB().Model(&models.DingtalkLog{}).Order("created_at DESC")
	
	if configID != "" {
		query = query.Where("config_id = ?", configID)
	}
	
	if limit > 0 {
		query = query.Limit(limit)
	}
	
	err := query.Find(&logs).Error
	return logs, err
}
