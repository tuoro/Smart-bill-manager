package ports

import (
	"context"
	"time"
)

type EmailSource struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"-"`
	DisplayName       string     `json:"display_name"`
	MailboxAddress    string     `json:"mailbox_address"`
	IMAPHost          string     `json:"imap_host"`
	IMAPPort          int        `json:"imap_port"`
	TransportSecurity string     `json:"transport_security"`
	Status            string     `json:"status"`
	CreatedByUserID   string     `json:"created_by_user_id"`
	CreatedAt         time.Time  `json:"created_at"`
	LastArchivedAt    *time.Time `json:"last_archived_at,omitempty"`
	Version           int        `json:"version"`
	MessageCount      int        `json:"message_count"`
	AttachmentCount   int        `json:"attachment_count"`
	BlockedCount      int        `json:"blocked_count"`
	// 连接与同步状态。密码永不回显，只说有没有。
	IMAPUsername        string     `json:"imap_username"`
	HasPassword         bool       `json:"has_password"`
	ConnectionStatus    string     `json:"connection_status"`
	ConnectionCheckedAt *time.Time `json:"connection_checked_at,omitempty"`
	ConnectionMessage   string     `json:"connection_message"`
	SyncEnabled         bool       `json:"sync_enabled"`
	LastSyncAt          *time.Time `json:"last_sync_at,omitempty"`
	LastSyncMessage     string     `json:"last_sync_message"`
}

type EmailSourceRegistrationReplay struct {
	RequestHash string
	Source      EmailSource
}

type EmailSourceCreateResult struct {
	Source   EmailSource `json:"source"`
	Replayed bool        `json:"replayed"`
}

type CreateEmailSourceCommand struct {
	Source            EmailSource
	IdempotencyKey    string
	RequestHash       string
	AuditEventID      string
	RequestID         string
	EncryptedPassword []byte
}

type ParsedEmailAttachment struct {
	PartIndex   int
	Name        string
	MIME        string
	Disposition string
	Content     []byte
}

type ParsedEmail struct {
	Subject       string
	SenderAddress string
	SentAt        *time.Time
	Attachments   []ParsedEmailAttachment
	BlockedCode   string
	SafeErrorText string
}

type EmailParser interface {
	Parse(raw []byte) ParsedEmail
}

type EmailMessage struct {
	ID            string            `json:"id"`
	EmailSourceID string            `json:"email_source_id"`
	Subject       string            `json:"subject"`
	SenderAddress string            `json:"sender_address"`
	SentAt        *time.Time        `json:"sent_at,omitempty"`
	ReceivedAt    time.Time         `json:"received_at"`
	Status        string            `json:"status"`
	SafeErrorCode string            `json:"safe_error_code,omitempty"`
	SafeErrorText string            `json:"safe_error_text,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	Attachments   []EmailAttachment `json:"attachments"`
}

type EmailAttachment struct {
	ID               string `json:"id"`
	PartIndex        int    `json:"part_index"`
	OriginalName     string `json:"original_name"`
	DeclaredMIME     string `json:"declared_mime"`
	Disposition      string `json:"disposition"`
	SizeBytes        int64  `json:"size_bytes"`
	ProcessingStatus string `json:"processing_status"`
	SafeReasonCode   string `json:"safe_reason_code,omitempty"`
	DocumentID       string `json:"document_id,omitempty"`
	JobID            string `json:"job_id,omitempty"`
}

type EmailMessagePage struct {
	Items      []EmailMessage `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type EmailMessagePageQuery struct {
	BeforeReceivedAt *time.Time
	BeforeID         string
	Limit            int
}

type EmailObject struct {
	StorageKey string
	Name       string
	MIME       string
	// 所属来源的归属人：成员只能打开自己邮箱里的东西。
	SourceCreatedByUserID string
}

type EmailMessageReplay struct {
	RawSHA256 string
	Result    EmailArchiveResult
}

type EmailAttachmentDraft struct {
	ID               string
	PartIndex        int
	StorageKey       string
	OriginalName     string
	DeclaredMIME     string
	Disposition      string
	SizeBytes        int64
	SHA256           string
	ProcessingStatus string
	SafeReasonCode   string
	Document         *Document
	Job              *ProcessingJob
}

type EmailArchiveCommand struct {
	TenantID           string
	EmailSourceID      string
	ExternalMessageKey string
	RawStorageKey      string
	RawSHA256          string
	RawSizeBytes       int64
	MessageID          string
	Subject            string
	SenderAddress      string
	SentAt             *time.Time
	ReceivedAt         time.Time
	Status             string
	SafeErrorCode      string
	SafeErrorText      string
	Attachments        []EmailAttachmentDraft
	AuditEventID       string
	RequestID          string
	CreatedAt          time.Time
}

type EmailArchiveResult struct {
	MessageID          string            `json:"message_id"`
	Status             string            `json:"status"`
	SafeErrorCode      string            `json:"safe_error_code,omitempty"`
	Attachments        []EmailAttachment `json:"attachments"`
	CreatedDocumentIDs []string          `json:"created_document_ids"`
	CreatedJobIDs      []string          `json:"created_job_ids"`
	Replayed           bool              `json:"replayed"`
}

type CompensateEmailArchiveCommand struct {
	TenantID           string
	EmailSourceID      string
	MessageID          string
	AuditEventID       string
	CreatedDocumentIDs []string
}

type EmailRepository interface {
	GetEmailSourceRegistrationReplay(ctx context.Context, tenantID, idempotencyKey string) (EmailSourceRegistrationReplay, error)
	ListEmailSources(ctx context.Context, tenantID string) ([]EmailSource, error)
	GetEmailSource(ctx context.Context, tenantID, sourceID string) (EmailSource, error)
	GetEmailMessageReplay(ctx context.Context, tenantID, sourceID, externalMessageKey string) (EmailMessageReplay, error)
	ListEmailMessages(ctx context.Context, tenantID, sourceID string, query EmailMessagePageQuery) (EmailMessagePage, error)
	GetEmailMessageObject(ctx context.Context, tenantID, messageID string) (EmailObject, error)
	GetEmailAttachmentObject(ctx context.Context, tenantID, attachmentID string) (EmailObject, error)
	GetEmailSourceConnection(ctx context.Context, tenantID, sourceID string) (EmailSourceConnection, error)
	ListSyncEnabledEmailSources(ctx context.Context) ([]EmailSourceRef, error)
}

type EmailTransaction interface {
	CreateEmailSource(ctx context.Context, command CreateEmailSourceCommand) (EmailSourceCreateResult, error)
	ArchiveEmailMessage(ctx context.Context, command EmailArchiveCommand) (EmailArchiveResult, error)
	CompensateEmailArchive(ctx context.Context, command CompensateEmailArchiveCommand) error
	SetEmailSourceCredentials(ctx context.Context, command SetEmailSourceCredentialsCommand) error
	RecordEmailSourceConnection(ctx context.Context, tenantID, sourceID, status, safeMessage string, expectedVersion int, checkedAt time.Time) error
	SetEmailSourceSync(ctx context.Context, tenantID, sourceID string, enabled bool, expectedVersion int, now time.Time) error
	RecordEmailSourceSync(ctx context.Context, tenantID, sourceID string, state MailboxState, safeMessage string, syncedAt time.Time) error
	DeleteEmailSource(ctx context.Context, command DeleteEmailSourceCommand) error
}
