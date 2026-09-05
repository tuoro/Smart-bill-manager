package domain

import (
	"slices"
	"strings"
	"time"
)

// 纠错预览必须携带所有活动 Link 的对端条件，不能只检查仍在候选窗口内的记录。
type CorrectionLink struct {
	ID                   string `json:"id"`
	TargetID             string `json:"target_id"`
	AllocatedMinor       int64  `json:"allocated_minor"`
	Currency             string `json:"currency"`
	TargetCurrency       string `json:"target_currency"`
	TargetBusinessDate   string `json:"target_business_date"`
	TargetAmountMinor    int64  `json:"target_amount_minor"`
	TargetAllocatedMinor int64  `json:"target_allocated_minor"`
	TargetVersion        int    `json:"target_version"`
	TargetAvailable      bool   `json:"target_available"`
}

type CorrectionIssue struct {
	LinkID  string `json:"link_id,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func CorrectionReason(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) < 1 || len([]rune(value)) > 500 {
		return "", NewRuleError("correction_reason_required", "纠错必须填写 1～500 字理由", ErrInvalidInput)
	}
	return value, nil
}

func CanonicalCorrectionWithdrawals(links []CorrectionLink, requested []string) ([]string, error) {
	known := make(map[string]bool, len(links))
	for _, link := range links {
		if link.ID == "" || known[link.ID] {
			return nil, NewRuleError("invalid_correction_links", "活动分配身份不完整", ErrConflict)
		}
		known[link.ID] = true
	}
	canonical := slices.Clone(requested)
	slices.Sort(canonical)
	for index, id := range canonical {
		if !known[id] || (index > 0 && canonical[index-1] == id) {
			return nil, NewRuleError("invalid_correction_withdrawal", "撤销清单只能包含不重复的当前活动分配", ErrInvalidInput)
		}
	}
	return canonical, nil
}

func ValidateCorrectionLinks(amount Money, businessDate string, links []CorrectionLink, withdrawals []string) ([]CorrectionIssue, error) {
	if err := amount.Validate(); err != nil {
		return nil, err
	}
	_, err := time.Parse(time.DateOnly, businessDate)
	if err != nil {
		return nil, NewRuleError("invalid_correction_date", "纠错业务日期无效", ErrInvalidInput)
	}
	canonical, err := CanonicalCorrectionWithdrawals(links, withdrawals)
	if err != nil {
		return nil, err
	}
	issues := make([]CorrectionIssue, 0)
	var allocated int64
	for _, link := range links {
		if slices.Contains(canonical, link.ID) {
			continue
		}
		if link.AllocatedMinor < 1 || link.AllocatedMinor > MaxSafeMinorUnits || link.TargetAmountMinor < 0 ||
			link.TargetAmountMinor > MaxSafeMinorUnits || link.TargetAllocatedMinor < link.AllocatedMinor {
			return nil, NewRuleError("invalid_correction_balance", "现有分配余额异常，不能继续纠错", ErrConflict)
		}
		if link.AllocatedMinor > amount.MinorUnits-allocated {
			issues = append(issues, CorrectionIssue{LinkID: link.ID, Code: "correction_overallocated", Message: "保留分配总额超过新金额，请明确撤销或先调整分配"})
		} else {
			allocated += link.AllocatedMinor
		}
		if !link.TargetAvailable || link.TargetAllocatedMinor > link.TargetAmountMinor {
			issues = append(issues, CorrectionIssue{LinkID: link.ID, Code: "correction_target_unavailable", Message: "分配对端已不可用或余额异常"})
		}
		if link.Currency != string(amount.Currency) || link.TargetCurrency != string(amount.Currency) {
			issues = append(issues, CorrectionIssue{LinkID: link.ID, Code: "correction_currency_conflict", Message: "保留分配与新币种不一致，请明确撤销该分配"})
		}
		_, err := time.Parse(time.DateOnly, link.TargetBusinessDate)
		if err != nil {
			return nil, NewRuleError("invalid_correction_target_date", "分配对端日期无效", ErrConflict)
		}
	}
	return issues, nil
}
