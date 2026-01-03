package services

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	paseto "aidanwoods.dev/go-paseto"
	"gorm.io/gorm"

	"smart-bill-manager/internal/config"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"
)

// APITokenService issues and verifies PASETO v4.local tokens for non-browser clients.
// Tokens are revocable because their token_id must exist in DB and be active.
type APITokenService struct {
	key    paseto.V4SymmetricKey
	parser paseto.Parser
}

func NewAPITokenService() *APITokenService {
	key := loadV4LocalKey()
	return &APITokenService{
		key:    key,
		parser: paseto.NewParserForValidNow(),
	}
}

func loadV4LocalKey() paseto.V4SymmetricKey {
	raw := strings.TrimSpace(config.AppConfig.PasetoV4LocalKey)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("PASETO_KEY"))
	}

	if raw != "" {
		if b, err := base64.RawStdEncoding.DecodeString(raw); err == nil {
			if key, err := paseto.V4SymmetricKeyFromBytes(b); err == nil {
				return key
			}
		}
		if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
			if key, err := paseto.V4SymmetricKeyFromBytes(b); err == nil {
				return key
			}
		}
		if key, err := paseto.V4SymmetricKeyFromHex(raw); err == nil {
			return key
		}
		log.Println("⚠️ WARNING: invalid PASETO_V4_LOCAL_KEY; generating a new one (tokens will reset on restart).")
	}

	if config.AppConfig.NodeEnv == "production" {
		log.Println("⚠️ WARNING: PASETO_V4_LOCAL_KEY not set in production. Using generated key (will change on restart).")
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	key, _ := paseto.V4SymmetricKeyFromBytes(b)
	return key
}

type CreateAPITokenInput struct {
	Name          string
	ExpiresInDays int
}

type CreateAPITokenResult struct {
	Token     string     `json:"token"`
	TokenHint string     `json:"token_hint"`
	ID        string     `json:"id"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (s *APITokenService) CreateForUser(userID string, input CreateAPITokenInput) (*CreateAPITokenResult, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "API Token"
	}
	if input.ExpiresInDays < 0 || input.ExpiresInDays > 3650 {
		return nil, fmt.Errorf("expiresInDays out of range")
	}

	var expiresAt *time.Time
	if input.ExpiresInDays > 0 {
		t := time.Now().Add(time.Duration(input.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}

	tokenID := utils.GenerateUUID()

	tok := paseto.NewToken()
	tok.SetIssuedAt(time.Now())
	if expiresAt != nil {
		tok.SetExpiration(*expiresAt)
	}
	tok.SetString("tid", tokenID)
	tok.SetString("uid", userID)

	encrypted := tok.V4Encrypt(s.key, nil)
	hint := tokenHint(encrypted)

	row := &models.APIToken{
		ID:        tokenID,
		Name:      name,
		UserID:    userID,
		TokenHint: hint,
		ExpiresAt: expiresAt,
	}
	db := database.GetDB()
	if err := db.Create(row).Error; err != nil {
		return nil, err
	}

	return &CreateAPITokenResult{
		Token:     encrypted,
		TokenHint: hint,
		ID:        tokenID,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *APITokenService) VerifyBearer(tokenString string) (userID string, err error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return "", ErrUnauthorized
	}

	tok, err := s.parser.ParseV4Local(s.key, tokenString, nil)
	if err != nil {
		return "", ErrUnauthorized
	}

	tid, err := tok.GetString("tid")
	if err != nil || strings.TrimSpace(tid) == "" {
		return "", ErrUnauthorized
	}
	uid, err := tok.GetString("uid")
	if err != nil || strings.TrimSpace(uid) == "" {
		return "", ErrUnauthorized
	}

	db := database.GetDB()
	now := time.Now()
	var row models.APIToken
	if err := db.Where("id = ? AND user_id = ? AND revoked_at IS NULL", tid, uid).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrUnauthorized
		}
		return "", err
	}
	if row.ExpiresAt != nil && row.ExpiresAt.Before(now) {
		return "", ErrUnauthorized
	}

	_ = db.Model(&models.APIToken{}).Where("id = ?", row.ID).Update("last_used_at", now).Error
	return uid, nil
}

func (s *APITokenService) ListByUser(userID string, limit int) ([]models.APIToken, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	db := database.GetDB()
	out := make([]models.APIToken, 0, limit)
	if err := db.Where("user_id = ?", userID).Order("created_at DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *APITokenService) Revoke(tokenID, userID string) error {
	tokenID = strings.TrimSpace(tokenID)
	userID = strings.TrimSpace(userID)
	if tokenID == "" || userID == "" {
		return fmt.Errorf("token_id and user_id are required")
	}
	db := database.GetDB()
	now := time.Now()
	res := db.Model(&models.APIToken{}).Where("id = ? AND user_id = ? AND revoked_at IS NULL", tokenID, userID).Update("revoked_at", &now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Some operators prefer to provision a fixed key via hex/base64; show a short hint for auditing UI.
func PasetoKeyHint(key string) string {
	k := strings.TrimSpace(key)
	if k == "" {
		return ""
	}
	if len(k) <= 12 {
		return k
	}
	return k[:6] + "…" + k[len(k)-4:]
}

// Debug helper: allow operators to copy the generated key if needed (not used by the app).
func EncodeV4LocalKeyBase64(keyBytes []byte) string {
	return base64.RawStdEncoding.EncodeToString(keyBytes)
}

// Decode helper used in tests/ops.
func DecodeV4LocalKeyHex(hexStr string) ([]byte, error) {
	return hex.DecodeString(strings.TrimSpace(hexStr))
}

