package cryptography

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultArgon2Params = Argon2Params{
	MemoryKiB:   64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

type PasswordHasher struct {
	params Argon2Params
}

func NewPasswordHasher(params Argon2Params) (PasswordHasher, error) {
	if params.MemoryKiB < 8*1024 || params.Iterations == 0 || params.Parallelism == 0 ||
		params.SaltLength < 16 || params.KeyLength < 32 {
		return PasswordHasher{}, errors.New("unsafe argon2 parameters")
	}
	return PasswordHasher{params: params}, nil
}

func (h PasswordHasher) Hash(password []byte) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey(password, salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyLength)
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		h.params.MemoryKiB,
		h.params.Iterations,
		h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func (h PasswordHasher) Verify(password []byte, encoded string) (bool, error) {
	if len(password) == 0 || len(password) > 1024 {
		return false, nil
	}
	params, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey(password, salt, params.Iterations, params.MemoryKiB, params.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func validatePassword(password []byte) error {
	if len(password) < 12 || len(password) > 1024 {
		return errors.New("password must contain 12 to 1024 bytes")
	}
	return nil
}

func parsePasswordHash(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return Argon2Params{}, nil, nil, errors.New("invalid password hash format")
	}
	var params Argon2Params
	settings := strings.Split(parts[3], ",")
	if len(settings) != 3 {
		return Argon2Params{}, nil, nil, errors.New("invalid password hash settings")
	}
	memory, err := parseUintSetting(settings[0], "m=")
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	iterations, err := parseUintSetting(settings[1], "t=")
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	parallelism, err := parseUintSetting(settings[2], "p=")
	if err != nil || parallelism > 255 {
		return Argon2Params{}, nil, nil, errors.New("invalid password hash parallelism")
	}
	params.MemoryKiB = uint32(memory)
	params.Iterations = uint32(iterations)
	params.Parallelism = uint8(parallelism)
	if params.MemoryKiB < 8*1024 || params.MemoryKiB > 1024*1024 || params.Iterations == 0 || params.Iterations > 10 || params.Parallelism == 0 {
		return Argon2Params{}, nil, nil, errors.New("password hash parameters out of bounds")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return Argon2Params{}, nil, nil, errors.New("invalid password hash salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 32 || len(expected) > 64 {
		return Argon2Params{}, nil, nil, errors.New("invalid password hash key")
	}
	return params, salt, expected, nil
}

func parseUintSetting(value, prefix string) (uint64, error) {
	if !strings.HasPrefix(value, prefix) {
		return 0, errors.New("invalid password hash setting")
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 32)
	if err != nil {
		return 0, errors.New("invalid password hash setting")
	}
	return parsed, nil
}
