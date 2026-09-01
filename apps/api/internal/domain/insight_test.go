package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestCanonicalInsightFilterDefaultsAndIdentity(t *testing.T) {
	t.Parallel()

	input := InsightFilter{}
	canonical, firstHash, err := CanonicalInsightFilter(input)
	if err != nil {
		t.Fatal(err)
	}
	want := InsightFilter{
		FactType:         InsightFactTypeAll,
		AllocationStatus: InsightStatusAll,
		TripScope:        InsightTripScopeAll,
	}
	if !reflect.DeepEqual(canonical, want) || !ValidSHA256Hex(firstHash) {
		t.Fatalf("canonical = %#v / %q", canonical, firstHash)
	}
	if !reflect.DeepEqual(input, InsightFilter{}) {
		t.Fatalf("input mutated: %#v", input)
	}
	second, secondHash, err := CanonicalInsightFilter(want)
	if err != nil || !reflect.DeepEqual(second, want) || secondHash != firstHash {
		t.Fatalf("explicit defaults changed identity: %#v / %q / %v", second, secondHash, err)
	}

	for name, filter := range map[string]InsightFilter{
		"fact type":        {FactType: "trip"},
		"one date":         {DateFrom: "2026-08-01"},
		"invalid date":     {DateFrom: "2026-02-30", DateTo: "2026-03-01"},
		"reversed dates":   {DateFrom: "2026-08-02", DateTo: "2026-08-01"},
		"currency":         {Currency: "GBP"},
		"status":           {AllocationStatus: "unknown"},
		"trip scope":       {TripScope: "current"},
		"trip combination": {TripScope: InsightTripUnassigned, TripID: "trip"},
		"padded":           {FactType: " all"},
	} {
		filter := filter
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := CanonicalInsightFilter(filter); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestBuildInsightPageFiltersAggregatesAndPaginatesWithoutMutation(t *testing.T) {
	t.Parallel()

	trip := &InsightTrip{ID: "trip-a", Destination: "合成行程", StartDate: "2026-08-01", EndDate: "2026-08-03"}
	facts := []InsightFact{
		{FactType: DocumentInvoice, FactID: "invoice-a", BusinessDate: "2026-08-03", DisplayName: "合成销售方", AmountMinor: 100, AllocatedMinor: 40, Currency: CurrencyCNY, Trip: trip},
		{FactType: DocumentPayment, FactID: "payment-b", BusinessDate: "2026-08-03", DisplayName: "合成商户乙", AmountMinor: 200, AllocatedMinor: 200, Currency: CurrencyCNY, Trip: trip},
		{FactType: DocumentPayment, FactID: "payment-a", BusinessDate: "2026-08-02", DisplayName: "合成商户甲", AmountMinor: 300, Currency: CurrencyUSD},
		{FactType: DocumentInvoice, FactID: "invoice-old", BusinessDate: "2026-07-01", DisplayName: "范围外销售方", AmountMinor: 50, Currency: CurrencyCNY},
	}
	original := []InsightFact{
		facts[0], facts[1], facts[2], facts[3],
	}
	filter := InsightFilter{DateFrom: "2026-08-01", DateTo: "2026-08-31"}
	first, err := BuildInsightPage(filter, facts, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(first.Groups), 3; got != want {
		t.Fatalf("groups = %#v", first.Groups)
	}
	if first.Groups[0].Currency != CurrencyCNY || first.Groups[0].FactType != DocumentPayment ||
		first.Groups[0].Count != 1 || first.Groups[0].TotalMinor != 200 ||
		first.Groups[0].AllocatedMinor != 200 || first.Groups[0].AllocatedCount != 1 {
		t.Fatalf("payment group = %#v", first.Groups[0])
	}
	if first.Groups[1].Currency != CurrencyCNY || first.Groups[1].FactType != DocumentInvoice ||
		first.Groups[1].PartialCount != 1 || first.Groups[1].RemainingMinor != 60 {
		t.Fatalf("invoice group = %#v", first.Groups[1])
	}
	if first.Groups[2].Currency != CurrencyUSD || first.Groups[2].UnallocatedCount != 1 {
		t.Fatalf("USD group = %#v", first.Groups[2])
	}
	if len(first.Items) != 2 || first.Items[0].FactID != "payment-b" || first.Items[1].FactID != "invoice-a" || first.Next == nil {
		t.Fatalf("first page = %#v", first)
	}
	second, err := BuildInsightPage(filter, facts, first.Next, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].FactID != "payment-a" || second.Next != nil {
		t.Fatalf("second page = %#v", second)
	}
	if second.Items[0].AllocationStatus != InsightStatusNone || second.Items[0].RemainingMinor != 300 {
		t.Fatalf("normalized item = %#v", second.Items[0])
	}
	if !reflect.DeepEqual(facts, original) || facts[0].Trip != trip {
		t.Fatalf("input mutated: %#v", facts)
	}

	assignedCNY, err := BuildInsightPage(InsightFilter{
		Currency: CurrencyCNY, AllocationStatus: InsightStatusPartial,
		TripScope: InsightTripAssigned, TripID: "trip-a",
	}, facts, nil, 100)
	if err != nil || len(assignedCNY.Items) != 1 || assignedCNY.Items[0].FactID != "invoice-a" {
		t.Fatalf("filtered page = %#v / %v", assignedCNY, err)
	}
}

func TestBuildInsightPageRejectsInvalidProjectionCursorAndOverflow(t *testing.T) {
	t.Parallel()

	base := InsightFact{
		FactType: DocumentPayment, FactID: "payment-a", BusinessDate: "2026-08-01",
		DisplayName: "合成商户", AmountMinor: 100, Currency: CurrencyCNY,
	}
	for name, facts := range map[string][]InsightFact{
		"invalid type":       {func() InsightFact { item := base; item.FactType = DocumentTrip; return item }()},
		"invalid date":       {func() InsightFact { item := base; item.BusinessDate = "bad"; return item }()},
		"over allocated":     {func() InsightFact { item := base; item.AllocatedMinor = 101; return item }()},
		"duplicate identity": {base, base},
		"invalid trip": {func() InsightFact {
			item := base
			item.Trip = &InsightTrip{ID: "trip", Destination: "合成", StartDate: "2026-08-02", EndDate: "2026-08-01"}
			return item
		}()},
	} {
		facts := facts
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := BuildInsightPage(InsightFilter{}, facts, nil, 50); !errors.Is(err, ErrConflict) {
				t.Fatalf("error = %v", err)
			}
		})
	}

	if _, err := BuildInsightPage(InsightFilter{}, []InsightFact{base}, &InsightSortKey{
		BusinessDate: "2026-08-01", FactType: DocumentPayment, FactID: "missing",
	}, 50); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing cursor error = %v", err)
	}

	maximum := base
	maximum.AmountMinor = MaxSafeMinorUnits
	second := base
	second.FactID = "payment-b"
	second.AmountMinor = 1
	if _, err := BuildInsightPage(InsightFilter{}, []InsightFact{maximum, second}, nil, 50); !errors.Is(err, ErrConflict) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestBuildProjectedInsightPageValidatesDatabaseProjection(t *testing.T) {
	t.Parallel()

	groups := []InsightAggregate{
		{
			Currency: CurrencyCNY, FactType: DocumentPayment, Count: 2,
			TotalMinor: 300, AllocatedMinor: 100, RemainingMinor: 200,
			UnallocatedCount: 1, PartialCount: 1,
		},
		{
			Currency: CurrencyCNY, FactType: DocumentInvoice, Count: 1,
			TotalMinor: 80, RemainingMinor: 80, UnallocatedCount: 1,
		},
	}
	items := []InsightFact{
		{FactType: DocumentPayment, FactID: "payment-b", BusinessDate: "2026-08-03", DisplayName: "合成商户乙", AmountMinor: 100, AllocatedMinor: 40, Currency: CurrencyCNY},
		{FactType: DocumentInvoice, FactID: "invoice-a", BusinessDate: "2026-08-03", DisplayName: "合成销售方", AmountMinor: 80, Currency: CurrencyCNY},
	}
	page, err := BuildProjectedInsightPage(InsightFilter{}, groups, items, nil, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Groups) != 2 || len(page.Items) != 2 || page.Next == nil ||
		page.Next.FactID != "invoice-a" || page.Items[0].RemainingMinor != 60 ||
		page.Items[0].AllocationStatus != InsightStatusPartial {
		t.Fatalf("projected page = %#v", page)
	}

	badGroups := append([]InsightAggregate(nil), groups...)
	badGroups[0].RemainingMinor++
	if _, err := BuildProjectedInsightPage(InsightFilter{}, badGroups, items, nil, 2, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid aggregate error = %v", err)
	}
	reversed := []InsightFact{items[1], items[0]}
	if _, err := BuildProjectedInsightPage(InsightFilter{}, groups, reversed, nil, 2, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("unsorted items error = %v", err)
	}
	missingGroup := []InsightFact{{FactType: DocumentPayment, FactID: "payment-usd", BusinessDate: "2026-08-01", DisplayName: "合成商户", AmountMinor: 1, Currency: CurrencyUSD}}
	if _, err := BuildProjectedInsightPage(InsightFilter{}, groups, missingGroup, nil, 2, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing group error = %v", err)
	}
}
