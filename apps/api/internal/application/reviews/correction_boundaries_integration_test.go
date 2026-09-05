package reviews

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	allocationapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestFactCorrectionInvoiceItemReplacementRetainsHistoricalOrigins(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	r := seedAdditionalReview(t, f, invoiceWithItemsEnvelope("CORRECTION-ITEMS"), "correction-items")
	fact := confirmFactWithoutLinks(t, s, f.tenant, r, "correction-items-first")
	w, err := s.GetCorrection(ctx, f.tenant, domain.DocumentInvoice, fact.FactID)
	if err != nil {
		t.Fatal(err)
	}
	const historicalSQL = `SELECT jsonb_build_object('items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM invoice_items i WHERE invoice_id=? AND review_decision_id=?),'origins',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM fact_field_origins o WHERE review_decision_id=?))::text`
	var before string
	if err := f.store.DB().QueryRow(historicalSQL, fact.FactID, w.State.CurrentReviewDecisionID, w.State.CurrentReviewDecisionID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	input := correctionInputFrom(w)
	const oldKey = "00000000-0000-0000-0000-000000000002"
	const newKey = "00000000-0000-0000-0000-000000000003"
	// 用新的明细身份替换一行，金额保持一致；旧明细及其来源不能被删掉或改写。
	for index := range input.Fields {
		field := &input.Fields[index]
		if !strings.HasPrefix(field.Path, "items["+oldKey+"]") {
			continue
		}
		field.Path = strings.Replace(field.Path, oldKey, newKey, 1)
		if field.Presence == "present" {
			field.ManualEvidence = []domain.ManualEvidenceInput{{Page: 1, Quote: "合成新增明细摘录"}}
			if strings.HasSuffix(field.Path, "].name") {
				field.Value = json.RawMessage(`"合成替换明细"`)
			}
		}
	}
	applyCorrection(t, s, f.tenant, domain.DocumentInvoice, fact.FactID, input, "correction-items-replaced")
	current, err := f.store.ReadFactDetail(ctx, f.tenant.TenantID, domain.DocumentInvoice, fact.FactID, true)
	if err != nil || current.Invoice == nil || len(current.Invoice.Items) != 2 || current.Invoice.Items[1].ItemKey != newKey {
		t.Fatalf("current items not replaced: %v", err)
	}
	var after string
	if err := f.store.DB().QueryRow(historicalSQL, fact.FactID, w.State.CurrentReviewDecisionID, w.State.CurrentReviewDecisionID).Scan(&after); err != nil || before != after {
		t.Fatal("historical invoice items or origins changed")
	}
}

func TestFactCorrectionRejectsChangedRelationsAndDifferentKeyConcurrency(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	r, err := s.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	p := confirmFactWithoutLinks(t, s, f.tenant, r, "correction-race-first")
	i := confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, invoiceEnvelope("CORRECTION-RACE"), "correction-race-invoice"), "correction-race-invoice")
	w, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil {
		t.Fatal(err)
	}
	input := correctionInputFrom(w)
	correctionField(t, &input, "merchant", "合成关系变更测试")
	preview, err := s.PreviewCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, input)
	if err != nil {
		t.Fatal(err)
	}
	a := allocationapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	aw, err := a.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Adjust(ctx, f.tenant, domain.DocumentPayment, p.FactID, allocationapp.AdjustmentInput{ExpectedPlanHash: aw.PlanHash, DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: i.FactID, AllocatedMinor: 1000}}, Reason: "合成预览后分配", IdempotencyKey: "correction-race-link", RequestID: "correction-race-link-request"}); err != nil {
		t.Fatal(err)
	}
	request := CorrectionConfirmInput{CorrectionInput: input, PreviewHash: preview.PreviewHash, AcknowledgedDuplicateKeys: []string{}, IdempotencyKey: "correction-race-stale", RequestID: "correction-race-stale-request"}
	if _, err := s.ConfirmCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, request); !errors.Is(err, domain.ErrConflict) && !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("changed relation accepted: %v", err)
	}
	w, err = s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil {
		t.Fatal(err)
	}
	input = correctionInputFrom(w)
	correctionField(t, &input, "merchant", "合成并发纠错")
	preview, err = s.PreviewCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, input)
	if err != nil {
		t.Fatal(err)
	}
	request.CorrectionInput = input
	request.PreviewHash = preview.PreviewHash
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, 2)
	for index, key := range []string{"correction-race-a", "correction-race-b"} {
		wg.Add(1)
		go func(index int, key string) {
			defer wg.Done()
			<-start
			candidate := request
			candidate.IdempotencyKey = key
			_, results[index] = s.ConfirmCorrection(ctx, f.tenant, domain.DocumentPayment, p.FactID, candidate)
		}(index, key)
	}
	close(start)
	wg.Wait()
	success := 0
	for _, err := range results {
		if err == nil {
			success++
			continue
		}
		if !errors.Is(err, domain.ErrVersionConflict) && !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	var count int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM fact_corrections WHERE payment_id=?`, p.FactID).Scan(&count); err != nil || success != 1 || count != 1 {
		t.Fatal("concurrent corrections did not preserve one winner")
	}
}

func TestFactCorrectionTripMaterialAndAmbiguousAutomaticAttribution(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	trips := tripapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	trip := seedManualTrip(t, f, "correction-material-container", "合成材料容器", "2026-08-26", "2026-08-28")
	r := seedAdditionalReview(t, f, tripEnvelope("合成出发", "合成目的", "2026-08-26", "2026-08-28"), "correction-material-ticket")
	evidence, err := s.Confirm(ctx, f.tenant, r.Job.ID, ConfirmInput{ExpectedRevision: r.Revision, IdempotencyKey: "correction-material-first", RequestID: "correction-material-first-request"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trips.AssignMaterial(ctx, f.tenant, tripapp.MaterialInput{EvidenceID: evidence.FactID, DesiredTripID: &trip.TripID, ExpectedVersion: 1, Reason: "合成材料关联", IdempotencyKey: "correction-material-link", RequestID: "correction-material-link-request"}); err != nil {
		t.Fatal(err)
	}
	const materialSQL = `SELECT jsonb_build_object('trips',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM trips t),'links',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM trip_material_links l),'decisions',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM trip_material_decisions d))::text`
	var before, after string
	if err := f.store.DB().QueryRow(materialSQL).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetCorrection(ctx, f.tenant, domain.DocumentTrip, evidence.FactID)
	if err != nil {
		t.Fatal(err)
	}
	input := correctionInputFrom(w)
	correctionField(t, &input, "destination", "合成新目的地")
	applyCorrection(t, s, f.tenant, domain.DocumentTrip, evidence.FactID, input, "correction-material-fields")
	if err := f.store.DB().QueryRow(materialSQL).Scan(&after); err != nil || before != after {
		t.Fatal("ticket correction moved or rewrote material/container")
	}
	payment := confirmTripPayment(t, f, "correction-ambiguous-payment", "2026-08-27T12:00:00+08:00")
	seedManualTrip(t, f, "correction-overlap-a", "合成重叠甲", "2026-09-01", "2026-09-03")
	seedManualTrip(t, f, "correction-overlap-b", "合成重叠乙", "2026-09-02", "2026-09-04")
	for _, tc := range []struct {
		key, instant string
		count        int
	}{{"overlap", "2026-09-02T12:00:00+08:00", 2}, {"unmatched", "2026-10-01T12:00:00+08:00", 0}} {
		w, err := s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, payment)
		if err != nil {
			t.Fatal(err)
		}
		input := correctionInputFrom(w)
		correctionField(t, &input, "transaction_time", tc.instant)
		preview, err := s.PreviewCorrection(ctx, f.tenant, domain.DocumentPayment, payment, input)
		if err != nil || preview.State.Attribution.MatchingTripCount != tc.count || preview.State.Attribution.DesiredTripID != "" {
			t.Fatalf("ambiguous attribution not explained: %v", err)
		}
		applyCorrection(t, s, f.tenant, domain.DocumentPayment, payment, input, "correction-"+tc.key)
		assertPaymentTrip(t, f, payment, "", "auto")
	}
	if err := trips.Preference(ctx, f.tenant, payment, "blocked", "correction-block-auto", assignmentVersion(t, f, domain.DocumentPayment, payment)); err != nil {
		t.Fatal(err)
	}
	w, err = s.GetCorrection(ctx, f.tenant, domain.DocumentPayment, payment)
	if err != nil {
		t.Fatal(err)
	}
	input = correctionInputFrom(w)
	correctionField(t, &input, "transaction_time", "2026-08-27T12:00:00+08:00")
	applyCorrection(t, s, f.tenant, domain.DocumentPayment, payment, input, "correction-remain-blocked")
	assertPaymentTrip(t, f, payment, "", "blocked")
}
