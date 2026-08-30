package providers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type CreateInput struct {
	Tenant     domain.TenantContext
	BaseURL    string
	APIKey     []byte
	Model      string
	OutputMode string
}

type Service struct {
	repository ports.ProviderRepository
	tx         ports.TransactionManager
	cipher     ports.SecretCipher
	detector   ports.ProviderDetector
	ids        ports.IDGenerator
	clock      ports.Clock
}

func NewService(
	repository ports.ProviderRepository,
	tx ports.TransactionManager,
	cipher ports.SecretCipher,
	detector ports.ProviderDetector,
	ids ports.IDGenerator,
	clock ports.Clock,
) Service {
	return Service{repository: repository, tx: tx, cipher: cipher, detector: detector, ids: ids, clock: clock}
}

func (s Service) Create(ctx context.Context, input CreateInput) (ports.ProviderConfig, error) {
	if err := input.Tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return ports.ProviderConfig{}, err
	}
	baseURL, err := normalizeBaseURL(input.BaseURL)
	if err != nil {
		return ports.ProviderConfig{}, err
	}
	model := strings.TrimSpace(input.Model)
	if model == "" || len(model) > 200 {
		return ports.ProviderConfig{}, domain.NewRuleError("invalid_provider_model", "模型名称长度必须为 1–200 个字符", domain.ErrInvalidInput)
	}
	outputMode := strings.TrimSpace(input.OutputMode)
	if outputMode != ports.ProviderOutputModeJSONSchema && outputMode != ports.ProviderOutputModeJSONObject {
		return ports.ProviderConfig{}, domain.NewRuleError("invalid_provider_output_mode", "输出模式必须是 json_schema 或 json_object", domain.ErrInvalidInput)
	}
	if len(input.APIKey) == 0 || len(input.APIKey) > 4096 {
		return ports.ProviderConfig{}, domain.NewRuleError("invalid_provider_key", "API Key 长度不正确", domain.ErrInvalidInput)
	}
	encrypted, err := s.cipher.Encrypt(input.APIKey)
	if err != nil {
		return ports.ProviderConfig{}, fmt.Errorf("encrypt provider key: %w", err)
	}
	id, err := s.ids.NewID()
	if err != nil {
		return ports.ProviderConfig{}, fmt.Errorf("generate provider config id: %w", err)
	}
	now := s.clock.Now()
	providerSchema := s.detector.ProviderSchemaIdentity()
	if providerSchema.Version == "" || providerSchema.SHA256 == "" {
		return ports.ProviderConfig{}, errors.New("provider schema identity is unavailable")
	}
	config := ports.ProviderConfig{
		ID:               id,
		TenantID:         input.Tenant.TenantID,
		BaseURL:          baseURL,
		EncryptedAPIKey:  encrypted,
		Model:            model,
		OutputMode:       outputMode,
		CapabilityStatus: "pending",
		Active:           false,
		Version:          1,
		SafeFingerprint: s.cipher.Fingerprint(
			[]byte(baseURL),
			[]byte(model),
			[]byte(outputMode),
			[]byte(providerSchema.Version),
			[]byte(providerSchema.SHA256),
			input.APIKey,
		),
		CreatedByUserID: input.Tenant.UserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.InsertProviderConfig(ctx, config)
	}); err != nil {
		return ports.ProviderConfig{}, err
	}
	return publicConfig(config), nil
}

