package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const emailPasswordEncPrefix = "enc:v1:"
const emailPasswordKeyFileName = "email_password.key"

var (
	emailPasswordKeyOnce sync.Once
	emailPasswordKey     []byte
	emailPasswordKeyErr  error
)

func getEmailPasswordKey() ([]byte, error) {
	emailPasswordKeyOnce.Do(func() {
		emailPasswordKey, emailPasswordKeyErr = loadEmailPasswordKey()
	})
	return emailPasswordKey, emailPasswordKeyErr
}

func loadEmailPasswordKey() ([]byte, error) {
	// 1) Explicit key
	if raw := strings.TrimSpace(os.Getenv("SBM_EMAIL_PASSWORD_KEY")); raw != "" {
		k, err := parseEmailPasswordKey(raw)
		if err != nil {
			return nil, err
		}
		log.Printf("[Email Monitor] email password encryption key: using SBM_EMAIL_PASSWORD_KEY")
		return k, nil
	}

	// 2) Local key file in DATA_DIR (default ./data). This keeps encryption always-on without extra env.
	keyPath := emailPasswordKeyFilePath()
	if b, err := os.ReadFile(keyPath); err == nil {
		k, err := parseEmailPasswordKey(string(b))
		if err != nil {
			return nil, fmt.Errorf("parse email password key file %s: %w", keyPath, err)
		}
		log.Printf("[Email Monitor] email password encryption key: using local key file (%s)", keyPath)
		return k, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read email password key file %s: %w", keyPath, err)
	}

	// Generate + persist a new key.
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o755); err != nil {
		return nil, fmt.Errorf("ensure key dir: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate email password key: %w", err)
	}
	encoded := base64.RawStdEncoding.EncodeToString(key)
	if err := os.WriteFile(keyPath, []byte(encoded), 0o600); err != nil {
		return nil, fmt.Errorf("write email password key file %s: %w", keyPath, err)
	}
	log.Printf("[Email Monitor] email password encryption key: generated local key file (%s) - keep it with your DB backups", keyPath)
	return key, nil
}

func emailPasswordKeyFilePath() string {
	// Allow overriding the path (e.g. in Docker secrets).
	if p := strings.TrimSpace(os.Getenv("SBM_EMAIL_PASSWORD_KEY_FILE")); p != "" {
		return p
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "./data"
	}
	return filepath.Join(dataDir, emailPasswordKeyFileName)
}

func parseEmailPasswordKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty key")
	}

	// Support "hex:" prefix, raw hex, or base64.
	if strings.HasPrefix(strings.ToLower(raw), "hex:") {
		raw = strings.TrimSpace(raw[4:])
	}
	if isHexString(raw) {
		b, err := hex.DecodeString(raw)
		if err != nil {
			return nil, err
		}
		if len(b) != 32 {
			return nil, fmt.Errorf("email password key must be 32 bytes (got %d)", len(b))
		}
		return b, nil
	}

	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		// Try raw base64 without padding.
		b, err = base64.RawStdEncoding.DecodeString(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("email password key must be base64 or hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("email password key must be 32 bytes (got %d)", len(b))
	}
	return b, nil
}

func isHexString(s string) bool {
	if s == "" || len(s)%2 != 0 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func encryptEmailPassword(plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", nil
	}
	// Already encrypted.
	if strings.HasPrefix(plain, emailPasswordEncPrefix) {
		return plain, nil
	}

	key, err := getEmailPasswordKey()
	if err != nil {
		return "", err
	}
	if len(key) == 0 {
		return "", errors.New("email password encryption key is not configured")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plain), nil)
	payload := append(nonce, ciphertext...)
	return emailPasswordEncPrefix + base64.StdEncoding.EncodeToString(payload), nil
}

func decryptEmailPassword(stored string) (string, error) {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, emailPasswordEncPrefix) {
		// Forced: refuse using plaintext storage (legacy DBs should be migrated on startup).
		return "", errors.New("email password is stored in plaintext; please restart to migrate or re-save the email config")
	}

	key, err := getEmailPasswordKey()
	if err != nil {
		return "", err
	}
	if len(key) == 0 {
		return "", errors.New("email password encryption key is not configured")
	}

	enc := strings.TrimSpace(strings.TrimPrefix(stored, emailPasswordEncPrefix))
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted password payload")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
