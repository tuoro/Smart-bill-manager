package repository

import (
	"context"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
)

type UserRepository struct{}

func NewUserRepository() *UserRepository {
	return &UserRepository{}
}

func (r *UserRepository) Create(user *models.User) error {
	return database.GetDB().Create(user).Error
}

func (r *UserRepository) FindByUsername(username string) (*models.User, error) {
	return r.FindByUsernameCtx(context.Background(), username)
}

func (r *UserRepository) FindByUsernameCtx(ctx context.Context, username string) (*models.User, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var user models.User
	err := database.GetDB().WithContext(ctx).Where("username = ? AND is_active = 1", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByID(id string) (*models.User, error) {
	return r.FindByIDCtx(context.Background(), id)
}

func (r *UserRepository) FindByIDCtx(ctx context.Context, id string) (*models.User, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var user models.User
	err := database.GetDB().WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindAll() ([]models.User, error) {
	return r.FindAllCtx(context.Background())
}

func (r *UserRepository) FindAllCtx(ctx context.Context) ([]models.User, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var users []models.User
	err := database.GetDB().WithContext(ctx).Find(&users).Error
	return users, err
}

func (r *UserRepository) Update(user *models.User) error {
	return database.GetDB().Save(user).Error
}

func (r *UserRepository) UpdatePassword(id, hashedPassword string) error {
	return database.GetDB().Model(&models.User{}).Where("id = ?", id).Update("password", hashedPassword).Error
}

func (r *UserRepository) UpdateRole(username, role string) error {
	return database.GetDB().Model(&models.User{}).Where("username = ?", username).Update("role", role).Error
}

func (r *UserRepository) Count() (int64, error) {
	return r.CountCtx(context.Background())
}

func (r *UserRepository) CountCtx(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var count int64
	err := database.GetDB().WithContext(ctx).Model(&models.User{}).Count(&count).Error
	return count, err
}

func (r *UserRepository) ExistsByUsername(username string) (bool, error) {
	return r.ExistsByUsernameCtx(context.Background(), username)
}

func (r *UserRepository) ExistsByUsernameCtx(ctx context.Context, username string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var count int64
	err := database.GetDB().WithContext(ctx).Model(&models.User{}).Where("username = ?", username).Count(&count).Error
	return count > 0, err
}
