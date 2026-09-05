package allocations

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type TargetSearchInput struct{ Query, View, Cursor string }

func targetScope(tenantID string, kind domain.DocumentType, id, query, view string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{"allocation-targets/1", tenantID, string(kind), id, query, view}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func encodeTargetCursor(id, scope string) string {
	if id == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(scope + ":" + id))
}

func (s Service) SearchTargets(ctx context.Context, tenant domain.TenantContext, kind domain.DocumentType, id string, input TargetSearchInput) (ports.AllocationTargetPage, error) {
	if err := tenant.Require(domain.CapabilityAllocationsManage); err != nil {
		return ports.AllocationTargetPage{}, err
	}
	if !validAnchor(kind, id) {
		return ports.AllocationTargetPage{}, domain.ErrInvalidInput
	}
	if input.View == "" {
		input.View = "recommended"
	}
	if input.View != "recommended" && input.View != "all_dates" {
		return ports.AllocationTargetPage{}, domain.ErrInvalidInput
	}
	filter, err := domain.CanonicalFactFilter(domain.FactFilter{Query: input.Query})
	if err != nil {
		return ports.AllocationTargetPage{}, err
	}
	scope := targetScope(tenant.TenantID, kind, id, filter.Query, input.View)
	query := ports.AllocationTargetQuery{Query: filter.Query, AllDates: input.View == "all_dates"}
	if input.Cursor != "" {
		if len(input.Cursor) > 512 {
			return ports.AllocationTargetPage{}, domain.ErrInvalidInput
		}
		raw, err := base64.RawURLEncoding.DecodeString(input.Cursor)
		if err != nil || !strings.HasPrefix(string(raw), scope+":") || len(raw) <= 65 || len(raw) > 265 {
			return ports.AllocationTargetPage{}, domain.NewRuleError("invalid_allocation_cursor", "分页范围已变化，请重新搜索", domain.ErrInvalidInput)
		}
		query.AfterID = string(raw[65:])
		if !utf8.ValidString(query.AfterID) || strings.IndexFunc(query.AfterID, unicode.IsControl) >= 0 {
			return ports.AllocationTargetPage{}, domain.ErrInvalidInput
		}
	}
	page, err := s.repository.SearchAllocationTargets(ctx, tenant.TenantID, kind, id, query)
	if err != nil {
		return page, err
	}
	page.NextCursor = encodeTargetCursor(page.NextCursor, scope)
	return page, nil
}
