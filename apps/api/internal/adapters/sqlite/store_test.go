package sqliteadapter

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestMigrationAndBootstrapOwner(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	owner := ports.BootstrapOwner{
		UserID:          "00000000-0000-4000-8000-000000000001",
		TenantID:        "00000000-0000-4000-8000-000000000002",
		Email:           "owner@example.test",
		PasswordHash:    "$argon2id$test-only",
		DisplayName:     "Owner",
		TenantName:      "Test Tenant",
		DefaultCurrency: domain.CurrencyCNY,
		Timezone:        "Asia/Shanghai",
		CreatedAt:       now,
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatalf("BootstrapOwner() error = %v", err)
	}
	if err := store.BootstrapOwner(ctx, owner); !errors.Is(err, domain.ErrBootstrapNotEmpty) {
		t.Fatalf("second BootstrapOwner() error = %v, want ErrBootstrapNotEmpty", err)
	}
	var users, tenants, memberships int
	if err := store.db.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM users),
		       (SELECT count(*) FROM tenants),
		       (SELECT count(*) FROM memberships)
	`).Scan(&users, &tenants, &memberships); err != nil {
		t.Fatal(err)
	}
	if users != 1 || tenants != 1 || memberships != 1 {
		t.Fatalf("bootstrap counts = %d/%d/%d", users, tenants, memberships)
	}
	if _, err := store.db.ExecContext(ctx, `
		UPDATE memberships SET role = 'viewer'
		WHERE tenant_id = ? AND user_id = ?
	`, owner.TenantID, owner.UserID); err == nil {
		t.Fatal("last active owner demotion unexpectedly succeeded")
	}
}

func TestBootstrapOwnerRollsBackOnInjectedTransactionFailure(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `
		CREATE TEMP TRIGGER fail_tenant_insert
		BEFORE INSERT ON tenants
		BEGIN
			SELECT RAISE(ABORT, 'synthetic_bootstrap_failure');
		END
	`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	if err := store.BootstrapOwner(ctx, ports.BootstrapOwner{
		UserID: "user", TenantID: "tenant", Email: "owner@example.test", PasswordHash: "test-only",
		DisplayName: "Owner", TenantName: "Tenant", DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC", CreatedAt: now,
	}); err == nil {
		t.Fatal("injected bootstrap failure was ignored")
	}
	var users, tenants, memberships int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM users),
		       (SELECT count(*) FROM tenants),
		       (SELECT count(*) FROM memberships)
	`).Scan(&users, &tenants, &memberships); err != nil {
		t.Fatal(err)
	}
	if users != 0 || tenants != 0 || memberships != 0 {
		t.Fatalf("failed bootstrap left rows: %d/%d/%d", users, tenants, memberships)
	}
}

func TestMembershipLastOwnerAndSuspensionInvariants(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	owner := ports.BootstrapOwner{
		UserID: "owner-1", TenantID: "tenant", Email: "one@example.test", PasswordHash: "test-only",
		DisplayName: "One", TenantName: "Tenant", DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC", CreatedAt: now,
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"UPDATE memberships SET status = 'suspended' WHERE tenant_id = 'tenant' AND user_id = 'owner-1'",
		"DELETE FROM memberships WHERE tenant_id = 'tenant' AND user_id = 'owner-1'",
	} {
		if _, err := store.DB().ExecContext(ctx, statement); err == nil {
			t.Fatalf("last owner mutation succeeded: %s", statement)
		}
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at)
		VALUES ('owner-2', 'two@example.test', 'test-only', 'Two', ?, ?);
	`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES ('tenant', 'owner-2', 'owner', 'active', ?, ?)
	`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE memberships SET status = 'suspended' WHERE tenant_id = 'tenant' AND user_id = 'owner-1'
	`); err != nil {
		t.Fatalf("owner suspension with another active owner failed: %v", err)
	}
	candidates, err := store.FindLoginCandidates(ctx, "one@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Status != "suspended" {
		t.Fatalf("suspended membership lookup = %#v", candidates)
	}
	if err := store.CreateSession(ctx, ports.SessionRecord{
		ID: "suspended-session", TenantID: owner.TenantID, UserID: owner.UserID,
		TokenHash: "suspended-token", CSRFTokenHash: "csrf", ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindSession(ctx, "suspended-token"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("suspended membership produced session principal: %v", err)
	}
}

func TestTenantScopedSessionForeignKey(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	owner := ports.BootstrapOwner{
		UserID:          "00000000-0000-4000-8000-000000000011",
		TenantID:        "00000000-0000-4000-8000-000000000012",
		Email:           "owner@example.test",
		PasswordHash:    "$argon2id$test-only",
		DisplayName:     "Owner",
		TenantName:      "Test Tenant",
		DefaultCurrency: domain.CurrencyUSD,
		Timezone:        "UTC",
		CreatedAt:       now,
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	err := store.CreateSession(ctx, ports.SessionRecord{
		ID:            "00000000-0000-4000-8000-000000000013",
		TenantID:      "00000000-0000-4000-8000-000000000099",
		UserID:        owner.UserID,
		TokenHash:     "token-hash",
		CSRFTokenHash: "csrf-hash",
		ExpiresAt:     now.Add(time.Hour),
		CreatedAt:     now,
		LastSeenAt:    now,
	})
	if err == nil {
		t.Fatal("cross-tenant session unexpectedly succeeded")
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	store := openTestStore(t)
	if err := migrate(context.Background(), store.db, migrationsDir(t)); err != nil {
		t.Fatalf("second migration error = %v", err)
	}
	var versions int
	if err := store.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 {
		t.Fatalf("migration versions = %d, want 2", versions)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), Config{
		DatabasePath:  ":memory:",
		MigrationsDir: migrationsDir(t),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../infra/migrations"))
}
