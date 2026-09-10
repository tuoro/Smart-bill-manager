// Package chatintake 把聊天通道送来的文件接进既有的识别管线。
//
// 它只依赖 UploadService 与身份解析，不直接写 Document、Job、数据库或对象存储
// ——与 docs/architecture.md 对邮箱连接器的要求相同：连接器不得成为第二处写入
// 实现。真实的钉钉 Stream 客户端应当只负责把消息翻译成 Message 交给这里。
package chatintake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// Message 是通道无关的投递事件。文件内容用流传入，避免连接器先把整份文件读进
// 内存——大小与类型边界仍由 UploadService 统一判定，这里不复制一份规则。
type Message struct {
	Platform       string
	ExternalUserID string
	FileName       string
	MIME           string
	Source         io.Reader
	// 连接归属的租户。每个工作区绑自己的机器人，某个机器人收到的消息若发送者绑定的
	// 是别的工作区，必须在写入前拒绝——否则一份单据会落进它不该去的账本。留空表示
	// 不校验（仅供测试与将来的部署级连接）。
	TenantID string
}

// 连接与租户不一致时的错误：不能落成 ErrChatSenderNotLinked，那句提示会让人去重新
// 绑定，而实际上是绑错了机器人。
var ErrTenantMismatch = errors.New("chat sender is bound to a different tenant")

type Result struct {
	TenantID   string
	UserID     string
	DocumentID string
	JobID      string
}

type Service struct {
	tx      ports.TransactionManager
	uploads documents.UploadService
	tokens  ports.TokenGenerator
	ids     ports.IDGenerator
	clock   ports.Clock
}

func NewService(
	tx ports.TransactionManager,
	uploads documents.UploadService,
	tokens ports.TokenGenerator,
	ids ports.IDGenerator,
	clock ports.Clock,
) Service {
	return Service{tx: tx, uploads: uploads, tokens: tokens, ids: ids, clock: clock}
}

type BindingCode struct {
	Code      string
	ExpiresAt time.Time
}

// IssueBindingCode 发一张一次性绑定码。持有它就证明持有者能登录网页、因而是在册
// 成员——这正是聊天通道自己无法证明的那件事。
//
// 不要求 documents.process：绑定确立的是身份，不是权限。能不能投件在投递时按
// 成员的真实角色判定，只读成员绑了也投不进来。把两件事分开，成员角色变化时不
// 需要重新绑定。
func (s Service) IssueBindingCode(
	ctx context.Context,
	tenant domain.TenantContext,
	platform string,
) (BindingCode, error) {
	if !domain.ValidChatPlatform(platform) {
		return BindingCode{}, fmt.Errorf("%w: unsupported platform", domain.ErrInvalidInput)
	}
	raw, hash, err := s.tokens.NewToken()
	if err != nil {
		return BindingCode{}, fmt.Errorf("generate chat binding code: %w", err)
	}
	id, err := s.ids.NewID()
	if err != nil {
		return BindingCode{}, fmt.Errorf("generate chat binding code id: %w", err)
	}
	now := s.clock.Now()
	record := domain.ChatBindingCode{
		ID:        id,
		TenantID:  tenant.TenantID,
		UserID:    tenant.UserID,
		Platform:  platform,
		CodeHash:  hash,
		CreatedAt: now,
		ExpiresAt: now.Add(domain.ChatBindingCodeTTL),
	}
	err = s.tx.WithinReadCommittedTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.InsertChatBindingCode(ctx, record)
	})
	if err != nil {
		return BindingCode{}, err
	}
	// 明文只在这里出现一次，库里只有哈希。
	return BindingCode{Code: raw, ExpiresAt: record.ExpiresAt}, nil
}

