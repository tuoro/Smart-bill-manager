package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func (t transaction) RequestJobCancellation(
	ctx context.Context,
	tenantID, jobID, actorUserID, decisionID, idempotencyKey string,
	now time.Time,
) error {
	var status domain.JobStatus
	var documentID string
	if err := t.tx.QueryRowContext(ctx, `
		SELECT status, document_id FROM processing_jobs
		WHERE tenant_id = ? AND id = ?
	`, tenantID, jobID).Scan(&status, &documentID); errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read job for cancellation: %w", err)
	}
	var hasFact int
	if err := t.tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM review_decisions r
			JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
			WHERE r.tenant_id = ? AND c.document_id = ? AND r.action = 'confirm'
		)
	`, tenantID, documentID).Scan(&hasFact); err != nil {
		return fmt.Errorf("inspect job fact: %w", err)
	}
	if !status.CanCancel(hasFact == 1) {
		return domain.NewRuleError("job_not_cancellable", "当前 Job 状态不允许取消", domain.ErrConflict)
	}
	formattedNow := now.UTC().Format(time.RFC3339Nano)
	switch status {
	case domain.JobProcessing:
		result, err := t.tx.ExecContext(ctx, `
			UPDATE processing_jobs
			SET status = 'cancel_requested', cancel_requested_at = ?, version = version + 1
			WHERE tenant_id = ? AND id = ? AND status = 'processing'
		`, formattedNow, tenantID, jobID)
		if err != nil {
			return fmt.Errorf("request running job cancellation: %w", err)
		}
		return requireAffected(result)
	case domain.JobQueued:
		return t.finishCancelledJob(ctx, tenantID, jobID, documentID, formattedNow)
	case domain.JobNeedsReview, domain.JobBlocked:
		var claimSetID string
		var revision int
		if err := t.tx.QueryRowContext(ctx, `
			SELECT id, revision FROM claim_sets
			WHERE tenant_id = ? AND document_id = ? AND status IN ('ready_for_review', 'blocked')
		`, tenantID, documentID).Scan(&claimSetID, &revision); errors.Is(err, sql.ErrNoRows) {
			return domain.ErrConflict
		} else if err != nil {
			return fmt.Errorf("read current claim for cancellation: %w", err)
		}
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO review_decisions (
				id, tenant_id, claim_set_id, actor_user_id, action,
				idempotency_key, expected_revision, created_at
			) VALUES (?, ?, ?, ?, 'cancel', ?, ?, ?)
		`, decisionID, tenantID, claimSetID, actorUserID, idempotencyKey, revision, formattedNow); err != nil {
			return fmt.Errorf("record claim cancellation: %w", err)
		}
		result, err := t.tx.ExecContext(ctx, `
			UPDATE claim_sets SET status = 'cancelled', optimistic_version = optimistic_version + 1
			WHERE tenant_id = ? AND id = ? AND status IN ('ready_for_review', 'blocked')
		`, tenantID, claimSetID)
		if err != nil {
			return fmt.Errorf("cancel current claim: %w", err)
		}
		if err := requireAffected(result); err != nil {
			return domain.ErrConflict
		}
		return t.finishCancelledJob(ctx, tenantID, jobID, documentID, formattedNow)
	default:
		return domain.NewRuleError("job_not_cancellable", "当前 Job 状态不允许取消", domain.ErrConflict)
	}
}

func (t transaction) finishCancelledJob(
	ctx context.Context,
	tenantID, jobID, documentID, formattedNow string,
) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE processing_jobs
		SET status = 'cancelled', cancel_requested_at = coalesce(cancel_requested_at, ?),
		    finished_at = ?, lease_owner = NULL, lease_expires_at = NULL,
		    version = version + 1
		WHERE tenant_id = ? AND id = ? AND status IN ('queued', 'needs_review', 'blocked')
	`, formattedNow, formattedNow, tenantID, jobID)
	if err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE documents SET status = 'cancelled' WHERE tenant_id = ? AND id = ?
	`, tenantID, documentID); err != nil {
		return fmt.Errorf("cancel document: %w", err)
	}
	return nil
}

func (t transaction) RetryJob(ctx context.Context, tenantID, jobID string) error {
	var status domain.JobStatus
	var documentID string
	var hasClaim int
	if err := t.tx.QueryRowContext(ctx, `
		SELECT j.status, j.document_id,
		       EXISTS(SELECT 1 FROM claim_sets c WHERE c.tenant_id = j.tenant_id AND c.document_id = j.document_id)
		FROM processing_jobs j
		WHERE j.tenant_id = ? AND j.id = ?
	`, tenantID, jobID).Scan(&status, &documentID, &hasClaim); errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read job for retry: %w", err)
	}
	if !status.CanRetry(hasClaim == 1) {
		return domain.NewRuleError("job_not_retryable", "仅可重试尚未产生 Claim 的失败 Job", domain.ErrConflict)
	}
	result, err := t.tx.ExecContext(ctx, `
		UPDATE processing_jobs
		SET status = 'queued', lease_owner = NULL, lease_expires_at = NULL,
		    cancel_requested_at = NULL, error_code = NULL, safe_error_message = NULL,
		    finished_at = NULL, version = version + 1
		WHERE tenant_id = ? AND id = ? AND status = 'failed'
	`, tenantID, jobID)
	if err != nil {
		return fmt.Errorf("retry job: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE documents SET status = 'stored' WHERE tenant_id = ? AND id = ?
	`, tenantID, documentID); err != nil {
		return fmt.Errorf("requeue document: %w", err)
	}
	return nil
}
