package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

const (
	AllocationPlanVersion    = "payment-invoice-allocation-plan/1"
	AllocationRequestVersion = "allocation-adjustment-request/1"
	MaxAllocationTargets     = 200
	AllocationModeSupplement = "supplement"
	AllocationModeWithdraw   = "withdraw"
	AllocationModeReplace    = "replace"
)

type ActiveAllocationLink struct {
	ID             string `json:"link_id"`
	PaymentID      string `json:"payment_id"`
	InvoiceID      string `json:"invoice_id"`
	AllocatedMinor int64  `json:"allocated_minor"`
	Currency       string `json:"currency"`
}

type DesiredAllocation struct {
	TargetFactID   string `json:"target_fact_id"`
	AllocatedMinor int64  `json:"allocated_minor"`
}

type AllocationTargetBalance struct {
	ID                      string
	Currency                string
	MaximumAllocatableMinor int64
	Available               bool
}

type AllocationAdjustmentDiff struct {
	Mode      string
	Unchanged []ActiveAllocationLink
	End       []ActiveAllocationLink
	Create    []DesiredAllocation
}

func CanonicalActiveAllocationPlan(
	anchorType DocumentType,
	anchorID string,
	links []ActiveAllocationLink,
) ([]ActiveAllocationLink, string, error) {
	if !validAllocationAnchor(anchorType, anchorID) {
		return nil, "", NewRuleError("invalid_allocation_anchor", "分配 anchor 不合法", ErrInvalidInput)
	}
	canonical := append([]ActiveAllocationLink(nil), links...)
	sort.Slice(canonical, func(left, right int) bool { return canonical[left].ID < canonical[right].ID })
	seenIDs := make(map[string]struct{}, len(canonical))
	seenPairs := make(map[string]struct{}, len(canonical))
	for _, link := range canonical {
		if link.ID == "" || link.PaymentID == "" || link.InvoiceID == "" ||
			link.AllocatedMinor < 1 || link.AllocatedMinor > MaxSafeMinorUnits {
			return nil, "", NewRuleError("invalid_allocation_plan", "活动分配计划包含非法 Link", ErrInvalidInput)
		}
		if _, ok := Currency(link.Currency).Exponent(); !ok {
			return nil, "", NewRuleError("unsupported_currency", "仅支持 CNY、USD、EUR 和 JPY", ErrInvalidInput)
		}
		if (anchorType == DocumentPayment && link.PaymentID != anchorID) ||
			(anchorType == DocumentInvoice && link.InvoiceID != anchorID) {
			return nil, "", NewRuleError("invalid_allocation_plan", "活动 Link 不属于当前 anchor", ErrInvalidInput)
		}
		if _, duplicate := seenIDs[link.ID]; duplicate {
			return nil, "", NewRuleError("invalid_allocation_plan", "活动分配计划包含重复 Link", ErrInvalidInput)
		}
		pair := link.PaymentID + "\x00" + link.InvoiceID
		if _, duplicate := seenPairs[pair]; duplicate {
			return nil, "", NewRuleError("invalid_allocation_plan", "同一支付与发票存在重复活动 Link", ErrInvalidInput)
		}
		seenIDs[link.ID] = struct{}{}
		seenPairs[pair] = struct{}{}
	}
	payload := struct {
		Version    string                 `json:"version"`
		AnchorType DocumentType           `json:"anchor_type"`
		AnchorID   string                 `json:"anchor_id"`
		Links      []ActiveAllocationLink `json:"links"`
	}{AllocationPlanVersion, anchorType, anchorID, canonical}
	digest, err := hashJSON(payload)
	if err != nil {
		return nil, "", err
	}
	return canonical, digest, nil
}

func CanonicalDesiredAllocations(items []DesiredAllocation) ([]DesiredAllocation, error) {
	if len(items) > MaxAllocationTargets {
		return nil, NewRuleError("allocation_target_limit_exceeded", "分配目标不能超过 200 项", ErrInvalidInput)
	}
	canonical := append([]DesiredAllocation(nil), items...)
	if canonical == nil {
		canonical = []DesiredAllocation{}
	}
	for _, item := range canonical {
		if item.TargetFactID == "" {
			return nil, NewRuleError("invalid_allocation_target", "分配目标 ID 不能为空", ErrInvalidInput)
		}
		if item.AllocatedMinor < 1 || item.AllocatedMinor > MaxSafeMinorUnits {
			return nil, NewRuleError("invalid_allocation_amount", "分配金额必须是安全范围内的正整数", ErrInvalidInput)
		}
	}
	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left].TargetFactID < canonical[right].TargetFactID
	})
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1].TargetFactID == canonical[index].TargetFactID {
			return nil, NewRuleError("duplicate_allocation_target", "同一目标不能重复分配", ErrInvalidInput)
		}
	}
	return canonical, nil
}

func CanonicalAllocationAdjustmentRequest(
	anchorType DocumentType,
	anchorID, expectedPlanHash string,
	desired []DesiredAllocation,
	reason string,
) ([]DesiredAllocation, string, string, error) {
	if !validAllocationAnchor(anchorType, anchorID) {
		return nil, "", "", NewRuleError("invalid_allocation_anchor", "分配 anchor 不合法", ErrInvalidInput)
	}
	if !ValidSHA256Hex(expectedPlanHash) {
		return nil, "", "", NewRuleError("invalid_plan_hash", "expected_plan_hash 必须是 64 位小写十六进制", ErrInvalidInput)
	}
	canonical, err := CanonicalDesiredAllocations(desired)
	if err != nil {
		return nil, "", "", err
	}
	trimmedReason := strings.TrimSpace(reason)
	if len([]rune(trimmedReason)) < 1 {
		return nil, "", "", NewRuleError("allocation_reason_required", "调整理由不能为空", ErrInvalidInput)
	}
	if len([]rune(trimmedReason)) > 500 {
		return nil, "", "", NewRuleError("allocation_reason_too_long", "调整理由不能超过 500 个字符", ErrInvalidInput)
	}
	payload := struct {
		Version          string              `json:"version"`
		AnchorType       DocumentType        `json:"anchor_type"`
		AnchorID         string              `json:"anchor_id"`
		ExpectedPlanHash string              `json:"expected_plan_hash"`
		Desired          []DesiredAllocation `json:"desired_allocations"`
		Reason           string              `json:"reason"`
	}{AllocationRequestVersion, anchorType, anchorID, expectedPlanHash, canonical, trimmedReason}
	digest, err := hashJSON(payload)
	if err != nil {
		return nil, "", "", err
	}
	return canonical, trimmedReason, digest, nil
}

