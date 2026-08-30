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

func (t transaction) PersistRevision(ctx context.Context, command ports.RevisionCommand) error {
	if command.ClaimSet.TenantID != command.TenantID ||
		command.ClaimSet.DocumentID != command.DocumentID ||
		command.ClaimSet.ID == command.PreviousClaimSetID {
		return domain.ErrInvalidInput
	}
	var currentDocumentID string
	var currentRevision, currentVersion int
	var currentStatus domain.ClaimStatus
	err := t.tx.QueryRowContext(ctx, `
		SELECT c.document_id, c.revision, c.optimistic_version, c.status
		FROM claim_sets c
		JOIN processing_jobs j ON j.tenant_id = c.tenant_id AND j.document_id = c.document_id
		WHERE c.tenant_id = ? AND c.id = ? AND j.id = ?
		  AND c.status IN ('ready_for_review', 'blocked')
		  AND j.status IN ('needs_review', 'blocked')
	`, command.TenantID, command.PreviousClaimSetID, command.JobID).Scan(
		&currentDocumentID,
		&currentRevision,
		&currentVersion,
		&currentStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("read current claim revision: %w", err)
	}
	if currentDocumentID != command.DocumentID ||
		currentRevision != command.ExpectedRevision ||
		currentVersion != command.ExpectedOptimisticVersion {
		return domain.ErrVersionConflict
	}
	result, err := t.tx.ExecContext(ctx, `
		UPDATE claim_sets
		SET status = 'superseded', optimistic_version = optimistic_version + 1
		WHERE tenant_id = ? AND id = ? AND revision = ? AND optimistic_version = ?
		  AND status IN ('ready_for_review', 'blocked')
	`,
		command.TenantID,
		command.PreviousClaimSetID,
		command.ExpectedRevision,
		command.ExpectedOptimisticVersion,
	)
	if err != nil {
		return fmt.Errorf("supersede current claim: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrVersionConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO claim_sets (
			id, tenant_id, document_id, origin_ai_run_id, produced_by_ai_run_id,
			revised_by_user_id, document_type, status, revision,
			supersedes_claim_set_id, optimistic_version, created_at
		) VALUES (?, ?, ?, ?, NULL, ?, ?, 'draft', ?, ?, 1, ?)
	`,
		command.ClaimSet.ID,
		command.TenantID,
		command.DocumentID,
		command.ClaimSet.OriginAiRunID,
		command.RevisedByUserID,
		command.ClaimSet.DocumentType,
		command.ExpectedRevision+1,
		command.PreviousClaimSetID,
		command.ClaimSet.CreatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("insert claim revision: %w", err)
	}
	for _, field := range command.Fields {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO field_claims (
				id, tenant_id, claim_set_id, field_path, value_type, presence,
				typed_value_json, normalized_value, source, source_user_id,
				supersedes_field_claim_id, created_at
			) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), ?)
		`,
			field.ID,
			command.TenantID,
			command.ClaimSet.ID,
			field.FieldPath,
			field.ValueType,
			field.Presence,
			field.TypedValueJSON,
			field.NormalizedValue,
			field.Source,
			field.SourceUserID,
			field.SupersedesFieldID,
			field.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("insert revision field %s: %w", field.FieldPath, err)
		}
	}
	for _, evidence := range command.Evidence {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO evidence (
				id, tenant_id, field_claim_id, document_page_id, quote,
				region_json, evidence_hash, copied_from_evidence_id, created_at
			) VALUES (?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), ?)
		`,
			evidence.ID,
			command.TenantID,
			evidence.FieldClaimID,
			evidence.DocumentPageID,
			evidence.Quote,
			evidence.RegionJSON,
			evidence.EvidenceHash,
			evidence.CopiedFromEvidenceID,
			evidence.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("insert revision evidence: %w", err)
		}
	}
	status := command.ClaimSet.Status
	validations := command.Validations
	if command.ClaimSet.DocumentType == domain.DocumentInvoice && command.NormalizedInvoiceNumber != "" {
		var duplicate int
		if err := t.tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM invoices
				WHERE tenant_id = ? AND normalized_invoice_number = ? AND deleted_at IS NULL
			)
		`, command.TenantID, command.NormalizedInvoiceNumber).Scan(&duplicate); err != nil {
			return fmt.Errorf("check revised invoice number: %w", err)
		}
		if duplicate == 1 && command.DuplicateInvoiceValidation != nil {
			status = domain.ClaimBlocked
			validations = append(validations, *command.DuplicateInvoiceValidation)
		}
	}
	for _, validation := range validations {
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO validation_results (
				id, tenant_id, claim_set_id, field_claim_id, rule_code,
				severity, status, safe_message, rule_version, created_at
			) VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?)
		`,
			validation.ID,
			command.TenantID,
			command.ClaimSet.ID,
			validation.FieldClaimID,
			validation.RuleCode,
			validation.Severity,
			validation.Status,
			validation.SafeMessage,
			validation.RuleVersion,
			validation.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("insert revision validation: %w", err)
		}
	}
	if err := t.insertLinkCandidates(ctx, command.Candidates); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE claim_sets SET status = ? WHERE tenant_id = ? AND id = ? AND status = 'draft'
	`, status, command.TenantID, command.ClaimSet.ID); err != nil {
		return fmt.Errorf("publish claim revision: %w", err)
	}
	jobStatus := domain.JobNeedsReview
	if status == domain.ClaimBlocked {
		jobStatus = domain.JobBlocked
	}
	result, err = t.tx.ExecContext(ctx, `
		UPDATE processing_jobs
		SET status = ?, version = version + 1
		WHERE tenant_id = ? AND id = ? AND document_id = ?
		  AND status IN ('needs_review', 'blocked')
	`, jobStatus, command.TenantID, command.JobID, command.DocumentID)
	if err != nil {
		return fmt.Errorf("publish revision job: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE documents SET status = ? WHERE tenant_id = ? AND id = ?
	`, jobStatus, command.TenantID, command.DocumentID); err != nil {
		return fmt.Errorf("publish revision document: %w", err)
	}
	return nil
}
