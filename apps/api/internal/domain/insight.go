package domain

import (
	"sort"
	"strings"
	"time"
)

const (
	InsightRuleVersion    = "fact-insights/1"
	InsightFactTypeAll    = "all"
	InsightStatusAll      = "all"
	InsightStatusNone     = "unallocated"
	InsightStatusPartial  = "partial"
	InsightStatusFull     = "allocated"
	InsightTripScopeAll   = "all"
	InsightTripAssigned   = "assigned"
	InsightTripUnassigned = "unassigned"
)

type InsightFilter struct {
	FactType         string   `json:"fact_type"`
	DateFrom         string   `json:"date_from,omitempty"`
	DateTo           string   `json:"date_to,omitempty"`
	Currency         Currency `json:"currency,omitempty"`
	AllocationStatus string   `json:"allocation_status"`
	TripScope        string   `json:"trip_scope"`
	TripID           string   `json:"trip_id,omitempty"`
}

type InsightTrip struct {
	ID          string `json:"id"`
	Destination string `json:"destination"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
}

type InsightFact struct {
	FactType         DocumentType `json:"fact_type"`
	FactID           string       `json:"fact_id"`
	BusinessDate     string       `json:"business_date"`
	DisplayName      string       `json:"display_name"`
	AmountMinor      int64        `json:"amount_minor"`
	AllocatedMinor   int64        `json:"allocated_minor"`
	RemainingMinor   int64        `json:"remaining_minor"`
	Currency         Currency     `json:"currency"`
	AllocationStatus string       `json:"allocation_status"`
	Trip             *InsightTrip `json:"trip,omitempty"`
}

type InsightAggregate struct {
	Currency         Currency     `json:"currency"`
	FactType         DocumentType `json:"fact_type"`
	Count            int64        `json:"count"`
	TotalMinor       int64        `json:"total_minor"`
	AllocatedMinor   int64        `json:"allocated_minor"`
	RemainingMinor   int64        `json:"remaining_minor"`
	UnallocatedCount int64        `json:"unallocated_count"`
	PartialCount     int64        `json:"partial_count"`
	AllocatedCount   int64        `json:"allocated_count"`
}

type InsightSortKey struct {
	BusinessDate string
	FactType     DocumentType
	FactID       string
}

type InsightPage struct {
	Groups []InsightAggregate
	Items  []InsightFact
	Next   *InsightSortKey
}

func CanonicalInsightFilter(input InsightFilter) (InsightFilter, string, error) {
	canonical := input
	if canonical.FactType == "" {
		canonical.FactType = InsightFactTypeAll
	}
	if canonical.AllocationStatus == "" {
		canonical.AllocationStatus = InsightStatusAll
	}
	if canonical.TripScope == "" {
		canonical.TripScope = InsightTripScopeAll
	}
	for _, value := range []string{
		canonical.FactType,
		canonical.DateFrom,
		canonical.DateTo,
		string(canonical.Currency),
		canonical.AllocationStatus,
		canonical.TripScope,
		canonical.TripID,
	} {
		if strings.TrimSpace(value) != value {
			return InsightFilter{}, "", invalidInsightFilter()
		}
	}
	if canonical.FactType != InsightFactTypeAll &&
		canonical.FactType != string(DocumentPayment) &&
		canonical.FactType != string(DocumentInvoice) {
		return InsightFilter{}, "", invalidInsightFilter()
	}
	if (canonical.DateFrom == "") != (canonical.DateTo == "") {
		return InsightFilter{}, "", NewRuleError("invalid_insight_date_range", "洞察起止日期必须同时提供", ErrInvalidInput)
	}
	if canonical.DateFrom != "" {
		from, fromErr := parseInsightDate(canonical.DateFrom)
		to, toErr := parseInsightDate(canonical.DateTo)
		if fromErr != nil || toErr != nil || from.After(to) {
			return InsightFilter{}, "", NewRuleError("invalid_insight_date_range", "洞察日期范围不合法", ErrInvalidInput)
		}
	}
	if canonical.Currency != "" {
		if _, ok := canonical.Currency.Exponent(); !ok {
			return InsightFilter{}, "", NewRuleError("unsupported_currency", "仅支持 CNY、USD、EUR 和 JPY", ErrInvalidInput)
		}
	}
	if canonical.AllocationStatus != InsightStatusAll &&
		canonical.AllocationStatus != InsightStatusNone &&
		canonical.AllocationStatus != InsightStatusPartial &&
		canonical.AllocationStatus != InsightStatusFull {
		return InsightFilter{}, "", invalidInsightFilter()
	}
	if canonical.TripScope != InsightTripScopeAll &&
		canonical.TripScope != InsightTripAssigned &&
		canonical.TripScope != InsightTripUnassigned {
		return InsightFilter{}, "", invalidInsightFilter()
	}
	if canonical.TripID != "" && canonical.TripScope != InsightTripAssigned {
		return InsightFilter{}, "", NewRuleError("invalid_insight_trip_filter", "指定行程时必须选择已归属范围", ErrInvalidInput)
	}
	payload := struct {
		Version string        `json:"version"`
		Filter  InsightFilter `json:"filter"`
	}{InsightRuleVersion, canonical}
	digest, err := hashJSON(payload)
	if err != nil {
		return InsightFilter{}, "", err
	}
	return canonical, digest, nil
}

func BuildInsightPage(
	filter InsightFilter,
	facts []InsightFact,
	after *InsightSortKey,
	limit int,
) (InsightPage, error) {
	canonical, _, err := CanonicalInsightFilter(filter)
	if err != nil {
		return InsightPage{}, err
	}
	if limit < 1 || limit > 100 {
		return InsightPage{}, NewRuleError("invalid_insight_limit", "洞察分页数量必须为 1–100", ErrInvalidInput)
	}
	filtered := make([]InsightFact, 0, len(facts))
	seenFacts := make(map[string]struct{}, len(facts))
	for _, source := range facts {
		item := cloneInsightFact(source)
		if err := normalizeInsightFact(&item); err != nil {
			return InsightPage{}, err
		}
		if insightFactMatches(canonical, item) {
			identity := string(item.FactType) + "\x00" + item.FactID
			if _, duplicate := seenFacts[identity]; duplicate {
				return InsightPage{}, invalidInsightProjection()
			}
			seenFacts[identity] = struct{}{}
			filtered = append(filtered, item)
		}
	}
	sort.Slice(filtered, func(left, right int) bool {
		return insightFactBefore(filtered[left], filtered[right])
	})
	groups, err := aggregateInsightFacts(filtered)
	if err != nil {
		return InsightPage{}, err
	}
	start := 0
	if after != nil {
		if err := validateInsightSortKey(*after); err != nil {
			return InsightPage{}, err
		}
		start = -1
		for index, item := range filtered {
			if item.BusinessDate == after.BusinessDate && item.FactType == after.FactType && item.FactID == after.FactID {
				start = index + 1
				break
			}
		}
		if start < 0 {
			return InsightPage{}, NewRuleError("invalid_insight_cursor", "洞察游标已不属于当前筛选结果", ErrInvalidInput)
		}
	}
	end := min(start+limit, len(filtered))
	items := append([]InsightFact(nil), filtered[start:end]...)
	result := InsightPage{Groups: groups, Items: items}
	if end < len(filtered) && len(items) > 0 {
		last := items[len(items)-1]
		result.Next = &InsightSortKey{BusinessDate: last.BusinessDate, FactType: last.FactType, FactID: last.FactID}
	}
	return result, nil
}

func normalizeInsightFact(item *InsightFact) error {
	if item.FactType != DocumentPayment && item.FactType != DocumentInvoice {
		return invalidInsightProjection()
	}
	if item.FactID == "" || strings.TrimSpace(item.FactID) != item.FactID ||
		item.DisplayName == "" || strings.TrimSpace(item.DisplayName) != item.DisplayName {
		return invalidInsightProjection()
	}
	if _, err := parseInsightDate(item.BusinessDate); err != nil {
		return invalidInsightProjection()
	}
	if item.AmountMinor < 0 || item.AmountMinor > MaxSafeMinorUnits ||
		item.AllocatedMinor < 0 || item.AllocatedMinor > item.AmountMinor {
		return invalidInsightProjection()
	}
	if _, ok := item.Currency.Exponent(); !ok {
		return invalidInsightProjection()
	}
	item.RemainingMinor = item.AmountMinor - item.AllocatedMinor
	switch {
	case item.AllocatedMinor == 0:
		item.AllocationStatus = InsightStatusNone
	case item.AllocatedMinor == item.AmountMinor:
		item.AllocationStatus = InsightStatusFull
	default:
		item.AllocationStatus = InsightStatusPartial
	}
	if item.Trip != nil {
		if item.Trip.ID == "" || strings.TrimSpace(item.Trip.ID) != item.Trip.ID ||
			item.Trip.Destination == "" || strings.TrimSpace(item.Trip.Destination) != item.Trip.Destination {
			return invalidInsightProjection()
		}
		start, startErr := parseInsightDate(item.Trip.StartDate)
		end, endErr := parseInsightDate(item.Trip.EndDate)
		if startErr != nil || endErr != nil || start.After(end) {
			return invalidInsightProjection()
		}
	}
	return nil
}

func insightFactMatches(filter InsightFilter, item InsightFact) bool {
	if filter.FactType != InsightFactTypeAll && filter.FactType != string(item.FactType) {
		return false
	}
	if filter.DateFrom != "" && (item.BusinessDate < filter.DateFrom || item.BusinessDate > filter.DateTo) {
		return false
	}
	if filter.Currency != "" && item.Currency != filter.Currency {
		return false
	}
	if filter.AllocationStatus != InsightStatusAll && item.AllocationStatus != filter.AllocationStatus {
		return false
	}
	if filter.TripScope == InsightTripAssigned && item.Trip == nil {
		return false
	}
	if filter.TripScope == InsightTripUnassigned && item.Trip != nil {
		return false
	}
	return filter.TripID == "" || (item.Trip != nil && item.Trip.ID == filter.TripID)
}

func aggregateInsightFacts(facts []InsightFact) ([]InsightAggregate, error) {
	groups := make(map[string]InsightAggregate)
	for _, item := range facts {
		key := string(item.Currency) + "\x00" + string(item.FactType)
		group := groups[key]
		group.Currency = item.Currency
		group.FactType = item.FactType
		var err error
		if group.Count, err = addInsightAmount(group.Count, 1); err != nil {
			return nil, err
		}
		if group.TotalMinor, err = addInsightAmount(group.TotalMinor, item.AmountMinor); err != nil {
			return nil, err
		}
		if group.AllocatedMinor, err = addInsightAmount(group.AllocatedMinor, item.AllocatedMinor); err != nil {
			return nil, err
		}
		if group.RemainingMinor, err = addInsightAmount(group.RemainingMinor, item.RemainingMinor); err != nil {
			return nil, err
		}
		switch item.AllocationStatus {
		case InsightStatusNone:
			group.UnallocatedCount, err = addInsightAmount(group.UnallocatedCount, 1)
		case InsightStatusPartial:
			group.PartialCount, err = addInsightAmount(group.PartialCount, 1)
		case InsightStatusFull:
			group.AllocatedCount, err = addInsightAmount(group.AllocatedCount, 1)
		}
		if err != nil {
			return nil, err
		}
		groups[key] = group
	}
	result := make([]InsightAggregate, 0, len(groups))
	for _, group := range groups {
		result = append(result, group)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Currency != result[right].Currency {
			return result[left].Currency < result[right].Currency
		}
		return result[left].FactType > result[right].FactType
	})
	return result, nil
}

func addInsightAmount(current, addition int64) (int64, error) {
	if addition < 0 || current < 0 || addition > MaxSafeMinorUnits-current {
		return 0, NewRuleError("insight_amount_overflow", "洞察金额汇总超出安全整数范围", ErrConflict)
	}
	return current + addition, nil
}

func insightFactBefore(left, right InsightFact) bool {
	if left.BusinessDate != right.BusinessDate {
		return left.BusinessDate > right.BusinessDate
	}
	if left.FactType != right.FactType {
		return left.FactType > right.FactType
	}
	return left.FactID > right.FactID
}

func validateInsightSortKey(key InsightSortKey) error {
	if _, err := parseInsightDate(key.BusinessDate); err != nil ||
		(key.FactType != DocumentPayment && key.FactType != DocumentInvoice) ||
		key.FactID == "" || strings.TrimSpace(key.FactID) != key.FactID {
		return NewRuleError("invalid_insight_cursor", "洞察游标格式不正确", ErrInvalidInput)
	}
	return nil
}

func cloneInsightFact(source InsightFact) InsightFact {
	clone := source
	if source.Trip != nil {
		trip := *source.Trip
		clone.Trip = &trip
	}
	return clone
}

func parseInsightDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return time.Time{}, ErrInvalidInput
	}
	return parsed, nil
}

func invalidInsightFilter() error {
	return NewRuleError("invalid_insight_filter", "洞察筛选条件不合法", ErrInvalidInput)
}

func invalidInsightProjection() error {
	return NewRuleError("insight_projection_invalid", "洞察数据状态不合法", ErrConflict)
}
