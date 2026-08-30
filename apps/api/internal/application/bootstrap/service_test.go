package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type bootstrapRepositoryStub struct {
	owner ports.BootstrapOwner
	err   error
}

func (s *bootstrapRepositoryStub) BootstrapOwner(_ context.Context, owner ports.BootstrapOwner) error {
	s.owner = owner
	return s.err
}
func (*bootstrapRepositoryStub) FindLoginCandidates(context.Context, string) ([]ports.LoginCandidate, error) {
	return nil, nil
}
func (*bootstrapRepositoryStub) CreateSession(context.Context, ports.SessionRecord) error { return nil }
func (*bootstrapRepositoryStub) FindSession(context.Context, string) (ports.SessionPrincipal, error) {
	return ports.SessionPrincipal{}, nil
}
func (*bootstrapRepositoryStub) TouchSession(context.Context, string, string, time.Time) error {
	return nil
}
func (*bootstrapRepositoryStub) RevokeSession(context.Context, string, string, time.Time) error {
	return nil
}

type bootstrapHasherStub struct{ err error }

func (s bootstrapHasherStub) Hash([]byte) (string, error)       { return "password-hash", s.err }
func (bootstrapHasherStub) Verify([]byte, string) (bool, error) { return false, nil }

type bootstrapIDStub struct {
	values []string
	errAt  int
	index  int
}

func (s *bootstrapIDStub) NewID() (string, error) {
	s.index++
	if s.errAt == s.index {
		return "", errors.New("id failure")
	}
	return s.values[s.index-1], nil
}

type bootstrapClockStub struct{ now time.Time }

func (s bootstrapClockStub) Now() time.Time { return s.now }

func TestBootstrapValidationAndAtomicBoundary(t *testing.T) {
	base := Input{
		Email: " OWNER@EXAMPLE.TEST ", Password: []byte("strong-password"), DisplayName: " Owner ", TenantName: " Primary ",
		DefaultCurrency: domain.CurrencyCNY, Timezone: "Asia/Shanghai",
	}
	invalid := []Input{
		func() Input { value := base; value.Email = "invalid"; return value }(),
		func() Input { value := base; value.DisplayName = " "; return value }(),
		func() Input { value := base; value.TenantName = " "; return value }(),
		func() Input { value := base; value.DefaultCurrency = domain.Currency("GBP"); return value }(),
		func() Input { value := base; value.Timezone = "Mars/Olympus"; return value }(),
	}
	for _, input := range invalid {
		service := NewService(&bootstrapRepositoryStub{}, bootstrapHasherStub{}, &bootstrapIDStub{values: []string{"user", "tenant"}}, bootstrapClockStub{})
		if _, err := service.Execute(context.Background(), input); err == nil {
			t.Fatalf("invalid input accepted: %#v", input)
		}
	}

	service := NewService(&bootstrapRepositoryStub{}, bootstrapHasherStub{err: errors.New("bad password")}, &bootstrapIDStub{values: []string{"user", "tenant"}}, bootstrapClockStub{})
	if _, err := service.Execute(context.Background(), base); err == nil {
		t.Fatal("password hash failure ignored")
	}
	for _, failure := range []int{1, 2} {
		service = NewService(&bootstrapRepositoryStub{}, bootstrapHasherStub{}, &bootstrapIDStub{values: []string{"user", "tenant"}, errAt: failure}, bootstrapClockStub{})
		if _, err := service.Execute(context.Background(), base); err == nil {
			t.Fatalf("ID failure %d ignored", failure)
		}
	}

	repository := &bootstrapRepositoryStub{err: domain.ErrBootstrapNotEmpty}
	service = NewService(repository, bootstrapHasherStub{}, &bootstrapIDStub{values: []string{"user", "tenant"}}, bootstrapClockStub{})
	if _, err := service.Execute(context.Background(), base); !errors.Is(err, domain.ErrBootstrapNotEmpty) {
		t.Fatalf("non-empty error = %v", err)
	}
	repository.err = nil
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	service = NewService(repository, bootstrapHasherStub{}, &bootstrapIDStub{values: []string{"user", "tenant"}}, bootstrapClockStub{now})
	result, err := service.Execute(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if result.UserID != "user" || result.TenantID != "tenant" || repository.owner.Email != "owner@example.test" || repository.owner.DisplayName != "Owner" || repository.owner.CreatedAt != now {
		t.Fatalf("bootstrap result/record = %#v / %#v", result, repository.owner)
	}
}
