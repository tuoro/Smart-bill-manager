package reviews

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestTripConfirmationIsAtomicTraceableAndIdempotent(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(3 * time.Hour)})
	review := seedAdditionalReview(t, fixture, tripEnvelope("上海", "北京", "2026-08-26", "2026-08-28"), "trip-confirmation")
	revision := revisionInputFrom(review)
	destinationIndex := revisionFieldIndex(revision.Fields, "destination")
	revision.Fields[destinationIndex].Value = json.RawMessage(`"北京南"`)
	revision.Fields[destinationIndex].EvidenceIDs = []string{review.Fields[fieldIndex(review.Fields, "destination")].Evidence[0].ID}
	review, err := service.Revise(context.Background(), fixture.tenant, review.Job.ID, revision)
	if err != nil {
		t.Fatal(err)
	}
	if review.Revision != 2 || review.Status != domain.ClaimReadyForReview ||
		review.Fields[fieldIndex(review.Fields, "destination")].Source != "user" {
		t.Fatalf("Trip revision = %#v", review)
	}

	if _, err := service.Confirm(context.Background(), fixture.tenant, review.Job.ID, ConfirmInput{
		ExpectedRevision: review.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "trip-invalid-association",
		RequestID:        "trip-invalid-association-request",
	}); !hasRuleCode(err, "invalid_trip_association") {
		t.Fatalf("Trip association error = %v", err)
	}

	input := ConfirmInput{
		ExpectedRevision: review.Revision,
		IdempotencyKey:   "trip-confirmation-one",
		RequestID:        "trip-confirmation-request",
	}
	first, err := service.Confirm(context.Background(), fixture.tenant, review.Job.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.FactType != domain.DocumentTrip || first.FactID == "" || first.Replayed || len(first.LinkIDs) != 0 {
		t.Fatalf("Trip confirmation = %#v", first)
	}
	replayed, err := service.Confirm(context.Background(), fixture.tenant, review.Job.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.FactID != first.FactID || replayed.ReviewDecisionID != first.ReviewDecisionID {
		t.Fatalf("Trip replay = %#v, first = %#v", replayed, first)
	}

	var destination, startDate, endDate, factType string
	var associationMode *string
	var trips, decisions, origins, audits int
	if err := fixture.store.DB().QueryRowContext(context.Background(), `
		SELECT trip.destination, trip.start_date, trip.end_date,
		       decision.fact_type, decision.association_mode,
		       (SELECT count(*) FROM trips WHERE tenant_id = ?),
		       (SELECT count(*) FROM review_decisions WHERE tenant_id = ? AND action = 'confirm' AND fact_type = 'trip'),
		       (SELECT count(*) FROM fact_field_origins WHERE tenant_id = ? AND trip_id = trip.id),
		       (SELECT count(*) FROM audit_events WHERE tenant_id = ? AND action = 'fact_confirmed' AND resource_type = 'trip')
		FROM trips trip
		JOIN review_decisions decision
		  ON decision.tenant_id = trip.tenant_id
		 AND decision.id = trip.source_review_decision_id
		WHERE trip.tenant_id = ? AND trip.id = ?
	`,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		first.FactID,
	).Scan(
		&destination,
		&startDate,
		&endDate,
		&factType,
		&associationMode,
		&trips,
		&decisions,
		&origins,
		&audits,
	); err != nil {
		t.Fatal(err)
	}
	if destination != "北京南" || startDate != "2026-08-26" || endDate != "2026-08-28" ||
		factType != "trip" || associationMode != nil || trips != 1 || decisions != 1 || origins != 7 || audits != 1 {
		t.Fatalf("Trip persistence = destination:%q dates:%s/%s type:%s mode:%v counts:%d/%d/%d/%d",
			destination, startDate, endDate, factType, associationMode, trips, decisions, origins, audits)
	}
}

func TestTripAttributionAssignmentLifecycleAndDeletion(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(3 * time.Hour)})
	factService := NewFactService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(8 * time.Hour)})
	tripService := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(6 * time.Hour)})

	paymentReview, err := reviewService.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment, err := reviewService.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: paymentReview.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "trip-seed-payment",
		RequestID:        "trip-seed-payment-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	invoiceReview := seedAdditionalReview(t, fixture, invoiceEnvelope("TRIP-INVOICE-1"), "trip-invoice")
	if len(invoiceReview.Candidates) != 1 || invoiceReview.Candidates[0].TargetID != payment.FactID {
		t.Fatalf("Trip-linked invoice candidates = %#v", invoiceReview.Candidates)
	}
	invoice, err := reviewService.Confirm(ctx, fixture.tenant, invoiceReview.Job.ID, ConfirmInput{
		ExpectedRevision: invoiceReview.Revision,
		AssociationMode:  AssociationAllocateCandidates,
		Allocations: []domain.AllocationRequest{{
			CandidateID:    invoiceReview.Candidates[0].ID,
			AllocatedMinor: invoiceReview.Candidates[0].RemainingMinor,
		}},
		IdempotencyKey: "trip-seed-invoice",
		RequestID:      "trip-seed-invoice-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	tripOneReview := seedAdditionalReview(t, fixture, tripEnvelope("上海", "北京", "2026-08-26", "2026-08-28"), "trip-one")
	tripOne, err := reviewService.Confirm(ctx, fixture.tenant, tripOneReview.Job.ID, ConfirmInput{
		ExpectedRevision: tripOneReview.Revision,
		IdempotencyKey:   "trip-seed-one",
		RequestID:        "trip-seed-one-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	tripTwoReview := seedAdditionalReview(t, fixture, tripEnvelope("北京", "深圳", "2026-09-10", "2026-09-12"), "trip-two")
	tripTwo, err := reviewService.Confirm(ctx, fixture.tenant, tripTwoReview.Job.ID, ConfirmInput{
		ExpectedRevision: tripTwoReview.Revision,
		IdempotencyKey:   "trip-seed-two",
		RequestID:        "trip-seed-two-request",
	})
	if err != nil {
		t.Fatal(err)
	}

	page, err := tripService.AttributionCandidates(ctx, fixture.tenant, tripOne.FactID, domain.TripAttributionViewSuggested, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.RuleVersion != domain.TripAttributionRuleVersion || len(page.Items) != 1 || page.NextCursor == "" ||
		!page.Items[0].Suggested || !containsString(page.Items[0].ReasonCodes, "date_inside_trip") {
		t.Fatalf("first Trip attribution page = %#v", page)
	}
	next, err := tripService.AttributionCandidates(ctx, fixture.tenant, tripOne.FactID, domain.TripAttributionViewSuggested, page.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].FactID == page.Items[0].FactID {
		t.Fatalf("second Trip attribution page = %#v", next)
	}
	if _, err := tripService.AttributionCandidates(ctx, fixture.tenant, tripTwo.FactID, domain.TripAttributionViewSuggested, page.NextCursor, 1); !hasRuleCode(err, "invalid_cursor") {
		t.Fatalf("cross-query cursor error = %v", err)
	}
	confirmPaymentAt := func(label, merchant, transactionTime string) string {
		t.Helper()
		review := seedAdditionalReview(t, fixture, paymentEnvelopeAt(merchant, transactionTime), label)
		result, confirmErr := reviewService.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{
			ExpectedRevision: review.Revision,
			AssociationMode:  AssociationNoCandidate,
			IdempotencyKey:   label + "-confirm",
			RequestID:        label + "-request",
		})
		if confirmErr != nil {
			t.Fatal(confirmErr)
		}
		return result.FactID
	}
	beforeID := confirmPaymentAt("trip-near-before", "Before Merchant", "2026-08-23T12:00:00+08:00")
	afterID := confirmPaymentAt("trip-near-after", "After Merchant", "2026-08-31T12:00:00+08:00")
	outsideID := confirmPaymentAt("trip-outside-window", "Outside Merchant", "2026-08-22T12:00:00+08:00")
	allCandidates, err := tripService.AttributionCandidates(ctx, fixture.tenant, tripOne.FactID, domain.TripAttributionViewAll, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	assertTripCandidateReason(t, allCandidates.Items, beforeID, true, "date_within_3_days_before")
	assertTripCandidateReason(t, allCandidates.Items, afterID, true, "date_within_3_days_after")
	assertTripCandidateReason(t, allCandidates.Items, outsideID, false, "")

	desiredTripOne := tripOne.FactID
	assignInput := tripapp.AssignmentInput{
		FactType:       domain.DocumentPayment,
		FactID:         payment.FactID,
		DesiredTripID:  &desiredTripOne,
		Reason:         "支付发生在行程日期内",
		IdempotencyKey: "trip-assign-payment-one",
		RequestID:      "trip-assign-payment-one-request",
	}
	assigned, err := tripService.Assign(ctx, fixture.tenant, assignInput)
	if err != nil {
		t.Fatal(err)
	}
	if assigned.Action != "assign" || assigned.AssignmentID == "" || assigned.Replayed {
		t.Fatalf("Trip assignment = %#v", assigned)
	}
	assignedPage, err := tripService.AttributionCandidates(ctx, fixture.tenant, tripOne.FactID, domain.TripAttributionViewAssigned, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignedPage.Items) != 1 || assignedPage.Items[0].FactID != payment.FactID ||
		!containsString(assignedPage.Items[0].ReasonCodes, "currently_assigned") {
		t.Fatalf("assigned Trip attribution page = %#v", assignedPage)
	}
	replayed, err := tripService.Assign(ctx, fixture.tenant, assignInput)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.AssignmentID != assigned.AssignmentID {
		t.Fatalf("Trip assignment replay = %#v", replayed)
	}
	changedReplay := assignInput
	changedReplay.Reason = "同键不得改变理由"
	if _, err := tripService.Assign(ctx, fixture.tenant, changedReplay); !hasRuleCode(err, "idempotency_key_conflict") {
		t.Fatalf("changed assignment replay error = %v", err)
	}

	desiredTripTwo := tripTwo.FactID
	expectedAssigned := assigned.AssignmentID
	moved, err := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
		FactType:             domain.DocumentPayment,
		FactID:               payment.FactID,
		DesiredTripID:        &desiredTripTwo,
		ExpectedAssignmentID: &expectedAssigned,
		Reason:               "明确改归另一行程",
		IdempotencyKey:       "trip-move-payment-two",
		RequestID:            "trip-move-payment-two-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Action != "move" || moved.AssignmentID == "" || moved.PreviousAssignmentID != assigned.AssignmentID {
		t.Fatalf("Trip move = %#v", moved)
	}
	expectedMoved := moved.AssignmentID
	unassigned, err := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
		FactType:             domain.DocumentPayment,
		FactID:               payment.FactID,
		ExpectedAssignmentID: &expectedMoved,
		Reason:               "撤销错误归属",
		IdempotencyKey:       "trip-unassign-payment",
		RequestID:            "trip-unassign-payment-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if unassigned.Action != "unassign" || unassigned.AssignmentID != "" || unassigned.PreviousAssignmentID != moved.AssignmentID {
		t.Fatalf("Trip unassign = %#v", unassigned)
	}

	if _, err := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
		FactType:             domain.DocumentPayment,
		FactID:               payment.FactID,
		DesiredTripID:        &desiredTripOne,
		ExpectedAssignmentID: &expectedMoved,
		Reason:               "陈旧快照不得覆盖",
		IdempotencyKey:       "trip-stale-payment",
		RequestID:            "trip-stale-payment-request",
	}); !hasRuleCode(err, "trip_assignment_stale") {
		t.Fatalf("stale assignment error = %v", err)
	}

	viewer := fixture.tenant
	viewer.Role = domain.RoleViewer
	if _, err := tripService.AttributionCandidates(ctx, viewer, tripOne.FactID, domain.TripAttributionViewAll, "", 50); err != nil {
		t.Fatalf("viewer Trip attribution read = %v", err)
	}
	if _, err := tripService.Assign(ctx, viewer, assignInput); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer Trip assignment error = %v", err)
	}
	reviewer := fixture.tenant
	reviewer.Role = domain.RoleReviewer
	if _, err := tripService.AttributionCandidates(ctx, reviewer, tripOne.FactID, domain.TripAttributionViewAll, "", 50); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("reviewer Trip attribution error = %v", err)
	}
	foreign := addTenantReviewFixture(t, fixture)
	foreignTripReview := seedAdditionalReview(t, foreign, tripEnvelope("广州", "成都", "2026-09-20", "2026-09-22"), "foreign-trip")
	foreignTrip, err := reviewService.Confirm(ctx, foreign.tenant, foreignTripReview.Job.ID, ConfirmInput{
		ExpectedRevision: foreignTripReview.Revision,
		IdempotencyKey:   "foreign-trip-confirm",
		RequestID:        "foreign-trip-confirm-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tripService.AttributionCandidates(ctx, fixture.tenant, foreignTrip.FactID, domain.TripAttributionViewAll, "", 50); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant Trip read error = %v", err)
	}
	foreignTripID := foreignTrip.FactID
	if _, err := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
		FactType:       domain.DocumentPayment,
		FactID:         payment.FactID,
		DesiredTripID:  &foreignTripID,
		Reason:         "跨租户目标必须拒绝",
		IdempotencyKey: "trip-cross-tenant-target",
		RequestID:      "trip-cross-tenant-target-request",
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant Trip assignment error = %v", err)
	}

	desiredTripOne = tripOne.FactID
	finance := fixture.tenant
	finance.Role = domain.RoleFinance
	if _, err := tripService.AttributionCandidates(ctx, finance, tripOne.FactID, domain.TripAttributionViewAll, "", 50); err != nil {
		t.Fatalf("finance Trip attribution read = %v", err)
	}
	invoiceAssignment, err := tripService.Assign(ctx, finance, tripapp.AssignmentInput{
		FactType:       domain.DocumentInvoice,
		FactID:         invoice.FactID,
		DesiredTripID:  &desiredTripOne,
		Reason:         "发票属于当前行程",
		IdempotencyKey: "trip-assign-invoice",
		RequestID:      "trip-assign-invoice-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	linkedPage, err := tripService.AttributionCandidates(ctx, fixture.tenant, tripOne.FactID, domain.TripAttributionViewAll, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	assertTripCandidateReason(t, linkedPage.Items, payment.FactID, true, "linked_fact_assigned_to_trip")
	if err := factService.Delete(ctx, fixture.tenant, domain.DocumentTrip, tripOne.FactID, "delete-trip-request"); err != nil {
		t.Fatal(err)
	}

	trips, err := factService.ListTrips(ctx, fixture.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 || trips[0].ID != tripTwo.FactID {
		t.Fatalf("Trips after deletion = %#v", trips)
	}
	var active, ended, decisions, audits int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT
		  (SELECT count(*) FROM trip_fact_assignments WHERE tenant_id = ? AND ended_at IS NULL),
		  (SELECT count(*) FROM trip_fact_assignments WHERE tenant_id = ? AND id = ? AND ended_at IS NOT NULL AND ended_by_audit_event_id IS NOT NULL),
		  (SELECT count(*) FROM trip_fact_assignment_decisions WHERE tenant_id = ?),
		  (SELECT count(*) FROM audit_events WHERE tenant_id = ? AND action = 'trip_fact_assignment_changed')
	`,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		invoiceAssignment.AssignmentID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
	).Scan(&active, &ended, &decisions, &audits); err != nil {
		t.Fatal(err)
	}
	if active != 0 || ended != 1 || decisions != 4 || audits != 4 {
		t.Fatalf("Trip assignment history after deletion = active:%d ended:%d decisions:%d audits:%d", active, ended, decisions, audits)
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE trip_fact_assignment_decisions SET reason = 'mutated' WHERE tenant_id = ?
	`, fixture.tenant.TenantID); err == nil {
		t.Fatal("Trip assignment decision mutation was accepted")
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		DELETE FROM trip_fact_assignments WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, invoiceAssignment.AssignmentID); err == nil {
		t.Fatal("Trip assignment history deletion was accepted")
	}
	rows, err := fixture.store.DB().QueryContext(ctx, `
		SELECT safe_metadata_json
		FROM audit_events
		WHERE tenant_id = ? AND action = 'trip_fact_assignment_changed'
		ORDER BY created_at, id
	`, fixture.tenant.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var metadata string
		if err := rows.Scan(&metadata); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(metadata, "行程") || strings.Contains(metadata, "归属") || strings.Contains(metadata, "北京") || strings.Contains(metadata, "深圳") {
			t.Fatalf("Trip assignment audit leaked business content: %s", metadata)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentTripAssignmentsAllowOneWinner(t *testing.T) {
	fixture := newFileReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(3 * time.Hour)})
	tripService := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(6 * time.Hour)})
	paymentReview, err := reviewService.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment, err := reviewService.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: paymentReview.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "trip-concurrent-payment",
		RequestID:        "trip-concurrent-payment-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	tripIDs := make([]string, 0, 2)
	for index, destination := range []string{"北京", "深圳"} {
		review := seedAdditionalReview(
			t,
			fixture,
			tripEnvelope("上海", destination, "2026-08-26", "2026-08-28"),
			"trip-concurrent-"+string(rune('a'+index)),
		)
		confirmed, confirmErr := reviewService.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{
			ExpectedRevision: review.Revision,
			IdempotencyKey:   "trip-concurrent-confirm-" + string(rune('a'+index)),
			RequestID:        "trip-concurrent-confirm-request",
		})
		if confirmErr != nil {
			t.Fatal(confirmErr)
		}
		tripIDs = append(tripIDs, confirmed.FactID)
	}

	type outcome struct {
		resultTripID string
		err          error
	}
	outcomes := make(chan outcome, 2)
	var wait sync.WaitGroup
	for index, tripID := range tripIDs {
		wait.Add(1)
		go func(index int, tripID string) {
			defer wait.Done()
			_, assignErr := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
				FactType:       domain.DocumentPayment,
				FactID:         payment.FactID,
				DesiredTripID:  &tripID,
				Reason:         "并发归属到不同合成行程",
				IdempotencyKey: "trip-concurrent-assign-" + string(rune('a'+index)),
				RequestID:      "trip-concurrent-assign-request-" + string(rune('a'+index)),
			})
			outcomes <- outcome{resultTripID: tripID, err: assignErr}
		}(index, tripID)
	}
	wait.Wait()
	close(outcomes)
	successes, conflicts := 0, 0
	winnerTripID := ""
	for item := range outcomes {
		if item.err == nil {
			successes++
			winnerTripID = item.resultTripID
			continue
		}
		if errors.Is(item.err, domain.ErrConflict) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent Trip assignment error = %v", item.err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent Trip assignment outcomes = successes:%d conflicts:%d", successes, conflicts)
	}
	var active, decisions, audits int
	var persistedTripID string
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT assignment.trip_id,
		       (SELECT count(*) FROM trip_fact_assignments WHERE tenant_id = ? AND payment_id = ? AND ended_at IS NULL),
		       (SELECT count(*) FROM trip_fact_assignment_decisions WHERE tenant_id = ? AND payment_id = ?),
		       (SELECT count(*) FROM audit_events WHERE tenant_id = ? AND action = 'trip_fact_assignment_changed' AND resource_id = ?)
		FROM trip_fact_assignments assignment
		WHERE assignment.tenant_id = ? AND assignment.payment_id = ? AND assignment.ended_at IS NULL
	`,
		fixture.tenant.TenantID, payment.FactID,
		fixture.tenant.TenantID, payment.FactID,
		fixture.tenant.TenantID, payment.FactID,
		fixture.tenant.TenantID, payment.FactID,
	).Scan(&persistedTripID, &active, &decisions, &audits); err != nil {
		t.Fatal(err)
	}
	if persistedTripID != winnerTripID || active != 1 || decisions != 1 || audits != 1 {
		t.Fatalf("concurrent Trip persistence = trip:%s winner:%s active:%d decisions:%d audits:%d", persistedTripID, winnerTripID, active, decisions, audits)
	}
}

func tripEnvelope(origin, destination, startDate, endDate string) domain.ClaimEnvelope {
	evidence := func(quote string) []domain.CandidateEvidence {
		return []domain.CandidateEvidence{{Page: 1, Quote: quote}}
	}
	return domain.ClaimEnvelope{
		SchemaVersion: "document-claim/3",
		DocumentType:  string(domain.DocumentTrip),
		Fields: []domain.FieldCandidate{
			{Path: "origin", ValueType: "string", Presence: "present", Value: json.RawMessage(`"` + origin + `"`), Evidence: evidence(origin), Issues: []string{}},
			{Path: "destination", ValueType: "string", Presence: "present", Value: json.RawMessage(`"` + destination + `"`), Evidence: evidence(destination), Issues: []string{}},
			{Path: "start_date", ValueType: "date", Presence: "present", Value: json.RawMessage(`"` + startDate + `"`), Evidence: evidence(startDate), Issues: []string{}},
			{Path: "end_date", ValueType: "date", Presence: "present", Value: json.RawMessage(`"` + endDate + `"`), Evidence: evidence(endDate), Issues: []string{}},
			{Path: "traveler_name", ValueType: "string", Presence: "present", Value: json.RawMessage(`"张三"`), Evidence: evidence("张三"), Issues: []string{}},
			{Path: "transport_type", ValueType: "string", Presence: "present", Value: json.RawMessage(`"高铁"`), Evidence: evidence("高铁"), Issues: []string{}},
			{Path: "booking_reference", ValueType: "string", Presence: "present", Value: json.RawMessage(`"TRIP-001"`), Evidence: evidence("TRIP-001"), Issues: []string{}},
			{
				Path: "supplementary_fields", ValueType: "supplementary", Presence: "present",
				Value:    json.RawMessage(`[{"path":"trip.note","value":"synthetic"}]`),
				Evidence: evidence("synthetic"), Issues: []string{},
			},
		},
		DocumentIssues: []string{},
	}
}

func paymentEnvelopeAt(merchant, transactionTime string) domain.ClaimEnvelope {
	envelope := paymentEnvelope()
	for index := range envelope.Fields {
		switch envelope.Fields[index].Path {
		case "merchant":
			envelope.Fields[index].Value = json.RawMessage(`"` + merchant + `"`)
			envelope.Fields[index].Evidence = []domain.CandidateEvidence{{Page: 1, Quote: merchant}}
		case "transaction_time":
			envelope.Fields[index].Value = json.RawMessage(`"` + transactionTime + `"`)
			envelope.Fields[index].Evidence = []domain.CandidateEvidence{{Page: 1, Quote: transactionTime}}
		}
	}
	return envelope
}

func assertTripCandidateReason(
	t *testing.T,
	items []ports.TripAttributionCandidate,
	factID string,
	suggested bool,
	reason string,
) {
	t.Helper()
	for _, item := range items {
		if item.FactID != factID {
			continue
		}
		if item.Suggested != suggested || (reason != "" && !containsString(item.ReasonCodes, reason)) {
			t.Fatalf("Trip candidate %s = %#v", factID, item)
		}
		return
	}
	t.Fatalf("Trip candidate %s was not returned", factID)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
