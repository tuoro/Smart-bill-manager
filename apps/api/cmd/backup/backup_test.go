package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestBackupRequiresFinishedMaterialPublicationJournal(t *testing.T) {
	root := t.TempDir()
	if _, err := localstorage.New(root); err != nil {
		t.Fatal(err)
	}
	if _, err := validateObjectStore(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "material-publications", "synthetic-pending.json"), []byte("synthetic pending intent"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateObjectStore(root); err == nil || !strings.Contains(err.Error(), "material-publications directory is not empty") {
		t.Fatalf("backup accepted pending publication: %v", err)
	}
}

func TestBackupExcludesAnonymousExportSpoolAndRejectsNamedResidue(t *testing.T) {
	root := t.TempDir()
	objects, err := localstorage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.InitializeExportSpool(); err != nil {
		t.Fatal(err)
	}
	file, err := objects.CreateExportFile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write([]byte("synthetic temporary ZIP")); err != nil {
		t.Fatal(err)
	}
	actual, err := validateObjectStore(root)
	if err != nil || actual != filepath.Join(root, "objects") {
		t.Fatal("backup selected non-authoritative exports")
	}
	if err := os.WriteFile(filepath.Join(root, "export-spool", "unknown-package"), []byte("synthetic residual"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateObjectStore(root); err == nil {
		t.Fatal("backup accepted unexpected named temporary package")
	}
}

func TestPostgreSQLBackupManifestAuthenticationAndStrictShape(t *testing.T) {
	root := t.TempDir()
	key := bytes.Repeat([]byte{0x31}, 32)
	manifest := validPostgreSQLManifest()
	if err := writeAuthenticatedManifest(root, manifest, key); err != nil {
		t.Fatal(err)
	}
	decoded, err := readAuthenticatedManifest(root, key)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ManifestVersion != 3 || decoded.Database.DumpFormat != "postgresql-custom" || decoded.Database.ServerMajor != 17 {
		t.Fatalf("decoded manifest = %#v", decoded)
	}
	manifestPath := filepath.Join(root, manifestName)
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	content[0] ^= 1
	if err := os.WriteFile(manifestPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAuthenticatedManifest(root, key); err == nil {
		t.Fatal("tampered manifest was accepted")
	}
}

func TestPostgreSQLToolFilesKeepPasswordOutOfServiceConfiguration(t *testing.T) {
	root := t.TempDir()
	passwordPath := filepath.Join(root, "password")
	secret := "synthetic-local-secret"
	if err := os.WriteFile(passwordPath, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := preparePostgresToolFiles(postgresqladapter.Config{
		Host: "postgres", Port: 5432, Database: "smart_bill_manager", User: "sbm_migration",
		PasswordFile: passwordPath, SSLMode: "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer files.cleanup()
	service, err := os.ReadFile(files.serviceFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(service, []byte(secret)) {
		t.Fatal("password leaked into PostgreSQL service configuration")
	}
	for _, expected := range [][]byte{
		[]byte("host=postgres\n"),
		[]byte("port=5432\n"),
		[]byte("dbname=smart_bill_manager\n"),
		[]byte("user=sbm_migration\n"),
		[]byte("sslmode=disable\n"),
	} {
		if !bytes.Contains(service, expected) {
			t.Fatalf("service configuration is missing %q", expected)
		}
	}
	if bytes.Contains(service, []byte("'disable'")) {
		t.Fatal("service configuration incorrectly quoted a native value")
	}
	passInfo, err := os.Stat(files.passFile)
	if err != nil {
		t.Fatal(err)
	}
	if passInfo.Mode().Perm() != 0o600 {
		t.Fatalf("passfile mode = %o", passInfo.Mode().Perm())
	}
}

func TestMigrationIdentityIncludesExactPostgreSQLContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "0001_initial.sql")
	if err := os.WriteFile(path, []byte("CREATE TABLE example (id bigint);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, descriptors, err := migrationSetIdentity(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) != 1 || descriptors[0].Name != "0001_initial.sql" || len(descriptors[0].SHA256) != 64 {
		t.Fatalf("migration descriptors = %#v", descriptors)
	}
	if err := os.WriteFile(path, []byte("CREATE TABLE example (id text);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, _, err := migrationSetIdentity(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("migration content change did not change identity")
	}
}

func TestSchemaIdentityPreservesVisibleColumnOrderWithoutPhysicalGaps(t *testing.T) {
	store := postgresqltest.Open(t)
	ctx := context.Background()
	execute := func(statement string) {
		t.Helper()
		if _, err := store.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	identity := func() string {
		t.Helper()
		tx, err := store.DB().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		value, err := schemaIdentity(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	execute(`CREATE TABLE synthetic_schema_columns (first_column text NOT NULL, removed_column boolean, last_column bigint DEFAULT 7);
		ALTER TABLE synthetic_schema_columns DROP COLUMN removed_column`)
	withGap := identity()
	execute(`DROP TABLE synthetic_schema_columns;
		CREATE TABLE synthetic_schema_columns (first_column text NOT NULL, last_column bigint DEFAULT 7)`)
	if identity() != withGap {
		t.Fatal("physical column gaps changed semantic schema identity")
	}
	for _, statement := range []string{
		`CREATE TABLE synthetic_schema_columns (last_column bigint DEFAULT 7, first_column text NOT NULL)`,
		`CREATE TABLE synthetic_schema_columns (first_column text NOT NULL, last_column integer DEFAULT 7)`,
		`CREATE TABLE synthetic_schema_columns (first_column text, last_column bigint DEFAULT 7)`,
		`CREATE TABLE synthetic_schema_columns (first_column text NOT NULL, last_column bigint DEFAULT 8)`,
		`CREATE TABLE synthetic_schema_columns (renamed_column text NOT NULL, last_column bigint DEFAULT 7)`,
	} {
		execute(`DROP TABLE synthetic_schema_columns`)
		execute(statement)
		if identity() == withGap {
			t.Fatal("visible schema drift was ignored")
		}
	}
}

func TestBackupArgumentsAreStrict(t *testing.T) {
	backup, err := parseBackupOptions([]string{
		"-objects", "/tmp/objects", "-master-key", "/tmp/key", "-migrations", "/app/migrations",
		"-output", "/tmp/backup", "-offline-confirmed",
	})
	if err != nil || backup.Output != "/tmp/backup" {
		t.Fatalf("backup options = %#v, %v", backup, err)
	}
}

func TestDeferRestoredProcessingLeasesPreservesAttemptsAndVersions(t *testing.T) {
	store := postgresqltest.Open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at)
		VALUES ('user', 'restore@example.invalid', 'synthetic', 'Restore', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at)
		VALUES ('tenant', 'Restore', 'CNY', 'Asia/Shanghai', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES ('tenant', 'user', 'owner', 'active', ?, ?)
	`, now, now); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"expired", "future", "queued"} {
		hash := strings.Repeat(string(rune('a'+index)), 64)
		if _, err := store.DB().ExecContext(ctx, `
			INSERT INTO documents (
				id, tenant_id, storage_key, original_name, declared_mime, detected_mime,
				size_bytes, sha256, page_count, status, created_by_user_id, created_at
			) VALUES (?, 'tenant', ?, ?, 'image/png', 'image/png', 1, ?, 1, 'processing', 'user', ?)
		`, id, "documents/"+id, id+".png", hash, now); err != nil {
			t.Fatal(err)
		}
	}
	expiredLease := now.Add(-time.Minute)
	futureLease := now.Add(2 * time.Minute)
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO processing_jobs (
			id, tenant_id, document_id, kind, status, attempt_count, lease_owner,
			lease_expires_at, created_at, started_at, version
		) VALUES
			('expired', 'tenant', 'expired', 'document_process', 'processing', 4, 'old', ?, ?, ?, 7),
			('future', 'tenant', 'future', 'document_process', 'processing', 5, 'old', ?, ?, ?, 8),
			('queued', 'tenant', 'queued', 'document_process', 'queued', 6, NULL, NULL, ?, NULL, 9)
	`, expiredLease, now, now, futureLease, now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := deferRestoredProcessingLeases(ctx, store.DB(), now); err != nil {
		t.Fatal(err)
	}
	rows, err := store.DB().QueryContext(ctx, `
		SELECT id, attempt_count, version, lease_expires_at
		FROM processing_jobs ORDER BY id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]struct {
		attempt int
		version int
		lease   *time.Time
	}{
		"expired": {4, 7, timePointer(now.Add(restoredProcessingLeaseGrace))},
		"future":  {5, 8, timePointer(futureLease)},
		"queued":  {6, 9, nil},
	}
	for rows.Next() {
		var id string
		var attempt, version int
		var lease *time.Time
		if err := rows.Scan(&id, &attempt, &version, &lease); err != nil {
			t.Fatal(err)
		}
		expected, ok := want[id]
		if !ok || attempt != expected.attempt || version != expected.version ||
			(lease == nil) != (expected.lease == nil) ||
			(lease != nil && !lease.Equal(*expected.lease)) {
			t.Fatalf("job %s = attempt:%d version:%d lease:%v", id, attempt, version, lease)
		}
		delete(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) != 0 {
		t.Fatalf("missing jobs: %#v", want)
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func validPostgreSQLManifest() backupManifest {
	hash := strings.Repeat("a", 64)
	return backupManifest{
		ManifestKind: manifestKind, ManifestVersion: manifestVersion,
		BackupSetID: strings.Repeat("b", 32), CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		ApplicationOffline: true, MigrationSetSHA256: hash,
		Database: databaseRecord{
			File:       fileRecord{Path: "database/sbm.pgcustom", Size: 1, SHA256: hash},
			DumpFormat: "postgresql-custom", ServerMajor: 17, SchemaSHA256: hash,
			TableCounts: map[string]int64{"documents": 0, "sessions": 0}, AuditChainSHA256: hash,
		},
		Objects: []fileRecord{}, DocumentCount: 0, ObjectReferenceCount: 0, UniqueObjectCount: 0,
	}
}
