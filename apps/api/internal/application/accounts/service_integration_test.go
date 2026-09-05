package accounts_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	postgresqladapter "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/accounts"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/bootstrap"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

const syntheticPassword = "synthetic-account-password"
const syntheticNextPassword = "synthetic-next-account-password"

type fixture struct {
	store      *postgresqladapter.Store
	service    accounts.Service
	auth       auth.Service
	hasher     cryptography.PasswordHasher
	owner      ports.SessionPrincipal
	ownerToken string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	store := postgresqltest.Open(t)
	hasher, err := cryptography.NewPasswordHasher(cryptography.Argon2Params{MemoryKiB: 8192, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bootstrap.NewService(store, hasher, system.IDGenerator{}, system.Clock{}).Execute(context.Background(), bootstrap.Input{
		Email: "owner@example.invalid", Password: []byte(syntheticPassword), DisplayName: "合成管理员", TenantName: "合成工作区",
		DefaultCurrency: domain.CurrencyCNY, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	authService, err := auth.NewService(store, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{store: store, auth: authService, hasher: hasher,
		service: accounts.NewService(store, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{})}
	view, err := f.auth.Login(context.Background(), auth.LoginInput{Email: "owner@example.invalid", Password: []byte(syntheticPassword)})
	if err != nil {
		t.Fatal(err)
	}
	f.ownerToken = view.SessionToken
	f.owner, err = f.auth.Authenticate(context.Background(), view.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) invite(t *testing.T, owner ports.SessionPrincipal, email string, role domain.Role) ports.InvitationCreated {
	t.Helper()
	key, err := (system.IDGenerator{}).NewID()
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Invite(context.Background(), owner, accounts.InviteInput{Email: email, Role: role, Reason: "合成成员验收", IdempotencyKey: key}, "synthetic-request")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func (f fixture) join(t *testing.T, email string, role domain.Role) (ports.SessionPrincipal, string) {
	t.Helper()
	invite := f.invite(t, f.owner, email, role)
	if err := f.service.Join(context.Background(), invite.Code, "合成成员", []byte(syntheticPassword), "synthetic-join"); err != nil {
		t.Fatal(err)
	}
	view, err := f.auth.Login(context.Background(), auth.LoginInput{Email: email, Password: []byte(syntheticPassword), TenantID: f.owner.TenantID})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := f.auth.Authenticate(context.Background(), view.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	return principal, view.SessionToken
}

func count(t *testing.T, f fixture, table string) int {
	t.Helper()
	var total int
	// 调用方仅提供本文件固定表名，不接收外部输入。
	if err := f.store.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}

func TestInvitationCreatesSecondMemberWithoutReplacingIdentity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	input := accounts.InviteInput{Email: " FINANCE@EXAMPLE.INVALID ", Role: domain.RoleFinance, Reason: "合成邀请", IdempotencyKey: "synthetic-idempotency"}
	invite, err := f.service.Invite(ctx, f.owner, input, "synthetic-request")
	if err != nil || !domain.ValidInvitationToken(invite.Code) {
		t.Fatal("invitation creation failed")
	}
	view, err := f.service.CheckInvitation(ctx, invite.Code)
	if err != nil || view.ExistingAccount || view.Role != domain.RoleFinance || view.Email != "finance@example.invalid" {
		t.Fatal("invitation view mismatch")
	}
	replay, err := f.service.Invite(ctx, f.owner, input, "synthetic-replay")
	if err != nil || !replay.Replayed || replay.Code != "" || replay.Invitation.ID != invite.Invitation.ID {
		t.Fatal("invitation replay disclosed or duplicated code")
	}
	input.Role = domain.RoleOwner
	if _, err := f.service.Invite(ctx, f.owner, input, "synthetic-replay"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("changed replay accepted")
	}
	if err := f.service.Join(ctx, invite.Code, "合成财务", []byte("short"), "synthetic-join"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("short password accepted")
	}
	beforeSessions := count(t, f, "sessions")
	if err := f.service.Join(ctx, invite.Code, "合成财务", []byte(syntheticPassword), "synthetic-join"); err != nil {
		t.Fatal(err)
	}
	if count(t, f, "memberships") != 2 || count(t, f, "users") != 2 || count(t, f, "sessions") != beforeSessions {
		t.Fatal("join did not create exact identity without session")
	}
	if err := f.service.Join(ctx, invite.Code, "合成财务", []byte(syntheticPassword), "synthetic-again"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("used code accepted")
	}
	page, err := f.service.Members(ctx, f.owner, "", 20)
	if err != nil || len(page.Items) != 2 {
		t.Fatal("second member missing")
	}
	if _, err := f.service.Invite(ctx, f.owner, accounts.InviteInput{Email: "finance@example.invalid", Role: domain.RoleViewer, Reason: "合成重复", IdempotencyKey: "synthetic-duplicate"}, "synthetic"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("existing member role overwritten through invitation")
	}
}

func TestMemberRoleSuspensionRestorationAndLastOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	member, token := f.join(t, "finance@example.invalid", domain.RoleFinance)
	if _, err := f.service.Members(ctx, member, "", 20); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("non-owner listed members")
	}
	if _, err := f.service.Invite(ctx, member, accounts.InviteInput{}, "synthetic"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("non-owner invited")
	}
	changed, err := f.service.ChangeMember(ctx, f.owner, member.UserID, accounts.MemberChange{Role: domain.RoleReviewer, Status: "suspended", ExpectedVersion: 1, Reason: "合成停用"}, "synthetic")
	if err != nil || changed.Version != 2 {
		t.Fatal("member suspension failed")
	}
	if _, err := f.auth.Authenticate(ctx, token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("suspended session accepted")
	}
	if _, err := f.service.ChangeMember(ctx, f.owner, member.UserID, accounts.MemberChange{Role: domain.RoleReviewer, Status: "active", ExpectedVersion: 1, Reason: "合成陈旧"}, "synthetic"); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatal("stale member version accepted")
	}
	_, err = f.service.ChangeMember(ctx, f.owner, member.UserID, accounts.MemberChange{Role: domain.RoleReviewer, Status: "active", ExpectedVersion: 2, Reason: "合成恢复"}, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.Authenticate(ctx, token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("restoration revived old cookie")
	}
	view, err := f.auth.Login(ctx, auth.LoginInput{Email: member.Email, Password: []byte(syntheticPassword)})
	if err != nil || view.Role != domain.RoleReviewer {
		t.Fatal("restored member did not receive current role")
	}
	before := count(t, f, "audit_events")
	_, err = f.service.ChangeMember(ctx, f.owner, f.owner.UserID, accounts.MemberChange{Role: domain.RoleViewer, Status: "active", ExpectedVersion: 1, Reason: "合成最后管理员"}, "synthetic")
	if !errors.Is(err, domain.ErrConflict) || count(t, f, "audit_events") != before {
		t.Fatal("last-owner failure did not fully roll back")
	}
}

func TestConcurrentOwnersCannotLeaveWorkspaceOwnerless(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprint(direct), func(t *testing.T) {
			f := newFixture(t)
			second, _ := f.join(t, "owner-two@example.invalid", domain.RoleOwner)
			start := make(chan struct{})
			results := make(chan error, 2)
			var group sync.WaitGroup
			for _, owner := range []ports.SessionPrincipal{f.owner, second} {
				group.Add(1)
				go func(owner ports.SessionPrincipal) {
					defer group.Done()
					<-start
					if direct {
						_, err := f.store.DB().Exec(`UPDATE memberships SET status = 'suspended' WHERE tenant_id = ? AND user_id = ?`, owner.TenantID, owner.UserID)
						results <- err
					} else {
						_, err := f.service.ChangeMember(context.Background(), owner, owner.UserID, accounts.MemberChange{Role: domain.RoleOwner, Status: "suspended", ExpectedVersion: 1, Reason: "合成并发"}, "synthetic")
						results <- err
					}
				}(owner)
			}
			close(start)
			group.Wait()
			close(results)
			succeeded := 0
			for err := range results {
				if err == nil {
					succeeded++
				}
			}
			var active int
			if err := f.store.DB().QueryRow(`SELECT count(*) FROM memberships WHERE role = 'owner' AND status = 'active'`).Scan(&active); err != nil {
				t.Fatal(err)
			}
			if active != 1 || succeeded != 1 {
				t.Fatalf("active owners=%d successful changes=%d", active, succeeded)
			}
		})
	}
}

type heldLoginRepository struct {
	ports.IdentityRepository
	reached, release chan struct{}
}

func (r heldLoginRepository) CreateSession(ctx context.Context, session ports.SessionRecord) error {
	close(r.reached)
	select {
	case <-r.release:
		return r.IdentityRepository.CreateSession(ctx, session)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPasswordChangeClosesVerifiedLoginRace(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	repository := heldLoginRepository{IdentityRepository: f.store, reached: make(chan struct{}), release: make(chan struct{})}
	login, err := auth.NewService(repository, f.hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := login.Login(ctx, auth.LoginInput{Email: f.owner.Email, Password: []byte(syntheticPassword)})
		result <- err
	}()
	<-repository.reached
	if err := f.service.ChangePassword(ctx, f.owner, []byte(syntheticPassword), []byte(syntheticNextPassword)); err != nil {
		close(repository.release)
		t.Fatal(err)
	}
	close(repository.release)
	if err := <-result; !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("old verified password created a later session")
	}
	if _, err := f.auth.Authenticate(ctx, f.ownerToken); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("password change did not revoke current cookie")
	}
	if _, err := f.auth.Login(ctx, auth.LoginInput{Email: f.owner.Email, Password: []byte(syntheticPassword)}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("old password accepted")
	}
	if _, err := f.auth.Login(ctx, auth.LoginInput{Email: f.owner.Email, Password: []byte(syntheticNextPassword)}); err != nil {
		t.Fatal(err)
	}
	if count(t, f, "account_events") != 1 {
		t.Fatal("password change audit missing")
	}
}
