package models

import "time"

// SystemSetting stores admin-managed global configuration.
// Values are stored as JSON for flexibility.
type SystemSetting struct {
	Key       string    `json:"key" gorm:"primaryKey"`
	ValueJSON string    `json:"value_json" gorm:"type:text;not null"`
	UpdatedBy *string   `json:"updated_by" gorm:"index"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (SystemSetting) TableName() string {
	return "system_settings"
}

