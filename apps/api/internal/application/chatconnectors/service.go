// Package chatconnectors 管理各工作区的聊天机器人凭据：保存、检测、启用。
// 与 providers 同一套做法：密文只在这里解密，界面只看得到「已设置」。
package chatconnectors

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// CredentialProbe 只回答一个问题：这对凭据能不能从平台换到 access token。
// 不建长连接——检测应当是幂等、秒级、可反复点的。
type CredentialProbe interface {
	Probe(ctx context.Context, platform, appKey, appSecret string) error
}

// Runtime 是正在运行的连接集合；启用/停用/换凭据都要同步到它，不用重启进程。
type Runtime interface {
	Start(tenantID, platform, appKey, appSecret string)
	Stop(tenantID, platform string)
}

type Cipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type Service struct {
	repository ports.ChatConnectorRepository
	tx         ports.TransactionManager
	cipher     Cipher
	probe      CredentialProbe
	runtime    Runtime
	clock      ports.Clock
}

func NewService(
	repository ports.ChatConnectorRepository,
	tx ports.TransactionManager,
	cipher Cipher,
	probe CredentialProbe,
	runtime Runtime,
	clock ports.Clock,
) Service {
	return Service{repository: repository, tx: tx, cipher: cipher, probe: probe, runtime: runtime, clock: clock}
}

// Public 是能给界面看的形状：不含密文，只说有没有。
type Public struct {
	Platform         string
	AppKey           string
	HasSecret        bool
	DetectionStatus  string
	DetectionAt      *time.Time
	DetectionMessage string
	Active           bool
	Version          int64
	UpdatedAt        time.Time
}

func public(c domain.ChatConnector) Public {
	return Public{
		Platform: c.Platform, AppKey: c.AppKey, HasSecret: len(c.EncryptedAppSecret) > 0,
		DetectionStatus: c.DetectionStatus, DetectionAt: c.DetectionCheckedAt, DetectionMessage: c.DetectionMessage,
		Active: c.Active, Version: c.Version, UpdatedAt: c.UpdatedAt,
	}
}

func (s Service) Get(ctx context.Context, tenant domain.TenantContext, platform string) (Public, error) {
	if err := tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return Public{}, err
	}
	if !domain.ValidChatPlatform(platform) {
		return Public{}, fmt.Errorf("%w: unsupported platform", domain.ErrInvalidInput)
	}
	c, err := s.repository.GetChatConnector(ctx, tenant.TenantID, platform)
	if err != nil {
		return Public{}, err
	}
	return public(c), nil
}

// Save 写入新凭据。保存即重置：检测回到 pending、启用清零、正在跑的连接停掉。
// 新密钥没验过就不该有连接在用它。
func (s Service) Save(
	ctx context.Context, tenant domain.TenantContext, platform, appKey string, appSecret []byte,
) (Public, error) {
	if err := tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return Public{}, err
	}
	if !domain.ValidChatPlatform(platform) {
		return Public{}, fmt.Errorf("%w: unsupported platform", domain.ErrInvalidInput)
	}
	appKey = strings.TrimSpace(appKey)
	if appKey == "" || len(appKey) > 200 {
		return Public{}, domain.NewRuleError("invalid_chat_app_key", "AppKey 长度必须为 1–200 个字符", domain.ErrInvalidInput)
	}
	if len(appSecret) == 0 || len(appSecret) > 512 {
		return Public{}, domain.NewRuleError("invalid_chat_app_secret", "AppSecret 长度不正确", domain.ErrInvalidInput)
	}
	encrypted, err := s.cipher.Encrypt(appSecret)
	if err != nil {
		return Public{}, fmt.Errorf("encrypt app secret: %w", err)
	}
	now := s.clock.Now()
	record := domain.ChatConnector{
		TenantID: tenant.TenantID, Platform: platform, AppKey: appKey, EncryptedAppSecret: encrypted,
		UpdatedByUserID: tenant.UserID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.UpsertChatConnector(ctx, record)
	}); err != nil {
		return Public{}, err
	}
	s.runtime.Stop(tenant.TenantID, platform)
	return s.Get(ctx, tenant, platform)
}

