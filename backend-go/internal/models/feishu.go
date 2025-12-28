package models

import "time"

// FeishuConfig represents Feishu (Lark) bot configuration.
type FeishuConfig struct {
	ID                string    `json:"id" gorm:"primaryKey"`
	Name              string    `json:"name" gorm:"not null"`
	AppID             *string   `json:"-"`
	AppSecret         *string   `json:"-"`
	VerificationToken *string   `json:"-"`
	EncryptKey        *string   `json:"-"`
	IsActive          int       `json:"is_active" gorm:"default:1"`
	CreatedAt         time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (FeishuConfig) TableName() string {
	return "feishu_configs"
}

// FeishuConfigResponse is the response with masked secrets.
type FeishuConfigResponse struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	AppID             *string   `json:"app_id"`
	AppSecret         *string   `json:"app_secret"`
	VerificationToken *string   `json:"verification_token"`
	EncryptKey        *string   `json:"encrypt_key"`
	IsActive          int       `json:"is_active"`
	CreatedAt         time.Time `json:"created_at"`
}

func (c *FeishuConfig) ToResponse() FeishuConfigResponse {
	mask := func(v *string) *string {
		if v == nil || *v == "" {
			return nil
		}
		m := "********"
		return &m
	}
	return FeishuConfigResponse{
		ID:                c.ID,
		Name:              c.Name,
		AppID:             c.AppID,
		AppSecret:         mask(c.AppSecret),
		VerificationToken: mask(c.VerificationToken),
		EncryptKey:        mask(c.EncryptKey),
		IsActive:          c.IsActive,
		CreatedAt:         c.CreatedAt,
	}
}

// FeishuLog records Feishu event processing.
type FeishuLog struct {
	ID            string    `json:"id" gorm:"primaryKey"`
	ConfigID      string    `json:"config_id" gorm:"not null;index"`
	EventType     *string   `json:"event_type"`
	MessageType   *string   `json:"message_type"`
	SenderID      *string   `json:"sender_id"`
	SenderName    *string   `json:"sender_name"`
	ChatID        *string   `json:"chat_id"`
	MessageID     *string   `json:"message_id"`
	Content       *string   `json:"content"`
	FileName      *string   `json:"file_name"`
	FileKey       *string   `json:"file_key"`
	HasAttachment int       `json:"has_attachment" gorm:"default:0"`
	Status        string    `json:"status" gorm:"default:processed"`
	Error         *string   `json:"error"`
	CreatedAt     time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (FeishuLog) TableName() string {
	return "feishu_logs"
}
