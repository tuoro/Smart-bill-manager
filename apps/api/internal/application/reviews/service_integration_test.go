package reviews

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/sqlite"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/claimsupport"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestConfirmPaymentIsAtomicAndIdempotent(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	review, err := service.Get(context.Background(), fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != domain.ClaimReadyForReview || len(review.Fields) != 10 {
		t.Fatalf("initial review = %#v", review)
	}
	input := ConfirmInput{
		ExpectedRevision: review.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "confirm-payment-1",
		RequestID:        "request-confirm-1",
	}
	first, err := service.Confirm(context.Background(), fixture.tenant, fixture.jobID, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.FactType != domain.DocumentPayment || first.FactID == "" || first.Replayed {
		t.Fatalf("first confirmation = %#v", first)
	}
	second, err := service.Confirm(context.Background(), fixture.tenant, fixture.jobID, input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.FactID != first.FactID || second.ReviewDecisionID != first.ReviewDecisionID {
		t.Fatalf("replayed confirmation = %#v, first = %#v", second, first)
	}
	var payments, decisions, origins, audits int
	var status string
	if err := fixture.store.DB().QueryRowContext(context.Background(), `
		SELECT (SELECT count(*) FROM payments WHERE tenant_id = ?),
		       (SELECT count(*) FROM review_decisions WHERE tenant_id = ?),
		       (SELECT count(*) FROM fact_field_origins WHERE tenant_id = ?),
		       (SELECT count(*) FROM audit_events WHERE tenant_id = ?),
		       (SELECT status FROM processing_jobs WHERE tenant_id = ? AND id = ?)
	`,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.jobID,
	).Scan(&payments, &decisions, &origins, &audits, &status); err != nil {
		t.Fatal(err)
	}
	if payments != 1 || decisions != 1 || origins != 5 || audits != 1 || status != "completed" {
		t.Fatalf("counts/status = payments:%d decisions:%d origins:%d audits:%d status:%s", payments, decisions, origins, audits, status)
	}
	var traceable int
	if err := fixture.store.DB().QueryRowContext(context.Background(), `
		SELECT count(*)
		FROM fact_field_origins o
		JOIN review_decisions r ON r.tenant_id = o.tenant_id AND r.id = o.review_decision_id
		JOIN field_claims f ON f.tenant_id = o.tenant_id AND f.id = o.field_claim_id
		JOIN claim_sets c ON c.tenant_id = f.tenant_id AND c.id = f.claim_set_id
		JOIN ai_runs a ON a.tenant_id = c.tenant_id AND a.id = c.origin_ai_run_id
		JOIN documents d ON d.tenant_id = c.tenant_id AND d.id = c.document_id
		WHERE o.tenant_id = ? AND o.payment_id = ? AND r.action = 'confirm'
	`, fixture.tenant.TenantID, first.FactID).Scan(&traceable); err != nil {
		t.Fatal(err)
	}
	if traceable != origins {
		t.Fatalf("traceable fields = %d, origins = %d", traceable, origins)
	}
}

func TestFieldDuplicateRequiresCompleteResolutionAndReplaysSameDecision(t *testing.T) {
	fixture := newReviewFixture(t)
	clock := fixedClock{now: fixture.now.Add(3 * time.Hour)}
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, clock)
	ctx := context.Background()
	initial, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	firstFact, err := service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: initial.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "duplicate-first-confirm",
		RequestID:        "duplicate-first-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	second := seedAdditionalReview(t, fixture, paymentEnvelope(), "field-duplicate-payment")
	if len(second.DuplicateCandidates) != 1 {
		t.Fatalf("duplicate candidates = %#v", second.DuplicateCandidates)
	}
	candidate := second.DuplicateCandidates[0]
	if candidate.Kind != "field_combination" || candidate.ExistingPaymentID != firstFact.FactID ||
		!candidate.Available || candidate.DisplayName != "Example Merchant" {
		t.Fatalf("field duplicate candidate = %#v", candidate)
	}
	forged := ConfirmInput{
		ExpectedRevision: second.Revision,
		AssociationMode:  AssociationNoCandidate,
		DuplicateResolutions: []domain.DuplicateResolution{{
			CandidateID: mustID(t, system.IDGenerator{}),
			Action:      domain.DuplicateKeepDistinct,
		}},
		IdempotencyKey: "duplicate-forged-confirm",
		RequestID:      "duplicate-forged-request",
	}
	if _, err := service.Confirm(ctx, fixture.tenant, second.Job.ID, forged); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("forged duplicate resolution error = %v", err)
	}
	missing := ConfirmInput{
		ExpectedRevision: second.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "duplicate-second-confirm",
		RequestID:        "duplicate-second-request",
	}
	if _, err := service.Confirm(ctx, fixture.tenant, second.Job.ID, missing); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("missing duplicate resolution error = %v", err)
	}
	input := missing
	input.DuplicateResolutions = []domain.DuplicateResolution{{
		CandidateID: candidate.ID,
		Action:      domain.DuplicateKeepDistinct,
	}}
	confirmed, err := service.Confirm(ctx, fixture.tenant, second.Job.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.Confirm(ctx, fixture.tenant, second.Job.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.FactID != confirmed.FactID {
		t.Fatalf("duplicate resolution replay = %#v, first = %#v", replayed, confirmed)
	}
	var decisions int
	var planHash string
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM duplicate_candidate_decisions WHERE tenant_id = ? AND candidate_id = ?),
		       duplicate_plan_hash
		FROM review_decisions
		WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, candidate.ID, fixture.tenant.TenantID, confirmed.ReviewDecisionID).Scan(
		&decisions,
		&planHash,
	); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 || len(planHash) != 64 {
		t.Fatalf("duplicate decision count/hash = %d/%q", decisions, planHash)
	}
	input.DuplicateResolutions = nil
	if _, err := service.Confirm(ctx, fixture.tenant, second.Job.ID, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed replay plan error = %v", err)
	}
}

