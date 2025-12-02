package models

import (
	"time"
)

// DingtalkConfig represents DingTalk configuration
type DingtalkConfig struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	Name         string    `json:"name" gorm:"not null"`
	AppKey       *string   `json:"-"`
	AppSecret    *string   `json:"-"`
	WebhookToken *string   `json:"-"`
	IsActive     int       `json:"is_active" gorm:"default:1"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (DingtalkConfig) TableName() string {
	return "dingtalk_configs"
}

// DingtalkConfigResponse is the response with masked secrets
type DingtalkConfigResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	AppKey       *string   `json:"app_key"`
	AppSecret    *string   `json:"app_secret"`
	WebhookToken *string   `json:"webhook_token"`
	IsActive     int       `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
}

func (d *DingtalkConfig) ToResponse() DingtalkConfigResponse {
	var appSecret, webhookToken *string
	if d.AppSecret != nil && *d.AppSecret != "" {
		masked := "********"
		appSecret = &masked
	}
	if d.WebhookToken != nil && *d.WebhookToken != "" {
		masked := "********"
		webhookToken = &masked
	}
	return DingtalkConfigResponse{
		ID:           d.ID,
		Name:         d.Name,
		AppKey:       d.AppKey,
		AppSecret:    appSecret,
		WebhookToken: webhookToken,
		IsActive:     d.IsActive,
		CreatedAt:    d.CreatedAt,
	}
}

// DingtalkLog represents DingTalk message log
type DingtalkLog struct {
	ID              string    `json:"id" gorm:"primaryKey"`
	ConfigID        string    `json:"config_id" gorm:"not null;index"`
	MessageType     *string   `json:"message_type"`
	SenderNick      *string   `json:"sender_nick"`
	SenderID        *string   `json:"sender_id"`
	Content         *string   `json:"content"`
	HasAttachment   int       `json:"has_attachment" gorm:"default:0"`
	AttachmentCount int       `json:"attachment_count" gorm:"default:0"`
	Status          string    `json:"status" gorm:"default:processed"`
	CreatedAt       time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (DingtalkLog) TableName() string {
	return "dingtalk_logs"
}

// DingtalkMessage represents incoming webhook message
type DingtalkMessage struct {
	Msgtype                   string               `json:"msgtype"`
	Text                      *DingtalkText        `json:"text,omitempty"`
	MsgID                     string               `json:"msgId,omitempty"`
	CreateAt                  int64                `json:"createAt,omitempty"`
	ConversationType          string               `json:"conversationType,omitempty"`
	ConversationID            string               `json:"conversationId,omitempty"`
	SenderID                  string               `json:"senderId,omitempty"`
	SenderNick                string               `json:"senderNick,omitempty"`
	SenderCorpID              string               `json:"senderCorpId,omitempty"`
	SessionWebhook            string               `json:"sessionWebhook,omitempty"`
	SessionWebhookExpiredTime int64                `json:"sessionWebhookExpiredTime,omitempty"`
	IsAdmin                   bool                 `json:"isAdmin,omitempty"`
	ChatbotUserID             string               `json:"chatbotUserId,omitempty"`
	IsInAtList                bool                 `json:"isInAtList,omitempty"`
	SenderStaffID             string               `json:"senderStaffId,omitempty"`
	ChatbotCorpID             string               `json:"chatbotCorpId,omitempty"`
	AtUsers                   []DingtalkAtUser     `json:"atUsers,omitempty"`
	Content                   *DingtalkFileContent `json:"content,omitempty"`
}

type DingtalkText struct {
	Content string `json:"content"`
}

type DingtalkAtUser struct {
	DingtalkID string `json:"dingtalkId"`
	StaffID    string `json:"staffId,omitempty"`
}

type DingtalkFileContent struct {
	DownloadCode string `json:"downloadCode,omitempty"`
	FileName     string `json:"fileName,omitempty"`
}

// DingtalkResponse represents response to DingTalk
type DingtalkResponse struct {
	Msgtype string             `json:"msgtype"`
	Text    DingtalkTextReply  `json:"text,omitempty"`
}

type DingtalkTextReply struct {
	Content string `json:"content"`
}
