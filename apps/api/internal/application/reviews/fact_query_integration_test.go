package reviews

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	allocationapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestFactManagementPagesTraverse201AndSurviveAnchorDeletion(t *testing.T) {
	for _, kind := range []domain.DocumentType{domain.DocumentPayment, domain.DocumentInvoice} {
		t.Run(string(kind), func(t *testing.T) {
			f := newReviewFixture(t)
			ctx := context.Background()
			s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			facts := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			seed := func(index int, service Service) string {
				t.Helper()
				label := fmt.Sprintf("fact-page-%s-%03d", kind, index)
				envelope := paymentEnvelopeAt("合成商户 "+label, "2026-08-27T12:00:00+08:00")
				if kind == domain.DocumentInvoice {
					envelope = invoiceEnvelopeWithTotal(label, int64(10000+index))
				}
				r := seedAdditionalReview(t, f, envelope, label)
				return confirmFactWithoutLinks(t, service, f.tenant, r, label+"-confirm").FactID
			}
			// 更正目标保留完整来源链；其余记录只验证分页，不重复 199 次入账流程。
			for index := 0; index < 2; index++ {
				seed(index, NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now.Add(time.Minute)}))
			}
			seedFactPageBoundary(t, f, kind, 199)
			query := func(input FactQueryInput) ([]string, string) {
				t.Helper()
				ids := []string{}
				if kind == domain.DocumentPayment {
					page, err := facts.ListPayments(ctx, f.tenant, input)
					if err != nil {
						t.Fatal(err)
					}
					for _, item := range page.Items {
						ids = append(ids, item.ID)
					}
					return ids, page.NextCursor
				}
				page, err := facts.ListInvoices(ctx, f.tenant, input)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range page.Items {
					if item.Items != nil {
						t.Fatal("list preloaded invoice items")
					}
					ids = append(ids, item.ID)
				}
				return ids, page.NextCursor
			}
			seen := map[string]bool{}
			cursor := ""
			for {
				ids, next := query(FactQueryInput{Limit: 17, Cursor: cursor})
				if len(ids) > 17 {
					t.Fatal("unbounded page")
				}
				for _, id := range ids {
					if seen[id] {
						t.Fatal("duplicate keyset item")
					}
					seen[id] = true
				}
				if next == "" {
					break
				}
				cursor = next
			}
			if len(seen) != 201 {
				t.Fatal("list silently truncated")
			}
			first, cursor := query(FactQueryInput{Limit: 20})
			if err := facts.Delete(ctx, f.tenant, kind, first[len(first)-1], "delete-page-anchor"); err != nil {
				t.Fatal(err)
			}
			newID := seed(999, NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now.Add(time.Hour)}))
			w, err := s.GetCorrection(ctx, f.tenant, kind, first[0])
			if err != nil {
				t.Fatal(err)
			}
			correction := correctionInputFrom(w)
			path, value := "transaction_time", "2026-10-01T12:00:00+08:00"
			if kind == domain.DocumentInvoice {
				path, value = "invoice_date", "2026-10-01"
			}
			correctionField(t, &correction, path, value)
			applyCorrection(t, s, f.tenant, kind, first[0], correction, "correct-page-date")
			tail := []string{}
			for {
				ids, next := query(FactQueryInput{Limit: 20, Cursor: cursor})
				tail = append(tail, ids...)
				if next == "" {
					break
				}
				cursor = next
			}
			if len(tail) != 181 {
				t.Fatalf("deleted anchor prevented continuation: %d", len(tail))
			}
			for _, id := range tail {
				if id == newID || id == first[0] {
					t.Fatal("new record or corrected date shifted into old page range")
				}
			}
			refreshed, _ := query(FactQueryInput{Limit: 20})
			if refreshed[0] != newID {
				t.Fatal("refresh omitted new admission")
			}
		})
	}
}

