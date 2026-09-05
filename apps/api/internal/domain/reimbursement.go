package domain

import (
	"sort"
	"strings"
	"time"
)

const (
	ReimbursementPolicyVersion = "reimbursement-policy/2"
	MaxReimbursementItems      = 200
	MaxReimbursementFindings   = 1000
)

type ReimbursementStatus string

const (
	ReimbursementStatusSubmitted  ReimbursementStatus = "submitted"
	ReimbursementStatusReimbursed ReimbursementStatus = "reimbursed"
	ReimbursementStatusRejected   ReimbursementStatus = "rejected"
)

const (
	ReimbursementFindingMissingInvoice = "missing_invoice"
	ReimbursementFindingAmountConflict = "amount_conflict"
	ReimbursementFindingDuplicate      = "duplicate_reimbursement"
)

type ReimbursementTripSnapshot struct {
	Timezone  *string `json:"timezone"`
	Version   *int    `json:"version"`
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	StartDate string  `json:"start_date"`
	EndDate   string  `json:"end_date"`
}

type ReimbursementPolicyItem struct {
	FactReviewDecisionID string       `json:"fact_review_decision_id"`
	AssignmentID         string       `json:"assignment_id"`
	FactType             DocumentType `json:"fact_type"`
	FactID               string       `json:"fact_id"`
	DisplayName          string       `json:"display_name"`
	BusinessDate         string       `json:"business_date"`
	AmountMinor          int64        `json:"amount_minor"`
	Currency             Currency     `json:"currency"`
}

type ReimbursementPolicyLink struct {
	ID             string   `json:"id"`
	PaymentID      string   `json:"payment_id"`
	InvoiceID      string   `json:"invoice_id"`
	AllocatedMinor int64    `json:"allocated_minor"`
	Currency       Currency `json:"currency"`
}

type ReimbursementPriorUse struct {
	FactType        DocumentType        `json:"fact_type"`
	FactID          string              `json:"fact_id"`
	ReimbursementID string              `json:"reimbursement_id"`
	Status          ReimbursementStatus `json:"status"`
}

type ReimbursementCurrencyTotal struct {
	Currency    Currency `json:"currency"`
	AmountMinor int64    `json:"amount_minor"`
}

type ReimbursementPolicyFinding struct {
	FindingKey             string              `json:"finding_key"`
	Code                   string              `json:"code"`
	AssignmentID           string              `json:"assignment_id"`
	FactType               DocumentType        `json:"fact_type"`
	FactID                 string              `json:"fact_id"`
	ExpectedMinor          *int64              `json:"expected_minor,omitempty"`
	ActualMinor            *int64              `json:"actual_minor,omitempty"`
	Currency               Currency            `json:"currency,omitempty"`
	RelatedReimbursementID string              `json:"related_reimbursement_id,omitempty"`
	RelatedStatus          ReimbursementStatus `json:"related_status,omitempty"`
}

type ReimbursementPolicySnapshot struct {
	Materials    []ReimbursementMaterial      `json:"materials"`
	RuleVersion  string                       `json:"rule_version"`
	Trip         ReimbursementTripSnapshot    `json:"trip"`
	Items        []ReimbursementPolicyItem    `json:"items"`
	Findings     []ReimbursementPolicyFinding `json:"findings"`
	Totals       []ReimbursementCurrencyTotal `json:"totals_by_currency"`
	SnapshotHash string                       `json:"snapshot_hash"`
}

type ReimbursementPolicyInput struct {
	Materials []ReimbursementMaterial
	Trip      ReimbursementTripSnapshot
	Items     []ReimbursementPolicyItem
	Links     []ReimbursementPolicyLink
	PriorUses []ReimbursementPriorUse
}

