package models

import "time"

// RegressionSample stores a "golden" expected parse result for a specific OCR raw_text.
// Samples are created by admin from already-confirmed records and later exported into repo testdata.
type RegressionSample struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	Kind         string    `json:"kind" gorm:"not null;index"` // payment_screenshot | invoice
	Name         string    `json:"name" gorm:"not null;index"`
	Origin       string    `json:"origin" gorm:"not null;default:ui;index"` // ui | repo
	SourceType   string    `json:"source_type" gorm:"not null;index"`       // payment | invoice
	SourceID     string    `json:"source_id" gorm:"not null;index"`
	CreatedBy    string    `json:"created_by" gorm:"not null;index"`
	RawText      string    `json:"raw_text" gorm:"type:text;not null"`
	RawHash      string    `json:"raw_hash" gorm:"not null;default:'';index"` // sha256(raw_text)
	ExpectedJSON string    `json:"expected_json" gorm:"type:text;not null"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (RegressionSample) TableName() string {
	return "regression_samples"
}