func TestFactManagementFiltersDetailsAndSourcePermissions(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	facts := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	r := seedAdditionalReview(t, f, paymentEnvelopeAt(`合成 %_\ 商户`, "2026-08-27T12:00:00+08:00"), "fact-filter-payment")
	p := confirmFactWithoutLinks(t, s, f.tenant, r, "fact-filter-payment")
	i := confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, invoiceWithItemsEnvelope(`FACT-%_\-NUMBER`), "fact-filter-invoice"), "fact-filter-invoice")
	confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, paymentEnvelopeAt("合成普通商户", "2026-08-28T12:00:00+08:00"), "fact-filter-other"), "fact-filter-other")
	page, err := facts.ListPayments(ctx, f.tenant, FactQueryInput{Limit: 20, Filter: domain.FactFilter{Query: `%_\`, DateFrom: "2026-08-27", DateTo: "2026-08-27"}})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != p.FactID {
		t.Fatalf("literal/date payment filter: %v", err)
	}
	invoicePage, err := facts.ListInvoices(ctx, f.tenant, FactQueryInput{Limit: 20, Filter: domain.FactFilter{Query: `%_\`}})
	if err != nil || len(invoicePage.Items) != 1 || invoicePage.Items[0].ItemCount != 2 || invoicePage.Items[0].Items != nil {
		t.Fatalf("invoice list shape/filter: %v", err)
	}
	a := allocationapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	aw, err := a.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Adjust(ctx, f.tenant, domain.DocumentPayment, p.FactID, allocationapp.AdjustmentInput{ExpectedPlanHash: aw.PlanHash, DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: i.FactID, AllocatedMinor: 1000}}, Reason: "合成筛选分配", IdempotencyKey: "fact-filter-link", RequestID: "fact-filter-link-request"}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []domain.DocumentType{domain.DocumentPayment, domain.DocumentInvoice} {
		for _, status := range []string{"unallocated", "partial", "allocated"} {
			input := FactQueryInput{Limit: 20, Filter: domain.FactFilter{AllocationStatus: status}}
			count := 0
			if kind == domain.DocumentPayment {
				p, err := facts.ListPayments(ctx, f.tenant, input)
				if err != nil {
					t.Fatal(err)
				}
				count = len(p.Items)
			} else {
				p, err := facts.ListInvoices(ctx, f.tenant, input)
				if err != nil {
					t.Fatal(err)
				}
				count = len(p.Items)
			}
			expected := 0
			if status == "partial" || kind == domain.DocumentPayment && status == "unallocated" {
				expected = 1
			}
			if count != expected {
				t.Fatal("allocation filter mismatch")
			}
		}
		id := p.FactID
		if kind == domain.DocumentInvoice {
			id = i.FactID
		}
		for _, role := range []domain.Role{domain.RoleOwner, domain.RoleFinance, domain.RoleViewer, domain.RoleReviewer} {
			tenant := f.tenant
			tenant.Role = role
			detail, err := facts.Detail(ctx, tenant, kind, id)
			if role == domain.RoleReviewer {
				if !errors.Is(err, domain.ErrForbidden) {
					t.Fatal("Reviewer saw formal details")
				}
				continue
			}
			if err != nil || len(detail.Links) != 1 {
				t.Fatalf("detail projection: %v", err)
			}
			if role == domain.RoleViewer {
				if detail.Source != nil {
					t.Fatal("Viewer received source identity")
				}
			} else if detail.Source == nil || detail.Source.OriginKind != "ai" {
				t.Fatal("genuine source missing")
			}
			if kind == domain.DocumentInvoice && len(detail.Invoice.Items) != 2 {
				t.Fatal("detail omitted current items")
			}
		}
		foreign := f.tenant
		foreign.TenantID = "synthetic-foreign"
		if _, err := facts.Detail(ctx, foreign, kind, id); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("cross tenant detail leak")
		}
	}
	first, err := facts.ListPayments(ctx, f.tenant, FactQueryInput{Limit: 1})
	if err != nil || first.NextCursor == "" {
		t.Fatal("test cursor missing")
	}
	if _, err := facts.ListPayments(ctx, f.tenant, FactQueryInput{Limit: 1, Cursor: first.NextCursor, Filter: domain.FactFilter{Query: "changed"}}); !hasRuleCode(err, "invalid_fact_cursor") {
		t.Fatal("cross-filter cursor accepted")
	}
	if _, err := facts.ListInvoices(ctx, f.tenant, FactQueryInput{Limit: 1, Cursor: first.NextCursor}); !hasRuleCode(err, "invalid_fact_cursor") {
		t.Fatal("cross-type cursor accepted")
	}
	foreign := f.tenant
	foreign.TenantID = "synthetic-foreign"
	if _, err := facts.ListPayments(ctx, foreign, FactQueryInput{Limit: 1, Cursor: first.NextCursor}); !hasRuleCode(err, "invalid_fact_cursor") {
		t.Fatal("cross-tenant cursor accepted")
	}
	aw, err = a.GetWorkspace(ctx, f.tenant, domain.DocumentPayment, p.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Adjust(ctx, f.tenant, domain.DocumentPayment, p.FactID, allocationapp.AdjustmentInput{ExpectedPlanHash: aw.PlanHash, DesiredAllocations: []domain.DesiredAllocation{{TargetFactID: i.FactID, AllocatedMinor: 12345}}, Reason: "合成全额分配", IdempotencyKey: "fact-filter-full", RequestID: "fact-filter-full-request"}); err != nil {
		t.Fatal(err)
	}
	full, err := facts.ListPayments(ctx, f.tenant, FactQueryInput{Limit: 20, Filter: domain.FactFilter{AllocationStatus: "allocated"}})
	if err != nil || len(full.Items) != 1 || full.Items[0].RemainingMinor != 0 || full.Items[0].AllocationStatus != "allocated" {
		t.Fatal("fully allocated payment filter mismatch")
	}
	fullInvoices, err := facts.ListInvoices(ctx, f.tenant, FactQueryInput{Limit: 20, Filter: domain.FactFilter{AllocationStatus: "allocated"}})
	if err != nil || len(fullInvoices.Items) != 1 || fullInvoices.Items[0].RemainingMinor != 0 {
		t.Fatal("fully allocated invoice filter mismatch")
	}
	zero := confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, paymentEnvelopeWithAmount(0), "fact-filter-zero"), "fact-filter-zero")
	unallocated, err := facts.ListPayments(ctx, f.tenant, FactQueryInput{Limit: 20, Filter: domain.FactFilter{AllocationStatus: "unallocated"}})
	if err != nil {
		t.Fatal(err)
	}
	zeroFound := false
	for _, item := range unallocated.Items {
		if item.ID == zero.FactID {
			zeroFound = item.AllocationStatus == "unallocated" && item.AmountMinor == 0
		}
	}
	if !zeroFound {
		t.Fatal("zero amount status differs between SQL and projection")
	}
	iw, err := s.GetCorrection(ctx, f.tenant, domain.DocumentInvoice, i.FactID)
	if err != nil {
		t.Fatal(err)
	}
	input := correctionInputFrom(iw)
	correctionField(t, &input, "seller_name", "合成最新销售方")
	corrected, _ := applyCorrection(t, s, f.tenant, domain.DocumentInvoice, i.FactID, input, "fact-detail-current-source")
	latest, err := facts.Detail(ctx, f.tenant, domain.DocumentInvoice, i.FactID)
	if err != nil || latest.Version != corrected.Version || latest.Source.ReviewDecisionID != corrected.ReviewDecisionID || latest.Invoice.SellerName != "合成最新销售方" || latest.Invoice.TotalMinor != 12345 || latest.Invoice.ItemCount != 2 || len(latest.Invoice.Items) != 2 {
		t.Fatalf("detail mixes current fields/source/items: %v", err)
	}
	for _, q := range []string{"最新销售方", "Example Buyer"} {
		page, err := facts.ListInvoices(ctx, f.tenant, FactQueryInput{Limit: 20, Filter: domain.FactFilter{Query: q}})
		if err != nil || len(page.Items) != 1 {
			t.Fatal("buyer/seller search failed")
		}
	}
}
