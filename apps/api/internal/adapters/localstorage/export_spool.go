package localstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const exportDirectory = "export-spool"
const exportPrefix = "sbm-export-"

// 只在启动时收口创建到 unlink 之间的零字节残留，不扫描或删除业务对象。
func (s *Store) InitializeExportSpool() error {
	directory := filepath.Join(s.root, exportDirectory)
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("export spool must be an owner-only real directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !strings.HasPrefix(entry.Name(), exportPrefix) || !info.Mode().IsRegular() || info.Size() != 0 || info.Mode().Perm() != 0o600 || !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
			return errors.New("export spool contains an unexpected entry; operator review required")
		}
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateExportFile(ctx context.Context) (ports.ExportFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Join(s.root, exportDirectory), exportPrefix)
	if err != nil {
		return nil, err
	}
	// 在任何业务字节写入前解除名称。失败不继续生成文件。
	if err := os.Remove(file.Name()); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
