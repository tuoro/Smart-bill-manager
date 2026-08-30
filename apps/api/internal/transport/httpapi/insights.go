package httpapi

import (
	"net/http"
	"strconv"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/insights"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

var insightQueryParameters = map[string]struct{}{
	"fact_type":         {},
	"date_from":         {},
	"date_to":           {},
	"currency":          {},
	"allocation_status": {},
	"trip_scope":        {},
	"trip_id":           {},
	"cursor":            {},
	"limit":             {},
}

func (s *Server) queryInsightsHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	query := request.URL.Query()
	for key, values := range query {
		_, allowed := insightQueryParameters[key]
		if !allowed || len(values) != 1 || values[0] == "" {
			writeError(response, request, invalidInsightQuery())
			return
		}
	}
	limit := 50
	if raw, exists := query["limit"]; exists {
		parsed, err := strconv.Atoi(raw[0])
		if err != nil {
			writeError(response, request, domain.NewRuleError("invalid_insight_limit", "洞察分页数量必须为 1–100", domain.ErrInvalidInput))
			return
		}
		limit = parsed
	}
	tripID := query.Get("trip_id")
	if tripID != "" && !validUUIDString(tripID) {
		writeError(response, request, domain.NewRuleError("invalid_insight_trip_filter", "洞察行程 ID 格式不正确", domain.ErrInvalidInput))
		return
	}
	page, err := s.insights.Query(request.Context(), tenantContext(principal), insights.QueryInput{
		Filter: domain.InsightFilter{
			FactType:         query.Get("fact_type"),
			DateFrom:         query.Get("date_from"),
			DateTo:           query.Get("date_to"),
			Currency:         domain.Currency(query.Get("currency")),
			AllocationStatus: query.Get("allocation_status"),
			TripScope:        query.Get("trip_scope"),
			TripID:           tripID,
		},
		Cursor: query.Get("cursor"),
		Limit:  limit,
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func invalidInsightQuery() error {
	return domain.NewRuleError("invalid_insight_query", "洞察查询参数不合法或重复", domain.ErrInvalidInput)
}
