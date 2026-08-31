package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/runtimeguard"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/sqlite"
)

func TestBackupVerifyAndRestoreRoundTrip(t *testing.T) {
	fixture := newBackupFixture(t)
	manifest, err := createBackup(context.Background(), fixture.backupOptions())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ManifestKind != manifestKind || manifest.ManifestVersion != manifestVersion ||
		manifest.DocumentCount != 2 || manifest.ObjectReferenceCount != 5 || manifest.UniqueObjectCount != 4 {
		t.Fatalf("manifest aggregate = %#v", manifest)
	}
	if _, err := os.Lstat(filepath.Join(fixture.backup, "secrets")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("data backup contains a secret directory")
	}
	verified, err := verifyBackup(context.Background(), fixture.verifyOptions())
	if err != nil {
		t.Fatal(err)
	}
	if verified.Database.File != manifest.Database.File {
		t.Fatal("verified database file record differs")
	}

	restore := fixture.restoreOptions()
	restored, invalidated, err := restoreBackup(context.Background(), restore)
	if err != nil {
		t.Fatal(err)
	}
	if restored.MigrationSetSHA256 != manifest.MigrationSetSHA256 || invalidated != 1 {
		t.Fatalf("restored identity/session count = %q / %d", restored.MigrationSetSHA256, invalidated)
	}
	key, err := os.ReadFile(restore.MasterKey)
	if err != nil || !bytes.Equal(key, bytes.Repeat([]byte{0x42}, 32)) {
		t.Fatal("restored master key differs")
	}
	shared, err := os.ReadFile(filepath.Join(restore.Objects, "objects", filepath.FromSlash(fixture.sharedKey)))
	if err != nil || string(shared) != string(fixture.sharedContent) {
		t.Fatalf("restored shared object = %q, %v", shared, err)
	}
	database, err := sql.Open("sqlite", "file:"+restore.Database+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var sessions, documents int64
	if err := database.QueryRow(`SELECT (SELECT count(*) FROM sessions), (SELECT count(*) FROM documents)`).Scan(&sessions, &documents); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || documents != 2 {
		t.Fatalf("restored counts = sessions:%d documents:%d", sessions, documents)
	}
	if _, err := os.Lstat(runtimeguard.ActivationPath(restore.Database)); err != nil {
		t.Fatalf("successful restore did not retain durable activation state: %v", err)
	}
	guard, err := runtimeguard.AcquireExclusive(restore.Database)
	if err != nil {
		t.Fatalf("completed restore was not activatable: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBackupRejectsIncompleteOrAmbiguousObjectStore(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, backupFixture)
	}{
		{
			name: "missing referenced object",
			mutate: func(t *testing.T, fixture backupFixture) {
				t.Helper()
				if err := os.Remove(filepath.Join(fixture.objects, "objects", filepath.FromSlash(fixture.sharedKey))); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unreferenced committed object",
			mutate: func(t *testing.T, fixture backupFixture) {
				t.Helper()
				writeFixtureFile(t, filepath.Join(fixture.objects, "objects", "unreferenced"), []byte("not referenced"))
			},
		},
		{
			name: "nonempty staging",
			mutate: func(t *testing.T, fixture backupFixture) {
				t.Helper()
				writeFixtureFile(t, filepath.Join(fixture.objects, "staging", "pending"), []byte("pending"))
			},
		},
		{
			name: "nonempty trash",
			mutate: func(t *testing.T, fixture backupFixture) {
				t.Helper()
				writeFixtureFile(t, filepath.Join(fixture.objects, "trash", "pending"), []byte("pending"))
			},
		},
		{
			name: "object symlink",
			mutate: func(t *testing.T, fixture backupFixture) {
				t.Helper()
				if err := os.Symlink(filepath.Join(fixture.objects, "objects", filepath.FromSlash(fixture.sharedKey)), filepath.Join(fixture.objects, "objects", "link")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "object with external hard link",
			mutate: func(t *testing.T, fixture backupFixture) {
				t.Helper()
				source := filepath.Join(fixture.objects, "objects", filepath.FromSlash(fixture.sharedKey))
				if err := os.Link(source, filepath.Join(fixture.root, "external-object-alias")); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBackupFixture(t)
			test.mutate(t, fixture)
			if _, err := createBackup(context.Background(), fixture.backupOptions()); err == nil {
				t.Fatal("invalid object store was accepted")
			}
			if _, err := os.Lstat(fixture.backup); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed backup published an output directory")
			}
		})
	}
}

func TestVerifyRejectsWrongKeyTamperingAndTrailingJSON(t *testing.T) {
	t.Run("wrong independent key", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		wrong := filepath.Join(fixture.root, "wrong-key")
		writeFixtureFile(t, wrong, bytes.Repeat([]byte{0x24}, 32))
		if _, err := verifyBackup(context.Background(), verifyOptions{Backup: fixture.backup, MasterKey: wrong, Migrations: fixture.migrations}); err == nil {
			t.Fatal("wrong key authenticated the backup")
		}
	})

	t.Run("tampered object", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		writeExistingFixtureFile(t, filepath.Join(fixture.backup, "objects", filepath.FromSlash(fixture.sharedKey)), []byte("tampered"))
		if _, err := verifyBackup(context.Background(), fixture.verifyOptions()); err == nil {
			t.Fatal("tampered object verified")
		}
	})

	t.Run("authenticated trailing JSON", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		manifestPath := filepath.Join(fixture.backup, manifestName)
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		content = append(content, []byte("{}\n")...)
		writeExistingFixtureFile(t, manifestPath, content)
		key, err := loadMasterKey(fixture.masterKey)
		if err != nil {
			t.Fatal(err)
		}
		tag := authenticateManifest(key, content)
		clear(key)
		writeExistingFixtureFile(t, filepath.Join(fixture.backup, manifestAuthName), append([]byte(hex.EncodeToString(tag)), '\n'))
		clear(tag)
		if _, err := verifyBackup(context.Background(), fixture.verifyOptions()); err == nil || !strings.Contains(err.Error(), "trailing") {
			t.Fatalf("trailing JSON error = %v", err)
		}
	})

	t.Run("authenticated duplicate field", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		manifestPath := filepath.Join(fixture.backup, manifestName)
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		content = bytes.Replace(content, []byte(`"manifest_version": 2,`), []byte(`"manifest_version": 2, "manifest_version": 2,`), 1)
		writeExistingFixtureFile(t, manifestPath, content)
		key, err := loadMasterKey(fixture.masterKey)
		if err != nil {
			t.Fatal(err)
		}
		tag := authenticateManifest(key, content)
		clear(key)
		writeExistingFixtureFile(t, filepath.Join(fixture.backup, manifestAuthName), append([]byte(hex.EncodeToString(tag)), '\n'))
		clear(tag)
		if _, err := verifyBackup(context.Background(), fixture.verifyOptions()); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate JSON field error = %v", err)
		}
	})
}