func TestDeletedDuplicateTargetMakesCandidateSetStaleWithoutPartialConfirmation(t *testing.T) {
	fixture := newReviewFixture(t)
	clock := fixedClock{now: fixture.now.Add(3 * time.Hour)}
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, clock)
	facts := NewFactService(fixture.store, fixture.store, system.IDGenerator{}, clock)
	ctx := context.Background()
	initial, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	firstFact, err := service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: initial.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "stale-first-confirm",
		RequestID:        "stale-first-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	second := seedAdditionalReview(t, fixture, paymentEnvelope(), "stale-duplicate-payment")
	if len(second.DuplicateCandidates) != 1 {
		t.Fatalf("duplicate candidates = %#v", second.DuplicateCandidates)
	}
	if err := facts.Delete(
		ctx,
		fixture.tenant,
		domain.DocumentPayment,
		firstFact.FactID,
		"stale-delete-request",
	); err != nil {
		t.Fatal(err)
	}
	input := ConfirmInput{
		ExpectedRevision: second.Revision,
		AssociationMode:  AssociationNoCandidate,
		DuplicateResolutions: []domain.DuplicateResolution{{
			CandidateID: second.DuplicateCandidates[0].ID,
			Action:      domain.DuplicateKeepDistinct,
		}},
		IdempotencyKey: "stale-second-confirm",
		RequestID:      "stale-second-request",
	}
	_, err = service.Confirm(ctx, fixture.tenant, second.Job.ID, input)
	var ruleError *domain.RuleError
	if !errors.As(err, &ruleError) || ruleError.Code != "duplicate_candidate_set_stale" ||
		!errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale duplicate error = %v", err)
	}
	var reviewDecisions, duplicateDecisions, activePayments int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM review_decisions WHERE tenant_id = ?),
		       (SELECT count(*) FROM duplicate_candidate_decisions WHERE tenant_id = ?),
		       (SELECT count(*) FROM payments WHERE tenant_id = ? AND deleted_at IS NULL)
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID).Scan(
		&reviewDecisions,
		&duplicateDecisions,
		&activePayments,
	); err != nil {
		t.Fatal(err)
	}
	if reviewDecisions != 1 || duplicateDecisions != 0 || activePayments != 0 {
		t.Fatalf("failed stale confirmation left rows = reviews:%d duplicates:%d active payments:%d", reviewDecisions, duplicateDecisions, activePayments)
	}
	refreshed, err := service.Get(ctx, fixture.tenant, second.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.DuplicateCandidates[0].Available {
		t.Fatalf("deleted duplicate target remained available: %#v", refreshed.DuplicateCandidates[0])
	}
	revised, err := service.Revise(ctx, fixture.tenant, second.Job.ID, revisionInputFrom(refreshed))
	if err != nil {
		t.Fatal(err)
	}
	if len(revised.DuplicateCandidates) != 0 || revised.Revision != second.Revision+1 {
		t.Fatalf("regenerated duplicate candidates = %#v", revised.DuplicateCandidates)
	}
	if _, err := service.Confirm(ctx, fixture.tenant, second.Job.ID, ConfirmInput{
		ExpectedRevision: revised.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "stale-revised-confirm",
		RequestID:        "stale-revised-request",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentDuplicateClaimsAllowOnlyOneConfirmation(t *testing.T) {
	fixture := newFileReviewFixture(t)
	service := NewService(
		fixture.store,
		fixture.store,
		system.IDGenerator{},
		fixedClock{now: fixture.now.Add(3 * time.Hour)},
	)
	ctx := context.Background()
	first, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	second := seedAdditionalReview(t, fixture, paymentEnvelope(), "concurrent-duplicate-payment")
	if len(first.DuplicateCandidates) != 0 || len(second.DuplicateCandidates) != 0 {
		t.Fatalf("pre-confirm duplicate candidates = first:%#v second:%#v", first.DuplicateCandidates, second.DuplicateCandidates)
	}

	type outcome struct {
		result ports.ConfirmResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	confirm := func(review ports.ReviewSnapshot, key string) {
		result, confirmErr := service.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{
			ExpectedRevision: review.Revision,
			AssociationMode:  AssociationNoCandidate,
			IdempotencyKey:   key,
			RequestID:        key + "-request",
		})
		outcomes <- outcome{result: result, err: confirmErr}
	}
	go confirm(first, "concurrent-duplicate-first")
	go confirm(second, "concurrent-duplicate-second")

	succeeded, stale := 0, 0
	for range 2 {
		entry := <-outcomes
		if entry.err == nil {
			succeeded++
			if entry.result.FactID == "" {
				t.Fatal("successful concurrent confirmation returned no Fact")
			}
			continue
		}
		var ruleError *domain.RuleError
		if errors.As(entry.err, &ruleError) && ruleError.Code == "duplicate_candidate_set_stale" &&
			errors.Is(entry.err, domain.ErrConflict) {
			stale++
			continue
		}
		t.Fatalf("unexpected concurrent confirmation error: %v", entry.err)
	}
	if succeeded != 1 || stale != 1 {
		t.Fatalf("concurrent outcomes = succeeded:%d stale:%d", succeeded, stale)
	}

	var facts, reviews, duplicateDecisions, audits, completedJobs int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM payments WHERE tenant_id = ? AND deleted_at IS NULL),
		       (SELECT count(*) FROM review_decisions WHERE tenant_id = ?),
		       (SELECT count(*) FROM duplicate_candidate_decisions WHERE tenant_id = ?),
		       (SELECT count(*) FROM audit_events WHERE tenant_id = ? AND action = 'fact_confirmed'),
		       (SELECT count(*) FROM processing_jobs WHERE tenant_id = ? AND status = 'completed')
	`,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
	).Scan(&facts, &reviews, &duplicateDecisions, &audits, &completedJobs); err != nil {
		t.Fatal(err)
	}
	if facts != 1 || reviews != 1 || duplicateDecisions != 0 || audits != 1 || completedJobs != 1 {
		t.Fatalf(
			"concurrent writes = facts:%d reviews:%d duplicate decisions:%d audits:%d completed jobs:%d",
			facts,
			reviews,
			duplicateDecisions,
			audits,
			completedJobs,
		)
	}
}

func TestNearFileCandidatePersistsWithoutCreatingCrossPageNoise(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(
		fixture.store,
		fixture.store,
		system.IDGenerator{},
		fixedClock{now: fixture.now.Add(3 * time.Hour)},
	)
	review := seedAdditionalReviewWithFingerprint(
		t,
		fixture,
		paymentEnvelopeWithAmount(54321),
		"near-file-payment",
		testVisualFingerprint("fixture-page"),
	)
	if len(review.DuplicateCandidates) != 1 {
		t.Fatalf("near-file duplicate candidates = %#v", review.DuplicateCandidates)
	}
	candidate := review.DuplicateCandidates[0]
	if candidate.Kind != "near_file" || candidate.ExistingDocumentID != fixture.documentID ||
		candidate.CurrentPageNumber != nil || candidate.ExistingPageNumber != nil ||
		candidate.DHashDistance == nil || *candidate.DHashDistance != 0 || !candidate.Available {
		t.Fatalf("near-file candidate = %#v", candidate)
	}
	if _, err := fixture.store.DB().ExecContext(context.Background(), `
		UPDATE claim_sets SET status = 'confirmed'
		WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, review.ClaimSetID); err == nil {
		t.Fatal("database allowed Claim confirmation without duplicate candidate decisions")
	}
	if _, err := service.Confirm(context.Background(), fixture.tenant, review.Job.ID, ConfirmInput{
		ExpectedRevision: review.Revision,
		AssociationMode:  AssociationNoCandidate,
		DuplicateResolutions: []domain.DuplicateResolution{{
			CandidateID: candidate.ID,
			Action:      domain.DuplicateKeepDistinct,
		}},
		IdempotencyKey: "near-file-confirm",
		RequestID:      "near-file-request",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateCandidateDatabaseRequiresOneImmutableConfirmDecision(t *testing.T) {
	fixture := newReviewFixture(t)
	fingerprint := testVisualFingerprint("fixture-page")
	_ = seedAdditionalReviewWithFingerprint(
		t,
		fixture,
		paymentEnvelopeWithAmount(22345),
		"database-duplicate-target",
		fingerprint,
	)
	review := seedAdditionalReviewWithFingerprint(
		t,
		fixture,
		paymentEnvelopeWithAmount(32345),
		"database-duplicate-current",
		fingerprint,
	)
	if len(review.DuplicateCandidates) != 2 {
		t.Fatalf("database invariant candidates = %#v", review.DuplicateCandidates)
	}
	ctx := context.Background()
	ids := system.IDGenerator{}
	firstReviewID := mustID(t, ids)
	secondReviewID := mustID(t, ids)
	for index, reviewID := range []string{firstReviewID, secondReviewID} {
		if _, err := fixture.store.DB().ExecContext(ctx, `
			INSERT INTO review_decisions (
				id, tenant_id, claim_set_id, actor_user_id, action, association_mode,
				duplicate_plan_hash, idempotency_key, expected_revision, created_at
			) VALUES (?, ?, ?, ?, 'confirm', 'no_candidate', ?, ?, ?, ?)
		`,
			reviewID,
			fixture.tenant.TenantID,
			review.ClaimSetID,
			fixture.tenant.UserID,
			strings.Repeat(string(rune('a'+index)), 64),
			"database-duplicate-review-"+strconv.Itoa(index),
			review.Revision,
			fixture.now.UTC().Format(time.RFC3339Nano),
		); err != nil {
			t.Fatal(err)
		}
	}
	decisionID := mustID(t, ids)
	if _, err := fixture.store.DB().ExecContext(ctx, `
		INSERT INTO duplicate_candidate_decisions (
			id, tenant_id, candidate_id, review_decision_id, action, created_at
		) VALUES (?, ?, ?, ?, 'keep_distinct', ?)
	`,
		decisionID,
		fixture.tenant.TenantID,
		review.DuplicateCandidates[0].ID,
		firstReviewID,
		fixture.now.UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		INSERT INTO duplicate_candidate_decisions (
			id, tenant_id, candidate_id, review_decision_id, action, created_at
		) VALUES (?, ?, ?, ?, 'keep_distinct', ?)
	`,
		mustID(t, ids),
		fixture.tenant.TenantID,
		review.DuplicateCandidates[1].ID,
		secondReviewID,
		fixture.now.UTC().Format(time.RFC3339Nano),
	); err == nil {
		t.Fatal("database allowed one Claim's duplicate decisions to use multiple confirm decisions")
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE duplicate_candidate_decisions SET review_decision_id = ?
		WHERE tenant_id = ? AND id = ?
	`, secondReviewID, fixture.tenant.TenantID, decisionID); err == nil {
		t.Fatal("database allowed a duplicate candidate decision to change")
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		DELETE FROM duplicate_candidate_decisions WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, decisionID); err == nil {
		t.Fatal("database allowed a duplicate candidate decision to be deleted")
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE claim_sets SET status = 'confirmed'
		WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, review.ClaimSetID); err == nil {
		t.Fatal("database confirmed a Claim with an incomplete duplicate decision set")
	}
}

func TestRejectDuplicateCandidateCreatesNoFactOrResolution(t *testing.T) {
	fixture := newReviewFixture(t)
	review := seedAdditionalReviewWithFingerprint(
		t,
		fixture,
		paymentEnvelopeWithAmount(54321),
		"reject-duplicate-payment",
		testVisualFingerprint("fixture-page"),
	)
	if len(review.DuplicateCandidates) != 1 {
		t.Fatalf("reject duplicate candidates = %#v", review.DuplicateCandidates)
	}
	service := NewService(
		fixture.store,
		fixture.store,
		system.IDGenerator{},
		fixedClock{now: fixture.now.Add(3 * time.Hour)},
	)
	if err := service.Reject(context.Background(), fixture.tenant, review.Job.ID, RejectInput{
		ExpectedRevision: review.Revision,
		Reason:           "synthetic duplicate",
		IdempotencyKey:   "reject-duplicate-review",
		RequestID:        "reject-duplicate-request",
	}); err != nil {
		t.Fatal(err)
	}
	var facts, duplicateDecisions, rejectedClaims, rejectedJobs int
	if err := fixture.store.DB().QueryRowContext(context.Background(), `
		SELECT (SELECT count(*) FROM payments WHERE tenant_id = ?),
		       (SELECT count(*) FROM duplicate_candidate_decisions WHERE tenant_id = ?),
		       (SELECT count(*) FROM claim_sets WHERE tenant_id = ? AND id = ? AND status = 'rejected'),
		       (SELECT count(*) FROM processing_jobs WHERE tenant_id = ? AND id = ? AND status = 'rejected')
	`,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		review.ClaimSetID,
		fixture.tenant.TenantID,
		review.Job.ID,
	).Scan(&facts, &duplicateDecisions, &rejectedClaims, &rejectedJobs); err != nil {
		t.Fatal(err)
	}
	if facts != 0 || duplicateDecisions != 0 || rejectedClaims != 1 || rejectedJobs != 1 {
		t.Fatalf(
			"reject duplicate writes = facts:%d decisions:%d claims:%d jobs:%d",
			facts,
			duplicateDecisions,
			rejectedClaims,
			rejectedJobs,
		)
	}
}

func TestRevisionTransitionsPreserveCompleteSnapshotsAndEvidence(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	ctx := context.Background()
	initial, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	changed := revisionInputFrom(initial)
	for index := range changed.Fields {
		if changed.Fields[index].Path == "merchant" {
			changed.Fields[index].Value = json.RawMessage(`"New Merchant"`)
			changed.Fields[index].EvidenceIDs = []string{initial.Fields[fieldIndex(initial.Fields, "merchant")].Evidence[0].ID}
		}
	}
	revised, err := service.Revise(ctx, fixture.tenant, fixture.jobID, changed)
	if err != nil {
		t.Fatal(err)
	}
	if revised.Revision != 2 || revised.Status != domain.ClaimReadyForReview || revised.Job.Status != domain.JobNeedsReview {
		t.Fatalf("ready revision = %#v", revised)
	}
	for _, field := range revised.Fields {
		want := "ai"
		if field.Path == "merchant" {
			want = "user"
		}
		if field.Source != want {
			t.Fatalf("revision field %s source = %s, want %s", field.Path, field.Source, want)
		}
	}
	blockedInput := revisionInputFrom(revised)
	merchantIndex := revisionFieldIndex(blockedInput.Fields, "merchant")
	blockedInput.Fields[merchantIndex].Presence = "absent"
	blockedInput.Fields[merchantIndex].Value = nil
	blockedInput.Fields[merchantIndex].EvidenceIDs = nil
	blocked, err := service.Revise(ctx, fixture.tenant, fixture.jobID, blockedInput)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Revision != 3 || blocked.Status != domain.ClaimBlocked || blocked.Job.Status != domain.JobBlocked {
		t.Fatalf("blocked revision = %#v", blocked)
	}
	recoveredInput := revisionInputFrom(blocked)
	merchantIndex = revisionFieldIndex(recoveredInput.Fields, "merchant")
	amount := blocked.Fields[fieldIndex(blocked.Fields, "amount_minor")]
	recoveredInput.Fields[merchantIndex].Presence = "present"
	recoveredInput.Fields[merchantIndex].Value = json.RawMessage(`"Recovered Merchant"`)
	recoveredInput.Fields[merchantIndex].EvidenceIDs = []string{amount.Evidence[0].ID}
	recovered, err := service.Revise(ctx, fixture.tenant, fixture.jobID, recoveredInput)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Revision != 4 || recovered.Status != domain.ClaimReadyForReview || recovered.Job.Status != domain.JobNeedsReview {
		t.Fatalf("recovered revision = %#v", recovered)
	}
	var claimSets, currentClaims, copiedEvidence int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT count(*),
		       sum(CASE WHEN status IN ('ready_for_review', 'blocked') THEN 1 ELSE 0 END),
		       (SELECT count(*) FROM evidence WHERE tenant_id = ? AND copied_from_evidence_id IS NOT NULL)
		FROM claim_sets WHERE tenant_id = ? AND document_id = ?
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.documentID).Scan(
		&claimSets,
		&currentClaims,
		&copiedEvidence,
	); err != nil {
		t.Fatal(err)
	}
	if claimSets != 4 || currentClaims != 1 || copiedEvidence == 0 {
		t.Fatalf("revision persistence = claims:%d current:%d copied evidence:%d", claimSets, currentClaims, copiedEvidence)
	}
}

func TestUserCanCorrectPaymentClaimIntoInvoiceAndCreateOnlyInvoiceFact(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	ctx := context.Background()
	current, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	evidenceID := current.Fields[fieldIndex(current.Fields, "amount_minor")].Evidence[0].ID
	input := RevisionInput{
		ExpectedRevision:          current.Revision,
		ExpectedOptimisticVersion: current.OptimisticVersion,
		DocumentType:              domain.DocumentInvoice,
	}
	for _, field := range invoiceEnvelope("CORRECTED-INV-001").Fields {
		entry := RevisionFieldInput{
			Path: field.Path, ValueType: field.ValueType, Presence: field.Presence,
			Value: append(json.RawMessage(nil), field.Value...),
		}
		if field.Presence == "present" {
			entry.EvidenceIDs = []string{evidenceID}
		}
		input.Fields = append(input.Fields, entry)
	}
	revised, err := service.Revise(ctx, fixture.tenant, fixture.jobID, input)
	if err != nil {
		t.Fatal(err)
	}
	if revised.DocumentType != domain.DocumentInvoice || revised.Status != domain.ClaimReadyForReview || revised.Revision != 2 {
		t.Fatalf("corrected revision = %#v", revised)
	}
	for _, field := range revised.Fields {
		if field.Path == "currency" && field.Source != "ai" {
			t.Fatalf("unchanged currency source = %s", field.Source)
		}
		if field.Presence == "present" && field.Path != "currency" && field.Source != "user" {
			t.Fatalf("corrected field %s source = %s", field.Path, field.Source)
		}
	}
	result, err := service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: revised.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "confirm-type-correction",
		RequestID:        "confirm-type-correction-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FactType != domain.DocumentInvoice || result.FactID == "" {
		t.Fatalf("corrected confirmation = %#v", result)
	}
	var payments, invoices, userFields, authors int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM payments WHERE tenant_id = ?),
		       (SELECT count(*) FROM invoices WHERE tenant_id = ? AND invoice_number = 'CORRECTED-INV-001'),
		       (SELECT count(*) FROM field_claims WHERE tenant_id = ? AND claim_set_id = ? AND presence = 'present' AND source = 'user' AND source_user_id = ?),
		       (SELECT count(*) FROM claim_sets WHERE tenant_id = ? AND document_id = ? AND ((revision = 1 AND origin_ai_run_id IS NOT NULL AND revised_by_user_id IS NULL) OR (revision = 2 AND origin_ai_run_id IS NOT NULL AND revised_by_user_id = ?)))
	`,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID, revised.ClaimSetID, fixture.tenant.UserID,
		fixture.tenant.TenantID, fixture.documentID, fixture.tenant.UserID,
	).Scan(&payments, &invoices, &userFields, &authors); err != nil {
		t.Fatal(err)
	}
	if payments != 0 || invoices != 1 || userFields != 6 || authors != 2 {
		t.Fatalf("type correction persistence = payments:%d invoices:%d user fields:%d authors:%d", payments, invoices, userFields, authors)
	}
}

func TestCrossTenantCandidateInjectionIsRejectedWithoutPartialWrites(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(4 * time.Hour)})
	ctx := context.Background()
	foreign := addTenantReviewFixture(t, fixture)
	foreignInvoice := seedAdditionalReview(t, foreign, invoiceEnvelope("FOREIGN-INV-001"), "foreign-invoice")
	if _, err := service.Confirm(ctx, foreign.tenant, foreignInvoice.Job.ID, ConfirmInput{
		ExpectedRevision: foreignInvoice.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "foreign-invoice-confirm",
		RequestID:        "foreign-invoice-request",
	}); err != nil {
		t.Fatal(err)
	}
	foreignPayment := seedAdditionalReview(t, foreign, paymentEnvelope(), "foreign-payment")
	if len(foreignPayment.Candidates) != 1 {
		t.Fatalf("foreign candidate count = %d", len(foreignPayment.Candidates))
	}
	primary, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: primary.Revision,
		AssociationMode:  AssociationAllocateCandidates,
		Allocations: []domain.AllocationRequest{{
			CandidateID: foreignPayment.Candidates[0].ID, AllocatedMinor: 1,
		}},
		IdempotencyKey: "cross-tenant-candidate",
		RequestID:      "cross-tenant-request",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("cross-tenant candidate error = %v", err)
	}
	if _, err := service.Get(ctx, fixture.tenant, foreignPayment.Job.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign review visibility error = %v", err)
	}
	var primaryFacts, primaryDecisions, primaryAudits, foreignFacts, foreignCandidates int
	var primaryJobStatus string
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT
			(SELECT count(*) FROM payments WHERE tenant_id = ?) + (SELECT count(*) FROM invoices WHERE tenant_id = ?),
			(SELECT count(*) FROM review_decisions WHERE tenant_id = ?),
			(SELECT count(*) FROM audit_events WHERE tenant_id = ?),
			(SELECT count(*) FROM payments WHERE tenant_id = ?) + (SELECT count(*) FROM invoices WHERE tenant_id = ?),
			(SELECT count(*) FROM payment_invoice_link_candidates WHERE tenant_id = ?),
			(SELECT status FROM processing_jobs WHERE tenant_id = ? AND id = ?)
	`,
		fixture.tenant.TenantID, fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		foreign.tenant.TenantID, foreign.tenant.TenantID,
		foreign.tenant.TenantID,
		fixture.tenant.TenantID, fixture.jobID,
	).Scan(&primaryFacts, &primaryDecisions, &primaryAudits, &foreignFacts, &foreignCandidates, &primaryJobStatus); err != nil {
		t.Fatal(err)
	}
	if primaryFacts != 0 || primaryDecisions != 0 || primaryAudits != 0 || foreignFacts != 1 || foreignCandidates != 1 || primaryJobStatus != "needs_review" {
		t.Fatalf(
			"cross-tenant injection state = primary facts:%d decisions:%d audits:%d foreign facts:%d candidates:%d job:%s",
			primaryFacts, primaryDecisions, primaryAudits, foreignFacts, foreignCandidates, primaryJobStatus,
		)
	}
}

