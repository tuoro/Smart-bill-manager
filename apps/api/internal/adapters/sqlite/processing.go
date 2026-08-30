package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Store) LeaseNextJob(
	ctx context.Context,
	workerID string,
	now, leaseExpires time.Time,
) (ports.LeasedJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.LeasedJob{}, fmt.Errorf("begin job lease: %w", err)
	}
	defer tx.Rollback()
	formattedNow := now.UTC().Format(time.RFC3339Nano)
	var job ports.LeasedJob
	var previousStatus domain.JobStatus
	var version int
	err = tx.QueryRowContext(ctx, `
		SELECT j.id, j.tenant_id, j.document_id, d.storage_key, d.detected_mime,
		       d.page_count, j.attempt_count, j.status, j.version
		FROM processing_jobs j
		JOIN documents d ON d.tenant_id = j.tenant_id AND d.id = j.document_id
		WHERE j.status = 'queued'
		   OR (j.status = 'processing' AND j.lease_expires_at < ?)
		ORDER BY CASE j.status WHEN 'processing' THEN 0 ELSE 1 END,
		         j.created_at, j.id
		LIMIT 1
	`, formattedNow).Scan(
		&job.ID,
		&job.TenantID,
		&job.DocumentID,
		&job.StorageKey,
		&job.MIME,
		&job.PageCount,
		&job.AttemptCount,
		&previousStatus,
		&version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.LeasedJob{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.LeasedJob{}, fmt.Errorf("select job lease: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE processing_jobs
		SET status = 'processing', attempt_count = attempt_count + 1,
		    lease_owner = ?, lease_expires_at = ?,
		    started_at = coalesce(started_at, ?), error_code = NULL,
		    safe_error_message = NULL, version = version + 1
		WHERE tenant_id = ? AND id = ? AND version = ?
		  AND (status = 'queued' OR (status = 'processing' AND lease_expires_at < ?))
	`, workerID, leaseExpires.UTC().Format(time.RFC3339Nano), formattedNow, job.TenantID, job.ID, version, formattedNow)
	if err != nil {
		return ports.LeasedJob{}, fmt.Errorf("claim job lease: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return ports.LeasedJob{}, domain.ErrConflict
	}
	if previousStatus == domain.JobProcessing {
		if _, err := tx.ExecContext(ctx, `
			UPDATE ai_runs
			SET outcome = 'failed', error_code = 'lease_expired', finished_at = ?
			WHERE tenant_id = ? AND job_id = ? AND outcome = 'running'
		`, formattedNow, job.TenantID, job.ID); err != nil {
			return ports.LeasedJob{}, fmt.Errorf("expire abandoned AI run: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE documents SET status = 'processing'
		WHERE tenant_id = ? AND id = ?
	`, job.TenantID, job.DocumentID); err != nil {
		return ports.LeasedJob{}, fmt.Errorf("mark document processing: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ports.LeasedJob{}, fmt.Errorf("commit job lease: %w", err)
	}
	job.AttemptCount++
	job.LeaseOwner = workerID
	job.LeaseExpires = leaseExpires
	return job, nil
}

func (s *Store) CancellationRequested(ctx context.Context, tenantID, jobID string) (bool, error) {
	var requested int
	err := s.db.QueryRowContext(ctx, `
		SELECT CASE WHEN status = 'cancel_requested' OR cancel_requested_at IS NOT NULL THEN 1 ELSE 0 END
		FROM processing_jobs WHERE tenant_id = ? AND id = ?
	`, tenantID, jobID).Scan(&requested)
	if errors.Is(err, sql.ErrNoRows) {
		return false, domain.ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("inspect job cancellation: %w", err)
	}
	return requested == 1, nil
}

func (s *Store) GetDocumentPages(ctx context.Context, tenantID, documentID string) ([]ports.NormalizedPage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, page_number, derived_image_storage_key, width, height, sha256
		FROM document_pages
		WHERE tenant_id = ? AND document_id = ?
		ORDER BY page_number
	`, tenantID, documentID)
	if err != nil {
		return nil, fmt.Errorf("list document pages: %w", err)
	}
	defer rows.Close()
	pages := make([]ports.NormalizedPage, 0)
	for rows.Next() {
		var page ports.NormalizedPage
		if err := rows.Scan(&page.ID, &page.PageNumber, &page.StorageKey, &page.Width, &page.Height, &page.SHA256); err != nil {
			return nil, fmt.Errorf("scan document page: %w", err)
		}
		page.MIME = "image/png"
		pages = append(pages, page)
	}
	return pages, rows.Err()
}

func (s *Store) GetActiveProviderConfig(ctx context.Context, tenantID string) (ports.ProviderConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, base_url, encrypted_api_key, model, output_mode, capability_status,
		       capability_checked_at, coalesce(capability_safe_message, ''),
		       coalesce(capability_schema_version, ''), coalesce(capability_schema_sha256, ''),
		       active, version, safe_fingerprint, created_by_user_id, created_at, updated_at
		FROM provider_configs
		WHERE tenant_id = ? AND active = 1 AND capability_status = 'passed' AND deleted_at IS NULL
	`, tenantID)
	config, err := scanProviderConfig(row, true)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ProviderConfig{}, domain.ErrNotFound
	}
	return config, err
}

