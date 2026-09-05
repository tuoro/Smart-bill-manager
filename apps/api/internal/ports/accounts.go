package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type AccountActor struct{ TenantID, UserID, SessionID string }
type AccountUser struct{ ID, Email, DisplayName, PasswordHash string }

type Member struct {
	UserID      string      `json:"user_id"`
	Email       string      `json:"email"`
	DisplayName string      `json:"display_name"`
	Role        domain.Role `json:"role"`
	Status      string      `json:"status"`
	Version     int         `json:"version"`
	CreatedAt   time.Time   `json:"created_at"`
}

type Invitation struct {
	ID         string      `json:"id"`
	TenantID   string      `json:"-"`
	TenantName string      `json:"-"`
	Email      string      `json:"email"`
	Role       domain.Role `json:"role"`
	Status     string      `json:"status"`
	Version    int         `json:"version"`
	ExpiresAt  time.Time   `json:"expires_at"`
	CreatedAt  time.Time   `json:"created_at"`
}

type AccountPageQuery struct {
	Limit int
	After *domain.FactSortKey
}
type MemberPage struct {
	Items []Member
	Next  *domain.FactSortKey
}
type InvitationPage struct {
	Items []Invitation
	Next  *domain.FactSortKey
}

type CreateInvitationCommand struct {
	Actor                                                              AccountActor
	ID, Email                                                          string
	Role                                                               domain.Role
	TokenHash, Reason, IdempotencyKey, RequestHash, AuditID, RequestID string
	CreatedAt, ExpiresAt                                               time.Time
}
type InvitationCreated struct {
	Invitation Invitation `json:"invitation"`
	Replayed   bool       `json:"replayed"`
	Code       string     `json:"code"`
}
type RevokeInvitationCommand struct {
	Actor                                    AccountActor
	InvitationID, Reason, AuditID, RequestID string
	ExpectedVersion                          int
	Now                                      time.Time
}
type ConsumeInvitationCommand struct {
	TenantID, InvitationID, TokenHash                           string
	ExpectedUserID, ExpectedPasswordHash                        string
	NewUserID, NewPasswordHash, DisplayName, AuditID, RequestID string
	Now                                                         time.Time
}
type ChangeMemberCommand struct {
	Actor                              AccountActor
	UserID                             string
	Role                               domain.Role
	Status, Reason, AuditID, RequestID string
	ExpectedVersion                    int
	Now                                time.Time
}
type ChangePasswordCommand struct {
	UserID, ExpectedPasswordHash, NewPasswordHash string
	SessionID, TenantID                           string
	Action, Reason, EventID                       string
	Now                                           time.Time
}

type AccountRepository interface {
	GetMember(context.Context, AccountActor, string) (Member, error)
	GetInvitation(context.Context, AccountActor, string, time.Time) (Invitation, error)
	ListMembers(context.Context, AccountActor, AccountPageQuery, time.Time) (MemberPage, error)
	ListInvitations(context.Context, AccountActor, AccountPageQuery, time.Time) (InvitationPage, error)
	FindAccountByEmail(context.Context, string) (AccountUser, error)
	FindAccountByID(context.Context, string) (AccountUser, error)
	FindInvitation(context.Context, string, time.Time) (Invitation, error)
	CreateInvitation(context.Context, CreateInvitationCommand) (InvitationCreated, error)
	RevokeInvitation(context.Context, RevokeInvitationCommand) (Invitation, error)
	ConsumeInvitation(context.Context, ConsumeInvitationCommand) error
	ChangeMember(context.Context, ChangeMemberCommand) (Member, error)
	ChangePassword(context.Context, ChangePasswordCommand) error
}
