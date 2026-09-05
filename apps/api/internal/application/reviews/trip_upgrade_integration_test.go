package reviews

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

// 真实应用 0001 后种合成旧记录，再运行 0002；不回退 Schema，不禁用触发器。
func TestPublishedTripUpgradePreservesEvidenceAssignmentsAndSnapshots(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	currentMigrations := config.MigrationsDir
	initial, err := os.ReadFile(filepath.Join(currentMigrations, "0001_initial.sql"))
	if err != nil {
		t.Fatal(err)
	}
	config.MigrationsDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, "0001_initial.sql"), initial, 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture := newReviewFixtureInStore(t, store)
	var old []oldTripState
	for index, status := range []string{"submitted", "reimbursed", "rejected"} {
		old = append(old, seedPublishedTrip(t, fixture, fmt.Sprintf("upgrade-%d", index), status))
	}
	deleted := old[2]
	execOldSQL(t, store.DB(), `INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at)
		VALUES (?, ?, ?, 'fact_deleted', 'trip', ?, 'synthetic-old-delete', '{}'::jsonb, ?)`, deleted.TripID+"-delete", fixture.tenant.TenantID, fixture.tenant.UserID, deleted.TripID, fixture.now)
	execOldSQL(t, store.DB(), `UPDATE trips SET deleted_at = ?, deleted_by_user_id = ?, deletion_audit_event_id = ?, version = version + 1 WHERE id = ?`, fixture.now, fixture.tenant.UserID, deleted.TripID+"-delete", deleted.TripID)
	execOldSQL(t, store.DB(), `UPDATE trip_fact_assignments SET ended_at = ?, ended_by_audit_event_id = ? WHERE id = ?`, fixture.now, deleted.TripID+"-delete", deleted.LinkID)
	// 显式人工撤销的旧偏好必须保留；使用原有决定与 Link 约束。
	execOldSQL(t, store.DB(), `INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at)
		VALUES (?, ?, ?, 'trip_fact_assignment_changed', 'payment', ?, 'synthetic-old-unassign', '{}'::jsonb, ?)`, old[1].PaymentID+"-unassign-audit", fixture.tenant.TenantID, fixture.tenant.UserID, old[1].PaymentID, fixture.now)
	execOldSQL(t, store.DB(), `INSERT INTO trip_fact_assignment_decisions (id, tenant_id, actor_user_id, fact_type, payment_id, previous_assignment_id, action, idempotency_key, request_hash, reason, audit_event_id, created_at)
		VALUES (?, ?, ?, 'payment', ?, ?, 'unassign', ?, ?, '合成历史撤销', ?, ?)`, old[1].PaymentID+"-unassign", fixture.tenant.TenantID, fixture.tenant.UserID, old[1].PaymentID, old[1].LinkID, "upgrade-unassign-key", old[1].Hash, old[1].PaymentID+"-unassign-audit", fixture.now)
	execOldSQL(t, store.DB(), `UPDATE trip_fact_assignments SET ended_at = ?, ended_by_decision_id = ? WHERE id = ?`, fixture.now, old[1].PaymentID+"-unassign", old[1].LinkID)
	historyBefore := publishedTripHistory(t, store.DB(), fixture.tenant.TenantID)
	upgrade, err := os.ReadFile(filepath.Join(currentMigrations, "0002_manual_trip_workspaces.sql"))
	if err != nil {
		t.Fatal(err)
	}
	failedUpgrade := append(upgrade, []byte("\nDO $$ BEGIN RAISE EXCEPTION 'synthetic_upgrade_failure'; END; $$;\n")...)
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, "0002_manual_trip_workspaces.sql"), failedUpgrade, 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err == nil {
		t.Fatal("injected migration failure unexpectedly succeeded")
	}
	var evidenceTable *string
	var migrationCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT to_regclass('public.trip_evidence_facts')::text, (SELECT count(*) FROM schema_migrations)`).Scan(&evidenceTable, &migrationCount); err != nil {
		t.Fatal(err)
	}
	if evidenceTable != nil || migrationCount != 1 {
		t.Fatal("failed migration left partial schema or ledger")
	}
	for section, before := range historyBefore {
		if publishedTripHistory(t, store.DB(), fixture.tenant.TenantID)[section] != before {
			t.Fatalf("failed migration changed %s", section)
		}
	}
	config.MigrationsDir = currentMigrations
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatalf("repeat upgrade: %v", err)
	}
	historyAfter := publishedTripHistory(t, store.DB(), fixture.tenant.TenantID)
	var fabricatedMaterialCaptures int
	if err := store.DB().QueryRow(`SELECT count(*) FROM reimbursements WHERE materials_captured OR material_count IS NOT NULL`).Scan(&fabricatedMaterialCaptures); err != nil || fabricatedMaterialCaptures != 0 {
		t.Fatal("upgrade fabricated historical material capture")
	}
	var backfilledHistoricalIdentities int
	if err := store.DB().QueryRow(`SELECT count(*) FROM reimbursement_items WHERE fact_review_decision_id IS NOT NULL`).Scan(&backfilledHistoricalIdentities); err != nil || backfilledHistoricalIdentities != 0 {
		t.Fatal("upgrade fabricated historical reimbursement revision identity")
	}
	for section, before := range historyBefore {
		if historyAfter[section] != before {
			t.Fatalf("published history changed during upgrade: %s", section)
		}
	}
	for index, previous := range old {
		var name, destination, source, hash, status, mode string
		var timezone, snapshotTimezone *string
		var snapshotVersion *int64
		var origins, materialLinks int
		var containerDeleted, evidenceDeleted bool
		err := store.DB().QueryRowContext(ctx, `SELECT t.name, t.timezone, e.destination, e.source_review_decision_id,
			t.deleted_at IS NOT NULL, e.deleted_at IS NOT NULL, r.snapshot_hash, r.status, p.trip_assignment_mode,
			(SELECT count(*) FROM fact_field_origins f WHERE f.tenant_id = t.tenant_id AND f.trip_id = e.id),
			(SELECT count(*) FROM trip_material_links l WHERE l.tenant_id = t.tenant_id AND l.evidence_id = e.id AND l.ended_at IS NULL),
			r.trip_timezone, r.trip_version
			FROM trips t JOIN trip_evidence_facts e ON e.tenant_id = t.tenant_id AND e.id = t.id
			JOIN reimbursements r ON r.tenant_id = t.tenant_id AND r.id = ? JOIN payments p ON p.tenant_id = t.tenant_id AND p.id = ?
			WHERE t.tenant_id = ? AND t.id = ?`, previous.ReimbursementID, previous.PaymentID, fixture.tenant.TenantID, previous.TripID).
			Scan(&name, &timezone, &destination, &source, &containerDeleted, &evidenceDeleted, &hash, &status, &mode, &origins, &materialLinks, &snapshotTimezone, &snapshotVersion)
		if err != nil {
			t.Fatal(err)
		}
		wantMode, wantLinks := "manual", 1
		if index != 0 {
			wantMode = "blocked"
		}
		if index == 2 {
			wantLinks = 0
		}
		if name != "合成迁移行程" || destination != "  合成迁移行程  " || timezone != nil || source != previous.ReviewID || hash != previous.Hash || status != previous.Status ||
			mode != wantMode || origins == 0 || materialLinks != wantLinks || containerDeleted != (index == 2) || evidenceDeleted != containerDeleted || snapshotTimezone != nil || snapshotVersion != nil {
			t.Fatalf("upgrade preservation mismatch for synthetic case %d", index)
		}
	}
	// 同 ID 的凭证删除不能结束容器上的费用。
	facts := NewFactService(store, store, system.IDGenerator{}, fixedClock{now: fixture.now})
	if err := facts.Delete(ctx, fixture.tenant, domain.DocumentTrip, old[0].TripID, "upgrade-evidence-delete"); err != nil {
		t.Fatal(err)
	}
	assertPaymentTrip(t, fixture, old[0].PaymentID, old[0].TripID, "manual")
	service := tripapp.NewService(store, store, system.IDGenerator{}, fixedClock{now: fixture.now})
	if _, err := service.Manage(ctx, fixture.tenant, old[1].TripID, "delete", tripapp.ManagementInput{ExpectedVersion: 1, Reason: "合成删除容器", IdempotencyKey: "upgrade-delete-container", RequestID: "upgrade-delete-container-request"}); err != nil {
		t.Fatal(err)
	}
	var materialDeleted bool
	if err := store.DB().QueryRowContext(ctx, `SELECT deleted_at IS NOT NULL FROM trip_evidence_facts WHERE id = ?`, old[1].TripID).Scan(&materialDeleted); err != nil || materialDeleted {
		t.Fatalf("container deleted evidence: %v", err)
	}
}

type oldTripState struct{ TripID, ReviewID, PaymentID, LinkID, ReimbursementID, Hash, Status string }

func seedPublishedTrip(t *testing.T, fixture reviewFixture, label, status string) oldTripState {
	t.Helper()
	ctx := context.Background()
	ids := system.IDGenerator{}
	tripReview := seedAdditionalReview(t, fixture, tripEnvelope("合成起点", "  合成迁移行程  ", "2026-08-26", "2026-08-28"), label+"-trip")
	paymentReview := seedAdditionalReview(t, fixture, paymentEnvelopeAt(label+"-merchant", "2026-08-27T12:00:00+08:00"), label+"-payment")
	state := oldTripState{TripID: mustID(t, ids), ReviewID: mustID(t, ids), PaymentID: mustID(t, ids), LinkID: mustID(t, ids), ReimbursementID: mustID(t, ids), Status: status}
	hash := sha256.Sum256([]byte(label))
	state.Hash = hex.EncodeToString(hash[:])
	tx, err := fixture.store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	paymentDecision, assignmentDecision, submitDecision := mustID(t, ids), mustID(t, ids), mustID(t, ids)
	for _, item := range []struct {
		id     string
		review ports.ReviewSnapshot
		kind   string
		mode   any
	}{{state.ReviewID, tripReview, "trip", nil}, {paymentDecision, paymentReview, "payment", "no_candidate"}} {
		execOldSQL(t, tx, `INSERT INTO review_decisions (id, tenant_id, claim_set_id, actor_user_id, action, fact_type, association_mode, duplicate_plan_hash, idempotency_key, expected_revision, created_at)
			VALUES (?, ?, ?, ?, 'confirm', ?, ?, ?, ?, ?, ?)`, item.id, fixture.tenant.TenantID, item.review.ClaimSetID, fixture.tenant.UserID, item.kind, item.mode, state.Hash, item.id+"-key", item.review.Revision, fixture.now)
	}
	execOldSQL(t, tx, `INSERT INTO trips (id, tenant_id, source_review_decision_id, origin, destination, start_date, end_date, traveler_name, transport_type, booking_reference, created_at, updated_at)
		VALUES (?, ?, ?, '合成起点', '  合成迁移行程  ', '2026-08-26', '2026-08-28', '合成人员', 'flight', 'SYN-UPGRADE', ?, ?)`, state.TripID, fixture.tenant.TenantID, state.ReviewID, fixture.now, fixture.now)
	execOldSQL(t, tx, `INSERT INTO fact_field_origins (id, tenant_id, trip_id, field_path, field_claim_id, review_decision_id, created_at)
		SELECT gen_random_uuid()::text, tenant_id, ?, field_path, id, ?, ? FROM field_claims WHERE tenant_id = ? AND claim_set_id = ?`, state.TripID, state.ReviewID, fixture.now, fixture.tenant.TenantID, tripReview.ClaimSetID)
	execOldSQL(t, tx, `INSERT INTO payments (id, tenant_id, source_review_decision_id, amount_minor, currency, merchant, transaction_time, source_timezone, business_date, created_at, updated_at)
		VALUES (?, ?, ?, 12345, 'CNY', ?, '2026-08-27T04:00:00Z', 'Asia/Shanghai', '2026-08-27', ?, ?)`, state.PaymentID, fixture.tenant.TenantID, paymentDecision, label+"-merchant", fixture.now, fixture.now)
	execOldSQL(t, tx, `INSERT INTO fact_field_origins (id, tenant_id, payment_id, field_path, field_claim_id, review_decision_id, created_at)
		SELECT gen_random_uuid()::text, tenant_id, ?, field_path, id, ?, ? FROM field_claims WHERE tenant_id = ? AND claim_set_id = ?`, state.PaymentID, paymentDecision, fixture.now, fixture.tenant.TenantID, paymentReview.ClaimSetID)
	for _, review := range []ports.ReviewSnapshot{tripReview, paymentReview} {
		execOldSQL(t, tx, `UPDATE claim_sets SET status = 'confirmed', optimistic_version = optimistic_version + 1 WHERE tenant_id = ? AND id = ?`, fixture.tenant.TenantID, review.ClaimSetID)
		execOldSQL(t, tx, `UPDATE processing_jobs SET status = 'completed', finished_at = ?, version = version + 1 WHERE tenant_id = ? AND id = ?`, fixture.now, fixture.tenant.TenantID, review.Job.ID)
		execOldSQL(t, tx, `UPDATE documents SET status = 'completed' WHERE tenant_id = ? AND id = ?`, fixture.tenant.TenantID, review.DocumentID)
	}
	for _, item := range []struct{ id, action, kind, resource string }{{assignmentDecision + "-audit", "trip_fact_assignment_changed", "payment", state.PaymentID}, {submitDecision + "-audit", "reimbursement_submitted", "reimbursement", state.ReimbursementID}} {
		execOldSQL(t, tx, `INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, '{}'::jsonb, ?)`, item.id, fixture.tenant.TenantID, fixture.tenant.UserID, item.action, item.kind, item.resource, label+"-request", fixture.now)
	}
	execOldSQL(t, tx, `INSERT INTO trip_fact_assignment_decisions (id, tenant_id, actor_user_id, fact_type, payment_id, desired_trip_id, action, idempotency_key, request_hash, reason, audit_event_id, created_at)
		VALUES (?, ?, ?, 'payment', ?, ?, 'assign', ?, ?, '合成历史归属', ?, ?)`, assignmentDecision, fixture.tenant.TenantID, fixture.tenant.UserID, state.PaymentID, state.TripID, label+"-assign", state.Hash, assignmentDecision+"-audit", fixture.now)
	execOldSQL(t, tx, `INSERT INTO trip_fact_assignments (id, tenant_id, trip_id, payment_id, created_by_decision_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`, state.LinkID, fixture.tenant.TenantID, state.TripID, state.PaymentID, assignmentDecision, fixture.now)
	execOldSQL(t, tx, `INSERT INTO reimbursements (id, tenant_id, trip_id, trip_destination, trip_start_date, trip_end_date, status, policy_rule_version, snapshot_hash, created_by_user_id, created_by_decision_id, created_at, updated_at, version)
		VALUES (?, ?, ?, '  合成迁移行程  ', '2026-08-26', '2026-08-28', 'submitted', 'reimbursement-policy/1', ?, ?, ?, ?, ?, 1)`, state.ReimbursementID, fixture.tenant.TenantID, state.TripID, state.Hash, fixture.tenant.UserID, submitDecision, fixture.now, fixture.now)
	execOldSQL(t, tx, `INSERT INTO reimbursement_items (id, tenant_id, reimbursement_id, trip_fact_assignment_id, fact_type, payment_id, display_name, business_date, amount_minor, currency, sort_order, created_at)
		VALUES (?, ?, ?, ?, 'payment', ?, ?, '2026-08-27', 12345, 'CNY', 0, ?)`, mustID(t, ids), fixture.tenant.TenantID, state.ReimbursementID, state.LinkID, state.PaymentID, label+"-merchant", fixture.now)
	execOldSQL(t, tx, `INSERT INTO reimbursement_status_decisions (id, tenant_id, reimbursement_id, actor_user_id, desired_status, expected_version, result_version, action, idempotency_key, request_hash, reason, audit_event_id, created_at)
		VALUES (?, ?, ?, ?, 'submitted', 0, 1, 'submit', ?, ?, '合成历史报销', ?, ?)`, submitDecision, fixture.tenant.TenantID, state.ReimbursementID, fixture.tenant.UserID, label+"-submit", state.Hash, submitDecision+"-audit", fixture.now)
	if status != "submitted" {
		decision, action := mustID(t, ids), "reject"
		if status == "reimbursed" {
			action = "mark_reimbursed"
		}
		execOldSQL(t, tx, `INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at) VALUES (?, ?, ?, 'reimbursement_status_changed', 'reimbursement', ?, ?, '{}'::jsonb, ?)`, decision+"-audit", fixture.tenant.TenantID, fixture.tenant.UserID, state.ReimbursementID, label+"-status", fixture.now)
		execOldSQL(t, tx, `INSERT INTO reimbursement_status_decisions (id, tenant_id, reimbursement_id, actor_user_id, previous_status, desired_status, expected_version, result_version, action, idempotency_key, request_hash, reason, audit_event_id, created_at)
			VALUES (?, ?, ?, ?, 'submitted', ?, 1, 2, ?, ?, ?, '合成历史状态', ?, ?)`, decision, fixture.tenant.TenantID, state.ReimbursementID, fixture.tenant.UserID, status, action, label+"-status", state.Hash, decision+"-audit", fixture.now)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return state
}

// 比较完整旧记录（包括结束决定、原始关联 ID、快照条目及状态历史），只归一化迁移新增/重命名字段。
func publishedTripHistory(t *testing.T, db *sql.DB, tenantID string) map[string]string {
	t.Helper()
	queries := map[string]string{
		"field_origins": `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY r.id), '[]'::jsonb) FROM fact_field_origins r WHERE tenant_id = ?`,
		"expense_links": `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY r.id), '[]'::jsonb) FROM trip_fact_assignments r WHERE tenant_id = ?`,
		"expense_decisions": `SELECT coalesce(jsonb_agg(to_jsonb(r) - 'decision_source' - 'expected_fact_version' - 'rule_version' ORDER BY r.id), '[]'::jsonb)
			FROM trip_fact_assignment_decisions r WHERE tenant_id = ?`,
		"reimbursement_items":     `SELECT coalesce(jsonb_agg(to_jsonb(r) - 'fact_review_decision_id' ORDER BY r.id), '[]'::jsonb) FROM reimbursement_items r WHERE tenant_id = ?`,
		"reimbursement_decisions": `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY r.id), '[]'::jsonb) FROM reimbursement_status_decisions r WHERE tenant_id = ?`,
		"reimbursement_snapshots": `SELECT coalesce(jsonb_agg((to_jsonb(r) - 'trip_destination' - 'trip_name' - 'trip_timezone' - 'trip_version' - 'materials_captured' - 'material_count')
			|| jsonb_build_object('trip_label', coalesce(to_jsonb(r)->'trip_destination', to_jsonb(r)->'trip_name')) ORDER BY r.id), '[]'::jsonb)
			FROM reimbursements r WHERE tenant_id = ?`,
	}
	snapshots := make(map[string]string, len(queries))
	for section, query := range queries {
		var snapshot string
		if err := db.QueryRowContext(context.Background(), query, tenantID).Scan(&snapshot); err != nil {
			t.Fatalf("read synthetic upgrade history %s: %v", section, err)
		}
		snapshots[section] = snapshot
	}
	return snapshots
}

func execOldSQL(t *testing.T, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("seed synthetic published schema: %v", err)
	}
}
