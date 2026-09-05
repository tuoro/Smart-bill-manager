package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Server) getReviewHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	item, err := s.reviews.Get(request.Context(), tenantContext(principal), request.PathValue("job_id"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, reviewResponse(item))
}

func (s *Server) getClaimSetHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	item, err := s.reviews.GetClaimSet(
		request.Context(),
		tenantContext(principal),
		request.PathValue("claim_set_id"),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, reviewResponse(item))
}

func (s *Server) reviseReviewHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		ExpectedRevision          int                 `json:"expected_revision"`
		ExpectedOptimisticVersion int                 `json:"expected_optimistic_version"`
		DocumentType              domain.DocumentType `json:"document_type"`
		Fields                    []struct {
			Path           string                       `json:"path"`
			ValueType      string                       `json:"value_type"`
			Presence       string                       `json:"presence"`
			Value          json.RawMessage              `json:"value"`
			EvidenceIDs    []string                     `json:"evidence_ids"`
			ManualEvidence []domain.ManualEvidenceInput `json:"manual_evidence"`
		} `json:"fields"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, request, err)
		return
	}
	input := reviews.RevisionInput{
		ExpectedRevision:          body.ExpectedRevision,
		ExpectedOptimisticVersion: body.ExpectedOptimisticVersion,
		DocumentType:              body.DocumentType,
		Fields:                    make([]reviews.RevisionFieldInput, 0, len(body.Fields)),
	}
	for _, field := range body.Fields {
		input.Fields = append(input.Fields, reviews.RevisionFieldInput{
			Path:           field.Path,
			ValueType:      field.ValueType,
			Presence:       field.Presence,
			Value:          field.Value,
			EvidenceIDs:    field.EvidenceIDs,
			ManualEvidence: field.ManualEvidence,
		})
	}
	item, err := s.reviews.Revise(request.Context(), tenantContext(principal), request.PathValue("job_id"), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, reviewResponse(item))
}

func (s *Server) confirmReviewHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		ExpectedRevision     int             `json:"expected_revision"`
		AssociationMode      json.RawMessage `json:"association_mode"`
		Allocations          json.RawMessage `json:"allocations"`
		DuplicateResolutions *[]struct {
			CandidateID string `json:"candidate_id"`
			Action      string `json:"action"`
		} `json:"duplicate_resolutions"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, request, err)
		return
	}
	if body.DuplicateResolutions == nil {
		writeError(response, request, domain.NewRuleError("invalid_duplicate_resolution", "duplicate_resolutions 必须是数组", domain.ErrInvalidInput))
		return
	}
	hasAssociationMode := len(body.AssociationMode) != 0
	hasAllocations := len(body.Allocations) != 0
	if hasAssociationMode != hasAllocations {
		writeError(response, request, domain.NewRuleError(
			"invalid_association",
			"association_mode 与 allocations 必须同时提供或同时省略",
			domain.ErrInvalidInput,
		))
		return
	}
	associationMode := ""
	allocations := make([]struct {
		CandidateID    string `json:"candidate_id"`
		AllocatedMinor int64  `json:"allocated_minor"`
	}, 0)
	if hasAssociationMode {
		if bytes.Equal(bytes.TrimSpace(body.AssociationMode), []byte("null")) ||
			bytes.Equal(bytes.TrimSpace(body.Allocations), []byte("null")) {
			writeError(response, request, domain.NewRuleError(
				"invalid_association",
				"association_mode 必须是非空字符串且 allocations 必须是数组",
				domain.ErrInvalidInput,
			))
			return
		}
		if err := json.Unmarshal(body.AssociationMode, &associationMode); err != nil || associationMode == "" {
			writeError(response, request, domain.NewRuleError("invalid_association", "association_mode 必须是非空字符串", domain.ErrInvalidInput))
			return
		}
		if err := json.Unmarshal(body.Allocations, &allocations); err != nil {
			writeError(response, request, domain.NewRuleError("invalid_association", "allocations 必须是数组", domain.ErrInvalidInput))
			return
		}
	}
	started := time.Now()
	input := reviews.ConfirmInput{
		ExpectedRevision:     body.ExpectedRevision,
		AssociationMode:      associationMode,
		Allocations:          make([]domain.AllocationRequest, 0, len(allocations)),
		DuplicateResolutions: make([]domain.DuplicateResolution, 0, len(*body.DuplicateResolutions)),
		IdempotencyKey:       request.Header.Get("Idempotency-Key"),
		RequestID:            requestIDFromRequest(request),
	}
	for _, resolution := range *body.DuplicateResolutions {
		input.DuplicateResolutions = append(input.DuplicateResolutions, domain.DuplicateResolution{
			CandidateID: resolution.CandidateID,
			Action:      resolution.Action,
		})
	}
	for _, allocation := range allocations {
		input.Allocations = append(input.Allocations, domain.AllocationRequest{
			CandidateID:    allocation.CandidateID,
			AllocatedMinor: allocation.AllocatedMinor,
		})
	}
	result, err := s.reviews.Confirm(request.Context(), tenantContext(principal), request.PathValue("job_id"), input)
	response.Header().Set("Server-Timing", "review-confirm;dur="+strconv.FormatFloat(float64(time.Since(started))/float64(time.Millisecond), 'f', 3, 64))
	if err != nil {
		writeError(response, request, err)
		return
	}
	payload := map[string]any{
		"review_decision_id": result.ReviewDecisionID,
		"fact_type":          result.FactType,
		"fact_id":            result.FactID,
		"link_ids":           result.LinkIDs,
		"replayed":           result.Replayed,
	}
	writeJSON(response, http.StatusOK, payload)
}

