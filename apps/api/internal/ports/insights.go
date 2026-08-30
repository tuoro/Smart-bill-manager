package ports

import (
	"context"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type InsightRepository interface {
	ReadInsightFacts(
		ctx context.Context,
		tenantID string,
		filter domain.InsightFilter,
	) ([]domain.InsightFact, error)
}