func BuildAllocationAdjustmentDiff(
	anchorType DocumentType,
	anchorID string,
	current []ActiveAllocationLink,
	desired []DesiredAllocation,
) (AllocationAdjustmentDiff, error) {
	canonicalCurrent, _, err := CanonicalActiveAllocationPlan(anchorType, anchorID, current)
	if err != nil {
		return AllocationAdjustmentDiff{}, err
	}
	canonicalDesired, err := CanonicalDesiredAllocations(desired)
	if err != nil {
		return AllocationAdjustmentDiff{}, err
	}
	currentByTarget := make(map[string]ActiveAllocationLink, len(canonicalCurrent))
	for _, link := range canonicalCurrent {
		currentByTarget[allocationTargetID(anchorType, link)] = link
	}
	desiredByTarget := make(map[string]DesiredAllocation, len(canonicalDesired))
	for _, item := range canonicalDesired {
		desiredByTarget[item.TargetFactID] = item
	}
	diff := AllocationAdjustmentDiff{
		Unchanged: []ActiveAllocationLink{},
		End:       []ActiveAllocationLink{},
		Create:    []DesiredAllocation{},
	}
	for targetID, link := range currentByTarget {
		item, remains := desiredByTarget[targetID]
		if remains && item.AllocatedMinor == link.AllocatedMinor {
			diff.Unchanged = append(diff.Unchanged, link)
			continue
		}
		diff.End = append(diff.End, link)
	}
	for _, item := range canonicalDesired {
		link, exists := currentByTarget[item.TargetFactID]
		if !exists || link.AllocatedMinor != item.AllocatedMinor {
			diff.Create = append(diff.Create, item)
		}
	}
	if len(diff.End) == 0 && len(diff.Create) == 0 {
		return AllocationAdjustmentDiff{}, NewRuleError("allocation_plan_unchanged", "分配计划没有变化", ErrConflict)
	}
	sort.Slice(diff.Unchanged, func(left, right int) bool { return diff.Unchanged[left].ID < diff.Unchanged[right].ID })
	sort.Slice(diff.End, func(left, right int) bool { return diff.End[left].ID < diff.End[right].ID })
	switch {
	case len(diff.End) == 0:
		diff.Mode = AllocationModeSupplement
	case len(diff.Create) == 0:
		diff.Mode = AllocationModeWithdraw
	default:
		diff.Mode = AllocationModeReplace
	}
	return diff, nil
}

func ValidateDesiredAllocationPlan(
	anchorAmount int64,
	anchorCurrency string,
	targets []AllocationTargetBalance,
	desired []DesiredAllocation,
) error {
	if anchorAmount < 0 || anchorAmount > MaxSafeMinorUnits {
		return NewRuleError("invalid_allocation_amount", "anchor 金额超出允许范围", ErrInvalidInput)
	}
	if _, ok := Currency(anchorCurrency).Exponent(); !ok {
		return NewRuleError("unsupported_currency", "仅支持 CNY、USD、EUR 和 JPY", ErrInvalidInput)
	}
	canonical, err := CanonicalDesiredAllocations(desired)
	if err != nil {
		return err
	}
	byID := make(map[string]AllocationTargetBalance, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	total := int64(0)
	for _, item := range canonical {
		target, exists := byID[item.TargetFactID]
		if !exists || !target.Available {
			return NewRuleError("allocation_target_unavailable", "分配目标不存在或当前不可用", ErrConflict)
		}
		if target.Currency != anchorCurrency {
			return NewRuleError("allocation_currency_mismatch", "分配双方币种必须一致", ErrConflict)
		}
		if item.AllocatedMinor > target.MaximumAllocatableMinor {
			return NewRuleError("allocation_exceeds_target_balance", "分配金额超过目标当前可调整余额", ErrConflict)
		}
		if total > anchorAmount-item.AllocatedMinor {
			return NewRuleError("allocation_exceeds_fact_amount", "期望分配合计超过 anchor 金额", ErrConflict)
		}
		total += item.AllocatedMinor
	}
	return nil
}

func ValidateIdempotencyKey(value string) error {
	length := len([]rune(value))
	if length < 8 || length > 128 {
		return NewRuleError("invalid_idempotency_key", "Idempotency-Key 长度必须为 8 到 128", ErrInvalidInput)
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return NewRuleError("invalid_idempotency_key", "Idempotency-Key 不能包含空白或控制字符", ErrInvalidInput)
		}
	}
	return nil
}

func ValidSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validAllocationAnchor(anchorType DocumentType, anchorID string) bool {
	return anchorID != "" && (anchorType == DocumentPayment || anchorType == DocumentInvoice)
}

func allocationTargetID(anchorType DocumentType, link ActiveAllocationLink) string {
	if anchorType == DocumentPayment {
		return link.InvoiceID
	}
	return link.PaymentID
}

func hashJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
