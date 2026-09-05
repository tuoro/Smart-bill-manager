package reviews

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type FactQueryInput struct {
	Filter domain.FactFilter
	Cursor string
	Limit  int
}

type FactListPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

type factCursor struct {
	Version   string             `json:"version"`
	ScopeHash string             `json:"scope_hash"`
	Key       domain.FactSortKey `json:"key"`
}

func prepareFactQuery(tenantID string, kind domain.DocumentType, input FactQueryInput) (ports.FactQuery, string, error) {
	query := ports.FactQuery{Limit: input.Limit}
	if input.Limit < 1 || input.Limit > 100 {
		return query, "", domain.ErrInvalidInput
	}
	var err error
	query.Filter, err = domain.CanonicalFactFilter(input.Filter)
	if err != nil {
		return query, "", err
	}
	encoded, err := json.Marshal(struct {
		Tenant string
		Kind   domain.DocumentType
		Filter domain.FactFilter
	}{tenantID, kind, query.Filter})
	if err != nil {
		return query, "", err
	}
	digest := sha256.Sum256(encoded)
	scope := hex.EncodeToString(digest[:])
	if input.Cursor == "" {
		return query, scope, nil
	}
	if len(input.Cursor) > 2048 {
		return query, "", invalidFactCursor()
	}
	payload, err := base64.RawURLEncoding.DecodeString(input.Cursor)
	if err != nil {
		return query, "", invalidFactCursor()
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor factCursor
	if err := decoder.Decode(&cursor); err != nil {
		return query, "", invalidFactCursor()
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return query, "", invalidFactCursor()
	}
	if cursor.Version != domain.FactQueryVersion || cursor.ScopeHash != scope || !cursor.Key.Valid() {
		return query, "", invalidFactCursor()
	}
	query.After = &cursor.Key
	return query, scope, nil
}

func factResultPage[T any](source ports.FactPage[T], scope string) (FactListPage[T], error) {
	result := FactListPage[T]{Items: source.Items}
	if result.Items == nil {
		result.Items = []T{}
	}
	if source.Next != nil {
		encoded, err := json.Marshal(factCursor{Version: domain.FactQueryVersion, ScopeHash: scope, Key: *source.Next})
		if err != nil {
			return result, err
		}
		result.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return result, nil
}

func invalidFactCursor() error {
	return domain.NewRuleError("invalid_fact_cursor", "分页游标不属于当前工作区、单据类型或筛选，请刷新首屏", domain.ErrInvalidInput)
}

func (s FactService) ListPayments(ctx context.Context, tenant domain.TenantContext, input FactQueryInput) (FactListPage[ports.Payment], error) {
	if err := tenant.Require(domain.CapabilityFactsRead); err != nil {
		return FactListPage[ports.Payment]{}, err
	}
	query, scope, err := prepareFactQuery(tenant.TenantID, domain.DocumentPayment, input)
	if err != nil {
		return FactListPage[ports.Payment]{}, err
	}
	page, err := s.facts.ReadPaymentPage(ctx, tenant.TenantID, query)
	if err != nil {
		return FactListPage[ports.Payment]{}, err
	}
	return factResultPage(page, scope)
}

func (s FactService) ListInvoices(ctx context.Context, tenant domain.TenantContext, input FactQueryInput) (FactListPage[ports.Invoice], error) {
	if err := tenant.Require(domain.CapabilityFactsRead); err != nil {
		return FactListPage[ports.Invoice]{}, err
	}
	query, scope, err := prepareFactQuery(tenant.TenantID, domain.DocumentInvoice, input)
	if err != nil {
		return FactListPage[ports.Invoice]{}, err
	}
	page, err := s.facts.ReadInvoicePage(ctx, tenant.TenantID, query)
	if err != nil {
		return FactListPage[ports.Invoice]{}, err
	}
	return factResultPage(page, scope)
}

func (s FactService) Detail(ctx context.Context, tenant domain.TenantContext, kind domain.DocumentType, id string) (ports.FactDetail, error) {
	if err := tenant.Require(domain.CapabilityFactsRead); err != nil {
		return ports.FactDetail{}, err
	}
	if id == "" || (kind != domain.DocumentPayment && kind != domain.DocumentInvoice) {
		return ports.FactDetail{}, domain.ErrInvalidInput
	}
	return s.facts.ReadFactDetail(ctx, tenant.TenantID, kind, id, tenant.Role.Allows(domain.CapabilityReviewSourceRead))
}
