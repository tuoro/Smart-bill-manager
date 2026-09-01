package postgresqladapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const listVisualDuplicateDocumentsQuery = `
	WITH current_pages AS MATERIALIZED (
		SELECT tenant_id, document_id, visual_fingerprint_version,
		       dhash_band_0, dhash_band_1, dhash_band_2, dhash_band_3
		FROM document_pages
		WHERE tenant_id = ? AND document_id = ?
	), candidate_documents AS (
		SELECT target.document_id
		FROM current_pages current
		JOIN document_pages target
		  ON target.tenant_id = current.tenant_id
		 AND target.visual_fingerprint_version = current.visual_fingerprint_version
		 AND target.dhash_band_0 = current.dhash_band_0
		WHERE target.document_id <> current.document_id
		UNION
		SELECT target.document_id
		FROM current_pages current
		JOIN document_pages target
		  ON target.tenant_id = current.tenant_id
		 AND target.visual_fingerprint_version = current.visual_fingerprint_version
		 AND target.dhash_band_1 = current.dhash_band_1
		WHERE target.document_id <> current.document_id
		UNION
		SELECT target.document_id
		FROM current_pages current
		JOIN document_pages target
		  ON target.tenant_id = current.tenant_id
		 AND target.visual_fingerprint_version = current.visual_fingerprint_version
		 AND target.dhash_band_2 = current.dhash_band_2
		WHERE target.document_id <> current.document_id
		UNION
		SELECT target.document_id
		FROM current_pages current
		JOIN document_pages target
		  ON target.tenant_id = current.tenant_id
		 AND target.visual_fingerprint_version = current.visual_fingerprint_version
		 AND target.dhash_band_3 = current.dhash_band_3
		WHERE target.document_id <> current.document_id
	)
	SELECT page.id, page.document_id, page.page_number, page.width, page.height,
	       page.visual_fingerprint_version, page.dhash64, page.ahash64,
	       page.dhash_band_0, page.dhash_band_1, page.dhash_band_2, page.dhash_band_3
	FROM document_pages page
	JOIN candidate_documents candidate ON candidate.document_id = page.document_id
	WHERE page.tenant_id = ?
	ORDER BY page.document_id, page.page_number
`

func (t transaction) ListVisualDuplicateDocuments(
	ctx context.Context,
	tenantID, documentID string,
) (domain.VisualDocument, []domain.VisualDocument, error) {
	current, err := t.loadVisualDocument(ctx, tenantID, documentID)
	if err != nil {
		return domain.VisualDocument{}, nil, err
	}
	rows, err := t.tx.QueryContext(ctx, listVisualDuplicateDocumentsQuery, tenantID, documentID, tenantID)
	if err != nil {
		return domain.VisualDocument{}, nil, fmt.Errorf("list visual duplicate documents: %w", err)
	}
	defer rows.Close()
	documents := make([]domain.VisualDocument, 0)
	for rows.Next() {
		page, err := scanVisualPage(rows)
		if err != nil {
			return domain.VisualDocument{}, nil, err
		}
		if len(documents) == 0 || documents[len(documents)-1].ID != page.DocumentID {
			documents = append(documents, domain.VisualDocument{ID: page.DocumentID})
		}
		documents[len(documents)-1].Pages = append(documents[len(documents)-1].Pages, page)
	}
	if err := rows.Err(); err != nil {
		return domain.VisualDocument{}, nil, fmt.Errorf("iterate visual duplicate documents: %w", err)
	}
	return current, documents, nil
}

