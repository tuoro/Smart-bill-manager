package ports

import (
	"context"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type FactQuery struct {
	Filter domain.FactFilter
	After  *domain.FactSortKey
	Limit  int
}

type FactPage[T any] struct {
	Items []T
	Next  *domain.FactSortKey
}

type FactSource struct {
	DocumentID       string `json:"document_id"`
	ClaimSetID       string `json:"claim_set_id"`
	ReviewDecisionID string `json:"review_decision_id"`
	Revision         int    `json:"revision"`
	OriginKind       string `json:"origin_kind"`
	OriginalName     string `json:"original_name"`
	PageCount        int    `json:"page_count"`
}

type FactDetail struct {
	FactType domain.DocumentType     `json:"fact_type"`
	Version  int                     `json:"version"`
	Payment  *Payment                `json:"payment,omitempty"`
	Invoice  *Invoice                `json:"invoice,omitempty"`
	Links    []domain.CorrectionLink `json:"links"`
	Trip     *domain.InsightTrip     `json:"trip,omitempty"`
	Source   *FactSource             `json:"source,omitempty"`
}

type FactRepository interface {
	ReadPaymentPage(ctx context.Context, tenantID string, query FactQuery) (FactPage[Payment], error)
	ReadInvoicePage(ctx context.Context, tenantID string, query FactQuery) (FactPage[Invoice], error)
	ReadFactDetail(ctx context.Context, tenantID string, kind domain.DocumentType, id string, includeSource bool) (FactDetail, error)
}
