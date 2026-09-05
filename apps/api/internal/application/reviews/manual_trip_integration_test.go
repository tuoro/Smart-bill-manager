package reviews

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestManualTripContainsMultipleReviewedTicketsWithoutCreatingContainers(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	reviews := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	input := tripapp.ManagementInput{Details: domain.TripDetails{Name: "合成多票出差", StartDate: "2026-08-26", EndDate: "2026-08-30", Timezone: "Asia/Shanghai", Notes: "合成备注"},
		Reason: "人工新建", IdempotencyKey: "manual-container-create", RequestID: "manual-container-request"}
	created, err := service.Manage(ctx, fixture.tenant, "", "create", input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Manage(ctx, fixture.tenant, "", "create", input)
	if err != nil || !replay.Replayed || replay.TripID != created.TripID {
		t.Fatalf("create replay = %#v / %v", replay, err)
	}
	changed := input
	changed.Details.Name = "不同名称"
	if _, err := service.Manage(ctx, fixture.tenant, "", "create", changed); !hasRuleCode(err, "idempotency_key_conflict") {
		t.Fatalf("changed key: %v", err)
	}
	var materials []string
	for index, destination := range []string{"北京", "上海"} {
		label := fmt.Sprintf("manual-ticket-%d", index)
		review := seedAdditionalReview(t, fixture, tripEnvelope("合成出发站", destination, "2026-08-26", "2026-08-30"), label)
		confirmed, err := reviews.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{ExpectedRevision: review.Revision, IdempotencyKey: label + "-confirm", RequestID: label + "-request"})
		if err != nil {
			t.Fatal(err)
		}
		materials = append(materials, confirmed.FactID)
		assignment := tripapp.MaterialInput{EvidenceID: confirmed.FactID, DesiredTripID: &created.TripID, ExpectedVersion: 1,
			Reason: "关联合成往返机票", IdempotencyKey: label + "-link", RequestID: label + "-link-request"}
		linked, err := service.AssignMaterial(ctx, fixture.tenant, assignment)
		if err != nil || linked.LinkID == "" || linked.Version != 2 {
			t.Fatalf("link material: %#v / %v", linked, err)
		}
		replayed, err := service.AssignMaterial(ctx, fixture.tenant, assignment)
		if err != nil || !replayed.Replayed || replayed.LinkID != linked.LinkID {
			t.Fatalf("material replay: %#v / %v", replayed, err)
		}
	}
	containers, err := service.List(ctx, fixture.tenant)
	if err != nil || len(containers) != 1 || containers[0].MaterialCount != 2 || containers[0].AssignedPaymentCount != 0 || containers[0].AssignedInvoiceCount != 0 {
		t.Fatalf("one workspace, two tickets, no invented expense: %#v / %v", containers, err)
	}
	page, err := service.Materials(ctx, fixture.tenant, created.TripID, "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("material page: %#v / %v", page, err)
	}
	next, err := service.Materials(ctx, fixture.tenant, created.TripID, page.NextCursor, 1)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.NextCursor != "" {
		t.Fatalf("material next: %#v / %v", next, err)
	}
	other := seedManualTrip(t, fixture, "manual-other", "另一趟出差", "2026-09-01", "2026-09-02")
	first := page.Items[0]
	moveInput := tripapp.MaterialInput{EvidenceID: first.ID, DesiredTripID: &other.TripID, ExpectedLinkID: &first.CurrentLinkID,
		ExpectedVersion: first.Version, Reason: "明确移入另一行程", IdempotencyKey: "manual-material-move", RequestID: "manual-material-move-request"}
	moved, err := service.AssignMaterial(ctx, fixture.tenant, moveInput)
	if err != nil || moved.Version != 3 {
		t.Fatalf("move: %#v / %v", moved, err)
	}
	moveInput.IdempotencyKey = "manual-stale-material"
	if _, err := service.AssignMaterial(ctx, fixture.tenant, moveInput); !hasRuleCode(err, "trip_assignment_stale") {
		t.Fatalf("stale material: %v", err)
	}
	if _, err := service.AssignMaterial(ctx, fixture.tenant, tripapp.MaterialInput{EvidenceID: first.ID, ExpectedLinkID: &moved.LinkID, ExpectedVersion: moved.Version,
		Reason: "撤销误关联", IdempotencyKey: "manual-material-unassign", RequestID: "manual-material-unassign-request"}); err != nil {
		t.Fatal(err)
	}
	deleteManualTrip(t, fixture, created, "manual-delete-container")
	allMaterials, err := service.Materials(ctx, fixture.tenant, "", "", 10)
	if err != nil || len(allMaterials.Items) != 2 {
		t.Fatalf("container deletion removed evidence: %#v / %v", allMaterials, err)
	}
	for _, item := range allMaterials.Items {
		if item.CurrentTripID != "" {
			t.Fatal("deleted container retained material link")
		}
	}
	if len(materials) != 2 {
		t.Fatal("missing tickets")
	}
}

