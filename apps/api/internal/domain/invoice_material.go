package domain

import (
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxInvoiceMaterials = 100
const InvoiceMaterialQueryVersion = "invoice-material-query/1"

type InvoiceMaterialRequest struct {
	InvoiceID       string `json:"invoice_id"`
	Action          string `json:"action"`
	DocumentID      string `json:"document_id"`
	LinkID          string `json:"link_id"`
	ExpectedVersion int    `json:"expected_version"`
	Reason          string `json:"reason"`
	IdempotencyKey  string `json:"idempotency_key"`
	UploadSHA256    string `json:"upload_sha256"`
	UploadName      string `json:"upload_name"`
	UploadMIME      string `json:"upload_mime"`
}

func CanonicalInvoiceMaterialRequest(input InvoiceMaterialRequest) (InvoiceMaterialRequest, string, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.InvoiceID == "" || strings.TrimSpace(input.InvoiceID) != input.InvoiceID || input.ExpectedVersion < 1 ||
		!utf8.ValidString(input.Reason) || len([]rune(input.Reason)) < 1 || len([]rune(input.Reason)) > 500 ||
		len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 || strings.TrimSpace(input.IdempotencyKey) != input.IdempotencyKey {
		return input, "", NewRuleError("invalid_invoice_material_request", "请核对发票版本、操作理由（1 至 500 字符）和请求标识", ErrInvalidInput)
	}
	valid := false
	switch input.Action {
	case "add":
		valid = input.DocumentID != "" && strings.TrimSpace(input.DocumentID) == input.DocumentID && input.LinkID == "" && input.UploadSHA256 == "" && input.UploadName == "" && input.UploadMIME == ""
	case "remove":
		valid = input.LinkID != "" && strings.TrimSpace(input.LinkID) == input.LinkID && input.DocumentID == "" && input.UploadSHA256 == "" && input.UploadName == "" && input.UploadMIME == ""
	case "upload":
		valid = input.DocumentID == "" && input.LinkID == "" && ValidSHA256Hex(input.UploadSHA256) && input.UploadName != "" && input.UploadMIME != ""
	}
	if !valid {
		return input, "", NewRuleError("invalid_invoice_material_action", "辅助材料操作与目标不一致", ErrInvalidInput)
	}
	hash, err := hashJSON(input)
	return input, hash, err
}

func RequireInvoiceMaterials(tenant TenantContext) error {
	for _, capability := range []Capability{CapabilityFactsRead, CapabilityReviewSourceRead, CapabilityDocumentsProcess} {
		if err := tenant.Require(capability); err != nil {
			return err
		}
	}
	return nil
}

type ReimbursementMaterial struct {
	InvoiceID  string `json:"invoice_id"`
	LinkID     string `json:"link_id"`
	DocumentID string `json:"document_id"`
}

func canonicalReimbursementMaterials(input []ReimbursementMaterial, items map[string]ReimbursementPolicyItem) ([]ReimbursementMaterial, error) {
	if len(input) > MaxInvoiceMaterials*MaxReimbursementItems {
		return nil, ErrInvalidInput
	}
	result := append([]ReimbursementMaterial{}, input...)
	seen := make(map[string]bool, len(input))
	counts := make(map[string]int)
	for _, material := range result {
		_, selected := items[reimbursementFactKey(DocumentInvoice, material.InvoiceID)]
		pair := material.InvoiceID + ":" + material.DocumentID
		if !selected || material.LinkID == "" || material.DocumentID == "" || seen["link:"+material.LinkID] || seen["pair:"+pair] {
			return nil, ErrInvalidInput
		}
		seen["link:"+material.LinkID], seen["pair:"+pair] = true, true
		counts[material.InvoiceID]++
		if counts[material.InvoiceID] > MaxInvoiceMaterials {
			return nil, ErrInvalidInput
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].InvoiceID != result[j].InvoiceID {
			return result[i].InvoiceID < result[j].InvoiceID
		}
		return result[i].LinkID < result[j].LinkID
	})
	return result, nil
}
