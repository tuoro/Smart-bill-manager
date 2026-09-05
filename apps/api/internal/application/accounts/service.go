package accounts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type Service struct {
	repository ports.AccountRepository
	hasher     ports.PasswordHasher
	tokens     ports.TokenGenerator
	ids        ports.IDGenerator
	clock      ports.Clock
}

func NewService(repository ports.AccountRepository, hasher ports.PasswordHasher, tokens ports.TokenGenerator, ids ports.IDGenerator, clock ports.Clock) Service {
	return Service{repository: repository, hasher: hasher, tokens: tokens, ids: ids, clock: clock}
}

type InviteInput struct {
	Email          string      `json:"email"`
	Role           domain.Role `json:"role"`
	Reason         string      `json:"reason"`
	IdempotencyKey string      `json:"idempotency_key"`
}

func accountActor(principal ports.SessionPrincipal, now time.Time, ownerOnly bool) (ports.AccountActor, error) {
	if principal.SessionID == "" || principal.RevokedAt != nil || !principal.ExpiresAt.After(now) {
		return ports.AccountActor{}, domain.ErrUnauthenticated
	}
	tenant := domain.TenantContext{TenantID: principal.TenantID, UserID: principal.UserID, Role: principal.Role}
	if !principal.Role.Valid() || tenant.UserID == "" || tenant.TenantID == "" {
		return ports.AccountActor{}, domain.ErrUnauthenticated
	}
	if ownerOnly {
		if err := tenant.Require(domain.CapabilityMembersManage); err != nil {
			return ports.AccountActor{}, err
		}
	}
	return ports.AccountActor{TenantID: tenant.TenantID, UserID: tenant.UserID, SessionID: principal.SessionID}, nil
}

func (s Service) Invite(ctx context.Context, principal ports.SessionPrincipal, input InviteInput, requestID string) (ports.InvitationCreated, error) {
	actor, err := accountActor(principal, s.clock.Now(), true)
	if err != nil {
		return ports.InvitationCreated{}, err
	}
	input.Email, err = domain.NormalizeLoginEmail(input.Email)
	if err != nil {
		return ports.InvitationCreated{}, err
	}
	input.Reason, err = domain.NormalizeAccountReason(input.Reason)
	if err != nil {
		return ports.InvitationCreated{}, err
	}
	if !input.Role.Valid() || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 || strings.ContainsAny(input.IdempotencyKey, " \r\n\t") {
		return ports.InvitationCreated{}, domain.ErrInvalidInput
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return ports.InvitationCreated{}, err
	}
	digest := sha256.Sum256(payload)
	id, auditID, err := s.twoIDs()
	if err != nil {
		return ports.InvitationCreated{}, err
	}
	code, hash, err := s.tokens.NewToken()
	if err != nil {
		return ports.InvitationCreated{}, err
	}
	now := s.clock.Now()
	result, err := s.repository.CreateInvitation(ctx, ports.CreateInvitationCommand{
		Actor: actor, ID: id, Email: input.Email, Role: input.Role, TokenHash: hash, Reason: input.Reason,
		IdempotencyKey: input.IdempotencyKey, RequestHash: hex.EncodeToString(digest[:]), AuditID: auditID, RequestID: requestID,
		CreatedAt: now, ExpiresAt: now.Add(48 * time.Hour),
	})
	if err != nil {
		return ports.InvitationCreated{}, err
	}
	if !result.Replayed {
		result.Code = code
	}
	return result, nil
}

func (s Service) Revoke(ctx context.Context, principal ports.SessionPrincipal, id string, version int, reason, requestID string) (ports.Invitation, error) {
	actor, err := accountActor(principal, s.clock.Now(), true)
	if err != nil {
		return ports.Invitation{}, err
	}
	reason, err = domain.NormalizeAccountReason(reason)
	if err != nil {
		return ports.Invitation{}, err
	}
	if id == "" || version < 1 {
		return ports.Invitation{}, domain.ErrInvalidInput
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return ports.Invitation{}, err
	}
	return s.repository.RevokeInvitation(ctx, ports.RevokeInvitationCommand{Actor: actor, InvitationID: id,
		ExpectedVersion: version, Reason: reason, AuditID: auditID, RequestID: requestID, Now: s.clock.Now()})
}

type InvitationView struct {
	TenantName      string      `json:"tenant_name"`
	Email           string      `json:"email"`
	Role            domain.Role `json:"role"`
	ExistingAccount bool        `json:"existing_account"`
	ExpiresAt       time.Time   `json:"expires_at"`
}

func (s Service) CheckInvitation(ctx context.Context, code string) (InvitationView, error) {
	invite, user, err := s.invitationIdentity(ctx, code)
	if err != nil {
		return InvitationView{}, err
	}
	return InvitationView{TenantName: invite.TenantName, Email: invite.Email, Role: invite.Role,
		ExistingAccount: user.ID != "", ExpiresAt: invite.ExpiresAt}, nil
}

func (s Service) invitationIdentity(ctx context.Context, code string) (ports.Invitation, ports.AccountUser, error) {
	if !domain.ValidInvitationToken(code) {
		return ports.Invitation{}, ports.AccountUser{}, domain.InvalidInvitation()
	}
	invite, err := s.repository.FindInvitation(ctx, s.tokens.Hash(code), s.clock.Now())
	if err != nil {
		return invite, ports.AccountUser{}, err
	}
	user, err := s.repository.FindAccountByEmail(ctx, invite.Email)
	if errors.Is(err, domain.ErrNotFound) {
		return invite, ports.AccountUser{}, nil
	}
	return invite, user, err
}

