package reviews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	allocationapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	reimbursementapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func correctionInputFrom(workspace CorrectionWorkspace) CorrectionInput {
	return CorrectionInput{ExpectedVersion: workspace.State.Version, CurrentReviewDecisionID: workspace.State.CurrentReviewDecisionID, Fields: revisionInputFrom(workspace.Review).Fields, Reason: "合成单据字段纠错", WithdrawLinkIDs: []string{}}
}

func TestFactCorrectionReconcilesAutomaticTripsButPreservesManualPreference(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	a := seedManualTrip(t, f, "correction-trip-a", "合成行程甲", "2026-08-26", "2026-08-28")
	b := seedManualTrip(t, f, "correction-trip-b", "合成行程乙", "2026-09-02", "2026-09-04")
	r, err := s.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	p := confirmFactWithoutLinks(t, s, f.tenant, r, "correction-attribution-first")
	w, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil || w.State.Attribution.CurrentTripID != a.TripID {
		t.Fatal("initial auto attribution missing")
	}
	input := correctionInputFrom(w)
	correctionField(t, &input, "transaction_time", "2026-09-03T12:00:00+08:00")
	preview, err := s.PreviewCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, input)
	if err != nil || preview.State.Attribution.DesiredTripID != b.TripID {
		t.Fatal("new automatic attribution not previewed")
	}
	result, _ := applyCorrection(t, s, f.tenant, domain.DocumentPayment, p.FactID, input, "correction-auto-move")
	w, err = s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil || w.State.Attribution.CurrentTripID != b.TripID || result.Version != input.ExpectedVersion+2 {
		t.Fatalf("automatic attribution/version mismatch: %v", err)
	}
	tripService := tripapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	currentAssignment := w.State.Attribution.AssignmentID
	if _, err := tripService.Assign(ctx, f.tenant, tripapp.AssignmentInput{FactType: domain.DocumentPayment, FactID: p.FactID, DesiredTripID: &a.TripID, ExpectedAssignmentID: &currentAssignment, ExpectedFactVersion: w.State.Version, Reason: "人工选择优先", IdempotencyKey: "correction-manual-preference", RequestID: "correction-manual-preference-request"}); err != nil {
		t.Fatal(err)
	}
	w, err = s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil {
		t.Fatal(err)
	}
	input = correctionInputFrom(w)
	correctionField(t, &input, "transaction_time", "2026-10-03T12:00:00+08:00")
	applyCorrection(t, s, f.tenant, domain.DocumentPayment, p.FactID, input, "correction-preserve-manual")
	w, err = s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil || w.State.Attribution.Mode != "manual" || w.State.Attribution.CurrentTripID != a.TripID {
		t.Fatal("correction replaced manual preference")
	}
}

