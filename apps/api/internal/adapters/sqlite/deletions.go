package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (t transaction) DeleteProviderConfig(ctx context.Context, command ports.ProviderDeleteCommand) error {
	deletedAt := command.DeletedAt.UTC().Format(time.RFC3339Nano)
	result, err := t.tx.ExecContext(ctx, `
		UPDATE provider_configs
		SET active = 0, encrypted_api_key = NULL, deleted_at = ?, updated_at = ?, version = version + 1
		WHERE tenant_id = ? AND id = ? AND version = ? AND deleted_at IS NULL
	`, deletedAt, deletedAt, command.TenantID, command.ConfigID, command.ExpectedVersion)
	if err != nil {
		return fmt.Errorf("delete provider config: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrVersionConflict
	}
	metadata, _ := json.Marshal(map[string]int{"deleted_version": command.ExpectedVersion})
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'provider_config_deleted', 'provider_config', ?, ?, ?, ?)
	`,
		command.AuditEventID,
		command.TenantID,
		command.ActorUserID,
		command.ConfigID,
		command.RequestID,
		string(metadata),
		deletedAt,
	); err != nil {
		return fmt.Errorf("insert provider deletion audit: %w", err)
	}
	return nil
}

func (t transaction) DeleteFact(ctx context.Context, command ports.FactDeleteCommand) error {
	if command.FactType != domain.DocumentPayment && command.FactType != domain.DocumentInvoice {
		return domain.ErrInvalidInput
	}
	deletedAt := command.DeletedAt.UTC().Format(time.RFC3339Nano)
	metadata, _ := json.Marshal(map[string]string{"fact_type": string(command.FactType)})
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'fact_deleted', ?, ?, ?, ?, ?)
	`,
		command.AuditEventID,
		command.TenantID,
		command.ActorUserID,
		command.FactType,
		command.FactID,
		command.RequestID,
		string(metadata),
		deletedAt,
	); err != nil {
		return fmt.Errorf("insert fact deletion audit: %w", err)
	}
	var result sql.Result
	var err error
	if command.FactType == domain.DocumentPayment {
		result, err = t.tx.ExecContext(ctx, `
			UPDATE payments
			SET deleted_at = ?, deleted_by_user_id = ?, deletion_audit_event_id = ?, version = version + 1
			WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL
		`, deletedAt, command.ActorUserID, command.AuditEventID, command.TenantID, command.FactID)
	} else {
		result, err = t.tx.ExecContext(ctx, `
			UPDATE invoices
			SET deleted_at = ?, deleted_by_user_id = ?, deletion_audit_event_id = ?, version = version + 1
			WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL
		`, deletedAt, command.ActorUserID, command.AuditEventID, command.TenantID, command.FactID)
	}
	if err != nil {
		return fmt.Errorf("delete %s fact: %w", command.FactType, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect fact deletion: %w", err)
	}
	if affected != 1 {
		return domain.ErrNotFound
	}
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE payment_invoice_links
		SET ended_at = ?, ended_by_audit_event_id = ?
		WHERE tenant_id = ? AND ended_at IS NULL
		  AND ((? = 'payment' AND payment_id = ?) OR (? = 'invoice' AND invoice_id = ?))
	`,
		deletedAt,
		command.AuditEventID,
		command.TenantID,
		command.FactType,
		command.FactID,
		command.FactType,
		command.FactID,
	); err != nil {
		return fmt.Errorf("end deleted fact links: %w", err)
	}
	return nil
}
