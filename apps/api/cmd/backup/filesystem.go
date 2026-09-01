package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func requireRegular(location string, ownerOnly bool) error {
	if err := requireRealParent(location); err != nil {
		return err
	}
	information, err := os.Lstat(location)
	if err != nil {
		return err
	}
	if !information.Mode().IsRegular() || information.Mode()&os.ModeSymlink != 0 {
		return errors.New("must be a regular file without symlinks")
	}
	stat, ok := information.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return errors.New("must have exactly one hard link")
	}
	if ownerOnly && information.Mode().Perm()&0o077 != 0 {
		return errors.New("must be accessible only by its owner")
	}
	return nil
}

func requireDirectory(location string) error {
	if err := requireRealParent(location); err != nil {
		return err
	}
	information, err := os.Lstat(location)
	if err != nil {
		return err
	}
	if !information.IsDir() || information.Mode()&os.ModeSymlink != 0 {
		return errors.New("must be a directory without symlinks")
	}
	return nil
}

func requireRealParent(location string) error {
	absolute, err := filepath.Abs(filepath.Dir(location))
	if err != nil {
		return err
	}
	current := string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(absolute, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
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

func readLimitedRegular(location string, maximum int64) ([]byte, error) {
	file, err := openRegular(location)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum {
		return nil, errors.New("file exceeds size limit")
	}
	return content, nil
}

func openRegular(location string) (*os.File, error) {
	if err := requireRealParent(location); err != nil {
		return nil, err
	}
	fd, err := unix.Open(location, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), location)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("invalid file descriptor")
	}
	information, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	stat, ok := information.Sys().(*syscall.Stat_t)
	if !information.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		_ = file.Close()
		return nil, errors.New("must be a singly linked regular file without symlinks")
	}
	return file, nil
}

func writeExclusiveFile(location string, content []byte, mode fs.FileMode) error {
	if err := requireRealParent(location); err != nil {
		return err
	}
	fd, err := unix.Open(location, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), location)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("invalid file descriptor")
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func copyRegularFile(source, destination string) error {
	input, err := openRegular(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := requireRealParent(destination); err != nil {
		return err
	}
	fd, err := unix.Open(destination, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	output := os.NewFile(uintptr(fd), destination)
	if output == nil {
		_ = unix.Close(fd)
		return errors.New("invalid file descriptor")
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func inspectFile(location, recordedPath string) (fileRecord, error) {
	hash, size, err := hashFile(location)
	return fileRecord{Path: recordedPath, Size: size, SHA256: hash}, err
}

func hashFile(location string) (string, int64, error) {
	file, err := openRegular(location)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), size, nil
}

func safeRelativePath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && !strings.HasPrefix(cleaned, "../")
}

func safeJoin(root, relative string) (string, error) {
	if !safeRelativePath(relative) {
		return "", errors.New("unsafe relative path")
	}
	return filepath.Join(root, filepath.FromSlash(relative)), nil
}

func listTree(root, prefix string) ([]fileRecord, error) {
	if err := requireDirectory(root); err != nil {
		return nil, err
	}
	records := make([]fileRecord, 0)
	err := filepath.WalkDir(root, func(location string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if location == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in file tree: %s", location)
		}
		information, err := entry.Info()
		if err != nil {
			return err
		}
		if information.IsDir() {
			return nil
		}
		if !information.Mode().IsRegular() {
			return fmt.Errorf("non-regular entry in file tree: %s", location)
		}
		relative, err := filepath.Rel(root, location)
		if err != nil {
			return err
		}
		recorded := filepath.ToSlash(filepath.Join(prefix, relative))
		record, err := inspectFile(location, recorded)
		if err != nil {
			return err
		}
		records = append(records, record)
		return nil
	})
	sort.Slice(records, func(left, right int) bool { return records[left].Path < records[right].Path })
	return records, err
}

func copyTree(source, destination, prefix string) ([]fileRecord, error) {
	if err := requireDirectory(source); err != nil {
		return nil, err
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return nil, err
	}
	records := make([]fileRecord, 0)
	err := filepath.WalkDir(source, func(location string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if location == source {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in file tree: %s", location)
		}
		relative, err := filepath.Rel(source, location)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		information, err := entry.Info()
		if err != nil {
			return err
		}
		if information.IsDir() {
			return os.Mkdir(target, 0o700)
		}
		if !information.Mode().IsRegular() {
			return fmt.Errorf("non-regular entry in file tree: %s", location)
		}
		if err := copyRegularFile(location, target); err != nil {
			return err
		}
		record, err := inspectFile(target, filepath.ToSlash(filepath.Join(prefix, relative)))
		if err != nil {
			return err
		}
		records = append(records, record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(left, right int) bool { return records[left].Path < records[right].Path })
	if err := syncTreeDirectories(destination); err != nil {
		return nil, err
	}
	return records, nil
}

func validateObjectStore(root string) (string, error) {
	if err := requireDirectory(root); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	expected := map[string]bool{"objects": false, "staging": false, "trash": false}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return "", fmt.Errorf("object store contains unexpected top-level entry %q", entry.Name())
		}
		expected[entry.Name()] = true
	}
	for name, found := range expected {
		if !found {
			return "", fmt.Errorf("object store is missing %s", name)
		}
		if name != "objects" {
			children, err := os.ReadDir(filepath.Join(root, name))
			if err != nil {
				return "", err
			}
			if len(children) != 0 {
				return "", fmt.Errorf("object store %s directory is not empty", name)
			}
		}
	}
	return filepath.Join(root, "objects"), nil
}

func createObjectStoreFromPackage(packageObjects, destination string) ([]fileRecord, error) {
	records, err := copyTree(packageObjects, filepath.Join(destination, "objects"), "objects")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"staging", "trash"} {
		if err := os.Mkdir(filepath.Join(destination, name), 0o700); err != nil {
			return nil, err
		}
	}
	if err := syncTreeDirectories(destination); err != nil {
		return nil, err
	}
	return records, nil
}

