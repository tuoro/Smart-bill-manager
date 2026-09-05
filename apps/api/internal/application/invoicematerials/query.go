package invoicematerials

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

type CandidateQuery struct {
	Query, Cursor string
	Limit         int
}
type CandidatePage struct {
	Items      []ports.InvoiceMaterial `json:"items"`
	NextCursor string                  `json:"next_cursor"`
}
type materialCursor struct {
	Version string             `json:"version"`
	Scope   string             `json:"scope"`
	Key     domain.FactSortKey `json:"key"`
}

func (s Service) Candidates(ctx context.Context, tenant domain.TenantContext, invoiceID string, input CandidateQuery) (CandidatePage, error) {
	var result CandidatePage
	if err := domain.RequireInvoiceMaterials(tenant); err != nil {
		return result, err
	}
	filter, err := domain.CanonicalFactFilter(domain.FactFilter{Query: input.Query})
	if err != nil || invoiceID == "" || input.Limit < 1 || input.Limit > 100 {
		return result, domain.ErrInvalidInput
	}
	encoded, err := json.Marshal([]string{tenant.TenantID, invoiceID, filter.Query})
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(encoded)
	scope := hex.EncodeToString(digest[:])
	query := ports.MaterialDocumentQuery{Query: filter.Query, Limit: input.Limit}
	if input.Cursor != "" {
		if len(input.Cursor) > 2048 {
			return result, domain.ErrInvalidInput
		}
		payload, err := base64.RawURLEncoding.DecodeString(input.Cursor)
		if err != nil {
			return result, domain.ErrInvalidInput
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		var cursor materialCursor
		if err := decoder.Decode(&cursor); err != nil {
			return result, domain.ErrInvalidInput
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || cursor.Version != domain.InvoiceMaterialQueryVersion || cursor.Scope != scope || !cursor.Key.Valid() {
			return result, domain.ErrInvalidInput
		}
		query.After = &cursor.Key
	}
	page, err := s.repository.ListMaterialDocuments(ctx, tenant.TenantID, invoiceID, query)
	if err != nil {
		return result, err
	}
	result.Items = page.Items
	if result.Items == nil {
		result.Items = []ports.InvoiceMaterial{}
	}
	if page.Next != nil {
		payload, err := json.Marshal(materialCursor{Version: domain.InvoiceMaterialQueryVersion, Scope: scope, Key: *page.Next})
		if err != nil {
			return result, err
		}
		result.NextCursor = base64.RawURLEncoding.EncodeToString(payload)
	}
	return result, nil
}
