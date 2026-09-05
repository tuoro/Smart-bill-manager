package accounts_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/accounts"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestConcurrentInvitationConsumptionCreatesExactlyOneMembership(t *testing.T) {
	f := newFixture(t)
	invite := f.invite(t, f.owner, "concurrent@example.invalid", domain.RoleFinance)
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- f.service.Join(context.Background(), invite.Code, "合成并发成员", []byte(syntheticPassword), "synthetic")
		}()
	}
	close(start)
	group.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, domain.ErrInvalidInput) && !errors.Is(err, domain.ErrConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 || count(t, f, "users") != 2 || count(t, f, "memberships") != 2 || count(t, f, "audit_events") != 2 {
		t.Fatal("concurrent join created partial/duplicate state")
	}
}

func TestInvitationCapacityHistoryPaginationAndExactReads(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var oldest string
	for index := range 201 {
		invite := f.invite(t, f.owner, fmt.Sprintf("history-%d@example.invalid", index), domain.RoleViewer)
		if index == 0 {
			oldest = invite.Invitation.ID
		}
		if _, err := f.service.Revoke(ctx, f.owner, invite.Invitation.ID, 1, "合成历史", "synthetic"); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := f.service.Invitations(ctx, f.owner, cursor, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate invitation page item")
			}
			seen[item.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 201 {
		t.Fatal("invitation history truncated")
	}
	if item, err := f.service.Invitation(ctx, f.owner, oldest); err != nil || item.Status != "revoked" {
		t.Fatal("off-page exact invitation unavailable")
	}
	other := otherWorkspace(t, f)
	if _, err := f.service.Invitation(ctx, other, oldest); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-workspace exact invitation exposed")
	}
	member, _ := f.join(t, "private@example.invalid", domain.RoleViewer)
	if _, err := f.service.Member(ctx, other, member.UserID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-workspace exact member exposed")
	}
	if _, err := f.service.Member(ctx, member, f.owner.UserID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("non-owner exact member read accepted")
	}
	for index := range domain.MaxPendingInvitations {
		f.invite(t, f.owner, fmt.Sprintf("pending-%d@example.invalid", index), domain.RoleViewer)
	}
	before := count(t, f, "audit_events")
	_, err := f.service.Invite(ctx, f.owner, accounts.InviteInput{Email: "excess@example.invalid", Role: domain.RoleViewer, Reason: "合成上限", IdempotencyKey: "synthetic-limit-test"}, "synthetic")
	if !errors.Is(err, domain.ErrConflict) || count(t, f, "audit_events") != before {
		t.Fatal("pending cap failure did not roll back")
	}
}

func TestMemberChangeClosesVerifiedLoginRace(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	member, _ := f.join(t, "race@example.invalid", domain.RoleFinance)
	repository := heldLoginRepository{IdentityRepository: f.store, reached: make(chan struct{}), release: make(chan struct{})}
	login, err := auth.NewService(repository, f.hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := login.Login(ctx, auth.LoginInput{Email: member.Email, Password: []byte(syntheticPassword)})
		result <- err
	}()
	<-repository.reached
	_, err = f.service.ChangeMember(ctx, f.owner, member.UserID, accounts.MemberChange{Role: domain.RoleViewer, Status: "active", ExpectedVersion: 1, Reason: "合成降权"}, "synthetic")
	close(repository.release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("stale role proof created session")
	}
}

func TestAccountAuditFailureRollsBackPasswordAndSessionRevocation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.store.DB().Exec(`CREATE FUNCTION synthetic_account_event_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic_failure'; END; $$; CREATE TRIGGER synthetic_account_event_failure BEFORE INSERT ON account_events FOR EACH ROW EXECUTE FUNCTION synthetic_account_event_failure()`); err != nil {
		t.Fatal(err)
	}
	before, err := f.store.FindAccountByID(ctx, f.owner.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.ChangePassword(ctx, f.owner, []byte(syntheticPassword), []byte(syntheticNextPassword)); err == nil {
		t.Fatal("audit failure was swallowed")
	}
	after, err := f.store.FindAccountByID(ctx, f.owner.UserID)
	if err != nil || before != after || count(t, f, "account_events") != 0 {
		t.Fatal("failed audit left password mutation")
	}
	if _, err := f.auth.Authenticate(ctx, f.ownerToken); err != nil {
		t.Fatal("failed transaction revoked valid session")
	}
}