func (t transaction) InsertDocumentPages(ctx context.Context, pages []ports.DocumentPageRecord) error {
	for _, page := range pages {
		_, err := t.tx.ExecContext(ctx, `
			INSERT INTO document_pages (
				id, tenant_id, document_id, page_number, derived_image_storage_key,
				width, height, sha256, processing_version, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (tenant_id, document_id, page_number) DO NOTHING
		`,
			page.ID,
			page.TenantID,
			page.DocumentID,
			page.PageNumber,
			page.StorageKey,
			page.Width,
			page.Height,
			page.SHA256,
			page.ProcessingVersion,
			page.CreatedAt.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("insert document page %d: %w", page.PageNumber, err)
		}
		var existingHash, existingKey string
		if err := t.tx.QueryRowContext(ctx, `
			SELECT sha256, derived_image_storage_key FROM document_pages
			WHERE tenant_id = ? AND document_id = ? AND page_number = ?
		`, page.TenantID, page.DocumentID, page.PageNumber).Scan(&existingHash, &existingKey); err != nil {
			return fmt.Errorf("verify document page %d: %w", page.PageNumber, err)
		}
		if existingHash != page.SHA256 || existingKey != page.StorageKey {
			return domain.ErrConflict
		}
	}
	return nil
}

func (t transaction) InsertAiRun(ctx context.Context, run ports.AiRun) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO ai_runs (
			id, tenant_id, job_id, provider_config_id, provider_config_version,
			provider_config_fingerprint, model, prompt_version, extraction_schema_version,
			provider_schema_version, provider_schema_sha256,
			claim_schema_version, claim_mapper_version,
			input_processing_version, request_hash, outcome, started_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		run.ID,
		run.TenantID,
		run.JobID,
		run.ProviderConfigID,
		run.ProviderConfigVersion,
		run.ProviderConfigFingerprint,
		run.Model,
		run.PromptVersion,
		run.ExtractionSchemaVersion,
		run.ProviderSchemaVersion,
		run.ProviderSchemaSHA256,
		run.ClaimSchemaVersion,
		run.ClaimMapperVersion,
		run.InputProcessingVersion,
		run.RequestHash,
		run.Outcome,
		run.StartedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert AI run: %w", err)
	}
	return nil
}

func (t transaction) CompleteAiRun(ctx context.Context, completion ports.AiRunCompletion) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE ai_runs
		SET response_hash = NULLIF(?, ''), input_tokens = ?, output_tokens = ?, latency_ms = ?,
		    outcome = ?, error_code = NULLIF(?, ''), finished_at = ?
		WHERE tenant_id = ? AND id = ? AND outcome = 'running'
	`,
		completion.ResponseHash,
		completion.InputTokens,
		completion.OutputTokens,
		completion.Latency.Milliseconds(),
		completion.Outcome,
		completion.ErrorCode,
		completion.FinishedAt.UTC().Format(time.RFC3339Nano),
		completion.TenantID,
		completion.AiRunID,
	)
	if err != nil {
		return fmt.Errorf("complete AI run: %w", err)
	}
	return requireAffected(result)
}

func (t transaction) InsertAiRunValidation(ctx context.Context, validation ports.ValidationRecord) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO validation_results (
			id, tenant_id, ai_run_id, rule_code, severity, status,
			safe_message, rule_version, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		validation.ID,
		validation.TenantID,
		validation.AiRunID,
		validation.RuleCode,
		validation.Severity,
		validation.Status,
		validation.SafeMessage,
		validation.RuleVersion,
		validation.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert AI run validation: %w", err)
	}
	return nil
}

func (t transaction) IncrementJobAttempt(ctx context.Context, tenantID, jobID string) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE processing_jobs SET attempt_count = attempt_count + 1, version = version + 1
		WHERE tenant_id = ? AND id = ? AND status = 'processing' AND cancel_requested_at IS NULL
	`, tenantID, jobID)
	if err != nil {
		return fmt.Errorf("increment job attempt: %w", err)
	}
	return requireAffected(result)
}

