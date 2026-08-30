package cryptography

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

const secretCipherVersion byte = 1

type SecretCipher struct {
	aead           cipher.AEAD
	fingerprintKey []byte
}

func NewSecretCipher(masterKey []byte) (SecretCipher, error) {
	if len(masterKey) != 32 {
		return SecretCipher{}, errors.New("master key must contain exactly 32 bytes")
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return SecretCipher{}, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return SecretCipher{}, fmt.Errorf("create AES-GCM: %w", err)
	}
	fingerprintKey := hmacSHA256(masterKey, []byte("smart-bill-manager/provider-fingerprint/v1"))
	return SecretCipher{aead: aead, fingerprintKey: fingerprintKey}, nil
}

func (c SecretCipher) Encrypt(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("secret cannot be empty")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate encryption nonce: %w", err)
	}
	result := make([]byte, 1, 1+len(nonce)+len(plaintext)+c.aead.Overhead())
	result[0] = secretCipherVersion
	result = append(result, nonce...)
	result = c.aead.Seal(result, nonce, plaintext, []byte("provider-api-key/v1"))
	return result, nil
}

func (c SecretCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	minimum := 1 + c.aead.NonceSize() + c.aead.Overhead()
	if len(ciphertext) < minimum || ciphertext[0] != secretCipherVersion {
		return nil, errors.New("invalid encrypted secret")
	}
	nonce := ciphertext[1 : 1+c.aead.NonceSize()]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext[1+c.aead.NonceSize():], []byte("provider-api-key/v1"))
	if err != nil {
		return nil, errors.New("encrypted secret authentication failed")
	}
	return plaintext, nil
}

func (c SecretCipher) Fingerprint(parts ...[]byte) string {
	mac := hmac.New(sha256.New, c.fingerprintKey)
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = mac.Write(length[:])
		_, _ = mac.Write(part)
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func hmacSHA256(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}
