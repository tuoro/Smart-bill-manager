package reviews

import (
	"context"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// 规则上线前写下的归属不会因为规则存在就自动生效——没有任何事件会再碰它们。
// 启动时的漂移修复用同一套规则把它们对齐，且不碰人工选择。
func TestStartupReconcilesTripLinkDriftLeftByEarlierVersions(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now.Add(time.Hour)})
	tripService := tripapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now.Add(2 * time.Hour)})

	paymentReview, err := reviewService.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment, err := reviewService.Confirm(ctx, f.tenant, f.jobID, ConfirmInput{
		ExpectedRevision: paymentReview.Revision, AssociationMode: AssociationNoCandidate,
		IdempotencyKey: "drift-payment", RequestID: "drift-payment-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	invoiceReview := seedAdditionalReview(t, f, invoiceEnvelope("DRIFT-INV-1"), "drift-invoice")
	invoice, err := reviewService.Confirm(ctx, f.tenant, invoiceReview.Job.ID, ConfirmInput{
		ExpectedRevision: invoiceReview.Revision, AssociationMode: AssociationAllocateCandidates,
		Allocations:    []domain.AllocationRequest{{CandidateID: invoiceReview.Candidates[0].ID, AllocatedMinor: invoiceReview.Candidates[0].RemainingMinor}},
		IdempotencyKey: "drift-invoice", RequestID: "drift-invoice-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 造出规则上线前的状态：先让发票不参与自动（支付归属时它不会被带走），行程建立、
	// 支付归属之后，再直接改库把发票放回自动模式——绕过偏好接口，也就绕过了重算。
	// 结果正是升级后的样子：关联在、支付已归属、发票自动模式却没有归属，且不会再有
	// 任何事件来碰它。历史不可变，所以不能靠删归属来模拟。
	blockInvoiceAuto(t, f, invoice.FactID)
	trip := seedManualTrip(t, f, "drift-trip", "北京", "2026-08-26", "2026-08-28")
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE invoices SET trip_assignment_mode = 'auto'
		WHERE tenant_id = ? AND id = ?`, f.tenant.TenantID, invoice.FactID); err != nil {
		t.Fatal(err)
	}
	if got := currentTripOf(t, f, domain.DocumentPayment, payment.FactID); got != trip.TripID {
		t.Fatalf("precondition: payment should be assigned by time, got %q", got)
	}
	if got := currentTripOf(t, f, domain.DocumentInvoice, invoice.FactID); got != "" {
		t.Fatal("failed to simulate legacy state")
	}

	fixed, err := tripService.ReconcileLinkDrift(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fixed != 1 {
		t.Fatalf("reconciled facts = %d, want 1", fixed)
	}
	if got := currentTripOf(t, f, domain.DocumentInvoice, invoice.FactID); got != trip.TripID {
		t.Fatalf("invoice after reconcile = %q, want %q", got, trip.TripID)
	}
	if rule := lastRuleVersionOf(t, f, domain.DocumentInvoice, invoice.FactID); rule != domain.TripLinkAttributionVersion {
		t.Fatalf("reconcile rule = %q", rule)
	}

	// 已经一致时不再写入：重复执行是安全的。
	again, err := tripService.ReconcileLinkDrift(ctx)
	if err != nil || again != 0 {
		t.Fatalf("second reconcile = %d / %v, want 0", again, err)
	}

	// 人工「保持无归属」的发票不被回填带走。
	if err := tripService.Preference(ctx, f.tenant, domain.DocumentInvoice, invoice.FactID, "blocked", "drift-block",
		assignmentVersion(t, f, domain.DocumentInvoice, invoice.FactID)); err != nil {
		t.Fatal(err)
	}
	blocked, err := tripService.ReconcileLinkDrift(ctx)
	if err != nil || blocked != 0 {
		t.Fatalf("blocked invoice reconcile = %d / %v, want 0", blocked, err)
	}
	if got := currentTripOf(t, f, domain.DocumentInvoice, invoice.FactID); got != "" {
		t.Fatalf("blocked invoice was reassigned: %q", got)
	}
}
