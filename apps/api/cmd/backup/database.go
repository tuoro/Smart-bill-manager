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
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type migrationDescriptor struct {
	Version int
	Name    string
	SHA256  string
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

func inspectDatabase(ctx context.Context, transaction *sql.Tx, committedObjects, migrations string) (databaseRecord, objectSummary, string, error) {
	serverMajor, err := postgresServerMajor(ctx, transaction)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", fmt.Errorf("inspect PostgreSQL server identity: %w", err)
	}
	if serverMajor != 17 {
		return databaseRecord{}, objectSummary{}, "", fmt.Errorf("PostgreSQL server major %d is unsupported", serverMajor)
	}
	if err := verifyValidatedForeignKeys(ctx, transaction); err != nil {
		return databaseRecord{}, objectSummary{}, "", fmt.Errorf("inspect PostgreSQL constraints: %w", err)
	}
	migrationHash, descriptors, err := migrationSetIdentity(migrations)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", err
	}
	if err := verifyAppliedMigrations(ctx, transaction, descriptors); err != nil {
		return databaseRecord{}, objectSummary{}, "", fmt.Errorf("inspect PostgreSQL migration identity: %w", err)
	}
	schemaHash, err := schemaIdentity(ctx, transaction)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", fmt.Errorf("inspect PostgreSQL schema identity: %w", err)
	}
	counts, err := tableCounts(ctx, transaction)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", fmt.Errorf("inspect PostgreSQL table inventory: %w", err)
	}
	auditHash, err := auditChainHash(ctx, transaction)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", fmt.Errorf("inspect PostgreSQL audit identity: %w", err)
	}
	objects, err := verifyObjectReferences(ctx, transaction, committedObjects)
	if err != nil {
		return databaseRecord{}, objectSummary{}, "", fmt.Errorf("inspect PostgreSQL object references: %w", err)
	}
	return databaseRecord{
		DumpFormat:       "postgresql-custom",
		ServerMajor:      serverMajor,
		SchemaSHA256:     schemaHash,
		TableCounts:      counts,
		AuditChainSHA256: auditHash,
	}, objects, migrationHash, nil
}

func postgresServerMajor(ctx context.Context, transaction *sql.Tx) (int, error) {
	var major int
	if err := transaction.QueryRowContext(ctx, `SELECT current_setting('server_version_num')::integer / 10000`).Scan(&major); err != nil {
		return 0, fmt.Errorf("read PostgreSQL server major: %w", err)
	}
	return major, nil
}

