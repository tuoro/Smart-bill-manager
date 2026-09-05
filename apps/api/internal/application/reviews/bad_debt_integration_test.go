package reviews

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	allocationapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func seedBadDebtPair(t *testing.T) (reviewFixture, string, string, ports.TripManagementResult) {
	t.Helper()
	f := newReviewFixture(t)
	trip := seedManualTrip(t, f, "bad-debt-trip", "合成坏账行程", "2026-08-26", "2026-08-28")
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	review, err := s.Get(context.Background(), f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	p := confirmFactWithoutLinks(t, s, f.tenant, review, "bad-debt-payment")
	i := confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, invoiceEnvelopeWithTotal("BAD-DEBT-SYN", 10000), "bad-debt-invoice"), "bad-debt-invoice-confirm")
	return f, p.FactID, i.FactID, trip
}

func setSyntheticBadDebt(t *testing.T, f reviewFixture, kind domain.DocumentType, id string, marked bool, key string) ports.BadDebtResult {
	t.Helper()
	s := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	r, err := s.SetBadDebt(context.Background(), f.tenant, kind, id, domain.BadDebtInput{Marked: marked, ExpectedVersion: assignmentVersion(t, f, kind, id), Reason: "合成异常状态理由"}, key, key+"-request")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestBadDebtLifecycleProtectsDirectAndIndirectTrips(t *testing.T) {
	f, payment, invoice, trip := seedBadDebtPair(t)
	ctx := context.Background()
	facts := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	trips := tripapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	assertLocked := func(id string, want bool) {
		t.Helper()
		list, err := trips.List(ctx, f.tenant)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range list {
			if item.ID == id {
				if item.BadDebtLocked != want {
					t.Fatalf("trip lock=%v want %v", item.BadDebtLocked, want)
				}
				return
			}
		}
		t.Fatal("trip missing")
	}
	remove := func(id string, version int, key string) error {
		_, err := trips.Manage(ctx, f.tenant, id, "delete", tripapp.ManagementInput{ExpectedVersion: version, Reason: "合成删除验证", IdempotencyKey: key, RequestID: key})
		return err
	}
	assertLocked(trip.TripID, false)
	beforeDetail, err := facts.Detail(ctx, f.tenant, domain.DocumentPayment, payment)
	if err != nil {
		t.Fatal(err)
	}
	expectedPayment := *beforeDetail.Payment
	expectedPayment.BadDebt = true
	marked := setSyntheticBadDebt(t, f, domain.DocumentPayment, payment, true, "bad-debt-mark-payment")
	detail, err := facts.Detail(ctx, f.tenant, domain.DocumentPayment, payment)
	if err != nil || !reflect.DeepEqual(expectedPayment, *detail.Payment) || detail.Version != marked.Version {
		t.Fatalf("bad debt changed financial identity: %v", err)
	}
	assertLocked(trip.TripID, true)
	var before int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM trip_management_decisions`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := remove(trip.TripID, trip.Version, "bad-debt-denied-delete"); !hasRuleCode(err, "trip_bad_debt_locked") {
		t.Fatalf("bad debt delete not blocked: %v", err)
	}
	var after int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM trip_management_decisions`).Scan(&after); err != nil || before != after {
		t.Fatal("blocked deletion persisted decision")
	}
	setSyntheticBadDebt(t, f, domain.DocumentPayment, payment, false, "bad-debt-clear-payment")
	assertLocked(trip.TripID, false)

	allocations := allocationapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	w, err := allocations.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, payment)
	if err != nil {
		t.Fatal(err)
	}
	_, err = allocations.Adjust(ctx, f.tenant, domain.DocumentPayment, payment, allocationapp.AdjustmentInput{ExpectedPlanHash: w.PlanHash, DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: invoice, AllocatedMinor: 1000}}, Reason: "合成间接关联", IdempotencyKey: "bad-debt-indirect-link", RequestID: "bad-debt-indirect-link"})
	if err != nil {
		t.Fatal(err)
	}
	setSyntheticBadDebt(t, f, domain.DocumentInvoice, invoice, true, "bad-debt-mark-invoice")
	assertLocked(trip.TripID, true)
	direct := seedManualTrip(t, f, "bad-debt-invoice-direct", "合成发票直属行程", "2026-10-01", "2026-10-02")
	_, err = trips.Assign(ctx, f.tenant, tripapp.AssignmentInput{FactType: domain.DocumentInvoice, FactID: invoice, DesiredTripID: &direct.TripID, ExpectedFactVersion: assignmentVersion(t, f, domain.DocumentInvoice, invoice), Reason: "合成发票归属", IdempotencyKey: "bad-debt-invoice-assign", RequestID: "bad-debt-invoice-assign"})
	if err != nil {
		t.Fatal(err)
	}
	assertLocked(direct.TripID, true)
	w, err = allocations.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, payment)
	if err != nil {
		t.Fatal(err)
	}
	_, err = allocations.Adjust(ctx, f.tenant, domain.DocumentPayment, payment, allocationapp.AdjustmentInput{ExpectedPlanHash: w.PlanHash, DesiredAllocations: []domain.DesiredAllocation{}, Reason: "显式解除分配", IdempotencyKey: "bad-debt-unlink", RequestID: "bad-debt-unlink"})
	if err != nil {
		t.Fatal(err)
	}
	assertLocked(trip.TripID, false)
	assertLocked(direct.TripID, true)
	if err := facts.Delete(ctx, f.tenant, domain.DocumentInvoice, invoice, "bad-debt-soft-delete"); err != nil {
		t.Fatal(err)
	}
	assertLocked(direct.TripID, false)
	if err := remove(direct.TripID, direct.Version, "bad-debt-delete-released"); err != nil {
		t.Fatal(err)
	}
	var history int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM fact_bad_debt_decisions`).Scan(&history); err != nil || history != 3 {
		t.Fatal("bad debt history lost")
	}
}

func TestBadDebtReplayVersionRoleTenantAndCorrection(t *testing.T) {
	f, payment, _, _ := seedBadDebtPair(t)
	ctx := context.Background()
	s := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	input := domain.BadDebtInput{Marked: true, ExpectedVersion: assignmentVersion(t, f, domain.DocumentPayment, payment), Reason: "合成标记"}
	result, err := s.SetBadDebt(ctx, f.tenant, domain.DocumentPayment, payment, input, "bad-debt-replay", "bad-debt-replay-request")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.SetBadDebt(ctx, f.tenant, domain.DocumentPayment, payment, input, "bad-debt-replay", "bad-debt-replay-request")
	if err != nil || !replay.Replayed || replay.DecisionID != result.DecisionID {
		t.Fatal("bad debt replay failed")
	}
	changed := input
	changed.Reason = "不同理由"
	if _, err := s.SetBadDebt(ctx, f.tenant, domain.DocumentPayment, payment, changed, "bad-debt-replay", "other-request"); !hasRuleCode(err, "idempotency_key_conflict") {
		t.Fatal("changed replay accepted")
	}
	input.Marked = false
	if _, err := s.SetBadDebt(ctx, f.tenant, domain.DocumentPayment, payment, input, "bad-debt-stale", "bad-debt-stale"); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatal("stale mark accepted")
	}
	for _, role := range []domain.Role{domain.RoleReviewer, domain.RoleViewer} {
		tenant := f.tenant
		tenant.Role = role
		if _, err := s.SetBadDebt(ctx, tenant, domain.DocumentPayment, payment, input, "bad-debt-forbidden", "bad-debt-forbidden"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("unauthorized bad debt mark")
		}
	}
	other := newReviewFixture(t)
	if _, err := s.SetBadDebt(ctx, other.tenant, domain.DocumentPayment, payment, input, "bad-debt-cross-tenant", "bad-debt-cross-tenant"); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-tenant mark accepted")
	}
	reviews := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	workspace, err := reviews.GetCorrection(ctx, f.tenant, domain.DocumentPayment, payment)
	if err != nil {
		t.Fatal(err)
	}
	correction := correctionInputFrom(workspace)
	correctionField(t, &correction, "merchant", "合成纠错仍保留坏账")
	applyCorrection(t, reviews, f.tenant, domain.DocumentPayment, payment, correction, "bad-debt-correction")
	detail, err := s.Detail(ctx, f.tenant, domain.DocumentPayment, payment)
	if err != nil || !detail.Payment.BadDebt {
		t.Fatal("correction cleared bad debt")
	}
	if _, err := f.store.DB().Exec(`UPDATE fact_bad_debt_decisions SET marked=false`); err == nil {
		t.Fatal("history mutation accepted")
	}
}

func TestBadDebtConcurrentMarkDeleteHasSerializableOutcome(t *testing.T) {
	for index := 0; index < 3; index++ {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			f, payment, _, trip := seedBadDebtPair(t)
			facts := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			trips := tripapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			version := assignmentVersion(t, f, domain.DocumentPayment, payment)
			start := make(chan struct{})
			marked := make(chan error, 1)
			deleted := make(chan error, 1)
			go func() {
				<-start
				_, err := facts.SetBadDebt(context.Background(), f.tenant, domain.DocumentPayment, payment, domain.BadDebtInput{Marked: true, ExpectedVersion: version, Reason: "合成并发标记"}, "bad-debt-race-mark", "bad-debt-race-mark")
				marked <- err
			}()
			go func() {
				<-start
				_, err := trips.Manage(context.Background(), f.tenant, trip.TripID, "delete", tripapp.ManagementInput{ExpectedVersion: trip.Version, Reason: "合成并发删除", IdempotencyKey: "bad-debt-race-delete", RequestID: "bad-debt-race-delete"})
				deleted <- err
			}()
			close(start)
			for _, err := range []error{<-marked, <-deleted} {
				if err != nil && !errors.Is(err, domain.ErrConflict) && !errors.Is(err, domain.ErrVersionConflict) {
					t.Fatalf("unexpected race: %v", err)
				}
			}
			var invalid bool
			if err := f.store.DB().QueryRow(`SELECT deleted_at IS NOT NULL AND sbm_trip_bad_debt_locked(tenant_id,id) FROM trips WHERE id=?`, trip.TripID).Scan(&invalid); err != nil || invalid {
				t.Fatal("deleted a currently protected trip")
			}
		})
	}
}

func TestBadDebtDecisionRequiresExactCommittedVersionStep(t *testing.T) {
	f, payment, _, _ := seedBadDebtPair(t)
	version := assignmentVersion(t, f, domain.DocumentPayment, payment)
	for _, mode := range []string{"missing-advance", "borrowed-version", "reverted-version"} {
		t.Run(mode, func(t *testing.T) {
			tx, err := f.store.DB().BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = tx.Exec(`INSERT INTO audit_events(id,tenant_id,actor_user_id,action,resource_type,resource_id,request_id,safe_metadata_json,created_at) VALUES ('version-step-audit',?,?,'fact_bad_debt_changed','payment',?,'version-step-request','{}',?)`, f.tenant.TenantID, f.tenant.UserID, payment, f.now)
			if err != nil {
				t.Fatal(err)
			}
			expected := version
			if mode == "borrowed-version" {
				expected--
			}
			_, err = tx.Exec(`INSERT INTO fact_bad_debt_decisions(id,tenant_id,payment_id,actor_user_id,marked,expected_version,resulting_version,reason,idempotency_key,request_hash,audit_event_id,created_at) VALUES ('version-step-decision',?,?,?,true,?,?, '合成版本完整性','version-step-key',?,'version-step-audit',?)`, f.tenant.TenantID, payment, f.tenant.UserID, expected, expected+1, strings.Repeat("a", 64), f.now)
			if mode == "borrowed-version" {
				if err == nil {
					t.Fatal("borrowed earlier version accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "reverted-version" {
				for _, next := range []int{version + 1, version} {
					if _, err := tx.Exec(`UPDATE payments SET version=? WHERE tenant_id=? AND id=?`, next, f.tenant.TenantID, payment); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := tx.Commit(); err == nil {
				t.Fatal("state changed without committed version advance")
			}
		})
	}
	var decisions int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM fact_bad_debt_decisions`).Scan(&decisions); err != nil || decisions != 0 || assignmentVersion(t, f, domain.DocumentPayment, payment) != version {
		t.Fatal("failed effect left state")
	}
}

func TestBadDebtConcurrentDifferentFactSameKeyConflicts(t *testing.T) {
	f, payment, invoice, _ := seedBadDebtPair(t)
	s := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, target := range []struct {
		kind domain.DocumentType
		id   string
	}{{domain.DocumentPayment, payment}, {domain.DocumentInvoice, invoice}} {
		version := assignmentVersion(t, f, target.kind, target.id)
		go func() {
			<-start
			_, err := s.SetBadDebt(context.Background(), f.tenant, target.kind, target.id, domain.BadDebtInput{Marked: true, ExpectedVersion: version, Reason: "合成同键并发"}, "bad-debt-shared-key", "bad-debt-shared-key")
			results <- err
		}()
	}
	close(start)
	success, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if hasRuleCode(err, "idempotency_key_conflict") {
			conflicts++
		} else {
			t.Fatalf("wrong same-key outcome: %v", err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("same key produced multiple effects")
	}
}
