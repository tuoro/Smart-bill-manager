package cryptography

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

func LoadMasterKeyFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect master key file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("master key file must be regular and accessible only by its owner")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open master key file: %w", err)
	}
	defer file.Close()
	encoded, err := io.ReadAll(io.LimitReader(file, 129))
	if err != nil {
		return nil, fmt.Errorf("read master key file: %w", err)
	}
	if len(encoded) > 128 {
		return nil, errors.New("master key file is too large")
	}
	encoded = trimLineEnding(encoded)
	if len(encoded) == 32 {
		return encoded, nil
	}
	if decoded, err := hex.DecodeString(string(encoded)); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(string(encoded)); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	return nil, errors.New("master key must be 32 raw bytes, 64 hexadecimal characters, or padded base64")
}

func trimLineEnding(value []byte) []byte {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		value = value[:len(value)-1]
		if len(value) > 0 && value[len(value)-1] == '\r' {
			value = value[:len(value)-1]
		}
	}
	return value
}