func verifyRecordedFile(root string, record fileRecord) error {
	location, err := safeJoin(root, record.Path)
	if err != nil {
		return err
	}
	actual, err := inspectFile(location, record.Path)
	if err != nil {
		return err
	}
	if actual != record {
		return errors.New("file hash or size differs from manifest")
	}
	return nil
}

func verifyPackageLayout(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	expected := map[string]bool{"database": false, "objects": false, manifestName: false, manifestAuthName: false}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup contains unexpected top-level entry %q", entry.Name())
		}
		expected[entry.Name()] = true
	}
	for name, found := range expected {
		if !found {
			return fmt.Errorf("backup is missing %s", name)
		}
	}
	databaseEntries, err := os.ReadDir(filepath.Join(root, "database"))
	if err != nil {
		return err
	}
	if len(databaseEntries) != 1 || databaseEntries[0].Name() != "sbm.pgcustom" || !databaseEntries[0].Type().IsRegular() {
		return errors.New("backup database directory is invalid")
	}
	return nil
}

func requireAbsent(location, label string) error {
	if _, err := os.Lstat(location); errors.Is(err, os.ErrNotExist) {
		return requireRealParent(location)
	} else if err != nil {
		return fmt.Errorf("inspect %s target: %w", label, err)
	}
	return fmt.Errorf("%s target already exists", label)
}

func pathsOverlap(left, right string) (bool, error) {
	leftAbsolute, err := filepath.Abs(left)
	if err != nil {
		return false, err
	}
	rightAbsolute, err := filepath.Abs(right)
	if err != nil {
		return false, err
	}
	isWithin := func(parent, child string) bool {
		relative, err := filepath.Rel(parent, child)
		return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	}
	return isWithin(leftAbsolute, rightAbsolute) || isWithin(rightAbsolute, leftAbsolute), nil
}

func publishNoReplace(source, destination string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(destination))
}

func syncTreeDirectories(root string) error {
	directories := make([]string, 0)
	if err := filepath.WalkDir(root, func(location string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, location)
		}
		return nil
	}); err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := syncDirectory(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func syncDirectory(location string) error {
	directory, err := os.Open(location)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
