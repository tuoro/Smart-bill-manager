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
	allocationapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	reimbursementapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestReimbursementSnapshotPolicyAndStatusLifecycle(t *testing.T) {
	fixture := newFileReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(3 * time.Hour)})
	tripService := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(4 * time.Hour)})
	allocationService := allocationapp.NewService(
		fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(4*time.Hour + 30*time.Minute)},
	)
	reimbursementService := reimbursementapp.NewService(
		fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(5 * time.Hour)},
	)
	factService := NewFactService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(6 * time.Hour)})

	paymentReview, err := reviewService.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment, err := reviewService.Confirm(ctx, fixture.tenant, paymentReview.Job.ID, ConfirmInput{
		ExpectedRevision: paymentReview.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "reimbursement-seed-payment",
		RequestID:        "reimbursement-seed-payment-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	invoiceReview := seedAdditionalReview(t, fixture, invoiceEnvelope("REIMBURSEMENT-INVOICE-1"), "reimbursement-invoice")
	if len(invoiceReview.Candidates) != 1 || invoiceReview.Candidates[0].TargetID != payment.FactID {
		t.Fatalf("invoice candidates = %#v", invoiceReview.Candidates)
	}
	invoice, err := reviewService.Confirm(ctx, fixture.tenant, invoiceReview.Job.ID, ConfirmInput{
		ExpectedRevision: invoiceReview.Revision,
		AssociationMode:  AssociationAllocateCandidates,
		Allocations: []domain.AllocationRequest{{
			CandidateID: invoiceReview.Candidates[0].ID, AllocatedMinor: invoiceReview.Candidates[0].RemainingMinor,
		}},
		IdempotencyKey: "reimbursement-seed-invoice",
		RequestID:      "reimbursement-seed-invoice-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	tripReview := seedAdditionalReview(
		t, fixture, tripEnvelope("上海", "北京", "2026-08-26", "2026-08-28"), "reimbursement-trip",
	)
	trip, err := reviewService.Confirm(ctx, fixture.tenant, tripReview.Job.ID, ConfirmInput{
		ExpectedRevision: tripReview.Revision,
		IdempotencyKey:   "reimbursement-seed-trip",
		RequestID:        "reimbursement-seed-trip-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	desiredTripID := trip.FactID
	assign := func(factType domain.DocumentType, factID, label string) string {
		t.Helper()
		result, assignErr := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
			FactType: factType, FactID: factID, DesiredTripID: &desiredTripID,
			Reason: "合成报销归属", IdempotencyKey: "reimbursement-assign-" + label,
			RequestID: "reimbursement-assign-" + label + "-request",
		})
		if assignErr != nil {
			t.Fatal(assignErr)
		}
		return result.AssignmentID
	}
	assignmentIDs := []string{
		assign(domain.DocumentPayment, payment.FactID, "payment"),
		assign(domain.DocumentInvoice, invoice.FactID, "invoice"),
	}

	preview, err := reimbursementService.Preview(ctx, fixture.tenant, trip.FactID, []string{assignmentIDs[1], assignmentIDs[0]})
	if err != nil {
		t.Fatal(err)
	}
	if preview.RuleVersion != domain.ReimbursementPolicyVersion || len(preview.Items) != 2 || len(preview.Findings) != 0 ||
		len(preview.Totals) != 1 || preview.Totals[0].Currency != domain.CurrencyCNY || preview.Totals[0].AmountMinor != 24690 ||
		!domain.ValidSHA256Hex(preview.SnapshotHash) {
		t.Fatalf("initial reimbursement preview = %#v", preview)
	}

	initialWorkspace, err := allocationService.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil || len(initialWorkspace.Links) != 1 {
		t.Fatalf("initial reimbursement allocation workspace = %#v, err = %v", initialWorkspace, err)
	}
	if _, err := allocationService.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash: initialWorkspace.PlanHash, DesiredAllocations: []domain.DesiredAllocation{},
		Reason: "合成报销预检后撤销分配", IdempotencyKey: "reimbursement-withdraw-link",
		RequestID: "reimbursement-withdraw-link-request",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reimbursementService.Submit(ctx, fixture.tenant, reimbursementapp.SubmissionInput{
		TripID: trip.FactID, AssignmentIDs: assignmentIDs, ExpectedSnapshotHash: preview.SnapshotHash,
		AcknowledgedFindingKeys: []string{}, Reason: "活动 Link 变化后旧预检必须失效",
		IdempotencyKey: "reimbursement-submit-stale-link", RequestID: "reimbursement-submit-stale-link-request",
	}); !hasRuleCode(err, "reimbursement_snapshot_stale") {
		t.Fatalf("changed Link snapshot error = %v", err)
	}
	withdrawnWorkspace, err := allocationService.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := allocationService.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash: withdrawnWorkspace.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{{
			TargetFactID: invoice.FactID, AllocatedMinor: initialWorkspace.Anchor.AmountMinor,
		}},
		Reason: "合成报销预检后恢复分配", IdempotencyKey: "reimbursement-restore-link",
		RequestID: "reimbursement-restore-link-request",
	}); err != nil {
		t.Fatal(err)
	}
	restoredPreview, err := reimbursementService.Preview(ctx, fixture.tenant, trip.FactID, assignmentIDs)
	if err != nil || len(restoredPreview.Findings) != 0 || restoredPreview.SnapshotHash == preview.SnapshotHash {
		t.Fatalf("restored reimbursement preview = %#v, err = %v", restoredPreview, err)
	}
	if _, err := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
		FactType: domain.DocumentPayment, FactID: payment.FactID, DesiredTripID: nil,
		ExpectedAssignmentID: &assignmentIDs[0],
		Reason:               "合成报销预检后撤销行程归属", IdempotencyKey: "reimbursement-unassign-payment",
		RequestID: "reimbursement-unassign-payment-request",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reimbursementService.Submit(ctx, fixture.tenant, reimbursementapp.SubmissionInput{
		TripID: trip.FactID, AssignmentIDs: assignmentIDs, ExpectedSnapshotHash: restoredPreview.SnapshotHash,
		AcknowledgedFindingKeys: []string{}, Reason: "活动 Assignment 变化后旧预检必须失效",
		IdempotencyKey: "reimbursement-submit-stale-assignment", RequestID: "reimbursement-submit-stale-assignment-request",
	}); !hasRuleCode(err, "reimbursement_selection_stale") {
		t.Fatalf("changed Assignment snapshot error = %v", err)
	}
	reassigned, err := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
		FactType: domain.DocumentPayment, FactID: payment.FactID, DesiredTripID: &desiredTripID,
		Reason: "恢复合成报销行程归属", IdempotencyKey: "reimbursement-reassign-payment",
		RequestID: "reimbursement-reassign-payment-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	assignmentIDs[0] = reassigned.AssignmentID
	preview, err = reimbursementService.Preview(ctx, fixture.tenant, trip.FactID, assignmentIDs)
	if err != nil || len(preview.Findings) != 0 {
		t.Fatalf("current reimbursement preview = %#v, err = %v", preview, err)
	}

	if _, err := fixture.store.DB().ExecContext(ctx, `
		CREATE TEMP TRIGGER fail_reimbursement_audit
		BEFORE INSERT ON audit_events
		WHEN NEW.resource_type = 'reimbursement'
		BEGIN
			SELECT RAISE(ABORT, 'synthetic_reimbursement_audit_failure');
		END
	`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = fixture.store.DB().ExecContext(context.Background(), "DROP TRIGGER IF EXISTS fail_reimbursement_audit")
	})
	if _, err := reimbursementService.Submit(ctx, fixture.tenant, reimbursementapp.SubmissionInput{
		TripID: trip.FactID, AssignmentIDs: assignmentIDs, ExpectedSnapshotHash: preview.SnapshotHash,
		AcknowledgedFindingKeys: []string{}, Reason: "合成事务回滚验证",
		IdempotencyKey: "reimbursement-submit-rollback", RequestID: "reimbursement-submit-rollback-request",
	}); err == nil {
		t.Fatal("injected reimbursement audit failure unexpectedly succeeded")
	}
	var rolledBackReimbursements, rolledBackItems, rolledBackDecisions int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM reimbursements WHERE tenant_id = ?),
		       (SELECT count(*) FROM reimbursement_items WHERE tenant_id = ?),
		       (SELECT count(*) FROM reimbursement_status_decisions WHERE tenant_id = ?)
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID).Scan(
		&rolledBackReimbursements, &rolledBackItems, &rolledBackDecisions,
	); err != nil {
		t.Fatal(err)
	}
	if rolledBackReimbursements != 0 || rolledBackItems != 0 || rolledBackDecisions != 0 {
		t.Fatalf("failed reimbursement left rows = %d/%d/%d", rolledBackReimbursements, rolledBackItems, rolledBackDecisions)
	}
	if _, err := fixture.store.DB().ExecContext(ctx, "DROP TRIGGER fail_reimbursement_audit"); err != nil {
		t.Fatal(err)
	}
	firstInput := reimbursementapp.SubmissionInput{
		TripID: trip.FactID, AssignmentIDs: assignmentIDs, ExpectedSnapshotHash: preview.SnapshotHash,
		AcknowledgedFindingKeys: []string{}, Reason: "提交合成报销快照",
		IdempotencyKey: "reimbursement-submit-first", RequestID: "reimbursement-submit-first-request",
	}
	staleInput := firstInput
	staleInput.ExpectedSnapshotHash = strings.Repeat("c", 64)
	staleInput.IdempotencyKey = "reimbursement-submit-stale"
	if _, err := reimbursementService.Submit(ctx, fixture.tenant, staleInput); !hasRuleCode(err, "reimbursement_snapshot_stale") {
		t.Fatalf("stale snapshot error = %v", err)
	}
	extraFindingInput := firstInput
	extraFindingInput.AcknowledgedFindingKeys = []string{strings.Repeat("d", 64)}
	extraFindingInput.IdempotencyKey = "reimbursement-submit-extra-finding"
	if _, err := reimbursementService.Submit(ctx, fixture.tenant, extraFindingInput); !hasRuleCode(err, "reimbursement_findings_unacknowledged") {
		t.Fatalf("extra finding acknowledgement error = %v", err)
	}
	concurrentInput := firstInput
	concurrentInput.Reason = "并发提交同一行程快照"
	concurrentInput.IdempotencyKey = "reimbursement-submit-concurrent"
	concurrentInput.RequestID = "reimbursement-submit-concurrent-request"
	type submissionOutcome struct {
		input  reimbursementapp.SubmissionInput
		result ports.ReimbursementMutationResult
		err    error
	}
	outcomes := make(chan submissionOutcome, 2)
	var wait sync.WaitGroup
	for _, input := range []reimbursementapp.SubmissionInput{firstInput, concurrentInput} {
		input := input
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, submitErr := reimbursementService.Submit(ctx, fixture.tenant, input)
			outcomes <- submissionOutcome{input: input, result: result, err: submitErr}
		}()
	}
	wait.Wait()
	close(outcomes)
	var first ports.ReimbursementMutationResult
	successes, conflicts := 0, 0
	for outcome := range outcomes {
		if outcome.err == nil {
			successes++
			first = outcome.result
			firstInput = outcome.input
			continue
		}
		if hasRuleCode(outcome.err, "reimbursement_trip_already_submitted") ||
			hasRuleCode(outcome.err, "reimbursement_snapshot_stale") {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent reimbursement error = %v", outcome.err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent reimbursement outcomes = successes:%d conflicts:%d", successes, conflicts)
	}
	if first.Status != domain.ReimbursementStatusSubmitted || first.Version != 1 || first.Replayed {
		t.Fatalf("first reimbursement = %#v", first)
	}
	replay, err := reimbursementService.Submit(ctx, fixture.tenant, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.ReimbursementID != first.ReimbursementID || replay.DecisionID != first.DecisionID || replay.Version != 1 {
		t.Fatalf("reimbursement replay = %#v", replay)
	}
	changedFirstInput := firstInput
	changedFirstInput.Reason = "同一幂等键不能更换理由"
	if _, err := reimbursementService.Submit(ctx, fixture.tenant, changedFirstInput); !hasRuleCode(err, "idempotency_key_conflict") {
		t.Fatalf("changed reimbursement replay = %v", err)
	}

	pendingPreview, err := reimbursementService.Preview(ctx, fixture.tenant, trip.FactID, assignmentIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingPreview.Findings) != 2 {
		t.Fatalf("pending duplicate findings = %#v", pendingPreview.Findings)
	}
	if _, err := reimbursementService.Submit(ctx, fixture.tenant, reimbursementapp.SubmissionInput{
		TripID: trip.FactID, AssignmentIDs: assignmentIDs, ExpectedSnapshotHash: pendingPreview.SnapshotHash,
		AcknowledgedFindingKeys: []string{}, Reason: "不能遗漏提示确认",
		IdempotencyKey: "reimbursement-submit-missing-findings", RequestID: "reimbursement-submit-missing-findings-request",
	}); !hasRuleCode(err, "reimbursement_findings_unacknowledged") {
		t.Fatalf("missing finding acknowledgements = %v", err)
	}
	if _, err := reimbursementService.Submit(ctx, fixture.tenant, reimbursementapp.SubmissionInput{
		TripID: trip.FactID, AssignmentIDs: assignmentIDs, ExpectedSnapshotHash: pendingPreview.SnapshotHash,
		AcknowledgedFindingKeys: reimbursementFindingKeysForTest(pendingPreview), Reason: "不得并存第二个待处理快照",
		IdempotencyKey: "reimbursement-submit-conflict", RequestID: "reimbursement-submit-conflict-request",
	}); !hasRuleCode(err, "reimbursement_trip_already_submitted") {
		t.Fatalf("same Trip submitted conflict = %v", err)
	}

	marked, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, first.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: domain.ReimbursementStatusReimbursed,
		ExpectedVersion: 1, Reason: "合成报销已完成",
		IdempotencyKey: "reimbursement-mark-first", RequestID: "reimbursement-mark-first-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if marked.Status != domain.ReimbursementStatusReimbursed || marked.Version != 2 {
		t.Fatalf("marked reimbursement = %#v", marked)
	}
	markedReplay, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, first.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: domain.ReimbursementStatusReimbursed,
		ExpectedVersion: 1, Reason: "合成报销已完成",
		IdempotencyKey: "reimbursement-mark-first", RequestID: "reimbursement-mark-first-request",
	})
	if err != nil || !markedReplay.Replayed || markedReplay.Version != 2 {
		t.Fatalf("marked reimbursement replay = %#v, err = %v", markedReplay, err)
	}
	if _, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, first.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: domain.ReimbursementStatusReimbursed,
		ExpectedVersion: 1, Reason: "同一幂等键不能改变状态理由",
		IdempotencyKey: "reimbursement-mark-first", RequestID: "reimbursement-mark-first-changed-request",
	}); !hasRuleCode(err, "idempotency_key_conflict") {
		t.Fatalf("changed status replay = %v", err)
	}
	reimbursedPreview, err := reimbursementService.Preview(ctx, fixture.tenant, trip.FactID, assignmentIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(reimbursedPreview.Findings) != 2 ||
		reimbursedPreview.Findings[0].Code != domain.ReimbursementFindingDuplicate ||
		reimbursedPreview.Findings[0].RelatedStatus != domain.ReimbursementStatusReimbursed {
		t.Fatalf("reimbursed duplicate findings = %#v", reimbursedPreview.Findings)
	}
	reopenedForStalePreview, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, first.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: domain.ReimbursementStatusReimbursed, DesiredStatus: domain.ReimbursementStatusSubmitted,
		ExpectedVersion: 2, Reason: "改变相关报销状态以验证旧预检失效",
		IdempotencyKey: "reimbursement-reopen-for-stale-preview", RequestID: "reimbursement-reopen-for-stale-preview-request",
	})
	if err != nil || reopenedForStalePreview.Version != 3 {
		t.Fatalf("reopen for stale preview = %#v, err = %v", reopenedForStalePreview, err)
	}
	if _, err := reimbursementService.Submit(ctx, fixture.tenant, reimbursementapp.SubmissionInput{
		TripID: trip.FactID, AssignmentIDs: assignmentIDs, ExpectedSnapshotHash: reimbursedPreview.SnapshotHash,
		AcknowledgedFindingKeys: reimbursementFindingKeysForTest(reimbursedPreview), Reason: "相关报销状态变化后旧预检必须失效",
		IdempotencyKey: "reimbursement-submit-stale-prior-status", RequestID: "reimbursement-submit-stale-prior-status-request",
	}); !hasRuleCode(err, "reimbursement_snapshot_stale") {
		t.Fatalf("changed related reimbursement status error = %v", err)
	}
	remarked, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, first.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: domain.ReimbursementStatusReimbursed,
		ExpectedVersion: 3, Reason: "恢复相关报销终态",
		IdempotencyKey: "reimbursement-remark-first", RequestID: "reimbursement-remark-first-request",
	})
	if err != nil || remarked.Version != 4 {
		t.Fatalf("remarked reimbursement = %#v, err = %v", remarked, err)
	}
	reimbursedPreview, err = reimbursementService.Preview(ctx, fixture.tenant, trip.FactID, assignmentIDs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reimbursementService.Submit(ctx, fixture.tenant, reimbursementapp.SubmissionInput{
		TripID: trip.FactID, AssignmentIDs: assignmentIDs, ExpectedSnapshotHash: reimbursedPreview.SnapshotHash,
		AcknowledgedFindingKeys: reimbursementFindingKeysForTest(reimbursedPreview), Reason: "确认重复提示后重新提交",
		IdempotencyKey: "reimbursement-submit-second", RequestID: "reimbursement-submit-second-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != domain.ReimbursementStatusSubmitted || second.ReimbursementID == first.ReimbursementID {
		t.Fatalf("second reimbursement = %#v", second)
	}
	if _, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, first.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: domain.ReimbursementStatusReimbursed, DesiredStatus: domain.ReimbursementStatusSubmitted,
		ExpectedVersion: 4, Reason: "另一待处理报销存在时不能重新打开",
		IdempotencyKey: "reimbursement-reopen-conflict", RequestID: "reimbursement-reopen-conflict-request",
	}); !hasRuleCode(err, "reimbursement_trip_already_submitted") {
		t.Fatalf("reopen uniqueness conflict = %v", err)
	}
	secondRejected, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, second.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: domain.ReimbursementStatusRejected,
		ExpectedVersion: 1, Reason: "合成报销退回",
		IdempotencyKey: "reimbursement-reject-second", RequestID: "reimbursement-reject-second-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondRejected.Status != domain.ReimbursementStatusRejected || secondRejected.Version != 2 {
		t.Fatalf("rejected reimbursement = %#v", secondRejected)
	}
	rejectedIgnoredPreview, err := reimbursementService.Preview(ctx, fixture.tenant, trip.FactID, assignmentIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(rejectedIgnoredPreview.Findings) != 2 {
		t.Fatalf("rejected reimbursement was not excluded from duplicate policy = %#v", rejectedIgnoredPreview.Findings)
	}
	for _, finding := range rejectedIgnoredPreview.Findings {
		if finding.RelatedReimbursementID == second.ReimbursementID {
			t.Fatalf("rejected reimbursement remained a duplicate prior use: %#v", finding)
		}
	}
	if _, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, second.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: domain.ReimbursementStatusReimbursed,
		ExpectedVersion: 1, Reason: "过期状态不得覆盖",
		IdempotencyKey: "reimbursement-stale-second", RequestID: "reimbursement-stale-second-request",
	}); !hasRuleCode(err, "reimbursement_status_stale") {
		t.Fatalf("stale reimbursement status = %v", err)
	}
	reopened, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, first.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: domain.ReimbursementStatusReimbursed, DesiredStatus: domain.ReimbursementStatusSubmitted,
		ExpectedVersion: 4, Reason: "重新打开原始不可变快照",
		IdempotencyKey: "reimbursement-reopen-first", RequestID: "reimbursement-reopen-first-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != domain.ReimbursementStatusSubmitted || reopened.Version != 5 {
		t.Fatalf("reopened reimbursement = %#v", reopened)
	}

	type statusOutcome struct {
		result ports.ReimbursementMutationResult
		err    error
	}
	statusOutcomes := make(chan statusOutcome, 2)
	for index, desired := range []domain.ReimbursementStatus{
		domain.ReimbursementStatusReimbursed,
		domain.ReimbursementStatusRejected,
	} {
		index, desired := index, desired
		go func() {
			result, statusErr := reimbursementService.ChangeStatus(ctx, fixture.tenant, first.ReimbursementID, reimbursementapp.StatusInput{
				ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: desired,
				ExpectedVersion: 5, Reason: "合成并发状态决定",
				IdempotencyKey: "reimbursement-status-race-" + string(rune('a'+index)),
				RequestID:      "reimbursement-status-race-request-" + string(rune('a'+index)),
			})
			statusOutcomes <- statusOutcome{result: result, err: statusErr}
		}()
	}
	var statusWinner ports.ReimbursementMutationResult
	statusSuccesses, statusConflicts := 0, 0
	for range 2 {
		outcome := <-statusOutcomes
		if outcome.err == nil {
			statusSuccesses++
			statusWinner = outcome.result
		} else if hasRuleCode(outcome.err, "reimbursement_status_stale") {
			statusConflicts++
		} else {
			t.Fatalf("unexpected concurrent reimbursement status error = %v", outcome.err)
		}
	}
	if statusSuccesses != 1 || statusConflicts != 1 || statusWinner.Version != 6 {
		t.Fatalf("concurrent status outcomes = successes:%d conflicts:%d winner:%#v", statusSuccesses, statusConflicts, statusWinner)
	}
	finalReopen, err := reimbursementService.ChangeStatus(ctx, fixture.tenant, first.ReimbursementID, reimbursementapp.StatusInput{
		ExpectedStatus: statusWinner.Status, DesiredStatus: domain.ReimbursementStatusSubmitted,
		ExpectedVersion: 6, Reason: "恢复并发验证后的待处理状态",
		IdempotencyKey: "reimbursement-final-reopen", RequestID: "reimbursement-final-reopen-request",
	})
	if err != nil || finalReopen.Version != 7 {
		t.Fatalf("final reimbursement reopen = %#v, err = %v", finalReopen, err)
	}

	detail, err := reimbursementService.Get(ctx, fixture.tenant, first.ReimbursementID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.ReimbursementStatusSubmitted || detail.Version != 7 || len(detail.Items) != 2 ||
		len(detail.Findings) != 0 || len(detail.Decisions) != 7 || detail.SnapshotHash != preview.SnapshotHash ||
		detail.Decisions[len(detail.Decisions)-1].DesiredStatus != detail.Status {
		t.Fatalf("reimbursement detail = %#v", detail)
	}
	databaseDecisionTx, err := fixture.store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	databaseDecisionAt := fixture.now.Add(13 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := databaseDecisionTx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'reimbursement_status_changed', 'reimbursement', ?, ?, ?, ?)
	`,
		"00000000-0000-4000-8000-00000000aa01",
		fixture.tenant.TenantID,
		fixture.tenant.UserID,
		first.ReimbursementID,
		"reimbursement-database-decision-request",
		`{"action":"mark_reimbursed","previous_status":"submitted","desired_status":"reimbursed"}`,
		databaseDecisionAt,
	); err != nil {
		_ = databaseDecisionTx.Rollback()
		t.Fatal(err)
	}
	if _, err := databaseDecisionTx.ExecContext(ctx, `
		INSERT INTO reimbursement_status_decisions (
			id, tenant_id, reimbursement_id, actor_user_id, previous_status,
			desired_status, expected_version, result_version, action,
			idempotency_key, request_hash, reason, audit_event_id, created_at
		) VALUES (?, ?, ?, ?, 'submitted', 'reimbursed', 7, 8, 'mark_reimbursed', ?, ?, ?, ?, ?)
	`,
		"00000000-0000-4000-8000-00000000aa02",
		fixture.tenant.TenantID,
		first.ReimbursementID,
		fixture.tenant.UserID,
		"reimbursement-database-decision",
		strings.Repeat("f", 64),
		"验证数据库决定原子推进状态",
		"00000000-0000-4000-8000-00000000aa01",
		databaseDecisionAt,
	); err != nil {
		_ = databaseDecisionTx.Rollback()
		t.Fatal(err)
	}
	var databaseStatus domain.ReimbursementStatus
	var databaseVersion int
	if err := databaseDecisionTx.QueryRowContext(ctx, `
		SELECT status, version FROM reimbursements WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, first.ReimbursementID).Scan(&databaseStatus, &databaseVersion); err != nil {
		_ = databaseDecisionTx.Rollback()
		t.Fatal(err)
	}
	if databaseStatus != domain.ReimbursementStatusReimbursed || databaseVersion != 8 {
		_ = databaseDecisionTx.Rollback()
		t.Fatalf("database-applied reimbursement decision = %s/v%d", databaseStatus, databaseVersion)
	}
	if err := databaseDecisionTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	afterDatabaseRollback, err := reimbursementService.Get(ctx, fixture.tenant, first.ReimbursementID)
	if err != nil || afterDatabaseRollback.Status != domain.ReimbursementStatusSubmitted || afterDatabaseRollback.Version != 7 {
		t.Fatalf("database decision rollback = %#v, err = %v", afterDatabaseRollback, err)
	}
	page, err := reimbursementService.List(ctx, fixture.tenant, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("first reimbursement page = %#v", page)
	}
	nextPage, err := reimbursementService.List(ctx, fixture.tenant, page.NextCursor, 1)
	if err != nil || len(nextPage.Items) != 1 || nextPage.Items[0].ID == page.Items[0].ID {
		t.Fatalf("second reimbursement page = %#v, err = %v", nextPage, err)
	}

	viewer := fixture.tenant
	viewer.Role = domain.RoleViewer
	if _, err := reimbursementService.List(ctx, viewer, "", 50); err != nil {
		t.Fatalf("viewer list = %v", err)
	}
	if _, err := reimbursementService.Preview(ctx, viewer, trip.FactID, assignmentIDs); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer preview = %v", err)
	}
	reviewer := fixture.tenant
	reviewer.Role = domain.RoleReviewer
	if _, err := reimbursementService.Get(ctx, reviewer, first.ReimbursementID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("reviewer detail = %v", err)
	}
	finance := fixture.tenant
	finance.Role = domain.RoleFinance
	if _, err := reimbursementService.Preview(ctx, finance, trip.FactID, assignmentIDs); err != nil {
		t.Fatalf("finance preview = %v", err)
	}
	otherTenant := addTenantReviewFixture(t, fixture)
	if _, err := reimbursementService.Get(ctx, otherTenant.tenant, first.ReimbursementID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant reimbursement detail = %v", err)
	}
	if _, err := reimbursementService.Preview(ctx, otherTenant.tenant, trip.FactID, assignmentIDs); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant reimbursement preview = %v", err)
	}

	var reimbursements, items, findings, decisions, audits, activeLinks int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM reimbursements WHERE tenant_id = ?),
		       (SELECT count(*) FROM reimbursement_items WHERE tenant_id = ?),
		       (SELECT count(*) FROM reimbursement_policy_findings WHERE tenant_id = ?),
		       (SELECT count(*) FROM reimbursement_status_decisions WHERE tenant_id = ?),
		       (SELECT count(*) FROM audit_events WHERE tenant_id = ? AND resource_type = 'reimbursement'),
		       (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND ended_at IS NULL)
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID,
		fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID,
	).Scan(&reimbursements, &items, &findings, &decisions, &audits, &activeLinks); err != nil {
		t.Fatal(err)
	}
	if reimbursements != 2 || items != 4 || findings != 2 || decisions != 9 || audits != 9 || activeLinks != 1 {
		t.Fatalf("reimbursement persistence = %d/%d/%d/%d/%d links:%d", reimbursements, items, findings, decisions, audits, activeLinks)
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE reimbursement_items SET amount_minor = amount_minor + 1 WHERE tenant_id = ? AND reimbursement_id = ?
	`, fixture.tenant.TenantID, first.ReimbursementID); err == nil {
		t.Fatal("immutable reimbursement item was updated")
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		DELETE FROM reimbursement_status_decisions WHERE tenant_id = ? AND reimbursement_id = ?
	`, fixture.tenant.TenantID, first.ReimbursementID); err == nil {
		t.Fatal("immutable reimbursement decision was deleted")
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE reimbursement_policy_findings SET code = 'missing_invoice' WHERE tenant_id = ? AND reimbursement_id = ?
	`, fixture.tenant.TenantID, second.ReimbursementID); err == nil {
		t.Fatal("immutable reimbursement finding was updated")
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		DELETE FROM reimbursements WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, first.ReimbursementID); err == nil {
		t.Fatal("reimbursement history was deleted")
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE reimbursements SET status = 'reimbursed', version = version + 1, updated_at = ?
		WHERE tenant_id = ? AND id = ?
	`, fixture.now.Add(9*time.Hour).UTC().Format(time.RFC3339Nano), fixture.tenant.TenantID, first.ReimbursementID); err == nil {
		t.Fatal("reimbursement status changed without a decision")
	}
	rows, err := fixture.store.DB().QueryContext(ctx, `
		SELECT safe_metadata_json FROM audit_events
		WHERE tenant_id = ? AND resource_type = 'reimbursement'
	`, fixture.tenant.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			t.Fatal(err)
		}
		var metadata map[string]any
		if err := json.Unmarshal([]byte(encoded), &metadata); err != nil {
			t.Fatalf("invalid reimbursement audit metadata = %q", encoded)
		}
		for _, forbiddenKey := range []string{"trip_id", "fact_id", "assignment_id", "destination", "date", "amount", "currency", "reason", "finding_key", "finding_code"} {
			for key := range metadata {
				if strings.Contains(key, forbiddenKey) {
					t.Fatalf("unsafe reimbursement audit metadata key %q in %s", key, encoded)
				}
			}
		}
		for _, forbiddenValue := range []string{trip.FactID, payment.FactID, invoice.FactID, "北京", "CNY", "12345", "合成报销"} {
			if strings.Contains(encoded, forbiddenValue) {
				t.Fatalf("unsafe reimbursement audit metadata value in %s", encoded)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if err := factService.Delete(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, "reimbursement-delete-payment"); err != nil {
		t.Fatal(err)
	}
	afterPaymentDelete, err := reimbursementService.Get(ctx, fixture.tenant, first.ReimbursementID)
	if err != nil {
		t.Fatal(err)
	}
	deletedSources := 0
	for _, item := range afterPaymentDelete.Items {
		if item.SourceDeleted {
			deletedSources++
		}
	}
	if deletedSources != 1 || afterPaymentDelete.SnapshotHash != preview.SnapshotHash {
		t.Fatalf("snapshot after Payment deletion = %#v", afterPaymentDelete)
	}
	if _, err := reimbursementService.Preview(ctx, fixture.tenant, trip.FactID, assignmentIDs); !hasRuleCode(err, "reimbursement_selection_stale") {
		t.Fatalf("deleted source preview = %v", err)
	}
	if err := factService.Delete(ctx, fixture.tenant, domain.DocumentTrip, trip.FactID, "reimbursement-delete-trip"); err != nil {
		t.Fatal(err)
	}
	afterTripDelete, err := reimbursementService.Get(ctx, fixture.tenant, first.ReimbursementID)
	if err != nil || !afterTripDelete.TripDeleted || afterTripDelete.Trip.Destination != "北京" {
		t.Fatalf("snapshot after Trip deletion = %#v, err = %v", afterTripDelete, err)
	}
}

func TestReimbursementPaymentBusinessDateCrossesSourceTimezoneBoundary(t *testing.T) {
	fixture := newFileReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(10 * time.Hour)})
	tripService := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(11 * time.Hour)})
	reimbursementService := reimbursementapp.NewService(
		fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(12 * time.Hour)},
	)

	paymentReview := seedAdditionalReview(
		t,
		fixture,
		paymentEnvelopeAt("跨时区换日合成商户", "2026-08-27T23:30:00Z"),
		"reimbursement-cross-date-payment",
	)
	payment, err := reviewService.Confirm(ctx, fixture.tenant, paymentReview.Job.ID, ConfirmInput{
		ExpectedRevision: paymentReview.Revision, AssociationMode: AssociationNoCandidate,
		IdempotencyKey: "reimbursement-cross-date-confirm", RequestID: "reimbursement-cross-date-confirm-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	var storedBusinessDate string
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT business_date FROM payments WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, payment.FactID).Scan(&storedBusinessDate); err != nil {
		t.Fatal(err)
	}
	if storedBusinessDate != "2026-08-28" {
		t.Fatalf("stored Payment business date = %q, want 2026-08-28", storedBusinessDate)
	}

	tripReview := seedAdditionalReview(
		t,
		fixture,
		tripEnvelope("上海", "合成跨日目的地", "2026-08-28", "2026-08-28"),
		"reimbursement-cross-date-trip",
	)
	trip, err := reviewService.Confirm(ctx, fixture.tenant, tripReview.Job.ID, ConfirmInput{
		ExpectedRevision: tripReview.Revision,
		IdempotencyKey:   "reimbursement-cross-date-trip-confirm", RequestID: "reimbursement-cross-date-trip-confirm-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	desiredTripID := trip.FactID
	assigned, err := tripService.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{
		FactType: domain.DocumentPayment, FactID: payment.FactID, DesiredTripID: &desiredTripID,
		Reason: "验证跨时区换日业务日期", IdempotencyKey: "reimbursement-cross-date-assign",
		RequestID: "reimbursement-cross-date-assign-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := reimbursementService.Preview(ctx, fixture.tenant, trip.FactID, []string{assigned.AssignmentID})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 1 || preview.Items[0].BusinessDate != "2026-08-28" ||
		len(preview.Findings) != 1 || preview.Findings[0].Code != domain.ReimbursementFindingMissingInvoice {
		t.Fatalf("cross-date reimbursement preview = %#v", preview)
	}
	created, err := reimbursementService.Submit(ctx, fixture.tenant, reimbursementapp.SubmissionInput{
		TripID: trip.FactID, AssignmentIDs: []string{assigned.AssignmentID},
		ExpectedSnapshotHash: preview.SnapshotHash, AcknowledgedFindingKeys: reimbursementFindingKeysForTest(preview),
		Reason: "确认跨时区业务日期快照", IdempotencyKey: "reimbursement-cross-date-submit",
		RequestID: "reimbursement-cross-date-submit-request",
	})
	if err != nil || created.Status != domain.ReimbursementStatusSubmitted {
		t.Fatalf("cross-date reimbursement submit = %#v, err = %v", created, err)
	}
}

func reimbursementFindingKeysForTest(snapshot domain.ReimbursementPolicySnapshot) []string {
	result := make([]string, 0, len(snapshot.Findings))
	for _, finding := range snapshot.Findings {
		result = append(result, finding.FindingKey)
	}
	return result
}
