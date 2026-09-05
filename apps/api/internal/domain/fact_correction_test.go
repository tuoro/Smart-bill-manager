package domain

import (
	"reflect"
	"strings"
	"testing"
)

func correctionLinks() []CorrectionLink {
	return []CorrectionLink{{ID: "link", TargetID: "invoice", AllocatedMinor: 600, Currency: "CNY", TargetCurrency: "CNY", TargetBusinessDate: "2026-09-01", TargetAmountMinor: 1000, TargetAllocatedMinor: 600, TargetAvailable: true, TargetVersion: 1}}
}

func TestCorrectionKeepsValidLinksAndRequiresExplicitWithdrawal(t *testing.T) {
	for _, date := range []string{"2026-08-02", "2026-09-01", "2026-10-01", "2026-08-01", "2026-10-02", "2030-01-01"} {
		issues, err := ValidateCorrectionLinks(Money{MinorUnits: 600, Currency: CurrencyCNY}, date, correctionLinks(), nil)
		if err != nil || len(issues) != 0 {
			t.Fatalf("valid boundary rejected: %v", err)
		}
	}
	for _, test := range []struct {
		amount     int64
		currency   Currency
		date, code string
	}{
		{599, CurrencyCNY, "2026-09-01", "correction_overallocated"},
		{600, CurrencyUSD, "2026-09-01", "correction_currency_conflict"},
	} {
		t.Run(test.code+test.date, func(t *testing.T) {
			links := correctionLinks()
			before := append([]CorrectionLink(nil), links...)
			issues, err := ValidateCorrectionLinks(Money{MinorUnits: test.amount, Currency: test.currency}, test.date, links, nil)
			if err != nil || len(issues) != 1 || issues[0].Code != test.code || issues[0].LinkID != "link" {
				t.Fatalf("expected explicit conflict: %v, %v", issues, err)
			}
			issues, err = ValidateCorrectionLinks(Money{MinorUnits: test.amount, Currency: test.currency}, test.date, links, []string{"link"})
			if err != nil || len(issues) != 0 || !reflect.DeepEqual(before, links) {
				t.Fatal("explicit withdrawal failed or mutated current links")
			}
		})
	}
}

func TestCorrectionRejectsInvalidWithdrawalsAndAggregateOverflow(t *testing.T) {
	for _, ids := range [][]string{{"missing"}, {"link", "link"}, {""}} {
		if _, err := CanonicalCorrectionWithdrawals(correctionLinks(), ids); err == nil {
			t.Fatal("invalid withdrawal accepted")
		}
	}
	links := correctionLinks()
	links = append(links, links[0])
	if _, err := CanonicalCorrectionWithdrawals(links, nil); err == nil {
		t.Fatal("duplicate database link accepted")
	}
	links[1].ID = "second"
	for index := range links {
		links[index].AllocatedMinor = MaxSafeMinorUnits
		links[index].TargetAmountMinor = MaxSafeMinorUnits
		links[index].TargetAllocatedMinor = MaxSafeMinorUnits
	}
	issues, err := ValidateCorrectionLinks(Money{MinorUnits: MaxSafeMinorUnits, Currency: CurrencyCNY}, "2026-09-01", links, nil)
	if err != nil || len(issues) != 1 || issues[0].Code != "correction_overallocated" {
		t.Fatal("sum overflow was not blocked")
	}
}

func TestCorrectionValidatesReasonAndCurrentTargetIntegrity(t *testing.T) {
	for _, reason := range []string{"", " \n", strings.Repeat("字", 501)} {
		if _, err := CorrectionReason(reason); err == nil {
			t.Fatal("invalid reason accepted")
		}
	}
	if value, err := CorrectionReason("  合成纠错理由  "); err != nil || value != "合成纠错理由" {
		t.Fatal("reason normalization failed")
	}
	links := correctionLinks()
	links[0].TargetAvailable = false
	issues, err := ValidateCorrectionLinks(Money{MinorUnits: 600, Currency: CurrencyCNY}, "2026-09-01", links, nil)
	if err != nil || len(issues) != 1 || issues[0].Code != "correction_target_unavailable" {
		t.Fatal("unavailable target accepted")
	}
	links[0].TargetBusinessDate = "invalid"
	if _, err := ValidateCorrectionLinks(Money{MinorUnits: 600, Currency: CurrencyCNY}, "2026-09-01", links, nil); err == nil {
		t.Fatal("invalid target date accepted")
	}
	links = correctionLinks()
	links[0].TargetAllocatedMinor = 0
	if _, err := ValidateCorrectionLinks(Money{MinorUnits: 600, Currency: CurrencyCNY}, "2026-09-01", links, nil); err == nil {
		t.Fatal("invalid target balance accepted")
	}
	if _, err := ValidateCorrectionLinks(Money{MinorUnits: -1, Currency: CurrencyCNY}, "2026-09-01", nil, nil); err == nil {
		t.Fatal("negative amount accepted")
	}
	if _, err := ValidateCorrectionLinks(Money{MinorUnits: 0, Currency: CurrencyCNY}, "invalid", nil, nil); err == nil {
		t.Fatal("invalid date accepted")
	}
}