func TestConcurrentAllocationsRespectLastBalanceWithoutPartialWrites(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	ctx := context.Background()
	paymentReview, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: paymentReview.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "seed-payment",
		RequestID:        "seed-payment-request",
	}); err != nil {
		t.Fatal(err)
	}
	first := seedAdditionalReview(t, fixture, invoiceEnvelope("INV-001"), "invoice-one")
	second := seedAdditionalReview(t, fixture, invoiceEnvelope("INV-002"), "invoice-two")
	if len(first.Candidates) != 1 || len(second.Candidates) != 1 ||
		first.Candidates[0].TargetType != domain.DocumentPayment || !first.Candidates[0].NameExact {
		t.Fatalf("generated candidates = first:%#v second:%#v", first.Candidates, second.Candidates)
	}
	type outcome struct {
		result ports.ConfirmResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	for index, review := range []ports.ReviewSnapshot{first, second} {
		go func(index int, review ports.ReviewSnapshot) {
			suffix := string(rune('1' + index))
			result, err := service.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{
				ExpectedRevision: review.Revision,
				AssociationMode:  AssociationAllocateCandidates,
				Allocations: []domain.AllocationRequest{{
					CandidateID: review.Candidates[0].ID, AllocatedMinor: review.Candidates[0].RemainingMinor,
				}},
				IdempotencyKey: "allocate-candidate-" + suffix,
				RequestID:      "allocate-request-" + suffix,
			})
			outcomes <- outcome{result: result, err: err}
		}(index, review)
	}
	successes, failures := 0, 0
	var linkedResult ports.ConfirmResult
	for range 2 {
		item := <-outcomes
		if item.err == nil {
			successes++
			linkedResult = item.result
			if len(item.result.LinkIDs) != 1 || item.result.FactType != domain.DocumentInvoice {
				t.Fatalf("successful candidate result = %#v", item.result)
			}
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent outcomes = successes:%d failures:%d", successes, failures)
	}
	var invoices, links, acceptedDecisions int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM invoices WHERE tenant_id = ?),
		       (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND ended_at IS NULL),
		       (SELECT count(*) FROM payment_invoice_link_decisions WHERE tenant_id = ? AND action = 'accept')
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID).Scan(
		&invoices,
		&links,
		&acceptedDecisions,
	); err != nil {
		t.Fatal(err)
	}
	if invoices != 1 || links != 1 || acceptedDecisions != 1 {
		t.Fatalf("concurrent persistence = invoices:%d links:%d accepts:%d", invoices, links, acceptedDecisions)
	}
	facts := NewFactService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(6 * time.Hour)})
	if err := facts.Delete(ctx, fixture.tenant, domain.DocumentInvoice, linkedResult.FactID, "delete-linked-invoice"); err != nil {
		t.Fatal(err)
	}
	var deletedInvoices, activeLinks, endedLinks, deletionAudits int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM invoices WHERE tenant_id = ? AND deleted_at IS NOT NULL),
		       (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND ended_at IS NULL),
		       (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND ended_at IS NOT NULL AND ended_by_audit_event_id IS NOT NULL),
		       (SELECT count(*) FROM audit_events WHERE tenant_id = ? AND action = 'fact_deleted')
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID).Scan(
		&deletedInvoices, &activeLinks, &endedLinks, &deletionAudits,
	); err != nil {
		t.Fatal(err)
	}
	if deletedInvoices != 1 || activeLinks != 0 || endedLinks != 1 || deletionAudits != 1 {
		t.Fatalf("linked deletion = invoices:%d active:%d ended:%d audits:%d", deletedInvoices, activeLinks, endedLinks, deletionAudits)
	}
}