func (s Service) Detect(
	ctx context.Context,
	tenant domain.TenantContext,
	configID string,
) (ports.ProviderConfig, error) {
	if err := tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return ports.ProviderConfig{}, err
	}
	config, err := s.repository.GetProviderConfig(ctx, tenant.TenantID, configID)
	if err != nil {
		return ports.ProviderConfig{}, err
	}
	apiKey, err := s.cipher.Decrypt(config.EncryptedAPIKey)
	if err != nil {
		return ports.ProviderConfig{}, fmt.Errorf("decrypt provider key: %w", err)
	}
	defer clear(apiKey)
	detectionCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	providerSchema := s.detector.ProviderSchemaIdentity()
	result := s.detector.DetectCapabilities(detectionCtx, ports.ProviderCredentials{
		BaseURL:                 config.BaseURL,
		APIKey:                  apiKey,
		Model:                   config.Model,
		OutputMode:              config.OutputMode,
		Version:                 config.Version,
		CapabilitySchemaVersion: providerSchema.Version,
		CapabilitySchemaSHA256:  providerSchema.SHA256,
	})
	status := "failed"
	if result.Passed {
		status = "passed"
	}
	checkedAt := s.clock.Now()
	if err := s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.RecordProviderCapability(
			ctx,
			tenant.TenantID,
			config.ID,
			config.Version,
			status,
			result.SafeMessage,
			providerSchema,
			checkedAt,
		)
	}); err != nil {
		return ports.ProviderConfig{}, err
	}
	config.CapabilityStatus = status
	config.CapabilityCheckedAt = &checkedAt
	config.CapabilitySafeMessage = result.SafeMessage
	config.CapabilitySchemaVersion = providerSchema.Version
	config.CapabilitySchemaSHA256 = providerSchema.SHA256
	if !result.Passed {
		config.Active = false
	}
	config.UpdatedAt = checkedAt
	return publicConfig(config), nil
}

func (s Service) Activate(
	ctx context.Context,
	tenant domain.TenantContext,
	configID string,
) (ports.ProviderConfig, error) {
	if err := tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return ports.ProviderConfig{}, err
	}
	config, err := s.repository.GetProviderConfig(ctx, tenant.TenantID, configID)
	if err != nil {
		return ports.ProviderConfig{}, err
	}
	providerSchema := s.detector.ProviderSchemaIdentity()
	if config.CapabilityStatus != "passed" ||
		config.CapabilitySchemaVersion != providerSchema.Version ||
		config.CapabilitySchemaSHA256 != providerSchema.SHA256 {
		return ports.ProviderConfig{}, domain.NewRuleError(
			"provider_capability_required",
			"配置必须先通过完整能力检测",
			domain.ErrConflict,
		)
	}
	now := s.clock.Now()
	if err := s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.ActivateProviderConfig(
			ctx,
			tenant.TenantID,
			config.ID,
			config.Version,
			providerSchema,
			now,
		)
	}); err != nil {
		return ports.ProviderConfig{}, err
	}
	config.Active = true
	config.UpdatedAt = now
	return publicConfig(config), nil
}

func (s Service) List(ctx context.Context, tenant domain.TenantContext) ([]ports.ProviderConfig, error) {
	if err := tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return nil, err
	}
	configs, err := s.repository.ListProviderConfigs(ctx, tenant.TenantID)
	if err != nil {
		return nil, err
	}
	for index := range configs {
		configs[index] = publicConfig(configs[index])
	}
	return configs, nil
}

func (s Service) Delete(
	ctx context.Context,
	tenant domain.TenantContext,
	configID, requestID string,
) error {
	if err := tenant.Require(domain.CapabilityProvidersManage); err != nil {
		return err
	}
	if requestID == "" {
		return domain.ErrInvalidInput
	}
	config, err := s.repository.GetProviderConfig(ctx, tenant.TenantID, configID)
	if err != nil {
		return err
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return err
	}
	command := ports.ProviderDeleteCommand{
		TenantID: tenant.TenantID, ConfigID: config.ID, ActorUserID: tenant.UserID,
		AuditEventID: auditID, RequestID: requestID, ExpectedVersion: config.Version, DeletedAt: s.clock.Now(),
	}
	return s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.DeleteProviderConfig(ctx, command)
	})
}

func normalizeBaseURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", domain.NewRuleError("invalid_provider_url", "Base URL 必须是完整的 HTTP(S) 地址", domain.ErrInvalidInput)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", domain.NewRuleError("invalid_provider_url", "Base URL 不能包含凭据、查询参数或片段", domain.ErrInvalidInput)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func publicConfig(config ports.ProviderConfig) ports.ProviderConfig {
	config.EncryptedAPIKey = nil
	return config
}
