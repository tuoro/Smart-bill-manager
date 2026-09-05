package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Store) FindAccountByEmail(ctx context.Context, email string) (ports.AccountUser, error) {
	return findAccount(ctx, s.db, "lower(email) = lower(?)", email)
}
func (s *Store) FindAccountByID(ctx context.Context, id string) (ports.AccountUser, error) {
	return findAccount(ctx, s.db, "id = ?", id)
}
func findAccount(ctx context.Context, query reimbursementQueryer, condition string, value string) (ports.AccountUser, error) {
	var user ports.AccountUser
	err := query.QueryRowContext(ctx, `SELECT id, email, display_name, password_hash FROM users WHERE `+condition, value).
		Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return user, domain.ErrNotFound
	}
	return user, err
}

func requireAccountOwner(ctx context.Context, query reimbursementQueryer, actor ports.AccountActor) error {
	var authorized bool
	err := query.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships m JOIN sessions s
		ON s.tenant_id = m.tenant_id AND s.user_id = m.user_id WHERE m.tenant_id = ? AND m.user_id = ?
		AND m.status = 'active' AND m.role = 'owner' AND s.id = ? AND s.revoked_at IS NULL AND s.expires_at > clock_timestamp())`,
		actor.TenantID, actor.UserID, actor.SessionID).Scan(&authorized)
	if err != nil {
		return err
	}
	if !authorized {
		return domain.ErrForbidden
	}
	return nil
}

const memberSelect = `SELECT m.user_id, u.email, u.display_name, m.role, m.status, m.version, m.created_at
	FROM memberships m JOIN users u ON u.id = m.user_id`

func (s *Store) GetMember(ctx context.Context, actor ports.AccountActor, id string) (ports.Member, error) {
	if err := requireAccountOwner(ctx, s.db, actor); err != nil {
		return ports.Member{}, err
	}
	return scanMember(s.db.QueryRowContext(ctx, memberSelect+` WHERE m.tenant_id = ? AND m.user_id = ?`, actor.TenantID, id))
}

func (s *Store) GetInvitation(ctx context.Context, actor ports.AccountActor, id string, now time.Time) (ports.Invitation, error) {
	if err := requireAccountOwner(ctx, s.db, actor); err != nil {
		return ports.Invitation{}, err
	}
	return scanInvitation(s.db.QueryRowContext(ctx, invitationSelect+` WHERE i.tenant_id = ? AND i.id = ?`, now, actor.TenantID, id))
}

func scanMember(row interface{ Scan(...any) error }) (ports.Member, error) {
	var member ports.Member
	var created string
	err := row.Scan(&member.UserID, &member.Email, &member.DisplayName, &member.Role, &member.Status, &member.Version, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return member, domain.ErrNotFound
	}
	if err == nil {
		member.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	}
	return member, err
}

func (s *Store) ListMembers(ctx context.Context, actor ports.AccountActor, query ports.AccountPageQuery, now time.Time) (ports.MemberPage, error) {
	page := ports.MemberPage{Items: []ports.Member{}}
	if err := requireAccountOwner(ctx, s.db, actor); err != nil {
		return page, err
	}
	statement := memberSelect + ` WHERE m.tenant_id = ?`
	args := []any{actor.TenantID}
	if query.After != nil {
		statement += ` AND (m.created_at, m.user_id) < (?, ?)`
		args = append(args, query.After.CreatedAt, query.After.ID)
	}
	statement += ` ORDER BY m.created_at DESC, m.user_id DESC LIMIT ?`
	args = append(args, query.Limit+1)
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		member, err := scanMember(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, member)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > query.Limit {
		page.Items = page.Items[:query.Limit]
		last := page.Items[len(page.Items)-1]
		page.Next = &domain.FactSortKey{ID: last.UserID, CreatedAt: last.CreatedAt}
	}
	return page, nil
}

const invitationSelect = `SELECT i.id, i.tenant_id, t.name, i.email, i.role, i.version, i.expires_at, i.created_at,
	CASE WHEN i.consumed_at IS NOT NULL THEN 'consumed' WHEN i.revoked_at IS NOT NULL THEN 'revoked'
	WHEN i.expires_at <= ? THEN 'expired' ELSE 'pending' END
	FROM member_invitations i JOIN tenants t ON t.id = i.tenant_id`

func scanInvitation(row interface{ Scan(...any) error }) (ports.Invitation, error) {
	var invite ports.Invitation
	var expires, created string
	err := row.Scan(&invite.ID, &invite.TenantID, &invite.TenantName, &invite.Email, &invite.Role, &invite.Version,
		&expires, &created, &invite.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return invite, domain.ErrNotFound
	}
	if err != nil {
		return invite, err
	}
	invite.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err == nil {
		invite.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	}
	return invite, err
}

func (s *Store) FindInvitation(ctx context.Context, hash string, now time.Time) (ports.Invitation, error) {
	invite, err := scanInvitation(s.db.QueryRowContext(ctx, invitationSelect+` WHERE i.token_hash = ?`, now, hash))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && invite.Status != "pending") {
		return ports.Invitation{}, domain.InvalidInvitation()
	}
	return invite, err
}

func (s *Store) ListInvitations(ctx context.Context, actor ports.AccountActor, query ports.AccountPageQuery, now time.Time) (ports.InvitationPage, error) {
	page := ports.InvitationPage{Items: []ports.Invitation{}}
	if err := requireAccountOwner(ctx, s.db, actor); err != nil {
		return page, err
	}
	statement := invitationSelect + ` WHERE i.tenant_id = ?`
	args := []any{now, actor.TenantID}
	if query.After != nil {
		statement += ` AND (i.created_at, i.id) < (?, ?)`
		args = append(args, query.After.CreatedAt, query.After.ID)
	}
	statement += ` ORDER BY i.created_at DESC, i.id DESC LIMIT ?`
	args = append(args, query.Limit+1)
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		invite, err := scanInvitation(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, invite)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > query.Limit {
		page.Items = page.Items[:query.Limit]
		last := page.Items[len(page.Items)-1]
		page.Next = &domain.FactSortKey{ID: last.ID, CreatedAt: last.CreatedAt}
	}
	return page, nil
}
