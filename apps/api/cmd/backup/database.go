package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type migrationDescriptor struct {
	Version int
	Name    string
	File    string
}

type objectReference struct {
	SHA256  string
	Size    int64
	HasSize bool
}

type objectSummary struct {
	ReferenceCount int64
	Objects        []fileRecord
}

func openDatabase(location string, readOnly bool) (*sql.DB, error) {
	if err := requireRegular(location, false); err != nil {
		return nil, fmt.Errorf("database file: %w", err)
	}
	absolute, err := filepath.Abs(location)
	if err != nil {
		return nil, err
	}
	mode := "rw"
	if readOnly {
		mode = "ro"
	}
	dsnURL := &url.URL{Scheme: "file", Path: absolute}
	query := dsnURL.Query()
	query.Set("mode", mode)
	if readOnly {
		query.Set("immutable", "1")
	}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(1000)")
	if !readOnly {
		query.Set("_txlock", "exclusive")
	}
	dsnURL.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", dsnURL.String())
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

func checkpoint(ctx context.Context, database *sql.DB) error {
	var busy, logFrames, checkpointed int
	if err := database.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("checkpoint database: %w", err)
	}
	if busy != 0 || logFrames != checkpointed {
		return fmt.Errorf("database checkpoint incomplete (busy=%d log=%d checkpointed=%d)", busy, logFrames, checkpointed)
	}
	return nil
}

func inspectDatabase(ctx context.Context, transaction *sql.Tx, committedObjects, migrations string) (databaseRecord, objectSummary, string, error) {
	integrity, err := integrityCheck(ctx, transaction)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", err
	}
	foreignKeyViolations, err := foreignKeyViolationCount(ctx, transaction)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", err
	}
	if foreignKeyViolations != 0 {
		return databaseRecord{}, objectSummary{}, "", fmt.Errorf("SQLite foreign_key_check found %d violations", foreignKeyViolations)
	}
	migrationHash, descriptors, err := migrationSetIdentity(migrations)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", err
	}
	if err := verifyAppliedMigrations(ctx, transaction, descriptors); err != nil {
		return databaseRecord{}, objectSummary{}, "", err
	}
	schemaHash, err := schemaIdentity(ctx, transaction)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", err
	}
	counts, err := tableCounts(ctx, transaction)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", err
	}
	auditHash, err := auditChainHash(ctx, transaction)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", err
	}
	objects, err := verifyObjectReferences(ctx, transaction, committedObjects)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", err
	}
	return databaseRecord{
		IntegrityCheck:           integrity,
		ForeignKeyViolationCount: foreignKeyViolations,
		SchemaSHA256:             schemaHash,
		TableCounts:              counts,
		AuditChainSHA256:         auditHash,
	}, objects, migrationHash, nil
}

func integrityCheck(ctx context.Context, transaction *sql.Tx) (string, error) {
	rows, err := transaction.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return "", fmt.Errorf("run SQLite integrity_check: %w", err)
	}
	defer rows.Close()
	results := make([]string, 0, 1)
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return "", err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(results) != 1 || results[0] != "ok" {
		return "", errors.New("SQLite integrity_check did not return exactly ok")
	}
	return "ok", nil
}

func foreignKeyViolationCount(ctx context.Context, transaction *sql.Tx) (int64, error) {
	rows, err := transaction.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return 0, fmt.Errorf("run SQLite foreign_key_check: %w", err)
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var foreignKeyID int64
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyID); err != nil {
			return 0, err
		}
		count++
	}
	return count, rows.Err()
}

