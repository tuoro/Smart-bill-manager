package models

import (
	"time"
)

// Invoice represents an invoice record
type Invoice struct {
	ID            string    `json:"id" gorm:"primaryKey"`
	OwnerUserID   string    `json:"owner_user_id" gorm:"not null;default:'';index"`
	IsDraft       bool      `json:"is_draft" gorm:"not null;default:false;index"`
	PaymentID     *string   `json:"payment_id" gorm:"index"` // Keep for backward compatibility
	Filename      string    `json:"filename" gorm:"not null"`
	OriginalName  string    `json:"original_name" gorm:"not null"`
	FilePath      string    `json:"file_path" gorm:"not null"`
	FileSize      *int64    `json:"file_size"`
	FileSHA256    *string   `json:"file_sha256" gorm:"index"`
	InvoiceNumber *string   `json:"invoice_number"`
	InvoiceDate   *string   `json:"invoice_date"`
	Amount        *float64  `json:"amount"`
	BadDebt       bool      `json:"bad_debt" gorm:"not null;default:false;index"`
	SellerName    *string   `json:"seller_name"`
	BuyerName     *string   `json:"buyer_name"`
	TaxAmount     *float64  `json:"tax_amount"` // New field for tax amount
	ExtractedData *string   `json:"extracted_data"`
	ParseStatus   string    `json:"parse_status" gorm:"default:pending"` // pending/parsing/success/failed
	ParseError    *string   `json:"parse_error"`
	RawText       *string   `json:"raw_text"` // OCR extracted raw text for frontend display
	Source        string    `json:"source" gorm:"default:upload"`
	DedupStatus   string    `json:"dedup_status" gorm:"not null;default:ok;index"`
	DedupRefID    *string   `json:"dedup_ref_id" gorm:"index"`
	CreatedAt     time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (Invoice) TableName() string {
	return "invoices"
}

// InvoicePaymentLink represents the many-to-many relationship between invoices and payments
type InvoicePaymentLink struct {
	InvoiceID string    `json:"invoice_id" gorm:"primaryKey;index"`
	PaymentID string    `json:"payment_id" gorm:"primaryKey;index"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (InvoicePaymentLink) TableName() string {
	return "invoice_payment_links"
}

// InvoiceStats represents invoice statistics
type InvoiceStats struct {
	TotalCount  int                `json:"totalCount"`
	TotalAmount float64            `json:"totalAmount"`
	BySource    map[string]int     `json:"bySource"`
	ByMonth     map[string]float64 `json:"byMonth"`
}
