package models

import "time"

// Task represents an async background job (e.g., OCR parse).
type Task struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	OwnerUserID string  `json:"owner_user_id" gorm:"not null;default:'';index"`
	Type      string    `json:"type" gorm:"not null;index"`   // payment_ocr | invoice_ocr
	Status    string    `json:"status" gorm:"not null;index"` // queued | processing | succeeded | failed | canceled
	CreatedBy string    `json:"created_by" gorm:"not null;index"`
	TargetID  string    `json:"target_id" gorm:"not null;index"` // payment_id or invoice_id
	FileSHA256 *string  `json:"file_sha256" gorm:"index"`
	ResultJSON *string  `json:"result_json"`
	Error      *string  `json:"error"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime;index"`
}

func (Task) TableName() string {
	return "tasks"
}
