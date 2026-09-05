package postgresqladapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func lockAccountTenant(ctx context.Context, tx *sql.Tx, tenantID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id = ? FOR UPDATE`, tenantID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

// 在拿齐锁后读取实际时钟，不把密码计算前或事务开始时的时间当作最终有效期。
func accountDatabaseNow(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&raw); err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, raw)
}

func accountAudit(ctx context.Context, tx *sql.Tx, actor ports.AccountActor, id, action, resourceType, resourceID, requestID string, metadata any, now time.Time) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?)`, id, actor.TenantID, actor.UserID, action, resourceType, resourceID, requestID, string(encoded), now)
	return err
}

func accountWriteError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.Code == "23505" || pg.Code == "40001" || pg.Code == "40P01" {
			return domain.ErrConflict
		}
		if pg.Message == "last_active_owner" {
			return domain.NewRuleError("last_active_owner", "工作区必须保留至少一名启用的管理员", domain.ErrConflict)
		}
	}
	return err
}

func (s *Store) CreateInvitation(ctx context.Context, c ports.CreateInvitationCommand) (result ports.InvitationCreated, resultErr error) {
	defer func() { resultErr = accountWriteError(resultErr) }()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := lockAccountTenant(ctx, tx, c.Actor.TenantID); err != nil {
		return result, err
	}
	c.CreatedAt, err = accountDatabaseNow(ctx, tx)
	if err != nil {
		return result, err
	}
	c.ExpiresAt = c.CreatedAt.Add(48 * time.Hour)
	if err := requireAccountOwner(ctx, tx, c.Actor); err != nil {
		return result, err
	}
	var existingID, hash string
	err = tx.QueryRowContext(ctx, `SELECT id, request_hash FROM member_invitations WHERE tenant_id = ? AND idempotency_key = ?`, c.Actor.TenantID, c.IdempotencyKey).Scan(&existingID, &hash)
	if err == nil {
		if hash != c.RequestHash {
			return result, domain.ErrConflict
		}
		result.Invitation, err = scanInvitation(tx.QueryRowContext(ctx, invitationSelect+` WHERE i.tenant_id = ? AND i.id = ?`, c.CreatedAt, c.Actor.TenantID, existingID))
		result.Replayed = true
		return result, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	var memberExists, pendingSameEmail bool
	var pending int
	err = tx.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.tenant_id = ? AND lower(u.email) = ?),
		count(*), coalesce(bool_or(email = ?), false)
		FROM member_invitations WHERE tenant_id = ? AND version = 1 AND expires_at > ?`,
		c.Actor.TenantID, c.Email, c.Email, c.Actor.TenantID, c.CreatedAt).Scan(&memberExists, &pending, &pendingSameEmail)
	if err != nil {
		return result, err
	}
	if memberExists {
		return result, domain.NewRuleError("member_exists", "该邮箱已是本工作区成员，请在成员列表管理", domain.ErrConflict)
	}
	if pendingSameEmail {
		return result, domain.NewRuleError("invitation_exists", "该邮箱已有有效邀请，请先核对或撤销", domain.ErrConflict)
	}
	if pending >= domain.MaxPendingInvitations {
		return result, domain.NewRuleError("invitation_limit", "有效邀请已达到 100 个，请先处理已有邀请", domain.ErrConflict)
	}
	if err := accountAudit(ctx, tx, c.Actor, c.AuditID, "member_invited", "member_invitation", c.ID, c.RequestID,
		map[string]any{"role": c.Role, "reason": c.Reason}, c.CreatedAt); err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO member_invitations (id, tenant_id, email, role, token_hash, created_by_user_id,
		created_at, expires_at, reason, idempotency_key, request_hash, audit_event_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, c.Actor.TenantID, c.Email, c.Role, c.TokenHash,
		c.Actor.UserID, c.CreatedAt, c.ExpiresAt, c.Reason, c.IdempotencyKey, c.RequestHash, c.AuditID)
	if err != nil {
		return result, err
	}
	result.Invitation, err = scanInvitation(tx.QueryRowContext(ctx, invitationSelect+` WHERE i.tenant_id = ? AND i.id = ?`, c.CreatedAt, c.Actor.TenantID, c.ID))
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (s *Store) RevokeInvitation(ctx context.Context, c ports.RevokeInvitationCommand) (result ports.Invitation, resultErr error) {
	defer func() { resultErr = accountWriteError(resultErr) }()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := lockAccountTenant(ctx, tx, c.Actor.TenantID); err != nil {
		return result, err
	}
	if err := requireAccountOwner(ctx, tx, c.Actor); err != nil {
		return result, err
	}
	result, err = scanInvitation(tx.QueryRowContext(ctx, invitationSelect+` WHERE i.tenant_id = ? AND i.id = ? FOR UPDATE OF i`, c.Now, c.Actor.TenantID, c.InvitationID))
	if err != nil {
		return result, err
	}
	if result.Version != c.ExpectedVersion || result.Version != 1 {
		return result, domain.ErrVersionConflict
	}
	if err := requireAccountOwner(ctx, tx, c.Actor); err != nil {
		return result, err
	}
	c.Now, err = accountDatabaseNow(ctx, tx)
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE member_invitations SET version = 2, revoked_at = ?, revoked_by_user_id = ?, revoke_reason = ?
		WHERE tenant_id = ? AND id = ?`, c.Now, c.Actor.UserID, c.Reason, c.Actor.TenantID, c.InvitationID)
	if err != nil {
		return result, err
	}
	if err := accountAudit(ctx, tx, c.Actor, c.AuditID, "member_invitation_revoked", "member_invitation", c.InvitationID, c.RequestID, map[string]string{"reason": c.Reason}, c.Now); err != nil {
		return result, err
	}
	result.Version, result.Status = 2, "revoked"
	return result, tx.Commit()
}

