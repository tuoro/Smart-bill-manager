package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const FactQueryVersion = "fact-management/1"

type FactFilter struct {
	DateFrom         string `json:"date_from"`
	DateTo           string `json:"date_to"`
	Query            string `json:"q"`
	AllocationStatus string `json:"allocation_status"`
}

type FactSortKey struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func CanonicalFactFilter(input FactFilter) (FactFilter, error) {
	input.Query = strings.Join(strings.Fields(input.Query), " ")
	if !utf8.ValidString(input.Query) || utf8.RuneCountInString(input.Query) > 200 || strings.ContainsFunc(input.Query, unicode.IsControl) {
		return FactFilter{}, ErrInvalidInput
	}
	for _, date := range []string{input.DateFrom, input.DateTo} {
		if date == "" {
			continue
		}
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil || parsed.Year() < 1 || parsed.Year() > 9999 || parsed.Format("2006-01-02") != date {
			return FactFilter{}, ErrInvalidInput
		}
	}
	if input.DateFrom != "" && input.DateTo != "" && input.DateFrom > input.DateTo {
		return FactFilter{}, ErrInvalidInput
	}
	if input.AllocationStatus == "" {
		input.AllocationStatus = InsightStatusAll
	}
	switch input.AllocationStatus {
	case InsightStatusAll, InsightStatusNone, InsightStatusPartial, InsightStatusFull:
	default:
		return FactFilter{}, ErrInvalidInput
	}
	return input, nil
}

func (key FactSortKey) Valid() bool {
	if key.CreatedAt.IsZero() || key.CreatedAt.Year() < 1 || key.CreatedAt.Year() > 9999 || len(key.ID) != 36 {
		return false
	}
	for index, ch := range key.ID {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if ch != '-' {
				return false
			}
			continue
		}
		if !(ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F' || ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}