func TestPaymentAllocatesAcrossInvoicesAndReplaysCanonicalPlan(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(6 * time.Hour)})
	ctx := context.Background()

	for index, total := range []int64{6_000, 7_000} {
		review := seedAdditionalReview(
			t,
			fixture,
			invoiceEnvelopeWithTotal("ALLOC-INV-"+strconv.Itoa(index+1), total),
			"allocation-invoice-"+strconv.Itoa(index+1),
		)
		if len(review.Candidates) != 0 {
			t.Fatalf("invoice %d candidates = %#v", index, review.Candidates)
		}
		if _, err := service.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{
			ExpectedRevision: review.Revision,
			AssociationMode:  AssociationNoCandidate,
			IdempotencyKey:   "seed-allocation-invoice-" + strconv.Itoa(index+1),
			RequestID:        "seed-allocation-invoice-request-" + strconv.Itoa(index+1),
		}); err != nil {
			t.Fatal(err)
		}
	}

	paymentReview := seedAdditionalReview(t, fixture, paymentEnvelopeWithAmount(10_000), "payment-two-invoices")
	if len(paymentReview.Candidates) != 2 {
		t.Fatalf("payment candidates = %#v", paymentReview.Candidates)
	}
	allocations := make([]domain.AllocationRequest, 0, 2)
	for _, candidate := range paymentReview.Candidates {
		allocated := int64(4_000)
		if candidate.AmountMinor == 7_000 {
			allocated = 6_000
		}
		allocations = append(allocations, domain.AllocationRequest{
			CandidateID: candidate.ID, AllocatedMinor: allocated,
		})
	}
	input := ConfirmInput{
		ExpectedRevision: paymentReview.Revision,
		AssociationMode:  AssociationAllocateCandidates,
		Allocations:      allocations,
		IdempotencyKey:   "payment-two-invoices-plan",
		RequestID:        "payment-two-invoices-request",
	}
	result, err := service.Confirm(ctx, fixture.tenant, paymentReview.Job.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LinkIDs) != 2 || result.FactType != domain.DocumentPayment {
		t.Fatalf("allocation result = %#v", result)
	}
	reversed := input
	reversed.Allocations = []domain.AllocationRequest{allocations[1], allocations[0]}
	replay, err := service.Confirm(ctx, fixture.tenant, paymentReview.Job.ID, reversed)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.FactID != result.FactID || strings.Join(replay.LinkIDs, ",") != strings.Join(result.LinkIDs, ",") {
		t.Fatalf("canonical replay = %#v, first = %#v", replay, result)
	}
	changed := input
	changed.Allocations = append([]domain.AllocationRequest(nil), allocations...)
	changed.Allocations[0].AllocatedMinor--
	changed.Allocations[1].AllocatedMinor++
	if _, err := service.Confirm(ctx, fixture.tenant, paymentReview.Job.ID, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed idempotent plan error = %v", err)
	}

	payments, err := fixture.store.ListPayments(ctx, fixture.tenant.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(payments) != 1 || payments[0].AllocatedMinor != 10_000 || payments[0].RemainingMinor != 0 || payments[0].AllocationStatus != "allocated" {
		t.Fatalf("payment allocation projection = %#v", payments)
	}
	invoices, err := fixture.store.ListInvoices(ctx, fixture.tenant.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	balances := make(map[int64][3]any, len(invoices))
	for _, invoice := range invoices {
		balances[invoice.TotalMinor] = [3]any{invoice.AllocatedMinor, invoice.RemainingMinor, invoice.AllocationStatus}
	}
	if balances[6_000] != [3]any{int64(4_000), int64(2_000), "partial"} ||
		balances[7_000] != [3]any{int64(6_000), int64(1_000), "partial"} {
		t.Fatalf("invoice allocation projections = %#v", balances)
	}
	rows, err := fixture.store.DB().QueryContext(ctx, `
		SELECT allocated_minor, currency
		FROM payment_invoice_links
		WHERE tenant_id = ? AND ended_at IS NULL
		ORDER BY allocated_minor
	`, fixture.tenant.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var persisted []string
	for rows.Next() {
		var amount int64
		var currency string
		if err := rows.Scan(&amount, &currency); err != nil {
			t.Fatal(err)
		}
		persisted = append(persisted, strconv.FormatInt(amount, 10)+":"+currency)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(persisted, ",") != "4000:CNY,6000:CNY" {
		t.Fatalf("persisted allocations = %#v", persisted)
	}
}

func TestInvoiceReceivesMultiplePaymentsAndDeletionRestoresBalance(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(6 * time.Hour)})
	ctx := context.Background()

	invoiceReview := seedAdditionalReview(t, fixture, invoiceEnvelopeWithTotal("MANY-PAYMENTS-INV", 10_000), "many-payments-invoice")
	invoiceResult, err := service.Confirm(ctx, fixture.tenant, invoiceReview.Job.ID, ConfirmInput{
		ExpectedRevision: invoiceReview.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "seed-many-payments-invoice",
		RequestID:        "seed-many-payments-invoice-request",
	})
	if err != nil {
		t.Fatal(err)
	}

	var paymentResults []ports.ConfirmResult
	for index, amount := range []int64{4_000, 5_000} {
		review := seedAdditionalReview(
			t,
			fixture,
			paymentEnvelopeWithAmount(amount),
			"invoice-payment-"+strconv.Itoa(index+1),
		)
		if len(review.Candidates) != 1 {
			t.Fatalf("payment %d candidates = %#v", index, review.Candidates)
		}
		wantAllocated := int64(index) * 4_000
		if review.Candidates[0].AllocatedMinor != wantAllocated || review.Candidates[0].RemainingMinor != 10_000-wantAllocated {
			t.Fatalf("payment %d candidate balance = %#v", index, review.Candidates[0])
		}
		result, err := service.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{
			ExpectedRevision: review.Revision,
			AssociationMode:  AssociationAllocateCandidates,
			Allocations: []domain.AllocationRequest{{
				CandidateID: review.Candidates[0].ID, AllocatedMinor: amount,
			}},
			IdempotencyKey: "allocate-invoice-payment-" + strconv.Itoa(index+1),
			RequestID:      "allocate-invoice-payment-request-" + strconv.Itoa(index+1),
		})
		if err != nil {
			t.Fatal(err)
		}
		paymentResults = append(paymentResults, result)
	}

	overflowReview := seedAdditionalReview(t, fixture, paymentEnvelopeWithAmount(2_000), "invoice-overflow-payment")
	if len(overflowReview.Candidates) != 1 || overflowReview.Candidates[0].RemainingMinor != 1_000 {
		t.Fatalf("overflow candidate = %#v", overflowReview.Candidates)
	}
	if _, err := service.Confirm(ctx, fixture.tenant, overflowReview.Job.ID, ConfirmInput{
		ExpectedRevision: overflowReview.Revision,
		AssociationMode:  AssociationAllocateCandidates,
		Allocations: []domain.AllocationRequest{{
			CandidateID: overflowReview.Candidates[0].ID, AllocatedMinor: 1_001,
		}},
		IdempotencyKey: "reject-overflow-allocation",
		RequestID:      "reject-overflow-allocation-request",
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("overflow allocation error = %v", err)
	}
	var overflowFacts, overflowDecisions int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM payments WHERE tenant_id = ? AND source_review_decision_id IN (
		           SELECT id FROM review_decisions WHERE tenant_id = ? AND claim_set_id = ?
		       )),
		       (SELECT count(*) FROM review_decisions WHERE tenant_id = ? AND claim_set_id = ?)
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, overflowReview.ClaimSetID,
		fixture.tenant.TenantID, overflowReview.ClaimSetID).Scan(&overflowFacts, &overflowDecisions); err != nil {
		t.Fatal(err)
	}
	if overflowFacts != 0 || overflowDecisions != 0 {
		t.Fatalf("overflow partial writes = facts:%d decisions:%d", overflowFacts, overflowDecisions)
	}

	invoices, err := fixture.store.ListInvoices(ctx, fixture.tenant.TenantID)
	if err != nil || len(invoices) != 1 {
		t.Fatalf("invoices before deletion = %#v, error = %v", invoices, err)
	}
	if invoices[0].ID != invoiceResult.FactID || invoices[0].AllocatedMinor != 9_000 || invoices[0].RemainingMinor != 1_000 || invoices[0].AllocationStatus != "partial" {
		t.Fatalf("many-to-one invoice projection = %#v", invoices[0])
	}
	facts := NewFactService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(8 * time.Hour)})
	if err := facts.Delete(ctx, fixture.tenant, domain.DocumentPayment, paymentResults[0].FactID, "delete-first-allocated-payment"); err != nil {
		t.Fatal(err)
	}
	invoices, err = fixture.store.ListInvoices(ctx, fixture.tenant.TenantID)
	if err != nil || len(invoices) != 1 {
		t.Fatalf("invoices after deletion = %#v, error = %v", invoices, err)
	}
	if invoices[0].AllocatedMinor != 5_000 || invoices[0].RemainingMinor != 5_000 || invoices[0].AllocationStatus != "partial" {
		t.Fatalf("restored invoice balance = %#v", invoices[0])
	}
	var activeLinks, endedLinks int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT sum(CASE WHEN ended_at IS NULL THEN 1 ELSE 0 END),
		       sum(CASE WHEN ended_at IS NOT NULL THEN 1 ELSE 0 END)
		FROM payment_invoice_links
		WHERE tenant_id = ? AND invoice_id = ?
	`, fixture.tenant.TenantID, invoiceResult.FactID).Scan(&activeLinks, &endedLinks); err != nil {
		t.Fatal(err)
	}
	if activeLinks != 1 || endedLinks != 1 {
		t.Fatalf("link history after deletion = active:%d ended:%d", activeLinks, endedLinks)
	}
}

func TestPaymentInvoiceLinkRejectsDuplicateActivePairAndMutation(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(6 * time.Hour)})
	ctx := context.Background()

	invoiceReview := seedAdditionalReview(
		t,
		fixture,
		invoiceEnvelopeWithTotal("PAIR-IMMUTABLE-INV", 10_000),
		"pair-immutable-invoice",
	)
	invoiceResult, err := service.Confirm(ctx, fixture.tenant, invoiceReview.Job.ID, ConfirmInput{
		ExpectedRevision: invoiceReview.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "seed-pair-immutable-invoice",
		RequestID:        "seed-pair-immutable-invoice-request",
	})
	if err != nil {
		t.Fatal(err)
	}

	paymentReview := seedAdditionalReview(t, fixture, paymentEnvelopeWithAmount(10_000), "pair-immutable-payment")
	if len(paymentReview.Candidates) != 1 ||
		paymentReview.Candidates[0].TargetType != domain.DocumentInvoice ||
		paymentReview.Candidates[0].TargetID != invoiceResult.FactID {
		t.Fatalf("pair candidate = %#v", paymentReview.Candidates)
	}
	paymentResult, err := service.Confirm(ctx, fixture.tenant, paymentReview.Job.ID, ConfirmInput{
		ExpectedRevision: paymentReview.Revision,
		AssociationMode:  AssociationAllocateCandidates,
		Allocations: []domain.AllocationRequest{{
			CandidateID:    paymentReview.Candidates[0].ID,
			AllocatedMinor: 4_000,
		}},
		IdempotencyKey: "create-pair-immutable-link",
		RequestID:      "create-pair-immutable-link-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paymentResult.LinkIDs) != 1 {
		t.Fatalf("pair link result = %#v", paymentResult)
	}

	ids := system.IDGenerator{}
	duplicateCandidateID := mustID(t, ids)
	duplicateDecisionID := mustID(t, ids)
	duplicateLinkID := mustID(t, ids)
	createdAt := fixture.now.Add(7 * time.Hour).Format(time.RFC3339Nano)
	tx, err := fixture.store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payment_invoice_link_candidates (
			id, tenant_id, claim_set_id, existing_invoice_id, candidate_key,
			rule_version, reason_codes_json, name_exact, date_distance_days, created_at
		) VALUES (?, ?, ?, ?, ?, 'payment-invoice-link/2', '["currency_exact","date_within_30_days","remaining_available","partial_allocation"]', 1, 0, ?)
	`, duplicateCandidateID, fixture.tenant.TenantID, paymentReview.ClaimSetID, invoiceResult.FactID,
		"duplicate-pair-candidate", createdAt); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payment_invoice_link_decisions (
			id, tenant_id, candidate_id, review_decision_id, action,
			allocated_minor, currency, created_at
		) VALUES (?, ?, ?, ?, 'accept', 1000, 'CNY', ?)
	`, duplicateDecisionID, fixture.tenant.TenantID, duplicateCandidateID, paymentResult.ReviewDecisionID, createdAt); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	_, duplicateErr := tx.ExecContext(ctx, `
		INSERT INTO payment_invoice_links (
			id, tenant_id, payment_id, invoice_id, link_decision_id,
			allocated_minor, currency, created_at
		) VALUES (?, ?, ?, ?, ?, 1000, 'CNY', ?)
	`, duplicateLinkID, fixture.tenant.TenantID, paymentResult.FactID, invoiceResult.FactID,
		duplicateDecisionID, createdAt)
	if duplicateErr == nil || !strings.Contains(duplicateErr.Error(), "UNIQUE constraint failed") {
		_ = tx.Rollback()
		t.Fatalf("duplicate active pair error = %v", duplicateErr)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var duplicateCandidates, duplicateDecisions, duplicateLinks int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM payment_invoice_link_candidates WHERE tenant_id = ? AND id = ?),
		       (SELECT count(*) FROM payment_invoice_link_decisions WHERE tenant_id = ? AND id = ?),
		       (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND id = ?)
	`, fixture.tenant.TenantID, duplicateCandidateID,
		fixture.tenant.TenantID, duplicateDecisionID,
		fixture.tenant.TenantID, duplicateLinkID).Scan(
		&duplicateCandidates, &duplicateDecisions, &duplicateLinks,
	); err != nil {
		t.Fatal(err)
	}
	if duplicateCandidates != 0 || duplicateDecisions != 0 || duplicateLinks != 0 {
		t.Fatalf("duplicate pair partial writes = candidates:%d decisions:%d links:%d", duplicateCandidates, duplicateDecisions, duplicateLinks)
	}

	linkID := paymentResult.LinkIDs[0]
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE payment_invoice_links SET allocated_minor = allocated_minor + 1
		WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, linkID); err == nil || !strings.Contains(err.Error(), "payment_invoice_link_immutable") {
		t.Fatalf("immutable amount update error = %v", err)
	}
	var allocatedMinor int64
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT allocated_minor FROM payment_invoice_links WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, linkID).Scan(&allocatedMinor); err != nil {
		t.Fatal(err)
	}
	if allocatedMinor != 4_000 {
		t.Fatalf("allocation mutated to %d", allocatedMinor)
	}

	facts := NewFactService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(8 * time.Hour)})
	if err := facts.Delete(ctx, fixture.tenant, domain.DocumentPayment, paymentResult.FactID, "end-pair-immutable-link"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE payment_invoice_links SET ended_at = NULL, ended_by_audit_event_id = NULL
		WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, linkID); err == nil || !strings.Contains(err.Error(), "payment_invoice_link_end_once") {
		t.Fatalf("link reactivation error = %v", err)
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		DELETE FROM payment_invoice_links WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, linkID); err == nil || !strings.Contains(err.Error(), "payment_invoice_link_history_required") {
		t.Fatalf("link history deletion error = %v", err)
	}
	var active, ended int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT sum(CASE WHEN ended_at IS NULL THEN 1 ELSE 0 END),
		       sum(CASE WHEN ended_at IS NOT NULL AND ended_by_audit_event_id IS NOT NULL THEN 1 ELSE 0 END)
		FROM payment_invoice_links WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, linkID).Scan(&active, &ended); err != nil {
		t.Fatal(err)
	}
	if active != 0 || ended != 1 {
		t.Fatalf("immutable link history = active:%d ended:%d", active, ended)
	}
}

