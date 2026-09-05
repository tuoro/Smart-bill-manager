package domain

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// ManualReviewIdentity 固定用户接管请求身份，不把失败伪装成模型成功。
func ManualReviewIdentity(jobID string, version int, kind DocumentType, reason string) (string, string, error) {
	reason = strings.TrimSpace(reason)
	if jobID == "" || version < 1 || (kind != DocumentPayment && kind != DocumentInvoice && kind != DocumentTrip) || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 500 {
		return "", "", NewRuleError("invalid_manual_review", "请选择单据类型并填写 1 至 500 字的人工接管理由", ErrInvalidInput)
	}
	hash, err := hashJSON(struct {
		Protocol string
		JobID    string
		Version  int
		Kind     DocumentType
		Reason   string
	}{"manual-review/1", jobID, version, kind, reason})
	return reason, hash, err
}

// EmptyManualClaim 使用同一字段规范创建空快照，不推断任何票面业务值。
func EmptyManualClaim(kind DocumentType, pageCount int) (ValidatedClaim, error) {
	if (kind != DocumentPayment && kind != DocumentInvoice && kind != DocumentTrip) || pageCount < 1 || pageCount > 20 {
		return ValidatedClaim{}, ErrInvalidInput
	}
	specs := expectedSpecs(kind, nil)
	fields := make([]FieldCandidate, 0, len(specs))
	for path, spec := range specs {
		fields = append(fields, FieldCandidate{Path: path, ValueType: spec.ValueType, Presence: "absent", Issues: []string{}})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })
	return ValidateClaim(ClaimEnvelope{SchemaVersion: "document-claim/3", DocumentType: string(kind), Fields: fields, DocumentIssues: []string{}}, pageCount), nil
}

type ManualEvidenceInput struct {
	Page  int    `json:"page"`
	Quote string `json:"quote"`
}

func (input ManualEvidenceInput) Validate(pageCount int) error {
	if input.Page < 1 || input.Page > pageCount || strings.TrimSpace(input.Quote) == "" || !utf8.ValidString(input.Quote) || utf8.RuneCountInString(input.Quote) > 500 {
		return NewRuleError("invalid_manual_evidence", "请标注原件中的有效页码和 1 至 500 字的实际摘录", ErrInvalidInput)
	}
	return nil
}
