package reviews

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestFactManagementIndexUpgradePreservesDataAndRollsBack(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	currentDir := config.MigrationsDir
	config.MigrationsDir = t.TempDir()
	for _, name := range []string{"0001_initial.sql", "0002_manual_trip_workspaces.sql", "0003_explicit_manual_review.sql", "0004_confirmed_fact_corrections.sql"} {
		content, err := os.ReadFile(filepath.Join(currentDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(config.MigrationsDir, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	f := newReviewFixtureInStore(t, store)
	s := NewService(store, store, system.IDGenerator{}, fixedClock{now: f.now})
	r, err := s.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	confirmFactWithoutLinks(t, s, f.tenant, r, "query-upgrade-payment")
	confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, invoiceWithItemsEnvelope("QUERY-UPGRADE"), "query-upgrade-invoice"), "query-upgrade-invoice")
	snapshot := func() string {
		t.Helper()
		var value string
		if err := store.DB().QueryRow(`SELECT jsonb_build_object('payments',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM payments p),'invoices',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM invoices i),'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM invoice_items i),'claims',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM claim_sets c),'origins',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM fact_field_origins o))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	content, err := os.ReadFile(filepath.Join(currentDir, "0005_fact_management_indexes.sql"))
	if err != nil {
		t.Fatal(err)
	}
	broken := append(append([]byte{}, content...), []byte("\nDO $$ BEGIN RAISE EXCEPTION 'synthetic_index_upgrade_failure'; END; $$;")...)
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, "0005_fact_management_indexes.sql"), broken, 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err == nil {
		t.Fatal("injected migration failure succeeded")
	}
	var ledger, indexes int
	const inventory = `SELECT (SELECT count(*) FROM schema_migrations),(SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND indexname IN ('payments_tenant_created_active_idx','invoices_tenant_created_active_idx'))`
	if err := store.DB().QueryRow(inventory).Scan(&ledger, &indexes); err != nil || ledger != 4 || indexes != 0 || snapshot() != before {
		t.Fatal("failed index upgrade changed schema or history")
	}
	// 此测试冻结 0005 升级边界，不能把后续迁移数量当成索引升级失败。
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, "0005_fact_management_indexes.sql"), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(inventory).Scan(&ledger, &indexes); err != nil || ledger != 5 || indexes != 2 || snapshot() != before {
		t.Fatal("index upgrade did not preserve records")
	}
}
