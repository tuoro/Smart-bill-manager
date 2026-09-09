// Package chatintake 把聊天通道送来的文件接进既有的识别管线。
//
// 它只依赖 UploadService 与身份解析，不直接写 Document、Job、数据库或对象存储
// ——与 docs/architecture.md 对邮箱连接器的要求相同：连接器不得成为第二处写入
// 实现。真实的钉钉 Stream 客户端应当只负责把消息翻译成 Message 交给这里。
package chatintake

import (
	"context"
	"fmt"
	"io"

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
}

type Result struct {
	TenantID   string
	UserID     string
	DocumentID string
	JobID      string
}

type Service struct {
	tx      ports.TransactionManager
	uploads documents.UploadService
}

func NewService(tx ports.TransactionManager, uploads documents.UploadService) Service {
	return Service{tx: tx, uploads: uploads}
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