func verifyValidatedForeignKeys(ctx context.Context, transaction *sql.Tx) error {
	var invalid int64
	if err := transaction.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_constraint constraint_row
		JOIN pg_namespace namespace ON namespace.oid = constraint_row.connamespace
		WHERE namespace.nspname = 'public' AND constraint_row.contype = 'f' AND NOT constraint_row.convalidated
	`).Scan(&invalid); err != nil {
		return fmt.Errorf("inspect PostgreSQL foreign keys: %w", err)
	}
	if invalid != 0 {
		return fmt.Errorf("PostgreSQL has %d unvalidated foreign keys", invalid)
	}
	return nil
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
		contentDigest := sha256.Sum256(content)
		writeFramed(hash, []byte(entry.Name()))
		writeFramed(hash, content)
		descriptors = append(descriptors, migrationDescriptor{
			Version: version,
			Name:    entry.Name(),
			SHA256:  hex.EncodeToString(contentDigest[:]),
			File:    entry.Name(),
		})
	}
	sort.Slice(descriptors, func(left, right int) bool { return descriptors[left].Version < descriptors[right].Version })
	if len(descriptors) == 0 {
		return "", nil, errors.New("migration directory is empty")
	}
	for index, descriptor := range descriptors {
		if descriptor.Version != index+1 {
			return "", nil, errors.New("migration versions must be contiguous from 0001")
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), descriptors, nil
}

func verifyAppliedMigrations(ctx context.Context, transaction *sql.Tx, expected []migrationDescriptor) error {
	rows, err := transaction.QueryContext(ctx, `SELECT version, name, content_sha256 FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()
	actual := make([]migrationDescriptor, 0, len(expected))
	for rows.Next() {
		var migration migrationDescriptor
		if err := rows.Scan(&migration.Version, &migration.Name, &migration.SHA256); err != nil {
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
		if actual[index].Version != expected[index].Version || actual[index].Name != expected[index].Name || actual[index].SHA256 != expected[index].SHA256 {
			return errors.New("database migration set differs from current migrations")
		}
	}
	return nil
}

func schemaIdentity(ctx context.Context, transaction *sql.Tx) (string, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT object_kind, object_identity, definition
		FROM (
			SELECT 'column' AS object_kind,
			       table_name || '.' || lpad(ordinal_position::text, 4, '0') AS object_identity,
			       concat_ws('|', column_name, data_type, udt_name, is_nullable, coalesce(column_default, '')) AS definition
			FROM information_schema.columns WHERE table_schema = 'public'
			UNION ALL
			SELECT 'constraint', relation.relname || '.' || constraint_row.conname,
			       pg_get_constraintdef(constraint_row.oid, true)
			FROM pg_constraint constraint_row
			JOIN pg_class relation ON relation.oid = constraint_row.conrelid
			JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
			WHERE namespace.nspname = 'public'
			UNION ALL
			SELECT 'index', tablename || '.' || indexname, indexdef
			FROM pg_indexes WHERE schemaname = 'public'
			UNION ALL
			SELECT 'function', routine.proname || '(' || pg_get_function_identity_arguments(routine.oid) || ')', pg_get_functiondef(routine.oid)
			FROM pg_proc routine JOIN pg_namespace namespace ON namespace.oid = routine.pronamespace
			WHERE namespace.nspname = 'public'
			UNION ALL
			SELECT 'trigger', relation.relname || '.' || trigger.tgname, pg_get_triggerdef(trigger.oid, true)
			FROM pg_trigger trigger JOIN pg_class relation ON relation.oid = trigger.tgrelid
			JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
			WHERE namespace.nspname = 'public' AND NOT trigger.tgisinternal
		) objects ORDER BY object_kind, object_identity
	`)
	if err != nil {
		return "", fmt.Errorf("read PostgreSQL schema identity: %w", err)
	}
	defer rows.Close()
	hash := sha256.New()
	for rows.Next() {
		var kind, identity, definition string
		if err := rows.Scan(&kind, &identity, &definition); err != nil {
			return "", err
		}
		encoded, err := json.Marshal([]string{kind, identity, definition})
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
	rows, err := transaction.QueryContext(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename`)
	if err != nil {
		return nil, err
	}
	var tables []string
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
		       request_id, safe_metadata_json::text, created_at::text
		FROM audit_events ORDER BY tenant_id, created_at, id
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
		) object_references ORDER BY storage_key, source_kind, source_id
	`)
	if err != nil {
		return objectSummary{}, fmt.Errorf("query PostgreSQL object references: %w", err)
	}
	defer rows.Close()
	references := make(map[string]objectReference)
	var referenceCount int64
	for rows.Next() {
		var sourceKind, sourceID, key, expectedHash string
		var size sql.NullInt64
		if err := rows.Scan(&sourceKind, &sourceID, &key, &expectedHash, &size); err != nil {
			return objectSummary{}, fmt.Errorf("scan PostgreSQL object references: %w", err)
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
		return objectSummary{}, fmt.Errorf("iterate PostgreSQL object references: %w", err)
	}
	actual, err := listTree(committedRoot, "objects")
	if err != nil {
		return objectSummary{}, fmt.Errorf("list committed object inventory: %w", err)
	}
	if len(actual) != len(references) {
		return objectSummary{}, fmt.Errorf("committed object count %d differs from unique database reference count %d", len(actual), len(references))
	}
	for _, record := range actual {
		key := strings.TrimPrefix(record.Path, "objects/")
		reference, found := references[key]
		if !found || reference.SHA256 != record.SHA256 || (reference.HasSize && reference.Size != record.Size) {
			return objectSummary{}, fmt.Errorf("committed object %q differs from its database reference", key)
		}
	}
	return objectSummary{ReferenceCount: referenceCount, Objects: actual}, nil
}

func invalidateSessions(ctx context.Context, database *sql.DB) (int64, error) {
	result, err := database.ExecContext(ctx, "DELETE FROM sessions")
	if err != nil {
		return 0, fmt.Errorf("invalidate restored sessions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return count, nil
}

func databaseStateEqual(actual, expected databaseRecord, expectedCounts map[string]int64) bool {
	return actual.DumpFormat == expected.DumpFormat && actual.ServerMajor == expected.ServerMajor &&
		actual.SchemaSHA256 == expected.SchemaSHA256 && actual.AuditChainSHA256 == expected.AuditChainSHA256 &&
		equalCounts(actual.TableCounts, expectedCounts)
}

func writeFramed(destination interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}
