package repository

import (
	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

type TripRepository struct{}

func NewTripRepository() *TripRepository {
	return &TripRepository{}
}

func (r *TripRepository) Create(trip *models.Trip) error {
	return database.GetDB().Create(trip).Error
}

func (r *TripRepository) FindByID(id string) (*models.Trip, error) {
	var trip models.Trip
	err := database.GetDB().Where("id = ?", id).First(&trip).Error
	if err != nil {
		return nil, err
	}
	return &trip, nil
}

func (r *TripRepository) FindAll() ([]models.Trip, error) {
	var trips []models.Trip
	err := database.GetDB().Model(&models.Trip{}).Order("start_time DESC").Find(&trips).Error
	return trips, err
}

func (r *TripRepository) Update(id string, data map[string]interface{}) error {
	result := database.GetDB().Model(&models.Trip{}).Where("id = ?", id).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *TripRepository) Delete(id string) error {
	result := database.GetDB().Where("id = ?", id).Delete(&models.Trip{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}
