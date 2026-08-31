package runtimeguard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	lockSuffix       = ".runtime.lock"
	activationSuffix = ".restore-state"
	incompleteState  = "smart-bill-manager-restore-incomplete/1\n"
	completeState    = "smart-bill-manager-restore-complete/1\n"
)

type Guard struct {
	file *os.File
}

func LockPath(databasePath string) string {
	return databasePath + lockSuffix
}

func ActivationPath(databasePath string) string {
	return databasePath + activationSuffix
}

func AcquireExclusive(databasePath string) (*Guard, error) {
	if databasePath == "" || databasePath == ":memory:" {
		return nil, errors.New("runtime lock requires a filesystem database")
	}
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := rejectIncompleteRestore(absolute); err != nil {
		return nil, err
	}
	if err := rejectUnsafeDatabasePath(absolute); err != nil {
		return nil, err
	}
	lockPath := LockPath(absolute)
	if err := requireRealDirectory(filepath.Dir(lockPath)); err != nil {
		return nil, fmt.Errorf("runtime lock parent: %w", err)
	}
	fd, err := unix.Open(lockPath, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open runtime lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), lockPath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open runtime lock: invalid file descriptor")
	}
	closeWithError := func(cause error) (*Guard, error) {
		_ = file.Close()
		return nil, cause
	}
	information, err := file.Stat()
	if err != nil {
		return closeWithError(fmt.Errorf("inspect runtime lock: %w", err))
	}
	if !information.Mode().IsRegular() || information.Mode().Perm()&0o077 != 0 {
		return closeWithError(errors.New("runtime lock must be a regular owner-only file"))
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return closeWithError(errors.New("runtime lock is already held; stop the application and local writers"))
		}
		return closeWithError(fmt.Errorf("acquire runtime lock: %w", err))
	}
	if err := rejectIncompleteRestore(absolute); err != nil {
		return closeWithError(err)
	}
	if err := rejectUnsafeDatabasePath(absolute); err != nil {
		return closeWithError(err)
	}
	return &Guard{file: file}, nil
}

func (g *Guard) Close() error {
	if g == nil || g.file == nil {
		return nil
	}
	file := g.file
	g.file = nil
	if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
		_ = file.Close()
		return fmt.Errorf("release runtime lock: %w", err)
	}
	return file.Close()
}

func rejectIncompleteRestore(databasePath string) error {
	location := ActivationPath(databasePath)
	if err := requireRealDirectory(filepath.Dir(location)); err != nil {
		return fmt.Errorf("restore activation state parent: %w", err)
	}
	information, err := os.Lstat(location)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect restore activation marker: %w", err)
	}
	if !information.Mode().IsRegular() || information.Mode().Perm()&0o077 != 0 {
		return errors.New("restore activation state is not a regular owner-only file")
	}
	fd, err := unix.Open(location, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open restore activation state: %w", err)
	}
	file := os.NewFile(uintptr(fd), location)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("open restore activation state: invalid file descriptor")
	}
	defer file.Close()
	openedInformation, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened restore activation state: %w", err)
	}
	stat, ok := openedInformation.Sys().(*syscall.Stat_t)
	if !openedInformation.Mode().IsRegular() || openedInformation.Mode().Perm()&0o077 != 0 || !ok || stat.Nlink != 1 {
		return errors.New("restore activation state must be a singly linked owner-only regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(len(completeState)+1)))
	if err != nil {
		return fmt.Errorf("read restore activation state: %w", err)
	}
	if bytes.Equal(content, []byte(completeState)) {
		if _, err := os.Lstat(databasePath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errors.New("completed restore activation state exists without its database")
			}
			return fmt.Errorf("inspect completed restore database: %w", err)
		}
		return nil
	}
	return errors.New("restore is incomplete; this database cannot be activated")
}

