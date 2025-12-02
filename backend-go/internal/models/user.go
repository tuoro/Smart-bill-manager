package models

import (
	"time"
)

// User represents a user in the system
type User struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Username  string    `json:"username" gorm:"uniqueIndex;not null"`
	Password  string    `json:"-" gorm:"not null"`
	Email     *string   `json:"email"`
	Role      string    `json:"role" gorm:"default:user"`
	IsActive  int       `json:"is_active" gorm:"default:1"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// UserResponse is the response without password
type UserResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     *string   `json:"email"`
	Role      string    `json:"role"`
	IsActive  int       `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (u *User) ToResponse() UserResponse {
	return UserResponse{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		Role:      u.Role,
		IsActive:  u.IsActive,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

func (User) TableName() string {
	return "users"
}
