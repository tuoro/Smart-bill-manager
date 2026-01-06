package models

import "time"

// InvoiceOCRBlob stores large OCR payloads for an invoice outside the main invoices table.
// This keeps list queries fast and prevents the invoices table from growing with large text blobs.
type InvoiceOCRBlob struct {
	InvoiceID     string    `json:"-" gorm:"primaryKey"`
	OwnerUserID   string    `json:"-" gorm:"not null;default:'';index"`
	ExtractedData *string   `json:"-"` // JSON string
	RawText       *string   `json:"-"` // Raw OCR text
	UpdatedAt     time.Time `json:"-" gorm:"autoUpdateTime"`
	CreatedAt     time.Time `json:"-" gorm:"autoCreateTime"`
}

func (InvoiceOCRBlob) TableName() string {
	return "invoice_ocr_blobs"
}

// PaymentOCRBlob stores large OCR payloads for a payment outside the main payments table.
type PaymentOCRBlob struct {
	PaymentID     string    `json:"-" gorm:"primaryKey"`
	OwnerUserID   string    `json:"-" gorm:"not null;default:'';index"`
	ExtractedData *string   `json:"-"` // JSON string
	UpdatedAt     time.Time `json:"-" gorm:"autoUpdateTime"`
	CreatedAt     time.Time `json:"-" gorm:"autoCreateTime"`
}

func (PaymentOCRBlob) TableName() string {
	return "payment_ocr_blobs"
}
