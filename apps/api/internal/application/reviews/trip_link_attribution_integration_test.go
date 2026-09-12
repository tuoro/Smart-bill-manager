package reviews

import (
	"context"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	allocationapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func currentTripOf(t *testing.T, f reviewFixture, factType domain.DocumentType, id string) string {
	t.Helper()
	column := "payment_id"
	if factType == domain.DocumentInvoice {
		column = "invoice_id"
	}
	var trip string
	err := f.store.DB().QueryRowContext(context.Background(), `SELECT coalesce(max(trip_id), '') FROM trip_fact_assignments
		WHERE tenant_id = ? AND `+column+` = ? AND ended_at IS NULL`, f.tenant.TenantID, id).Scan(&trip)
	if err != nil {
		t.Fatal(err)
	}
	return trip
}

func lastRuleVersionOf(t *testing.T, f reviewFixture, factType domain.DocumentType, id string) string {
	t.Helper()
	column := "payment_id"
	if factType == domain.DocumentInvoice {
		column = "invoice_id"
	}
	var rule string
	err := f.store.DB().QueryRowContext(context.Background(), `SELECT coalesce(rule_version, 'manual') FROM trip_fact_assignment_decisions
		WHERE tenant_id = ? AND `+column+` = ? ORDER BY created_at DESC, id DESC LIMIT 1`, f.tenant.TenantID, id).Scan(&rule)
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

// 已确认关联的支付与发票互相跟随行程归属；冲突留人工；人工偏好优先；解除关联即撤回。
func TestLinkedPaymentAndInvoiceFollowEachOthersTrip(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now.Add(time.Hour)})
	tripService := tripapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now.Add(2 * time.Hour)})

	// 支付先确认；行程尚不存在，所以时间规则给不出归属。
	paymentReview, err := reviewService.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment, err := reviewService.Confirm(ctx, f.tenant, f.jobID, ConfirmInput{
		ExpectedRevision: paymentReview.Revision, AssociationMode: AssociationNoCandidate,
		IdempotencyKey: "link-payment", RequestID: "link-payment-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 发票确认时关联到这笔支付。两边都没有行程，发票保持未归属。
	invoiceReview := seedAdditionalReview(t, f, invoiceEnvelope("LINK-INV-1"), "link-invoice")
	invoice, err := reviewService.Confirm(ctx, f.tenant, invoiceReview.Job.ID, ConfirmInput{
		ExpectedRevision: invoiceReview.Revision, AssociationMode: AssociationAllocateCandidates,
		Allocations:    []domain.AllocationRequest{{CandidateID: invoiceReview.Candidates[0].ID, AllocatedMinor: invoiceReview.Candidates[0].RemainingMinor}},
		IdempotencyKey: "link-invoice", RequestID: "link-invoice-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if currentTripOf(t, f, domain.DocumentInvoice, invoice.FactID) != "" {
		t.Fatal("invoice assigned without any trip")
	}

	// 行程建立后，支付按时间自动归属；关联的发票跟着归属，规则记为关联规则。
	tripOne := seedManualTrip(t, f, "link-trip-one", "北京", "2026-08-26", "2026-08-28")
	if got := currentTripOf(t, f, domain.DocumentPayment, payment.FactID); got != tripOne.TripID {
		t.Fatalf("payment trip = %q, want time match %q", got, tripOne.TripID)
	}
	if got := currentTripOf(t, f, domain.DocumentInvoice, invoice.FactID); got != tripOne.TripID {
		t.Fatalf("invoice trip = %q, want to follow linked payment %q", got, tripOne.TripID)
	}
	if rule := lastRuleVersionOf(t, f, domain.DocumentInvoice, invoice.FactID); rule != domain.TripLinkAttributionVersion {
		t.Fatalf("invoice rule = %q", rule)
	}

	// 人工把支付改到另一行程：发票跟着走。
	tripTwo := seedManualTrip(t, f, "link-trip-two", "深圳", "2026-09-10", "2026-09-12")
	currentAssignment := func(factType domain.DocumentType, id string) string {
		t.Helper()
		column := "payment_id"
		if factType == domain.DocumentInvoice {
			column = "invoice_id"
		}
		var assignment string
		if err := f.store.DB().QueryRowContext(ctx, `SELECT coalesce(max(id), '') FROM trip_fact_assignments WHERE tenant_id = ? AND `+column+` = ? AND ended_at IS NULL`, f.tenant.TenantID, id).Scan(&assignment); err != nil {
			t.Fatal(err)
		}
		return assignment
	}
	desiredTwo := tripTwo.TripID
	expected := currentAssignment(domain.DocumentPayment, payment.FactID)
	if _, err := tripService.Assign(ctx, f.tenant, tripapp.AssignmentInput{
		ExpectedFactVersion: assignmentVersion(t, f, domain.DocumentPayment, payment.FactID),
		FactType:            domain.DocumentPayment, FactID: payment.FactID, DesiredTripID: &desiredTwo, ExpectedAssignmentID: &expected,
		Reason: "人工改归", IdempotencyKey: "link-move-payment", RequestID: "link-move-payment-request",
	}); err != nil {
		t.Fatal(err)
	}
	if got := currentTripOf(t, f, domain.DocumentInvoice, invoice.FactID); got != tripTwo.TripID {
		t.Fatalf("invoice did not follow manual move: %q", got)
	}

	// 发票被人工「保持无归属」后不再跟随；恢复自动后重新跟随。
	if err := tripService.Preference(ctx, f.tenant, domain.DocumentInvoice, invoice.FactID, "blocked", "link-block-invoice",
		assignmentVersion(t, f, domain.DocumentInvoice, invoice.FactID)); err != nil {
		t.Fatal(err)
	}
	if got := currentTripOf(t, f, domain.DocumentInvoice, invoice.FactID); got != "" {
		t.Fatalf("blocked invoice still assigned: %q", got)
	}
	if err := tripService.Preference(ctx, f.tenant, domain.DocumentInvoice, invoice.FactID, "auto", "link-restore-invoice",
		assignmentVersion(t, f, domain.DocumentInvoice, invoice.FactID)); err != nil {
		t.Fatal(err)
	}
	if got := currentTripOf(t, f, domain.DocumentInvoice, invoice.FactID); got != tripTwo.TripID {
		t.Fatalf("restored invoice did not follow: %q", got)
	}

	// 反向：另一笔无时间命中的支付关联到一张已人工归属的发票，支付跟随发票。
	secondPaymentReview := seedAdditionalReview(t, f, paymentEnvelopeAt("Follow Merchant", "2026-07-01T12:00:00+08:00"), "link-second-payment")
	secondPayment := confirmFactWithoutLinks(t, reviewService, f.tenant, secondPaymentReview, "link-second-payment-confirm")
	secondInvoiceReview := seedAdditionalReview(t, f, invoiceEnvelope("LINK-INV-2"), "link-second-invoice")
	secondInvoice := confirmFactWithoutLinks(t, reviewService, f.tenant, secondInvoiceReview, "link-second-invoice-confirm")
	desiredOne := tripOne.TripID
	if _, err := tripService.Assign(ctx, f.tenant, tripapp.AssignmentInput{
		ExpectedFactVersion: assignmentVersion(t, f, domain.DocumentInvoice, secondInvoice.FactID),
		FactType:            domain.DocumentInvoice, FactID: secondInvoice.FactID, DesiredTripID: &desiredOne,
		Reason: "人工归属发票", IdempotencyKey: "link-assign-invoice-two", RequestID: "link-assign-invoice-two-request",
	}); err != nil {
		t.Fatal(err)
	}
	allocations := allocationapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now.Add(3 * time.Hour)})
	workspace, err := allocations.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, secondPayment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := allocations.Adjust(ctx, f.tenant, domain.DocumentPayment, secondPayment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash:   workspace.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: secondInvoice.FactID, AllocatedMinor: 1}},
		Reason:             "", IdempotencyKey: "link-adjust-second", RequestID: "link-adjust-second-request",
	}); err != nil {
		t.Fatal(err)
	}
	if got := currentTripOf(t, f, domain.DocumentPayment, secondPayment.FactID); got != tripOne.TripID {
		t.Fatalf("payment did not follow linked invoice after allocation: %q", got)
	}

	// 冲突：这笔支付再关联到一张人工归属于另一行程的发票 → 两个不同行程，撤回归属留人工。
	thirdInvoiceReview := seedAdditionalReview(t, f, invoiceEnvelope("LINK-INV-3"), "link-third-invoice")
	thirdInvoice := confirmFactWithoutLinks(t, reviewService, f.tenant, thirdInvoiceReview, "link-third-invoice-confirm")
	if _, err := tripService.Assign(ctx, f.tenant, tripapp.AssignmentInput{
		ExpectedFactVersion: assignmentVersion(t, f, domain.DocumentInvoice, thirdInvoice.FactID),
		FactType:            domain.DocumentInvoice, FactID: thirdInvoice.FactID, DesiredTripID: &desiredTwo,
		Reason: "人工归属第三张发票", IdempotencyKey: "link-assign-invoice-three", RequestID: "link-assign-invoice-three-request",
	}); err != nil {
		t.Fatal(err)
	}
	workspace, err = allocations.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, secondPayment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := allocations.Adjust(ctx, f.tenant, domain.DocumentPayment, secondPayment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash: workspace.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{
			{TargetFactID: secondInvoice.FactID, AllocatedMinor: 1},
			{TargetFactID: thirdInvoice.FactID, AllocatedMinor: 1},
		},
		Reason: "", IdempotencyKey: "link-adjust-conflict", RequestID: "link-adjust-conflict-request",
	}); err != nil {
		t.Fatal(err)
	}
	if got := currentTripOf(t, f, domain.DocumentPayment, secondPayment.FactID); got != "" {
		t.Fatalf("conflicting links must leave payment unassigned, got %q", got)
	}

	// 解除全部关联：支付没有任何信号，保持未归属；第一张发票仍跟着第一笔支付在行程二。
	workspace, err = allocations.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, secondPayment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := allocations.Adjust(ctx, f.tenant, domain.DocumentPayment, secondPayment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash: workspace.PlanHash, DesiredAllocations: []domain.DesiredAllocation{},
		Reason: "", IdempotencyKey: "link-adjust-clear", RequestID: "link-adjust-clear-request",
	}); err != nil {
		t.Fatal(err)
	}
	if got := currentTripOf(t, f, domain.DocumentInvoice, invoice.FactID); got != tripTwo.TripID {
		t.Fatalf("first invoice lost its trip after unrelated unlink: %q", got)
	}
}
