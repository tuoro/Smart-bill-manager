package reviews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	allocationapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestConfirmedFactAllocationAdjustmentLifecycle(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	paymentReview, err := reviewService.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment := confirmFactWithoutLinks(t, reviewService, fixture.tenant, paymentReview, "adjust-seed-payment")
	invoiceA := confirmFactWithoutLinks(
		t,
		reviewService,
		fixture.tenant,
		seedAdditionalReview(t, fixture, invoiceEnvelopeWithTotal("ADJUST-A", 6_000), "adjust-invoice-a"),
		"adjust-seed-invoice-a",
	)
	invoiceB := confirmFactWithoutLinks(
		t,
		reviewService,
		fixture.tenant,
		seedAdditionalReview(t, fixture, invoiceEnvelopeWithTotal("ADJUST-B", 7_000), "adjust-invoice-b"),
		"adjust-seed-invoice-b",
	)
	service := allocationapp.NewService(
		fixture.store,
		fixture.store,
		system.IDGenerator{},
		fixedClock{now: fixture.now.Add(10 * time.Hour)},
	)

	initial, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Links) != 0 || len(initial.Targets) != 2 || initial.Anchor.AllocatedMinor != 0 {
		t.Fatalf("initial workspace = %#v", initial)
	}
	firstInput := allocationapp.AdjustmentInput{
		ExpectedPlanHash: initial.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{{
			TargetFactID: invoiceA.FactID, AllocatedMinor: 4_000,
		}},
		Reason:         " 首次人工补充 ",
		IdempotencyKey: "allocation-supplement-one",
		RequestID:      "allocation-supplement-one-request",
	}
	first, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.Mode != domain.AllocationModeSupplement || first.Replayed || len(first.CreatedLinkIDs) != 1 || len(first.EndedLinkIDs) != 0 {
		t.Fatalf("supplement result = %#v", first)
	}
	replay, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.AdjustmentID != first.AdjustmentID || strings.Join(replay.CreatedLinkIDs, ",") != strings.Join(first.CreatedLinkIDs, ",") {
		t.Fatalf("replay = %#v, first = %#v", replay, first)
	}
	changedKeyRequest := firstInput
	changedKeyRequest.Reason = "改变同键请求"
	if _, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, changedKeyRequest); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed idempotency request error = %v", err)
	}
	stale := firstInput
	stale.IdempotencyKey = "allocation-stale-plan"
	stale.RequestID = "allocation-stale-plan-request"
	stale.DesiredAllocations = []domain.DesiredAllocation{{TargetFactID: invoiceB.FactID, AllocatedMinor: 1_000}}
	if _, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, stale); !hasRuleCode(err, "allocation_plan_stale") {
		t.Fatalf("stale plan error = %v", err)
	}

	afterFirst, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	unchanged := allocationapp.AdjustmentInput{
		ExpectedPlanHash: afterFirst.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{{
			TargetFactID: invoiceA.FactID, AllocatedMinor: 4_000,
		}},
		Reason: "无变化不应留痕", IdempotencyKey: "allocation-unchanged-plan", RequestID: "allocation-unchanged-plan-request",
	}
	if _, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, unchanged); !hasRuleCode(err, "allocation_plan_unchanged") {
		t.Fatalf("unchanged plan error = %v", err)
	}
	retainedSupplement, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash: afterFirst.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{
			{TargetFactID: invoiceA.FactID, AllocatedMinor: 4_000},
			{TargetFactID: invoiceB.FactID, AllocatedMinor: 1_000},
		},
		Reason: "保留原分配并补充目标", IdempotencyKey: "allocation-retained-supplement", RequestID: "allocation-retained-supplement-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if retainedSupplement.Mode != domain.AllocationModeSupplement || len(retainedSupplement.EndedLinkIDs) != 0 || len(retainedSupplement.CreatedLinkIDs) != 1 {
		t.Fatalf("retained supplement result = %#v", retainedSupplement)
	}
	afterSupplement, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	replace := allocationapp.AdjustmentInput{
		ExpectedPlanHash: afterSupplement.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{
			{TargetFactID: invoiceB.FactID, AllocatedMinor: 3_000},
			{TargetFactID: invoiceA.FactID, AllocatedMinor: 5_000},
		},
		Reason: "修改金额并补充目标", IdempotencyKey: "allocation-replace-plan", RequestID: "allocation-replace-plan-request",
	}
	replaced, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, replace)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Mode != domain.AllocationModeReplace || len(replaced.EndedLinkIDs) != 2 || len(replaced.CreatedLinkIDs) != 2 {
		t.Fatalf("replace result = %#v", replaced)
	}
	afterReplace, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	decreased, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash: afterReplace.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{
			{TargetFactID: invoiceA.FactID, AllocatedMinor: 4_500},
			{TargetFactID: invoiceB.FactID, AllocatedMinor: 3_000},
		},
		Reason: "降低现有分配金额", IdempotencyKey: "allocation-decrease-amount", RequestID: "allocation-decrease-amount-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decreased.Mode != domain.AllocationModeReplace || len(decreased.EndedLinkIDs) != 1 || len(decreased.CreatedLinkIDs) != 1 {
		t.Fatalf("decrease result = %#v", decreased)
	}
	afterDecrease, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	withdrawOne, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash: afterDecrease.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{{
			TargetFactID: invoiceA.FactID, AllocatedMinor: 4_500,
		}},
		Reason: "撤销第二个目标", IdempotencyKey: "allocation-withdraw-one", RequestID: "allocation-withdraw-one-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if withdrawOne.Mode != domain.AllocationModeWithdraw || len(withdrawOne.EndedLinkIDs) != 1 || len(withdrawOne.CreatedLinkIDs) != 0 {
		t.Fatalf("withdraw one result = %#v", withdrawOne)
	}
	afterWithdraw, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	withdrawAll, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash:   afterWithdraw.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{},
		Reason:             "撤销全部分配", IdempotencyKey: "allocation-withdraw-all", RequestID: "allocation-withdraw-all-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if withdrawAll.Mode != domain.AllocationModeWithdraw || len(withdrawAll.EndedLinkIDs) != 1 || len(withdrawAll.CreatedLinkIDs) != 0 {
		t.Fatalf("withdraw all result = %#v", withdrawAll)
	}

	var adjustments, activeLinks, historicalLinks, audits, reviewCreated, adjustmentCreated, deletionEnded, adjustmentEnded int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT
		  (SELECT count(*) FROM payment_invoice_allocation_adjustments WHERE tenant_id = ?),
		  (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND ended_at IS NULL),
		  (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ?),
		  (SELECT count(*) FROM audit_events WHERE tenant_id = ? AND action = 'payment_invoice_allocation_adjusted'),
		  (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND link_decision_id IS NOT NULL),
		  (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND created_by_adjustment_id IS NOT NULL),
		  (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND ended_by_audit_event_id IS NOT NULL),
		  (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND ended_by_adjustment_id IS NOT NULL)
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID,
		fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID).Scan(
		&adjustments, &activeLinks, &historicalLinks, &audits,
		&reviewCreated, &adjustmentCreated, &deletionEnded, &adjustmentEnded,
	); err != nil {
		t.Fatal(err)
	}
	if adjustments != 6 || activeLinks != 0 || historicalLinks != 5 || audits != 6 || reviewCreated != 0 || adjustmentCreated != 5 || deletionEnded != 0 || adjustmentEnded != 5 {
		t.Fatalf("history = adjustments:%d active:%d links:%d audits:%d review-created:%d adjustment-created:%d deletion-ended:%d adjustment-ended:%d",
			adjustments, activeLinks, historicalLinks, audits, reviewCreated, adjustmentCreated, deletionEnded, adjustmentEnded)
	}
	rows, err := fixture.store.DB().QueryContext(ctx, `
		SELECT safe_metadata_json FROM audit_events
		WHERE tenant_id = ? AND action = 'payment_invoice_allocation_adjusted'
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
		if strings.Contains(metadata, "5000") || strings.Contains(metadata, "首次人工补充") {
			t.Fatalf("unsafe audit metadata = %s", metadata)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(metadata), &decoded); err != nil || len(decoded) != 3 {
			t.Fatalf("audit metadata = %#v, error = %v", decoded, err)
		}
		for key := range decoded {
			if key != "mode" && key != "created_link_count" && key != "ended_link_count" {
				t.Fatalf("unexpected audit metadata key %q in %#v", key, decoded)
			}
		}
		mode, ok := decoded["mode"].(string)
		if !ok || (mode != domain.AllocationModeSupplement && mode != domain.AllocationModeWithdraw && mode != domain.AllocationModeReplace) {
			t.Fatalf("unsafe audit mode = %#v", decoded["mode"])
		}
		for _, key := range []string{"created_link_count", "ended_link_count"} {
			count, ok := decoded[key].(float64)
			if !ok || count < 0 || count > domain.MaxAllocationTargets {
				t.Fatalf("unsafe audit count %s = %#v", key, decoded[key])
			}
		}
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE payment_invoice_allocation_adjustments SET reason = 'mutated' WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, first.AdjustmentID); err == nil || !strings.Contains(err.Error(), "allocation_adjustment_immutable") {
		t.Fatalf("adjustment mutation error = %v", err)
	}
	if _, err := fixture.store.DB().ExecContext(ctx, `
		UPDATE payment_invoice_links SET allocated_minor = allocated_minor + 1 WHERE tenant_id = ? AND id = ?
	`, fixture.tenant.TenantID, first.CreatedLinkIDs[0]); err == nil || !strings.Contains(err.Error(), "payment_invoice_link_immutable") {
		t.Fatalf("link mutation error = %v", err)
	}
}

func TestAllocationAdjustmentPermissionsAndTargetBoundaries(t *testing.T) {
	fixture := newReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	paymentReview, err := reviewService.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment := confirmFactWithoutLinks(t, reviewService, fixture.tenant, paymentReview, "boundary-seed-payment")
	validInvoice := confirmFactWithoutLinks(
		t, reviewService, fixture.tenant,
		seedAdditionalReview(t, fixture, invoiceEnvelopeWithTotal("BOUNDARY-VALID", 5_000), "boundary-valid-invoice"),
		"boundary-seed-valid-invoice",
	)
	wrongCurrencyEnvelope := invoiceEnvelopeWithTotal("BOUNDARY-USD", 5_000)
	setClaimField(t, &wrongCurrencyEnvelope, "currency", `"USD"`)
	wrongCurrency := confirmFactWithoutLinks(
		t, reviewService, fixture.tenant,
		seedAdditionalReview(t, fixture, wrongCurrencyEnvelope, "boundary-usd-invoice"),
		"boundary-seed-usd-invoice",
	)
	outOfRangeEnvelope := invoiceEnvelopeWithTotal("BOUNDARY-OLD", 5_000)
	setClaimField(t, &outOfRangeEnvelope, "invoice_date", `"2026-06-01"`)
	outOfRange := confirmFactWithoutLinks(
		t, reviewService, fixture.tenant,
		seedAdditionalReview(t, fixture, outOfRangeEnvelope, "boundary-old-invoice"),
		"boundary-seed-old-invoice",
	)
	deletedInvoice := confirmFactWithoutLinks(
		t, reviewService, fixture.tenant,
		seedAdditionalReview(t, fixture, invoiceEnvelopeWithTotal("BOUNDARY-DELETED", 5_000), "boundary-deleted-invoice"),
		"boundary-seed-deleted-invoice",
	)
	facts := NewFactService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(5 * time.Hour)})
	if err := facts.Delete(ctx, fixture.tenant, domain.DocumentInvoice, deletedInvoice.FactID, "boundary-delete-invoice"); err != nil {
		t.Fatal(err)
	}
	foreignFixture := addTenantReviewFixture(t, fixture)
	foreignInvoice := confirmFactWithoutLinks(
		t,
		reviewService,
		foreignFixture.tenant,
		seedAdditionalReview(t, foreignFixture, invoiceEnvelopeWithTotal("BOUNDARY-FOREIGN", 5_000), "boundary-foreign-invoice"),
		"boundary-seed-foreign-invoice",
	)
	service := allocationapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(10 * time.Hour)})
	workspace, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Targets) != 1 || workspace.Targets[0].ID != validInvoice.FactID {
		t.Fatalf("eligible targets = %#v", workspace.Targets)
	}
	roles := []struct {
		role    domain.Role
		allowed bool
	}{{domain.RoleOwner, true}, {domain.RoleFinance, true}, {domain.RoleReviewer, false}, {domain.RoleViewer, false}}
	for _, entry := range roles {
		t.Run(string(entry.role), func(t *testing.T) {
			tenant := fixture.tenant
			tenant.Role = entry.role
			_, getErr := service.GetWorkspace(ctx, tenant, domain.DocumentPayment, payment.FactID)
			if (getErr == nil) != entry.allowed {
				t.Fatalf("GetWorkspace error = %v", getErr)
			}
			_, adjustErr := service.Adjust(ctx, tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{
				ExpectedPlanHash:   workspace.PlanHash,
				DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: validInvoice.FactID, AllocatedMinor: 1}},
				Reason:             "权限矩阵", IdempotencyKey: "role-matrix-" + string(entry.role), RequestID: "role-matrix-request-" + string(entry.role),
			})
			if entry.allowed {
				if adjustErr != nil && !hasRuleCode(adjustErr, "allocation_plan_stale") && !hasRuleCode(adjustErr, "allocation_plan_unchanged") {
					t.Fatalf("allowed Adjust error = %v", adjustErr)
				}
			} else if !errors.Is(adjustErr, domain.ErrForbidden) {
				t.Fatalf("forbidden Adjust error = %v", adjustErr)
			}
		})
	}
	latest, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil {
		t.Fatal(err)
	}
	invalidTargets := []string{wrongCurrency.FactID, deletedInvoice.FactID, foreignInvoice.FactID}
	for index, targetID := range invalidTargets {
		_, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{
			ExpectedPlanHash:   latest.PlanHash,
			DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: targetID, AllocatedMinor: 1}},
			Reason:             "边界拒绝", IdempotencyKey: "boundary-invalid-target-" + string(rune('a'+index)), RequestID: "boundary-invalid-target-request",
		})
		if !hasRuleCode(err, "allocation_target_unavailable") {
			t.Fatalf("target %s error = %v", targetID, err)
		}
	}
	if _, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentInvoice, foreignInvoice.FactID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign anchor error = %v", err)
	}
	if _, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{
		ExpectedPlanHash:   latest.PlanHash,
		DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: validInvoice.FactID, AllocatedMinor: 5_001}},
		Reason:             "目标超额", IdempotencyKey: "boundary-target-overflow", RequestID: "boundary-target-overflow-request",
	}); !hasRuleCode(err, "allocation_exceeds_target_balance") {
		t.Fatalf("target overflow error = %v", err)
	}
	if _, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{ExpectedPlanHash: latest.PlanHash, DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: outOfRange.FactID, AllocatedMinor: 1}}, Reason: "人工核对跨期目标", IdempotencyKey: "boundary-cross-date-allowed", RequestID: "boundary-cross-date-allowed"}); err != nil {
		t.Fatalf("reasoned cross-date allocation rejected: %v", err)
	}
}

func TestConcurrentAdjustmentsAndFactDeletionKeepBalancesAtomic(t *testing.T) {
	fixture := newFileReviewFixture(t)
	ctx := context.Background()
	reviewService := NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(time.Hour)})
	invoice := confirmFactWithoutLinks(
		t, reviewService, fixture.tenant,
		seedAdditionalReview(t, fixture, invoiceEnvelopeWithTotal("CONCURRENT-TARGET", 1_000), "concurrent-target"),
		"concurrent-seed-target",
	)
	paymentOneReview, err := reviewService.Get(ctx, fixture.tenant, fixture.jobID)
	if err != nil {
		t.Fatal(err)
	}
	paymentOne := confirmFactWithoutLinks(t, reviewService, fixture.tenant, paymentOneReview, "concurrent-seed-payment-one")
	paymentTwo := confirmFactWithoutLinks(
		t, reviewService, fixture.tenant,
		seedAdditionalReview(t, fixture, paymentEnvelopeWithAmount(1_000), "concurrent-payment-two"),
		"concurrent-seed-payment-two",
	)
	service := allocationapp.NewService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(10 * time.Hour)})
	one, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, paymentOne.FactID)
	if err != nil {
		t.Fatal(err)
	}
	two, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, paymentTwo.FactID)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		paymentID string
		result    ports.AllocationAdjustmentResult
		err       error
	}
	outcomes := make(chan outcome, 2)
	var wait sync.WaitGroup
	for index, entry := range []struct {
		paymentID string
		hash      string
	}{{paymentOne.FactID, one.PlanHash}, {paymentTwo.FactID, two.PlanHash}} {
		wait.Add(1)
		go func(index int, entry struct{ paymentID, hash string }) {
			defer wait.Done()
			result, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, entry.paymentID, allocationapp.AdjustmentInput{
				ExpectedPlanHash:   entry.hash,
				DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: invoice.FactID, AllocatedMinor: 1_000}},
				Reason:             "并发占用最后余额", IdempotencyKey: "concurrent-allocation-" + string(rune('a'+index)), RequestID: "concurrent-allocation-request",
			})
			outcomes <- outcome{paymentID: entry.paymentID, result: result, err: err}
		}(index, entry)
	}
	wait.Wait()
	close(outcomes)
	var winner outcome
	successes, conflicts := 0, 0
	for item := range outcomes {
		if item.err == nil {
			successes++
			winner = item
		} else if errors.Is(item.err, domain.ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent error = %v", item.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent outcomes = successes:%d conflicts:%d", successes, conflicts)
	}
	var activeLinks, adjustments, audits int
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT
		 (SELECT count(*) FROM payment_invoice_links WHERE tenant_id = ? AND invoice_id = ? AND ended_at IS NULL),
		 (SELECT count(*) FROM payment_invoice_allocation_adjustments WHERE tenant_id = ?),
		 (SELECT count(*) FROM audit_events WHERE tenant_id = ? AND action = 'payment_invoice_allocation_adjusted')
	`, fixture.tenant.TenantID, invoice.FactID, fixture.tenant.TenantID, fixture.tenant.TenantID).Scan(&activeLinks, &adjustments, &audits); err != nil {
		t.Fatal(err)
	}
	if activeLinks != 1 || adjustments != 1 || audits != 1 {
		t.Fatalf("concurrent persistence = active:%d adjustments:%d audits:%d", activeLinks, adjustments, audits)
	}

	winnerWorkspace, err := service.GetWorkspace(ctx, fixture.tenant, domain.DocumentPayment, winner.paymentID)
	if err != nil {
		t.Fatal(err)
	}
	factService := NewFactService(fixture.store, fixture.store, system.IDGenerator{}, fixedClock{now: fixture.now.Add(11 * time.Hour)})
	deleteErrors := make(chan error, 1)
	adjustErrors := make(chan error, 1)
	go func() {
		deleteErrors <- factService.Delete(ctx, fixture.tenant, domain.DocumentInvoice, invoice.FactID, "concurrent-delete-target")
	}()
	go func() {
		_, err := service.Adjust(ctx, fixture.tenant, domain.DocumentPayment, winner.paymentID, allocationapp.AdjustmentInput{
			ExpectedPlanHash:   winnerWorkspace.PlanHash,
			DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: invoice.FactID, AllocatedMinor: 500}},
			Reason:             "与删除竞态替换", IdempotencyKey: "concurrent-delete-adjust", RequestID: "concurrent-delete-adjust-request",
		})
		adjustErrors <- err
	}()
	if err := <-deleteErrors; err != nil {
		t.Fatalf("delete race error = %v", err)
	}
	if err := <-adjustErrors; err != nil && !errors.Is(err, domain.ErrConflict) && !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("adjust race error = %v", err)
	}
	if err := fixture.store.DB().QueryRowContext(ctx, `
		SELECT count(*) FROM payment_invoice_links
		WHERE tenant_id = ? AND invoice_id = ? AND ended_at IS NULL
	`, fixture.tenant.TenantID, invoice.FactID).Scan(&activeLinks); err != nil {
		t.Fatal(err)
	}
	if activeLinks != 0 {
		t.Fatalf("active links after deletion race = %d", activeLinks)
	}
}

