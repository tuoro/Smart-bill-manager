package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

type AllocationRequest struct {
	CandidateID    string `json:"candidate_id"`
	AllocatedMinor int64  `json:"allocated_minor"`
}

type AllocationCandidate struct {
	ID             string
	Currency       string
	RemainingMinor int64
	Available      bool
}

// CanonicalAllocationPlan validates request-local invariants, sorts a copy by
// candidate ID and returns the stable identity used by confirmation idempotency.
func CanonicalAllocationPlan(items []AllocationRequest) ([]AllocationRequest, string, error) {
	canonical := append([]AllocationRequest(nil), items...)
	for _, item := range canonical {
		if item.CandidateID == "" {
			return nil, "", NewRuleError("invalid_link_candidate", "分配候选 ID 不能为空", ErrInvalidInput)
		}
		if item.AllocatedMinor < 1 || item.AllocatedMinor > MaxSafeMinorUnits {
			return nil, "", NewRuleError("invalid_allocation_amount", "分配金额必须是安全范围内的正整数", ErrInvalidInput)
		}
	}
	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left].CandidateID < canonical[right].CandidateID
	})
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1].CandidateID == canonical[index].CandidateID {
			return nil, "", NewRuleError("duplicate_allocation_candidate", "同一候选不能重复分配", ErrInvalidInput)
		}
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(encoded)
	return canonical, hex.EncodeToString(digest[:]), nil
}

// ValidateAllocationPlan checks values returned by the review projection. The
// confirmation transaction must repeat these checks against current database
// state before creating any link.
func ValidateAllocationPlan(
	factAmount int64,
	factCurrency string,
	candidates []AllocationCandidate,
	plan []AllocationRequest,
) error {
	if factAmount < 0 || factAmount > MaxSafeMinorUnits {
		return NewRuleError("invalid_allocation_amount", "Fact 金额超出允许范围", ErrInvalidInput)
	}
	if _, ok := Currency(factCurrency).Exponent(); !ok {
		return NewRuleError("unsupported_currency", "仅支持 CNY、USD、EUR 和 JPY", ErrInvalidInput)
	}
	byID := make(map[string]AllocationCandidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
	}
	total := int64(0)
	for _, item := range plan {
		candidate, exists := byID[item.CandidateID]
		if !exists {
			return NewRuleError("invalid_link_candidate", "关联候选不属于当前 Claim", ErrConflict)
		}
		if !candidate.Available || candidate.RemainingMinor <= 0 {
			return NewRuleError("allocation_candidate_unavailable", "候选已删除或没有可分配余额", ErrConflict)
		}
		if candidate.Currency != factCurrency {
			return NewRuleError("allocation_currency_mismatch", "分配双方币种必须一致", ErrConflict)
		}
		if item.AllocatedMinor > candidate.RemainingMinor {
			return NewRuleError("allocation_exceeds_target_balance", "分配金额超过候选当前剩余余额", ErrConflict)
		}
		if total > factAmount-item.AllocatedMinor {
			return NewRuleError("allocation_exceeds_fact_amount", "本次分配合计超过新建 Fact 金额", ErrConflict)
		}
		total += item.AllocatedMinor
	}
	return nil
}