func CreateIncompleteRestoreState(databasePath string) error {
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return fmt.Errorf("resolve restore database path: %w", err)
	}
	location := ActivationPath(absolute)
	if err := requireRealDirectory(filepath.Dir(location)); err != nil {
		return fmt.Errorf("restore activation state parent: %w", err)
	}
	fd, err := unix.Open(location, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create restore activation state: %w", err)
	}
	file := os.NewFile(uintptr(fd), location)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("create restore activation state: invalid file descriptor")
	}
	if _, err := file.WriteString(incompleteState); err != nil {
		_ = file.Close()
		return fmt.Errorf("write restore activation state: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync restore activation state: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close restore activation state: %w", err)
	}
	return syncParentDirectory(location)
}

func MarkRestoreComplete(databasePath string) error {
	return markRestoreComplete(
		databasePath,
		func(file *os.File) error { return file.Sync() },
		os.Rename,
		syncParentDirectory,
	)
}

func markRestoreComplete(
	databasePath string,
	syncFile func(*os.File) error,
	rename func(string, string) error,
	syncParent func(string) error,
) error {
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return fmt.Errorf("resolve restore database path: %w", err)
	}
	location := ActivationPath(absolute)
	if err := requireRealDirectory(filepath.Dir(location)); err != nil {
		return fmt.Errorf("restore activation state parent: %w", err)
	}
	fd, err := unix.Open(location, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open restore activation state: %w", err)
	}
	file := os.NewFile(uintptr(fd), location)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("open restore activation state: invalid file descriptor")
	}
	information, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("inspect restore activation state: %w", err)
	}
	stat, ok := information.Sys().(*syscall.Stat_t)
	if !information.Mode().IsRegular() || information.Mode().Perm()&0o077 != 0 || !ok || stat.Nlink != 1 {
		_ = file.Close()
		return errors.New("restore activation state is unsafe")
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(len(incompleteState)+1)))
	if err != nil || !bytes.Equal(content, []byte(incompleteState)) {
		_ = file.Close()
		return errors.New("restore activation state is not incomplete")
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close incomplete restore activation state: %w", err)
	}
	completed, err := os.CreateTemp(filepath.Dir(location), ".sbm-restore-complete-")
	if err != nil {
		return fmt.Errorf("create completed restore activation state: %w", err)
	}
	completedPath := completed.Name()
	published := false
	defer func() {
		_ = completed.Close()
		if !published {
			_ = os.Remove(completedPath)
		}
	}()
	if err := completed.Chmod(0o600); err != nil {
		return fmt.Errorf("protect completed restore activation state: %w", err)
	}
	if _, err := completed.WriteString(completeState); err != nil {
		return fmt.Errorf("complete restore activation state: %w", err)
	}
	if err := syncFile(completed); err != nil {
		return fmt.Errorf("sync completed restore activation state: %w", err)
	}
	if err := completed.Close(); err != nil {
		return fmt.Errorf("close completed restore activation state: %w", err)
	}
	if err := rename(completedPath, location); err != nil {
		return fmt.Errorf("publish completed restore activation state: %w", err)
	}
	published = true
	return syncParent(location)
}

func syncParentDirectory(location string) error {
	directory, err := os.Open(filepath.Dir(location))
	if err != nil {
		return fmt.Errorf("open restore activation state parent: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync restore activation state parent: %w", err)
	}
	return nil
}

func rejectUnsafeDatabasePath(databasePath string) error {
	information, err := os.Lstat(databasePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect database path: %w", err)
	}
	if information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() {
		return errors.New("database path must be absent or a regular file without symlinks")
	}
	stat, ok := information.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return errors.New("database path must have exactly one hard link")
	}
	return nil
}

func requireRealDirectory(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(absolute)
	remainder := absolute[len(volume):]
	current := volume + string(filepath.Separator)
	for _, component := range splitPath(remainder) {
		current = filepath.Join(current, component)
		information, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !information.IsDir() || information.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %s is not a real directory", current)
		}
	}
	return nil
}

func splitPath(path string) []string {
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return nil
	}
	components := make([]string, 0)
	for cleaned != "." && cleaned != string(filepath.Separator) {
		directory, base := filepath.Split(cleaned)
		if base != "" {
			components = append(components, base)
		}
		cleaned = filepath.Clean(directory)
	}
	for left, right := 0, len(components)-1; left < right; left, right = left+1, right-1 {
		components[left], components[right] = components[right], components[left]
	}
	return components
}