func TestConfirmInvoiceWithStableItemsRejectsAllCandidatesAtomically(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	ctx := context.Background()
	payment, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: payment.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "seed-payment-for-reject-all",
		RequestID:        "seed-payment-for-reject-all-request",
	}); err != nil {
		t.Fatal(err)
	}

	review := seedAdditionalReview(t, fixture, invoiceWithItemsEnvelope("INV-ITEMS-001"), "invoice-items")
	if len(review.Candidates) != 1 {
		t.Fatalf("candidate count = %d", len(review.Candidates))
	}
	result, err := service.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{
		ExpectedRevision: review.Revision,
		AssociationMode:  AssociationRejectAll,
		IdempotencyKey:   "reject-all-candidates",
		RequestID:        "reject-all-candidates-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FactType != domain.DocumentInvoice || result.FactID == "" || len(result.LinkIDs) != 0 {
		t.Fatalf("invoice confirmation = %#v", result)
	}
	var invoices, items, rejectedCandidates, links, audits int
	var jobStatus string
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM invoices WHERE tenant_id = ? AND id = ?),
		       (SELECT count(*) FROM invoice_items WHERE tenant_id = ? AND invoice_id = ?),
		       (SELECT count(*) FROM payment_invoice_link_decisions WHERE tenant_id = ? AND action = 'reject'),
		       (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ?),
		       (SELECT count(*) FROM audit_events WHERE tenant_id = ? AND resource_id = ?),
		       (SELECT status FROM processing_jobs WHERE tenant_id = ? AND id = ?)
	`,
		fixture.tenant.TenantID, result.FactID,
		fixture.tenant.TenantID, result.FactID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID, result.FactID,
		fixture.tenant.TenantID, review.Job.ID,
	).Scan(&invoices, &items, &rejectedCandidates, &links, &audits, &jobStatus); err != nil {
		t.Fatal(err)
	}
	if invoices != 1 || items != 2 || rejectedCandidates != 1 || links != 0 || audits != 1 || jobStatus != "completed" {
		t.Fatalf(
			"invoice transaction = invoices:%d items:%d rejects:%d links:%d audits:%d status:%s",
			invoices,
			items,
			rejectedCandidates,
			links,
			audits,
			jobStatus,
		)
	}
}

func TestRejectReviewIsIdempotentAndNeverCreatesFact(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	ctx := context.Background()
	review, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	input := RejectInput{
		ExpectedRevision: review.Revision,
		Reason:           "原始凭证与候选内容不一致",
		IdempotencyKey:   "reject-payment-claim",
		RequestID:        "reject-payment-request",
	}
	if err := service.Reject(ctx, fixture.tenant, fixture.jobID, input); err != nil {
		t.Fatal(err)
	}
	if err := service.Reject(ctx, fixture.tenant, fixture.jobID, input); err != nil {
		t.Fatalf("idempotent rejection failed: %v", err)
	}
	var decisions, payments, invoices, audits int
	var jobStatus, claimStatus string
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM review_decisions WHERE tenant_id = ? AND action = 'reject'),
		       (SELECT count(*) FROM payments WHERE tenant_id = ?),
		       (SELECT count(*) FROM invoices WHERE tenant_id = ?),
		       (SELECT count(*) FROM audit_events WHERE tenant_id = ?),
		       (SELECT status FROM processing_jobs WHERE tenant_id = ? AND id = ?),
		       (SELECT status FROM claim_sets WHERE tenant_id = ? AND id = ?)
	`,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID, fixture.jobID,
		fixture.tenant.TenantID, review.ClaimSetID,
	).Scan(&decisions, &payments, &invoices, &audits, &jobStatus, &claimStatus); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 || payments != 0 || invoices != 0 || audits != 1 || jobStatus != "rejected" || claimStatus != "rejected" {
		t.Fatalf(
			"rejection = decisions:%d payments:%d invoices:%d audits:%d job:%s claim:%s",
			decisions,
			payments,
			invoices,
			audits,
			jobStatus,
			claimStatus,
		)
	}
}

