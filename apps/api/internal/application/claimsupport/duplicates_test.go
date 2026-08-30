package claimsupport

import (
	"context"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestBuildDuplicateCandidatesBlocksInsteadOfTruncatingOverLimit(t *testing.T) {
	pages := make([]domain.VisualPage, 0, 11)
	for pageNumber := 1; pageNumber <= 11; pageNumber++ {
		pages = append(pages, domain.VisualPage{
			ID:          string(rune('a' + pageNumber)),
			DocumentID:  "document",
			PageNumber:  pageNumber,
			Width:       1000,
			Height:      1400,
			Fingerprint: domain.NewPageVisualFingerprint(0, 0),
		})
	}
	source := duplicateSourceStub{current: domain.VisualDocument{ID: "document", Pages: pages}}
	candidates, exceeded, err := BuildDuplicateCandidates(
		context.Background(),
		source,
		domain.ValidatedClaim{DocumentType: domain.DocumentUnknown},
		"tenant",
		"document",
		"claim",
		idStub{},
		time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !exceeded || len(candidates) != 0 {
		t.Fatalf("limit result = exceeded:%t candidates:%d", exceeded, len(candidates))
	}
}

type duplicateSourceStub struct {
	current domain.VisualDocument
}

func (stub duplicateSourceStub) ListVisualDuplicateDocuments(
	context.Context,
	string,
	string,
) (domain.VisualDocument, []domain.VisualDocument, error) {
	return stub.current, nil, nil
}

func (duplicateSourceStub) ListFieldDuplicateTargets(
	context.Context,
	string,
	domain.FieldDuplicateInput,
) ([]domain.FieldDuplicateTarget, error) {
	return nil, nil
}

type idStub struct{}

func (idStub) NewID() (string, error) { return "unused", nil }
