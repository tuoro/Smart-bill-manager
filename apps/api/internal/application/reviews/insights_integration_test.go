package reviews

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	insightapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/insights"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestFactInsightsUseCurrentLinksAssignmentsAndStableTenantSnapshot(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	factService := NewFactService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(8 * time.Hour)})
	tripService := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(6 * time.Hour)})
	insightService := insightapp.NewService(fixture.store)

	paymentReview, err := reviewService.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment := confirmFactWithoutLinks(t, reviewService, fixture.tenant, paymentReview, "insight-payment")
	invoiceReview := seedAdditionalReview(t, fixture, invoiceEnvelopeWithTotal("INSIGHT-001", 6_000), "insight-invoice")
	if len(invoiceReview.Candidates) != 1 || invoiceReview.Candidates[0].TargetID != payment.FactID {
		t.Fatalf("invoice candidates = %#v", invoiceReview.Candidates)
	}
	invoice, err := reviewService.Confirm(ctx, fixture.tenant, invoiceReview.Job.ID, ConfirmInput{
		ExpectedRevision: invoiceReview.Revision,
		AssociationMode:  AssociationAllocateCandidates,
		Allocations: []domain.AllocationRequest{{
			CandidateID: invoiceReview.Candidates[0].ID, AllocatedMinor: 6_000,
		}},
		IdempotencyKey: "insight-invoice-confirm",
		RequestID:      "insight-invoice-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	blockPaymentAuto(t, fixture, payment.FactID)
	trip := seedManualTrip(t, fixture, "insight-trip", "北京", "2026-08-26", "2026-08-28")
	tripID := trip.TripID
	if _, err := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
		ExpectedFactVersion: assignmentVersion(t, fixture, domain.DocumentPayment, payment.FactID),
		FactType:            domain.DocumentPayment, FactID: payment.FactID, DesiredTripID: &tripID,
		Reason: "合成行程归属", IdempotencyKey: "insight-trip-assignment", RequestID: "insight-trip-assignment-request",
	}); err != nil {
		t.Fatal(err)
	}

	var auditBefore int
	if err := fixture.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id = ?`, fixture.tenant.TenantID).Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}
	first, err := insightService.Query(ctx, fixture.tenant, insightapp.QueryInput{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Groups) != 2 || len(first.Items) != 1 || first.Items[0].FactID != payment.FactID || first.NextCursor == "" {
		t.Fatalf("first insights page = %#v", first)
	}
	if first.Groups[0].FactType != domain.DocumentPayment || first.Groups[0].PartialCount != 1 ||
		first.Groups[0].AllocatedMinor != 6_000 || first.Groups[0].RemainingMinor != 6_345 {
		t.Fatalf("payment insights group = %#v", first.Groups[0])
	}
	if first.Groups[1].FactType != domain.DocumentInvoice || first.Groups[1].AllocatedCount != 1 || first.Groups[1].TotalMinor != 6_000 {
		t.Fatalf("invoice insights group = %#v", first.Groups[1])
	}
	second, err := insightService.Query(ctx, fixture.tenant, insightapp.QueryInput{Cursor: first.NextCursor, Limit: 1})
	if err != nil || len(second.Items) != 1 || second.Items[0].FactID != invoice.FactID || second.NextCursor != "" {
		t.Fatalf("second insights page = %#v / %v", second, err)
	}

	assigned, err := insightService.Query(ctx, fixture.tenant, insightapp.QueryInput{
		Filter: domain.InsightFilter{TripScope: domain.InsightTripAssigned, TripID: trip.TripID}, Limit: 50,
	})
	if err != nil || len(assigned.Items) != 1 || assigned.Items[0].FactID != payment.FactID || assigned.Items[0].Trip == nil {
		t.Fatalf("assigned insights = %#v / %v", assigned, err)
	}
	unassigned, err := insightService.Query(ctx, fixture.tenant, insightapp.QueryInput{
		Filter: domain.InsightFilter{TripScope: domain.InsightTripUnassigned}, Limit: 50,
	})
	if err != nil || len(unassigned.Items) != 1 || unassigned.Items[0].FactID != invoice.FactID {
		t.Fatalf("unassigned insights = %#v / %v", unassigned, err)
	}
	allocated, err := insightService.Query(ctx, fixture.tenant, insightapp.QueryInput{
		Filter: domain.InsightFilter{AllocationStatus: domain.InsightStatusFull}, Limit: 50,
	})
	if err != nil || len(allocated.Items) != 1 || allocated.Items[0].FactID != invoice.FactID {
		t.Fatalf("allocated insights = %#v / %v", allocated, err)
	}
	dateEmpty, err := insightService.Query(ctx, fixture.tenant, insightapp.QueryInput{
		Filter: domain.InsightFilter{DateFrom: "2026-08-28", DateTo: "2026-08-29"}, Limit: 50,
	})
	if err != nil || len(dateEmpty.Items) != 0 || len(dateEmpty.Groups) != 0 {
		t.Fatalf("date-filtered insights = %#v / %v", dateEmpty, err)
	}
	var auditAfter int
	if err := fixture.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id = ?`, fixture.tenant.TenantID).Scan(&auditAfter); err != nil {
		t.Fatal(err)
	}
	if auditAfter != auditBefore {
		t.Fatalf("read-only insights wrote audit events: before=%d after=%d", auditBefore, auditAfter)
	}

	viewer := fixture.tenant
	viewer.Role = domain.RoleViewer
	if _, err := insightService.Query(ctx, viewer, insightapp.QueryInput{Limit: 50}); err != nil {
		t.Fatalf("viewer insights error = %v", err)
	}
	reviewer := fixture.tenant
	reviewer.Role = domain.RoleReviewer
	if _, err := insightService.Query(ctx, reviewer, insightapp.QueryInput{Limit: 50}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("reviewer insights error = %v", err)
	}

	foreign := addTenantReviewFixture(t, fixture)
	foreignTrip := seedManualTrip(t, foreign, "foreign-insight-trip", "成都", "2026-09-01", "2026-09-03")
	if _, err := insightService.Query(ctx, fixture.tenant, insightapp.QueryInput{
		Filter: domain.InsightFilter{TripScope: domain.InsightTripAssigned, TripID: foreignTrip.TripID}, Limit: 50,
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant trip filter error = %v", err)
	}

	if err := factService.Delete(ctx, fixture.tenant, domain.DocumentInvoice, invoice.FactID, "delete-insight-invoice"); err != nil {
		t.Fatal(err)
	}
	deleteManualTrip(t, fixture, trip, "delete-insight-trip")
	afterDeletion, err := insightService.Query(ctx, fixture.tenant, insightapp.QueryInput{Limit: 50})
	if err != nil || len(afterDeletion.Items) != 1 || afterDeletion.Items[0].FactID != payment.FactID || afterDeletion.Items[0].Trip != nil {
		t.Fatalf("insights after deletion = %#v / %v", afterDeletion, err)
	}
}
