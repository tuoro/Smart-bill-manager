package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"smart-bill-manager/internal/config"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"
)

const defaultSessionCookieName = "sbm_session"

var (
	sessionPepperOnce sync.Once
	sessionPepper     []byte
)

func getSessionPepper() []byte {
	sessionPepperOnce.Do(func() {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		sessionPepper = b
	})
	return sessionPepper
}

type SessionService struct {
	cookieName string
	ttl        time.Duration
}

func NewSessionService() *SessionService {
	ttl, err := time.ParseDuration(config.AppConfig.SessionExpiresIn)
	if err != nil || ttl <= 0 {
		ttl = 168 * time.Hour
	}
	name := strings.TrimSpace(config.AppConfig.SessionCookieName)
	if name == "" {
		name = defaultSessionCookieName
	}
	return &SessionService{cookieName: name, ttl: ttl}
}

func (s *SessionService) CookieName() string { return s.cookieName }

func (s *SessionService) Issue(c *gin.Context, userID string) (tokenHint string, err error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("userID is required")
	}

	raw, hint, hashHex, err := generateSessionToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	exp := now.Add(s.ttl)
	ua := strings.TrimSpace(c.GetHeader("User-Agent"))
	if ua == "" {
		ua = ""
	}
	ip := strings.TrimSpace(c.ClientIP())

	row := &models.Session{
		ID:        utils.GenerateUUID(),
		TokenHash: hashHex,
		TokenHint: hint,
		UserID:    userID,
		ExpiresAt: exp,
		LastSeen:  now,
	}
	if ua != "" {
		row.UserAgent = &ua
	}
	if ip != "" {
		row.IP = &ip
	}

	db := database.GetDB()
	if err := db.Create(row).Error; err != nil {
		return "", err
	}

	setSessionCookie(c, s.cookieName, raw, exp)
	return hint, nil
}

func (s *SessionService) Clear(c *gin.Context) {
	clearSessionCookie(c, s.cookieName)
}

func (s *SessionService) RevokeCurrent(c *gin.Context) error {
	raw, err := c.Cookie(s.cookieName)
	if err != nil || strings.TrimSpace(raw) == "" {
		s.Clear(c)
		return nil
	}
	hashHex := hashSessionToken(raw)
	db := database.GetDB()
	now := time.Now()
	_ = db.Model(&models.Session{}).Where("token_hash = ? AND revoked_at IS NULL", hashHex).Update("revoked_at", &now).Error
	s.Clear(c)
	return nil
}

func (s *SessionService) RevokeAllForUser(userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("userID is required")
	}
	db := database.GetDB()
	now := time.Now()
	return db.Model(&models.Session{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", &now).Error
}

func (s *SessionService) GetUserIDFromCookie(c *gin.Context) (string, error) {
	raw, err := c.Cookie(s.cookieName)
	if err != nil || strings.TrimSpace(raw) == "" {
		return "", ErrUnauthorized
	}
	hashHex := hashSessionToken(raw)

	db := database.GetDB()
	now := time.Now()
	var sess models.Session
	if err := db.
		Where("token_hash = ?", hashHex).
		Where("revoked_at IS NULL").
		Where("expires_at > ?", now).
		First(&sess).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", ErrUnauthorized
		}
		return "", err
	}

	// Touch last_seen (best effort).
	_ = db.Model(&models.Session{}).Where("id = ?", sess.ID).Update("last_seen", now).Error
	return sess.UserID, nil
}

func generateSessionToken() (raw string, hint string, hashHex string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	hashHex = hashSessionToken(raw)
	hint = tokenHint(raw)
	return raw, hint, hashHex, nil
}

func hashSessionToken(raw string) string {
	h := sha256.New()
	h.Write(getSessionPepper())
	h.Write([]byte(raw))
	return hex.EncodeToString(h.Sum(nil))
}

func tokenHint(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) <= 10 {
		return s
	}
	return fmt.Sprintf("%s…%s", s[:6], s[len(s)-4:])
}

func setSessionCookie(c *gin.Context, name, value string, expiresAt time.Time) {
	secure := config.AppConfig.NodeEnv == "production"
	if config.AppConfig.CookieSecure != nil {
		secure = *config.AppConfig.CookieSecure
	}
	sameSite := http.SameSiteLaxMode
	switch strings.ToLower(strings.TrimSpace(config.AppConfig.CookieSameSite)) {
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		sameSite = http.SameSiteNoneMode
	case "lax", "":
		sameSite = http.SameSiteLaxMode
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Expires:  expiresAt,
	})
}

func clearSessionCookie(c *gin.Context, name string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   config.AppConfig.NodeEnv == "production",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}
