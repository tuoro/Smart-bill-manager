package processing

import (
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/claimsupport"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type builtClaim struct {
	Bundle   ports.ClaimBundle
	FieldIDs map[string]string
}

func buildClaimBundle(
	validated domain.ValidatedClaim,
	tenantID, documentID, aiRunID string,
	pages []ports.NormalizedPage,
	ids ports.IDGenerator,
	now time.Time,
) (builtClaim, error) {
	claimSetID, err := ids.NewID()
	if err != nil {
		return builtClaim{}, err
	}
	pageIDs := make(map[int]string, len(pages))
	for _, page := range pages {
		pageIDs[page.PageNumber] = page.ID
	}
	result := builtClaim{
		Bundle: ports.ClaimBundle{
			ClaimSet: ports.ClaimSetRecord{
				ID:            claimSetID,
				TenantID:      tenantID,
				DocumentID:    documentID,
				OriginAiRunID: aiRunID,
				DocumentType:  validated.DocumentType,
				Status:        validated.Status,
				CreatedAt:     now,
			},
		},
		FieldIDs: make(map[string]string, len(validated.Fields)),
	}
	for _, field := range validated.Fields {
		fieldID, err := ids.NewID()
		if err != nil {
			return builtClaim{}, err
		}
		result.FieldIDs[field.Path] = fieldID
		result.Bundle.Fields = append(result.Bundle.Fields, ports.FieldClaimRecord{
			ID:              fieldID,
			TenantID:        tenantID,
			ClaimSetID:      claimSetID,
			FieldPath:       field.Path,
			ValueType:       field.ValueType,
			Presence:        field.Presence,
			TypedValueJSON:  string(field.Value),
			NormalizedValue: string(field.NormalizedValue),
			CreatedAt:       now,
		})
		for _, evidence := range field.Evidence {
			pageID := pageIDs[evidence.Page]
			if pageID == "" {
				return builtClaim{}, fmt.Errorf("evidence page %d has no persisted page", evidence.Page)
			}
			evidenceID, err := ids.NewID()
			if err != nil {
				return builtClaim{}, err
			}
			region := string(evidence.Region)
			result.Bundle.Evidence = append(result.Bundle.Evidence, ports.EvidenceRecord{
				ID:             evidenceID,
				TenantID:       tenantID,
				FieldClaimID:   fieldID,
				DocumentPageID: pageID,
				Quote:          evidence.Quote,
				RegionJSON:     region,
				EvidenceHash:   claimsupport.EvidenceHash(pageID, evidence.Quote, region),
				CreatedAt:      now,
			})
		}
	}
	for _, validation := range validated.Validations {
		record, err := claimsupport.NewValidationRecord(validation, tenantID, claimSetID, result.FieldIDs, ids, now)
		if err != nil {
			return builtClaim{}, err
		}
		result.Bundle.Validations = append(result.Bundle.Validations, record)
	}
	return result, nil
}
