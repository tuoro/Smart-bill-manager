package cryptography

import (
	"errors"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// ReadPrivateFile 在同一描述符上核实身份，避免先检查再打开的文件替换窗口。
func ReadPrivateFile(path string, maximum int64) ([]byte, error) {
	if maximum < 1 {
		return nil, errors.New("private file size limit is required")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("private file could not be opened")
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("private file descriptor is invalid")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, errors.New("private file could not be inspected")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !ok || stat.Nlink != 1 || int(stat.Uid) != os.Geteuid() {
		return nil, errors.New("private file must be singly linked and accessible only by its current owner")
	}
	value, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || len(value) == 0 || int64(len(value)) > maximum {
		clear(value)
		return nil, errors.New("private file is empty, unreadable or too large")
	}
	return value, nil
}
