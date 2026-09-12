// Package mailsync 让登记好的邮箱真正跑起来：存密码、测连接、开关同步、拉邮件。
//
// 拉到的每封邮件都交给 emails.Service.Archive——那是唯一的归档入口，这里不直接写
// Document、Job、数据库或对象存储（docs/architecture.md 对连接器的要求）。
package mailsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	applicationemails "github.com/tuoro/smart-bill-manager/apps/api/internal/application/emails"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// Runtime 是正在轮询的邮箱集合；开关同步、换密码、删除都同步到它。
type Runtime interface {
	Start(tenantID, sourceID string)
	Stop(tenantID, sourceID string)
}

// 一轮最多拉这么多封：首次接入几千封历史邮件也不会把一次请求拖到超时，
// 轮询会一轮轮追上。
const BatchLimit = 50

var (
	ErrCredentialsRejected = errors.New("mailbox rejected the credentials")
	ErrUnreachable         = errors.New("mailbox is unreachable")
)

type Service struct {
	repository ports.EmailRepository
	tx         ports.TransactionManager
	cipher     ports.SecretCipher
	mailbox    ports.MailboxClient
	runtime    Runtime
	archive    applicationemails.Service
	ids        ports.IDGenerator
	clock      ports.Clock
}

func NewService(
	repository ports.EmailRepository,
	tx ports.TransactionManager,
	cipher ports.SecretCipher,
	mailbox ports.MailboxClient,
	runtime Runtime,
	archive applicationemails.Service,
	ids ports.IDGenerator,
	clock ports.Clock,
) Service {
	return Service{repository: repository, tx: tx, cipher: cipher, mailbox: mailbox, runtime: runtime, archive: archive, ids: ids, clock: clock}
}

// manageable 取一个来源并确认调用者能管它：要有管理能力，还得是自己的（管理员除外）。
func (s Service) manageable(ctx context.Context, tenant domain.TenantContext, sourceID string) (ports.EmailSourceConnection, error) {
	if err := tenant.Require(domain.CapabilityEmailSourcesManage); err != nil {
		return ports.EmailSourceConnection{}, err
	}
	if sourceID == "" {
		return ports.EmailSourceConnection{}, domain.ErrInvalidInput
	}
	c, err := s.repository.GetEmailSourceConnection(ctx, tenant.TenantID, sourceID)
	if err != nil {
		return ports.EmailSourceConnection{}, err
	}
	if c.Deleted || !domain.EmailSourceVisibleTo(tenant, c.CreatedByUserID) {
		return ports.EmailSourceConnection{}, domain.ErrNotFound
	}
	return c, nil
}

func (s Service) public(ctx context.Context, tenant domain.TenantContext, sourceID string) (ports.EmailSource, error) {
	return s.archive.VisibleSource(ctx, tenant, sourceID)
}

// SetCredentials 换用户名/密码。保存即重置：连接回到待检测、同步关闭、轮询停掉。
func (s Service) SetCredentials(ctx context.Context, tenant domain.TenantContext, sourceID, username string, password []byte) (ports.EmailSource, error) {
	c, err := s.manageable(ctx, tenant, sourceID)
	if err != nil {
		return ports.EmailSource{}, err
	}
	normalized, err := domain.NormalizeIMAPUsername(username)
	if err != nil {
		return ports.EmailSource{}, err
	}
	if err := domain.ValidateIMAPPassword(password); err != nil {
		return ports.EmailSource{}, err
	}
	encrypted, err := s.cipher.Encrypt(password)
	if err != nil {
		return ports.EmailSource{}, fmt.Errorf("encrypt mailbox password: %w", err)
	}
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.SetEmailSourceCredentials(ctx, ports.SetEmailSourceCredentialsCommand{
			TenantID: tenant.TenantID, SourceID: c.ID, IMAPUsername: normalized,
			EncryptedPassword: encrypted, ExpectedVersion: c.Version, UpdatedAt: s.clock.Now(),
		})
	}); err != nil {
		return ports.EmailSource{}, err
	}
	s.runtime.Stop(tenant.TenantID, c.ID)
	return s.public(ctx, tenant, c.ID)
}

