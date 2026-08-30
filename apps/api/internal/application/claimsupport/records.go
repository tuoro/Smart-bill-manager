package claimsupport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const ValidationRuleVersion = "m1-validation/1"

func NewValidationRecord(
	validation domain.ClaimValidation,
	tenantID, claimSetID string,
	fieldIDs map[string]string,
	ids ports.IDGenerator,
	now time.Time,
) (ports.ValidationRecord, error) {
	id, err := ids.NewID()
	if err != nil {
		return ports.ValidationRecord{}, err
	}
	return ports.ValidationRecord{
		ID:           id,
		TenantID:     tenantID,
		ClaimSetID:   claimSetID,
		FieldClaimID: fieldIDs[validation.FieldPath],
		RuleCode:     validation.RuleCode,
		Severity:     validation.Severity,
		Status:       validation.Status,
		SafeMessage:  validation.SafeMessage,
		RuleVersion:  ValidationRuleVersion,
		CreatedAt:    now,
	}, nil
}

func EvidenceHash(pageID, quote, region string) string {
	encoded, _ := json.Marshal([]string{pageID, quote, region})
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func HashBytes(value []byte) string {
	hash := sha256.Sum256(value)
	return hex.EncodeToString(hash[:])
}

func NormalizedInvoiceNumber(fields []domain.FieldCandidate) string {
	for _, field := range fields {
		if field.Path != "invoice_number" || field.Presence != "present" {
			continue
		}
		var normalized string
		if len(field.NormalizedValue) != 0 && json.Unmarshal(field.NormalizedValue, &normalized) == nil {
			return normalized
		}
		var original string
		if json.Unmarshal(field.Value, &original) == nil {
			return domain.NormalizeExact(original)
		}
	}
	return ""
}