func (s Service) Join(ctx context.Context, code, displayName string, password []byte, requestID string) error {
	invite, user, err := s.invitationIdentity(ctx, code)
	if err != nil {
		return err
	}
	if len(password) > 1024 {
		return domain.InvalidCredentials()
	}
	command := ports.ConsumeInvitationCommand{TenantID: invite.TenantID, InvitationID: invite.ID,
		TokenHash: s.tokens.Hash(code), Now: s.clock.Now(), RequestID: requestID}
	if user.ID != "" {
		if displayName != "" {
			return domain.ErrInvalidInput
		}
		valid, err := s.hasher.Verify(password, user.PasswordHash)
		if err != nil {
			return err
		}
		if !valid {
			return domain.InvalidCredentials()
		}
		command.ExpectedUserID, command.ExpectedPasswordHash = user.ID, user.PasswordHash
	} else {
		command.DisplayName, err = domain.NormalizeAccountName(displayName)
		if err != nil {
			return err
		}
		command.NewPasswordHash, err = s.passwordHash(password)
		if err != nil {
			return err
		}
		command.NewUserID, err = s.ids.NewID()
		if err != nil {
			return err
		}
	}
	command.AuditID, err = s.ids.NewID()
	if err != nil {
		return err
	}
	return s.repository.ConsumeInvitation(ctx, command)
}

type MemberChange struct {
	Role            domain.Role `json:"role"`
	Status          string      `json:"status"`
	ExpectedVersion int         `json:"expected_version"`
	Reason          string      `json:"reason"`
}

func (s Service) ChangeMember(ctx context.Context, principal ports.SessionPrincipal, userID string, input MemberChange, requestID string) (ports.Member, error) {
	actor, err := accountActor(principal, s.clock.Now(), true)
	if err != nil {
		return ports.Member{}, err
	}
	if userID == "" || !input.Role.Valid() || (input.Status != "active" && input.Status != "suspended") || input.ExpectedVersion < 1 {
		return ports.Member{}, domain.ErrInvalidInput
	}
	input.Reason, err = domain.NormalizeAccountReason(input.Reason)
	if err != nil {
		return ports.Member{}, err
	}
	id, err := s.ids.NewID()
	if err != nil {
		return ports.Member{}, err
	}
	return s.repository.ChangeMember(ctx, ports.ChangeMemberCommand{Actor: actor, UserID: userID, Role: input.Role, Status: input.Status,
		ExpectedVersion: input.ExpectedVersion, Reason: input.Reason, AuditID: id, RequestID: requestID, Now: s.clock.Now()})
}

func (s Service) ChangePassword(ctx context.Context, principal ports.SessionPrincipal, current, next []byte) error {
	actor, err := accountActor(principal, s.clock.Now(), false)
	if err != nil {
		return err
	}
	user, err := s.repository.FindAccountByID(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if len(current) > 1024 {
		return domain.InvalidCredentials()
	}
	valid, err := s.hasher.Verify(current, user.PasswordHash)
	if err != nil {
		return err
	}
	if !valid {
		return domain.InvalidCredentials()
	}
	if bytes.Equal(current, next) {
		return domain.NewRuleError("password_unchanged", "新密码不能与当前密码相同", domain.ErrInvalidInput)
	}
	return s.changePassword(ctx, user, next, actor, "password_changed", "本人修改密码")
}

func (s Service) Recover(ctx context.Context, userID string, next []byte, reason string) error {
	if userID == "" {
		return domain.ErrInvalidInput
	}
	reason, err := domain.NormalizeAccountReason(reason)
	if err != nil {
		return err
	}
	user, err := s.repository.FindAccountByID(ctx, userID)
	if err != nil {
		return err
	}
	if len(next) > 1024 {
		return domain.ErrInvalidInput
	}
	same, err := s.hasher.Verify(next, user.PasswordHash)
	if err != nil {
		return err
	}
	if same {
		return domain.NewRuleError("password_unchanged", "恢复密码不能与当前密码相同", domain.ErrInvalidInput)
	}
	return s.changePassword(ctx, user, next, ports.AccountActor{}, "password_recovered", reason)
}

func (s Service) changePassword(ctx context.Context, user ports.AccountUser, next []byte, actor ports.AccountActor, action, reason string) error {
	hash, err := s.passwordHash(next)
	if err != nil {
		return err
	}
	id, err := s.ids.NewID()
	if err != nil {
		return err
	}
	return s.repository.ChangePassword(ctx, ports.ChangePasswordCommand{UserID: user.ID, ExpectedPasswordHash: user.PasswordHash,
		NewPasswordHash: hash, SessionID: actor.SessionID, TenantID: actor.TenantID, Action: action, Reason: reason, EventID: id, Now: s.clock.Now()})
}

func (s Service) passwordHash(value []byte) (string, error) {
	if len(value) < 12 || len(value) > 1024 {
		return "", domain.NewRuleError("invalid_password", "密码必须包含 12–1024 个字节", domain.ErrInvalidInput)
	}
	return s.hasher.Hash(value)
}

func (s Service) twoIDs() (string, string, error) {
	first, err := s.ids.NewID()
	if err != nil {
		return "", "", err
	}
	second, err := s.ids.NewID()
	return first, second, err
}