func TestVerifyRejectsOldManifestAndMigrationDrift(t *testing.T) {
	t.Run("old manifest", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		key, err := loadMasterKey(fixture.masterKey)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := readAuthenticatedManifest(fixture.backup, key)
		if err != nil {
			t.Fatal(err)
		}
		manifest.ManifestVersion = 1
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		encoded = append(encoded, '\n')
		writeExistingFixtureFile(t, filepath.Join(fixture.backup, manifestName), encoded)
		tag := authenticateManifest(key, encoded)
		clear(key)
		writeExistingFixtureFile(t, filepath.Join(fixture.backup, manifestAuthName), append([]byte(hex.EncodeToString(tag)), '\n'))
		clear(tag)
		if _, err := verifyBackup(context.Background(), fixture.verifyOptions()); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("old manifest error = %v", err)
		}
	})

	t.Run("migration content drift", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		drifted := filepath.Join(fixture.root, "drifted-migrations")
		if err := os.Mkdir(drifted, 0o700); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(fixture.migrations)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			content, err := os.ReadFile(filepath.Join(fixture.migrations, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if entry.Name() == "0002_fact_insight_indexes.sql" {
				content = append(content, []byte("\n-- drift\n")...)
			}
			writeFixtureFile(t, filepath.Join(drifted, entry.Name()), content)
		}
		if _, err := verifyBackup(context.Background(), verifyOptions{Backup: fixture.backup, MasterKey: fixture.masterKey, Migrations: drifted}); err == nil || !strings.Contains(err.Error(), "migration") {
			t.Fatalf("migration drift error = %v", err)
		}
	})
}

