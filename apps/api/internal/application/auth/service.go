package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type LoginInput struct {
	Email    string
	Password []byte
	TenantID string
}

type SessionView struct {
	SessionID    string
	SessionToken string
	CSRFToken    string
	UserID       string
	Email        string
	DisplayName  string
	TenantID     string
	TenantName   string
	Currency     domain.Currency
	Timezone     string
	Role         domain.Role
	Capabilities []domain.Capability
	ExpiresAt    time.Time
}

type Service struct {
	repository ports.IdentityRepository
	hasher     ports.PasswordHasher
	tokens     ports.TokenGenerator
	ids        ports.IDGenerator
	clock      ports.Clock
	sessionTTL time.Duration
	dummyHash  string
}

func NewService(
	repository ports.IdentityRepository,
	hasher ports.PasswordHasher,
	tokens ports.TokenGenerator,
	ids ports.IDGenerator,
	clock ports.Clock,
	sessionTTL time.Duration,
) (Service, error) {
	if sessionTTL < 5*time.Minute || sessionTTL > 30*24*time.Hour {
		return Service{}, errors.New("session TTL must be between 5 minutes and 30 days")
	}
	dummyHash, err := hasher.Hash([]byte("dummy-password-never-used"))
	if err != nil {
		return Service{}, fmt.Errorf("prepare authentication guard: %w", err)
	}
	return Service{
		repository: repository,
		hasher:     hasher,
		tokens:     tokens,
		ids:        ids,
		clock:      clock,
		sessionTTL: sessionTTL,
		dummyHash:  dummyHash,
	}, nil
}

func (s Service) Login(ctx context.Context, input LoginInput) (SessionView, error) {
	email := strings.ToLower(strings.TrimSpace(norm.NFKC.String(input.Email)))
	candidates, err := s.repository.FindLoginCandidates(ctx, email)
	if err != nil {
		return SessionView{}, err
	}
	if len(candidates) == 0 {
		_, _ = s.hasher.Verify(input.Password, s.dummyHash)
		return SessionView{}, domain.ErrUnauthenticated
	}
	passwordOK, err := s.hasher.Verify(input.Password, candidates[0].PasswordHash)
	if err != nil {
		return SessionView{}, fmt.Errorf("verify password: %w", err)
	}
	if !passwordOK {
		return SessionView{}, domain.ErrUnauthenticated
	}
	candidate, err := selectCandidate(candidates, input.TenantID)
	if err != nil {
		return SessionView{}, err
	}
	sessionID, err := s.ids.NewID()
	if err != nil {
		return SessionView{}, fmt.Errorf("generate session id: %w", err)
	}
	sessionToken, sessionHash, err := s.tokens.NewToken()
	if err != nil {
		return SessionView{}, err
	}
	csrfToken, csrfHash, err := s.tokens.NewToken()
	if err != nil {
		return SessionView{}, err
	}
	now := s.clock.Now()
	expiresAt := now.Add(s.sessionTTL)
	if err := s.repository.CreateSession(ctx, ports.SessionRecord{
		ID:            sessionID,
		TenantID:      candidate.TenantID,
		UserID:        candidate.UserID,
		TokenHash:     sessionHash,
		CSRFTokenHash: csrfHash,
		ExpiresAt:     expiresAt,
		CreatedAt:     now,
		LastSeenAt:    now,
	}); err != nil {
		return SessionView{}, err
	}
	return viewFromCandidate(candidate, sessionID, sessionToken, csrfToken, expiresAt), nil
}

func (s Service) Authenticate(ctx context.Context, rawToken string) (ports.SessionPrincipal, error) {
	if rawToken == "" {
		return ports.SessionPrincipal{}, domain.ErrUnauthenticated
	}
	principal, err := s.repository.FindSession(ctx, s.tokens.Hash(rawToken))
	if err != nil {
		return ports.SessionPrincipal{}, err
	}
	now := s.clock.Now()
	if principal.RevokedAt != nil || !principal.ExpiresAt.After(now) {
		return ports.SessionPrincipal{}, domain.ErrUnauthenticated
	}
	if err := s.repository.TouchSession(ctx, principal.TenantID, principal.SessionID, now); err != nil {
		return ports.SessionPrincipal{}, err
	}
	return principal, nil
}

func (s Service) VerifyCSRF(principal ports.SessionPrincipal, rawToken string) error {
	if rawToken == "" {
		return domain.ErrForbidden
	}
	actual := s.tokens.Hash(rawToken)
	if subtle.ConstantTimeCompare([]byte(actual), []byte(principal.CSRFTokenHash)) != 1 {
		return domain.ErrForbidden
	}
	return nil
}

func (s Service) Logout(ctx context.Context, principal ports.SessionPrincipal) error {
	return s.repository.RevokeSession(ctx, principal.TenantID, principal.SessionID, s.clock.Now())
}

func selectCandidate(candidates []ports.LoginCandidate, tenantID string) (ports.LoginCandidate, error) {
	active := make([]ports.LoginCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Status != "active" {
			continue
		}
		if tenantID != "" && candidate.TenantID != tenantID {
			continue
		}
		active = append(active, candidate)
	}
	if len(active) == 0 {
		return ports.LoginCandidate{}, domain.ErrUnauthenticated
	}
	if tenantID == "" && len(active) > 1 {
		return ports.LoginCandidate{}, domain.ErrTenantRequired
	}
	return active[0], nil
}

func viewFromCandidate(
	candidate ports.LoginCandidate,
	sessionID, sessionToken, csrfToken string,
	expiresAt time.Time,
) SessionView {
	return SessionView{
		SessionID:    sessionID,
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
		UserID:       candidate.UserID,
		Email:        candidate.Email,
		DisplayName:  candidate.DisplayName,
		TenantID:     candidate.TenantID,
		TenantName:   candidate.TenantName,
		Currency:     candidate.Currency,
		Timezone:     candidate.Timezone,
		Role:         candidate.Role,
		Capabilities: candidate.Role.Capabilities(),
		ExpiresAt:    expiresAt,
	}
}