// RedeemBindingCode 由连接器在收到一条文本消息时调用：把发送者的外部账号绑定到
// 发码的那个成员。无效、过期、已用过对外是同一句话，不告诉尝试者猜到了哪一步。
// expectedTenantID 非空时，码所属的租户必须与之一致；不一致整笔回滚，码不消耗。
func (s Service) RedeemBindingCode(
	ctx context.Context,
	platform, externalUserID, code, expectedTenantID string,
) (domain.ChatIdentity, error) {
	if !domain.ValidChatPlatform(platform) {
		return domain.ChatIdentity{}, fmt.Errorf("%w: unsupported platform", domain.ErrInvalidInput)
	}
	if externalUserID == "" || code == "" {
		return domain.ChatIdentity{}, domain.InvalidChatBindingCode()
	}
	var identity domain.ChatIdentity
	err := s.tx.WithinReadCommittedTransaction(ctx, func(transaction ports.Transaction) error {
		var redeemErr error
		identity, redeemErr = transaction.RedeemChatBindingCode(
			ctx,
			platform,
			s.tokens.Hash(code),
			externalUserID,
			s.clock.Now(),
		)
		if redeemErr != nil {
			return redeemErr
		}
		if expectedTenantID != "" && identity.TenantID != expectedTenantID {
			return ErrTenantMismatch
		}
		return nil
	})
	if err != nil {
		return domain.ChatIdentity{}, err
	}
	return identity, nil
}

// Receive 解析发送者后按该成员的真实身份投件。
//
// 未绑定的发送者返回 domain.ErrChatSenderNotLinked：这条通道绕开了登录会话，
// 能给机器人发消息不等于有权往账目里投单据，所以不设任何默认租户。
// 重复文件返回既有的 domain.DuplicateDocumentError，通道重投同一条消息因此
// 天然幂等，不需要另一套去重。
func (s Service) Receive(ctx context.Context, message Message) (Result, error) {
	if !domain.ValidChatPlatform(message.Platform) {
		return Result{}, fmt.Errorf("%w: unsupported platform", domain.ErrInvalidInput)
	}
	if message.ExternalUserID == "" {
		return Result{}, domain.ErrChatSenderNotLinked
	}
	var identity domain.ChatIdentity
	err := s.tx.WithinReadCommittedTransaction(ctx, func(transaction ports.Transaction) error {
		var findErr error
		identity, findErr = transaction.FindChatIdentity(
			ctx,
			message.Platform,
			message.ExternalUserID,
		)
		return findErr
	})
	if err != nil {
		return Result{}, err
	}
	if message.TenantID != "" && identity.TenantID != message.TenantID {
		return Result{}, ErrTenantMismatch
	}
	uploaded, err := s.uploads.Execute(ctx, documents.UploadInput{
		Tenant:        identity.TenantContext(),
		Name:          message.FileName,
		MIME:          message.MIME,
		Source:        message.Source,
		IngestionKind: domain.DocumentIngestionDingTalk,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		TenantID:   identity.TenantID,
		UserID:     identity.UserID,
		DocumentID: uploaded.DocumentID,
		JobID:      uploaded.JobID,
	}, nil
}

func (s Service) ListBindings(
	ctx context.Context,
	tenant domain.TenantContext,
) ([]domain.ChatBinding, error) {
	var bindings []domain.ChatBinding
	err := s.tx.WithinReadCommittedTransaction(ctx, func(transaction ports.Transaction) error {
		var listErr error
		bindings, listErr = transaction.ListChatIdentities(ctx, tenant.TenantID, tenant.UserID)
		return listErr
	})
	if err != nil {
		return nil, err
	}
	return bindings, nil
}

// Unbind 只解除自己名下的绑定。丢了手机、或者账号被别人接管时，这是把投件能力
// 收回来的动作，因此不需要任何额外能力——本人随时可以断开自己的通道。
func (s Service) Unbind(
	ctx context.Context,
	tenant domain.TenantContext,
	platform string,
) error {
	if !domain.ValidChatPlatform(platform) {
		return fmt.Errorf("%w: unsupported platform", domain.ErrInvalidInput)
	}
	return s.tx.WithinReadCommittedTransaction(ctx, func(transaction ports.Transaction) error {
		removed, err := transaction.DeleteChatIdentity(
			ctx,
			platform,
			tenant.TenantID,
			tenant.UserID,
		)
		if err != nil {
			return err
		}
		if !removed {
			return domain.ErrNotFound
		}
		return nil
	})
}
