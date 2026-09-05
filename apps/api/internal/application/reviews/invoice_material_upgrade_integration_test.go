package reviews

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	reimbursementapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestInvoiceMaterialUpgradePreservesHistoricalSnapshotsAndRollsBack(t *testing.T) {
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
	var old []oldTripState
	for index, status := range []string{"submitted", "reimbursed", "rejected"} {
		old = append(old, seedPublishedTrip(t, f, fmt.Sprintf("material-upgrade-%d", index), status))
	}
	for _, name := range []string{"0002_manual_trip_workspaces.sql", "0003_explicit_manual_review.sql", "0004_confirmed_fact_corrections.sql", "0005_fact_management_indexes.sql"} {
		copyMigration(name)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	before := publishedTripHistory(t, store.DB(), f.tenant.TenantID)
	name := "0006_invoice_supporting_materials.sql"
	data := copyMigration(name)
	broken := append(append([]byte{}, data...), []byte("\nDO $$ BEGIN RAISE EXCEPTION 'synthetic_material_upgrade_failure'; END; $$;")...)
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, name), broken, 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err == nil {
		t.Fatal("injected migration failure succeeded")
	}
	var ledger int
	var table *string
	if err := store.DB().QueryRow(`SELECT (SELECT count(*) FROM schema_migrations),to_regclass('public.invoice_material_links')::text`).Scan(&ledger, &table); err != nil || ledger != 5 || table != nil {
		t.Fatal("failed material migration left state")
	}
	assertHistory := func() {
		t.Helper()
		after := publishedTripHistory(t, store.DB(), f.tenant.TenantID)
		for section, value := range before {
			if after[section] != value {
				t.Fatalf("historical %s changed", section)
			}
		}
	}
	assertHistory()
	copyMigration(name)
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	assertHistory()
	rs := reimbursementapp.NewService(store, store, system.IDGenerator{}, fixedClock{now: f.now})
	for _, item := range old {
		detail, err := rs.Get(ctx, f.tenant, item.ReimbursementID)
		if err != nil || detail.MaterialsCaptured || detail.MaterialCount != nil || detail.RuleVersion != "reimbursement-policy/1" || detail.SnapshotHash != item.Hash || string(detail.Status) != item.Status {
			t.Fatalf("old material history fabricated: %v", err)
		}
		inventory, err := store.BuildMaterialExport(ctx, f.tenant.TenantID, domain.ExportScope{Kind: "reimbursement", ID: item.ReimbursementID})
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := domain.CanonicalExportManifest(inventory.Manifest)
		if err != nil || manifest.MaterialsCaptured || len(manifest.Warnings) != 3 || manifest.SnapshotHash != item.Hash || len(manifest.References) != len(detail.Items) {
			t.Fatalf("historical export fabricated collection: %v", err)
		}
		for _, reference := range manifest.References {
			if reference.ReviewDecisionID != nil || reference.FactVersion != nil || reference.Kind != "original" {
				t.Fatal("historical export backfilled review or materials")
			}
		}
	}
	var count int
	if err := store.DB().QueryRow(`SELECT (SELECT count(*) FROM invoice_material_links)+(SELECT count(*) FROM reimbursement_material_snapshots)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("migration backfilled materials")
	}
}
