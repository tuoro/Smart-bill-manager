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

func TestManualReviewUpgradePreservesAIHistoryAndRollsBackFailure(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	currentDir := config.MigrationsDir
	config.MigrationsDir = t.TempDir()
	for _, name := range []string{"0001_initial.sql", "0002_manual_trip_workspaces.sql"} {
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
	service := NewService(store, store, system.IDGenerator{}, fixedClock{now: f.now})
	current, err := service.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	current, err = service.Revise(ctx, f.tenant, f.jobID, revisionInputFrom(current))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Confirm(ctx, f.tenant, f.jobID, ConfirmInput{ExpectedRevision: current.Revision, AssociationMode: AssociationNoCandidate, IdempotencyKey: "before-manual-migration", RequestID: "before-manual-audit"}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var value string
		if err := store.DB().QueryRow(`SELECT jsonb_build_object(
		 'claims', (SELECT jsonb_agg(to_jsonb(c) - ARRAY['manual_reason','manual_idempotency_key','manual_request_hash'] ORDER BY id) FROM claim_sets c),
		 'fields', (SELECT jsonb_agg(to_jsonb(f) ORDER BY id) FROM field_claims f),
		 'origins', (SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM fact_field_origins o),
		 'payments', (SELECT jsonb_agg(to_jsonb(p) - 'current_review_decision_id' ORDER BY id) FROM payments p),
		 'decisions', (SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM review_decisions r))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	upgrade, err := os.ReadFile(filepath.Join(currentDir, "0003_explicit_manual_review.sql"))
	if err != nil {
		t.Fatal(err)
	}
	broken := append(append([]byte(nil), upgrade...), []byte("\nDO $$ BEGIN RAISE EXCEPTION 'synthetic_manual_upgrade_failure'; END; $$;\n")...)
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, "0003_explicit_manual_review.sql"), broken, 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err == nil {
		t.Fatal("failed upgrade committed")
	}
	var ledger, columns int
	if err := store.DB().QueryRow(`SELECT (SELECT count(*) FROM schema_migrations), (SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'claim_sets' AND column_name = 'manual_reason')`).Scan(&ledger, &columns); err != nil {
		t.Fatal(err)
	}
	if ledger != 2 || columns != 0 || snapshot() != before {
		t.Fatal("failed upgrade left schema or history changes")
	}
	config.MigrationsDir = currentDir
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("forward migration changed existing AI history")
	}
	var mismatched int
	if err := store.DB().QueryRow(`SELECT count(*) FROM payments WHERE current_review_decision_id IS DISTINCT FROM source_review_decision_id`).Scan(&mismatched); err != nil || mismatched != 0 {
		t.Fatal("forward migration did not preserve initial field revision")
	}
}