// Detect 拿凭据去平台换一次 token。结果连同可展示的原因写回，成败都记。
func (s Service) Detect(ctx context.Context, tenant domain.TenantContext, platform string) (Public, error) {
	if err := tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return Public{}, err
	}
	c, err := s.repository.GetChatConnector(ctx, tenant.TenantID, platform)
	if err != nil {
		return Public{}, err
	}
	secret, err := s.cipher.Decrypt(c.EncryptedAppSecret)
	if err != nil {
		return Public{}, fmt.Errorf("decrypt app secret: %w", err)
	}
	defer clear(secret)
	status, message := domain.ChatConnectorDetectionPassed, "凭据有效"
	if probeErr := s.probe.Probe(ctx, platform, c.AppKey, string(secret)); probeErr != nil {
		status, message = domain.ChatConnectorDetectionFailed, safeProbeMessage(probeErr)
	}
	now := s.clock.Now()
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.RecordChatConnectorDetection(ctx, tenant.TenantID, platform, status, message, c.Version, now)
	}); err != nil {
		return Public{}, err
	}
	return s.Get(ctx, tenant, platform)
}

// Activate 只接受检测通过的凭据（数据库约束同样如此），并立即拉起连接。
func (s Service) Activate(ctx context.Context, tenant domain.TenantContext, platform string) (Public, error) {
	if err := tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return Public{}, err
	}
	c, err := s.repository.GetChatConnector(ctx, tenant.TenantID, platform)
	if err != nil {
		return Public{}, err
	}
	if c.DetectionStatus != domain.ChatConnectorDetectionPassed {
		return Public{}, domain.ChatConnectorDetectionRequired()
	}
	secret, err := s.cipher.Decrypt(c.EncryptedAppSecret)
	if err != nil {
		return Public{}, fmt.Errorf("decrypt app secret: %w", err)
	}
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.SetChatConnectorActive(ctx, tenant.TenantID, platform, true, c.Version, s.clock.Now())
	}); err != nil {
		clear(secret)
		return Public{}, err
	}
	s.runtime.Start(tenant.TenantID, platform, c.AppKey, string(secret))
	clear(secret)
	return s.Get(ctx, tenant, platform)
}

func (s Service) Deactivate(ctx context.Context, tenant domain.TenantContext, platform string) (Public, error) {
	if err := tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return Public{}, err
	}
	c, err := s.repository.GetChatConnector(ctx, tenant.TenantID, platform)
	if err != nil {
		return Public{}, err
	}
	if err := s.tx.WithinReadCommittedTransaction(ctx, func(t ports.Transaction) error {
		return t.SetChatConnectorActive(ctx, tenant.TenantID, platform, false, c.Version, s.clock.Now())
	}); err != nil {
		return Public{}, err
	}
	s.runtime.Stop(tenant.TenantID, platform)
	return s.Get(ctx, tenant, platform)
}

// StartActive 在进程启动时把所有已启用的连接拉起来。
func (s Service) StartActive(ctx context.Context) error {
	items, err := s.repository.ListActiveChatConnectors(ctx)
	if err != nil {
		return err
	}
	for _, c := range items {
		secret, err := s.cipher.Decrypt(c.EncryptedAppSecret)
		if err != nil {
			return fmt.Errorf("decrypt app secret for %s: %w", c.TenantID, err)
		}
		s.runtime.Start(c.TenantID, c.Platform, c.AppKey, string(secret))
		clear(secret)
	}
	return nil
}

// 探测失败的原因要能给用户看，但不能把平台返回的原始报文（可能含凭据片段）照抄出去。
func safeProbeMessage(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "连接钉钉超时"
	case errors.Is(err, ErrCredentialsRejected):
		return "钉钉拒绝了这对 AppKey/AppSecret"
	default:
		return "无法连接钉钉，请检查网络"
	}
}

var ErrCredentialsRejected = errors.New("credentials rejected by platform")