func (t transaction) InvoiceNumberExists(ctx context.Context, tenantID, normalizedInvoiceNumber string) (bool, error) {
	var exists int
	if err := t.tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM invoices
			WHERE tenant_id = ? AND normalized_invoice_number = ? AND deleted_at IS NULL
		)
	`, tenantID, normalizedInvoiceNumber).Scan(&exists); err != nil {
		return false, fmt.Errorf("check invoice number: %w", err)
	}
	return exists == 1, nil
}

func (t transaction) ListEligibleLinkTargets(
	ctx context.Context,
	tenantID string,
	documentType domain.DocumentType,
	currency string,
) ([]ports.LinkTarget, error) {
	var query string
	switch documentType {
	case domain.DocumentPayment:
		query = `
			SELECT i.id, i.total_minor, coalesce(a.allocated_minor, 0),
			       i.total_minor - coalesce(a.allocated_minor, 0),
			       i.currency, i.invoice_date, i.seller_name, ''
			FROM invoices i
			LEFT JOIN (
			    SELECT tenant_id, invoice_id, sum(allocated_minor) AS allocated_minor
			    FROM payment_invoice_links
			    WHERE ended_at IS NULL
			    GROUP BY tenant_id, invoice_id
			) a ON a.tenant_id = i.tenant_id AND a.invoice_id = i.id
			WHERE i.tenant_id = ? AND i.currency = ? AND i.deleted_at IS NULL
			  AND i.total_minor > coalesce(a.allocated_minor, 0)
			ORDER BY i.id`
	case domain.DocumentInvoice:
		query = `
			SELECT p.id, p.amount_minor, coalesce(a.allocated_minor, 0),
			       p.amount_minor - coalesce(a.allocated_minor, 0),
			       p.currency, p.transaction_time, p.merchant, p.source_timezone
			FROM payments p
			LEFT JOIN (
			    SELECT tenant_id, payment_id, sum(allocated_minor) AS allocated_minor
			    FROM payment_invoice_links
			    WHERE ended_at IS NULL
			    GROUP BY tenant_id, payment_id
			) a ON a.tenant_id = p.tenant_id AND a.payment_id = p.id
			WHERE p.tenant_id = ? AND p.currency = ? AND p.deleted_at IS NULL
			  AND p.amount_minor > coalesce(a.allocated_minor, 0)
			ORDER BY p.id`
	default:
		return []ports.LinkTarget{}, nil
	}
	rows, err := t.tx.QueryContext(ctx, query, tenantID, currency)
	if err != nil {
		return nil, fmt.Errorf("list eligible link targets: %w", err)
	}
	defer rows.Close()
	items := make([]ports.LinkTarget, 0)
	for rows.Next() {
		var item ports.LinkTarget
		var temporal, timezoneName string
		if err := rows.Scan(
			&item.FactID,
			&item.AmountMinor,
			&item.AllocatedMinor,
			&item.RemainingMinor,
			&item.Currency,
			&temporal,
			&item.DisplayName,
			&timezoneName,
		); err != nil {
			return nil, fmt.Errorf("scan eligible link target: %w", err)
		}
		if documentType == domain.DocumentPayment {
			item.DocumentType = domain.DocumentInvoice
			item.BusinessDate = temporal
		} else {
			item.DocumentType = domain.DocumentPayment
			item.BusinessDate, err = paymentBusinessDate(temporal, timezoneName)
			if err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (t transaction) PersistInitialClaim(ctx context.Context, jobID string, bundle ports.ClaimBundle) error {
	claim := bundle.ClaimSet
	var allowed int
	if err := t.tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM processing_jobs
			WHERE tenant_id = ? AND id = ? AND document_id = ?
			  AND status = 'processing' AND cancel_requested_at IS NULL
		)
	`, claim.TenantID, jobID, claim.DocumentID).Scan(&allowed); err != nil {
		return fmt.Errorf("verify claim job: %w", err)
	}
	if allowed != 1 {
		return domain.ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO claim_sets (
			id, tenant_id, document_id, origin_ai_run_id, produced_by_ai_run_id,
			document_type, status, revision, optimistic_version, created_at
		) VALUES (?, ?, ?, ?, ?, ?, 'draft', 1, 1, ?)
	`,
		claim.ID,
		claim.TenantID,
		claim.DocumentID,
		claim.OriginAiRunID,
		claim.OriginAiRunID,
		claim.DocumentType,
		claim.CreatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("insert initial claim set: %w", err)
	}
	for _, field := range bundle.Fields {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO field_claims (
				id, tenant_id, claim_set_id, field_path, value_type, presence,
				typed_value_json, normalized_value, source, created_at
			) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), 'ai', ?)
		`,
			field.ID,
			field.TenantID,
			field.ClaimSetID,
			field.FieldPath,
			field.ValueType,
			field.Presence,
			field.TypedValueJSON,
			field.NormalizedValue,
			field.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("insert field claim %s: %w", field.FieldPath, err)
		}
	}
	for _, evidence := range bundle.Evidence {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO evidence (
				id, tenant_id, field_claim_id, document_page_id, quote,
				region_json, evidence_hash, created_at
			) VALUES (?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)
		`,
			evidence.ID,
			evidence.TenantID,
			evidence.FieldClaimID,
			evidence.DocumentPageID,
			evidence.Quote,
			evidence.RegionJSON,
			evidence.EvidenceHash,
			evidence.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("insert evidence: %w", err)
		}
	}
	for _, validation := range bundle.Validations {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO validation_results (
				id, tenant_id, claim_set_id, field_claim_id, rule_code,
				severity, status, safe_message, rule_version, created_at
			) VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?)
		`,
			validation.ID,
			validation.TenantID,
			validation.ClaimSetID,
			validation.FieldClaimID,
			validation.RuleCode,
			validation.Severity,
			validation.Status,
			validation.SafeMessage,
			validation.RuleVersion,
			validation.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("insert claim validation: %w", err)
		}
	}
	if err := t.insertLinkCandidates(ctx, bundle.Candidates); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE claim_sets SET status = ? WHERE tenant_id = ? AND id = ? AND status = 'draft'
	`, claim.Status, claim.TenantID, claim.ID); err != nil {
		return fmt.Errorf("publish initial claim: %w", err)
	}
	jobStatus := domain.JobNeedsReview
	if claim.Status == domain.ClaimBlocked {
		jobStatus = domain.JobBlocked
	}
	result, err := t.tx.ExecContext(ctx, `
		UPDATE processing_jobs
		SET status = ?, lease_owner = NULL, lease_expires_at = NULL, version = version + 1
		WHERE tenant_id = ? AND id = ? AND status = 'processing' AND cancel_requested_at IS NULL
	`, jobStatus, claim.TenantID, jobID)
	if err != nil {
		return fmt.Errorf("publish claim job: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE documents SET status = ? WHERE tenant_id = ? AND id = ?
	`, jobStatus, claim.TenantID, claim.DocumentID); err != nil {
		return fmt.Errorf("publish claim document: %w", err)
	}
	return nil
}

