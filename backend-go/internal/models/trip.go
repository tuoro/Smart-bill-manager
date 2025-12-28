package models

import "time"

// Trip represents a travel/business trip period used for grouping payments.
// Note: start_time/end_time are stored as RFC3339 strings to match payments.transaction_time.
type Trip struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"not null"`
	StartTime string    `json:"start_time" gorm:"not null;index"`
	EndTime   string    `json:"end_time" gorm:"not null;index"`
	Note      *string   `json:"note"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (Trip) TableName() string {
	return "trips"
}