func CanonicalReimbursementSelection(assignmentIDs []string) ([]string, error) {
	if len(assignmentIDs) < 1 || len(assignmentIDs) > MaxReimbursementItems {
		return nil, NewRuleError("invalid_reimbursement_selection", "报销选择必须包含 1 至 200 个行程归属项", ErrInvalidInput)
	}
	result := make([]string, len(assignmentIDs))
	seen := make(map[string]struct{}, len(assignmentIDs))
	for index, value := range assignmentIDs {
		if value == "" || strings.TrimSpace(value) != value {
			return nil, NewRuleError("invalid_reimbursement_selection", "行程归属 ID 不能为空或包含首尾空白", ErrInvalidInput)
		}
		if _, exists := seen[value]; exists {
			return nil, NewRuleError("duplicate_reimbursement_assignment", "报销选择不能包含重复行程归属", ErrInvalidInput)
		}
		seen[value] = struct{}{}
		result[index] = value
	}
	sort.Strings(result)
	return result, nil
}

func EvaluateReimbursementPolicy(input ReimbursementPolicyInput) (ReimbursementPolicySnapshot, error) {
	if err := validateReimbursementTrip(input.Trip); err != nil {
		return ReimbursementPolicySnapshot{}, err
	}
	items, itemByFact, err := canonicalReimbursementItems(input.Items)
	if err != nil {
		return ReimbursementPolicySnapshot{}, err
	}
	materials, err := canonicalReimbursementMaterials(input.Materials, itemByFact)
	if err != nil {
		return ReimbursementPolicySnapshot{}, err
	}
	links, linkTotals, linkCounts, linksByFact, err := canonicalReimbursementLinks(input.Links, itemByFact)
	if err != nil {
		return ReimbursementPolicySnapshot{}, err
	}
	priorUses, err := canonicalReimbursementPriorUses(input.PriorUses, itemByFact)
	if err != nil {
		return ReimbursementPolicySnapshot{}, err
	}
	findings := make([]ReimbursementPolicyFinding, 0)
	for _, item := range items {
		key := reimbursementFactKey(item.FactType, item.FactID)
		linked := linkCounts[key]
		actual := linkTotals[key]
		if item.FactType == DocumentPayment && linked == 0 {
			findings = append(findings, newReimbursementFinding(
				ReimbursementFindingMissingInvoice,
				item,
				int64Pointer(item.AmountMinor),
				int64Pointer(0),
				item.Currency,
				ReimbursementPriorUse{},
				linksByFact[key],
			))
		} else if linked > 0 && actual != item.AmountMinor {
			findings = append(findings, newReimbursementFinding(
				ReimbursementFindingAmountConflict,
				item,
				int64Pointer(item.AmountMinor),
				int64Pointer(actual),
				item.Currency,
				ReimbursementPriorUse{},
				linksByFact[key],
			))
		}
	}
	for _, prior := range priorUses {
		item := itemByFact[reimbursementFactKey(prior.FactType, prior.FactID)]
		findings = append(findings, newReimbursementFinding(
			ReimbursementFindingDuplicate,
			item,
			nil,
			nil,
			"",
			prior,
			linksByFact[reimbursementFactKey(item.FactType, item.FactID)],
		))
	}
	if len(findings) > MaxReimbursementFindings {
		return ReimbursementPolicySnapshot{}, NewRuleError(
			"reimbursement_finding_limit_exceeded",
			"报销政策提示超过 1000 条，不能静默截断",
			ErrConflict,
		)
	}
	sort.Slice(findings, func(left, right int) bool {
		if findings[left].Code != findings[right].Code {
			return findings[left].Code < findings[right].Code
		}
		if findings[left].FactType != findings[right].FactType {
			return findings[left].FactType < findings[right].FactType
		}
		if findings[left].FactID != findings[right].FactID {
			return findings[left].FactID < findings[right].FactID
		}
		return findings[left].RelatedReimbursementID < findings[right].RelatedReimbursementID
	})
	totals, err := reimbursementCurrencyTotals(items)
	if err != nil {
		return ReimbursementPolicySnapshot{}, err
	}
	hash, err := hashJSON(struct {
		Materials []ReimbursementMaterial      `json:"materials"`
		Version   string                       `json:"version"`
		Trip      ReimbursementTripSnapshot    `json:"trip"`
		Items     []ReimbursementPolicyItem    `json:"items"`
		Links     []ReimbursementPolicyLink    `json:"links"`
		PriorUses []ReimbursementPriorUse      `json:"prior_uses"`
		Findings  []ReimbursementPolicyFinding `json:"findings"`
	}{
		Materials: materials,
		Version:   ReimbursementPolicyVersion, Trip: input.Trip,
		Items: items, Links: links, PriorUses: priorUses, Findings: findings,
	})
	if err != nil {
		return ReimbursementPolicySnapshot{}, err
	}
	return ReimbursementPolicySnapshot{
		Materials:   materials,
		RuleVersion: ReimbursementPolicyVersion,
		Trip:        input.Trip, Items: items, Findings: findings, Totals: totals, SnapshotHash: hash,
	}, nil
}