func (t transaction) loadVisualDocument(
	ctx context.Context,
	tenantID, documentID string,
) (domain.VisualDocument, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT id, document_id, page_number, width, height,
		       visual_fingerprint_version, dhash64, ahash64,
		       dhash_band_0, dhash_band_1, dhash_band_2, dhash_band_3
		FROM document_pages
		WHERE tenant_id = ? AND document_id = ?
		ORDER BY page_number
	`, tenantID, documentID)
	if err != nil {
		return domain.VisualDocument{}, fmt.Errorf("load visual document: %w", err)
	}
	defer rows.Close()
	document := domain.VisualDocument{ID: documentID}
	for rows.Next() {
		page, err := scanVisualPage(rows)
		if err != nil {
			return domain.VisualDocument{}, err
		}
		document.Pages = append(document.Pages, page)
	}
	if err := rows.Err(); err != nil {
		return domain.VisualDocument{}, fmt.Errorf("iterate visual document: %w", err)
	}
	if len(document.Pages) == 0 {
		return domain.VisualDocument{}, domain.ErrNotFound
	}
	return document, nil
}

type visualPageScanner interface {
	Scan(dest ...any) error
}

func scanVisualPage(scanner visualPageScanner) (domain.VisualPage, error) {
	var page domain.VisualPage
	if err := scanner.Scan(
		&page.ID,
		&page.DocumentID,
		&page.PageNumber,
		&page.Width,
		&page.Height,
		&page.Fingerprint.Version,
		&page.Fingerprint.DHash64,
		&page.Fingerprint.AHash64,
		&page.Fingerprint.DHashBands[0],
		&page.Fingerprint.DHashBands[1],
		&page.Fingerprint.DHashBands[2],
		&page.Fingerprint.DHashBands[3],
	); err != nil {
		return domain.VisualPage{}, fmt.Errorf("scan visual document page: %w", err)
	}
	return page, nil
}

func (t transaction) ListFieldDuplicateTargets(
	ctx context.Context,
	tenantID string,
	input domain.FieldDuplicateInput,
) ([]domain.FieldDuplicateTarget, error) {
	switch input.DocumentType {
	case domain.DocumentPayment:
		return t.listPaymentDuplicateTargets(ctx, tenantID, input)
	case domain.DocumentInvoice:
		return t.listInvoiceDuplicateTargets(ctx, tenantID, input)
	default:
		return nil, domain.ErrInvalidInput
	}
}

func (t transaction) listPaymentDuplicateTargets(
	ctx context.Context,
	tenantID string,
	input domain.FieldDuplicateInput,
) ([]domain.FieldDuplicateTarget, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT id, amount_minor, currency, merchant, transaction_time, coalesce(order_number, '')
		FROM payments
		WHERE tenant_id = ? AND deleted_at IS NULL AND amount_minor = ? AND currency = ?
		ORDER BY id
	`, tenantID, input.AmountMinor, input.Currency)
	if err != nil {
		return nil, fmt.Errorf("list payment duplicate targets: %w", err)
	}
	defer rows.Close()
	result := make([]domain.FieldDuplicateTarget, 0)
	for rows.Next() {
		item := domain.FieldDuplicateTarget{DocumentType: domain.DocumentPayment}
		if err := rows.Scan(
			&item.ID,
			&item.AmountMinor,
			&item.Currency,
			&item.Merchant,
			&item.TransactionTime,
			&item.OrderNumber,
		); err != nil {
			return nil, fmt.Errorf("scan payment duplicate target: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (t transaction) listInvoiceDuplicateTargets(
	ctx context.Context,
	tenantID string,
	input domain.FieldDuplicateInput,
) ([]domain.FieldDuplicateTarget, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT id, total_minor, currency, invoice_number, invoice_date::text, seller_name, buyer_name
		FROM invoices
		WHERE tenant_id = ? AND deleted_at IS NULL AND total_minor = ? AND currency = ? AND invoice_date = ?
		ORDER BY id
	`, tenantID, input.AmountMinor, input.Currency, input.InvoiceDate)
	if err != nil {
		return nil, fmt.Errorf("list invoice duplicate targets: %w", err)
	}
	defer rows.Close()
	result := make([]domain.FieldDuplicateTarget, 0)
	for rows.Next() {
		item := domain.FieldDuplicateTarget{DocumentType: domain.DocumentInvoice}
		if err := rows.Scan(
			&item.ID,
			&item.AmountMinor,
			&item.Currency,
			&item.InvoiceNumber,
			&item.InvoiceDate,
			&item.SellerName,
			&item.BuyerName,
		); err != nil {
			return nil, fmt.Errorf("scan invoice duplicate target: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) listReviewDuplicateCandidates(
	ctx context.Context,
	tenantID, claimSetID string,
) ([]ports.DuplicateCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT candidate.id, candidate.kind,
		       coalesce(candidate.existing_document_id, ''),
		       coalesce(candidate.existing_payment_id, ''),
		       coalesce(candidate.existing_invoice_id, ''),
		       coalesce(document.original_name, payment.merchant, invoice.seller_name, ''),
		       coalesce(payment.business_date::text, invoice.invoice_date::text, ''),
		       '',
		       coalesce(payment.amount_minor, invoice.total_minor),
		       current_page.page_number, existing_page.page_number,
		       candidate.dhash_distance, candidate.ahash_distance,
		       CASE candidate.kind
		           WHEN 'near_file' THEN document.id IS NOT NULL
		           WHEN 'cross_page' THEN document.id IS NOT NULL
		                                  AND current_page.id IS NOT NULL
		                                  AND existing_page.id IS NOT NULL
		           WHEN 'field_combination' THEN
		               (payment.id IS NOT NULL AND payment.deleted_at IS NULL)
		               OR (invoice.id IS NOT NULL AND invoice.deleted_at IS NULL)
		           ELSE FALSE
		       END,
		       candidate.reason_codes_json
		FROM duplicate_candidates candidate
		LEFT JOIN documents document
		  ON document.tenant_id = candidate.tenant_id
		 AND document.id = candidate.existing_document_id
		LEFT JOIN document_pages current_page
		  ON current_page.tenant_id = candidate.tenant_id
		 AND current_page.id = candidate.current_document_page_id
		LEFT JOIN document_pages existing_page
		  ON existing_page.tenant_id = candidate.tenant_id
		 AND existing_page.id = candidate.existing_document_page_id
		LEFT JOIN payments payment
		  ON payment.tenant_id = candidate.tenant_id
		 AND payment.id = candidate.existing_payment_id
		LEFT JOIN invoices invoice
		  ON invoice.tenant_id = candidate.tenant_id
		 AND invoice.id = candidate.existing_invoice_id
		WHERE candidate.tenant_id = ? AND candidate.claim_set_id = ?
		ORDER BY candidate.kind, candidate.candidate_key
	`, tenantID, claimSetID)
	if err != nil {
		return nil, fmt.Errorf("list review duplicate candidates: %w", err)
	}
	defer rows.Close()
	result := make([]ports.DuplicateCandidate, 0)
	for rows.Next() {
		var item ports.DuplicateCandidate
		var amount, currentPage, existingPage, dhashDistance, ahashDistance sql.NullInt64
		var temporal, timezoneName, reasons string
		if err := rows.Scan(
			&item.ID,
			&item.Kind,
			&item.ExistingDocumentID,
			&item.ExistingPaymentID,
			&item.ExistingInvoiceID,
			&item.DisplayName,
			&temporal,
			&timezoneName,
			&amount,
			&currentPage,
			&existingPage,
			&dhashDistance,
			&ahashDistance,
			&item.Available,
			&reasons,
		); err != nil {
			return nil, fmt.Errorf("scan review duplicate candidate: %w", err)
		}
		item.AmountMinor = duplicateNullableInt64(amount)
		item.CurrentPageNumber = nullableInt(currentPage)
		item.ExistingPageNumber = nullableInt(existingPage)
		item.DHashDistance = nullableInt(dhashDistance)
		item.AHashDistance = nullableInt(ahashDistance)
		item.BusinessDate = temporal
		if err := json.Unmarshal([]byte(reasons), &item.ReasonCodes); err != nil {
			return nil, fmt.Errorf("decode duplicate candidate reasons: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review duplicate candidates: %w", err)
	}
	sort.Slice(result, func(left, right int) bool {
		priority := map[string]int{"near_file": 0, "cross_page": 1, "field_combination": 2}
		if priority[result[left].Kind] != priority[result[right].Kind] {
			return priority[result[left].Kind] < priority[result[right].Kind]
		}
		leftDistance := nullableDistance(result[left].DHashDistance, result[left].AHashDistance)
		rightDistance := nullableDistance(result[right].DHashDistance, result[right].AHashDistance)
		if leftDistance != rightDistance {
			return leftDistance < rightDistance
		}
		leftTarget := result[left].ExistingDocumentID + result[left].ExistingPaymentID + result[left].ExistingInvoiceID
		rightTarget := result[right].ExistingDocumentID + result[right].ExistingPaymentID + result[right].ExistingInvoiceID
		if leftTarget != rightTarget {
			return leftTarget < rightTarget
		}
		leftCurrentPage := nullableIntValue(result[left].CurrentPageNumber)
		rightCurrentPage := nullableIntValue(result[right].CurrentPageNumber)
		if leftCurrentPage != rightCurrentPage {
			return leftCurrentPage < rightCurrentPage
		}
		leftExistingPage := nullableIntValue(result[left].ExistingPageNumber)
		rightExistingPage := nullableIntValue(result[right].ExistingPageNumber)
		if leftExistingPage != rightExistingPage {
			return leftExistingPage < rightExistingPage
		}
		return result[left].ID < result[right].ID
	})
	return result, nil
}

func duplicateNullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func nullableDistance(left, right *int) int {
	if left == nil || right == nil {
		return 0
	}
	return *left + *right
}

func nullableIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func (t transaction) validateDuplicateConfirmation(
	ctx context.Context,
	command ports.ConfirmCommand,
	documentID string,
) error {
	persisted, err := t.loadDuplicateCandidates(ctx, command.TenantID, command.ClaimSetID)
	if err != nil {
		return err
	}
	candidateIDs := make([]string, 0, len(persisted))
	for _, candidate := range persisted {
		candidateIDs = append(candidateIDs, candidate.ID)
	}
	resolutions := make([]domain.DuplicateResolution, 0, len(command.DuplicateDecisions))
	for _, decision := range command.DuplicateDecisions {
		if decision.ID == "" {
			return domain.ErrInvalidInput
		}
		resolutions = append(resolutions, domain.DuplicateResolution{
			CandidateID: decision.CandidateID,
			Action:      decision.Action,
		})
	}
	if err := domain.ValidateDuplicatePlan(candidateIDs, resolutions); err != nil {
		return err
	}
	_, planHash, err := domain.CanonicalDuplicatePlan(resolutions)
	if err != nil {
		return err
	}
	if planHash != command.DuplicatePlanHash {
		return domain.ErrInvalidInput
	}
	current, visualTargets, err := t.ListVisualDuplicateDocuments(ctx, command.TenantID, documentID)
	if err != nil {
		return err
	}
	fieldInput, err := duplicateInputFromConfirmCommand(command)
	if err != nil {
		return err
	}
	var fieldTargets []domain.FieldDuplicateTarget
	if fieldInput != nil {
		fieldTargets, err = t.ListFieldDuplicateTargets(ctx, command.TenantID, *fieldInput)
		if err != nil {
			return err
		}
	}
	specs, err := domain.BuildDuplicateCandidateSpecs(current, visualTargets, fieldInput, fieldTargets)
	if err != nil {
		return err
	}
	if len(specs) > domain.MaxDuplicateCandidates || len(specs) != len(persisted) {
		return staleDuplicateCandidateSet()
	}
	recomputedKeys := make([]string, 0, len(specs))
	for _, spec := range specs {
		key, err := domain.DuplicateCandidateKey(command.TenantID, command.ClaimSetID, spec)
		if err != nil {
			return err
		}
		recomputedKeys = append(recomputedKeys, key)
	}
	sort.Strings(recomputedKeys)
	persistedKeys := make([]string, 0, len(persisted))
	for _, candidate := range persisted {
		persistedKeys = append(persistedKeys, candidate.CandidateKey)
	}
	sort.Strings(persistedKeys)
	for index := range recomputedKeys {
		if recomputedKeys[index] != persistedKeys[index] {
			return staleDuplicateCandidateSet()
		}
	}
	return nil
}

func (t transaction) loadDuplicateCandidates(
	ctx context.Context,
	tenantID, claimSetID string,
) ([]persistedDuplicateCandidate, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT id, candidate_key
		FROM duplicate_candidates
		WHERE tenant_id = ? AND claim_set_id = ?
		ORDER BY id
	`, tenantID, claimSetID)
	if err != nil {
		return nil, fmt.Errorf("load duplicate candidates: %w", err)
	}
	defer rows.Close()
	result := make([]persistedDuplicateCandidate, 0)
	for rows.Next() {
		var item persistedDuplicateCandidate
		if err := rows.Scan(&item.ID, &item.CandidateKey); err != nil {
			return nil, fmt.Errorf("scan duplicate candidate: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate duplicate candidates: %w", err)
	}
	return result, nil
}

func duplicateInputFromConfirmCommand(command ports.ConfirmCommand) (*domain.FieldDuplicateInput, error) {
	if command.Payment != nil && command.Invoice == nil && command.Trip == nil {
		input := domain.FieldDuplicateInput{
			DocumentType:    domain.DocumentPayment,
			AmountMinor:     command.Payment.AmountMinor,
			Currency:        command.Payment.Currency,
			Merchant:        command.Payment.Merchant,
			TransactionTime: command.Payment.TransactionTime,
		}
		if command.Payment.OrderNumber != nil {
			input.OrderNumber = *command.Payment.OrderNumber
		}
		return &input, nil
	}
	if command.Invoice != nil && command.Payment == nil && command.Trip == nil {
		input := domain.FieldDuplicateInput{
			DocumentType:  domain.DocumentInvoice,
			AmountMinor:   command.Invoice.TotalMinor,
			Currency:      command.Invoice.Currency,
			InvoiceNumber: command.Invoice.InvoiceNumber,
			InvoiceDate:   command.Invoice.InvoiceDate,
			SellerName:    command.Invoice.SellerName,
			BuyerName:     command.Invoice.BuyerName,
		}
		return &input, nil
	}
	if command.Trip != nil && command.Payment == nil && command.Invoice == nil {
		return nil, nil
	}
	return nil, domain.ErrInvalidInput
}

func staleDuplicateCandidateSet() error {
	return domain.NewRuleError(
		"duplicate_candidate_set_stale",
		"疑似重复候选集合已变化，请保存当前完整字段为新 revision 后重试",
		domain.ErrConflict,
	)
}