func TestBackupRuntimeLockAndRestoreTargetsAreFailClosed(t *testing.T) {
	t.Run("source runtime lock", func(t *testing.T) {
		fixture := newBackupFixture(t)
		guard, err := runtimeguard.AcquireExclusive(fixture.database)
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Close()
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err == nil || !strings.Contains(err.Error(), "already held") {
			t.Fatalf("runtime contention error = %v", err)
		}
	})

	t.Run("backup output inside source data", func(t *testing.T) {
		for _, location := range []struct {
			name   string
			output func(backupFixture) string
		}{
			{name: "database", output: func(fixture backupFixture) string {
				return filepath.Join(filepath.Dir(fixture.database), "nested-backup")
			}},
			{name: "objects", output: func(fixture backupFixture) string {
				return filepath.Join(fixture.objects, "objects", "nested-backup")
			}},
			{name: "staging", output: func(fixture backupFixture) string {
				return filepath.Join(fixture.objects, "staging", "nested-backup")
			}},
			{name: "trash", output: func(fixture backupFixture) string {
				return filepath.Join(fixture.objects, "trash", "nested-backup")
			}},
			{name: "migrations", output: func(fixture backupFixture) string {
				return filepath.Join(fixture.migrations, "nested-backup")
			}},
		} {
			location := location
			t.Run(location.name, func(t *testing.T) {
				fixture := newBackupFixture(t)
				options := fixture.backupOptions()
				options.Output = location.output(fixture)
				if _, err := createBackup(context.Background(), options); err == nil || !strings.Contains(err.Error(), "disjoint") {
					t.Fatalf("overlapping backup output error = %v", err)
				}
				for _, name := range []string{"staging", "trash"} {
					entries, err := os.ReadDir(filepath.Join(fixture.objects, name))
					if err != nil || len(entries) != 0 {
						t.Fatalf("object store %s changed after rejected output: entries=%d err=%v", name, len(entries), err)
					}
				}
			})
		}
	})

	t.Run("hardlinked master key", func(t *testing.T) {
		fixture := newBackupFixture(t)
		alias := filepath.Join(fixture.root, "master-key-alias")
		if err := os.Link(fixture.masterKey, alias); err != nil {
			t.Fatal(err)
		}
		options := fixture.backupOptions()
		options.MasterKey = alias
		if _, err := createBackup(context.Background(), options); err == nil || !strings.Contains(err.Error(), "hard link") {
			t.Fatalf("hardlinked master key error = %v", err)
		}
	})

	t.Run("hardlinked database", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if err := os.Link(fixture.database, filepath.Join(fixture.root, "database-alias")); err != nil {
			t.Fatal(err)
		}
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err == nil || !strings.Contains(err.Error(), "hard link") {
			t.Fatalf("hardlinked database error = %v", err)
		}
	})

	t.Run("existing target", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		restore := fixture.restoreOptions()
		writeFixtureFile(t, restore.Database, []byte("preserve"))
		if _, _, err := restoreBackup(context.Background(), restore); err == nil {
			t.Fatal("restore overwrote an existing target")
		}
		content, err := os.ReadFile(restore.Database)
		if err != nil || string(content) != "preserve" {
			t.Fatal("existing target changed")
		}
	})

	t.Run("restore target inside migrations", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		restore := fixture.restoreOptions()
		restore.Objects = filepath.Join(fixture.migrations, "restored-objects")
		if _, _, err := restoreBackup(context.Background(), restore); err == nil || !strings.Contains(err.Error(), "disjoint") {
			t.Fatalf("migration overlap error = %v", err)
		}
		if _, err := os.Lstat(restore.Database); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("database target changed before overlap rejection: %v", err)
		}
	})

	t.Run("restore target uses runtime lock path", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		restore := fixture.restoreOptions()
		restore.Objects = runtimeguard.LockPath(restore.Database)
		if _, _, err := restoreBackup(context.Background(), restore); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("reserved target error = %v", err)
		}
	})

	t.Run("source key inside restored database directory", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		restore := fixture.restoreOptions()
		restore.MasterKeySource = filepath.Join(filepath.Dir(restore.Database), "source-key")
		writeFixtureFile(t, restore.MasterKeySource, bytes.Repeat([]byte{0x42}, 32))
		if _, _, err := restoreBackup(context.Background(), restore); err == nil || !strings.Contains(err.Error(), "disjoint") {
			t.Fatalf("non-independent source key error = %v", err)
		}
	})

	t.Run("source key inside restored key directory", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		restore := fixture.restoreOptions()
		restore.MasterKeySource = filepath.Join(filepath.Dir(restore.MasterKey), "source-key")
		writeFixtureFile(t, restore.MasterKeySource, bytes.Repeat([]byte{0x42}, 32))
		if _, _, err := restoreBackup(context.Background(), restore); err == nil || !strings.Contains(err.Error(), "disjoint") {
			t.Fatalf("co-located source and restored key error = %v", err)
		}
	})

	t.Run("partial publication marker", func(t *testing.T) {
		fixture := newBackupFixture(t)
		if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
			t.Fatal(err)
		}
		restore := fixture.restoreOptions()
		calls := 0
		restore.publish = func(source, destination string) error {
			calls++
			if calls == 2 {
				return errors.New("injected publish failure")
			}
			return publishNoReplace(source, destination)
		}
		if _, _, err := restoreBackup(context.Background(), restore); err == nil || !strings.Contains(err.Error(), "injected") {
			t.Fatalf("partial publication error = %v", err)
		}
		if _, err := os.Lstat(runtimeguard.ActivationPath(restore.Database)); err != nil {
			t.Fatalf("activation marker missing: %v", err)
		}
		if _, err := runtimeguard.AcquireExclusive(restore.Database); err == nil || !strings.Contains(err.Error(), "incomplete") {
			t.Fatalf("partial restore was activatable: %v", err)
		}
	})
}