func TestAllocationWorkspacePagesMoreThanTwoHundredTargetsWithoutTruncation(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	trip := seedManualTrip(t, f, "allocation-query-boundary", "合成分页行程", "2026-08-26", "2026-08-28")
	seedAssignedPaymentsForExportBoundary(t, f, trip.TripID, 201)
	reviews := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	invoice := confirmFactWithoutLinks(t, reviews, f.tenant, seedAdditionalReview(t, f, invoiceEnvelopeWithTotal("PAGE-BOUNDARY", 10000), "allocation-page-invoice"), "allocation-page-confirm")
	service := allocationapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	workspace, err := service.GetWorkspace(ctx, f.tenant, domain.DocumentInvoice, invoice.FactID)
	if err != nil || len(workspace.Targets) != 50 || workspace.NextCursor == "" {
		t.Fatalf("first bounded workspace: %v", err)
	}
	seen := map[string]bool{}
	for _, item := range workspace.Targets {
		seen[item.ID] = true
	}
	cursor := workspace.NextCursor
	for cursor != "" {
		page, err := service.SearchTargets(ctx, f.tenant, domain.DocumentInvoice, invoice.FactID, allocationapp.TargetSearchInput{Cursor: cursor})
		if err != nil || len(page.Items) > 50 {
			t.Fatalf("page: %v", err)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("repeated target")
			}
			seen[item.ID] = true
		}
		cursor = page.NextCursor
	}
	if len(seen) != 201 {
		t.Fatalf("truncated target collection: %d", len(seen))
	}
	desired := make([]domain.DesiredAllocation, 0, 200)
	for index := 1; index <= 200; index++ {
		desired = append(desired, domain.DesiredAllocation{TargetFactID: fmt.Sprintf("export-bulk-payment-%03d", index), AllocatedMinor: 1})
	}
	_, err = service.Adjust(ctx, f.tenant, domain.DocumentInvoice, invoice.FactID, allocationapp.AdjustmentInput{ExpectedPlanHash: workspace.PlanHash, DesiredAllocations: desired, Reason: "合成 200 条边界", IdempotencyKey: "allocation-limit-200", RequestID: "allocation-limit-200"})
	if err != nil {
		t.Fatal(err)
	}
	extra, err := service.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, "export-bulk-payment-201")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Adjust(ctx, f.tenant, domain.DocumentPayment, "export-bulk-payment-201", allocationapp.AdjustmentInput{ExpectedPlanHash: extra.PlanHash, DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: invoice.FactID, AllocatedMinor: 1}}, Reason: "不能绕过对端数量上限", IdempotencyKey: "allocation-limit-201", RequestID: "allocation-limit-201"})
	if !hasRuleCode(err, "allocation_active_target_limit_exceeded") {
		t.Fatalf("201st reverse link: %v", err)
	}
	var linkCount, adjustments int
	if err := f.store.DB().QueryRow(`SELECT (SELECT count(*) FROM payment_invoice_links),(SELECT count(*) FROM payment_invoice_allocation_adjustments)`).Scan(&linkCount, &adjustments); err != nil || linkCount != 200 || adjustments != 1 {
		t.Fatal("capacity rejection did not rollback")
	}
	_, err = service.SearchTargets(ctx, f.tenant, domain.DocumentInvoice, invoice.FactID, allocationapp.TargetSearchInput{Cursor: workspace.NextCursor, View: "all_dates"})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("cursor reused across scope")
	}
}

