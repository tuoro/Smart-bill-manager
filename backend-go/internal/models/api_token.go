package models

import "time"

// APIToken represents a long-lived API credential for non-browser clients.
// The actual credential is a PASETO string; the DB stores only a token ID for revocation and auditing.
type APIToken struct {
	ID         string     `json:"id" gorm:"primaryKey"` // token_id embedded in PASETO
	Name       string     `json:"name" gorm:"index;not null"`
	UserID     string     `json:"user_id" gorm:"index;not null"`
	TokenHint  string     `json:"token_hint" gorm:"index"`
	ExpiresAt  *time.Time `json:"expires_at" gorm:"index"`
	LastUsedAt *time.Time `json:"last_used_at" gorm:"index"`
	RevokedAt  *time.Time `json:"revoked_at" gorm:"index"`
	CreatedAt  time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

func (APIToken) TableName() string {
	return "api_tokens"
}