func TestRunPrintsOnlySafeAggregate(t *testing.T) {
	fixture := newBackupFixture(t)
	if _, err := createBackup(context.Background(), fixture.backupOptions()); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"verify", "-backup", fixture.backup, "-master-key", fixture.masterKey, "-migrations", fixture.migrations}, &output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, forbidden := range []string{fixture.backup, fixture.masterKey, fixture.sharedKey, hashBytes(fixture.sharedContent), hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("safe CLI output contains forbidden value %q: %s", forbidden, text)
		}
	}
	var result operationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.Passed || result.UniqueObjectCount != 4 ||
		!lowerHex32Pattern.MatchString(result.BackupSetID) || result.OperationStartedAtMS < 1 ||
		result.OperationFinishedAtMS < result.OperationStartedAtMS {
		t.Fatalf("safe result = %#v, %v", result, err)
	}
}

func TestSafeErrorCodeNeverEchoesSensitiveDetail(t *testing.T) {
	detail := "object tenants/private/document differs at /secure/private and hash " + strings.Repeat("a", 64)
	code := safeErrorCode(errors.New(detail))
	if code != "object_inventory_invalid" || strings.Contains(code, "private") || strings.Contains(code, "secure") || strings.Contains(code, strings.Repeat("a", 64)) {
		t.Fatalf("safe error code = %q", code)
	}
}

