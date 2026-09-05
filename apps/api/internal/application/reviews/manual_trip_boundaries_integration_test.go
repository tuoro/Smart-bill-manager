package reviews

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	reimbursementapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestTripDSTAndTimezoneOnlyEdit(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	for index, item := range []struct {
		date  string
		times []string
	}{
		{"2026-03-08", []string{"2026-03-08T04:59:59Z", "2026-03-08T05:00:00Z", "2026-03-09T03:59:59Z", "2026-03-09T04:00:00Z"}},
		{"2026-11-01", []string{"2026-11-01T03:59:59Z", "2026-11-01T04:00:00Z", "2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z", "2026-11-02T04:59:59Z", "2026-11-02T05:00:00Z"}},
	} {
		trip, err := service.Manage(ctx, fixture.tenant, "", "create", tripapp.ManagementInput{Details: domain.TripDetails{
			Name: "合成夏令时行程", StartDate: item.date, EndDate: item.date, Timezone: "America/New_York"},
			Reason: "验证当地完整自然日", IdempotencyKey: fmt.Sprintf("dst-create-%d", index), RequestID: "dst-request"})
		if err != nil {
			t.Fatal(err)
		}
		for position, timestamp := range item.times {
			payment := confirmTripPayment(t, fixture, fmt.Sprintf("dst-%d-%d", index, position), timestamp)
			want := trip.TripID
			if position == 0 || position == len(item.times)-1 {
				want = ""
			}
			assertPaymentTrip(t, fixture, payment, want, "auto")
		}
	}
	trip, err := service.Manage(ctx, fixture.tenant, "", "create", tripapp.ManagementInput{Details: domain.TripDetails{
		Name: "合成时区调整", StartDate: "2026-08-27", EndDate: "2026-08-27", Timezone: "America/New_York"},
		Reason: "新建", IdempotencyKey: "timezone-create", RequestID: "timezone-create-request"})
	if err != nil {
		t.Fatal(err)
	}
	oldPayment := confirmTripPayment(t, fixture, "timezone-old", "2026-08-27T20:00:00Z")
	newPayment := confirmTripPayment(t, fixture, "timezone-new", "2026-08-26T20:00:00Z")
	assertPaymentTrip(t, fixture, oldPayment, trip.TripID, "auto")
	assertPaymentTrip(t, fixture, newPayment, "", "auto")
	if _, err := service.Manage(ctx, fixture.tenant, trip.TripID, "edit", tripapp.ManagementInput{Details: domain.TripDetails{
		Name: "合成时区调整", StartDate: "2026-08-27", EndDate: "2026-08-27", Timezone: "Asia/Shanghai"}, ExpectedVersion: trip.Version,
		Reason: "仅改时区", IdempotencyKey: "timezone-edit", RequestID: "timezone-edit-request"}); err != nil {
		t.Fatal(err)
	}
	assertPaymentTrip(t, fixture, oldPayment, "", "auto")
	assertPaymentTrip(t, fixture, newPayment, trip.TripID, "auto")
}

