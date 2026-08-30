package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type authRepositoryStub struct {
	candidates       []ports.LoginCandidate
	candidateErr     error
	principal        ports.SessionPrincipal
	principalErr     error
	createdSession   ports.SessionRecord
	touchedTenant    string
	touchedSession   string
	revokedTenant    string
	revokedSession   string
	createSessionErr error
	touchErr         error
	revokeErr        error
}

func (*authRepositoryStub) BootstrapOwner(context.Context, ports.BootstrapOwner) error { return nil }
func (s *authRepositoryStub) FindLoginCandidates(context.Context, string) ([]ports.LoginCandidate, error) {
	return s.candidates, s.candidateErr
}
func (s *authRepositoryStub) CreateSession(_ context.Context, session ports.SessionRecord) error {
	s.createdSession = session
	return s.createSessionErr
}
func (s *authRepositoryStub) FindSession(context.Context, string) (ports.SessionPrincipal, error) {
	return s.principal, s.principalErr
}
func (s *authRepositoryStub) TouchSession(_ context.Context, tenantID, sessionID string, _ time.Time) error {
	s.touchedTenant, s.touchedSession = tenantID, sessionID
	return s.touchErr
}
func (s *authRepositoryStub) RevokeSession(_ context.Context, tenantID, sessionID string, _ time.Time) error {
	s.revokedTenant, s.revokedSession = tenantID, sessionID
	return s.revokeErr
}

type authHasherStub struct{ hashErr error }

func (s authHasherStub) Hash([]byte) (string, error) { return "dummy-hash", s.hashErr }
func (authHasherStub) Verify(password []byte, _ string) (bool, error) {
	return string(password) == "correct-password", nil
}

type authTokenStub struct{ index int }

func (s *authTokenStub) NewToken() (string, string, error) {
	s.index++
	raw := "token-" + string(rune('0'+s.index))
	return raw, s.Hash(raw), nil
}
func (*authTokenStub) Hash(raw string) string { return "hash:" + raw }

type authIDStub struct{ err error }

func (s authIDStub) NewID() (string, error) { return "session-id", s.err }

type authClockStub struct{ now time.Time }

func (s authClockStub) Now() time.Time { return s.now }

func TestAuthenticationServiceBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	if _, err := NewService(&authRepositoryStub{}, authHasherStub{}, &authTokenStub{}, authIDStub{}, authClockStub{now}, time.Minute); err == nil {
		t.Fatal("short session TTL accepted")
	}
	if _, err := NewService(&authRepositoryStub{}, authHasherStub{hashErr: errors.New("hash")}, &authTokenStub{}, authIDStub{}, authClockStub{now}, time.Hour); err == nil {
		t.Fatal("dummy hash failure ignored")
	}

	repository := &authRepositoryStub{}
	tokens := &authTokenStub{}
	service, err := NewService(repository, authHasherStub{}, tokens, authIDStub{}, authClockStub{now}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(context.Background(), LoginInput{Email: "missing@example.test", Password: []byte("correct-password")}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("missing user error = %v", err)
	}
	candidate := ports.LoginCandidate{
		UserID: "user", Email: "owner@example.test", DisplayName: "Owner", PasswordHash: "stored",
		TenantID: "tenant-a", TenantName: "A", Currency: domain.CurrencyCNY, Timezone: "Asia/Shanghai",
		Role: domain.RoleOwner, Status: "active",
	}
	repository.candidates = []ports.LoginCandidate{candidate}
	if _, err := service.Login(context.Background(), LoginInput{Email: candidate.Email, Password: []byte("wrong-password")}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("wrong password error = %v", err)
	}
	second := candidate
	second.TenantID, second.TenantName = "tenant-b", "B"
	repository.candidates = []ports.LoginCandidate{candidate, second}
	if _, err := service.Login(context.Background(), LoginInput{Email: candidate.Email, Password: []byte("correct-password")}); !errors.Is(err, domain.ErrTenantRequired) {
		t.Fatalf("tenant selection error = %v", err)
	}
	view, err := service.Login(context.Background(), LoginInput{Email: candidate.Email, Password: []byte("correct-password"), TenantID: second.TenantID})
	if err != nil {
		t.Fatal(err)
	}
	if view.TenantID != second.TenantID || repository.createdSession.TenantID != second.TenantID || view.SessionToken == "" || view.CSRFToken == "" {
		t.Fatalf("login view/session = %#v / %#v", view, repository.createdSession)
	}
	repository.candidates = []ports.LoginCandidate{{Status: "suspended", PasswordHash: "stored"}}
	if _, err := service.Login(context.Background(), LoginInput{Password: []byte("correct-password")}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("suspended login error = %v", err)
	}

	if _, err := service.Authenticate(context.Background(), ""); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("empty token error = %v", err)
	}
	repository.principal = ports.SessionPrincipal{SessionID: "session", TenantID: "tenant", UserID: "user", Role: domain.RoleOwner, ExpiresAt: now.Add(time.Hour), CSRFTokenHash: "hash:csrf"}
	principal, err := service.Authenticate(context.Background(), "raw")
	if err != nil || principal.SessionID != "session" || repository.touchedSession != "session" {
		t.Fatalf("authenticate = %#v, error=%v", principal, err)
	}
	revoked := now
	repository.principal.RevokedAt = &revoked
	if _, err := service.Authenticate(context.Background(), "raw"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("revoked token error = %v", err)
	}
	repository.principal.RevokedAt = nil
	repository.principal.ExpiresAt = now
	if _, err := service.Authenticate(context.Background(), "raw"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("expired token error = %v", err)
	}
	repository.principal.ExpiresAt = now.Add(time.Hour)
	if err := service.VerifyCSRF(repository.principal, "csrf"); err != nil {
		t.Fatalf("valid CSRF error = %v", err)
	}
	if err := service.VerifyCSRF(repository.principal, "wrong"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("wrong CSRF error = %v", err)
	}
	if err := service.Logout(context.Background(), repository.principal); err != nil || repository.revokedSession != "session" {
		t.Fatalf("logout error=%v session=%s", err, repository.revokedSession)
	}
}