func TestFactCorrectionPreservesEveryReimbursementStatusAndInvalidatesPreview(t *testing.T) {
	for _, status := range []domain.ReimbursementStatus{domain.ReimbursementStatusSubmitted, domain.ReimbursementStatusReimbursed, domain.ReimbursementStatusRejected} {
		t.Run(string(status), func(t *testing.T) {
			f := newReviewFixture(t)
			ctx := context.Background()
			s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			trip := seedManualTrip(t, f, "correction-reimbursement-trip", "合成报销行程", "2026-08-26", "2026-08-28")
			r, err := s.Get(ctx, f.tenant, f.jobID)
			if err != nil {
				t.Fatal(err)
			}
			p := confirmFactWithoutLinks(t, s, f.tenant, r, "correction-reimbursement-first")
			w, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
			if err != nil {
				t.Fatal(err)
			}
			assignments := []string{w.State.Attribution.AssignmentID}
			rs := reimbursementapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			pre, err := rs.Preview(ctx, f.tenant, trip.TripID, assignments)
			if err != nil {
				t.Fatal(err)
			}
			created, err := rs.Submit(ctx, f.tenant, reimbursementapp.SubmissionInput{TripID: trip.TripID, AssignmentIDs: assignments, ExpectedSnapshotHash: pre.SnapshotHash, AcknowledgedFindingKeys: reimbursementFindingKeysForTest(pre), Reason: "合成历史快照", IdempotencyKey: "correction-reimbursement-submit", RequestID: "correction-reimbursement-submit-request"})
			if err != nil {
				t.Fatal(err)
			}
			if status != domain.ReimbursementStatusSubmitted {
				if _, err := rs.ChangeStatus(ctx, f.tenant, created.ReimbursementID, reimbursementapp.StatusInput{ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: status, ExpectedVersion: 1, Reason: "合成状态", IdempotencyKey: "correction-reimbursement-status", RequestID: "correction-reimbursement-status-request"}); err != nil {
					t.Fatal(err)
				}
			}
			const snapshotSQL = `SELECT jsonb_build_object('snapshots',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM reimbursements r),'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM reimbursement_items i),'findings',(SELECT jsonb_agg(to_jsonb(f) ORDER BY id) FROM reimbursement_policy_findings f),'decisions',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM reimbursement_status_decisions d))::text`
			var before string
			if err := f.store.DB().QueryRow(snapshotSQL).Scan(&before); err != nil {
				t.Fatal(err)
			}
			pending, err := rs.Preview(ctx, f.tenant, trip.TripID, assignments)
			if err != nil {
				t.Fatal(err)
			}
			input := correctionInputFrom(w)
			correctionField(t, &input, "merchant", "合成更正后的商户")
			result, _ := applyCorrection(t, s, f.tenant, domain.DocumentPayment, p.FactID, input, "correction-reimbursement-fields")
			var after string
			if err := f.store.DB().QueryRow(snapshotSQL).Scan(&after); err != nil || before != after {
				t.Fatal("correction rewrote reimbursement history")
			}
			latest, err := rs.Preview(ctx, f.tenant, trip.TripID, assignments)
			if err != nil || latest.SnapshotHash == pending.SnapshotHash || latest.Items[0].FactReviewDecisionID != result.ReviewDecisionID {
				t.Fatal("new reimbursement does not bind current field revision")
			}
			if _, err := rs.Submit(ctx, f.tenant, reimbursementapp.SubmissionInput{TripID: trip.TripID, AssignmentIDs: assignments, ExpectedSnapshotHash: pending.SnapshotHash, AcknowledgedFindingKeys: reimbursementFindingKeysForTest(pending), Reason: "旧预览禁止提交", IdempotencyKey: "correction-reimbursement-stale", RequestID: "correction-reimbursement-stale-request"}); !hasRuleCode(err, "reimbursement_snapshot_stale") {
				t.Fatalf("old reimbursement preview accepted: %v", err)
			}
			if status != domain.ReimbursementStatusRejected && len(latest.Findings) == 0 {
				t.Fatal("stable Fact duplicate usage lost")
			}
		})
	}
}

