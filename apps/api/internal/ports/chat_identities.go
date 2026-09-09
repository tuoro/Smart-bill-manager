package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// 解析必须一次拿到租户与成员，且只在成员仍然在册时才算数：成员被移出租户后，
// 他的聊天账号不能继续往账目里投单据。
type ChatIdentityTransaction interface {
	FindChatIdentity(
		ctx context.Context,
		platform, externalUserID string,
	) (domain.ChatIdentity, error)

	InsertChatBindingCode(ctx context.Context, code domain.ChatBindingCode) error

	// 兑换是一次状态迁移，必须整体完成或整体不发生：校验码仍然有效、标记已用、
	// 替换该成员在这个平台上的旧绑定、写入新绑定。拆成多步会留下「码已作废但
	// 没绑上」或「绑上了但码还能再用一次」的中间态。
	RedeemChatBindingCode(
		ctx context.Context,
		platform, codeHash, externalUserID string,
		now time.Time,
	) (domain.ChatIdentity, error)

	ListChatIdentities(
		ctx context.Context,
		tenantID, userID string,
	) ([]domain.ChatBinding, error)

	DeleteChatIdentity(ctx context.Context, platform, tenantID, userID string) (bool, error)
}
