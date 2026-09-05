package accounts_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestAccountUpgradePreservesIdentitiesAndSessionsAndRollsBack(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	current := config.MigrationsDir
	config.MigrationsDir = t.TempDir()
	copyMigration := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(current, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(config.MigrationsDir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, name := range []string{"0001_initial.sql", "0002_manual_trip_workspaces.sql", "0003_explicit_manual_review.sql", "0004_confirmed_fact_corrections.sql", "0005_fact_management_indexes.sql", "0006_invoice_supporting_materials.sql"} {
		copyMigration(name)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for _, statement := range []string{
		`INSERT INTO users(id,email,password_hash,display_name,created_at,updated_at) VALUES('synthetic-user','upgrade@example.invalid','synthetic-unused-hash','合成用户',$1,$1)`,
		`INSERT INTO tenants(id,name,default_currency,timezone,created_at,updated_at) VALUES('synthetic-tenant','合成工作区','CNY','UTC',$1,$1)`,
		`INSERT INTO memberships(tenant_id,user_id,role,status,created_at,updated_at) VALUES('synthetic-tenant','synthetic-user','owner','active',$1,$1)`,
		`INSERT INTO sessions(id,tenant_id,user_id,token_hash,csrf_token_hash,expires_at,created_at,last_seen_at) VALUES('synthetic-session','synthetic-tenant','synthetic-user',repeat('a',64),repeat('b',64),$1::timestamptz+interval '1 hour',$1,$1)`,
	} {
		if _, err := store.DB().Exec(statement, now); err != nil {
			t.Fatal(err)
		}
	}
	const snapshotSQL = `SELECT jsonb_build_object('users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),'tenants',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM tenants t),'members',(SELECT jsonb_agg(to_jsonb(m)-'version' ORDER BY user_id) FROM memberships m),'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s))::text`
	var before string
	if err := store.DB().QueryRow(snapshotSQL).Scan(&before); err != nil {
		t.Fatal(err)
	}
	name := "0007_member_account_lifecycle.sql"
	data := copyMigration(name)
	broken := append(append([]byte{}, data...), []byte("\nDO $$ BEGIN RAISE EXCEPTION 'synthetic_account_upgrade_failure'; END; $$;")...)
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, name), broken, 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err == nil {
		t.Fatal("broken upgrade accepted")
	}
	var ledger int
	var table *string
	if err := store.DB().QueryRow(`SELECT (SELECT count(*) FROM schema_migrations),to_regclass('public.member_invitations')::text`).Scan(&ledger, &table); err != nil || ledger != 6 || table != nil {
		t.Fatal("failed migration did not roll back")
	}
	copyMigration(name)
	for range 2 {
		if err := postgres.Migrate(ctx, config); err != nil {
			t.Fatal(err)
		}
	}
	var after string
	if err := store.DB().QueryRow(snapshotSQL).Scan(&after); err != nil || before != after {
		t.Fatal("upgrade changed existing identity or session")
	}
	var version, additions int
	if err := store.DB().QueryRow(`SELECT version,(SELECT count(*) FROM member_invitations)+(SELECT count(*) FROM account_events) FROM memberships`).Scan(&version, &additions); err != nil || version != 1 || additions != 0 {
		t.Fatal("upgrade fabricated lifecycle history")
	}
}
