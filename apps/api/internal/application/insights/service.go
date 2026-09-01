package insights

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const insightCursorVersion = "fact-insight-cursor/1"

type Service struct {
	repository ports.InsightRepository
}

type QueryInput struct {
	Filter domain.InsightFilter
	Cursor string
	Limit  int
}

type Page struct {
	RuleVersion string                    `json:"rule_version"`
	Filter      domain.InsightFilter      `json:"filter"`
	Groups      []domain.InsightAggregate `json:"groups"`
	Items       []domain.InsightFact      `json:"items"`
	NextCursor  string                    `json:"next_cursor,omitempty"`
}

type cursorEnvelope struct {
	Version      string              `json:"version"`
	FilterHash   string              `json:"filter_hash"`
	BusinessDate string              `json:"business_date"`
	FactType     domain.DocumentType `json:"fact_type"`
	FactID       string              `json:"fact_id"`
}

func NewService(repository ports.InsightRepository) Service {
	return Service{repository: repository}
}

func (s Service) Query(
	ctx context.Context,
	tenant domain.TenantContext,
	input QueryInput,
) (Page, error) {
	if err := tenant.Require(domain.CapabilityInsightsRead); err != nil {
		return Page{}, err
	}
	if input.Limit < 1 || input.Limit > 100 {
		return Page{}, domain.NewRuleError("invalid_insight_limit", "洞察分页数量必须为 1–100", domain.ErrInvalidInput)
	}
	filter, filterHash, err := domain.CanonicalInsightFilter(input.Filter)
	if err != nil {
		return Page{}, err
	}
	if filter.TripID != "" && !validResourceID(filter.TripID) {
		return Page{}, domain.NewRuleError("invalid_insight_trip_filter", "洞察行程 ID 格式不正确", domain.ErrInvalidInput)
	}
	after, err := decodeCursor(input.Cursor, filterHash)
	if err != nil {
		return Page{}, err
	}
	result, err := s.repository.ReadInsightPage(ctx, tenant.TenantID, filter, after, input.Limit)
	if err != nil {
		return Page{}, err
	}
	page := Page{
		RuleVersion: domain.InsightRuleVersion,
		Filter:      filter,
		Groups:      result.Groups,
		Items:       result.Items,
	}
	if page.Groups == nil {
		page.Groups = []domain.InsightAggregate{}
	}
	if page.Items == nil {
		page.Items = []domain.InsightFact{}
	}
	if result.Next != nil {
		page.NextCursor, err = encodeCursor(filterHash, *result.Next)
		if err != nil {
			return Page{}, err
		}
	}
	return page, nil
}

func decodeCursor(encoded, expectedFilterHash string) (*domain.InsightSortKey, error) {
	if encoded == "" {
		return nil, nil
	}
	if len(encoded) > 2048 {
		return nil, invalidCursor()
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) == 0 || len(payload) > 1536 {
		return nil, invalidCursor()
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope cursorEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, invalidCursor()
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, invalidCursor()
	}
	if envelope.Version != insightCursorVersion ||
		!domain.ValidSHA256Hex(envelope.FilterHash) ||
		envelope.FilterHash != expectedFilterHash ||
		!validResourceID(envelope.FactID) ||
		(envelope.FactType != domain.DocumentPayment && envelope.FactType != domain.DocumentInvoice) {
		return nil, invalidCursor()
	}
	return &domain.InsightSortKey{
		BusinessDate: envelope.BusinessDate,
		FactType:     envelope.FactType,
		FactID:       envelope.FactID,
	}, nil
}

func encodeCursor(filterHash string, key domain.InsightSortKey) (string, error) {
	payload, err := json.Marshal(cursorEnvelope{
		Version:      insightCursorVersion,
		FilterHash:   filterHash,
		BusinessDate: key.BusinessDate,
		FactType:     key.FactType,
		FactID:       key.FactID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func invalidCursor() error {
	return domain.NewRuleError("invalid_insight_cursor", "洞察游标格式或筛选身份不正确", domain.ErrInvalidInput)
}

func validResourceID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if value[index] != '-' {
				return false
			}
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}