func TestTripConcurrentRecalculationAndUnassignedVersion(t *testing.T) {
	fixture := newFileReviewFixture(t)
	ctx := context.Background()
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	review := seedAdditionalReview(t, fixture, paymentEnvelopeAt("合成并发支付", "2026-08-27T12:00:00+08:00"), "race-confirm")
	create := func(label string) (ports.TripManagementResult, error) {
		return service.Manage(ctx, fixture.tenant, "", "create", tripapp.ManagementInput{Details: domain.TripDetails{
			Name: "合成并发行程", StartDate: "2026-08-27", EndDate: "2026-08-27", Timezone: "Asia/Shanghai"},
			Reason: "并发创建", IdempotencyKey: label + "-create", RequestID: label + "-request"})
	}
	var first ports.TripManagementResult
	var confirmed ports.ConfirmResult
	runTripRace(t, func() error { var err error; first, err = create("race-first"); return err }, func() error {
		var err error
		confirmed, err = reviewService.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{ExpectedRevision: review.Revision,
			AssociationMode: AssociationNoCandidate, IdempotencyKey: "race-payment-confirm", RequestID: "race-payment-request"})
		return err
	})
	assertPaymentTrip(t, fixture, confirmed.FactID, first.TripID, "auto")
	deleteManualTrip(t, fixture, first, "race-first-delete")
	beforeVersion := assignmentVersion(t, fixture, domain.DocumentPayment, confirmed.FactID)
	var overlap [2]ports.TripManagementResult
	runTripRace(t, func() error { var err error; overlap[0], err = create("race-overlap-a"); return err },
		func() error { var err error; overlap[1], err = create("race-overlap-b"); return err })
	assertPaymentTrip(t, fixture, confirmed.FactID, "", "auto")
	if assignmentVersion(t, fixture, domain.DocumentPayment, confirmed.FactID) <= beforeVersion {
		t.Fatal("missing automatic version history")
	}
	if _, err := service.Assign(ctx, fixture.tenant, tripapp.AssignmentInput{FactType: domain.DocumentPayment, FactID: confirmed.FactID,
		DesiredTripID: &overlap[0].TripID, ExpectedFactVersion: beforeVersion, Reason: "陈旧未归属请求", IdempotencyKey: "race-stale-null", RequestID: "race-stale-request"}); !hasRuleCode(err, "trip_assignment_stale") {
		t.Fatalf("unassigned ABA accepted: %v", err)
	}
	manualTarget := seedManualTrip(t, fixture, "race-manual-target", "人工指定的其他行程", "2026-09-01", "2026-09-02")
	manualInput := tripapp.AssignmentInput{FactType: domain.DocumentPayment, FactID: confirmed.FactID, DesiredTripID: &manualTarget.TripID,
		ExpectedFactVersion: assignmentVersion(t, fixture, domain.DocumentPayment, confirmed.FactID), Reason: "人工优先", IdempotencyKey: "race-manual-priority", RequestID: "race-manual-priority-request"}
	var manualErr error
	runTripRace(t, func() error {
		_, manualErr = service.Assign(ctx, fixture.tenant, manualInput)
		if errors.Is(manualErr, domain.ErrConflict) {
			return nil
		}
		return manualErr
	},
		func() error {
			_, err := service.Manage(ctx, fixture.tenant, overlap[1].TripID, "delete", tripapp.ManagementInput{
				ExpectedVersion: overlap[1].Version, Reason: "并发删除重叠行程", IdempotencyKey: "race-delete-overlap", RequestID: "race-delete-overlap-request"})
			return err
		})
	if manualErr != nil {
		// 明确读取新版本后模拟用户重试，不把失败请求当作已接受。
		var link string
		if err := fixture.store.DB().QueryRowContext(ctx, `SELECT id FROM trip_fact_assignments WHERE tenant_id = ? AND payment_id = ? AND ended_at IS NULL`, fixture.tenant.TenantID, confirmed.FactID).Scan(&link); err != nil {
			t.Fatal(err)
		}
		manualInput.ExpectedAssignmentID = &link
		manualInput.ExpectedFactVersion = assignmentVersion(t, fixture, domain.DocumentPayment, confirmed.FactID)
		manualInput.IdempotencyKey = "race-manual-fresh"
		if _, err := service.Assign(ctx, fixture.tenant, manualInput); err != nil {
			t.Fatal(err)
		}
	}
	assertPaymentTrip(t, fixture, confirmed.FactID, manualTarget.TripID, "manual")
	if _, err := create("race-after-manual"); err != nil {
		t.Fatal(err)
	}
	assertPaymentTrip(t, fixture, confirmed.FactID, manualTarget.TripID, "manual")
}