// Detect 用保存的密码登录一次、打开收件箱就断开。成败都写回，原因要能给人看。
func (s Service) Detect(ctx context.Context, tenant domain.TenantContext, sourceID string) (ports.EmailSource, error) {
	c, err := s.manageable(ctx, tenant, sourceID)
	if err != nil {
		return ports.EmailSource{}, err
	}
	if len(c.EncryptedPassword) == 0 {
		return ports.EmailSource{}, domain.NewRuleError("email_password_required", "请先填写邮箱密码或授权码", domain.ErrConflict)
	}
	credentials, err := s.credentials(c)
	if err != nil {
		return ports.EmailSource{}, err
	}
	defer clear(credentials.Password)
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, message := domain.EmailConnectionPassed, "连接成功，收件箱可读"
	if probeErr := s.mailbox.Probe(probeCtx, credentials); probeErr != nil {
		status, message = domain.EmailConnectionFailed, safeMessage(probeErr, c)
	}
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.RecordEmailSourceConnection(ctx, tenant.TenantID, c.ID, status, message, c.Version, s.clock.Now())
	}); err != nil {
		return ports.EmailSource{}, err
	}
	if status == domain.EmailConnectionFailed {
		s.runtime.Stop(tenant.TenantID, c.ID)
	}
	return s.public(ctx, tenant, c.ID)
}

// Activate 开启后台同步，只接受检测通过的邮箱（数据库约束同样如此）。
func (s Service) Activate(ctx context.Context, tenant domain.TenantContext, sourceID string) (ports.EmailSource, error) {
	c, err := s.manageable(ctx, tenant, sourceID)
	if err != nil {
		return ports.EmailSource{}, err
	}
	if c.ConnectionStatus != domain.EmailConnectionPassed || len(c.EncryptedPassword) == 0 {
		return ports.EmailSource{}, domain.EmailConnectionRequired()
	}
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.SetEmailSourceSync(ctx, tenant.TenantID, c.ID, true, c.Version, s.clock.Now())
	}); err != nil {
		return ports.EmailSource{}, err
	}
	s.runtime.Start(tenant.TenantID, c.ID)
	return s.public(ctx, tenant, c.ID)
}

func (s Service) Deactivate(ctx context.Context, tenant domain.TenantContext, sourceID string) (ports.EmailSource, error) {
	c, err := s.manageable(ctx, tenant, sourceID)
	if err != nil {
		return ports.EmailSource{}, err
	}
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.SetEmailSourceSync(ctx, tenant.TenantID, c.ID, false, c.Version, s.clock.Now())
	}); err != nil {
		return ports.EmailSource{}, err
	}
	s.runtime.Stop(tenant.TenantID, c.ID)
	return s.public(ctx, tenant, c.ID)
}

// SyncNow 由用户手动触发一轮，同步执行完再返回，页面立刻能看到结果。
func (s Service) SyncNow(ctx context.Context, tenant domain.TenantContext, sourceID string) (ports.EmailSource, error) {
	c, err := s.manageable(ctx, tenant, sourceID)
	if err != nil {
		return ports.EmailSource{}, err
	}
	if c.ConnectionStatus != domain.EmailConnectionPassed || len(c.EncryptedPassword) == 0 {
		return ports.EmailSource{}, domain.EmailConnectionRequired()
	}
	if err := s.SyncOnce(ctx, tenant.TenantID, c.ID); err != nil {
		return ports.EmailSource{}, err
	}
	return s.public(ctx, tenant, c.ID)
}

// Delete 软删除：停轮询、清密码、从列表消失；已归档的邮件与单据是不可变 Source，留着。
func (s Service) Delete(ctx context.Context, tenant domain.TenantContext, sourceID, requestID string) error {
	c, err := s.manageable(ctx, tenant, sourceID)
	if err != nil {
		return err
	}
	if requestID == "" {
		return domain.ErrInvalidInput
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return err
	}
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.DeleteEmailSource(ctx, ports.DeleteEmailSourceCommand{
			TenantID: tenant.TenantID, SourceID: c.ID, ActorUserID: tenant.UserID,
			AuditEventID: auditID, RequestID: requestID, ExpectedVersion: c.Version, DeletedAt: s.clock.Now(),
		})
	}); err != nil {
		return err
	}
	s.runtime.Stop(tenant.TenantID, c.ID)
	return nil
}

