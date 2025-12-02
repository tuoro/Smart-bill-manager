package models

import (
	"time"
)

// EmailConfig represents email configuration for IMAP
type EmailConfig struct {
	ID        string    `json:"id" gorm:"primaryKey"`
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
	EmailConfigID   string    `json:"email_config_id" gorm:"not null;index"`
	Subject         *string   `json:"subject"`
	FromAddress     *string   `json:"from_address"`
	ReceivedDate    *string   `json:"received_date"`
	HasAttachment   int       `json:"has_attachment" gorm:"default:0"`
	AttachmentCount int       `json:"attachment_count" gorm:"default:0"`
	Status          string    `json:"status" gorm:"default:processed"`
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
