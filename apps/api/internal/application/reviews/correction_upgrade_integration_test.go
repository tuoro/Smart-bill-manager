package reviews

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestFactCorrectionForwardUpgradePreservesThreeTypesAndRollback(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	currentDir := config.MigrationsDir
	config.MigrationsDir = t.TempDir()
	for _, name := range []string{"0001_initial.sql", "0002_manual_trip_workspaces.sql", "0003_explicit_manual_review.sql"} {
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
	p := confirmFactWithoutLinks(t, s, f.tenant, r, "correction-before-upgrade")
	confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, invoiceWithItemsEnvelope("CORRECTION-UPGRADE"), "correction-upgrade-invoice"), "correction-upgrade-invoice")
	trip := seedAdditionalReview(t, f, tripEnvelope("合成始发地", "合成终点", "2026-08-26", "2026-08-28"), "correction-upgrade-trip")
	if _, err := s.Confirm(ctx, f.tenant, trip.Job.ID, ConfirmInput{ExpectedRevision: trip.Revision, IdempotencyKey: "correction-upgrade-trip", RequestID: "correction-upgrade-trip-request"}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var result string
		if err := store.DB().QueryRow(`SELECT jsonb_build_object(
		'payments',(SELECT jsonb_agg(to_jsonb(p)-'current_review_decision_id' ORDER BY id) FROM payments p),
		'invoices',(SELECT jsonb_agg(to_jsonb(i)-'current_review_decision_id' ORDER BY id) FROM invoices i),
		'trips',(SELECT jsonb_agg(to_jsonb(t)-'current_review_decision_id' ORDER BY id) FROM trip_evidence_facts t),
		'items',(SELECT jsonb_agg(to_jsonb(i)-'review_decision_id' ORDER BY id) FROM invoice_items i),
		'claims',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM claim_sets c),
		'origins',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM fact_field_origins o),
		'reviews',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM review_decisions r))::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	content, err := os.ReadFile(filepath.Join(currentDir, "0004_confirmed_fact_corrections.sql"))
	if err != nil {
		t.Fatal(err)
	}
	broken := append(append([]byte(nil), content...), []byte("\nDO $$ BEGIN RAISE EXCEPTION 'synthetic_correction_upgrade_failure'; END; $$;\n")...)
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, "0004_confirmed_fact_corrections.sql"), broken, 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err == nil {
		t.Fatal("failed correction migration committed")
	}
	var ledger, columns int
	if err := store.DB().QueryRow(`SELECT (SELECT count(*) FROM schema_migrations),(SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='payments' AND column_name='current_review_decision_id')`).Scan(&ledger, &columns); err != nil || ledger != 3 || columns != 0 || snapshot() != before {
		t.Fatal("failed correction migration changed previous schema/data")
	}
	config.MigrationsDir = currentDir
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("forward correction migration rewrote history")
	}
	w, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil {
		t.Fatal(err)
	}
	input := correctionInputFrom(w)
	correctionField(t, &input, "merchant", "升级后合成纠错")
	applyCorrection(t, s, f.tenant, domain.DocumentPayment, p.FactID, input, "correction-after-upgrade")
}
