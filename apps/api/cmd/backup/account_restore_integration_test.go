//go:build postgresql_tools

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/accounts"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/bootstrap"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestAccountAuthenticatedRestorePreservesHistoryAndInvalidatesCredentials(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hasher, err := cryptography.NewPasswordHasher(cryptography.Argon2Params{MemoryKiB: 8192, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	if err != nil {
		t.Fatal(err)
	}
	const oldPassword = "synthetic-restore-old-password"
	const newPassword = "synthetic-restore-new-password"
	_, err = bootstrap.NewService(store, hasher, system.IDGenerator{}, system.Clock{}).Execute(ctx, bootstrap.Input{Email: "owner@example.invalid", Password: []byte(oldPassword), DisplayName: "合成管理员", TenantName: "合成恢复工作区", DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	login, err := auth.NewService(store, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	view, err := login.Login(ctx, auth.LoginInput{Email: "owner@example.invalid", Password: []byte(oldPassword)})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := login.Authenticate(ctx, view.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	service := accounts.NewService(store, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{})
	invite := func(email, key string) ports.InvitationCreated {
		t.Helper()
		result, err := service.Invite(ctx, owner, accounts.InviteInput{Email: email, Role: domain.RoleViewer, Reason: "合成恢复邀请", IdempotencyKey: key}, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	pending := invite("pending@example.invalid", "synthetic-pending")
	consumed := invite("consumed@example.invalid", "synthetic-consumed")
	if err := service.Join(ctx, consumed.Code, "合成成员", []byte(oldPassword), "synthetic"); err != nil {
		t.Fatal(err)
	}
	revoked := invite("revoked@example.invalid", "synthetic-revoked")
	if _, err := service.Revoke(ctx, owner, revoked.Invitation.ID, 1, "合成撤销", "synthetic"); err != nil {
		t.Fatal(err)
	}
	if err := service.ChangePassword(ctx, owner, []byte(oldPassword), []byte(newPassword)); err != nil {
		t.Fatal(err)
	}
	view, err = login.Login(ctx, auth.LoginInput{Email: "owner@example.invalid", Password: []byte(newPassword)})
	if err != nil {
		t.Fatal(err)
	}
	const historySQL = `SELECT jsonb_build_object('users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),'members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY user_id) FROM memberships m),'events',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM account_events a),'invitations',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM member_invitations i WHERE version=2))::text`
	var before string
	if err := store.DB().QueryRow(historySQL).Scan(&before); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	objects := filepath.Join(root, "source-objects")
	if _, err := localstorage.New(objects); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(root, "source-master")
	if err := os.WriteFile(master, key, 0600); err != nil {
		t.Fatal(err)
	}
	clear(key)
	setTripBackupEnvironment(t, config, false)
	backupPath := filepath.Join(root, "backup")
	manifest, err := createBackup(ctx, backupOptions{Objects: objects, MasterKey: master, Migrations: config.MigrationsDir, Output: backupPath, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Database.TableCounts["member_invitations"] != 3 || manifest.Database.TableCounts["account_events"] != 1 || manifest.Database.TableCounts["sessions"] != 2 {
		t.Fatal("account backup inventory incomplete")
	}
	restoredConfig := postgresqltest.NewDatabase(t)
	setTripBackupEnvironment(t, restoredConfig, true)
	secrets := filepath.Join(root, "restored-secrets")
	if err := os.Mkdir(secrets, 0700); err != nil {
		t.Fatal(err)
	}
	_, invalidated, err := restoreBackup(ctx, restoreOptions{Backup: backupPath, MasterKeySource: master, Migrations: config.MigrationsDir, Objects: filepath.Join(root, "restored-objects"), MasterKey: filepath.Join(secrets, "master"), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	if invalidated != 2 {
		t.Fatal("restore did not remove all captured sessions")
	}
	restored, err := postgres.Open(ctx, restoredConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	// 已消费/撤销历史必须保持；本次恢复撤销的 pending 记录单独核对。
	var after string
	query := `SELECT jsonb_build_object('users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),'members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY user_id) FROM memberships m),'events',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM account_events a),'invitations',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM member_invitations i WHERE id<>?))::text`
	if err := restored.DB().QueryRow(query, pending.Invitation.ID).Scan(&after); err != nil || before != after {
		t.Fatal("restored account history changed")
	}
	var revokedByRestore bool
	if err := restored.DB().QueryRow(`SELECT version=2 AND revoked_at IS NOT NULL AND revoked_by_user_id IS NULL AND revoke_reason='restore' FROM member_invitations WHERE id=?`, pending.Invitation.ID).Scan(&revokedByRestore); err != nil || !revokedByRestore {
		t.Fatal("pending invitation revived after restore")
	}
	restoredService := accounts.NewService(restored, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{})
	if _, err := restoredService.CheckInvitation(ctx, pending.Code); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("pre-restore code remains usable")
	}
	restoredLogin, err := auth.NewService(restored, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restoredLogin.Authenticate(ctx, view.SessionToken); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("pre-restore cookie remains usable")
	}
	if _, err := restoredLogin.Login(ctx, auth.LoginInput{Email: "owner@example.invalid", Password: []byte(newPassword)}); err != nil {
		t.Fatal("restored global identity cannot login")
	}
}
