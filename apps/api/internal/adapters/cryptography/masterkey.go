package cryptography

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func LoadMasterKeyFile(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open master key file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open master key file: invalid file descriptor")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect master key file: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !ok || stat.Nlink != 1 {
		return nil, errors.New("master key file must be a singly linked regular file accessible only by its owner")
	}
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