func (s *Server) rejectReviewHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	var body struct {
		ExpectedRevision int    `json:"expected_revision"`
		Reason           string `json:"reason"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, request, err)
		return
	}
	if err := s.reviews.Reject(request.Context(), tenantContext(principal), request.PathValue("job_id"), reviews.RejectInput{
		ExpectedRevision: body.ExpectedRevision,
		Reason:           body.Reason,
		IdempotencyKey:   request.Header.Get("Idempotency-Key"),
		RequestID:        requestIDFromRequest(request),
	}); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func reviewResponse(item ports.ReviewSnapshot) map[string]any {
	entryMode := "ai"
	if item.OriginAiRunID == "" {
		entryMode = "manual"
	}
	return map[string]any{
		"entry_mode":           entryMode,
		"job":                  jobResponse(item.Job),
		"claim_set_id":         item.ClaimSetID,
		"document_type":        item.DocumentType,
		"revision":             item.Revision,
		"optimistic_version":   item.OptimisticVersion,
		"claim_status":         item.Status,
		"page_count":           item.PageCount,
		"pages":                reviewPageResponses(item.Pages),
		"invoice_item_spans":   invoiceItemSpanResponses(item.InvoiceItemSpans),
		"fields":               reviewFieldResponses(item.Fields),
		"validations":          validationResponses(item.Validations),
		"candidates":           candidateResponses(item.Candidates),
		"duplicate_candidates": duplicateCandidateResponses(item.DuplicateCandidates),
	}
}

func reviewPageResponses(items []domain.ClaimReviewPage) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, page := range items {
		result = append(result, map[string]any{
			"page_number": page.PageNumber,
			"field_paths": page.FieldPaths,
			"item_keys":   page.ItemKeys,
		})
	}
	return result
}

func invoiceItemSpanResponses(items []domain.InvoiceItemPageSpan) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"item_key":     item.ItemKey,
			"sort_order":   item.SortOrder,
			"page_numbers": item.PageNumbers,
			"cross_page":   item.CrossPage,
		}
		if item.StartPage > 0 {
			entry["start_page"] = item.StartPage
			entry["end_page"] = item.EndPage
		}
		result = append(result, entry)
	}
	return result
}

func reviewFieldResponses(fields []ports.ReviewField) []map[string]any {
	result := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		entry := map[string]any{
			"id":         field.ID,
			"path":       field.Path,
			"value_type": field.ValueType,
			"presence":   field.Presence,
			"source":     field.Source,
			"evidence":   evidenceResponses(field.Evidence),
		}
		if len(field.Value) != 0 {
			entry["value"] = field.Value
		}
		if len(field.NormalizedValue) != 0 {
			entry["normalized_value"] = field.NormalizedValue
		}
		result = append(result, entry)
	}
	return result
}

func evidenceResponses(items []ports.ReviewEvidence) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, evidence := range items {
		entry := map[string]any{"id": evidence.ID, "page": evidence.Page}
		if evidence.Quote != "" {
			entry["quote"] = evidence.Quote
		}
		if len(evidence.Region) != 0 {
			entry["region"] = evidence.Region
		}
		result = append(result, entry)
	}
	return result
}

func validationResponses(items []ports.ReviewValidation) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, validation := range items {
		entry := map[string]any{
			"id":           validation.ID,
			"rule_code":    validation.RuleCode,
			"severity":     validation.Severity,
			"status":       validation.Status,
			"safe_message": validation.SafeMessage,
		}
		if validation.FieldClaimID != "" {
			entry["field_claim_id"] = validation.FieldClaimID
		}
		result = append(result, entry)
	}
	return result
}

func candidateResponses(items []ports.LinkCandidate) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, candidate := range items {
		result = append(result, map[string]any{
			"id":                 candidate.ID,
			"target_type":        candidate.TargetType,
			"target_id":          candidate.TargetID,
			"amount_minor":       candidate.AmountMinor,
			"allocated_minor":    candidate.AllocatedMinor,
			"remaining_minor":    candidate.RemainingMinor,
			"currency":           candidate.Currency,
			"business_date":      candidate.BusinessDate,
			"display_name":       candidate.DisplayName,
			"available":          candidate.Available,
			"name_exact":         candidate.NameExact,
			"date_distance_days": candidate.DateDistanceDays,
			"reason_codes":       candidate.ReasonCodes,
		})
	}
	return result
}

func duplicateCandidateResponses(items []ports.DuplicateCandidate) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, candidate := range items {
		entry := map[string]any{
			"id":           candidate.ID,
			"kind":         candidate.Kind,
			"display_name": candidate.DisplayName,
			"available":    candidate.Available,
			"reason_codes": candidate.ReasonCodes,
		}
		if candidate.ExistingDocumentID != "" {
			entry["existing_document_id"] = candidate.ExistingDocumentID
		}
		if candidate.ExistingPaymentID != "" {
			entry["existing_payment_id"] = candidate.ExistingPaymentID
		}
		if candidate.ExistingInvoiceID != "" {
			entry["existing_invoice_id"] = candidate.ExistingInvoiceID
		}
		if candidate.BusinessDate != "" {
			entry["business_date"] = candidate.BusinessDate
		}
		if candidate.AmountMinor != nil {
			entry["amount_minor"] = *candidate.AmountMinor
		}
		if candidate.CurrentPageNumber != nil {
			entry["current_page_number"] = *candidate.CurrentPageNumber
		}
		if candidate.ExistingPageNumber != nil {
			entry["existing_page_number"] = *candidate.ExistingPageNumber
		}
		if candidate.DHashDistance != nil {
			entry["dhash_distance"] = *candidate.DHashDistance
		}
		if candidate.AHashDistance != nil {
			entry["ahash_distance"] = *candidate.AHashDistance
		}
		result = append(result, entry)
	}
	return result
}
