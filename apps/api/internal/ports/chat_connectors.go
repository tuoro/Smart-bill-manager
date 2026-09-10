package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type ChatConnectorRepository interface {
	GetChatConnector(ctx context.Context, tenantID, platform string) (domain.ChatConnector, error)
	// 进程启动时要把所有已启用的连接一次拉起来，跨租户读取只在这里发生。
	ListActiveChatConnectors(ctx context.Context) ([]domain.ChatConnector, error)
}

type ChatConnectorTransaction interface {
	// 保存即重置：新凭据未经检测，启用状态一并清掉，数据库约束也不允许反过来。
	UpsertChatConnector(ctx context.Context, connector domain.ChatConnector) error
	RecordChatConnectorDetection(
		ctx context.Context,
		tenantID, platform, status, message string,
		expectedVersion int64,
		now time.Time,
	) error
	SetChatConnectorActive(
		ctx context.Context,
		tenantID, platform string,
		active bool,
		expectedVersion int64,
		now time.Time,
	) error
}
