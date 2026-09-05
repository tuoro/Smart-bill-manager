package reviews

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestFactCorrectionFoundationPreservesInitialIdentityAndRejectsUnreviewedChanges(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	service := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	review, err := service.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	input := ConfirmInput{ExpectedRevision: review.Revision, AssociationMode: AssociationNoCandidate, IdempotencyKey: "correction-foundation-original", RequestID: "correction-foundation-audit"}
	confirmed, err := service.Confirm(ctx, f.tenant, f.jobID, input)
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.store.GetFactCorrectionState(ctx, f.tenant.TenantID, domain.DocumentPayment, confirmed.FactID, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentReviewDecisionID != confirmed.ReviewDecisionID || state.ClaimSetID != review.ClaimSetID || state.DocumentID != review.DocumentID || state.Version != 1 || len(state.Links) != 0 || state.Attribution.Mode != "auto" {
		t.Fatal("initial correction snapshot changed source identity")
	}
	if _, err := f.store.GetFactCorrectionState(ctx, "other-synthetic-tenant", domain.DocumentPayment, confirmed.FactID, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("correction snapshot crossed tenant")
	}
	if _, err := f.store.GetFactCorrectionState(ctx, f.tenant.TenantID, domain.DocumentPayment, confirmed.FactID, "invalid"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("invalid proposed instant accepted")
	}
	_, err = f.store.DB().ExecContext(ctx, `UPDATE payments SET merchant = 'unreviewed', version = version + 1 WHERE tenant_id = ? AND id = ?`, f.tenant.TenantID, confirmed.FactID)
	var rejected *pgconn.PgError
	if !errors.As(err, &rejected) || rejected.Message != "confirmed_correction_required" {
		t.Fatalf("unexpected unreviewed-change result: %v", err)
	}
	replay, err := service.Confirm(ctx, f.tenant, f.jobID, input)
	if err != nil || !replay.Replayed || replay.FactID != confirmed.FactID {
		t.Fatal("original confirmation replay was lost")
	}
	// 只有归属等操作增加聚合版本，也不能把首次确认包装成虚假纠错历史。
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE payments SET version = version + 1 WHERE tenant_id = ? AND id = ?`, f.tenant.TenantID, confirmed.FactID); err != nil {
		t.Fatal(err)
	}
	var auditID string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM audit_events WHERE tenant_id = ? AND resource_id = ?`, f.tenant.TenantID, confirmed.FactID).Scan(&auditID); err != nil {
		t.Fatal(err)
	}
	_, err = f.store.DB().ExecContext(ctx, `INSERT INTO fact_corrections (tenant_id, review_decision_id, previous_review_decision_id, payment_id, expected_version, resulting_version, request_hash, preview_hash, audit_event_id) VALUES (?, ?, ?, ?, 1, 2, ?, ?, ?)`, f.tenant.TenantID, confirmed.ReviewDecisionID, confirmed.ReviewDecisionID, confirmed.FactID, strings.Repeat("a", 64), strings.Repeat("b", 64), auditID)
	if !errors.As(err, &rejected) || rejected.Message != "correction_identity_mismatch" {
		t.Fatalf("unexpected fake-history rejection: %v", err)
	}
	var historyCount int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM fact_corrections`).Scan(&historyCount); err != nil || historyCount != 0 {
		t.Fatal("failed history insert survived")
	}
}