func runTripRace(t *testing.T, first, second func() error) {
	t.Helper()
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	for _, action := range []func() error{first, second} {
		go func(action func() error) { <-start; outcomes <- action() }(action)
	}
	close(start)
	for range 2 {
		if err := <-outcomes; err != nil {
			t.Errorf("concurrent trip operation: %v", err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}

func TestTripManagementAndMaterialFailuresRollback(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	confirmTripPayment(t, fixture, "rollback-payment", "2026-08-27T12:00:00+08:00")
	before := tripTransactionalState(t, fixture)
	execOldSQL(t, fixture.store.DB(), `CREATE FUNCTION synthetic_fail_trip_link() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic_trip_link_failure'; END; $$;
		CREATE TRIGGER synthetic_fail_trip_link BEFORE INSERT ON trip_fact_assignments FOR EACH ROW EXECUTE FUNCTION synthetic_fail_trip_link()`)
	_, err := service.Manage(ctx, fixture.tenant, "", "create", tripapp.ManagementInput{Details: domain.TripDetails{
		Name: "合成失败行程", StartDate: "2026-08-27", EndDate: "2026-08-27", Timezone: "Asia/Shanghai"}, Reason: "事务回滚",
		IdempotencyKey: "rollback-container", RequestID: "rollback-container-request"})
	if err == nil || !strings.Contains(err.Error(), "synthetic_trip_link_failure") {
		t.Fatalf("expected injected automatic link failure: %v", err)
	}
	if after := tripTransactionalState(t, fixture); after != before {
		t.Fatal("failed auto assignment left partial management/payment state")
	}
	execOldSQL(t, fixture.store.DB(), `DROP TRIGGER synthetic_fail_trip_link ON trip_fact_assignments`)
	first := seedManualTrip(t, fixture, "rollback-first", "合成原行程", "2026-09-01", "2026-09-02")
	other := seedManualTrip(t, fixture, "rollback-other", "合成目标行程", "2026-09-03", "2026-09-04")
	review := seedAdditionalReview(t, fixture, tripEnvelope("合成出发", "合成目的地", "2026-09-01", "2026-09-02"), "rollback-material")
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	material, err := reviewService.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{ExpectedRevision: review.Revision, IdempotencyKey: "rollback-material-confirm", RequestID: "rollback-material-request"})
	if err != nil {
		t.Fatal(err)
	}
	linked, err := service.AssignMaterial(ctx, fixture.tenant, tripapp.MaterialInput{EvidenceID: material.FactID, DesiredTripID: &first.TripID, ExpectedVersion: 1,
		Reason: "合成归集", IdempotencyKey: "rollback-material-link", RequestID: "rollback-link-request"})
	if err != nil {
		t.Fatal(err)
	}
	before = tripTransactionalState(t, fixture)
	execOldSQL(t, fixture.store.DB(), `CREATE TRIGGER synthetic_fail_material_link BEFORE INSERT ON trip_material_links FOR EACH ROW EXECUTE FUNCTION synthetic_fail_trip_link()`)
	_, err = service.AssignMaterial(ctx, fixture.tenant, tripapp.MaterialInput{EvidenceID: material.FactID, DesiredTripID: &other.TripID,
		ExpectedLinkID: &linked.LinkID, ExpectedVersion: linked.Version, Reason: "失败移动", IdempotencyKey: "rollback-material-move", RequestID: "rollback-move-request"})
	if err == nil || !strings.Contains(err.Error(), "synthetic_trip_link_failure") {
		t.Fatalf("expected injected material link failure: %v", err)
	}
	if after := tripTransactionalState(t, fixture); after != before {
		t.Fatal("failed move ended the old link or changed version/history")
	}
}

func TestTripEditInvalidatesPreviewButPreservesSubmittedSnapshot(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	reimbursements := reimbursementapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	trip := seedManualTrip(t, fixture, "snapshot-edit", "合成原始名称", "2026-08-27", "2026-08-27")
	payment := confirmTripPayment(t, fixture, "snapshot-edit-payment", "2026-08-27T12:00:00+08:00")
	var link string
	if err := fixture.store.DB().QueryRowContext(ctx, `SELECT id FROM trip_fact_assignments WHERE tenant_id = ? AND payment_id = ? AND ended_at IS NULL`, fixture.tenant.TenantID, payment).Scan(&link); err != nil {
		t.Fatal(err)
	}
	preview, err := reimbursements.Preview(ctx, fixture.tenant, trip.TripID, []string{link})
	if err != nil {
		t.Fatal(err)
	}
	edit := tripapp.ManagementInput{Details: domain.TripDetails{Name: "合成新名称", StartDate: "2026-08-27", EndDate: "2026-08-27", Timezone: "Asia/Shanghai"},
		ExpectedVersion: trip.Version, Reason: "仅改名称", IdempotencyKey: "snapshot-rename", RequestID: "snapshot-rename-request"}
	updated, err := service.Manage(ctx, fixture.tenant, trip.TripID, "edit", edit)
	if err != nil {
		t.Fatal(err)
	}
	before := tripTransactionalState(t, fixture)
	submit := reimbursementapp.SubmissionInput{TripID: trip.TripID, AssignmentIDs: []string{link}, ExpectedSnapshotHash: preview.SnapshotHash,
		AcknowledgedFindingKeys: reimbursementFindingKeysForTest(preview), Reason: "合成报销", IdempotencyKey: "snapshot-submit", RequestID: "snapshot-submit-request"}
	if _, err := reimbursements.Submit(ctx, fixture.tenant, submit); !hasRuleCode(err, "reimbursement_snapshot_stale") {
		t.Fatalf("rename did not invalidate preview: %v", err)
	}
	if after := tripTransactionalState(t, fixture); after != before {
		t.Fatal("stale preview wrote state")
	}
	preview, err = reimbursements.Preview(ctx, fixture.tenant, trip.TripID, []string{link})
	if err != nil {
		t.Fatal(err)
	}
	submit.ExpectedSnapshotHash = preview.SnapshotHash
	submit.AcknowledgedFindingKeys = reimbursementFindingKeysForTest(preview)
	result, err := reimbursements.Submit(ctx, fixture.tenant, submit)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reimbursements.Get(ctx, fixture.tenant, result.ReimbursementID)
	if err != nil {
		t.Fatal(err)
	}
	edit.Details.Notes = "只变更容器备注"
	edit.ExpectedVersion = updated.Version
	edit.IdempotencyKey = "snapshot-notes"
	if _, err := service.Manage(ctx, fixture.tenant, trip.TripID, "edit", edit); err != nil {
		t.Fatal(err)
	}
	after, err := reimbursements.Get(ctx, fixture.tenant, result.ReimbursementID)
	if err != nil || !reflect.DeepEqual(snapshot, after) {
		t.Fatalf("submitted snapshot changed after container edit: %v", err)
	}
}

func TestTripCrossTenantOperationsAreZeroWrite(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	foreign := addTenantReviewFixture(t, fixture)
	service := tripapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	trip := seedManualTrip(t, fixture, "tenant-trip", "合成租户行程", "2026-08-27", "2026-08-27")
	payment := confirmTripPayment(t, fixture, "tenant-payment", "2026-08-27T12:00:00+08:00")
	review := seedAdditionalReview(t, fixture, tripEnvelope("合成出发", "合成目的", "2026-08-27", "2026-08-27"), "tenant-material")
	r := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now})
	material, err := r.Confirm(ctx, fixture.tenant, review.Job.ID, ConfirmInput{ExpectedRevision: review.Revision, IdempotencyKey: "tenant-material-confirm", RequestID: "tenant-material-request"})
	if err != nil {
		t.Fatal(err)
	}
	before := tripTransactionalState(t, fixture)
	_, err = service.Manage(ctx, foreign.tenant, trip.TripID, "edit", tripapp.ManagementInput{Details: domain.TripDetails{Name: "不能跨租户改名", StartDate: "2026-08-27", EndDate: "2026-08-27", Timezone: "Asia/Shanghai"},
		ExpectedVersion: 1, Reason: "合成越界", IdempotencyKey: "tenant-edit-denied", RequestID: "tenant-edit-request"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross tenant edit: %v", err)
	}
	_, err = service.AssignMaterial(ctx, foreign.tenant, tripapp.MaterialInput{EvidenceID: material.FactID, DesiredTripID: &trip.TripID, ExpectedVersion: 1,
		Reason: "合成越界", IdempotencyKey: "tenant-material-denied", RequestID: "tenant-link-request"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross tenant material: %v", err)
	}
	if err := service.Preference(ctx, foreign.tenant, payment, "blocked", "tenant-preference-request", assignmentVersion(t, fixture, domain.DocumentPayment, payment)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross tenant preference: %v", err)
	}
	if after := tripTransactionalState(t, fixture); after != before {
		t.Fatal("cross tenant operation wrote state")
	}
}

func tripTransactionalState(t *testing.T, fixture reviewFixture) string {
	t.Helper()
	var state strings.Builder
	for _, table := range []string{"trips", "trip_management_decisions", "payments", "trip_evidence_facts", "trip_fact_assignments", "trip_fact_assignment_decisions", "trip_material_links", "trip_material_decisions", "audit_events", "reimbursements", "reimbursement_items", "reimbursement_status_decisions"} {
		var snapshot string
		if err := fixture.store.DB().QueryRowContext(context.Background(), `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY r.id), '[]'::jsonb) FROM `+table+` r`).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		state.WriteString(table)
		state.WriteString(snapshot)
	}
	return state.String()
}
