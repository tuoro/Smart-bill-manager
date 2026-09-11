package postgresqladapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// GetEmailSourceConnection 是同步用的私有视图，带密文；HTTP 层永远不碰它。
func (s *Store) GetEmailSourceConnection(ctx context.Context, tenantID, sourceID string) (ports.EmailSourceConnection, error) {
	var c ports.EmailSourceConnection
	var uidValidity sql.NullInt64
	var lastUID int64
	var deletedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, id, created_by_user_id, mailbox_address_normalized, imap_host_normalized, imap_port,
		       transport_security, imap_username, encrypted_password, connection_status, sync_enabled,
		       deleted_at, version, sync_uid_validity, sync_last_uid
		FROM email_sources WHERE tenant_id = ? AND id = ?
	`, tenantID, sourceID).Scan(
		&c.TenantID, &c.ID, &c.CreatedByUserID, &c.MailboxAddress, &c.IMAPHost, &c.IMAPPort,
		&c.TransportSecurity, &c.IMAPUsername, &c.EncryptedPassword, &c.ConnectionStatus, &c.SyncEnabled,
		&deletedAt, &c.Version, &uidValidity, &lastUID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.EmailSourceConnection{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.EmailSourceConnection{}, fmt.Errorf("get email source connection: %w", err)
	}
	c.Deleted = deletedAt.Valid
	if uidValidity.Valid {
		c.State.UIDValidity = uint32(uidValidity.Int64)
	}
	c.State.LastUID = uint32(lastUID)
	return c, nil
}

func (s *Store) ListSyncEnabledEmailSources(ctx context.Context) ([]ports.EmailSourceRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id FROM email_sources WHERE sync_enabled AND deleted_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list sync-enabled email sources: %w", err)
	}
	defer rows.Close()
	items := make([]ports.EmailSourceRef, 0)
	for rows.Next() {
		var ref ports.EmailSourceRef
		if err := rows.Scan(&ref.TenantID, &ref.ID); err != nil {
			return nil, err
		}
		items = append(items, ref)
	}
	return items, rows.Err()
}

// SetEmailSourceCredentials 换密码即重置：连接回到待检测、同步关闭。旧检测证明的是旧密码。
func (t transaction) SetEmailSourceCredentials(ctx context.Context, command ports.SetEmailSourceCredentialsCommand) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE email_sources
		SET imap_username = ?, encrypted_password = ?, connection_status = 'pending', connection_checked_at = NULL,
		    connection_safe_message = '', sync_enabled = FALSE, version = version + 1
		WHERE tenant_id = ? AND id = ? AND version = ? AND deleted_at IS NULL
	`, command.IMAPUsername, command.EncryptedPassword, command.TenantID, command.SourceID, command.ExpectedVersion)
	if err != nil {
		return fmt.Errorf("set email source credentials: %w", err)
	}
	return requireOneRow(result, domain.ErrVersionConflict)
}

// RecordEmailSourceConnection 记检测结果。失败时同步一并关掉：密码已经不对了，再跑只会反复失败。
func (t transaction) RecordEmailSourceConnection(
	ctx context.Context, tenantID, sourceID, status, safeMessage string, expectedVersion int, checkedAt time.Time,
) error {
	if status != domain.EmailConnectionPassed && status != domain.EmailConnectionFailed {
		return domain.ErrInvalidInput
	}
	result, err := t.tx.ExecContext(ctx, `
		UPDATE email_sources
		SET connection_status = ?, connection_checked_at = ?, connection_safe_message = ?,
		    sync_enabled = CASE WHEN ? = 'passed' THEN sync_enabled ELSE FALSE END
		WHERE tenant_id = ? AND id = ? AND version = ? AND deleted_at IS NULL
	`, status, checkedAt.UTC().Format(time.RFC3339Nano), truncateSafe(safeMessage), status, tenantID, sourceID, expectedVersion)
	if err != nil {
		return fmt.Errorf("record email source connection: %w", err)
	}
	return requireOneRow(result, domain.ErrVersionConflict)
}

func (t transaction) SetEmailSourceSync(ctx context.Context, tenantID, sourceID string, enabled bool, expectedVersion int, now time.Time) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE email_sources SET sync_enabled = ?, version = version + 1
		WHERE tenant_id = ? AND id = ? AND version = ? AND deleted_at IS NULL
		  AND (NOT ? OR (connection_status = 'passed' AND encrypted_password IS NOT NULL))
	`, enabled, tenantID, sourceID, expectedVersion, enabled)
	if err != nil {
		return fmt.Errorf("set email source sync: %w", err)
	}
	return requireOneRow(result, domain.ErrVersionConflict)
}

// RecordEmailSourceSync 推进游标并记录本轮结果。不改 version：同步是后台自动发生的，
// 不该让用户在页面上的操作因此撞上版本冲突。
func (t transaction) RecordEmailSourceSync(ctx context.Context, tenantID, sourceID string, state ports.MailboxState, safeMessage string, syncedAt time.Time) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE email_sources
		SET sync_uid_validity = ?, sync_last_uid = ?, last_sync_at = ?, last_sync_safe_message = ?
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL
	`, int64(state.UIDValidity), int64(state.LastUID), syncedAt.UTC().Format(time.RFC3339Nano), truncateSafe(safeMessage), tenantID, sourceID)
	if err != nil {
		return fmt.Errorf("record email source sync: %w", err)
	}
	return requireOneRow(result, domain.ErrNotFound)
}

// DeleteEmailSource 软删除：清掉密码、关同步、打删除时间。已归档的邮件和单据留着。
func (t transaction) DeleteEmailSource(ctx context.Context, command ports.DeleteEmailSourceCommand) error {
	deletedAt := command.DeletedAt.UTC().Format(time.RFC3339Nano)
	result, err := t.tx.ExecContext(ctx, `
		UPDATE email_sources
		SET deleted_at = ?, sync_enabled = FALSE, encrypted_password = NULL, version = version + 1
		WHERE tenant_id = ? AND id = ? AND version = ? AND deleted_at IS NULL
	`, deletedAt, command.TenantID, command.SourceID, command.ExpectedVersion)
	if err != nil {
		return fmt.Errorf("delete email source: %w", err)
	}
	if err := requireOneRow(result, domain.ErrVersionConflict); err != nil {
		return err
	}
	metadata, _ := json.Marshal(map[string]int{"deleted_version": command.ExpectedVersion})
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'email_source_deleted', 'email_source', ?, ?, ?::jsonb, ?)
	`, command.AuditEventID, command.TenantID, command.ActorUserID, command.SourceID,
		command.RequestID, string(metadata), deletedAt); err != nil {
		return fmt.Errorf("insert email source deletion audit: %w", err)
	}
	return nil
}

func truncateSafe(value string) string {
	runes := []rune(value)
	if len(runes) > 200 {
		return string(runes[:200])
	}
	return value
}