func TestInvoiceItemRevisionUsesStableKeysAndFactReadsOnlyCurrentSnapshot(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(3 * time.Hour)})
	ctx := context.Background()
	review := seedAdditionalReview(t, fixture, invoiceWithItemsEnvelope("INV-REVISION-001"), "invoice-revision")
	input := revisionInputFrom(review)
	keptKey := "00000000-0000-0000-0000-000000000001"
	removedKey := "00000000-0000-0000-0000-000000000002"
	newKey := "00000000-0000-0000-0000-000000000003"
	evidenceID := review.Fields[fieldIndex(review.Fields, "total_minor")].Evidence[0].ID
	filtered := make([]RevisionFieldInput, 0, len(input.Fields)+7)
	for _, field := range input.Fields {
		if strings.HasPrefix(field.Path, "items["+removedKey+"]") {
			continue
		}
		if field.Path == "items["+keptKey+"].name" {
			field.Value = json.RawMessage(`"服务费（修订）"`)
			field.EvidenceIDs = []string{evidenceID}
		}
		if field.Path == "items["+keptKey+"].sort_order" {
			field.Value = json.RawMessage(`1`)
			field.EvidenceIDs = []string{evidenceID}
		}
		filtered = append(filtered, field)
	}
	newPrefix := "items[" + newKey + "]."
	filtered = append(filtered,
		RevisionFieldInput{Path: newPrefix + "name", ValueType: "string", Presence: "present", Value: json.RawMessage(`"材料费（新增）"`), EvidenceIDs: []string{evidenceID}},
		RevisionFieldInput{Path: newPrefix + "quantity", ValueType: "decimal", Presence: "absent"},
		RevisionFieldInput{Path: newPrefix + "unit", ValueType: "string", Presence: "absent"},
		RevisionFieldInput{Path: newPrefix + "unit_price_minor", ValueType: "money_minor", Presence: "absent"},
		RevisionFieldInput{Path: newPrefix + "amount_minor", ValueType: "money_minor", Presence: "present", Value: json.RawMessage(`7345`), EvidenceIDs: []string{evidenceID}},
		RevisionFieldInput{Path: newPrefix + "tax_minor", ValueType: "money_minor", Presence: "absent"},
		RevisionFieldInput{Path: newPrefix + "sort_order", ValueType: "integer", Presence: "present", Value: json.RawMessage(`0`), EvidenceIDs: []string{evidenceID}},
	)
	input.Fields = filtered
	revised, err := service.Revise(ctx, fixture.tenant, review.Job.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if revised.Status != domain.ClaimReadyForReview || revised.Revision != 2 {
		t.Fatalf("revised invoice = %#v", revised)
	}
	var removedTombstones, removedLinked, newFields, newLinked, preservedAI, modifiedUser int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT sum(CASE WHEN field_path LIKE ? AND presence = 'absent' AND source = 'user' THEN 1 ELSE 0 END),
		       sum(CASE WHEN field_path LIKE ? AND supersedes_field_claim_id IS NOT NULL THEN 1 ELSE 0 END),
		       sum(CASE WHEN field_path LIKE ? THEN 1 ELSE 0 END),
		       sum(CASE WHEN field_path LIKE ? AND supersedes_field_claim_id IS NOT NULL THEN 1 ELSE 0 END),
		       sum(CASE WHEN field_path = ? AND source = 'ai' AND source_user_id IS NULL THEN 1 ELSE 0 END),
		       sum(CASE WHEN field_path IN (?, ?) AND source = 'user' AND source_user_id = ? THEN 1 ELSE 0 END)
		FROM field_claims WHERE tenant_id = ? AND claim_set_id = ?
	`,
		"items["+removedKey+"]%", "items["+removedKey+"]%",
		"items["+newKey+"]%", "items["+newKey+"]%",
		"items["+keptKey+"].amount_minor",
		"items["+keptKey+"].name", "items["+keptKey+"].sort_order", fixture.tenant.UserID,
		fixture.tenant.TenantID, revised.ClaimSetID,
	).Scan(&removedTombstones, &removedLinked, &newFields, &newLinked, &preservedAI, &modifiedUser); err != nil {
		t.Fatal(err)
	}
	if removedTombstones != 7 || removedLinked != 7 || newFields != 7 || newLinked != 0 || preservedAI != 1 || modifiedUser != 2 {
		t.Fatalf(
			"revision lineage = removed:%d/%d new:%d/%d preserved:%d modified:%d",
			removedTombstones, removedLinked, newFields, newLinked, preservedAI, modifiedUser,
		)
	}
	confirmed, err := service.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{
		ExpectedRevision: revised.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "confirm-revised-invoice",
		RequestID:        "confirm-revised-invoice-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemCount, removedFacts, newFacts int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT count(*),
		       sum(CASE WHEN item_key = ? THEN 1 ELSE 0 END),
		       sum(CASE WHEN item_key = ? AND sort_order = 0 THEN 1 ELSE 0 END)
		FROM invoice_items WHERE tenant_id = ? AND invoice_id = ?
	`, removedKey, newKey, fixture.tenant.TenantID, confirmed.FactID).Scan(&itemCount, &removedFacts, &newFacts); err != nil {
		t.Fatal(err)
	}
	if itemCount != 2 || removedFacts != 0 || newFacts != 1 {
		t.Fatalf("current-snapshot invoice items = count:%d removed:%d new:%d", itemCount, removedFacts, newFacts)
	}
}

func TestDeletedCandidateAbortsConfirmationWithoutPartialFact(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(4 * time.Hour)})
	ctx := context.Background()
	paymentReview, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment, err := service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: paymentReview.Revision,
		AssociationMode:  AssociationNoCandidate,
		IdempotencyKey:   "seed-payment-for-stale-candidate",
		RequestID:        "seed-payment-for-stale-candidate-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	invoiceReview := seedAdditionalReview(t, fixture, invoiceEnvelope("INV-STALE-001"), "invoice-stale")
	if len(invoiceReview.Candidates) != 1 {
		t.Fatalf("candidate count = %d", len(invoiceReview.Candidates))
	}
	deletedAt := fixture.now.Add(3 * time.Hour).Format(time.RFC3339Nano)
	auditID := mustID(t, system.IDGenerator{})
	if _, err := fixture.store.DB().ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'fact_deleted', 'payment', ?, 'delete-before-confirm', '{}', ?)
	`, auditID, fixture.tenant.TenantID, fixture.tenant.UserID, payment.FactID, deletedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE payments
		SET deleted_at = ?, deleted_by_user_id = ?, deletion_audit_event_id = ?
		WHERE tenant_id = ? AND id = ?
	`, deletedAt, fixture.tenant.UserID, auditID, fixture.tenant.TenantID, payment.FactID); err != nil {
		t.Fatal(err)
	}
	_, err = service.Confirm(ctx, fixture.tenant, invoiceReview.Job.ID, ConfirmInput{
		ExpectedRevision: invoiceReview.Revision,
		AssociationMode:  AssociationAllocateCandidates,
		Allocations: []domain.AllocationRequest{{
			CandidateID: invoiceReview.Candidates[0].ID, AllocatedMinor: invoiceReview.Candidates[0].RemainingMinor,
		}},
		IdempotencyKey: "allocate-deleted-candidate",
		RequestID:      "allocate-deleted-candidate-request",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("deleted candidate error = %v", err)
	}
	var invoices, confirmDecisions, candidateDecisions, links int
	var jobStatus string
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM invoices WHERE tenant_id = ?),
		       (SELECT count(*) FROM review_decisions WHERE tenant_id = ? AND claim_set_id = ?),
		       (SELECT count(*) FROM payment_invoice_link_decisions WHERE tenant_id = ?),
		       (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ?),
		       (SELECT status FROM processing_jobs WHERE tenant_id = ? AND id = ?)
	`,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID, invoiceReview.ClaimSetID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID, invoiceReview.Job.ID,
	).Scan(&invoices, &confirmDecisions, &candidateDecisions, &links, &jobStatus); err != nil {
		t.Fatal(err)
	}
	if invoices != 0 || confirmDecisions != 0 || candidateDecisions != 0 || links != 0 || jobStatus != "needs_review" {
		t.Fatalf(
			"stale candidate partial state = invoices:%d decisions:%d candidate-decisions:%d links:%d job:%s",
			invoices, confirmDecisions, candidateDecisions, links, jobStatus,
		)
	}
}

func TestFactConfirmationFaultInjectionRollsBackBeforeAndAfterFactInsert(t *testing.T) {
	fixture := newReviewFixture(t)
	service := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(5 * time.Hour)})
	ctx := context.Background()
	review, err := service.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	assertClean := func(label string) {
		t.Helper()
		var payments, decisions, origins, audits int
		var jobStatus, claimStatus string
		if err := fixture.store.DB().QueryRowContext(ctx, `
			SELECT (SELECT count(*) FROM payments WHERE tenant_id = ?),
			       (SELECT count(*) FROM review_decisions WHERE tenant_id = ?),
			       (SELECT count(*) FROM fact_field_origins WHERE tenant_id = ?),
			       (SELECT count(*) FROM audit_events WHERE tenant_id = ?),
			       (SELECT status FROM processing_jobs WHERE tenant_id = ? AND id = ?),
			       (SELECT status FROM claim_sets WHERE tenant_id = ? AND id = ?)
		`,
			fixture.tenant.TenantID,
			fixture.tenant.TenantID,
			fixture.tenant.TenantID,
			fixture.tenant.TenantID,
			fixture.tenant.TenantID, fixture.jobID,
			fixture.tenant.TenantID, review.ClaimSetID,
		).Scan(&payments, &decisions, &origins, &audits, &jobStatus, &claimStatus); err != nil {
			t.Fatal(err)
		}
		if payments != 0 || decisions != 0 || origins != 0 || audits != 0 || jobStatus != "needs_review" || claimStatus != "ready_for_review" {
			t.Fatalf(
				"%s left partial state = payments:%d decisions:%d origins:%d audits:%d job:%s claim:%s",
				label, payments, decisions, origins, audits, jobStatus, claimStatus,
			)
		}
	}

	if _, err := fixture.store.DB().ExecContext(ctx, `
		CREATE TEMP TRIGGER fail_before_payment_fact
		BEFORE INSERT ON payments
		BEGIN SELECT RAISE(ABORT, 'synthetic_before_fact_failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: review.Revision, AssociationMode: AssociationNoCandidate,
		IdempotencyKey: "fault-before-fact", RequestID: "fault-before-fact-request",
	}); err == nil {
		t.Fatal("pre-fact failure was ignored")
	}
	assertClean("pre-fact failure")
	if _, err := fixture.store.DB().ExecContext(ctx, "DROP TRIGGER fail_before_payment_fact"); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.store.DB().ExecContext(ctx, `
		CREATE TEMP TRIGGER fail_after_payment_fact
		BEFORE INSERT ON audit_events
		WHEN NEW.action = 'fact_confirmed'
		BEGIN SELECT RAISE(ABORT, 'synthetic_after_fact_failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: review.Revision, AssociationMode: AssociationNoCandidate,
		IdempotencyKey: "fault-after-fact", RequestID: "fault-after-fact-request",
	}); err == nil {
		t.Fatal("post-fact failure was ignored")
	}
	assertClean("post-fact failure")
	if _, err := fixture.store.DB().ExecContext(ctx, "DROP TRIGGER fail_after_payment_fact"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Confirm(ctx, fixture.tenant, fixture.jobID, ConfirmInput{
		ExpectedRevision: review.Revision, AssociationMode: AssociationNoCandidate,
		IdempotencyKey: "fact-after-recovery", RequestID: "fact-after-recovery-request",
	}); err != nil {
		t.Fatalf("confirmation after rollback failed: %v", err)
	}
}

type reviewFixture struct {
	store      *sqliteadapter.Store
	tenant     domain.TenantContext
	now        time.Time
	documentID string
	jobID      string
	providerID string
}

func newReviewFixture(t *testing.T) reviewFixture {
	return newReviewFixtureAt(t, ":memory:")
}

func newFileReviewFixture(t *testing.T) reviewFixture {
	return newReviewFixtureAt(t, filepath.Join(t.TempDir(), "reviews.sqlite"))
}

func newReviewFixtureAt(t *testing.T, databasePath string) reviewFixture {
	t.Helper()
	ctx := context.Background()
	store, err := sqliteadapter.Open(ctx, sqliteadapter.Config{
		DatabasePath:  databasePath,
		MigrationsDir: reviewMigrationsDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ids := system.IDGenerator{}
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	userID := mustID(t, ids)
	tenantID := mustID(t, ids)
	owner := ports.BootstrapOwner{
		UserID:          userID,
		TenantID:        tenantID,
		Email:           "owner@example.test",
		PasswordHash:    "test-only",
		DisplayName:     "Owner",
		TenantName:      "Tenant",
		DefaultCurrency: domain.CurrencyCNY,
		Timezone:        "Asia/Shanghai",
		CreatedAt:       now,
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	providerID := mustID(t, ids)
	documentID := mustID(t, ids)
	jobID := mustID(t, ids)
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		if err := transaction.InsertProviderConfig(ctx, ports.ProviderConfig{
			ID:                      providerID,
			TenantID:                tenantID,
			BaseURL:                 "https://provider.example/v1",
			EncryptedAPIKey:         []byte("test-only"),
			Model:                   "test-model",
			OutputMode:              ports.ProviderOutputModeJSONSchema,
			CapabilityStatus:        "passed",
			CapabilitySafeMessage:   "passed",
			CapabilitySchemaVersion: "bill-visible-text-provider/1",
			CapabilitySchemaSHA256:  strings.Repeat("c", 64),
			Version:                 1,
			SafeFingerprint:         "test-fingerprint",
			CreatedByUserID:         userID,
			CreatedAt:               now,
			UpdatedAt:               now,
		}); err != nil {
			return err
		}
		if err := transaction.InsertDocument(ctx, ports.Document{
			ID:              documentID,
			TenantID:        tenantID,
			StorageKey:      "tenants/" + tenantID + "/documents/fixture.png",
			OriginalName:    "fixture.png",
			DeclaredMIME:    "image/png",
			DetectedMIME:    "image/png",
			SizeBytes:       100,
			SHA256:          strings.Repeat("a", 64),
			PageCount:       1,
			Status:          "stored",
			CreatedByUserID: userID,
			CreatedAt:       now,
		}); err != nil {
			return err
		}
		return transaction.InsertProcessingJob(ctx, ports.ProcessingJob{
			ID: jobID, TenantID: tenantID, DocumentID: documentID,
			Kind: "document_process", Status: domain.JobQueued, CreatedAt: now, Version: 1,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LeaseNextJob(ctx, "test-worker", now, now.Add(165*time.Second)); err != nil {
		t.Fatal(err)
	}
	pageID := mustID(t, ids)
	aiRunID := mustID(t, ids)
	claimID := mustID(t, ids)
	validated := domain.ValidateClaim(paymentEnvelope(), 1)
	bundle := ports.ClaimBundle{ClaimSet: ports.ClaimSetRecord{
		ID: claimID, TenantID: tenantID, DocumentID: documentID, OriginAiRunID: aiRunID,
		DocumentType: domain.DocumentPayment, Status: validated.Status, CreatedAt: now,
	}}
	fieldIDs := make(map[string]string)
	for _, field := range validated.Fields {
		fieldID := mustID(t, ids)
		fieldIDs[field.Path] = fieldID
		bundle.Fields = append(bundle.Fields, ports.FieldClaimRecord{
			ID: fieldID, TenantID: tenantID, ClaimSetID: claimID, FieldPath: field.Path,
			ValueType: field.ValueType, Presence: field.Presence,
			TypedValueJSON: string(field.Value), NormalizedValue: string(field.NormalizedValue), CreatedAt: now,
		})
		for _, evidence := range field.Evidence {
			region := string(evidence.Region)
			bundle.Evidence = append(bundle.Evidence, ports.EvidenceRecord{
				ID: mustID(t, ids), TenantID: tenantID, FieldClaimID: fieldID, DocumentPageID: pageID,
				Quote: evidence.Quote, RegionJSON: region,
				EvidenceHash: claimsupport.EvidenceHash(pageID, evidence.Quote, region), CreatedAt: now,
			})
		}
	}
	for _, validation := range validated.Validations {
		record, err := claimsupport.NewValidationRecord(validation, tenantID, claimID, fieldIDs, ids, now)
		if err != nil {
			t.Fatal(err)
		}
		bundle.Validations = append(bundle.Validations, record)
	}
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		if err := transaction.InsertDocumentPages(ctx, []ports.DocumentPageRecord{{
			ID: pageID, TenantID: tenantID, DocumentID: documentID, PageNumber: 1,
			StorageKey: "tenants/" + tenantID + "/documents/fixture/page-1.png",
			Width:      100, Height: 100, SHA256: strings.Repeat("b", 64),
			VisualFingerprint: testVisualFingerprint("fixture-page"),
			ProcessingVersion: "document-normalize/1", CreatedAt: now,
		}}); err != nil {
			return err
		}
		if err := transaction.InsertAiRun(ctx, ports.AiRun{
			ID: aiRunID, TenantID: tenantID, JobID: jobID, ProviderConfigID: providerID,
			ProviderConfigVersion: 1, ProviderConfigFingerprint: "test-fingerprint", Model: "test-model",
			PromptVersion: "bill-visible-text-cn/1", ExtractionSchemaVersion: "bill-visible-text/1",
			ProviderSchemaVersion: "bill-visible-text-provider/1", ProviderSchemaSHA256: strings.Repeat("c", 64),
			ClaimSchemaVersion: "document-claim/2", ClaimMapperVersion: "claim-mapper/3",
			InputProcessingVersion: "document-normalize/1", RequestHash: "request-hash",
			Outcome: "running", StartedAt: now,
		}); err != nil {
			return err
		}
		if err := transaction.CompleteAiRun(ctx, ports.AiRunCompletion{
			TenantID: tenantID, AiRunID: aiRunID, Outcome: "succeeded", ResponseHash: "response-hash", FinishedAt: now,
		}); err != nil {
			return err
		}
		return transaction.PersistInitialClaim(ctx, jobID, bundle)
	}); err != nil {
		t.Fatal(err)
	}
	return reviewFixture{
		store:  store,
		tenant: domain.TenantContext{TenantID: tenantID, UserID: userID, Role: domain.RoleOwner},
		now:    now, documentID: documentID, jobID: jobID, providerID: providerID,
	}
}

func addTenantReviewFixture(t *testing.T, fixture reviewFixture) reviewFixture {
	t.Helper()
	ctx := context.Background()
	ids := system.IDGenerator{}
	tenantID := mustID(t, ids)
	providerID := mustID(t, ids)
	now := fixture.now.Add(time.Hour)
	if _, err := fixture.store.DB().ExecContext(ctx, `
		INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at)
		VALUES (?, 'Foreign Tenant', 'CNY', 'Asia/Shanghai', ?, ?)
	`, tenantID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES (?, ?, 'owner', 'active', ?, ?)
	`, tenantID, fixture.tenant.UserID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.InsertProviderConfig(ctx, ports.ProviderConfig{
			ID: providerID, TenantID: tenantID, BaseURL: "https://provider.example/v1",
			EncryptedAPIKey: []byte("foreign-test-only"), Model: "test-model",
			OutputMode:       ports.ProviderOutputModeJSONSchema,
			CapabilityStatus: "passed", CapabilitySafeMessage: "passed", Version: 1,
			CapabilitySchemaVersion: "bill-visible-text-provider/1", CapabilitySchemaSHA256: strings.Repeat("c", 64),
			SafeFingerprint: "foreign-test-fingerprint", CreatedByUserID: fixture.tenant.UserID,
			CreatedAt: now, UpdatedAt: now,
		})
	}); err != nil {
		t.Fatal(err)
	}
	return reviewFixture{
		store: fixture.store,
		tenant: domain.TenantContext{
			TenantID: tenantID, UserID: fixture.tenant.UserID, Role: domain.RoleOwner,
		},
		now: now, providerID: providerID,
	}
}

