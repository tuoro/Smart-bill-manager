package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

const chatConnectorColumns = `tenant_id, platform, app_key, encrypted_app_secret, detection_status,
	detection_checked_at, detection_safe_message, active, version, updated_by_user_id, created_at, updated_at`

func scanChatConnector(row interface{ Scan(...any) error }) (domain.ChatConnector, error) {
	var c domain.ChatConnector
	var checked sql.NullTime
	err := row.Scan(
		&c.TenantID, &c.Platform, &c.AppKey, &c.EncryptedAppSecret, &c.DetectionStatus,
		&checked, &c.DetectionMessage, &c.Active, &c.Version, &c.UpdatedByUserID, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return domain.ChatConnector{}, err
	}
	if checked.Valid {
		t := checked.Time
		c.DetectionCheckedAt = &t
	}
	return c, nil
}

func (s *Store) GetChatConnector(ctx context.Context, tenantID, platform string) (domain.ChatConnector, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+chatConnectorColumns+` FROM chat_connectors WHERE tenant_id = ? AND platform = ?`,
		tenantID, platform)
	c, err := scanChatConnector(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ChatConnector{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ChatConnector{}, fmt.Errorf("get chat connector: %w", err)
	}
	return c, nil
}

func (s *Store) ListActiveChatConnectors(ctx context.Context) ([]domain.ChatConnector, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+chatConnectorColumns+` FROM chat_connectors WHERE active ORDER BY tenant_id, platform`)
	if err != nil {
		return nil, fmt.Errorf("list active chat connectors: %w", err)
	}
	defer rows.Close()
	result := make([]domain.ChatConnector, 0)
	for rows.Next() {
		c, err := scanChatConnector(rows)
		if err != nil {
			return nil, fmt.Errorf("scan chat connector: %w", err)
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// 保存即重置：检测回到 pending、启用清零、版本递增。
func (t transaction) UpsertChatConnector(ctx context.Context, c domain.ChatConnector) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO chat_connectors
		(tenant_id, platform, app_key, encrypted_app_secret, detection_status, detection_checked_at,
		 detection_safe_message, active, version, updated_by_user_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'pending', NULL, '', FALSE, 1, ?, ?, ?)
		ON CONFLICT (tenant_id, platform) DO UPDATE SET
		 app_key = EXCLUDED.app_key,
		 encrypted_app_secret = EXCLUDED.encrypted_app_secret,
		 detection_status = 'pending', detection_checked_at = NULL, detection_safe_message = '',
		 active = FALSE,
		 version = chat_connectors.version + 1,
		 updated_by_user_id = EXCLUDED.updated_by_user_id,
		 updated_at = EXCLUDED.updated_at`,
		c.TenantID, c.Platform, c.AppKey, c.EncryptedAppSecret, c.UpdatedByUserID, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert chat connector: %w", err)
	}
	return nil
}

func (t transaction) RecordChatConnectorDetection(
	ctx context.Context, tenantID, platform, status, message string, expectedVersion int64, now time.Time,
) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE chat_connectors
		SET detection_status = ?, detection_checked_at = ?, detection_safe_message = ?, updated_at = ?
		WHERE tenant_id = ? AND platform = ? AND version = ?`,
		status, now, message, now, tenantID, platform, expectedVersion)
	if err != nil {
		return fmt.Errorf("record chat connector detection: %w", err)
	}
	return requireOneRow(result, domain.ErrConflict)
}

func (t transaction) SetChatConnectorActive(
	ctx context.Context, tenantID, platform string, active bool, expectedVersion int64, now time.Time,
) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE chat_connectors SET active = ?, updated_at = ?
		WHERE tenant_id = ? AND platform = ? AND version = ?`,
		active, now, tenantID, platform, expectedVersion)
	if err != nil {
		return fmt.Errorf("set chat connector active: %w", err)
	}
	return requireOneRow(result, domain.ErrConflict)
}

func requireOneRow(result sql.Result, missing error) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return missing
	}
	return nil
}
