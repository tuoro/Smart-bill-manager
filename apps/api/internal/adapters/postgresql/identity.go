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

func (s *Store) BootstrapOwner(ctx context.Context, owner ports.BootstrapOwner) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin bootstrap: %w", err)
	}
	defer tx.Rollback()
	var records int
	if err := tx.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM users)
		     + (SELECT count(*) FROM tenants)
		     + (SELECT count(*) FROM memberships)
	`).Scan(&records); err != nil {
		return fmt.Errorf("inspect bootstrap state: %w", err)
	}
	if records != 0 {
		return domain.ErrBootstrapNotEmpty
	}
	now := owner.CreatedAt.UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, owner.UserID, owner.Email, owner.PasswordHash, owner.DisplayName, now, now); err != nil {
		return fmt.Errorf("create bootstrap user: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, owner.TenantID, owner.TenantName, owner.DefaultCurrency, owner.Timezone, now, now); err != nil {
		return fmt.Errorf("create bootstrap tenant: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES (?, ?, 'owner', 'active', ?, ?)
	`, owner.TenantID, owner.UserID, now, now); err != nil {
		return fmt.Errorf("create bootstrap membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap: %w", err)
	}
	return nil
}

func (s *Store) FindLoginCandidates(ctx context.Context, normalizedEmail string) ([]ports.LoginCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.email, u.display_name, u.password_hash,
		       t.id, t.name, t.default_currency, t.timezone,
		       m.role, m.status
		FROM users u
		JOIN memberships m ON m.user_id = u.id
		JOIN tenants t ON t.id = m.tenant_id
		WHERE lower(u.email) = lower(?)
		ORDER BY t.id
	`, normalizedEmail)
	if err != nil {
		return nil, fmt.Errorf("find login candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]ports.LoginCandidate, 0)
	for rows.Next() {
		var candidate ports.LoginCandidate
		if err := rows.Scan(
			&candidate.UserID,
			&candidate.Email,
			&candidate.DisplayName,
			&candidate.PasswordHash,
			&candidate.TenantID,
			&candidate.TenantName,
			&candidate.Currency,
			&candidate.Timezone,
			&candidate.Role,
			&candidate.Status,
		); err != nil {
			return nil, fmt.Errorf("scan login candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate login candidates: %w", err)
	}
	return candidates, nil
}

func (s *Store) CreateSession(ctx context.Context, session ports.SessionRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (
			id, tenant_id, user_id, token_hash, csrf_token_hash,
			expires_at, created_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		session.ID,
		session.TenantID,
		session.UserID,
		session.TokenHash,
		session.CSRFTokenHash,
		session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		session.CreatedAt.UTC().Format(time.RFC3339Nano),
		session.LastSeenAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) FindSession(ctx context.Context, tokenHash string) (ports.SessionPrincipal, error) {
	var principal ports.SessionPrincipal
	var expiresAt string
	var revokedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.tenant_id, t.name, t.default_currency, t.timezone,
		       s.user_id, u.email, u.display_name, m.role, s.expires_at, s.revoked_at,
		       s.csrf_token_hash
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		JOIN tenants t ON t.id = s.tenant_id
		JOIN memberships m ON m.tenant_id = s.tenant_id AND m.user_id = s.user_id
		WHERE s.token_hash = ? AND m.status = 'active'
	`, tokenHash).Scan(
		&principal.SessionID,
		&principal.TenantID,
		&principal.TenantName,
		&principal.Currency,
		&principal.Timezone,
		&principal.UserID,
		&principal.Email,
		&principal.DisplayName,
		&principal.Role,
		&expiresAt,
		&revokedAt,
		&principal.CSRFTokenHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.SessionPrincipal{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return ports.SessionPrincipal{}, fmt.Errorf("find session: %w", err)
	}
	principal.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return ports.SessionPrincipal{}, fmt.Errorf("parse session expiry: %w", err)
	}
	if revokedAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, revokedAt.String)
		if parseErr != nil {
			return ports.SessionPrincipal{}, fmt.Errorf("parse session revocation: %w", parseErr)
		}
		principal.RevokedAt = &parsed
	}
	return principal, nil
}

func (s *Store) TouchSession(ctx context.Context, tenantID, sessionID string, now time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		"UPDATE sessions SET last_seen_at = ? WHERE tenant_id = ? AND id = ? AND revoked_at IS NULL",
		now.UTC().Format(time.RFC3339Nano),
		tenantID,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) RevokeSession(ctx context.Context, tenantID, sessionID string, now time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		"UPDATE sessions SET revoked_at = ? WHERE tenant_id = ? AND id = ? AND revoked_at IS NULL",
		now.UTC().Format(time.RFC3339Nano),
		tenantID,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return requireAffected(result)
}

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect affected rows: %w", err)
	}
	if affected != 1 {
		return domain.ErrNotFound
	}
	return nil
}
