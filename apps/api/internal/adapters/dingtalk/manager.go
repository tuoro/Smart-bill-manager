package dingtalk

import (
	"context"
	"log/slog"
	"sync"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/chatintake"
)

// Manager 实现 chatconnectors.Runtime：每个工作区一条连接，按需起停，不重启进程。
type Manager struct {
	ctx    context.Context
	intake chatintake.Service
	logger *slog.Logger

	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func NewManager(ctx context.Context, intake chatintake.Service, logger *slog.Logger) *Manager {
	return &Manager{ctx: ctx, intake: intake, logger: logger, running: map[string]context.CancelFunc{}}
}

func key(tenantID, platform string) string { return platform + ":" + tenantID }

// Start 幂等：同一工作区重复启动会先停掉旧连接——换了凭据的情况正是如此。
func (m *Manager) Start(tenantID, platform, appKey, appSecret string) {
	m.Stop(tenantID, platform)
	ctx, cancel := context.WithCancel(m.ctx)
	m.mu.Lock()
	m.running[key(tenantID, platform)] = cancel
	m.mu.Unlock()
	api := NewOpenAPI(appKey, appSecret)
	handler := NewHandler(tenantID, m.intake, api, chatbot.NewChatbotReplier(), m.logger.With("tenant", tenantID))
	go Run(ctx, appKey, appSecret, handler, m.logger.With("tenant", tenantID))
}

func (m *Manager) Stop(tenantID, platform string) {
	m.mu.Lock()
	cancel, ok := m.running[key(tenantID, platform)]
	if ok {
		delete(m.running, key(tenantID, platform))
	}
	m.mu.Unlock()
	if ok {
		cancel()
	}
}
