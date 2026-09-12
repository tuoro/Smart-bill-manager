package dingtalk

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/chatintake"
)

// Manager 实现 chatconnectors.Runtime：每个工作区一条连接，按需起停，不重启进程。
type Manager struct {
	ctx     context.Context
	intake  chatintake.Service
	replies TextHandler
	logger  *slog.Logger

	mu      sync.Mutex
	running map[string]context.CancelFunc
	// 主动发消息要用该工作区的凭据换 token，所以连接跑起来之后把 API 留着。
	apis map[string]*OpenAPI
}

// TextHandler 是文本消息的去处：绑定码、确认、作废都在那边判断。
// 用接口而不是直接依赖 chatdialogue，连接器才不至于反过来依赖对话规则。
type TextHandler interface {
	Handle(ctx context.Context, platform, externalUserID, text, tenantID string) string
}

func NewManager(ctx context.Context, intake chatintake.Service, logger *slog.Logger) *Manager {
	return &Manager{ctx: ctx, intake: intake, logger: logger,
		running: map[string]context.CancelFunc{}, apis: map[string]*OpenAPI{}}
}

// BindTexts 后绑对话服务：它需要本 Manager 当通知器，本 Manager 需要它处理文本，
// 两者互相引用，只能建完再接。
func (m *Manager) BindTexts(replies TextHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replies = replies
}

// Send 实现 ports.ChatNotifier：找到该工作区正在跑的连接，用它的凭据发消息。
// 连接没启用就发不出去——这与产品一致：没启用的工作区本来就不该收到机器人消息。
func (m *Manager) Send(ctx context.Context, tenantID, platform, externalUserID, text string) error {
	m.mu.Lock()
	api := m.apis[key(tenantID, platform)]
	m.mu.Unlock()
	if api == nil {
		return fmt.Errorf("no active %s connection for tenant %s", platform, tenantID)
	}
	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return api.SendText(sendCtx, externalUserID, text)
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
	m.mu.Lock()
	m.apis[key(tenantID, platform)] = api
	replies := m.replies
	m.mu.Unlock()
	handler := NewHandler(tenantID, m.intake, replies, api, chatbot.NewChatbotReplier(), m.logger.With("tenant", tenantID))
	go Run(ctx, appKey, appSecret, handler, m.logger.With("tenant", tenantID))
}

func (m *Manager) Stop(tenantID, platform string) {
	m.mu.Lock()
	cancel, ok := m.running[key(tenantID, platform)]
	if ok {
		delete(m.running, key(tenantID, platform))
	}
	delete(m.apis, key(tenantID, platform))
	m.mu.Unlock()
	if ok {
		cancel()
	}
}
