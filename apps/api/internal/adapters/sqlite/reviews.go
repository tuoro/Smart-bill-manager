package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Store) GetReview(ctx context.Context, tenantID, jobID string) (ports.ReviewSnapshot, error) {
	var result ports.ReviewSnapshot
	var jobCreatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT j.id, j.document_id, d.original_name, d.detected_mime, j.status,
		       j.attempt_count, coalesce(j.error_code, ''), coalesce(j.safe_error_message, ''),
		       j.created_at, j.version, d.page_count,
		       c.id, c.origin_ai_run_id, c.document_type, c.revision,
		       c.optimistic_version, c.status
		FROM processing_jobs j
		JOIN documents d ON d.tenant_id = j.tenant_id AND d.id = j.document_id
		JOIN claim_sets c ON c.tenant_id = j.tenant_id AND c.document_id = j.document_id
		WHERE j.tenant_id = ? AND j.id = ?
		  AND j.status IN ('needs_review', 'blocked')
		  AND c.status IN ('ready_for_review', 'blocked')
	`, tenantID, jobID).Scan(
		&result.Job.ID,
		&result.Job.DocumentID,
		&result.Job.OriginalName,
		&result.Job.DetectedMIME,
		&result.Job.Status,
		&result.Job.AttemptCount,
		&result.Job.ErrorCode,
		&result.Job.SafeErrorMessage,
		&jobCreatedAt,
		&result.Job.Version,
		&result.PageCount,
		&result.ClaimSetID,
		&result.OriginAiRunID,
		&result.DocumentType,
		&result.Revision,
		&result.OptimisticVersion,
		&result.Status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ReviewSnapshot{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.ReviewSnapshot{}, fmt.Errorf("read review: %w", err)
	}
	result.DocumentID = result.Job.DocumentID
	result.Job.CreatedAt, err = time.Parse(time.RFC3339Nano, jobCreatedAt)
	if err != nil {
		return ports.ReviewSnapshot{}, fmt.Errorf("parse review job time: %w", err)
	}
	result.Fields, err = s.listReviewFields(ctx, tenantID, result.ClaimSetID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	result.Validations, err = s.listReviewValidations(ctx, tenantID, result.ClaimSetID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	result.Candidates, err = s.listReviewCandidates(ctx, tenantID, result.ClaimSetID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	result.DuplicateCandidates, err = s.listReviewDuplicateCandidates(ctx, tenantID, result.ClaimSetID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	return result, nil
}

func (s *Store) GetClaimSet(ctx context.Context, tenantID, claimSetID string) (ports.ReviewSnapshot, error) {
	var result ports.ReviewSnapshot
	var jobCreatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT j.id, j.document_id, d.original_name, d.detected_mime, j.status,
		       j.attempt_count, coalesce(j.error_code, ''), coalesce(j.safe_error_message, ''),
		       j.created_at, j.version, d.page_count,
		       c.id, c.origin_ai_run_id, c.document_type, c.revision,
		       c.optimistic_version, c.status
		FROM claim_sets c
		JOIN documents d ON d.tenant_id = c.tenant_id AND d.id = c.document_id
		JOIN processing_jobs j ON j.tenant_id = c.tenant_id AND j.document_id = c.document_id
		WHERE c.tenant_id = ? AND c.id = ?
	`, tenantID, claimSetID).Scan(
		&result.Job.ID,
		&result.Job.DocumentID,
		&result.Job.OriginalName,
		&result.Job.DetectedMIME,
		&result.Job.Status,
		&result.Job.AttemptCount,
		&result.Job.ErrorCode,
		&result.Job.SafeErrorMessage,
		&jobCreatedAt,
		&result.Job.Version,
		&result.PageCount,
		&result.ClaimSetID,
		&result.OriginAiRunID,
		&result.DocumentType,
		&result.Revision,
		&result.OptimisticVersion,
		&result.Status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ReviewSnapshot{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.ReviewSnapshot{}, fmt.Errorf("read claim set: %w", err)
	}
	result.DocumentID = result.Job.DocumentID
	result.Job.CreatedAt, err = time.Parse(time.RFC3339Nano, jobCreatedAt)
	if err != nil {
		return ports.ReviewSnapshot{}, fmt.Errorf("parse claim job time: %w", err)
	}
	result.Fields, err = s.listReviewFields(ctx, tenantID, result.ClaimSetID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	result.Validations, err = s.listReviewValidations(ctx, tenantID, result.ClaimSetID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	result.Candidates, err = s.listReviewCandidates(ctx, tenantID, result.ClaimSetID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	result.DuplicateCandidates, err = s.listReviewDuplicateCandidates(ctx, tenantID, result.ClaimSetID)
	if err != nil {
		return ports.ReviewSnapshot{}, err
	}
	return result, nil
}

func (s *Store) listReviewFields(ctx context.Context, tenantID, claimSetID string) ([]ports.ReviewField, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, field_path, value_type, presence, typed_value_json,
		       normalized_value, source, coalesce(source_user_id, '')
		FROM field_claims
		WHERE tenant_id = ? AND claim_set_id = ?
		ORDER BY field_path
	`, tenantID, claimSetID)
	if err != nil {
		return nil, fmt.Errorf("list review fields: %w", err)
	}
	fields := make([]ports.ReviewField, 0)
	fieldIndexes := make(map[string]int)
	for rows.Next() {
		var field ports.ReviewField
		var value, normalized sql.NullString
		if err := rows.Scan(
			&field.ID,
			&field.Path,
			&field.ValueType,
			&field.Presence,
			&value,
			&normalized,
			&field.Source,
			&field.SourceUserID,
		); err != nil {
			return nil, fmt.Errorf("scan review field: %w", err)
		}
		if value.Valid {
			field.Value = json.RawMessage(value.String)
		}
		if normalized.Valid {
			field.NormalizedValue = json.RawMessage(normalized.String)
		}
		field.Evidence = []ports.ReviewEvidence{}
		fieldIndexes[field.ID] = len(fields)
		fields = append(fields, field)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate review fields: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close review fields: %w", err)
	}
	if len(fields) == 0 {
		return fields, nil
	}

	arguments := make([]any, 0, len(fields)+1)
	arguments = append(arguments, tenantID)
	placeholders := make([]string, 0, len(fields))
	for _, field := range fields {
		placeholders = append(placeholders, "?")
		arguments = append(arguments, field.ID)
	}
	evidenceQuery := `
		SELECT e.field_claim_id, e.id, e.document_page_id, p.page_number,
		       coalesce(e.quote, ''), e.region_json
		FROM evidence e
		JOIN document_pages p ON p.tenant_id = e.tenant_id AND p.id = e.document_page_id
		WHERE e.tenant_id = ? AND e.field_claim_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY e.field_claim_id, p.page_number, e.id
	`
	evidenceRows, err := s.db.QueryContext(ctx, evidenceQuery, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list review evidence: %w", err)
	}
	defer evidenceRows.Close()
	for evidenceRows.Next() {
		var fieldID string
		var item ports.ReviewEvidence
		var region sql.NullString
		if err := evidenceRows.Scan(&fieldID, &item.ID, &item.DocumentPageID, &item.Page, &item.Quote, &region); err != nil {
			return nil, fmt.Errorf("scan field evidence: %w", err)
		}
		if region.Valid {
			item.Region = json.RawMessage(region.String)
		}
		index, exists := fieldIndexes[fieldID]
		if !exists {
			return nil, errors.New("review evidence query returned an unexpected field")
		}
		fields[index].Evidence = append(fields[index].Evidence, item)
	}
	if err := evidenceRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review evidence: %w", err)
	}
	return fields, nil
}

func (s *Store) listReviewValidations(ctx context.Context, tenantID, claimSetID string) ([]ports.ReviewValidation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, coalesce(field_claim_id, ''), rule_code, severity, status, safe_message
		FROM validation_results
		WHERE tenant_id = ? AND claim_set_id = ?
		ORDER BY CASE status WHEN 'blocked' THEN 0 WHEN 'error' THEN 1 WHEN 'warning' THEN 2 ELSE 3 END,
		         rule_code, id
	`, tenantID, claimSetID)
	if err != nil {
		return nil, fmt.Errorf("list review validations: %w", err)
	}
	defer rows.Close()
	items := make([]ports.ReviewValidation, 0)
	for rows.Next() {
		var item ports.ReviewValidation
		if err := rows.Scan(
			&item.ID,
			&item.FieldClaimID,
			&item.RuleCode,
			&item.Severity,
			&item.Status,
			&item.SafeMessage,
		); err != nil {
			return nil, fmt.Errorf("scan review validation: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listReviewCandidates(ctx context.Context, tenantID, claimSetID string) ([]ports.LinkCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH payment_allocations AS (
		    SELECT tenant_id, payment_id, sum(allocated_minor) AS allocated_minor
		    FROM payment_invoice_links
		    WHERE ended_at IS NULL
		    GROUP BY tenant_id, payment_id
		), invoice_allocations AS (
		    SELECT tenant_id, invoice_id, sum(allocated_minor) AS allocated_minor
		    FROM payment_invoice_links
		    WHERE ended_at IS NULL
		    GROUP BY tenant_id, invoice_id
		)
		SELECT c.id, 'payment', p.id, p.amount_minor,
		       coalesce(a.allocated_minor, 0), p.amount_minor - coalesce(a.allocated_minor, 0),
		       p.currency, p.business_date, '', p.merchant,
		       p.deleted_at IS NULL AND p.amount_minor > coalesce(a.allocated_minor, 0),
		       c.name_exact, c.date_distance_days, c.reason_codes_json
		FROM payment_invoice_link_candidates c
		JOIN payments p ON p.tenant_id = c.tenant_id AND p.id = c.existing_payment_id
		LEFT JOIN payment_allocations a ON a.tenant_id = p.tenant_id AND a.payment_id = p.id
		WHERE c.tenant_id = ? AND c.claim_set_id = ?
		UNION ALL
		SELECT c.id, 'invoice', i.id, i.total_minor,
		       coalesce(a.allocated_minor, 0), i.total_minor - coalesce(a.allocated_minor, 0),
		       i.currency, i.invoice_date, '', i.seller_name,
		       i.deleted_at IS NULL AND i.total_minor > coalesce(a.allocated_minor, 0),
		       c.name_exact, c.date_distance_days, c.reason_codes_json
		FROM payment_invoice_link_candidates c
		JOIN invoices i ON i.tenant_id = c.tenant_id AND i.id = c.existing_invoice_id
		LEFT JOIN invoice_allocations a ON a.tenant_id = i.tenant_id AND a.invoice_id = i.id
		WHERE c.tenant_id = ? AND c.claim_set_id = ?
	`, tenantID, claimSetID, tenantID, claimSetID)
	if err != nil {
		return nil, fmt.Errorf("list review candidates: %w", err)
	}
	defer rows.Close()
	items := make([]ports.LinkCandidate, 0)
	for rows.Next() {
		var item ports.LinkCandidate
		var temporal, timezoneName, reasons string
		if err := rows.Scan(
			&item.ID,
			&item.TargetType,
			&item.TargetID,
			&item.AmountMinor,
			&item.AllocatedMinor,
			&item.RemainingMinor,
			&item.Currency,
			&temporal,
			&timezoneName,
			&item.DisplayName,
			&item.Available,
			&item.NameExact,
			&item.DateDistanceDays,
			&reasons,
		); err != nil {
			return nil, fmt.Errorf("scan review candidate: %w", err)
		}
		item.BusinessDate = temporal
		if err := json.Unmarshal([]byte(reasons), &item.ReasonCodes); err != nil {
			return nil, fmt.Errorf("decode candidate reasons: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review candidates: %w", err)
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].NameExact != items[right].NameExact {
			return items[left].NameExact
		}
		if items[left].DateDistanceDays != items[right].DateDistanceDays {
			return items[left].DateDistanceDays < items[right].DateDistanceDays
		}
		return items[left].TargetID < items[right].TargetID
	})
	return items, nil
}

func (s *Store) GetConfirmReplay(
	ctx context.Context,
	tenantID, jobID, idempotencyKey string,
) (ports.ConfirmReplay, error) {
	var replay ports.ConfirmReplay
	var recordedJobID string
	var linkIDsJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT r.id, j.id, c.id, r.expected_revision, coalesce(r.association_mode, ''),
		       coalesce(r.association_plan_hash, ''), r.duplicate_plan_hash, c.document_type,
		       coalesce(p.id, i.id, trip.id),
		       coalesce((SELECT json_group_array(link_id) FROM (
		           SELECT l.id AS link_id
		           FROM payment_invoice_link_decisions d
		           JOIN payment_invoice_links l
		             ON l.tenant_id = d.tenant_id AND l.link_decision_id = d.id
		           WHERE d.tenant_id = r.tenant_id AND d.review_decision_id = r.id
		           ORDER BY d.candidate_id
		       )), '[]')
		FROM review_decisions r
		JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
		JOIN processing_jobs j ON j.tenant_id = c.tenant_id AND j.document_id = c.document_id
		LEFT JOIN payments p ON p.tenant_id = r.tenant_id AND p.source_review_decision_id = r.id
		LEFT JOIN invoices i ON i.tenant_id = r.tenant_id AND i.source_review_decision_id = r.id
		LEFT JOIN trips trip ON trip.tenant_id = r.tenant_id AND trip.source_review_decision_id = r.id
		WHERE r.tenant_id = ? AND r.idempotency_key = ? AND r.action = 'confirm'
	`, tenantID, idempotencyKey).Scan(
		&replay.Result.ReviewDecisionID,
		&recordedJobID,
		&replay.ClaimSetID,
		&replay.ExpectedRevision,
		&replay.AssociationMode,
		&replay.AllocationPlanHash,
		&replay.DuplicatePlanHash,
		&replay.Result.FactType,
		&replay.Result.FactID,
		&linkIDsJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ConfirmReplay{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.ConfirmReplay{}, fmt.Errorf("read confirm replay: %w", err)
	}
	if recordedJobID != jobID {
		return ports.ConfirmReplay{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于其他请求", domain.ErrConflict)
	}
	if err := json.Unmarshal([]byte(linkIDsJSON), &replay.Result.LinkIDs); err != nil {
		return ports.ConfirmReplay{}, fmt.Errorf("decode confirm replay links: %w", err)
	}
	replay.Result.Replayed = true
	return replay, nil
}

func (s *Store) GetRejectReplay(
	ctx context.Context,
	tenantID, jobID, idempotencyKey string,
) (ports.RejectReplay, error) {
	var replay ports.RejectReplay
	var recordedJobID string
	err := s.db.QueryRowContext(ctx, `
		SELECT j.id, c.id, r.expected_revision, coalesce(r.reason, '')
		FROM review_decisions r
		JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
		JOIN processing_jobs j ON j.tenant_id = c.tenant_id AND j.document_id = c.document_id
		WHERE r.tenant_id = ? AND r.idempotency_key = ? AND r.action = 'reject'
	`, tenantID, idempotencyKey).Scan(
		&recordedJobID,
		&replay.ClaimSetID,
		&replay.ExpectedRevision,
		&replay.Reason,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.RejectReplay{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.RejectReplay{}, fmt.Errorf("read reject replay: %w", err)
	}
	if recordedJobID != jobID {
		return ports.RejectReplay{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于其他请求", domain.ErrConflict)
	}
	return replay, nil
}
