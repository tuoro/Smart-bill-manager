package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
