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

func (t transaction) InsertProviderConfig(ctx context.Context, config ports.ProviderConfig) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO provider_configs (
			id, tenant_id, base_url, encrypted_api_key, model, output_mode,
			capability_status, capability_schema_version, capability_schema_sha256,
			active, version, safe_fingerprint,
			created_by_user_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?)
	`,
		config.ID,
		config.TenantID,
		config.BaseURL,
		config.EncryptedAPIKey,
		config.Model,
		config.OutputMode,
		config.CapabilityStatus,
		config.CapabilitySchemaVersion,
		config.CapabilitySchemaSHA256,
		config.Active,
		config.Version,
		config.SafeFingerprint,
		config.CreatedByUserID,
		config.CreatedAt.UTC().Format(time.RFC3339Nano),
		config.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert provider config: %w", err)
	}
	return nil
}

func (t transaction) RecordProviderCapability(
	ctx context.Context,
	tenantID, configID string,
	expectedVersion int,
	status, safeMessage string,
	providerSchema ports.ProviderSchemaIdentity,
	checkedAt time.Time,
) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE provider_configs
		SET capability_status = ?,
		    active = CASE WHEN ? = 'passed' THEN active ELSE FALSE END,
		    capability_checked_at = ?, capability_safe_message = ?,
		    capability_schema_version = ?, capability_schema_sha256 = ?, updated_at = ?
		WHERE tenant_id = ? AND id = ? AND version = ? AND deleted_at IS NULL
	`,
		status,
		status,
		checkedAt.UTC().Format(time.RFC3339Nano),
		safeMessage,
		providerSchema.Version,
		providerSchema.SHA256,
		checkedAt.UTC().Format(time.RFC3339Nano),
		tenantID,
		configID,
		expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("record provider capability: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrVersionConflict
	}
	return nil
}

func (t transaction) ActivateProviderConfig(
	ctx context.Context,
	tenantID, configID string,
	expectedVersion int,
	providerSchema ports.ProviderSchemaIdentity,
	now time.Time,
) error {
	formattedNow := now.UTC().Format(time.RFC3339Nano)
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE provider_configs SET active = FALSE, updated_at = ?
		WHERE tenant_id = ? AND active = TRUE AND id <> ?
	`, formattedNow, tenantID, configID); err != nil {
		return fmt.Errorf("deactivate provider config: %w", err)
	}
	result, err := t.tx.ExecContext(ctx, `
		UPDATE provider_configs SET active = TRUE, updated_at = ?
		WHERE tenant_id = ? AND id = ? AND version = ?
		  AND capability_status = 'passed'
		  AND capability_schema_version = ? AND capability_schema_sha256 = ?
		  AND deleted_at IS NULL
	`, formattedNow, tenantID, configID, expectedVersion, providerSchema.Version, providerSchema.SHA256)
	if err != nil {
		return fmt.Errorf("activate provider config: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return domain.ErrVersionConflict
	}
	return nil
}

func (s *Store) ListProviderConfigs(ctx context.Context, tenantID string) ([]ports.ProviderConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, base_url, model, output_mode, capability_status,
		       capability_checked_at, coalesce(capability_safe_message, ''),
		       coalesce(capability_schema_version, ''), coalesce(capability_schema_sha256, ''),
		       active, version, safe_fingerprint, created_by_user_id, created_at, updated_at
		FROM provider_configs
		WHERE tenant_id = ? AND deleted_at IS NULL
		ORDER BY active DESC, created_at DESC, id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list provider configs: %w", err)
	}
	defer rows.Close()
	configs := make([]ports.ProviderConfig, 0)
	for rows.Next() {
		config, err := scanProviderConfig(rows, false)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider configs: %w", err)
	}
	return configs, nil
}

func (s *Store) GetProviderConfig(ctx context.Context, tenantID, configID string) (ports.ProviderConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, base_url, encrypted_api_key, model, output_mode, capability_status,
		       capability_checked_at, coalesce(capability_safe_message, ''),
		       coalesce(capability_schema_version, ''), coalesce(capability_schema_sha256, ''),
		       active, version, safe_fingerprint, created_by_user_id, created_at, updated_at
		FROM provider_configs
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL
	`, tenantID, configID)
	config, err := scanProviderConfig(row, true)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ProviderConfig{}, domain.ErrNotFound
	}
	return config, err
}

func scanProviderConfig(source scanner, includeSecret bool) (ports.ProviderConfig, error) {
	var config ports.ProviderConfig
	var checkedAt sql.NullString
	var active bool
	var createdAt, updatedAt string
	destinations := []any{
		&config.ID,
		&config.TenantID,
		&config.BaseURL,
	}
	if includeSecret {
		destinations = append(destinations, &config.EncryptedAPIKey)
	}
	destinations = append(destinations,
		&config.Model,
		&config.OutputMode,
		&config.CapabilityStatus,
		&checkedAt,
		&config.CapabilitySafeMessage,
		&config.CapabilitySchemaVersion,
		&config.CapabilitySchemaSHA256,
		&active,
		&config.Version,
		&config.SafeFingerprint,
		&config.CreatedByUserID,
		&createdAt,
		&updatedAt,
	)
	if err := source.Scan(destinations...); err != nil {
		return ports.ProviderConfig{}, err
	}
	config.Active = active
	var err error
	config.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ports.ProviderConfig{}, fmt.Errorf("parse provider created_at: %w", err)
	}
	config.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return ports.ProviderConfig{}, fmt.Errorf("parse provider updated_at: %w", err)
	}
	if checkedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, checkedAt.String)
		if err != nil {
			return ports.ProviderConfig{}, fmt.Errorf("parse provider capability_checked_at: %w", err)
		}
		config.CapabilityCheckedAt = &parsed
	}
	return config, nil
}
