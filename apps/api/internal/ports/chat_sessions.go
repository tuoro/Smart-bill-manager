package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// ChatSession 是一次「识别完等你回话」的对话状态，不是业务事实：删掉它不改变
// 任何账目，单据仍在网页待审核队列里。
type ChatSession struct {
	Platform       string
	ExternalUserID string
	TenantID       string
	UserID         string
	JobID          string
	DocumentName   string
	State          string
	// PlanJSON 是这段对话攒下的决定，形状由对话服务定义，其他层不解释它。
	PlanJSON         string
	ExpectedRevision int
	Reminded         bool
	StartedAt        time.Time
	UpdatedAt        time.Time
}

// ChatRecipient 是一份单据该把回执推给谁：只有通过聊天投进来的单据才有。
type ChatRecipient struct {
	Identity     domain.ChatIdentity
	DocumentName string
	JobStatus    domain.JobStatus
	SafeError    string
}

type ChatSessionRepository interface {
	FindChatSession(ctx context.Context, platform, externalUserID string) (ChatSession, error)
	// FindChatRecipient 回答「这个 Job 的回执发给谁」。单据不是聊天投递的、
	// 或投递人已解绑时返回 ErrNotFound——静默不推，不是错误。
	FindChatRecipient(ctx context.Context, tenantID, jobID string) (ChatRecipient, error)
	// ListStaleChatSessions 取 startedAt 早于 before 的会话，供提醒与超时扫描。
	ListStaleChatSessions(ctx context.Context, before time.Time, limit int) ([]ChatSession, error)
}

type ChatSessionTransaction interface {
	// 一人一条：同一个外部账号再来一份单据即覆盖，旧的那条对话就此作废。
	UpsertChatSession(ctx context.Context, session ChatSession) error
	MarkChatSessionReminded(ctx context.Context, platform, externalUserID string, now time.Time) error
	DeleteChatSession(ctx context.Context, platform, externalUserID string) error
}

// ChatNotifier 主动给某个外部账号发一条文本——回执、提醒、超时通知都走它。
// 与回复不同，主动发消息没有 sessionWebhook 可用，由平台的主动发消息接口承担。
type ChatNotifier interface {
	Send(ctx context.Context, tenantID, platform, externalUserID, text string) error
}
