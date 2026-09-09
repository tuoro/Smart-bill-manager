package ports

import (
	"context"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// 解析必须一次拿到租户与成员，且只在成员仍然在册时才算数：成员被移出租户后，
// 他的聊天账号不能继续往账目里投单据。
type ChatIdentityTransaction interface {
	FindChatIdentity(
		ctx context.Context,
		platform, externalUserID string,
	) (domain.ChatIdentity, error)
}
