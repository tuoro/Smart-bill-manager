package localstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type Store struct {
	root string
	ids  system.IDGenerator
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("object store root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve object store root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absolute, "staging"), 0o700); err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absolute, "objects"), 0o700); err != nil {
		return nil, fmt.Errorf("create objects directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absolute, "trash"), 0o700); err != nil {
		return nil, fmt.Errorf("create deletion trash directory: %w", err)
	}
	return &Store{root: absolute, ids: system.IDGenerator{}}, nil
}

func (s *Store) Stage(ctx context.Context, source io.Reader, maxBytes int64) (ports.StagedObject, error) {
	if source == nil || maxBytes < 1 {
		return ports.StagedObject{}, domain.ErrInvalidInput
	}
	id, err := s.ids.NewID()
	if err != nil {
		return ports.StagedObject{}, err
	}
	location := filepath.Join(s.root, "staging", id)
	file, err := os.OpenFile(location, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ports.StagedObject{}, fmt.Errorf("create staged object: %w", err)
	}
	hash := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(file, hash), io.LimitReader(source, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(location)
		return ports.StagedObject{}, fmt.Errorf("stage object: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(location)
		return ports.StagedObject{}, fmt.Errorf("close staged object: %w", closeErr)
	}
	if written > maxBytes {
		_ = os.Remove(location)
		return ports.StagedObject{}, domain.NewRuleError(
			"document_too_large",
			"文件不能超过 20 MiB",
			domain.ErrPayloadTooLarge,
		)
	}
	if written == 0 {
		_ = os.Remove(location)
		return ports.StagedObject{}, domain.NewRuleError("empty_document", "文件内容不能为空", domain.ErrInvalidInput)
	}
	return ports.StagedObject{ID: id, Size: written, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (s *Store) Commit(_ context.Context, staged ports.StagedObject, storageKey string) error {
	source, err := s.stagePath(staged.ID)
	if err != nil {
		return err
	}
	destination, err := s.objectPath(storageKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create object directory: %w", err)
	}
	if _, err := os.Lstat(destination); err == nil {
		existingHash, hashErr := fileSHA256(destination)
		if hashErr != nil {
			return hashErr
		}
		if existingHash != staged.SHA256 {
			return errors.New("object destination already exists with different content")
		}
		if err := os.Remove(source); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("discard duplicate staged object: %w", err)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect object destination: %w", err)
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("commit staged object: %w", err)
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		return fmt.Errorf("protect committed object: %w", err)
	}
	return nil
}

func fileSHA256(location string) (string, error) {
	file, err := os.Open(location)
	if err != nil {
		return "", fmt.Errorf("open object for hash: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash object: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Store) Abort(_ context.Context, staged ports.StagedObject) error {
	location, err := s.stagePath(staged.ID)
	if err != nil {
		return err
	}
	if err := os.Remove(location); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged object: %w", err)
	}
	return nil
}

func (s *Store) Open(_ context.Context, storageKey string) (io.ReadCloser, error) {
	location, err := s.objectPath(storageKey)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(location)
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open object: %w", err)
	}
	return file, nil
}

func (s *Store) Delete(_ context.Context, storageKey string) error {
	location, err := s.objectPath(storageKey)
	if err != nil {
		return err
	}
	if err := os.Remove(location); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func (s *Store) stagePath(id string) (string, error) {
	if !safeUUID(id) {
		return "", domain.ErrInvalidInput
	}
	return filepath.Join(s.root, "staging", id), nil
}

func (s *Store) objectPath(storageKey string) (string, error) {
	clean := path.Clean(storageKey)
	if clean != storageKey || clean == "." || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") {
		return "", domain.ErrInvalidInput
	}
	return filepath.Join(s.root, "objects", filepath.FromSlash(clean)), nil
}

func safeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}