func seedAdditionalReview(
	t *testing.T,
	fixture reviewFixture,
	envelope domain.ClaimEnvelope,
	label string,
) ports.ReviewSnapshot {
	return seedAdditionalReviewWithFingerprint(
		t,
		fixture,
		envelope,
		label,
		testVisualFingerprint(label+"-page"),
	)
}

func seedAdditionalReviewWithFingerprint(
	t *testing.T,
	fixture reviewFixture,
	envelope domain.ClaimEnvelope,
	label string,
	fingerprint domain.PageVisualFingerprint,
) ports.ReviewSnapshot {
	t.Helper()
	ctx := context.Background()
	ids := system.IDGenerator{}
	documentID := mustID(t, ids)
	jobID := mustID(t, ids)
	now := fixture.now.Add(2 * time.Hour)
	if err := fixture.store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		if err := transaction.InsertDocument(ctx, ports.Document{
			ID: documentID, TenantID: fixture.tenant.TenantID,
			StorageKey:   "tenants/" + fixture.tenant.TenantID + "/documents/" + label + ".png",
			OriginalName: label + ".png", DeclaredMIME: "image/png", DetectedMIME: "image/png",
			SizeBytes: 100, SHA256: claimsupport.HashBytes([]byte(label)), PageCount: 1,
			Status: "stored", CreatedByUserID: fixture.tenant.UserID, CreatedAt: now,
		}); err != nil {
			return err
		}
		return transaction.InsertProcessingJob(ctx, ports.ProcessingJob{
			ID: jobID, TenantID: fixture.tenant.TenantID, DocumentID: documentID,
			Kind: "document_process", Status: domain.JobQueued, CreatedAt: now, Version: 1,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.LeaseNextJob(ctx, "test-worker-"+label, now, now.Add(165*time.Second)); err != nil {
		t.Fatal(err)
	}
	pageID := mustID(t, ids)
	aiRunID := mustID(t, ids)
	claimID := mustID(t, ids)
	validated := domain.ValidateClaim(envelope, 1)
	bundle := ports.ClaimBundle{ClaimSet: ports.ClaimSetRecord{
		ID: claimID, TenantID: fixture.tenant.TenantID, DocumentID: documentID, OriginAiRunID: aiRunID,
		DocumentType: validated.DocumentType, Status: validated.Status, CreatedAt: now,
	}}
	fieldIDs := make(map[string]string)
	for _, field := range validated.Fields {
		fieldID := mustID(t, ids)
		fieldIDs[field.Path] = fieldID
		bundle.Fields = append(bundle.Fields, ports.FieldClaimRecord{
			ID: fieldID, TenantID: fixture.tenant.TenantID, ClaimSetID: claimID,
			FieldPath: field.Path, ValueType: field.ValueType, Presence: field.Presence,
			TypedValueJSON: string(field.Value), NormalizedValue: string(field.NormalizedValue), CreatedAt: now,
		})
		for _, evidence := range field.Evidence {
			region := string(evidence.Region)
			bundle.Evidence = append(bundle.Evidence, ports.EvidenceRecord{
				ID: mustID(t, ids), TenantID: fixture.tenant.TenantID, FieldClaimID: fieldID,
				DocumentPageID: pageID, Quote: evidence.Quote, RegionJSON: region,
				EvidenceHash: claimsupport.EvidenceHash(pageID, evidence.Quote, region), CreatedAt: now,
			})
		}
	}
	for _, validation := range validated.Validations {
		record, err := claimsupport.NewValidationRecord(
			validation,
			fixture.tenant.TenantID,
			claimID,
			fieldIDs,
			ids,
			now,
		)
		if err != nil {
			t.Fatal(err)
		}
		bundle.Validations = append(bundle.Validations, record)
	}
	if err := fixture.store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		if err := transaction.InsertDocumentPages(ctx, []ports.DocumentPageRecord{{
			ID: pageID, TenantID: fixture.tenant.TenantID, DocumentID: documentID, PageNumber: 1,
			StorageKey: "tenants/" + fixture.tenant.TenantID + "/documents/" + label + "/page-1.png",
			Width:      100, Height: 100, SHA256: claimsupport.HashBytes([]byte(label + "-page")),
			VisualFingerprint: fingerprint,
			ProcessingVersion: "document-normalize/1", CreatedAt: now,
		}}); err != nil {
			return err
		}
		if err := transaction.InsertAiRun(ctx, ports.AiRun{
			ID: aiRunID, TenantID: fixture.tenant.TenantID, JobID: jobID,
			ProviderConfigID: fixture.providerID, ProviderConfigVersion: 1,
			ProviderConfigFingerprint: "test-fingerprint", Model: "test-model",
			PromptVersion: "bill-visible-text-cn/1", ExtractionSchemaVersion: "bill-visible-text/1",
			ProviderSchemaVersion: "bill-visible-text-provider/1", ProviderSchemaSHA256: strings.Repeat("c", 64),
			ClaimSchemaVersion: "document-claim/2", ClaimMapperVersion: "claim-mapper/3",
			InputProcessingVersion: "document-normalize/1", RequestHash: "request-" + label,
			Outcome: "running", StartedAt: now,
		}); err != nil {
			return err
		}
		if err := transaction.CompleteAiRun(ctx, ports.AiRunCompletion{
			TenantID: fixture.tenant.TenantID, AiRunID: aiRunID,
			Outcome: "succeeded", ResponseHash: "response-" + label, FinishedAt: now,
		}); err != nil {
			return err
		}
		if input, ok := claimsupport.LinkInputFromValidated(validated); ok {
			targets, err := transaction.ListEligibleLinkTargets(
				ctx,
				fixture.tenant.TenantID,
				input.DocumentType,
				input.Currency,
			)
			if err != nil {
				return err
			}
			bundle.Candidates, err = claimsupport.BuildLinkCandidates(
				input,
				targets,
				fixture.tenant.TenantID,
				claimID,
				ids,
				now,
			)
			if err != nil {
				return err
			}
		}
		var limitExceeded bool
		var err error
		bundle.DuplicateCandidates, limitExceeded, err = claimsupport.BuildDuplicateCandidates(
			ctx,
			transaction,
			validated,
			fixture.tenant.TenantID,
			documentID,
			claimID,
			ids,
			now,
		)
		if err != nil {
			return err
		}
		if limitExceeded {
			validation, err := claimsupport.NewDuplicateCandidateLimitValidation(
				fixture.tenant.TenantID,
				claimID,
				ids,
				now,
			)
			if err != nil {
				return err
			}
			bundle.Validations = append(bundle.Validations, validation)
			bundle.ClaimSet.Status = domain.ClaimBlocked
		}
		return transaction.PersistInitialClaim(ctx, jobID, bundle)
	}); err != nil {
		t.Fatal(err)
	}
	review, err := fixture.store.GetReview(ctx, fixture.tenant.TenantID, jobID)
	if err != nil {
		t.Fatal(err)
	}
	return review
}