func confirmFactWithoutLinks(
	t *testing.T,
	service Service,
	tenant domain.TenantContext,
	review ports.ReviewSnapshot,
	key string,
) ports.ConfirmResult {
	t.Helper()
	mode := AssociationNoCandidate
	if len(review.Candidates) != 0 {
		mode = AssociationRejectAll
	}
	resolutions := make([]domain.DuplicateResolution, 0, len(review.DuplicateCandidates))
	for _, candidate := range review.DuplicateCandidates {
		resolutions = append(resolutions, domain.DuplicateResolution{
			CandidateID: candidate.ID,
			Action:      domain.DuplicateKeepDistinct,
		})
	}
	result, err := service.Confirm(context.Background(), tenant, review.Job.ID, ConfirmInput{
		ExpectedRevision:     review.Revision,
		AssociationMode:      mode,
		Allocations:          []domain.AllocationRequest{},
		DuplicateResolutions: resolutions,
		IdempotencyKey:       key,
		RequestID:            key + "-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func setClaimField(t *testing.T, envelope *domain.ClaimEnvelope, path, value string) {
	t.Helper()
	for index := range envelope.Fields {
		if envelope.Fields[index].Path == path {
			envelope.Fields[index].Value = json.RawMessage(value)
			return
		}
	}
	t.Fatalf("missing claim field %s", path)
}

func hasRuleCode(err error, code string) bool {
	var ruleError *domain.RuleError
	return errors.As(err, &ruleError) && ruleError.Code == code
}

func fmtInt(value int) string {
	return strconv.Itoa(value)
}
