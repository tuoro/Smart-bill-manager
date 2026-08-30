package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/sqlite"
)

func TestBackupVerifyAndRestoreRoundTrip(t *testing.T) {
	t.Parallel()
	fixture := newBackupFixture(t)
	backup := filepath.Join(fixture.root, "backup")
	manifest, err := createBackup(context.Background(), backupOptions{
		Database: fixture.database, Objects: fixture.objects, MasterKey: fixture.masterKey,
		Output: backup, Offline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Database.QuickCheck != "ok" || manifest.Database.TableCounts["tenants"] != 0 || len(manifest.Objects) != 1 {
		t.Fatalf("manifest = %#v", manifest)
	}
	verified, err := verifyBackup(context.Background(), backup)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Database.File.SHA256 != manifest.Database.File.SHA256 {
		t.Fatal("verified database hash differs")
	}
	restoreRoot := filepath.Join(fixture.root, "restore")
	if err := os.Mkdir(restoreRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	options := restoreOptions{
		Backup: backup, Database: filepath.Join(restoreRoot, "database", "sbm.sqlite"),
		Objects: filepath.Join(restoreRoot, "objects"), MasterKey: filepath.Join(restoreRoot, "secrets", "master-key"),
	}
	if err := os.Mkdir(options.Objects, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreBackup(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(options.Objects, "objects", "unreferenced"))
	if err != nil || string(content) != "synthetic object" {
		t.Fatalf("restored object = %q, %v", content, err)
	}
	key, err := os.ReadFile(options.MasterKey)
	if err != nil || !bytes.Equal(key, bytes.Repeat([]byte{0x42}, 32)) {
		t.Fatal("restored master key differs")
	}
}

func TestVerifyRejectsTamperedObject(t *testing.T) {
	t.Parallel()
	fixture := newBackupFixture(t)
	backup := filepath.Join(fixture.root, "backup")
	if _, err := createBackup(context.Background(), backupOptions{
		Database: fixture.database, Objects: fixture.objects, MasterKey: fixture.masterKey,
		Output: backup, Offline: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "objects", "objects", "unreferenced"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyBackup(context.Background(), backup); err == nil {
		t.Fatal("tampered backup verified successfully")
	}
}

func TestBackupRejectsObjectSymlink(t *testing.T) {
	t.Parallel()
	fixture := newBackupFixture(t)
	if err := os.Symlink(filepath.Join(fixture.objects, "objects", "unreferenced"), filepath.Join(fixture.objects, "objects", "link")); err != nil {
		t.Fatal(err)
	}
	_, err := createBackup(context.Background(), backupOptions{
		Database: fixture.database, Objects: fixture.objects, MasterKey: fixture.masterKey,
		Output: filepath.Join(fixture.root, "backup"), Offline: true,
	})
	if err == nil {
		t.Fatal("backup accepted an object-store symlink")
	}
}

func TestRestoreRefusesExistingTarget(t *testing.T) {
	t.Parallel()
	fixture := newBackupFixture(t)
	backup := filepath.Join(fixture.root, "backup")
	if _, err := createBackup(context.Background(), backupOptions{
		Database: fixture.database, Objects: fixture.objects, MasterKey: fixture.masterKey,
		Output: backup, Offline: true,
	}); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(fixture.root, "existing.sqlite")
	if err := os.WriteFile(existing, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := restoreBackup(context.Background(), restoreOptions{
		Backup: backup, Database: existing,
		Objects: filepath.Join(fixture.root, "restored-objects"), MasterKey: filepath.Join(fixture.root, "restored-key"),
	})
	if err == nil {
		t.Fatal("restore overwrote an existing database target")
	}
	content, readErr := os.ReadFile(existing)
	if readErr != nil || string(content) != "preserve" {
		t.Fatal("existing restore target changed")
	}
}

type backupFixture struct {
	root, database, objects, masterKey string
}

func newBackupFixture(t *testing.T) backupFixture {
	t.Helper()
	root := t.TempDir()
	database := filepath.Join(root, "source", "sbm.sqlite")
	store, err := sqliteadapter.Open(context.Background(), sqliteadapter.Config{
		DatabasePath: database, MigrationsDir: backupMigrationsDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	objects := filepath.Join(root, "objects")
	for _, name := range []string{"objects", "staging", "trash"} {
		if err := os.MkdirAll(filepath.Join(objects, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(objects, "objects", "unreferenced"), []byte("synthetic object"), 0o600); err != nil {
		t.Fatal(err)
	}
	masterKey := filepath.Join(root, "master-key")
	if err := os.WriteFile(masterKey, bytes.Repeat([]byte{0x42}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	return backupFixture{root: root, database: database, objects: objects, masterKey: masterKey}
}

func backupMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../infra/migrations"))
}
