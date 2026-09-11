package ports

import (
	"context"
	"time"
)

// MailboxCredentials 是一次 IMAP 会话需要的全部东西。密码只在调用期间存在于内存。
type MailboxCredentials struct {
	Host              string
	Port              int
	TransportSecurity string
	Username          string
	Password          []byte
}

// MailboxState 是增量同步的游标：UIDVALIDITY 变了意味着服务器重编了号，游标作废。
type MailboxState struct {
	UIDValidity uint32
	LastUID     uint32
}

type MailboxMessage struct {
	UID          uint32
	UIDValidity  uint32
	InternalDate time.Time
	Raw          []byte
}

// MailboxClient 是 IMAP 的最小抽象：能不能登录、有哪些新邮件。
// 只读：不打已读标记、不移动、不删除，用户自己的邮箱不该被我们动过。
type MailboxClient interface {
	Probe(ctx context.Context, credentials MailboxCredentials) error
	// FetchNew 按 UID 升序把游标之后的新邮件交给 handle，最多 limit 封；
	// handle 返回错误即停止，返回的游标只推进到最后一封成功处理的邮件。
	FetchNew(
		ctx context.Context,
		credentials MailboxCredentials,
		state MailboxState,
		limit int,
		handle func(MailboxMessage) error,
	) (MailboxState, error)
}

// EmailSourceConnection 是同步需要的私有视图：含密文，只给应用层用，永不出 HTTP。
type EmailSourceConnection struct {
	TenantID          string
	ID                string
	CreatedByUserID   string
	MailboxAddress    string
	IMAPHost          string
	IMAPPort          int
	TransportSecurity string
	IMAPUsername      string
	EncryptedPassword []byte
	ConnectionStatus  string
	SyncEnabled       bool
	Deleted           bool
	Version           int
	State             MailboxState
}

type EmailSourceRef struct {
	TenantID string
	ID       string
}

type SetEmailSourceCredentialsCommand struct {
	TenantID          string
	SourceID          string
	IMAPUsername      string
	EncryptedPassword []byte
	ExpectedVersion   int
	UpdatedAt         time.Time
}

type DeleteEmailSourceCommand struct {
	TenantID        string
	SourceID        string
	ActorUserID     string
	AuditEventID    string
	RequestID       string
	ExpectedVersion int
	DeletedAt       time.Time
}
