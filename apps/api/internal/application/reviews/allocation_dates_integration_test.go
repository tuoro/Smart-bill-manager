package reviews

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	allocationapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"testing"
)

func TestManualAllocationCrossDateWindowSearchAndPreservation(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	reviews := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	r, err := reviews.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	payment := confirmFactWithoutLinks(t, reviews, f.tenant, r, "date-window-payment")
	invoices := map[string]string{}
	for _, date := range []string{"2026-09-26", "2026-09-27", "2027-09-27"} {
		envelope := invoiceEnvelopeWithTotal("DATE-"+date, 10000)
		for i := range envelope.Fields {
			if envelope.Fields[i].Path == "invoice_date" {
				envelope.Fields[i].Value, _ = json.Marshal(date)
				envelope.Fields[i].Evidence[0].Quote = date
			}
		}
		ready := seedAdditionalReview(t, f, envelope, "date-window-"+date)
		if date != "2026-09-26" && len(ready.Candidates) != 0 {
			t.Fatal("initial review offered out-of-window candidate")
		}
		invoice := confirmFactWithoutLinks(t, reviews, f.tenant, ready, "date-window-confirm-"+date)
		invoices[date] = invoice.FactID
	}
	s := allocationapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	w, err := s.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil || len(w.Targets) != 1 || w.Targets[0].DateDistanceDays != 30 {
		t.Fatalf("30 day recommendation: %v", err)
	}
	page, err := s.SearchTargets(ctx, f.tenant, domain.DocumentPayment, payment.FactID, allocationapp.TargetSearchInput{View: "all_dates", Query: "DATE-2026-09-27"})
	if err != nil || len(page.Items) != 1 || page.Items[0].DateDistanceDays != 31 {
		t.Fatalf("31 day search: %v", err)
	}
	reverse, err := s.SearchTargets(ctx, f.tenant, domain.DocumentInvoice, invoices["2026-09-27"], allocationapp.TargetSearchInput{View: "all_dates", Query: payment.FactID})
	if err != nil || len(reverse.Items) != 1 || reverse.Items[0].DateDistanceDays != 31 {
		t.Fatalf("reverse search: %v", err)
	}
	for _, q := range []string{"%", "_", "\\"} {
		page, err := s.SearchTargets(ctx, f.tenant, domain.DocumentPayment, payment.FactID, allocationapp.TargetSearchInput{View: "all_dates", Query: q})
		if err != nil || len(page.Items) != 0 {
			t.Fatal("search did not treat metacharacters literally")
		}
	}
	desired := []domain.DesiredAllocation{{TargetFactID: invoices["2027-09-27"], AllocatedMinor: 10000}}
	input := allocationapp.AdjustmentInput{ExpectedPlanHash: w.PlanHash, DesiredAllocations: desired, IdempotencyKey: "manual-cross-date", RequestID: "manual-cross-date"}
	if _, err := s.Adjust(ctx, f.tenant, domain.DocumentPayment, payment.FactID, input); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("missing reason accepted")
	}
	input.Reason = "跨期发票由人工核对"
	_, err = s.Adjust(ctx, f.tenant, domain.DocumentPayment, payment.FactID, input)
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, payment.FactID)
	if err != nil || len(current.Links) != 1 || len(current.Targets) != 2 {
		t.Fatalf("current cross-date target missing: %v", err)
	}
	found := false
	for _, target := range current.Targets {
		if target.ID == invoices["2027-09-27"] {
			found = target.CurrentAllocatedMinor == 10000 && target.RemainingMinor == 0 && target.DateDistanceDays > 30
		}
	}
	if !found {
		t.Fatal("fully allocated cross-date target was silently removed")
	}
	setSyntheticBadDebt(t, f, domain.DocumentInvoice, invoices["2026-09-27"], true, "cross-date-bad-debt")
	_, err = s.Adjust(ctx, f.tenant, domain.DocumentPayment, payment.FactID, allocationapp.AdjustmentInput{ExpectedPlanHash: current.PlanHash, DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: invoices["2026-09-27"], AllocatedMinor: 1000}}, Reason: "明确跨期替换", IdempotencyKey: "manual-cross-date-replace", RequestID: "manual-cross-date-replace"})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now}).Detail(ctx, f.tenant, domain.DocumentInvoice, invoices["2026-09-27"])
	if err != nil || !fact.Invoice.BadDebt || fact.Invoice.AllocatedMinor != 1000 {
		t.Fatal("allocation changed bad debt or balance")
	}
}