func CanonicalReimbursementSubmissionRequest(
	tripID string,
	assignmentIDs []string,
	expectedSnapshotHash string,
	acknowledgedFindingKeys []string,
	reason string,
) ([]string, []string, string, string, error) {
	selection, err := CanonicalReimbursementSelection(assignmentIDs)
	if err != nil {
		return nil, nil, "", "", err
	}
	if tripID == "" || strings.TrimSpace(tripID) != tripID || !ValidSHA256Hex(expectedSnapshotHash) {
		return nil, nil, "", "", NewRuleError("invalid_reimbursement_submission", "报销 Trip 或快照身份不合法", ErrInvalidInput)
	}
	acknowledged, err := canonicalFindingKeys(acknowledgedFindingKeys)
	if err != nil {
		return nil, nil, "", "", err
	}
	trimmedReason, err := validateReimbursementReason(reason)
	if err != nil {
		return nil, nil, "", "", err
	}
	requestHash, err := hashJSON(struct {
		Version                 string   `json:"version"`
		TripID                  string   `json:"trip_id"`
		AssignmentIDs           []string `json:"assignment_ids"`
		ExpectedSnapshotHash    string   `json:"expected_snapshot_hash"`
		AcknowledgedFindingKeys []string `json:"acknowledged_finding_keys"`
		Reason                  string   `json:"reason"`
	}{
		Version: "reimbursement-submit-request/1", TripID: tripID,
		AssignmentIDs: selection, ExpectedSnapshotHash: expectedSnapshotHash,
		AcknowledgedFindingKeys: acknowledged, Reason: trimmedReason,
	})
	if err != nil {
		return nil, nil, "", "", err
	}
	return selection, acknowledged, trimmedReason, requestHash, nil
}

func CanonicalReimbursementStatusRequest(
	reimbursementID string,
	expectedStatus, desiredStatus ReimbursementStatus,
	expectedVersion int,
	reason string,
) (string, string, string, error) {
	if reimbursementID == "" || strings.TrimSpace(reimbursementID) != reimbursementID || expectedVersion < 1 {
		return "", "", "", NewRuleError("invalid_reimbursement_status_request", "报销状态请求身份或版本不合法", ErrInvalidInput)
	}
	action, err := ReimbursementTransitionAction(expectedStatus, desiredStatus)
	if err != nil {
		return "", "", "", err
	}
	trimmedReason, err := validateReimbursementReason(reason)
	if err != nil {
		return "", "", "", err
	}
	requestHash, err := hashJSON(struct {
		Version         string              `json:"version"`
		ReimbursementID string              `json:"reimbursement_id"`
		ExpectedStatus  ReimbursementStatus `json:"expected_status"`
		DesiredStatus   ReimbursementStatus `json:"desired_status"`
		ExpectedVersion int                 `json:"expected_version"`
		Reason          string              `json:"reason"`
	}{
		Version: "reimbursement-status-request/1", ReimbursementID: reimbursementID,
		ExpectedStatus: expectedStatus, DesiredStatus: desiredStatus,
		ExpectedVersion: expectedVersion, Reason: trimmedReason,
	})
	if err != nil {
		return "", "", "", err
	}
	return action, trimmedReason, requestHash, nil
}