func TestFactCorrectionLinkedBoundariesExplicitWithdrawalAndRollback(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	r, err := s.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment := confirmFactWithoutLinks(t, s, f.tenant, r, "correction-linked-payment")
	invoice := confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, invoiceEnvelope("CORRECTION-LINKED"), "correction-linked-invoice"), "correction-linked-invoice")
	a := allocationapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	w, err := a.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	link, err := a.Adjust(ctx, f.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{ExpectedPlanHash: w.PlanHash, DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: invoice.FactID, AllocatedMinor: 10000}}, Reason: "合成分配", IdempotencyKey: "correction-link-created", RequestID: "correction-link-request"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		path  string
		value any
		code  string
	}{{"amount_minor", 5000, "correction_overallocated"}, {"currency", "USD", "correction_currency_conflict"}} {
		input := correctionInputFrom(workspace)
		correctionField(t, &input, change.path, change.value)
		preview, err := s.PreviewCorrection(ctx, f.tenant, domain.DocumentPayment, payment.FactID, input)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, issue := range preview.Issues {
			if issue.Code == change.code {
				found = true
			}
		}
		if preview.CanConfirm || !found {
			t.Fatalf("missing linked conflict %s", change.code)
		}
	}
	input := correctionInputFrom(workspace)
	correctionField(t, &input, "merchant", "合成保持分配")
	correctionField(t, &input, "transaction_time", "2026-10-30T12:00:00+08:00")
	applyCorrection(t, s, f.tenant, domain.DocumentPayment, payment.FactID, input, "correction-retain-link")
	workspace, err = s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil || len(workspace.State.Links) != 1 || workspace.State.Links[0].ID != link.CreatedLinkIDs[0] {
		t.Fatal("valid link silently changed")
	}
	input = correctionInputFrom(workspace)
	correctionField(t, &input, "amount_minor", 5000)
	input.WithdrawLinkIDs = []string{link.CreatedLinkIDs[0]}
	preview, err := s.PreviewCorrection(ctx, f.tenant, domain.DocumentPayment, payment.FactID, input)
	if err != nil || !preview.CanConfirm {
		t.Fatalf("explicit withdrawal preview: %v", err)
	}
	var countsBefore string
	const countsSQL = `SELECT json_build_array((SELECT count(*) FROM claim_sets),(SELECT count(*) FROM review_decisions),(SELECT count(*) FROM fact_corrections),(SELECT count(*) FROM fact_field_origins),(SELECT count(*) FROM audit_events))::text`
	if err := f.store.DB().QueryRow(countsSQL).Scan(&countsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().Exec(`CREATE FUNCTION fail_correction_end() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic_correction_failure'; END; $$; CREATE TRIGGER fail_correction_end BEFORE INSERT ON fact_corrections FOR EACH ROW EXECUTE FUNCTION fail_correction_end()`); err != nil {
		t.Fatal(err)
	}
	request := CorrectionConfirmInput{CorrectionInput: input, PreviewHash: preview.PreviewHash, IdempotencyKey: "correction-rollback-then-success", RequestID: "correction-rollback-request"}
	for _, d := range preview.Duplicates {
		request.AcknowledgedDuplicateKeys = append(request.AcknowledgedDuplicateKeys, d.Key)
	}
	if _, err := s.ConfirmCorrection(ctx, f.tenant, domain.DocumentPayment, payment.FactID, request); err == nil {
		t.Fatal("injected final failure succeeded")
	}
	var countsAfter string
	if err := f.store.DB().QueryRow(countsSQL).Scan(&countsAfter); err != nil || countsBefore != countsAfter {
		t.Fatal("partial correction persisted")
	}
	rolledBack, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil || rolledBack.State.Version != workspace.State.Version || len(rolledBack.State.Links) != 1 {
		t.Fatal("failed correction changed fields or links")
	}
	if _, err := f.store.DB().Exec(`DROP TRIGGER fail_correction_end ON fact_corrections`); err != nil {
		t.Fatal(err)
	}
	result, err := s.ConfirmCorrection(ctx, f.tenant, domain.DocumentPayment, payment.FactID, request)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil || len(latest.State.Links) != 0 || latest.State.Version != result.Version {
		t.Fatal("explicit withdrawal did not apply")
	}
	var historyCount int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM payment_invoice_links WHERE id=? AND ended_at IS NOT NULL`, link.CreatedLinkIDs[0]).Scan(&historyCount); err != nil || historyCount != 1 {
		t.Fatal("withdrawal history missing")
	}
}

func TestFactCorrectionPermissionsStaleInputAndConcurrentReplay(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	r, err := s.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	p := confirmFactWithoutLinks(t, s, f.tenant, r, "correction-rbac-original")
	for _, role := range []domain.Role{domain.RoleReviewer, domain.RoleViewer} {
		tenant := f.tenant
		tenant.Role = role
		if _, err := s.GetCorrection(ctx, tenant, domain.DocumentPayment, p.FactID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("formal correction permission bypass")
		}
		if _, err := s.CorrectionHistory(ctx, tenant, domain.DocumentPayment, p.FactID, 0, 20); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("history permission bypass")
		}
		if _, err := s.PreviewCorrection(ctx, tenant, domain.DocumentPayment, p.FactID, CorrectionInput{}); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("preview permission bypass")
		}
		if _, err := s.ConfirmCorrection(ctx, tenant, domain.DocumentPayment, p.FactID, CorrectionConfirmInput{}); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("confirm permission bypass")
		}
	}
	other := f.tenant
	other.TenantID = "other-synthetic-workspace"
	if _, err := s.GetCorrection(ctx, other, domain.DocumentPayment, p.FactID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("tenant boundary bypass")
	}
	w, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil {
		t.Fatal(err)
	}
	input := correctionInputFrom(w)
	correctionField(t, &input, "merchant", "合成并发字段")
	preview, err := s.PreviewCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, input)
	if err != nil {
		t.Fatal(err)
	}
	request := CorrectionConfirmInput{CorrectionInput: input, PreviewHash: preview.PreviewHash, IdempotencyKey: "correction-concurrent-key", RequestID: "correction-concurrent-request"}
	var wg sync.WaitGroup
	results := make([]ports.FactCorrectionResult, 2)
	failures := make([]error, 2)
	for index := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[index], failures[index] = s.ConfirmCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, request)
		}()
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if results[0].ReviewDecisionID != results[1].ReviewDecisionID {
		t.Fatal("same key produced multiple corrections")
	}
	if _, err := s.PreviewCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, input); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatal("stale fields accepted")
	}
	if _, err := s.Confirm(ctx, f.tenant, r.Job.ID, ConfirmInput{ExpectedRevision: r.Revision, AssociationMode: AssociationNoCandidate, IdempotencyKey: request.IdempotencyKey, RequestID: "cross-kind-key"}); !hasRuleCode(err, "idempotency_key_conflict") {
		t.Fatalf("correction key used by confirm: %v", err)
	}
	request.IdempotencyKey = "correction-rbac-original"
	if _, err := s.ConfirmCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, request); !hasRuleCode(err, "idempotency_key_conflict") {
		t.Fatalf("confirm key used by correction: %v", err)
	}
}

func TestFactCorrectionDuplicateTargetRevisionAndInvoiceNumberConflicts(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	r, err := s.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	p := confirmFactWithoutLinks(t, s, f.tenant, r, "correction-duplicate-first")
	p2 := confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, paymentEnvelope(), "correction-duplicate-second"), "correction-duplicate-second")
	w, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil {
		t.Fatal(err)
	}
	input := correctionInputFrom(w)
	preview, err := s.PreviewCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, input)
	if err != nil || len(preview.Duplicates) == 0 {
		t.Fatal("duplicate preview absent")
	}
	request := CorrectionConfirmInput{CorrectionInput: input, PreviewHash: preview.PreviewHash, IdempotencyKey: "correction-duplicate-stale", RequestID: "correction-duplicate-stale-request"}
	if _, err := s.ConfirmCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, request); !hasRuleCode(err, "duplicate_confirmation_required") {
		t.Fatal("duplicate acknowledgment not required")
	}
	for _, d := range preview.Duplicates {
		request.AcknowledgedDuplicateKeys = append(request.AcknowledgedDuplicateKeys, d.Key)
	}
	w2, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p2.FactID)
	if err != nil {
		t.Fatal(err)
	}
	input2 := correctionInputFrom(w2)
	correctionField(t, &input2, "transaction_time", "2026-08-27T12:01:00+08:00")
	applyCorrection(t, s, f.tenant, domain.DocumentPayment, p2.FactID, input2, "correction-duplicate-target-change")
	if _, err := s.ConfirmCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, request); !hasRuleCode(err, "stale_correction_preview") {
		t.Fatalf("changed duplicate target accepted: %v", err)
	}
	i1 := confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, invoiceEnvelope("CORRECTION-NUMBER-A"), "correction-number-a"), "correction-number-a")
	confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, invoiceEnvelope("CORRECTION-NUMBER-B"), "correction-number-b"), "correction-number-b")
	iw, err := s.GetCorrection(ctx, f.tenant, domain.DocumentInvoice, i1.FactID)
	if err != nil {
		t.Fatal(err)
	}
	iinput := correctionInputFrom(iw)
	correctionField(t, &iinput, "invoice_number", "CORRECTION-NUMBER-B")
	blocked, err := s.PreviewCorrection(ctx, f.tenant, domain.DocumentInvoice, i1.FactID, iinput)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.CanConfirm {
		t.Fatal("exact invoice number conflict accepted")
	}
}

func correctionField(t *testing.T, input *CorrectionInput, path string, value any) {
	t.Helper()
	index := revisionFieldIndex(input.Fields, path)
	if index < 0 {
		t.Fatal("missing correction field")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	input.Fields[index].Value = encoded
	input.Fields[index].Presence = "present"
	input.Fields[index].EvidenceIDs = []string{}
	input.Fields[index].ManualEvidence = []domain.ManualEvidenceInput{{Page: 1, Quote: "明确核对原件后的合成摘录"}}
}

func applyCorrection(t *testing.T, service Service, tenant domain.TenantContext, kind domain.DocumentType, id string, input CorrectionInput, key string) (ports.FactCorrectionResult, CorrectionConfirmInput) {
	t.Helper()
	preview, err := service.PreviewCorrection(context.Background(), tenant, kind, id, input)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.CanConfirm {
		t.Fatalf("correction not confirmable: %#v", preview.Issues)
	}
	confirm := CorrectionConfirmInput{CorrectionInput: input, PreviewHash: preview.PreviewHash, AcknowledgedDuplicateKeys: []string{}, IdempotencyKey: key, RequestID: key + "-request"}
	for _, duplicate := range preview.Duplicates {
		confirm.AcknowledgedDuplicateKeys = append(confirm.AcknowledgedDuplicateKeys, duplicate.Key)
	}
	result, err := service.ConfirmCorrection(context.Background(), tenant, kind, id, confirm)
	if err != nil {
		t.Fatal(err)
	}
	return result, confirm
}

func TestFactCorrectionThreeTypesPreserveIdentitySourceAndHistory(t *testing.T) {
	for _, kind := range []domain.DocumentType{domain.DocumentPayment, domain.DocumentInvoice, domain.DocumentTrip} {
		t.Run(string(kind), func(t *testing.T) {
			f := newReviewFixture(t)
			ctx := context.Background()
			s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			review, err := s.Get(ctx, f.tenant, f.jobID)
			if err != nil {
				t.Fatal(err)
			}
			path := "merchant"
			if kind == domain.DocumentInvoice {
				review = seedAdditionalReview(t, f, invoiceWithItemsEnvelope("CORRECTION-INVOICE"), "correction-invoice")
				path = "seller_name"
			}
			if kind == domain.DocumentTrip {
				review = seedAdditionalReview(t, f, tripEnvelope("合成出发", "合成目的", "2026-08-26", "2026-08-28"), "correction-trip")
				path = "destination"
			}
			var original ports.ConfirmResult
			if kind == domain.DocumentTrip {
				original, err = s.Confirm(ctx, f.tenant, review.Job.ID, ConfirmInput{ExpectedRevision: review.Revision, IdempotencyKey: "correction-first-confirm", RequestID: "correction-first-request"})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				original = confirmFactWithoutLinks(t, s, f.tenant, review, "correction-first-confirm")
			}
			var beforeJob string
			if err := f.store.DB().QueryRow(`SELECT row_to_json(j)::text FROM processing_jobs j WHERE id = ?`, review.Job.ID).Scan(&beforeJob); err != nil {
				t.Fatal(err)
			}
			for index := 1; index <= 2; index++ {
				workspace, err := s.GetCorrection(ctx, f.tenant, kind, original.FactID)
				if err != nil {
					t.Fatal(err)
				}
				input := correctionInputFrom(workspace)
				correctionField(t, &input, path, fmt.Sprintf("合成修订%d", index))
				var beforeCount int
				if err := f.store.DB().QueryRow(`SELECT count(*) FROM claim_sets`).Scan(&beforeCount); err != nil {
					t.Fatal(err)
				}
				preview, err := s.PreviewCorrection(ctx, f.tenant, kind, original.FactID, input)
				if err != nil || !preview.CanConfirm {
					t.Fatalf("preview: %v %#v", err, preview.Issues)
				}
				previewAgain, err := s.PreviewCorrection(ctx, f.tenant, kind, original.FactID, input)
				if err != nil || previewAgain.PreviewHash != preview.PreviewHash {
					t.Fatal("preview identity is unstable")
				}
				var afterCount int
				if err := f.store.DB().QueryRow(`SELECT count(*) FROM claim_sets`).Scan(&afterCount); err != nil || beforeCount != afterCount {
					t.Fatal("preview wrote claims")
				}
				result, request := applyCorrection(t, s, f.tenant, kind, original.FactID, input, fmt.Sprintf("correction-apply-%d", index))
				if result.FactID != original.FactID || result.Version != workspace.State.Version+1 || result.Replayed {
					t.Fatal("correction changed stable fact identity")
				}
				replay, err := s.ConfirmCorrection(ctx, f.tenant, kind, original.FactID, request)
				if err != nil || !replay.Replayed || replay.ReviewDecisionID != result.ReviewDecisionID {
					t.Fatalf("correction replay: %v", err)
				}
				request.Reason = "不同请求"
				if _, err := s.ConfirmCorrection(ctx, f.tenant, kind, original.FactID, request); !errors.Is(err, domain.ErrConflict) {
					t.Fatal("changed replay accepted")
				}
				latest, err := s.GetCorrection(ctx, f.tenant, kind, original.FactID)
				if err != nil {
					t.Fatal(err)
				}
				if latest.Review.OriginAiRunID != review.OriginAiRunID || latest.Review.Status != domain.ClaimConfirmed {
					t.Fatal("correction lost genuine AI origin")
				}
				changed := latest.Review.Fields[fieldIndex(latest.Review.Fields, path)]
				if changed.Source != "user" || changed.SourceUserID != f.tenant.UserID || changed.Evidence[0].Quote != "明确核对原件后的合成摘录" {
					t.Fatal("manual correction provenance missing")
				}
			}
			history, err := s.CorrectionHistory(ctx, f.tenant, kind, original.FactID, 0, 2)
			if err != nil || len(history) != 2 || history[0].Revision != review.Revision+2 {
				t.Fatalf("correction history: %v", err)
			}
			older, err := s.CorrectionHistory(ctx, f.tenant, kind, original.FactID, history[1].Revision, 2)
			if err != nil || len(older) != 1 || older[0].ReviewDecisionID != original.ReviewDecisionID {
				t.Fatal("history keyset lost first confirmation")
			}
			old, err := s.GetClaimSet(ctx, f.tenant, review.ClaimSetID)
			if err != nil || old.Status != domain.ClaimConfirmed {
				t.Fatal("original confirmation overwritten")
			}
			var afterJob string
			if err := f.store.DB().QueryRow(`SELECT row_to_json(j)::text FROM processing_jobs j WHERE id = ?`, review.Job.ID).Scan(&afterJob); err != nil || beforeJob != afterJob {
				t.Fatal("correction changed completed job")
			}
			if kind == domain.DocumentInvoice {
				var versions, items int
				if err := f.store.DB().QueryRow(`SELECT count(DISTINCT review_decision_id), count(*) FROM invoice_items WHERE invoice_id = ?`, original.FactID).Scan(&versions, &items); err != nil || versions != 3 || items != 6 {
					t.Fatal("old invoice items lost")
				}
				detail, err := f.store.ReadFactDetail(ctx, f.tenant.TenantID, domain.DocumentInvoice, original.FactID, true)
				if err != nil || detail.Invoice == nil || len(detail.Invoice.Items) != 2 {
					t.Fatal("current invoice mixed historical items")
				}
			}
		})
	}
}
