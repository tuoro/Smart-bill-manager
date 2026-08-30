package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type Input struct {
	Email           string
	Password        []byte
	DisplayName     string
	TenantName      string
	DefaultCurrency domain.Currency
	Timezone        string
}

type Result struct {
	UserID   string
	TenantID string
}

type Service struct {
	repository ports.IdentityRepository
	hasher     ports.PasswordHasher
	ids        ports.IDGenerator
	clock      ports.Clock
}

func NewService(
	repository ports.IdentityRepository,
	hasher ports.PasswordHasher,
	ids ports.IDGenerator,
	clock ports.Clock,
) Service {
	return Service{repository: repository, hasher: hasher, ids: ids, clock: clock}
}

func (s Service) Execute(ctx context.Context, input Input) (Result, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return Result{}, err
	}
	displayName := strings.TrimSpace(norm.NFKC.String(input.DisplayName))
	tenantName := strings.TrimSpace(norm.NFKC.String(input.TenantName))
	if displayName == "" || len([]rune(displayName)) > 100 {
		return Result{}, domain.NewRuleError("invalid_display_name", "姓名长度必须为 1–100 个字符", domain.ErrInvalidInput)
	}
	if tenantName == "" || len([]rune(tenantName)) > 120 {
		return Result{}, domain.NewRuleError("invalid_tenant_name", "工作区名称长度必须为 1–120 个字符", domain.ErrInvalidInput)
	}
	if _, ok := input.DefaultCurrency.Exponent(); !ok {
		return Result{}, domain.NewRuleError("unsupported_currency", "默认币种不受支持", domain.ErrInvalidInput)
	}
	if input.Timezone == "" {
		return Result{}, domain.NewRuleError("invalid_timezone", "时区不能为空", domain.ErrInvalidInput)
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return Result{}, domain.NewRuleError("invalid_timezone", "时区不是有效的 IANA 时区", domain.ErrInvalidInput)
	}
	passwordHash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return Result{}, domain.NewRuleError("invalid_password", "密码必须包含 12–1024 个字节", domain.ErrInvalidInput)
	}
	userID, err := s.ids.NewID()
	if err != nil {
		return Result{}, fmt.Errorf("generate bootstrap user id: %w", err)
	}
	tenantID, err := s.ids.NewID()
	if err != nil {
		return Result{}, fmt.Errorf("generate bootstrap tenant id: %w", err)
	}
	owner := ports.BootstrapOwner{
		UserID:          userID,
		TenantID:        tenantID,
		Email:           email,
		PasswordHash:    passwordHash,
		DisplayName:     displayName,
		TenantName:      tenantName,
		DefaultCurrency: input.DefaultCurrency,
		Timezone:        input.Timezone,
		CreatedAt:       s.clock.Now(),
	}
	if err := s.repository.BootstrapOwner(ctx, owner); err != nil {
		if errors.Is(err, domain.ErrBootstrapNotEmpty) {
			return Result{}, domain.NewRuleError(
				"bootstrap_not_empty",
				"bootstrap-owner 只允许在空数据库执行一次",
				domain.ErrBootstrapNotEmpty,
			)
		}
		return Result{}, err
	}
	return Result{UserID: userID, TenantID: tenantID}, nil
}

func normalizeEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(norm.NFKC.String(value)))
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized || len(normalized) > 254 {
		return "", domain.NewRuleError("invalid_email", "登录邮箱格式不正确", domain.ErrInvalidInput)
	}
	return normalized, nil
}
