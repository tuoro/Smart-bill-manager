package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// 关联 memberships 并要求 status = 'active'：成员被停用或移出租户后，映射行
// 可能还在，但他不该再能通过聊天往账目里投单据。把这个条件放在查询里而不是
// 调用方，避免第二处实现漏掉。
func (t transaction) FindChatIdentity(
	ctx context.Context,
	platform, externalUserID string,
) (domain.ChatIdentity, error) {
	identity := domain.ChatIdentity{Platform: platform, ExternalUserID: externalUserID}
	err := t.tx.QueryRowContext(
		ctx,
		`SELECT c.tenant_id, c.user_id, m.role
		FROM chat_identities c
		JOIN memberships m ON m.tenant_id = c.tenant_id AND m.user_id = c.user_id
		WHERE c.platform = ? AND c.external_user_id = ? AND m.status = 'active'`,
		platform,
		externalUserID,
	).Scan(&identity.TenantID, &identity.UserID, &identity.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ChatIdentity{}, domain.ErrChatSenderNotLinked
	}
	if err != nil {
		return domain.ChatIdentity{}, fmt.Errorf("find chat identity: %w", err)
	}
	return identity, nil
}

func (t transaction) InsertChatBindingCode(ctx context.Context, code domain.ChatBindingCode) error {
	_, err := t.tx.ExecContext(
		ctx,
		`INSERT INTO chat_binding_codes
		(id, tenant_id, user_id, platform, code_hash, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		code.ID,
		code.TenantID,
		code.UserID,
		code.Platform,
		code.CodeHash,
		code.CreatedAt,
		code.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert chat binding code: %w", err)
	}
	return nil
}

// 兑换一次做完四件事，任何一步不成立就整体不发生：
// 认领码（未用过、未过期，且发码时那个成员现在仍然在册）、标记已用、清掉该成员
// 在这个平台上的旧绑定、写入新绑定。
//
// 认领用带条件的 UPDATE ... RETURNING：并发两次兑换同一个码时，只有一次能把
// consumed_at 从 NULL 改掉，另一次拿不到行。这比先 SELECT 再 UPDATE 少一个
// 竞态窗口。
func (t transaction) RedeemChatBindingCode(
	ctx context.Context,
	platform, codeHash, externalUserID string,
	now time.Time,
) (domain.ChatIdentity, error) {
	identity := domain.ChatIdentity{Platform: platform, ExternalUserID: externalUserID}
	// 这个外部账号可能已经绑在别的成员名下。直接插入会撞主键，但那样报错含糊，
	// 而且语义上等于让持码人把别人的账号悄悄换到自己名下。明确拒绝，要求先解绑。
	var boundTo string
	err := t.tx.QueryRowContext(
		ctx,
		`SELECT user_id FROM chat_identities WHERE platform = ? AND external_user_id = ?`,
		platform,
		externalUserID,
	).Scan(&boundTo)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.ChatIdentity{}, fmt.Errorf("read existing chat identity: %w", err)
	}
	err = t.tx.QueryRowContext(
		ctx,
		`UPDATE chat_binding_codes
		SET consumed_at = ?, consumed_external_user_id = ?
		WHERE code_hash = ? AND platform = ? AND consumed_at IS NULL AND expires_at > ?
		  AND EXISTS (
		      SELECT 1 FROM memberships m
		      WHERE m.tenant_id = chat_binding_codes.tenant_id
		        AND m.user_id = chat_binding_codes.user_id
		        AND m.status = 'active'
		  )
		RETURNING tenant_id, user_id`,
		now,
		externalUserID,
		codeHash,
		platform,
		now,
	).Scan(&identity.TenantID, &identity.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ChatIdentity{}, domain.InvalidChatBindingCode()
	}
	if err != nil {
		return domain.ChatIdentity{}, fmt.Errorf("claim chat binding code: %w", err)
	}
	if boundTo != "" && boundTo != identity.UserID {
		return domain.ChatIdentity{}, domain.ChatAccountAlreadyBound()
	}
	if _, err := t.tx.ExecContext(
		ctx,
		`DELETE FROM chat_identities WHERE platform = ? AND tenant_id = ? AND user_id = ?`,
		platform,
		identity.TenantID,
		identity.UserID,
	); err != nil {
		return domain.ChatIdentity{}, fmt.Errorf("clear previous chat identity: %w", err)
	}
	if _, err := t.tx.ExecContext(
		ctx,
		`INSERT INTO chat_identities
		(platform, external_user_id, tenant_id, user_id, created_by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		platform,
		externalUserID,
		identity.TenantID,
		identity.UserID,
		identity.UserID,
		now,
	); err != nil {
		return domain.ChatIdentity{}, fmt.Errorf("insert chat identity: %w", err)
	}
	err = t.tx.QueryRowContext(
		ctx,
		`SELECT role FROM memberships WHERE tenant_id = ? AND user_id = ?`,
		identity.TenantID,
		identity.UserID,
	).Scan(&identity.Role)
	if err != nil {
		return domain.ChatIdentity{}, fmt.Errorf("read member role: %w", err)
	}
	return identity, nil
}

func (t transaction) ListChatIdentities(
	ctx context.Context,
	tenantID, userID string,
) ([]domain.ChatBinding, error) {
	rows, err := t.tx.QueryContext(
		ctx,
		`SELECT platform, external_user_id, created_at
		FROM chat_identities WHERE tenant_id = ? AND user_id = ?
		ORDER BY platform`,
		tenantID,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list chat identities: %w", err)
	}
	defer rows.Close()
	bindings := make([]domain.ChatBinding, 0)
	for rows.Next() {
		var binding domain.ChatBinding
		if err := rows.Scan(
			&binding.Platform,
			&binding.ExternalUserID,
			&binding.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan chat identity: %w", err)
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chat identities: %w", err)
	}
	return bindings, nil
}

// 只解绑自己名下的：租户与成员都写进 WHERE，不依赖调用方先查一次。
func (t transaction) DeleteChatIdentity(
	ctx context.Context,
	platform, tenantID, userID string,
) (bool, error) {
	result, err := t.tx.ExecContext(
		ctx,
		`DELETE FROM chat_identities WHERE platform = ? AND tenant_id = ? AND user_id = ?`,
		platform,
		tenantID,
		userID,
	)
	if err != nil {
		return false, fmt.Errorf("delete chat identity: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete chat identity rows: %w", err)
	}
	return affected > 0, nil
}
