package domain

import "strings"

type BadDebtInput struct {
	Marked          bool   `json:"marked"`
	ExpectedVersion int    `json:"expected_version"`
	Reason          string `json:"reason"`
}

func CanonicalBadDebtRequest(kind DocumentType, id string, input BadDebtInput) (BadDebtInput, string, error) {
	if (kind != DocumentPayment && kind != DocumentInvoice) || id == "" || input.ExpectedVersion < 1 {
		return input, "", ErrInvalidInput
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len([]rune(input.Reason)) < 1 || len([]rune(input.Reason)) > 500 {
		return input, "", NewRuleError("bad_debt_reason_required", "标记或取消坏账必须填写 1～500 字理由", ErrInvalidInput)
	}
	hash, err := hashJSON(struct {
		Kind  DocumentType
		ID    string
		Input BadDebtInput
	}{kind, id, input})
	return input, hash, err
}
