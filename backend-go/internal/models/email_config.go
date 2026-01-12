package models

import (
	"time"
)

// EmailConfig represents email configuration for IMAP
type EmailConfig struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	OwnerUserID string  `json:"owner_user_id" gorm:"not null;default:'';index"`
	Email     string    `json:"email" gorm:"not null"`
	IMAPHost  string    `json:"imap_host" gorm:"not null"`
	IMAPPort  int       `json:"imap_port" gorm:"default:993"`
	Password  string    `json:"-" gorm:"not null"`
	IsActive  int       `json:"is_active" gorm:"default:1"`
	LastCheck *string   `json:"last_check"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (EmailConfig) TableName() string {
	return "email_configs"
}

// EmailConfigResponse is the response with masked password
type EmailConfigResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	IMAPHost  string    `json:"imap_host"`
	IMAPPort  int       `json:"imap_port"`
	Password  string    `json:"password"`
	IsActive  int       `json:"is_active"`
	LastCheck *string   `json:"last_check"`
	CreatedAt time.Time `json:"created_at"`
}

func (e *EmailConfig) ToResponse() EmailConfigResponse {
	return EmailConfigResponse{
		ID:        e.ID,
		Email:     e.Email,
		IMAPHost:  e.IMAPHost,
		IMAPPort:  e.IMAPPort,
		Password:  "********",
		IsActive:  e.IsActive,
		LastCheck: e.LastCheck,
		CreatedAt: e.CreatedAt,
	}
}

// EmailLog represents email log
type EmailLog struct {
	ID              string    `json:"id" gorm:"primaryKey"`
	OwnerUserID     string    `json:"owner_user_id" gorm:"not null;default:'';index"`
	EmailConfigID   string    `json:"email_config_id" gorm:"not null;index"`
	Mailbox         string    `json:"mailbox" gorm:"not null;default:INBOX"`
	MessageUID      uint32    `json:"message_uid" gorm:"not null;default:0;index"`
	Subject         *string   `json:"subject"`
	FromAddress     *string   `json:"from_address"`
	ReceivedDate    *string   `json:"received_date"`
	HasAttachment   int       `json:"has_attachment" gorm:"default:0"`
	AttachmentCount int       `json:"attachment_count" gorm:"default:0"`
	InvoiceXMLURL   *string   `json:"invoice_xml_url"`
	InvoicePDFURL   *string   `json:"invoice_pdf_url"`
	ParsedInvoiceID *string   `json:"parsed_invoice_id" gorm:"index"`
	ParsedInvoiceIDs *string  `json:"parsed_invoice_ids"`
	ParseError      *string   `json:"parse_error"`
	Status          string    `json:"status" gorm:"default:received"` // received/parsed/error
	CreatedAt       time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (EmailLog) TableName() string {
	return "email_logs"
}

// MonitorStatus represents monitoring status
type MonitorStatus struct {
	ConfigID string `json:"configId"`
	Status   string `json:"status"`
}
