package reviews

import (
	"context"
	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
	"os"
	"path/filepath"
	"testing"
)

func TestBadDebtForwardUpgradePreservesDataAndRollsBack(t *testing.T) {
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
	copyMigration("0001_initial.sql")
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	f := newReviewFixtureInStore(t, store)
	old := seedPublishedTrip(t, f, "bad-debt-upgrade", "submitted")
	for _, name := range []string{"0002_manual_trip_workspaces.sql", "0003_explicit_manual_review.sql", "0004_confirmed_fact_corrections.sql", "0005_fact_management_indexes.sql", "0006_invoice_supporting_materials.sql", "0007_member_account_lifecycle.sql"} {
		copyMigration(name)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	before := publishedTripHistory(t, store.DB(), f.tenant.TenantID)
	name := "0008_allocation_search_and_bad_debt.sql"
	data := copyMigration(name)
	broken := append(append([]byte{}, data...), []byte("\nDO $$ BEGIN RAISE EXCEPTION 'synthetic_bad_debt_upgrade_failure'; END; $$;")...)
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, name), broken, 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err == nil {
		t.Fatal("injected migration failure succeeded")
	}
	var ledger int
	var table *string
	if err := store.DB().QueryRow(`SELECT (SELECT count(*) FROM schema_migrations),to_regclass('public.fact_bad_debt_decisions')::text`).Scan(&ledger, &table); err != nil || ledger != 7 || table != nil {
		t.Fatal("failed migration left state")
	}
	assertHistory := func() {
		t.Helper()
		after := publishedTripHistory(t, store.DB(), f.tenant.TenantID)
		for section, value := range before {
			if after[section] != value {
				t.Fatalf("upgrade changed %s", section)
			}
		}
	}
	assertHistory()
	copyMigration(name)
	for range 2 {
		if err := postgres.Migrate(ctx, config); err != nil {
			t.Fatal(err)
		}
	}
	assertHistory()
	var marked bool
	if err := store.DB().QueryRow(`SELECT sbm_fact_bad_debt(?, 'payment', ?)`, f.tenant.TenantID, old.PaymentID).Scan(&marked); err != nil || marked {
		t.Fatal("upgrade invented bad debt")
	}
	service := NewFactService(store, store, system.IDGenerator{}, fixedClock{now: f.now})
	if _, err := service.SetBadDebt(ctx, f.tenant, domain.DocumentPayment, old.PaymentID, domain.BadDebtInput{Marked: true, ExpectedVersion: assignmentVersion(t, f, domain.DocumentPayment, old.PaymentID), Reason: "升级后显式标记"}, "bad-debt-upgraded-mark", "bad-debt-upgraded-mark"); err != nil {
		t.Fatal(err)
	}
	var locked bool
	if err := store.DB().QueryRow(`SELECT sbm_trip_bad_debt_locked(?,?)`, f.tenant.TenantID, old.TripID).Scan(&locked); err != nil || !locked {
		t.Fatal("upgraded graph not protected")
	}
}