func (s *Store) ConsumeInvitation(ctx context.Context, c ports.ConsumeInvitationCommand) (resultErr error) {
	defer func() { resultErr = accountWriteError(resultErr) }()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockAccountTenant(ctx, tx, c.TenantID); err != nil {
		return err
	}
	invite, err := scanInvitation(tx.QueryRowContext(ctx, invitationSelect+` WHERE i.tenant_id = ? AND i.id = ? AND i.token_hash = ? FOR UPDATE OF i`, c.Now, c.TenantID, c.InvitationID, c.TokenHash))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && invite.Status != "pending") {
		return domain.InvalidInvitation()
	}
	if err != nil {
		return err
	}
	userID := c.ExpectedUserID
	if userID == "" {
		if c.NewUserID == "" || c.NewPasswordHash == "" {
			return domain.ErrInvalidInput
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, c.NewUserID, invite.Email, c.NewPasswordHash, c.DisplayName, c.Now, c.Now)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return domain.NewRuleError("account_changed", "账号状态已变化，请重新检查邀请并验证身份", domain.ErrConflict)
		}
		userID = c.NewUserID
	} else {
		var currentHash string
		err := tx.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ? AND lower(email) = ? FOR UPDATE`, userID, invite.Email).Scan(&currentHash)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrConflict
		}
		if err != nil {
			return err
		}
		if c.ExpectedPasswordHash == "" || currentHash != c.ExpectedPasswordHash {
			return domain.InvalidCredentials()
		}
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id = ? AND user_id = ?)`, c.TenantID, userID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return domain.NewRuleError("member_exists", "该账号已是本工作区成员，请联系管理员管理状态", domain.ErrConflict)
	}
	c.Now, err = accountDatabaseNow(ctx, tx)
	if err != nil {
		return err
	}
	if !invite.ExpiresAt.After(c.Now) {
		return domain.InvalidInvitation()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)`, c.TenantID, userID, invite.Role, c.Now, c.Now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE member_invitations SET version = 2, consumed_at = ?, consumed_by_user_id = ? WHERE tenant_id = ? AND id = ?`, c.Now, userID, c.TenantID, c.InvitationID)
	if err != nil {
		return err
	}
	if err := accountAudit(ctx, tx, ports.AccountActor{TenantID: c.TenantID, UserID: userID}, c.AuditID, "member_joined", "member_invitation", c.InvitationID, c.RequestID, map[string]domain.Role{"role": invite.Role}, c.Now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ChangeMember(ctx context.Context, c ports.ChangeMemberCommand) (result ports.Member, resultErr error) {
	defer func() { resultErr = accountWriteError(resultErr) }()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := lockAccountTenant(ctx, tx, c.Actor.TenantID); err != nil {
		return result, err
	}
	if err := requireAccountOwner(ctx, tx, c.Actor); err != nil {
		return result, err
	}
	result, err = scanMember(tx.QueryRowContext(ctx, memberSelect+` WHERE m.tenant_id = ? AND m.user_id = ? FOR UPDATE OF m`, c.Actor.TenantID, c.UserID))
	if err != nil {
		return result, err
	}
	if result.Version != c.ExpectedVersion {
		return result, domain.ErrVersionConflict
	}
	if result.Role == c.Role && result.Status == c.Status {
		return result, domain.NewRuleError("member_unchanged", "成员角色与状态未改变", domain.ErrInvalidInput)
	}
	if err := requireAccountOwner(ctx, tx, c.Actor); err != nil {
		return result, err
	}
	c.Now, err = accountDatabaseNow(ctx, tx)
	if err != nil {
		return result, err
	}
	if err := accountAudit(ctx, tx, c.Actor, c.AuditID, "member_changed", "membership", c.UserID, c.RequestID,
		map[string]any{"previous_role": result.Role, "previous_status": result.Status, "role": c.Role, "status": c.Status, "version": result.Version + 1, "reason": c.Reason}, c.Now); err != nil {
		return result, err
	}
	err = tx.QueryRowContext(ctx, `UPDATE memberships SET role = ?, status = ?, updated_at = ? WHERE tenant_id = ? AND user_id = ? RETURNING version`,
		c.Role, c.Status, c.Now, c.Actor.TenantID, c.UserID).Scan(&result.Version)
	if err != nil {
		return result, err
	}
	result.Role, result.Status = c.Role, c.Status
	return result, tx.Commit()
}

func (s *Store) ChangePassword(ctx context.Context, c ports.ChangePasswordCommand) (resultErr error) {
	defer func() { resultErr = accountWriteError(resultErr) }()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash string
	err = tx.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ? FOR UPDATE`, c.UserID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if c.ExpectedPasswordHash == "" || hash != c.ExpectedPasswordHash {
		return domain.InvalidCredentials()
	}
	actorKind := "local_operator"
	if c.Action == "password_changed" {
		actorKind = "self"
		var active bool
		err := tx.QueryRowContext(ctx, `SELECT status = 'active' FROM memberships WHERE tenant_id = ? AND user_id = ? FOR UPDATE`, c.TenantID, c.UserID).Scan(&active)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		if !active {
			return domain.ErrUnauthenticated
		}
		var expires string
		var revoked sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT expires_at, revoked_at FROM sessions
			WHERE tenant_id = ? AND user_id = ? AND id = ? FOR UPDATE`, c.TenantID, c.UserID, c.SessionID).Scan(&expires, &revoked)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		current, err := accountDatabaseNow(ctx, tx)
		if err != nil {
			return err
		}
		expiry, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			return err
		}
		if revoked.Valid || !expiry.After(current) {
			return domain.ErrUnauthenticated
		}
	} else if c.Action != "password_recovered" || c.SessionID != "" || c.TenantID != "" {
		return domain.ErrInvalidInput
	}
	if c.NewPasswordHash == "" || c.NewPasswordHash == hash {
		return domain.ErrInvalidInput
	}
	c.Now, err = accountDatabaseNow(ctx, tx)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, c.NewPasswordHash, c.Now, c.UserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_events (id, user_id, actor_kind, action, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`, c.EventID, c.UserID, actorKind, c.Action, c.Reason, c.Now); err != nil {
		return fmt.Errorf("record account event: %w", err)
	}
	return tx.Commit()
}
