package localstorage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const publicationDirectory = "material-publications"

func validMaterialPublication(p ports.MaterialPublication) bool {
	return safeUUID(p.ID) && safeUUID(p.DocumentID) && safeUUID(p.Staged.ID) &&
		p.TenantID != "" && p.TenantID != "." && p.TenantID != ".." && !strings.ContainsAny(p.TenantID, "/\\\x00") &&
		p.StorageKey == "tenants/"+p.TenantID+"/documents/"+p.DocumentID+"/original" &&
		domain.ValidSHA256Hex(p.Staged.SHA256) && p.Staged.Size > 0 && p.Staged.Size <= ports.MaxDocumentBytes
}

func (s *Store) RecordMaterialPublication(ctx context.Context, p ports.MaterialPublication) (resultErr error) {
	if !validMaterialPublication(p) {
		return domain.ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Join(s.root, publicationDirectory)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		return err
	}
	// 先写不可见的暂存意图；完整落盘后才允许发布原件。
	file, err := os.CreateTemp(filepath.Join(s.root, "staging"), "publication-")
	if err != nil {
		return err
	}
	defer func() {
		removeErr := os.Remove(file.Name())
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, removeErr)
		}
	}()
	_, writeErr := file.Write(encoded)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	target := filepath.Join(directory, p.ID+".json")
	// 调用方持有同一发布 ID 的数据库锁，原子改名避免暴露半写意图。
	if _, err := os.Lstat(target); err == nil {
		return domain.ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(file.Name(), target); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func (s *Store) PendingMaterialPublications(ctx context.Context, limit int) ([]ports.MaterialPublication, error) {
	if limit < 1 || limit > 100 {
		return nil, domain.ErrInvalidInput
	}
	directory := filepath.Join(s.root, publicationDirectory)
	folder, err := os.Open(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []ports.MaterialPublication{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer folder.Close()
	entries, err := folder.ReadDir(limit)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	result := make([]ports.MaterialPublication, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || !safeUUID(strings.TrimSuffix(name, ".json")) || entry.Type() != 0 {
			return nil, errors.New("invalid material publication entry")
		}
		p, err := readMaterialPublication(filepath.Join(directory, name))
		if err != nil {
			return nil, err
		}
		if p.ID+".json" != name {
			return nil, errors.New("material publication filename mismatch")
		}
		result = append(result, p)
	}
	return result, nil
}

func readMaterialPublication(path string) (ports.MaterialPublication, error) {
	var p ports.MaterialPublication
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return p, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return p, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || info.Size() > 4096 {
		return p, errors.New("invalid material publication file")
	}
	content, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return p, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return p, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || !validMaterialPublication(p) {
		return p, errors.New("invalid material publication identity")
	}
	return p, nil
}

func (s *Store) FinishMaterialPublication(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !safeUUID(id) {
		return domain.ErrInvalidInput
	}
	directory := filepath.Join(s.root, publicationDirectory)
	if err := os.Remove(filepath.Join(directory, id+".json")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncDirectory(directory)
}

func (s *Store) GetMaterialPublication(ctx context.Context, id string) (ports.MaterialPublication, error) {
	if err := ctx.Err(); err != nil {
		return ports.MaterialPublication{}, err
	}
	if !safeUUID(id) {
		return ports.MaterialPublication{}, domain.ErrInvalidInput
	}
	p, err := readMaterialPublication(filepath.Join(s.root, publicationDirectory, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, domain.ErrNotFound
	}
	return p, err
}

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}
