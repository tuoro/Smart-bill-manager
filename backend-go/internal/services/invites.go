package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/utils"
)

type InviteCreateResult struct {
	Code      string     `json:"code"`
	CodeHint  string     `json:"code_hint"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func normalizeInviteCode(code string) string {
	s := strings.TrimSpace(code)
	s = strings.ToUpper(s)
	// Allow user-friendly formats like XXXX-XXXX-.... or with spaces.
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

func inviteCodeHash(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func formatInviteCode(normalized string) string {
	// Group by 4 for readability: XXXX-XXXX-....
	if len(normalized) <= 4 {
		return normalized
	}
	var b strings.Builder
	for i := 0; i < len(normalized); i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(normalized[i])
	}
	return b.String()
}

func inviteCodeHint(normalized string) string {
	if len(normalized) <= 8 {
		return normalized
	}
	return fmt.Sprintf("%s…%s", normalized[:4], normalized[len(normalized)-4:])
}

func generateRawInviteCode() (string, error) {
	// 20 random bytes -> 32 base32 chars (no padding)
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return strings.ToUpper(enc), nil
}

func (s *AuthService) CreateInvite(createdByUserID string, expiresInDays int) (*InviteCreateResult, error) {
	return s.CreateInviteCtx(context.Background(), createdByUserID, expiresInDays)
}

func (s *AuthService) CreateInviteCtx(ctx context.Context, createdByUserID string, expiresInDays int) (*InviteCreateResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var expiresAt *time.Time
	if expiresInDays > 0 {
		t := time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}

	raw, err := generateRawInviteCode()
	if err != nil {
		return nil, err
	}
	normalized := normalizeInviteCode(raw)
	invite := &models.Invite{
		ID:        utils.GenerateUUID(),
		CodeHash:  inviteCodeHash(normalized),
		CodeHint:  inviteCodeHint(normalized),
		CreatedBy: createdByUserID,
		ExpiresAt: expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(invite).Error; err != nil {
		return nil, fmt.Errorf("create invite: %w", err)
	}

	return &InviteCreateResult{
		Code:      formatInviteCode(normalized),
		CodeHint:  invite.CodeHint,
		ExpiresAt: expiresAt,
	}, nil
}

var (
	errInvalidInvite  = errors.New("invalid invite")
	errInviteExpired  = errors.New("invite expired")
	errInviteConsumed = errors.New("invite consumed")
	errUsernameExists = errors.New("username exists")
)

func (s *AuthService) ListInvites(limit int) ([]models.Invite, error) {
	return s.ListInvitesCtx(context.Background(), limit)
}

func (s *AuthService) ListInvitesCtx(ctx context.Context, limit int) ([]models.Invite, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}

	db := s.db.WithContext(ctx)
	out := make([]models.Invite, 0, limit)
	if err := db.Order("created_at DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *AuthService) DeleteInvite(id string) error {
	return s.DeleteInviteCtx(context.Background(), id)
}

func (s *AuthService) DeleteInviteCtx(ctx context.Context, id string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	db := s.db.WithContext(ctx)

	var inv models.Invite
	if err := db.Where("id = ?", id).First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}

	if inv.UsedAt != nil {
		usedBy := ""
		if inv.UsedBy != nil {
			usedBy = strings.TrimSpace(*inv.UsedBy)
		}
		if usedBy == "" {
			return ErrInviteUsed
		}
		exists, err := s.userRepo.ExistsByIDCtx(ctx, usedBy)
		if err != nil {
			return err
		}
		if exists {
			return ErrInviteUsed
		}
	}

	if err := db.Delete(&models.Invite{}, "id = ?", id).Error; err != nil {
		return err
	}
	return nil
}

func (s *AuthService) RegisterWithInvite(inviteCode, username, password string, email *string) (*AuthResult, error) {
	return s.RegisterWithInviteCtx(context.Background(), inviteCode, username, password, email)
}

func (s *AuthService) RegisterWithInviteCtx(ctx context.Context, inviteCode, username, password string, email *string) (*AuthResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized := normalizeInviteCode(inviteCode)
	if normalized == "" {
		return &AuthResult{Success: false, Message: "邀请码不能为空"}, nil
	}
	hash := inviteCodeHash(normalized)

	// Hash password (outside tx).
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	userID := utils.GenerateUUID()
	now := time.Now()
	var createdUser models.User

	db := s.db.WithContext(ctx)
	if err := db.Transaction(func(tx *gorm.DB) error {
		var inv models.Invite
		if err := tx.Where("code_hash = ?", hash).First(&inv).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errInvalidInvite
			}
			return err
		}

		if inv.UsedAt != nil {
			return errInviteConsumed
		}
		if inv.ExpiresAt != nil && inv.ExpiresAt.Before(now) {
			return errInviteExpired
		}

		// Check username exists (within tx).
		var cnt int64
		if err := tx.Model(&models.User{}).Where("username = ?", username).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			return errUsernameExists
		}

		u := models.User{
			ID:       userID,
			Username: username,
			Password: string(hashedPassword),
			Email:    email,
			Role:     "user",
			IsActive: 1,
		}
		if err := tx.Create(&u).Error; err != nil {
			return err
		}

		usedBy := userID
		res := tx.Model(&models.Invite{}).
			Where("id = ? AND used_at IS NULL", inv.ID).
			Updates(map[string]any{
				"used_at": now,
				"used_by": &usedBy,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errInviteConsumed
		}

		createdUser = u
		return nil
	}); err != nil {
		switch {
		case errors.Is(err, errInvalidInvite):
			return &AuthResult{Success: false, Message: "邀请码无效"}, nil
		case errors.Is(err, errInviteConsumed):
			return &AuthResult{Success: false, Message: "邀请码已被使用"}, nil
		case errors.Is(err, errInviteExpired):
			return &AuthResult{Success: false, Message: "邀请码已过期"}, nil
		case errors.Is(err, errUsernameExists):
			return &AuthResult{Success: false, Message: "用户名已存在"}, nil
		default:
			return nil, err
		}
	}

	// Generate token for the new user.
	token, err := s.tokenManager.GenerateToken(createdUser.ID, createdUser.Username, createdUser.Role)
	if err != nil {
		return nil, err
	}
	userResp := createdUser.ToResponse()
	return &AuthResult{
		Success: true,
		Message: "注册成功",
		User:    &userResp,
		Token:   token,
	}, nil
}