func TestTripTimeAttributionBoundariesOverlapsAndManualPriority(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	trip := seedManualTrip(t, fixture, "time-container", "合成日期行程", "2026-08-27", "2026-08-27")
	for index, item := range []struct {
		time     string
		assigned bool
	}{
		{"2026-08-26T23:59:59+08:00", false}, {"2026-08-27T00:00:00+08:00", true},
		{"2026-08-27T23:59:59+08:00", true}, {"2026-08-28T00:00:00+08:00", false},
	} {
		id := confirmTripPayment(t, fixture, fmt.Sprintf("boundary-%d", index), item.time)
		expected := ""
		if item.assigned {
			expected = trip.TripID
		}
		assertPaymentTrip(t, fixture, id, expected, "auto")
	}
	payment := confirmTripPayment(t, fixture, "overlap-payment", "2026-08-27T12:00:00+08:00")
	overlap := seedManualTrip(t, fixture, "overlap-container", "合成重叠行程", "2026-08-27", "2026-08-28")
	assertPaymentTrip(t, fixture, payment, "", "auto")
	page, err := service.AttributionCandidates(ctx, fixture.tenant, trip.TripID, "all", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	foundOverlap := false
	for _, item := range page.Items {
		if item.FactID == payment {
			foundOverlap = item.AssignmentState == "overlap" && item.MatchCount == 2
		}
	}
	if !foundOverlap {
		t.Fatal("overlap not surfaced to human review")
	}
	manual, err := service.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{FactType: domain.DocumentPayment, FactID: payment, DesiredTripID: &trip.TripID,
		ExpectedFactVersion: assignmentVersion(t, fixture, domain.DocumentPayment, payment), Reason: "人工选择重叠行程", IdempotencyKey: "overlap-manual-choose", RequestID: "overlap-manual-request"})
	if err != nil {
		t.Fatal(err)
	}
	deleteManualTrip(t, fixture, overlap, "delete-overlap")
	assertPaymentTrip(t, fixture, payment, trip.TripID, "manual")
	if _, err := service.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{FactType: domain.DocumentPayment, FactID: payment,
		ExpectedAssignmentID: &manual.AssignmentID, ExpectedFactVersion: manual.FactVersion, Reason: "保持无归属", IdempotencyKey: "manual-block-again", RequestID: "manual-block-request"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Preference(ctx, fixture.tenant, payment, "auto", "restore-auto-request", assignmentVersion(t, fixture, domain.DocumentPayment, payment)); err != nil {
		t.Fatal(err)
	}
	assertPaymentTrip(t, fixture, payment, trip.TripID, "auto")
	if err := service.Preference(ctx, fixture.tenant, payment, "blocked", "block-auto-request", assignmentVersion(t, fixture, domain.DocumentPayment, payment)); err != nil {
		t.Fatal(err)
	}
	oldRangePayment := confirmTripPayment(t, fixture, "edit-old-range", "2026-08-27T14:00:00+08:00")
	newRangePayment := confirmTripPayment(t, fixture, "edit-new-range", "2026-09-01T14:00:00+08:00")
	assertPaymentTrip(t, fixture, oldRangePayment, trip.TripID, "auto")
	assertPaymentTrip(t, fixture, newRangePayment, "", "auto")
	edit := tripapp.ManagementInput{Details: domain.TripDetails{Name: "缩小日期", StartDate: "2026-09-01", EndDate: "2026-09-02", Timezone: "Asia/Shanghai"},
		ExpectedVersion: trip.Version, Reason: "调整日期", IdempotencyKey: "change-time-range", RequestID: "change-time-request"}
	if _, err := service.Manage(ctx, fixture.tenant, trip.TripID, "edit", edit); err != nil {
		t.Fatal(err)
	}
	assertPaymentTrip(t, fixture, payment, "", "blocked")
	assertPaymentTrip(t, fixture, oldRangePayment, "", "auto")
	assertPaymentTrip(t, fixture, newRangePayment, trip.TripID, "auto")
	if _, err := service.Manage(ctx, fixture.tenant, trip.TripID, "edit", tripapp.ManagementInput{Details: edit.Details, ExpectedVersion: trip.Version,
		Reason: "旧版本不得覆盖", IdempotencyKey: "stale-time-range", RequestID: "stale-time-request"}); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale management: %v", err)
	}
}

func TestTripAutoRecalculationUsesMultipleKeysetBatches(t *testing.T) {
	fixture := newReviewFixture(t)
	payments := make([]string, 0, 101)
	for index := 0; index < 101; index++ {
		payments = append(payments, confirmTripPayment(t, fixture, fmt.Sprintf("keyset-%03d", index), "2026-08-27T12:00:00+08:00"))
	}
	trip := seedManualTrip(t, fixture, "keyset-container", "合成分页行程", "2026-08-27", "2026-08-27")
	for _, payment := range payments {
		assertPaymentTrip(t, fixture, payment, trip.TripID, "auto")
	}
	deleteManualTrip(t, fixture, trip, "keyset-delete")
	for _, payment := range payments {
		assertPaymentTrip(t, fixture, payment, "", "auto")
	}
}

func confirmTripPayment(t *testing.T, fixture reviewFixture, label, transactionTime string) string {
	t.Helper()
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	review := seedAdditionalReview(t, fixture, paymentEnvelopeAt("合成商户 "+label, transactionTime), label)
	result, err := service.Confirm(context.Background(), fixture.tenant, review.Job.ID, ConfirmInput{ExpectedRevision: review.Revision,
		AssociationMode: AssociationNoCandidate, IdempotencyKey: label + "-confirm", RequestID: label + "-request"})
	if err != nil {
		t.Fatal(err)
	}
	return result.FactID
}

func assertPaymentTrip(t *testing.T, fixture reviewFixture, paymentID, tripID, mode string) {
	t.Helper()
	var actualTrip, actualMode string
	err := fixture.store.DB().QueryRowContext(context.Background(), `SELECT coalesce(a.trip_id, ''), p.trip_assignment_mode
		FROM payments p LEFT JOIN trip_fact_assignments a ON a.tenant_id = p.tenant_id AND a.payment_id = p.id AND a.ended_at IS NULL
		WHERE p.tenant_id = ? AND p.id = ?`, fixture.tenant.TenantID, paymentID).Scan(&actualTrip, &actualMode)
	if err != nil || actualTrip != tripID || actualMode != mode {
		t.Fatalf("payment attribution mismatch: mode=%s wanted=%s tripMatches=%t error=%v", actualMode, mode, actualTrip == tripID, err)
	}
}