func migrationSetIdentity(root string) (string, []migrationDescriptor, error) {
	if err := requireDirectory(root); err != nil {
		return "", nil, fmt.Errorf("migration directory: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", nil, err
	}
	descriptors := make([]migrationDescriptor, 0, len(entries))
	hash := sha256.New()
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".sql") {
			return "", nil, fmt.Errorf("migration directory contains invalid entry %q", entry.Name())
		}
		prefix, suffix, ok := strings.Cut(strings.TrimSuffix(entry.Name(), ".sql"), "_")
		version, parseErr := strconv.Atoi(prefix)
		if !ok || len(prefix) != 4 || parseErr != nil || version < 1 || suffix == "" || !identifierPattern.MatchString(suffix) {
			return "", nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		content, err := readLimitedRegular(filepath.Join(root, entry.Name()), 16*1024*1024)
		if err != nil {
			return "", nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		writeFramed(hash, []byte(entry.Name()))
		writeFramed(hash, content)
		descriptors = append(descriptors, migrationDescriptor{Version: version, Name: suffix, File: entry.Name()})
	}
	sort.Slice(descriptors, func(left, right int) bool { return descriptors[left].Version < descriptors[right].Version })
	if len(descriptors) == 0 {
		return "", nil, errors.New("migration directory is empty")
	}
	for index := range descriptors {
		if index > 0 && descriptors[index-1].Version >= descriptors[index].Version {
			return "", nil, errors.New("migration versions are not unique and increasing")
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), descriptors, nil
}

func verifyAppliedMigrations(ctx context.Context, transaction *sql.Tx, expected []migrationDescriptor) error {
	rows, err := transaction.QueryContext(ctx, "SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()
	actual := make([]migrationDescriptor, 0, len(expected))
	for rows.Next() {
		var migration migrationDescriptor
		if err := rows.Scan(&migration.Version, &migration.Name); err != nil {
			return err
		}
		actual = append(actual, migration)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return errors.New("database migration set differs from current migrations")
	}
	for index := range expected {
		if actual[index].Version != expected[index].Version || actual[index].Name != expected[index].Name {
			return errors.New("database migration set differs from current migrations")
		}
	}
	return nil
}

func schemaIdentity(ctx context.Context, transaction *sql.Tx) (string, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT type, name, tbl_name, coalesce(sql, '')
		FROM sqlite_schema
		WHERE name NOT LIKE 'sqlite_%'
		ORDER BY type, name
	`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	hash := sha256.New()
	for rows.Next() {
		var objectType, name, table, statement string
		if err := rows.Scan(&objectType, &name, &table, &statement); err != nil {
			return "", err
		}
		encoded, err := json.Marshal([]string{objectType, name, table, statement})
		if err != nil {
			return "", err
		}
		writeFramed(hash, encoded)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func tableCounts(ctx context.Context, transaction *sql.Tx) (map[string]int64, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT name FROM sqlite_schema
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	tables := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if !identifierPattern.MatchString(name) {
			_ = rows.Close()
			return nil, fmt.Errorf("unexpected table name %q", name)
		}
		tables = append(tables, name)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		var count int64
		if err := transaction.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&count); err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		counts[table] = count
	}
	return counts, nil
}

func auditChainHash(ctx context.Context, transaction *sql.Tx) (string, error) {
	hash := sha256.New()
	rows, err := transaction.QueryContext(ctx, `
		SELECT id, tenant_id, actor_user_id, action, resource_type, resource_id,
		       request_id, safe_metadata_json, created_at
		FROM audit_events
		ORDER BY tenant_id, created_at, id
	`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, tenantID, action, resourceType, resourceID, requestID, metadata, createdAt string
		var actor sql.NullString
		if err := rows.Scan(&id, &tenantID, &actor, &action, &resourceType, &resourceID, &requestID, &metadata, &createdAt); err != nil {
			return "", err
		}
		var actorValue any
		if actor.Valid {
			actorValue = actor.String
		}
		encoded, err := json.Marshal([]any{id, tenantID, actorValue, action, resourceType, resourceID, requestID, json.RawMessage(metadata), createdAt})
		if err != nil {
			return "", err
		}
		writeFramed(hash, encoded)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyObjectReferences(ctx context.Context, transaction *sql.Tx, committedRoot string) (objectSummary, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT source_kind, source_id, storage_key, sha256, size_bytes
		FROM (
			SELECT 'document' AS source_kind, id AS source_id, storage_key, sha256, size_bytes FROM documents
			UNION ALL
			SELECT 'document_page', id, derived_image_storage_key, sha256, NULL FROM document_pages
			UNION ALL
			SELECT 'email_message', id, raw_storage_key, raw_sha256, raw_size_bytes FROM email_messages
			UNION ALL
			SELECT 'email_attachment', id, storage_key, sha256, size_bytes
			FROM email_attachments WHERE storage_key IS NOT NULL
		)
		ORDER BY storage_key, source_kind, source_id
	`)
	if err != nil {
		return objectSummary{}, err
	}
	defer rows.Close()
	references := make(map[string]objectReference)
	var referenceCount int64
	for rows.Next() {
		var sourceKind, sourceID, key, expectedHash string
		var size sql.NullInt64
		if err := rows.Scan(&sourceKind, &sourceID, &key, &expectedHash, &size); err != nil {
			return objectSummary{}, err
		}
		if sourceKind == "" || sourceID == "" || !safeRelativePath(key) || !lowerHex64Pattern.MatchString(expectedHash) || (size.Valid && size.Int64 < 1) {
			return objectSummary{}, errors.New("database contains an invalid object reference")
		}
		incoming := objectReference{SHA256: expectedHash, Size: size.Int64, HasSize: size.Valid}
		if existing, found := references[key]; found {
			if existing.SHA256 != incoming.SHA256 || (existing.HasSize && incoming.HasSize && existing.Size != incoming.Size) {
				return objectSummary{}, fmt.Errorf("object key %q has conflicting database references", key)
			}
			if !existing.HasSize && incoming.HasSize {
				references[key] = incoming
			}
		} else {
			references[key] = incoming
		}
		referenceCount++
	}
	if err := rows.Err(); err != nil {
		return objectSummary{}, err
	}
	actual, err := listTree(committedRoot, "objects")
	if err != nil {
		return objectSummary{}, err
	}
	if len(actual) != len(references) {
		return objectSummary{}, fmt.Errorf("committed object count %d differs from unique database reference count %d", len(actual), len(references))
	}
	for _, record := range actual {
		key := strings.TrimPrefix(record.Path, "objects/")
		reference, found := references[key]
		if !found {
			return objectSummary{}, fmt.Errorf("committed object %q has no database reference", key)
		}
		if reference.SHA256 != record.SHA256 || (reference.HasSize && reference.Size != record.Size) {
			return objectSummary{}, fmt.Errorf("committed object %q differs from its database reference", key)
		}
	}
	return objectSummary{ReferenceCount: referenceCount, Objects: actual}, nil
}

func invalidateSessions(ctx context.Context, databasePath string) (int64, error) {
	database, err := openDatabase(databasePath, false)
	if err != nil {
		return 0, err
	}
	defer database.Close()
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	result, err := transaction.ExecContext(ctx, "DELETE FROM sessions")
	if err != nil {
		_ = transaction.Rollback()
		return 0, fmt.Errorf("invalidate restored sessions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		_ = transaction.Rollback()
		return 0, err
	}
	if err := transaction.Commit(); err != nil {
		return 0, err
	}
	if err := checkpoint(ctx, database); err != nil {
		return 0, err
	}
	return count, nil
}

func databaseStateEqual(actual, expected databaseRecord, expectedCounts map[string]int64) bool {
	return actual.IntegrityCheck == expected.IntegrityCheck &&
		actual.ForeignKeyViolationCount == expected.ForeignKeyViolationCount &&
		actual.SchemaSHA256 == expected.SchemaSHA256 &&
		actual.AuditChainSHA256 == expected.AuditChainSHA256 &&
		equalCounts(actual.TableCounts, expectedCounts)
}

func writeFramed(destination interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}
