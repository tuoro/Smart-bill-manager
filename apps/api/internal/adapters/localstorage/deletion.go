package localstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

const deletionManifestName = "manifest.json"

type deletionManifest struct {
	StorageKeys []string `json:"storage_keys"`
}

func (s *Store) StageDeletion(ctx context.Context, deletionID string, storageKeys []string) error {
	if !safeUUID(deletionID) || len(storageKeys) == 0 {
		return domain.ErrInvalidInput
	}
	batch, err := s.deletionBatchPath(deletionID)
	if err != nil {
		return err
	}
	if err := os.Mkdir(batch, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return domain.ErrConflict
		}
		return fmt.Errorf("create deletion batch: %w", err)
	}
	manifest := deletionManifest{StorageKeys: append([]string(nil), storageKeys...)}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		_ = os.RemoveAll(batch)
		return fmt.Errorf("encode deletion manifest: %w", err)
	}
	manifestFile, err := os.OpenFile(filepath.Join(batch, deletionManifestName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.RemoveAll(batch)
		return fmt.Errorf("create deletion manifest: %w", err)
	}
	if _, err := manifestFile.Write(encoded); err != nil {
		_ = manifestFile.Close()
		_ = os.RemoveAll(batch)
		return fmt.Errorf("write deletion manifest: %w", err)
	}
	if err := manifestFile.Sync(); err != nil {
		_ = manifestFile.Close()
		_ = os.RemoveAll(batch)
		return fmt.Errorf("sync deletion manifest: %w", err)
	}
	if err := manifestFile.Close(); err != nil {
		_ = os.RemoveAll(batch)
		return fmt.Errorf("close deletion manifest: %w", err)
	}
	moved := make([]string, 0, len(storageKeys))
	for _, storageKey := range storageKeys {
		if err := ctx.Err(); err != nil {
			_ = s.restoreMoved(batch, moved)
			_ = os.RemoveAll(batch)
			return err
		}
		source, err := s.objectPath(storageKey)
		if err != nil {
			_ = s.restoreMoved(batch, moved)
			_ = os.RemoveAll(batch)
			return err
		}
		destination := filepath.Join(batch, deletionEntryName(storageKey))
		if err := os.Rename(source, destination); err != nil {
			_ = s.restoreMoved(batch, moved)
			_ = os.RemoveAll(batch)
			if errors.Is(err, os.ErrNotExist) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("stage object deletion: %w", err)
		}
		moved = append(moved, storageKey)
	}
	return nil
}

func (s *Store) RestoreDeletion(ctx context.Context, deletionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	batch, manifest, err := s.readDeletionManifest(deletionID)
	if err != nil {
		return err
	}
	if err := s.restoreMoved(batch, manifest.StorageKeys); err != nil {
		return err
	}
	if err := os.RemoveAll(batch); err != nil {
		return fmt.Errorf("remove restored deletion batch: %w", err)
	}
	return nil
}

func (s *Store) PurgeDeletion(ctx context.Context, deletionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	batch, err := s.deletionBatchPath(deletionID)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(batch, deletionManifestName)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect deletion manifest: %w", err)
	}
	if err := os.RemoveAll(batch); err != nil {
		return fmt.Errorf("purge deletion batch: %w", err)
	}
	return nil
}

func (s *Store) PendingDeletions(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "trash"))
	if err != nil {
		return nil, fmt.Errorf("list deletion batches: %w", err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || !safeUUID(entry.Name()) {
			return nil, errors.New("deletion trash contains an invalid entry")
		}
		result = append(result, entry.Name())
	}
	sort.Strings(result)
	return result, nil
}

func (s *Store) readDeletionManifest(deletionID string) (string, deletionManifest, error) {
	batch, err := s.deletionBatchPath(deletionID)
	if err != nil {
		return "", deletionManifest{}, err
	}
	content, err := os.ReadFile(filepath.Join(batch, deletionManifestName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", deletionManifest{}, domain.ErrNotFound
		}
		return "", deletionManifest{}, fmt.Errorf("read deletion manifest: %w", err)
	}
	var manifest deletionManifest
	if err := json.Unmarshal(content, &manifest); err != nil || len(manifest.StorageKeys) == 0 {
		return "", deletionManifest{}, errors.New("deletion manifest is invalid")
	}
	return batch, manifest, nil
}

func (s *Store) restoreMoved(batch string, storageKeys []string) error {
	for index := len(storageKeys) - 1; index >= 0; index-- {
		storageKey := storageKeys[index]
		source := filepath.Join(batch, deletionEntryName(storageKey))
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect staged deletion: %w", err)
		}
		destination, err := s.objectPath(storageKey)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("create restored object directory: %w", err)
		}
		if _, err := os.Lstat(destination); err == nil {
			return errors.New("cannot restore deletion over an existing object")
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect restore destination: %w", err)
		}
		if err := os.Rename(source, destination); err != nil {
			return fmt.Errorf("restore staged deletion: %w", err)
		}
	}
	return nil
}

func (s *Store) deletionBatchPath(deletionID string) (string, error) {
	if !safeUUID(deletionID) {
		return "", domain.ErrInvalidInput
	}
	return filepath.Join(s.root, "trash", deletionID), nil
}

func deletionEntryName(storageKey string) string {
	hash := sha256.Sum256([]byte(storageKey))
	return hex.EncodeToString(hash[:])
}
