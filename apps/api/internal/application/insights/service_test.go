package insights

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type insightRepositoryFixture struct {
	facts    []domain.InsightFact
	tenantID string
	filter   domain.InsightFilter
	err      error
}

func (f *insightRepositoryFixture) ReadInsightPage(
	_ context.Context,
	tenantID string,
	filter domain.InsightFilter,
	after *domain.InsightSortKey,
	limit int,
) (domain.InsightPage, error) {
	f.tenantID = tenantID
	f.filter = filter
	if f.err != nil {
		return domain.InsightPage{}, f.err
	}
	return domain.BuildInsightPage(filter, f.facts, after, limit)
}

func TestServiceQueryPaginatesAndBindsCursorToFilter(t *testing.T) {
	t.Parallel()

	repository := &insightRepositoryFixture{facts: []domain.InsightFact{
		{FactType: domain.DocumentPayment, FactID: "11111111-1111-4111-8111-111111111111", BusinessDate: "2026-08-03", DisplayName: "合成商户", AmountMinor: 100, Currency: domain.CurrencyCNY},
		{FactType: domain.DocumentInvoice, FactID: "22222222-2222-4222-8222-222222222222", BusinessDate: "2026-08-02", DisplayName: "合成销售方", AmountMinor: 80, Currency: domain.CurrencyCNY},
	}}
	service := NewService(repository)
	tenant := domain.TenantContext{TenantID: "tenant-a", UserID: "user-a", Role: domain.RoleViewer}
	first, err := service.Query(context.Background(), tenant, QueryInput{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.RuleVersion != domain.InsightRuleVersion || first.Filter.FactType != domain.InsightFactTypeAll ||
		len(first.Items) != 1 || first.Items[0].FactType != domain.DocumentPayment || first.NextCursor == "" {
		t.Fatalf("first page = %#v", first)
	}
	if repository.tenantID != tenant.TenantID || repository.filter.TripScope != domain.InsightTripScopeAll {
		t.Fatalf("repository call = %q / %#v", repository.tenantID, repository.filter)
	}
	second, err := service.Query(context.Background(), tenant, QueryInput{Cursor: first.NextCursor, Limit: 1})
	if err != nil || len(second.Items) != 1 || second.Items[0].FactType != domain.DocumentInvoice || second.NextCursor != "" {
		t.Fatalf("second page = %#v / %v", second, err)
	}
	_, err = service.Query(context.Background(), tenant, QueryInput{
		Filter: domain.InsightFilter{Currency: domain.CurrencyUSD}, Cursor: first.NextCursor, Limit: 1,
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("mismatched cursor error = %v", err)
	}
}

func TestServiceQueryRejectsMalformedInputsAndEnforcesCapability(t *testing.T) {
	t.Parallel()

	service := NewService(&insightRepositoryFixture{})
	viewer := domain.TenantContext{TenantID: "tenant", UserID: "viewer", Role: domain.RoleViewer}
	reviewer := domain.TenantContext{TenantID: "tenant", UserID: "reviewer", Role: domain.RoleReviewer}
	if _, err := service.Query(context.Background(), reviewer, QueryInput{Limit: 50}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("reviewer error = %v", err)
	}
	for name, input := range map[string]QueryInput{
		"zero limit":      {Limit: 0},
		"large limit":     {Limit: 101},
		"bad trip id":     {Filter: domain.InsightFilter{TripScope: domain.InsightTripAssigned, TripID: "trip"}, Limit: 50},
		"bad base64":      {Cursor: "%%%", Limit: 50},
		"unknown cursor":  {Cursor: base64.RawURLEncoding.EncodeToString([]byte(`{"version":"fact-insight-cursor/1","filter_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","business_date":"2026-08-01","fact_type":"payment","fact_id":"11111111-1111-4111-8111-111111111111","extra":true}`)), Limit: 50},
		"trailing cursor": {Cursor: base64.RawURLEncoding.EncodeToString([]byte(`{} {}`)), Limit: 50},
	} {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := service.Query(context.Background(), viewer, input); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestServiceQueryPropagatesRepositoryFailureAndNormalizesEmptySlices(t *testing.T) {
	t.Parallel()

	tenant := domain.TenantContext{TenantID: "tenant", UserID: "owner", Role: domain.RoleOwner}
	repositoryError := errors.New("read failed")
	service := NewService(&insightRepositoryFixture{err: repositoryError})
	if _, err := service.Query(context.Background(), tenant, QueryInput{Limit: 50}); !errors.Is(err, repositoryError) {
		t.Fatalf("repository error = %v", err)
	}
	service = NewService(&insightRepositoryFixture{})
	page, err := service.Query(context.Background(), tenant, QueryInput{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items == nil || page.Groups == nil || len(page.Items) != 0 || len(page.Groups) != 0 {
		t.Fatalf("empty page = %#v", page)
	}
}
