package runtimeguard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireExclusiveRejectsContentionAndIncompleteRestore(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "sbm.sqlite")
	guard, err := AcquireExclusive(database)
	if err != nil {
		t.Fatalf("AcquireExclusive() error = %v", err)
	}
	if _, err := AcquireExclusive(database); err == nil || !strings.Contains(err.Error(), "already held") {
		t.Fatalf("contended lock error = %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	marker := ActivationPath(database)
	if err := os.WriteFile(marker, []byte("incomplete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireExclusive(database); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete restore error = %v", err)
	}
}

func TestCompletedRestoreStateIsDurableAndActivatable(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "restored.sqlite")
	if err := os.WriteFile(database, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CreateIncompleteRestoreState(database); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireExclusive(database); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete state error = %v", err)
	}
	if err := MarkRestoreComplete(database); err != nil {
		t.Fatal(err)
	}
	guard, err := AcquireExclusive(database)
	if err != nil {
		t.Fatalf("complete state error = %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := MarkRestoreComplete(database); err == nil || !strings.Contains(err.Error(), "not incomplete") {
		t.Fatalf("second completion error = %v", err)
	}
}

func TestCompletedRestoreStatePublicationIsFailClosed(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "restored.sqlite")
	if err := os.WriteFile(database, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CreateIncompleteRestoreState(database); err != nil {
		t.Fatal(err)
	}
	if err := markRestoreComplete(
		database,
		func(*os.File) error { return errors.New("injected file sync failure") },
		os.Rename,
		syncParentDirectory,
	); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("completion sync error = %v", err)
	}
	if _, err := AcquireExclusive(database); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("unsynced completion was activatable: %v", err)
	}
}

func TestCompletedRestoreStateWithoutDatabaseIsRejected(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "missing.sqlite")
	if err := CreateIncompleteRestoreState(database); err != nil {
		t.Fatal(err)
	}
	if err := MarkRestoreComplete(database); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireExclusive(database); err == nil || !strings.Contains(err.Error(), "without its database") {
		t.Fatalf("orphan completion state error = %v", err)
	}
}

func TestAcquireExclusiveRejectsUnsafeLockAndSymlinkParent(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "unsafe.sqlite")
	if err := os.WriteFile(LockPath(database), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireExclusive(database); err == nil {
		t.Fatal("owner-readable runtime lock was accepted")
	}

	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireExclusive(filepath.Join(linkedParent, "sbm.sqlite")); err == nil {
		t.Fatal("symlinked runtime lock parent was accepted")
	}

	realDatabase := filepath.Join(realParent, "real.sqlite")
	if err := os.WriteFile(realDatabase, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedDatabase := filepath.Join(root, "linked.sqlite")
	if err := os.Symlink(realDatabase, linkedDatabase); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireExclusive(linkedDatabase); err == nil || !strings.Contains(err.Error(), "without symlinks") {
		t.Fatalf("symlinked database error = %v", err)
	}
}

func TestAcquireExclusiveRejectsHardlinkedDatabase(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "sbm.sqlite")
	alias := filepath.Join(root, "database-alias.sqlite")
	if err := os.WriteFile(database, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(database, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireExclusive(alias); err == nil || !strings.Contains(err.Error(), "hard link") {
		t.Fatalf("hardlinked database error = %v", err)
	}
}