func (t transaction) insertLinkCandidates(ctx context.Context, candidates []ports.LinkCandidateRecord) error {
	for _, candidate := range candidates {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO payment_invoice_link_candidates (
				id, tenant_id, claim_set_id, existing_payment_id, existing_invoice_id,
				candidate_key, rule_version, reason_codes_json, name_exact,
				date_distance_days, created_at
			) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?)
		`,
			candidate.ID,
			candidate.TenantID,
			candidate.ClaimSetID,
			candidate.ExistingPaymentID,
			candidate.ExistingInvoiceID,
			candidate.CandidateKey,
			candidate.RuleVersion,
			candidate.ReasonCodesJSON,
			candidate.NameExact,
			candidate.DateDistanceDays,
			candidate.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("insert payment/invoice link candidate: %w", err)
		}
	}
	return nil
}

func (t transaction) MarkJobFailed(
	ctx context.Context,
	tenantID, jobID, code, safeMessage string,
	finishedAt time.Time,
) error {
	formatted := finishedAt.UTC().Format(time.RFC3339Nano)
	result, err := t.tx.ExecContext(ctx, `
		UPDATE processing_jobs
		SET status = 'failed', error_code = ?, safe_error_message = ?, finished_at = ?,
		    lease_owner = NULL, lease_expires_at = NULL, version = version + 1
		WHERE tenant_id = ? AND id = ? AND status IN ('processing', 'cancel_requested')
	`, code, safeMessage, formatted, tenantID, jobID)
	if err != nil {
		return fmt.Errorf("mark job failed: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE documents SET status = 'failed'
		WHERE tenant_id = ? AND id = (SELECT document_id FROM processing_jobs WHERE tenant_id = ? AND id = ?)
	`, tenantID, tenantID, jobID); err != nil {
		return fmt.Errorf("mark document failed: %w", err)
	}
	return nil
}

func (t transaction) MarkJobCancelled(ctx context.Context, tenantID, jobID string, finishedAt time.Time) error {
	formatted := finishedAt.UTC().Format(time.RFC3339Nano)
	result, err := t.tx.ExecContext(ctx, `
		UPDATE processing_jobs
		SET status = 'cancelled', finished_at = ?, cancel_requested_at = coalesce(cancel_requested_at, ?),
		    lease_owner = NULL, lease_expires_at = NULL, version = version + 1
		WHERE tenant_id = ? AND id = ? AND status IN ('processing', 'cancel_requested')
	`, formatted, formatted, tenantID, jobID)
	if err != nil {
		return fmt.Errorf("mark job cancelled: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE documents SET status = 'cancelled'
		WHERE tenant_id = ? AND id = (SELECT document_id FROM processing_jobs WHERE tenant_id = ? AND id = ?)
	`, tenantID, tenantID, jobID); err != nil {
		return fmt.Errorf("mark document cancelled: %w", err)
	}
	return nil
}
