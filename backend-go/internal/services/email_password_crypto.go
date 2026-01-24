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
	"os"
	"strings"
)

const emailPasswordEncPrefix = "enc:v1:"

func getEmailPasswordKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("SBM_EMAIL_PASSWORD_KEY"))
	if raw == "" {
		return nil, nil
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
			return nil, fmt.Errorf("SBM_EMAIL_PASSWORD_KEY must be 32 bytes (got %d)", len(b))
		}
		return b, nil
	}

	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		// Try raw base64 without padding.
		b, err = base64.RawStdEncoding.DecodeString(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("SBM_EMAIL_PASSWORD_KEY must be base64 or hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("SBM_EMAIL_PASSWORD_KEY must be 32 bytes (got %d)", len(b))
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
	// Backward compatible: no key configured -> store plaintext.
	if len(key) == 0 {
		return plain, nil
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
		return stored, nil
	}

	key, err := getEmailPasswordKey()
	if err != nil {
		return "", err
	}
	if len(key) == 0 {
		return "", errors.New("email password is encrypted but SBM_EMAIL_PASSWORD_KEY is not set")
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

