package accounts_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/accounts"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func otherWorkspace(t *testing.T, f fixture) ports.SessionPrincipal {
	t.Helper()
	id, err := (system.IDGenerator{}).NewID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := f.store.DB().Exec(`INSERT INTO tenants (id,name,default_currency,timezone,created_at,updated_at) VALUES (?, '合成其他工作区','CNY','UTC',?,?)`, id, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().Exec(`INSERT INTO memberships (tenant_id,user_id,role,status,created_at,updated_at) VALUES (?,?,'owner','active',?,?)`, id, f.owner.UserID, now, now); err != nil {
		t.Fatal(err)
	}
	view, err := f.auth.Login(context.Background(), auth.LoginInput{Email: f.owner.Email, Password: []byte(syntheticPassword), TenantID: id})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := f.auth.Authenticate(context.Background(), view.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func TestSharedAccountJoinAndGlobalPasswordRevocation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	member, firstToken := f.join(t, "shared@example.invalid", domain.RoleFinance)
	before, err := f.store.FindAccountByID(ctx, member.UserID)
	if err != nil {
		t.Fatal(err)
	}
	other := otherWorkspace(t, f)
	invite := f.invite(t, other, member.Email, domain.RoleViewer)
	view, err := f.service.CheckInvitation(ctx, invite.Code)
	if err != nil || !view.ExistingAccount {
		t.Fatal("shared global identity not recognized")
	}
	if err := f.service.Join(ctx, invite.Code, "", []byte("synthetic-wrong-password"), "synthetic"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("existing account accepted replacement password")
	}
	if err := f.service.Join(ctx, invite.Code, "replace-name", []byte(syntheticPassword), "synthetic"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("existing global profile replacement accepted")
	}
	if err := f.service.Join(ctx, invite.Code, "", []byte(syntheticPassword), "synthetic"); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.FindAccountByID(ctx, member.UserID)
	if err != nil || before != after {
		t.Fatal("joining another workspace altered global credentials/profile")
	}
	if _, err := f.auth.Login(ctx, auth.LoginInput{Email: member.Email, Password: []byte(syntheticPassword)}); !errors.Is(err, domain.ErrTenantRequired) {
		t.Fatal("multiple workspaces silently selected")
	}
	choices, err := f.auth.Workspaces(ctx, auth.LoginInput{Email: member.Email, Password: []byte(syntheticPassword)})
	if err != nil || len(choices) != 2 {
		t.Fatal("verified workspace choices missing")
	}
	if _, err := f.auth.Workspaces(ctx, auth.LoginInput{Email: member.Email, Password: []byte("synthetic-wrong-password")}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("workspace enumeration without credentials")
	}
	second, err := f.auth.Login(ctx, auth.LoginInput{Email: member.Email, Password: []byte(syntheticPassword), TenantID: other.TenantID})
	if err != nil || second.Role != domain.RoleViewer {
		t.Fatal("explicit second workspace login failed")
	}
	if err := f.service.ChangePassword(ctx, member, []byte("synthetic-wrong-password"), []byte(syntheticNextPassword)); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("wrong current password accepted")
	}
	if err := f.service.ChangePassword(ctx, member, []byte(syntheticPassword), []byte(syntheticNextPassword)); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{firstToken, second.SessionToken} {
		if _, err := f.auth.Authenticate(ctx, token); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("cross-workspace session survived password change")
		}
	}
	if count(t, f, "account_events") != 1 {
		t.Fatal("global account audit duplicated or missing")
	}
	if _, err := f.store.DB().Exec(`UPDATE account_events SET reason='changed'`); err == nil {
		t.Fatal("global account audit mutable")
	}
}

func TestSuspendedGlobalAccountCanAcceptNewWorkspaceOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	member, _ := f.join(t, "suspended@example.invalid", domain.RoleViewer)
	_, err := f.service.ChangeMember(ctx, f.owner, member.UserID, accounts.MemberChange{Role: domain.RoleViewer, Status: "suspended", ExpectedVersion: 1, Reason: "合成停用"}, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	other := otherWorkspace(t, f)
	invite := f.invite(t, other, member.Email, domain.RoleReviewer)
	if err := f.service.Join(ctx, invite.Code, "", []byte(syntheticPassword), "synthetic"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := f.store.DB().QueryRow(`SELECT status FROM memberships WHERE tenant_id=? AND user_id=?`, f.owner.TenantID, member.UserID).Scan(&status); err != nil || status != "suspended" {
		t.Fatal("joining new workspace restored old membership")
	}
}

func TestInvitationRevocationExpiryAndTenantBoundaries(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	invite := f.invite(t, f.owner, "revoked@example.invalid", domain.RoleViewer)
	other := otherWorkspace(t, f)
	if _, err := f.service.Revoke(ctx, other, invite.Invitation.ID, 1, "合成越界", "synthetic"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-tenant invitation revoked")
	}
	if _, err := f.service.Revoke(ctx, f.owner, invite.Invitation.ID, 2, "合成陈旧", "synthetic"); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatal("stale invitation version accepted")
	}
	if _, err := f.service.Revoke(ctx, f.owner, invite.Invitation.ID, 1, "合成撤销", "synthetic"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CheckInvitation(ctx, invite.Code); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("revoked invitation readable")
	}
	if err := f.service.Join(ctx, invite.Code, "合成成员", []byte(syntheticPassword), "synthetic"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("revoked invitation consumed")
	}
	if _, err := f.service.Members(ctx, f.owner, "malformed", 20); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("malformed cursor accepted")
	}
	if _, err := f.service.Invitations(ctx, f.owner, "", 101); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("unbounded page accepted")
	}
	active := f.invite(t, f.owner, "null-reason@example.invalid", domain.RoleViewer)
	if _, err := f.store.DB().Exec(`UPDATE member_invitations SET version=2, revoked_at=clock_timestamp(), revoke_reason=NULL WHERE id=?`, active.Invitation.ID); err == nil {
		t.Fatal("null revoke reason bypassed CHECK")
	}
}