func ReimbursementTransitionAction(current, desired ReimbursementStatus) (string, error) {
	if !current.Valid() || !desired.Valid() {
		return "", NewRuleError("invalid_reimbursement_status", "报销状态不合法", ErrInvalidInput)
	}
	if current == desired {
		return "", NewRuleError("reimbursement_status_no_change", "报销状态没有变化", ErrConflict)
	}
	switch {
	case current == ReimbursementStatusSubmitted && desired == ReimbursementStatusReimbursed:
		return "mark_reimbursed", nil
	case current == ReimbursementStatusSubmitted && desired == ReimbursementStatusRejected:
		return "reject", nil
	case (current == ReimbursementStatusReimbursed || current == ReimbursementStatusRejected) && desired == ReimbursementStatusSubmitted:
		return "reopen", nil
	default:
		return "", NewRuleError("invalid_reimbursement_transition", "报销状态不能直接进行该变化", ErrConflict)
	}
}

func (status ReimbursementStatus) Valid() bool {
	return status == ReimbursementStatusSubmitted ||
		status == ReimbursementStatusReimbursed ||
		status == ReimbursementStatusRejected
}

func validateReimbursementTrip(trip ReimbursementTripSnapshot) error {
	if trip.ID == "" || strings.TrimSpace(trip.ID) != trip.ID ||
		trip.Name == "" || strings.TrimSpace(trip.Name) != trip.Name {
		return NewRuleError("invalid_reimbursement_trip", "报销 Trip 快照不合法", ErrInvalidInput)
	}
	start, startErr := time.Parse("2006-01-02", trip.StartDate)
	end, endErr := time.Parse("2006-01-02", trip.EndDate)
	if startErr != nil || endErr != nil || end.Before(start) {
		return NewRuleError("invalid_reimbursement_trip", "报销 Trip 日期不合法", ErrInvalidInput)
	}
	return nil
}

func canonicalReimbursementItems(
	input []ReimbursementPolicyItem,
) ([]ReimbursementPolicyItem, map[string]ReimbursementPolicyItem, error) {
	if len(input) < 1 || len(input) > MaxReimbursementItems {
		return nil, nil, NewRuleError("invalid_reimbursement_selection", "报销选择必须包含 1 至 200 个行程归属项", ErrInvalidInput)
	}
	items := append([]ReimbursementPolicyItem(nil), input...)
	byAssignment := make(map[string]struct{}, len(items))
	byFact := make(map[string]ReimbursementPolicyItem, len(items))
	for _, item := range items {
		if item.AssignmentID == "" || strings.TrimSpace(item.AssignmentID) != item.AssignmentID ||
			!ValidTripAssignmentFactType(item.FactType) || item.FactID == "" ||
			strings.TrimSpace(item.FactID) != item.FactID || item.DisplayName == "" ||
			strings.TrimSpace(item.DisplayName) != item.DisplayName {
			return nil, nil, NewRuleError("invalid_reimbursement_item", "报销项目身份不合法", ErrInvalidInput)
		}
		if _, err := time.Parse("2006-01-02", item.BusinessDate); err != nil {
			return nil, nil, NewRuleError("invalid_reimbursement_item", "报销项目业务日期不合法", ErrInvalidInput)
		}
		if err := (Money{MinorUnits: item.AmountMinor, Currency: item.Currency}).Validate(); err != nil {
			return nil, nil, NewRuleError("invalid_reimbursement_item", "报销项目金额或币种不合法", ErrInvalidInput)
		}
		factKey := reimbursementFactKey(item.FactType, item.FactID)
		if _, exists := byAssignment[item.AssignmentID]; exists {
			return nil, nil, NewRuleError("duplicate_reimbursement_assignment", "报销选择不能包含重复行程归属", ErrInvalidInput)
		}
		if _, exists := byFact[factKey]; exists {
			return nil, nil, NewRuleError("duplicate_reimbursement_fact", "同一 Fact 不能在一次报销中重复", ErrInvalidInput)
		}
		byAssignment[item.AssignmentID] = struct{}{}
		byFact[factKey] = item
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].FactType != items[right].FactType {
			return items[left].FactType < items[right].FactType
		}
		if items[left].FactID != items[right].FactID {
			return items[left].FactID < items[right].FactID
		}
		return items[left].AssignmentID < items[right].AssignmentID
	})
	return items, byFact, nil
}

