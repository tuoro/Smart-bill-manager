package repository

import (
	"context"
	"strings"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

type TripRepository struct {
	db *gorm.DB
}

func NewTripRepository(db *gorm.DB) *TripRepository {
	return &TripRepository{db: db}
}

func (r *TripRepository) Create(trip *models.Trip) error {
	return r.db.Create(trip).Error
}

func (r *TripRepository) FindByID(id string) (*models.Trip, error) {
	return r.FindByIDCtx(context.Background(), id)
}

func (r *TripRepository) FindByIDCtx(ctx context.Context, id string) (*models.Trip, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var trip models.Trip
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&trip).Error
	if err != nil {
		return nil, err
	}
	return &trip, nil
}

func (r *TripRepository) FindAll(ownerUserID string) ([]models.Trip, error) {
	return r.FindAllCtx(context.Background(), ownerUserID)
}

func (r *TripRepository) FindAllCtx(ctx context.Context, ownerUserID string) ([]models.Trip, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var trips []models.Trip
	q := r.db.WithContext(ctx).Model(&models.Trip{}).Order("start_time_ts DESC")
	if strings.TrimSpace(ownerUserID) != "" {
		q = q.Where("owner_user_id = ?", strings.TrimSpace(ownerUserID))
	}
	err := q.Find(&trips).Error
	return trips, err
}

func (r *TripRepository) FindByIDForOwner(ownerUserID string, id string) (*models.Trip, error) {
	return r.FindByIDForOwnerCtx(context.Background(), ownerUserID, id)
}

func (r *TripRepository) FindByIDForOwnerCtx(ctx context.Context, ownerUserID string, id string) (*models.Trip, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var trip models.Trip
	ownerUserID = strings.TrimSpace(ownerUserID)
	id = strings.TrimSpace(id)
	if ownerUserID == "" || id == "" {
		return nil, gorm.ErrRecordNotFound
	}
	q := r.db.WithContext(ctx).Where("id = ? AND owner_user_id = ?", id, ownerUserID)
	err := q.First(&trip).Error
	if err != nil {
		return nil, err
	}
	return &trip, nil
}

func (r *TripRepository) Update(id string, data map[string]interface{}) error {
	result := r.db.Model(&models.Trip{}).Where("id = ?", id).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *TripRepository) UpdateForOwner(ownerUserID string, id string, data map[string]interface{}) error {
	q := r.db.Model(&models.Trip{}).Where("id = ?", id)
	if ownerUserID != "" {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	result := q.Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *TripRepository) Delete(id string) error {
	result := r.db.Where("id = ?", id).Delete(&models.Trip{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *TripRepository) DeleteForOwner(ownerUserID string, id string) error {
	q := r.db.Where("id = ?", id)
	if ownerUserID != "" {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	result := q.Delete(&models.Trip{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}
