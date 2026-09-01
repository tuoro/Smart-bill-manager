package ports

import (
	"context"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type InsightRepository interface {
	ReadInsightPage(
		ctx context.Context,
		tenantID string,
		filter domain.InsightFilter,
		after *domain.InsightSortKey,
		limit int,
	) (domain.InsightPage, error)
}