func canonicalReimbursementLinks(
	input []ReimbursementPolicyLink,
	items map[string]ReimbursementPolicyItem,
) ([]ReimbursementPolicyLink, map[string]int64, map[string]int, map[string][]ReimbursementPolicyLink, error) {
	links := append([]ReimbursementPolicyLink(nil), input...)
	sort.Slice(links, func(left, right int) bool { return links[left].ID < links[right].ID })
	totals := make(map[string]int64, len(items))
	counts := make(map[string]int, len(items))
	linksByFact := make(map[string][]ReimbursementPolicyLink, len(items))
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		payment, paymentExists := items[reimbursementFactKey(DocumentPayment, link.PaymentID)]
		invoice, invoiceExists := items[reimbursementFactKey(DocumentInvoice, link.InvoiceID)]
		if link.ID == "" || !paymentExists || !invoiceExists ||
			link.AllocatedMinor < 1 || link.AllocatedMinor > MaxSafeMinorUnits ||
			link.Currency != payment.Currency || link.Currency != invoice.Currency {
			return nil, nil, nil, nil, NewRuleError("invalid_reimbursement_link", "报销政策 Link 输入不合法", ErrInvalidInput)
		}
		if _, exists := seen[link.ID]; exists {
			return nil, nil, nil, nil, NewRuleError("invalid_reimbursement_link", "报销政策 Link 不能重复", ErrInvalidInput)
		}
		seen[link.ID] = struct{}{}
		for _, key := range []string{
			reimbursementFactKey(DocumentPayment, link.PaymentID),
			reimbursementFactKey(DocumentInvoice, link.InvoiceID),
		} {
			if totals[key] > MaxSafeMinorUnits-link.AllocatedMinor {
				return nil, nil, nil, nil, NewRuleError("reimbursement_amount_overflow", "报销政策金额合计超出安全范围", ErrConflict)
			}
			totals[key] += link.AllocatedMinor
			counts[key]++
			linksByFact[key] = append(linksByFact[key], link)
		}
	}
	return links, totals, counts, linksByFact, nil
}

func canonicalReimbursementPriorUses(
	input []ReimbursementPriorUse,
	items map[string]ReimbursementPolicyItem,
) ([]ReimbursementPriorUse, error) {
	result := append([]ReimbursementPriorUse(nil), input...)
	seen := make(map[string]struct{}, len(result))
	for _, prior := range result {
		if _, exists := items[reimbursementFactKey(prior.FactType, prior.FactID)]; !exists ||
			prior.ReimbursementID == "" ||
			(prior.Status != ReimbursementStatusSubmitted && prior.Status != ReimbursementStatusReimbursed) {
			return nil, NewRuleError("invalid_reimbursement_prior_use", "重复报销政策输入不合法", ErrInvalidInput)
		}
		key := reimbursementFactKey(prior.FactType, prior.FactID) + "\x00" + prior.ReimbursementID
		if _, exists := seen[key]; exists {
			return nil, NewRuleError("invalid_reimbursement_prior_use", "重复报销政策输入不能重复", ErrInvalidInput)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].FactType != result[right].FactType {
			return result[left].FactType < result[right].FactType
		}
		if result[left].FactID != result[right].FactID {
			return result[left].FactID < result[right].FactID
		}
		return result[left].ReimbursementID < result[right].ReimbursementID
	})
	return result, nil
}

