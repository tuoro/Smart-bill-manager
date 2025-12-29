package models

import "time"

// Trip represents a travel/business trip period used for grouping payments.
// Note: start_time/end_time are stored as RFC3339 strings to match payments.transaction_time.
type Trip struct {
	ID              string    `json:"id" gorm:"primaryKey"`
	Name            string    `json:"name" gorm:"not null"`
	StartTime       string    `json:"start_time" gorm:"not null;index"`
	EndTime         string    `json:"end_time" gorm:"not null;index"`
	StartTimeTs     int64     `json:"start_time_ts" gorm:"not null;default:0;index"`
	EndTimeTs       int64     `json:"end_time_ts" gorm:"not null;default:0;index"`
	Timezone        string    `json:"timezone" gorm:"not null;default:Asia/Shanghai;index"`
	ReimburseStatus string    `json:"reimburse_status" gorm:"not null;default:unreimbursed;index"` // unreimbursed|reimbursed
	BadDebtLocked   bool      `json:"bad_debt_locked" gorm:"not null;default:false;index"`         // auto: any bad_debt under this trip
	Note            *string   `json:"note"`
	CreatedAt       time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (Trip) TableName() string {
	return "trips"
}