func paymentEnvelope() domain.ClaimEnvelope {
	evidence := func(quote string) []domain.CandidateEvidence {
		return []domain.CandidateEvidence{{Page: 1, Quote: quote}}
	}
	return domain.ClaimEnvelope{
		SchemaVersion: "document-claim/2",
		DocumentType:  "payment",
		Fields: []domain.FieldCandidate{
			{Path: "amount_minor", ValueType: "money_minor", Presence: "present", Value: json.RawMessage(`12345`), Evidence: evidence("123.45"), Issues: []string{}},
			{Path: "currency", ValueType: "string", Presence: "present", Value: json.RawMessage(`"CNY"`), Evidence: evidence("CNY"), Issues: []string{}},
			{Path: "merchant", ValueType: "string", Presence: "present", Value: json.RawMessage(`"Example Merchant"`), Evidence: evidence("Example Merchant"), Issues: []string{}},
			{Path: "transaction_time", ValueType: "instant", Presence: "present", Value: json.RawMessage(`"2026-08-27T12:00:00+08:00"`), Evidence: evidence("2026-08-27 12:00"), Issues: []string{}},
			{Path: "source_timezone", ValueType: "string", Presence: "present", Value: json.RawMessage(`"Asia/Shanghai"`), Evidence: evidence("北京时间"), Issues: []string{}},
			{Path: "payment_method", ValueType: "string", Presence: "absent", Issues: []string{}},
			{Path: "order_number", ValueType: "string", Presence: "absent", Issues: []string{}},
			{Path: "category", ValueType: "string", Presence: "absent", Issues: []string{}},
			{Path: "supplementary_fields", ValueType: "supplementary", Presence: "absent", Issues: []string{}},
		},
		DocumentIssues: []string{},
	}
}

func paymentEnvelopeWithAmount(amountMinor int64) domain.ClaimEnvelope {
	envelope := paymentEnvelope()
	for index := range envelope.Fields {
		if envelope.Fields[index].Path == "amount_minor" {
			envelope.Fields[index].Value = json.RawMessage(strconv.FormatInt(amountMinor, 10))
			break
		}
	}
	return envelope
}

func invoiceEnvelope(number string) domain.ClaimEnvelope {
	evidence := func(quote string) []domain.CandidateEvidence {
		return []domain.CandidateEvidence{{Page: 1, Quote: quote}}
	}
	return domain.ClaimEnvelope{
		SchemaVersion: "document-claim/2",
		DocumentType:  "invoice",
		Fields: []domain.FieldCandidate{
			{Path: "invoice_number", ValueType: "string", Presence: "present", Value: json.RawMessage(`"` + number + `"`), Evidence: evidence(number), Issues: []string{}},
			{Path: "invoice_date", ValueType: "date", Presence: "present", Value: json.RawMessage(`"2026-08-27"`), Evidence: evidence("2026-08-27"), Issues: []string{}},
			{Path: "total_minor", ValueType: "money_minor", Presence: "present", Value: json.RawMessage(`12345`), Evidence: evidence("123.45"), Issues: []string{}},
			{Path: "tax_minor", ValueType: "money_minor", Presence: "absent", Issues: []string{}},
			{Path: "currency", ValueType: "string", Presence: "present", Value: json.RawMessage(`"CNY"`), Evidence: evidence("CNY"), Issues: []string{}},
			{Path: "seller_name", ValueType: "string", Presence: "present", Value: json.RawMessage(`"Example Merchant"`), Evidence: evidence("Example Merchant"), Issues: []string{}},
			{Path: "buyer_name", ValueType: "string", Presence: "present", Value: json.RawMessage(`"Example Buyer"`), Evidence: evidence("Example Buyer"), Issues: []string{}},
			{Path: "supplementary_fields", ValueType: "supplementary", Presence: "absent", Issues: []string{}},
		},
		DocumentIssues: []string{},
	}
}

func invoiceEnvelopeWithTotal(number string, totalMinor int64) domain.ClaimEnvelope {
	envelope := invoiceEnvelope(number)
	for index := range envelope.Fields {
		if envelope.Fields[index].Path == "total_minor" {
			envelope.Fields[index].Value = json.RawMessage(strconv.FormatInt(totalMinor, 10))
			break
		}
	}
	return envelope
}

func invoiceWithItemsEnvelope(number string) domain.ClaimEnvelope {
	envelope := invoiceEnvelope(number)
	evidence := func(quote string) []domain.CandidateEvidence {
		return []domain.CandidateEvidence{{Page: 1, Quote: quote}}
	}
	items := []struct {
		key    string
		name   string
		amount string
		order  string
	}{
		{key: "00000000-0000-0000-0000-000000000001", name: "服务费", amount: "5000", order: "0"},
		{key: "00000000-0000-0000-0000-000000000002", name: "材料费", amount: "7345", order: "1"},
	}
	for _, item := range items {
		prefix := "items[" + item.key + "]."
		envelope.Fields = append(envelope.Fields,
			domain.FieldCandidate{Path: prefix + "name", ValueType: "string", Presence: "present", Value: json.RawMessage(`"` + item.name + `"`), Evidence: evidence(item.name), Issues: []string{}},
			domain.FieldCandidate{Path: prefix + "quantity", ValueType: "decimal", Presence: "present", Value: json.RawMessage(`"1"`), Evidence: evidence("1"), Issues: []string{}},
			domain.FieldCandidate{Path: prefix + "unit", ValueType: "string", Presence: "present", Value: json.RawMessage(`"项"`), Evidence: evidence("项"), Issues: []string{}},
			domain.FieldCandidate{Path: prefix + "unit_price_minor", ValueType: "money_minor", Presence: "present", Value: json.RawMessage(item.amount), Evidence: evidence(item.amount), Issues: []string{}},
			domain.FieldCandidate{Path: prefix + "amount_minor", ValueType: "money_minor", Presence: "present", Value: json.RawMessage(item.amount), Evidence: evidence(item.amount), Issues: []string{}},
			domain.FieldCandidate{Path: prefix + "tax_minor", ValueType: "money_minor", Presence: "absent", Issues: []string{}},
			domain.FieldCandidate{Path: prefix + "sort_order", ValueType: "integer", Presence: "present", Value: json.RawMessage(item.order), Evidence: evidence(item.order), Issues: []string{}},
		)
	}
	return envelope
}

func revisionInputFrom(snapshot ports.ReviewSnapshot) RevisionInput {
	input := RevisionInput{
		ExpectedRevision:          snapshot.Revision,
		ExpectedOptimisticVersion: snapshot.OptimisticVersion,
		DocumentType:              snapshot.DocumentType,
	}
	for _, field := range snapshot.Fields {
		if field.Path == "document_type" || (field.Presence == "absent" && len(field.Value) == 0 && field.Source == "user" && strings.HasPrefix(field.Path, "items[")) {
			continue
		}
		input.Fields = append(input.Fields, RevisionFieldInput{
			Path: field.Path, ValueType: field.ValueType, Presence: field.Presence,
			Value: append(json.RawMessage(nil), field.Value...),
		})
	}
	return input
}

func fieldIndex(fields []ports.ReviewField, path string) int {
	for index, field := range fields {
		if field.Path == path {
			return index
		}
	}
	return -1
}

func revisionFieldIndex(fields []RevisionFieldInput, path string) int {
	for index, field := range fields {
		if field.Path == path {
			return index
		}
	}
	return -1
}

func mustID(t *testing.T, ids system.IDGenerator) string {
	t.Helper()
	id, err := ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testVisualFingerprint(value string) domain.PageVisualFingerprint {
	digest := sha256.Sum256([]byte(value))
	return domain.NewPageVisualFingerprint(
		binary.BigEndian.Uint64(digest[:8]),
		binary.BigEndian.Uint64(digest[8:16]),
	)
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func reviewMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../infra/migrations"))
}
