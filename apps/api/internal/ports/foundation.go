package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewID() (string, error)
}

type PasswordHasher interface {
	Hash(password []byte) (string, error)
	Verify(password []byte, encoded string) (bool, error)
}

type TokenGenerator interface {
	NewToken() (raw string, hash string, err error)
	Hash(raw string) string
}

type BootstrapOwner struct {
	UserID          string
	TenantID        string
	Email           string
	PasswordHash    string
	DisplayName     string
	TenantName      string
	DefaultCurrency domain.Currency
	Timezone        string
	CreatedAt       time.Time
}

type LoginCandidate struct {
	UserID       string
	Email        string
	DisplayName  string
	PasswordHash string
	TenantID     string
	TenantName   string
	Currency     domain.Currency
	Timezone     string
	Role         domain.Role
	Status       string
}

type SessionRecord struct {
	ID            string
	TenantID      string
	UserID        string
	TokenHash     string
	CSRFTokenHash string
	ExpiresAt     time.Time
	CreatedAt     time.Time
	LastSeenAt    time.Time
}

type SessionPrincipal struct {
	SessionID     string
	TenantID      string
	TenantName    string
	Currency      domain.Currency
	Timezone      string
	UserID        string
	Email         string
	DisplayName   string
	Role          domain.Role
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	CSRFTokenHash string
}

type IdentityRepository interface {
	BootstrapOwner(ctx context.Context, owner BootstrapOwner) error
	FindLoginCandidates(ctx context.Context, normalizedEmail string) ([]LoginCandidate, error)
	CreateSession(ctx context.Context, session SessionRecord) error
	FindSession(ctx context.Context, tokenHash string) (SessionPrincipal, error)
	TouchSession(ctx context.Context, tenantID, sessionID string, now time.Time) error
	RevokeSession(ctx context.Context, tenantID, sessionID string, now time.Time) error
}
