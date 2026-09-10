package dingtalk

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// 应用密钥与主密钥同一套纪律：只读、不跟随符号链接、单链接普通文件、仅属主可读。
// 它能换到 access token，进而下载成员发来的原件，暴露面不比数据库口令小。
func LoadAppSecretFile(path string) (string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", fmt.Errorf("open dingtalk secret file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return "", errors.New("open dingtalk secret file: invalid file descriptor")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect dingtalk secret file: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !ok || stat.Nlink != 1 {
		return "", errors.New("dingtalk secret file must be a singly linked regular file accessible only by its owner")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 513))
	if err != nil {
		return "", fmt.Errorf("read dingtalk secret file: %w", err)
	}
	if len(raw) > 512 {
		return "", errors.New("dingtalk secret file is too large")
	}
	secret := string(bytes.TrimRight(raw, "\r\n"))
	if secret == "" {
		return "", errors.New("dingtalk secret file is empty")
	}
	return secret, nil
}
