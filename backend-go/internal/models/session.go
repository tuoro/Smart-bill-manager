package models

import "time"

// Session represents a server-side login session.
// The raw session token is only stored client-side in an HttpOnly cookie; the DB stores only its hash.
type Session struct {
	ID        string     `json:"id" gorm:"primaryKey"`
	TokenHash string     `json:"-" gorm:"uniqueIndex;not null"` // sha256 hex of raw token
	TokenHint string     `json:"token_hint" gorm:"index"`
	UserID    string     `json:"user_id" gorm:"index;not null"`
	ExpiresAt time.Time  `json:"expires_at" gorm:"index;not null"`
	LastSeen  time.Time  `json:"last_seen" gorm:"index"`
	UserAgent *string    `json:"user_agent"`
	IP        *string    `json:"ip"`
	RevokedAt *time.Time `json:"revoked_at" gorm:"index"`
	CreatedAt time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

func (Session) TableName() string {
	return "sessions"
}

