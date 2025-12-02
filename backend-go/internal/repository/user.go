package repository

import (
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
	var user models.User
	err := database.GetDB().Where("username = ? AND is_active = 1", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByID(id string) (*models.User, error) {
	var user models.User
	err := database.GetDB().Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindAll() ([]models.User, error) {
	var users []models.User
	err := database.GetDB().Find(&users).Error
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
	var count int64
	err := database.GetDB().Model(&models.User{}).Count(&count).Error
	return count, err
}

func (r *UserRepository) ExistsByUsername(username string) (bool, error) {
	var count int64
	err := database.GetDB().Model(&models.User{}).Where("username = ?", username).Count(&count).Error
	return count > 0, err
}