type backupFixture struct {
	root, database, objects, masterKey, migrations, backup string
	sharedKey                                              string
	sharedContent                                          []byte
}

func (f backupFixture) backupOptions() backupOptions {
	return backupOptions{
		Database: f.database, Objects: f.objects, MasterKey: f.masterKey,
		Migrations: f.migrations, Output: f.backup, Offline: true,
	}
}

func (f backupFixture) verifyOptions() verifyOptions {
	return verifyOptions{Backup: f.backup, MasterKey: f.masterKey, Migrations: f.migrations}
}

func (f backupFixture) restoreOptions() restoreOptions {
	restoreRoot := filepath.Join(f.root, "restore")
	for _, parent := range []string{
		filepath.Join(restoreRoot, "database"),
		filepath.Join(restoreRoot, "objects"),
		filepath.Join(restoreRoot, "secrets"),
	} {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			panic(err)
		}
	}
	return restoreOptions{
		Backup: f.backup, MasterKeySource: f.masterKey, Migrations: f.migrations,
		Database:  filepath.Join(restoreRoot, "database", "sbm.sqlite"),
		Objects:   filepath.Join(restoreRoot, "objects", "object-store"),
		MasterKey: filepath.Join(restoreRoot, "secrets", "master-key"), Offline: true,
	}
}

