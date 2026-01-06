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
	"smart-bill-manager/pkg/database"
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
	var expiresAt *time.Time
	if expiresInDays > 0 {
		t := time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}

	db := database.GetDB()
	for i := 0; i < 5; i++ {
		raw, err := generateRawInviteCode()
		if err != nil {
			return nil, err
		}
		normalized := normalizeInviteCode(raw)
		hash := inviteCodeHash(normalized)
		invite := &models.Invite{
			ID:        utils.GenerateUUID(),
			CodeHash:  hash,
			CodeHint:  inviteCodeHint(normalized),
			CreatedBy: createdByUserID,
			ExpiresAt: expiresAt,
		}
		if err := db.Create(invite).Error; err != nil {
			// In the unlikely event of a collision, retry a few times.
			continue
		}

		return &InviteCreateResult{
			Code:      formatInviteCode(normalized),
			CodeHint:  invite.CodeHint,
			ExpiresAt: expiresAt,
		}, nil
	}
	return nil, errors.New("failed to generate unique invite code")
}

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

	db := database.GetDB().WithContext(ctx)
	out := make([]models.Invite, 0, limit)
	if err := db.Order("created_at DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *AuthService) DeleteInvite(id string) error {
	db := database.GetDB()

	var inv models.Invite
	if err := db.Where("id = ?", id).First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}

	if inv.UsedAt != nil {
		return ErrInviteUsed
	}

	if err := db.Delete(&models.Invite{}, "id = ?", id).Error; err != nil {
		return err
	}
	return nil
}

func (s *AuthService) RegisterWithInvite(inviteCode, username, password string, email *string) (*AuthResult, error) {
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

	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		var inv models.Invite
		if err := tx.Where("code_hash = ?", hash).First(&inv).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("invalid invite: %w", err)
			}
			return err
		}

		if inv.UsedAt != nil {
			return fmt.Errorf("invite already used")
		}
		if inv.ExpiresAt != nil && inv.ExpiresAt.Before(now) {
			return fmt.Errorf("invite expired")
		}

		// Check username exists (within tx).
		var cnt int64
		if err := tx.Model(&models.User{}).Where("username = ?", username).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			return fmt.Errorf("username exists")
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
			return fmt.Errorf("invite already consumed")
		}

		createdUser = u
		return nil
	}); err != nil {
		switch {
		case strings.Contains(err.Error(), "invalid invite"):
			return &AuthResult{Success: false, Message: "邀请码无效"}, nil
		case strings.Contains(err.Error(), "invite already used"):
			return &AuthResult{Success: false, Message: "邀请码已被使用"}, nil
		case strings.Contains(err.Error(), "invite expired"):
			return &AuthResult{Success: false, Message: "邀请码已过期"}, nil
		case strings.Contains(err.Error(), "username exists"):
			return &AuthResult{Success: false, Message: "用户名已存在"}, nil
		case strings.Contains(err.Error(), "invite already consumed"):
			return &AuthResult{Success: false, Message: "邀请码已被使用"}, nil
		default:
			return nil, err
		}
	}

	// Generate token for the new user.
	token, err := utils.GenerateToken(createdUser.ID, createdUser.Username, createdUser.Role)
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
