package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (t transaction) LockManualReview(ctx context.Context, tenantID, jobID string) (ports.ManualReviewState, error) {
	var state ports.ManualReviewState
	err := t.tx.QueryRowContext(ctx, `
		SELECT j.status, j.version, d.id, d.storage_key, d.detected_mime, d.page_count, d.sha256,
		       EXISTS(SELECT 1 FROM claim_sets c WHERE c.tenant_id = j.tenant_id AND c.document_id = j.document_id)
		FROM processing_jobs j JOIN documents d ON d.tenant_id = j.tenant_id AND d.id = j.document_id
		WHERE j.tenant_id = ? AND j.id = ? FOR UPDATE OF j, d
	`, tenantID, jobID).Scan(&state.Status, &state.Version, &state.Document.ID, &state.Document.StorageKey,
		&state.Document.DetectedMIME, &state.Document.PageCount, &state.Document.SHA256, &state.HasClaim)
	if errors.Is(err, sql.ErrNoRows) {
		return state, domain.ErrNotFound
	}
	if err != nil {
		return state, fmt.Errorf("lock manual review job: %w", err)
	}
	state.Document.TenantID = tenantID
	state.Pages, err = readDocumentPages(ctx, t.tx, tenantID, state.Document.ID)
	return state, err
}

func (t transaction) FindManualReviewReplay(ctx context.Context, tenantID, key string) (ports.ManualReviewReplay, error) {
	var replay ports.ManualReviewReplay
	err := t.tx.QueryRowContext(ctx, `
		SELECT c.id, j.id, c.manual_request_hash FROM claim_sets c
		JOIN processing_jobs j ON j.tenant_id = c.tenant_id AND j.document_id = c.document_id
		WHERE c.tenant_id = ? AND c.manual_idempotency_key = ?
	`, tenantID, key).Scan(&replay.ClaimSetID, &replay.JobID, &replay.RequestHash)
	if errors.Is(err, sql.ErrNoRows) {
		return replay, domain.ErrNotFound
	}
	if err != nil {
		return replay, fmt.Errorf("read manual review replay: %w", err)
	}
	return replay, nil
}

func (t transaction) PersistManualReview(ctx context.Context, command ports.ManualReviewCommand) error {
	revision := command.Revision
	result, err := t.tx.ExecContext(ctx, `
		INSERT INTO claim_sets (id, tenant_id, document_id, revised_by_user_id, document_type,
		    status, revision, optimistic_version, manual_reason, manual_idempotency_key, manual_request_hash, created_at)
		SELECT ?, j.tenant_id, j.document_id, ?, ?, 'draft', 1, 1, ?, ?, ?, ?
		FROM processing_jobs j WHERE j.tenant_id = ? AND j.id = ? AND j.document_id = ?
		  AND j.status = 'failed' AND j.version = ?
		  AND NOT EXISTS(SELECT 1 FROM claim_sets c WHERE c.tenant_id = j.tenant_id AND c.document_id = j.document_id)
	`, revision.ClaimSet.ID, revision.RevisedByUserID, revision.ClaimSet.DocumentType,
		command.Reason, command.IdempotencyKey, command.RequestHash, revision.ClaimSet.CreatedAt.UTC().Format(time.RFC3339Nano),
		revision.TenantID, revision.JobID, revision.DocumentID, command.ExpectedJobVersion)
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" && conflict.ConstraintName == "claim_sets_manual_key_idx" {
			return domain.NewRuleError("idempotency_key_conflict", "同一幂等键不能用于不同接管请求", domain.ErrConflict)
		}
		return fmt.Errorf("insert manual review: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrVersionConflict
	}
	if err := t.publishRevisionContents(ctx, revision, []string{"failed"}); err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at)
		VALUES (?, ?, ?, 'manual_review_started', 'claim_set', ?, ?, '{"entry_mode":"manual"}'::jsonb, ?)
	`, command.AuditID, revision.TenantID, revision.RevisedByUserID, revision.ClaimSet.ID, command.AuditID, revision.ClaimSet.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("audit manual review: %w", err)
	}
	return nil
}