func newBackupFixture(t *testing.T) backupFixture {
	t.Helper()
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	if err := os.Mkdir(sourceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(sourceRoot, "sbm.sqlite")
	migrations := backupMigrationsDir(t)
	store, err := sqliteadapter.Open(context.Background(), sqliteadapter.Config{DatabasePath: databasePath, MigrationsDir: migrations})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	objects := filepath.Join(root, "object-store")
	for _, name := range []string{"objects", "staging", "trash"} {
		if err := os.MkdirAll(filepath.Join(objects, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	uploadKey := "tenants/tenant/documents/upload"
	sharedKey := "tenants/tenant/email/attachments/shared"
	rawKey := "tenants/tenant/email/messages/raw"
	pageKey := "tenants/tenant/pages/page-1"
	uploadContent := []byte("synthetic upload object")
	sharedContent := []byte("synthetic shared email attachment")
	rawContent := []byte("synthetic RFC822 message")
	pageContent := []byte("synthetic normalized page")
	for key, content := range map[string][]byte{
		uploadKey: uploadContent, sharedKey: sharedContent, rawKey: rawContent, pageKey: pageContent,
	} {
		writeFixtureFile(t, filepath.Join(objects, "objects", filepath.FromSlash(key)), content)
	}
	seedBackupDatabase(t, databasePath, map[string]objectSeed{
		"upload": {key: uploadKey, content: uploadContent},
		"shared": {key: sharedKey, content: sharedContent},
		"raw":    {key: rawKey, content: rawContent},
		"page":   {key: pageKey, content: pageContent},
	})
	escrow := filepath.Join(root, "escrow")
	if err := os.Mkdir(escrow, 0o700); err != nil {
		t.Fatal(err)
	}
	masterKey := filepath.Join(escrow, "master-key")
	writeFixtureFile(t, masterKey, bytes.Repeat([]byte{0x42}, 32))
	backupParent := filepath.Join(root, "data-backups")
	if err := os.Mkdir(backupParent, 0o700); err != nil {
		t.Fatal(err)
	}
	return backupFixture{
		root: root, database: databasePath, objects: objects, masterKey: masterKey,
		migrations: migrations, backup: filepath.Join(backupParent, "backup"),
		sharedKey: sharedKey, sharedContent: sharedContent,
	}
}

type objectSeed struct {
	key     string
	content []byte
}

func seedBackupDatabase(t *testing.T, databasePath string, objects map[string]objectSeed) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at) VALUES ('user', 'owner@example.invalid', 'fixture-hash', 'Owner', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at) VALUES ('tenant', 'Synthetic', 'CNY', 'Asia/Shanghai', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at) VALUES ('tenant', 'user', 'owner', 'active', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO sessions (id, tenant_id, user_id, token_hash, csrf_token_hash, expires_at, created_at, last_seen_at) VALUES ('session', 'tenant', 'user', 'fixture-session-hash', 'fixture-csrf-hash', '2026-09-01T00:00:00Z', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at) VALUES ('audit', 'tenant', 'user', 'email_archived', 'email_message', 'message', 'fixture-request', '{}', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO email_sources (id, tenant_id, display_name, mailbox_address_normalized, imap_host_normalized, imap_port, transport_security, status, idempotency_key, request_hash, created_by_user_id, created_at, last_archived_at, version) VALUES ('source', 'tenant', 'Synthetic mailbox', 'archive@example.invalid', 'imap.example.invalid', 993, 'implicit_tls', 'active', 'fixture-source-key', ?, 'user', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z', 1)`, []any{strings.Repeat("b", 64)}},
		{`INSERT INTO documents (id, tenant_id, storage_key, original_name, declared_mime, detected_mime, size_bytes, sha256, page_count, status, ingestion_kind, original_object_owner, created_by_user_id, created_at) VALUES ('document-upload', 'tenant', ?, 'upload.png', 'image/png', 'image/png', ?, ?, 1, 'failed', 'upload', 'document', 'user', '2026-08-31T00:00:00Z')`, []any{objects["upload"].key, len(objects["upload"].content), hashBytes(objects["upload"].content)}},
		{`INSERT INTO documents (id, tenant_id, storage_key, original_name, declared_mime, detected_mime, size_bytes, sha256, page_count, status, ingestion_kind, original_object_owner, created_by_user_id, created_at) VALUES ('document-email', 'tenant', ?, 'shared.png', 'image/png', 'image/png', ?, ?, 1, 'needs_review', 'email_attachment', 'email_attachment', 'user', '2026-08-31T00:00:00Z')`, []any{objects["shared"].key, len(objects["shared"].content), hashBytes(objects["shared"].content)}},
		{`INSERT INTO email_messages (id, tenant_id, email_source_id, external_message_key, raw_storage_key, raw_sha256, raw_size_bytes, subject, sender_address, received_at, status, audit_event_id, created_at) VALUES ('message', 'tenant', 'source', ?, ?, ?, ?, 'Synthetic', 'sender@example.invalid', '2026-08-31T00:00:00Z', 'archived', 'audit', '2026-08-31T00:00:00Z')`, []any{strings.Repeat("a", 64), objects["raw"].key, hashBytes(objects["raw"].content), len(objects["raw"].content)}},
		{`INSERT INTO email_attachments (id, tenant_id, email_message_id, part_index, storage_key, original_name, declared_mime, disposition, size_bytes, sha256, processing_status, document_id, created_at) VALUES ('attachment', 'tenant', 'message', 1, ?, 'shared.png', 'image/png', 'attachment', ?, ?, 'existing_document', 'document-email', '2026-08-31T00:00:00Z')`, []any{objects["shared"].key, len(objects["shared"].content), hashBytes(objects["shared"].content)}},
		{`INSERT INTO document_pages (id, tenant_id, document_id, page_number, derived_image_storage_key, width, height, sha256, processing_version, visual_fingerprint_version, dhash64, ahash64, dhash_band_0, dhash_band_1, dhash_band_2, dhash_band_3, created_at) VALUES ('page', 'tenant', 'document-email', 1, ?, 1, 1, ?, 'document-normalize/2', 'page-visual-dedup/1', '0000000000000000', '0000000000000000', 0, 0, 0, 0, '2026-08-31T00:00:00Z')`, []any{objects["page"].key, hashBytes(objects["page"].content)}},
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed backup database: %v\n%s", err, statement.query)
		}
	}
}

func writeFixtureFile(t *testing.T, location string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(location), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(location, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeExistingFixtureFile(t *testing.T, location string, content []byte) {
	t.Helper()
	if err := os.WriteFile(location, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func hashBytes(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func backupMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../infra/migrations"))
}
