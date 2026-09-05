// Package restorestate 保存恢复副本的对象配对身份；完成阶段只属于 PostgreSQL。
package restorestate

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
)

const FileName = "restore-identity.json"

var ErrNotReady = errors.New("restore activation is incomplete or identity is invalid")
var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Identity struct {
	Version      int    `json:"version"`
	RestoreID    string `json:"restore_id"`
	DatabaseOID  int64  `json:"database_oid"`
	DatabaseName string `json:"database_name"`
}

func (identity Identity) Valid() bool {
	return identity.Version == 1 && idPattern.MatchString(identity.RestoreID) && identity.DatabaseOID > 0 && identity.DatabaseName != ""
}

// CheckObjects 必须在 localstorage.New 之前运行，不能以自动建目录掩盖缺失副本。
func CheckObjects(root string, expected Identity) error {
	if root == "" || realDirectories(root) != nil {
		return ErrNotReady
	}
	path := filepath.Join(root, FileName)
	_, err := os.Lstat(path)
	if expected == (Identity{}) {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return ErrNotReady
	}
	if !expected.Valid() || err != nil {
		return ErrNotReady
	}
	actual, err := Read(root)
	if err != nil || actual != expected {
		return ErrNotReady
	}
	return nil
}

func Read(root string) (Identity, error) {
	if err := realDirectories(root); err != nil {
		return Identity{}, err
	}
	content, err := cryptography.ReadPrivateFile(filepath.Join(root, FileName), 1024)
	if err != nil {
		return Identity{}, ErrNotReady
	}
	var identity Identity
	if json.Unmarshal(content, &identity) != nil || !identity.Valid() {
		return Identity{}, ErrNotReady
	}
	canonical, err := json.Marshal(identity)
	if err != nil || !bytes.Equal(content, append(canonical, '\n')) {
		return Identity{}, ErrNotReady
	}
	return identity, nil
}

func Write(root string, identity Identity) error {
	if !identity.Valid() || realDirectories(root) != nil {
		return ErrNotReady
	}
	content, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(root, FileName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrNotReady
	}
	defer file.Close()
	if _, err := file.Write(append(content, '\n')); err != nil {
		return ErrNotReady
	}
	if err := file.Sync(); err != nil {
		return ErrNotReady
	}
	directory, err := os.Open(root)
	if err != nil {
		return ErrNotReady
	}
	defer directory.Close()
	return directory.Sync()
}

func realDirectories(root string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return ErrNotReady
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrNotReady
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}
