package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const chatSessionColumns = `platform, external_user_id, tenant_id, user_id, job_id,
	document_name, state, plan_json, expected_revision, reminded, started_at, updated_at`

func scanChatSession(row scanner) (ports.ChatSession, error) {
	var s ports.ChatSession
	var startedAt, updatedAt string
	if err := row.Scan(&s.Platform, &s.ExternalUserID, &s.TenantID, &s.UserID, &s.JobID,
		&s.DocumentName, &s.State, &s.PlanJSON, &s.ExpectedRevision, &s.Reminded, &startedAt, &updatedAt); err != nil {
		return ports.ChatSession{}, err
	}
	var err error
	if s.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt); err != nil {
		return ports.ChatSession{}, fmt.Errorf("parse chat session started_at: %w", err)
	}
	if s.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return ports.ChatSession{}, fmt.Errorf("parse chat session updated_at: %w", err)
	}
	return s, nil
}

func (s *Store) FindChatSession(ctx context.Context, platform, externalUserID string) (ports.ChatSession, error) {
	session, err := scanChatSession(s.db.QueryRowContext(ctx,
		`SELECT `+chatSessionColumns+` FROM chat_sessions WHERE platform = ? AND external_user_id = ?`,
		platform, externalUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ChatSession{}, domain.ErrNotFound
	}
	return session, err
}

// FindChatRecipient 只认聊天投进来的单据：网页上传的同一个人不该收到钉钉回执。
func (s *Store) FindChatRecipient(ctx context.Context, tenantID, jobID string) (ports.ChatRecipient, error) {
	var r ports.ChatRecipient
	err := s.db.QueryRowContext(ctx, `
		SELECT identity.platform, identity.external_user_id, identity.tenant_id, identity.user_id,
		       membership.role, document.original_name, job.status, coalesce(job.safe_error_message, '')
		FROM processing_jobs job
		JOIN documents document ON document.tenant_id = job.tenant_id AND document.id = job.document_id
		JOIN chat_identities identity ON identity.tenant_id = document.tenant_id
		                             AND identity.user_id = document.created_by_user_id
		JOIN memberships membership ON membership.tenant_id = identity.tenant_id
		                           AND membership.user_id = identity.user_id
		                           AND membership.status = 'active'
		WHERE job.tenant_id = ? AND job.id = ? AND document.ingestion_kind = 'dingtalk_message'
	`, tenantID, jobID).Scan(&r.Identity.Platform, &r.Identity.ExternalUserID, &r.Identity.TenantID,
		&r.Identity.UserID, &r.Identity.Role, &r.DocumentName, &r.JobStatus, &r.SafeError)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ChatRecipient{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.ChatRecipient{}, fmt.Errorf("find chat recipient: %w", err)
	}
	return r, nil
}

func (s *Store) ListStaleChatSessions(ctx context.Context, before time.Time, limit int) ([]ports.ChatSession, error) {
	if limit < 1 {
		return nil, domain.ErrInvalidInput
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+chatSessionColumns+` FROM chat_sessions WHERE started_at < ? ORDER BY started_at LIMIT ?`,
		before.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("list stale chat sessions: %w", err)
	}
	defer rows.Close()
	items := make([]ports.ChatSession, 0)
	for rows.Next() {
		item, err := scanChatSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (t transaction) UpsertChatSession(ctx context.Context, s ports.ChatSession) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO chat_sessions (`+chatSessionColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, FALSE, ?, ?)
		ON CONFLICT (platform, external_user_id) DO UPDATE SET
		 tenant_id = EXCLUDED.tenant_id, user_id = EXCLUDED.user_id, job_id = EXCLUDED.job_id,
		 document_name = EXCLUDED.document_name, state = EXCLUDED.state,
		 plan_json = EXCLUDED.plan_json,
		 expected_revision = EXCLUDED.expected_revision, reminded = FALSE,
		 started_at = EXCLUDED.started_at, updated_at = EXCLUDED.updated_at`,
		s.Platform, s.ExternalUserID, s.TenantID, s.UserID, s.JobID,
		s.DocumentName, s.State, planOrEmpty(s.PlanJSON), s.ExpectedRevision,
		s.StartedAt.UTC().Format(time.RFC3339Nano), s.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert chat session: %w", err)
	}
	return nil
}

func planOrEmpty(value string) string {
	if value == "" {
		return "{}"
	}
	return value
}

func (t transaction) MarkChatSessionReminded(ctx context.Context, platform, externalUserID string, now time.Time) error {
	_, err := t.tx.ExecContext(ctx,
		`UPDATE chat_sessions SET reminded = TRUE, updated_at = ? WHERE platform = ? AND external_user_id = ?`,
		now.UTC().Format(time.RFC3339Nano), platform, externalUserID)
	if err != nil {
		return fmt.Errorf("mark chat session reminded: %w", err)
	}
	return nil
}

func (t transaction) DeleteChatSession(ctx context.Context, platform, externalUserID string) error {
	if _, err := t.tx.ExecContext(ctx,
		`DELETE FROM chat_sessions WHERE platform = ? AND external_user_id = ?`, platform, externalUserID); err != nil {
		return fmt.Errorf("delete chat session: %w", err)
	}
	return nil
}