// StartActive 在进程启动时把所有开着同步的邮箱拉起来。
func (s Service) StartActive(ctx context.Context) error {
	refs, err := s.repository.ListSyncEnabledEmailSources(ctx)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		s.runtime.Start(ref.TenantID, ref.ID)
	}
	return nil
}

// SyncOnce 跑一轮增量同步：拉游标之后的新邮件逐封归档，游标只推进到最后一封
// 成功归档的。无论成败都把结果写回来源，页面上看得到「上次同步」。
// 不带 TenantContext：轮询是系统行为，不是某个人的操作。
func (s Service) SyncOnce(ctx context.Context, tenantID, sourceID string) error {
	c, err := s.repository.GetEmailSourceConnection(ctx, tenantID, sourceID)
	if err != nil {
		return err
	}
	if c.Deleted {
		return domain.ErrNotFound
	}
	credentials, err := s.credentials(c)
	if err != nil {
		return err
	}
	defer clear(credentials.Password)
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	archived := 0
	state, fetchErr := s.mailbox.FetchNew(fetchCtx, credentials, c.State, BatchLimit, func(message ports.MailboxMessage) error {
		receivedAt := message.InternalDate
		if receivedAt.IsZero() {
			receivedAt = s.clock.Now()
		}
		_, archiveErr := s.archive.Archive(ctx, applicationemails.ArchiveInput{
			TenantID: tenantID, EmailSourceID: sourceID,
			ExternalMessageKey: ExternalMessageKey(sourceID, message.UIDValidity, message.UID),
			ReceivedAt:         receivedAt, Raw: bytesReader(message.Raw),
			RequestID: "imap-sync-" + sourceID + "-" + strconv.FormatUint(uint64(message.UID), 10),
		})
		if archiveErr != nil {
			// 同一封邮件原文变了（极罕见）不该卡住整个邮箱：跳过继续。
			var rule *domain.RuleError
			if errors.As(archiveErr, &rule) && rule.Code == "email_message_identity_conflict" {
				return nil
			}
			return archiveErr
		}
		archived++
		return nil
	})
	message := "已同步 " + strconv.Itoa(archived) + " 封新邮件"
	if fetchErr != nil {
		message = safeMessage(fetchErr, c)
	}
	if recordErr := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.RecordEmailSourceSync(ctx, tenantID, sourceID, state, message, s.clock.Now())
	}); recordErr != nil {
		return recordErr
	}
	return fetchErr
}

func (s Service) credentials(c ports.EmailSourceConnection) (ports.MailboxCredentials, error) {
	password, err := s.cipher.Decrypt(c.EncryptedPassword)
	if err != nil {
		return ports.MailboxCredentials{}, fmt.Errorf("decrypt mailbox password: %w", err)
	}
	username := c.IMAPUsername
	if username == "" {
		username = c.MailboxAddress
	}
	return ports.MailboxCredentials{
		Host: c.IMAPHost, Port: c.IMAPPort, TransportSecurity: c.TransportSecurity,
		Username: username, Password: password,
	}, nil
}

// ExternalMessageKey 由服务器端稳定身份（UIDVALIDITY + UID）散列而来：确定、不可逆，
// 不含邮箱地址或凭据（ADR-0014）。
func ExternalMessageKey(sourceID string, uidValidity, uid uint32) string {
	sum := sha256.Sum256([]byte("imap/1|" + sourceID + "|" + strconv.FormatUint(uint64(uidValidity), 10) + "|" + strconv.FormatUint(uint64(uid), 10)))
	return hex.EncodeToString(sum[:])
}

// safeMessage 把连接错误翻译成能给用户看的一句话，不照抄服务器原文（可能含凭据片段）。
func safeMessage(err error, c ports.EmailSourceConnection) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "连接 " + c.IMAPHost + " 超时"
	case errors.Is(err, ErrCredentialsRejected):
		return "邮箱拒绝了登录，请检查用户名与密码（多数邮箱要用「授权码」而不是登录密码）"
	case errors.Is(err, ErrUnreachable):
		return "无法连接 " + c.IMAPHost + ":" + strconv.Itoa(c.IMAPPort) + "，请检查服务器地址、端口与加密方式"
	default:
		return "同步失败，请稍后重试"
	}
}