func newReimbursementFinding(
	code string,
	item ReimbursementPolicyItem,
	expectedMinor, actualMinor *int64,
	currency Currency,
	prior ReimbursementPriorUse,
	relatedLinks []ReimbursementPolicyLink,
) ReimbursementPolicyFinding {
	finding := ReimbursementPolicyFinding{
		Code: code, AssignmentID: item.AssignmentID, FactType: item.FactType, FactID: item.FactID,
		ExpectedMinor: expectedMinor, ActualMinor: actualMinor, Currency: currency,
		RelatedReimbursementID: prior.ReimbursementID, RelatedStatus: prior.Status,
	}
	finding.FindingKey, _ = hashJSON(struct {
		Version                string                    `json:"version"`
		Code                   string                    `json:"code"`
		AssignmentID           string                    `json:"assignment_id"`
		FactType               DocumentType              `json:"fact_type"`
		FactID                 string                    `json:"fact_id"`
		ExpectedMinor          *int64                    `json:"expected_minor"`
		ActualMinor            *int64                    `json:"actual_minor"`
		Currency               Currency                  `json:"currency"`
		RelatedLinks           []ReimbursementPolicyLink `json:"related_links"`
		RelatedReimbursementID string                    `json:"related_reimbursement_id"`
		RelatedStatus          ReimbursementStatus       `json:"related_status"`
	}{
		Version: ReimbursementPolicyVersion, Code: finding.Code,
		AssignmentID: finding.AssignmentID, FactType: finding.FactType, FactID: finding.FactID,
		ExpectedMinor: finding.ExpectedMinor, ActualMinor: finding.ActualMinor, Currency: finding.Currency,
		RelatedLinks:           relatedLinks,
		RelatedReimbursementID: finding.RelatedReimbursementID, RelatedStatus: finding.RelatedStatus,
	})
	return finding
}

func reimbursementCurrencyTotals(items []ReimbursementPolicyItem) ([]ReimbursementCurrencyTotal, error) {
	totals := make(map[Currency]int64)
	for _, item := range items {
		if totals[item.Currency] > MaxSafeMinorUnits-item.AmountMinor {
			return nil, NewRuleError("reimbursement_amount_overflow", "报销币种合计超出安全范围", ErrConflict)
		}
		totals[item.Currency] += item.AmountMinor
	}
	currencies := make([]string, 0, len(totals))
	for currency := range totals {
		currencies = append(currencies, string(currency))
	}
	sort.Strings(currencies)
	result := make([]ReimbursementCurrencyTotal, 0, len(currencies))
	for _, raw := range currencies {
		currency := Currency(raw)
		result = append(result, ReimbursementCurrencyTotal{Currency: currency, AmountMinor: totals[currency]})
	}
	return result, nil
}

func canonicalFindingKeys(input []string) ([]string, error) {
	if len(input) > MaxReimbursementFindings {
		return nil, NewRuleError("invalid_reimbursement_finding_acknowledgement", "报销提示确认不能超过 1000 项", ErrInvalidInput)
	}
	result := append([]string(nil), input...)
	seen := make(map[string]struct{}, len(result))
	for _, key := range result {
		if !ValidSHA256Hex(key) {
			return nil, NewRuleError("invalid_reimbursement_finding_acknowledgement", "报销提示确认 key 不合法", ErrInvalidInput)
		}
		if _, exists := seen[key]; exists {
			return nil, NewRuleError("invalid_reimbursement_finding_acknowledgement", "报销提示确认不能重复", ErrInvalidInput)
		}
		seen[key] = struct{}{}
	}
	sort.Strings(result)
	return result, nil
}

func validateReimbursementReason(reason string) (string, error) {
	trimmed := strings.TrimSpace(reason)
	length := len([]rune(trimmed))
	if length < 1 || length > 500 {
		return "", NewRuleError("invalid_reimbursement_reason", "报销操作理由必须为 1 至 500 个字符", ErrInvalidInput)
	}
	return trimmed, nil
}

func reimbursementFactKey(factType DocumentType, factID string) string {
	return string(factType) + "\x00" + factID
}

func int64Pointer(value int64) *int64 {
	return &value
}
