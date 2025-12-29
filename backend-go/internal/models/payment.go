package models

import (
	"time"
)

// Payment represents a payment record
type Payment struct {
	ID                string    `json:"id" gorm:"primaryKey"`
	TripID            *string   `json:"trip_id" gorm:"index"`
	TripAssignSrc     string    `json:"trip_assignment_source" gorm:"not null;default:auto;index"`    // auto|manual|blocked
	TripAssignState   string    `json:"trip_assignment_state" gorm:"not null;default:no_match;index"` // assigned|no_match|overlap|blocked
	BadDebt           bool      `json:"bad_debt" gorm:"not null;default:false;index"`
	Amount            float64   `json:"amount" gorm:"not null"`
	Merchant          *string   `json:"merchant"`
	Category          *string   `json:"category"`
	PaymentMethod     *string   `json:"payment_method"`
	Description       *string   `json:"description"`
	TransactionTime   string    `json:"transaction_time" gorm:"not null"`
	TransactionTimeTs int64     `json:"transaction_time_ts" gorm:"not null;default:0;index"`
	ScreenshotPath    *string   `json:"screenshot_path"`
	ExtractedData     *string   `json:"extracted_data"`
	CreatedAt         time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (Payment) TableName() string {
	return "payments"
}

// PaymentStats represents payment statistics
type PaymentStats struct {
	TotalAmount   float64            `json:"totalAmount"`
	TotalCount    int                `json:"totalCount"`
	CategoryStats map[string]float64 `json:"categoryStats"`
	MerchantStats map[string]float64 `json:"merchantStats"`
	DailyStats    map[string]float64 `json:"dailyStats"`
}