func TestMemberPagesAreCompleteAndScopeBound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// 历史分页不需要反复执行 Argon2；只生成纯合成已存在身份。
	now := time.Now().UTC()
	for index := range 201 {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", index+1)
		if _, err := f.store.DB().Exec(`INSERT INTO users (id,email,password_hash,display_name,created_at,updated_at) VALUES (?,?, 'synthetic-unused-hash','合成分页成员',?,?)`, id, fmt.Sprintf("page-%d@example.invalid", index), now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.DB().Exec(`INSERT INTO memberships (tenant_id,user_id,role,status,created_at,updated_at) VALUES (?,?,'viewer','active',?,?)`, f.owner.TenantID, id, now, now); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	firstNext := ""
	for {
		page, err := f.service.Members(ctx, f.owner, cursor, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, member := range page.Items {
			if seen[member.UserID] {
				t.Fatal("duplicate paged member")
			}
			seen[member.UserID] = true
		}
		if firstNext == "" {
			firstNext = page.NextCursor
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 202 {
		t.Fatalf("member traversal count=%d", len(seen))
	}
	if _, err := f.service.Invitations(ctx, f.owner, firstNext, 20); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("member cursor accepted by invitation page")
	}
	other := otherWorkspace(t, f)
	if _, err := f.service.Members(ctx, other, firstNext, 20); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("cross-tenant cursor accepted")
	}
}

type heldHasher struct {
	ports.PasswordHasher
	reached, release chan struct{}
}

func (h heldHasher) Hash(value []byte) (string, error) {
	close(h.reached)
	<-h.release
	return h.PasswordHasher.Hash(value)
}

func TestInvitationExpiryIsRecheckedAfterPasswordWork(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code, hash, err := (cryptography.TokenGenerator{}).NewToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	id, _ := (system.IDGenerator{}).NewID()
	auditID, _ := (system.IDGenerator{}).NewID()
	if _, err := f.store.DB().Exec(`INSERT INTO audit_events (id,tenant_id,actor_user_id,action,resource_type,resource_id,request_id,safe_metadata_json,created_at)
		VALUES (?, ?, ?, 'member_invited','member_invitation',?,'synthetic','{}',?)`, auditID, f.owner.TenantID, f.owner.UserID, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().Exec(`INSERT INTO member_invitations (id,tenant_id,email,role,token_hash,created_by_user_id,created_at,expires_at,reason,idempotency_key,request_hash,audit_event_id)
		VALUES (?,?,'short-lived@example.invalid','viewer',?,?,?,?,'合成短期','synthetic-short',?,?)`, id, f.owner.TenantID, hash, f.owner.UserID, now, now.Add(150*time.Millisecond), hash, auditID); err != nil {
		t.Fatal(err)
	}
	hasher := heldHasher{PasswordHasher: f.hasher, reached: make(chan struct{}), release: make(chan struct{})}
	service := accounts.NewService(f.store, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{})
	result := make(chan error, 1)
	go func() {
		result <- service.Join(ctx, code, "合成短期用户", []byte(syntheticPassword), "synthetic")
	}()
	select {
	case <-hasher.reached:
	case <-time.After(time.Second):
		close(hasher.release)
		t.Fatal("password work was not reached before expiry")
	}
	<-time.After(200 * time.Millisecond)
	close(hasher.release)
	if err := <-result; !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("invitation expired during password work was consumed")
	}
	if count(t, f, "users") != 1 || count(t, f, "memberships") != 1 {
		t.Fatal("expired invitation left partial identity")
	}
}

func TestLocalRecoveryRevokesSessionsAndAuditsWithoutOwnerActor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.service.Recover(ctx, "missing", []byte(syntheticNextPassword), "合成恢复"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("unknown recovery target accepted")
	}
	if err := f.service.Recover(ctx, f.owner.UserID, []byte(syntheticNextPassword), "合成本地恢复"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.Authenticate(ctx, f.ownerToken); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("recovery retained old cookie")
	}
	if _, err := f.auth.Login(ctx, auth.LoginInput{Email: f.owner.Email, Password: []byte(syntheticNextPassword)}); err != nil {
		t.Fatal(err)
	}
	var actor string
	if err := f.store.DB().QueryRow(`SELECT actor_kind FROM account_events`).Scan(&actor); err != nil || actor != "local_operator" {
		t.Fatal("recovery audit impersonates a tenant Owner")
	}
}
