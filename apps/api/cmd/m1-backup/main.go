package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	_ "modernc.org/sqlite"
)

const manifestVersion = 1

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type fileRecord struct {
	Path   string `json:"path"`
	Size   int64  `json:"size_bytes"`
	SHA256 string `json:"sha256"`
}

type databaseRecord struct {
	File             fileRecord       `json:"file"`
	QuickCheck       string           `json:"quick_check"`
	TableCounts      map[string]int64 `json:"table_counts"`
	AuditChainSHA256 string           `json:"audit_chain_sha256"`
}

type backupManifest struct {
	ManifestKind        string         `json:"manifest_kind"`
	ManifestVersion     int            `json:"manifest_version"`
	CreatedAt           string         `json:"created_at"`
	ApplicationOffline  bool           `json:"application_offline_confirmed"`
	Database            databaseRecord `json:"database"`
	Objects             []fileRecord   `json:"objects"`
	MasterKey           fileRecord     `json:"master_key"`
	ReferencedObjectCnt int64          `json:"referenced_object_count"`
}

type backupOptions struct {
	Database  string
	Objects   string
	MasterKey string
	Output    string
	Offline   bool
}

type restoreOptions struct {
	Backup    string
	Database  string
	Objects   string
	MasterKey string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "m1-backup:", err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("subcommand backup, verify, or restore is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch arguments[0] {
	case "backup":
		options, err := parseBackupOptions(arguments[1:])
		if err != nil {
			return err
		}
		manifest, err := createBackup(ctx, options)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(manifest)
	case "verify":
		options, err := parseVerifyOptions(arguments[1:])
		if err != nil {
			return err
		}
		manifest, err := verifyBackup(ctx, options)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(manifest)
	case "restore":
		options, err := parseRestoreOptions(arguments[1:])
		if err != nil {
			return err
		}
		manifest, err := restoreBackup(ctx, options)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(manifest)
	default:
		return fmt.Errorf("unknown subcommand %q", arguments[0])
	}
}

func parseBackupOptions(arguments []string) (backupOptions, error) {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options backupOptions
	flags.StringVar(&options.Database, "database", "", "offline SQLite database")
	flags.StringVar(&options.Objects, "objects", "", "offline object store root")
	flags.StringVar(&options.MasterKey, "master-key", "", "protected master key file")
	flags.StringVar(&options.Output, "output", "", "new backup directory")
	flags.BoolVar(&options.Offline, "offline-confirmed", false, "confirm the application is stopped")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return backupOptions{}, errors.New("invalid backup arguments")
	}
	if options.Database == "" || options.Objects == "" || options.MasterKey == "" || options.Output == "" {
		return backupOptions{}, errors.New("-database, -objects, -master-key, and -output are required")
	}
	if !options.Offline {
		return backupOptions{}, errors.New("stop the application and pass -offline-confirmed")
	}
	return options, nil
}

func parseVerifyOptions(arguments []string) (string, error) {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var backup string
	flags.StringVar(&backup, "backup", "", "backup directory")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || backup == "" {
		return "", errors.New("verify requires exactly -backup")
	}
	return backup, nil
}

func parseRestoreOptions(arguments []string) (restoreOptions, error) {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options restoreOptions
	flags.StringVar(&options.Backup, "backup", "", "backup directory")
	flags.StringVar(&options.Database, "database", "", "new database path")
	flags.StringVar(&options.Objects, "objects", "", "new object store root")
	flags.StringVar(&options.MasterKey, "master-key", "", "new master key path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return restoreOptions{}, errors.New("invalid restore arguments")
	}
	if options.Backup == "" || options.Database == "" || options.Objects == "" || options.MasterKey == "" {
		return restoreOptions{}, errors.New("-backup, -database, -objects, and -master-key are required")
	}
	return options, nil
}

func createBackup(ctx context.Context, options backupOptions) (backupManifest, error) {
	if err := requireRegular(options.Database, false); err != nil {
		return backupManifest{}, fmt.Errorf("database: %w", err)
	}
	if err := requireDirectory(options.Objects); err != nil {
		return backupManifest{}, fmt.Errorf("objects: %w", err)
	}
	if err := requireRegular(options.MasterKey, true); err != nil {
		return backupManifest{}, fmt.Errorf("master key: %w", err)
	}
	if err := validateMasterKey(options.MasterKey); err != nil {
		return backupManifest{}, err
	}
	if err := createEmptyDirectory(options.Output); err != nil {
		return backupManifest{}, err
	}

	database, err := openDatabase(options.Database, false)
	if err != nil {
		return backupManifest{}, err
	}
	defer database.Close()
	if err := checkpoint(database, ctx); err != nil {
		return backupManifest{}, err
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return backupManifest{}, fmt.Errorf("begin backup transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	record, referencedObjects, err := inspectDatabase(ctx, transaction, options.Objects)
	if err != nil {
		return backupManifest{}, err
	}

	databaseDestination := filepath.Join(options.Output, "database", "sbm.sqlite")
	if err := os.Mkdir(filepath.Dir(databaseDestination), 0o700); err != nil {
		return backupManifest{}, fmt.Errorf("create backup database directory: %w", err)
	}
	if err := copyRegularFile(options.Database, databaseDestination); err != nil {
		return backupManifest{}, fmt.Errorf("copy database: %w", err)
	}
	record.File, err = inspectFile(databaseDestination, "database/sbm.sqlite")
	if err != nil {
		return backupManifest{}, err
	}
	objects, err := copyTree(options.Objects, filepath.Join(options.Output, "objects"), "objects")
	if err != nil {
		return backupManifest{}, err
	}
	keyDestination := filepath.Join(options.Output, "secrets", "master-key")
	if err := os.Mkdir(filepath.Dir(keyDestination), 0o700); err != nil {
		return backupManifest{}, fmt.Errorf("create backup secret directory: %w", err)
	}
	if err := copyRegularFile(options.MasterKey, keyDestination); err != nil {
		return backupManifest{}, fmt.Errorf("copy master key: %w", err)
	}
	masterKey, err := inspectFile(keyDestination, "secrets/master-key")
	if err != nil {
		return backupManifest{}, err
	}
	if err := transaction.Commit(); err != nil {
		return backupManifest{}, fmt.Errorf("finish backup transaction: %w", err)
	}
	committed = true
	manifest := backupManifest{
		ManifestKind:        "smart-bill-manager-m1-backup",
		ManifestVersion:     manifestVersion,
		CreatedAt:           time.Now().UTC().Format(time.RFC3339Nano),
		ApplicationOffline:  true,
		Database:            record,
		Objects:             objects,
		MasterKey:           masterKey,
		ReferencedObjectCnt: referencedObjects,
	}
	if err := writeManifest(options.Output, manifest); err != nil {
		return backupManifest{}, err
	}
	return manifest, nil
}

func verifyBackup(ctx context.Context, root string) (backupManifest, error) {
	if err := requireDirectory(root); err != nil {
		return backupManifest{}, err
	}
	manifest, err := readManifest(root)
	if err != nil {
		return backupManifest{}, err
	}
	if err := verifyRecordedFile(root, manifest.Database.File); err != nil {
		return backupManifest{}, fmt.Errorf("database file: %w", err)
	}
	if err := verifyRecordedFile(root, manifest.MasterKey); err != nil {
		return backupManifest{}, fmt.Errorf("master key: %w", err)
	}
	if err := validateMasterKey(filepath.Join(root, filepath.FromSlash(manifest.MasterKey.Path))); err != nil {
		return backupManifest{}, err
	}
	for _, object := range manifest.Objects {
		if err := verifyRecordedFile(root, object); err != nil {
			return backupManifest{}, fmt.Errorf("object %s: %w", object.Path, err)
		}
	}
	actualObjectFiles, err := listTree(filepath.Join(root, "objects"), "objects")
	if err != nil {
		return backupManifest{}, err
	}
	if !equalFileRecords(actualObjectFiles, manifest.Objects) {
		return backupManifest{}, errors.New("backup object inventory differs from manifest")
	}
	database, err := openDatabase(filepath.Join(root, filepath.FromSlash(manifest.Database.File.Path)), true)
	if err != nil {
		return backupManifest{}, err
	}
	defer database.Close()
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return backupManifest{}, err
	}
	record, references, err := inspectDatabase(ctx, transaction, filepath.Join(root, "objects"))
	_ = transaction.Rollback()
	if err != nil {
		return backupManifest{}, err
	}
	if record.QuickCheck != manifest.Database.QuickCheck ||
		record.AuditChainSHA256 != manifest.Database.AuditChainSHA256 ||
		!equalCounts(record.TableCounts, manifest.Database.TableCounts) ||
		references != manifest.ReferencedObjectCnt {
		return backupManifest{}, errors.New("backup database contents differ from manifest")
	}
	return manifest, nil
}

func restoreBackup(ctx context.Context, options restoreOptions) (backupManifest, error) {
	manifest, err := verifyBackup(ctx, options.Backup)
	if err != nil {
		return backupManifest{}, err
	}
	if err := requireAbsent(options.Database, "database"); err != nil {
		return backupManifest{}, err
	}
	if err := requireAbsent(options.MasterKey, "master key"); err != nil {
		return backupManifest{}, err
	}
	if err := requireAbsentOrEmptyDirectory(options.Objects, "objects"); err != nil {
		return backupManifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(options.Database), 0o700); err != nil {
		return backupManifest{}, err
	}
	if err := copyRegularFile(filepath.Join(options.Backup, filepath.FromSlash(manifest.Database.File.Path)), options.Database); err != nil {
		return backupManifest{}, err
	}
	if _, err := copyTree(filepath.Join(options.Backup, "objects"), options.Objects, "objects"); err != nil {
		return backupManifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(options.MasterKey), 0o700); err != nil {
		return backupManifest{}, err
	}
	if err := copyRegularFile(filepath.Join(options.Backup, filepath.FromSlash(manifest.MasterKey.Path)), options.MasterKey); err != nil {
		return backupManifest{}, err
	}
	if err := os.Chmod(options.MasterKey, 0o600); err != nil {
		return backupManifest{}, err
	}
	if err := verifyRestored(ctx, options, manifest); err != nil {
		return backupManifest{}, err
	}
	return manifest, nil
}

func verifyRestored(ctx context.Context, options restoreOptions, manifest backupManifest) error {
	databaseFile, err := inspectFile(options.Database, manifest.Database.File.Path)
	if err != nil {
		return err
	}
	if databaseFile != manifest.Database.File {
		return errors.New("restored database file differs from manifest")
	}
	masterKey, err := inspectFile(options.MasterKey, manifest.MasterKey.Path)
	if err != nil {
		return err
	}
	if masterKey != manifest.MasterKey {
		return errors.New("restored master key differs from manifest")
	}
	if err := validateMasterKey(options.MasterKey); err != nil {
		return err
	}
	objects, err := listTree(options.Objects, "objects")
	if err != nil {
		return err
	}
	if !equalFileRecords(objects, manifest.Objects) {
		return errors.New("restored object inventory differs from manifest")
	}
	database, err := openDatabase(options.Database, true)
	if err != nil {
		return err
	}
	defer database.Close()
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	record, references, err := inspectDatabase(ctx, transaction, options.Objects)
	_ = transaction.Rollback()
	if err != nil {
		return err
	}
	if record.QuickCheck != manifest.Database.QuickCheck ||
		record.AuditChainSHA256 != manifest.Database.AuditChainSHA256 ||
		!equalCounts(record.TableCounts, manifest.Database.TableCounts) ||
		references != manifest.ReferencedObjectCnt {
		return errors.New("restored database contents differ from manifest")
	}
	return nil
}

func inspectDatabase(ctx context.Context, transaction *sql.Tx, objectsRoot string) (databaseRecord, int64, error) {
	var quickCheck string
	if err := transaction.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&quickCheck); err != nil {
		return databaseRecord{}, 0, fmt.Errorf("run SQLite quick_check: %w", err)
	}
	if quickCheck != "ok" {
		return databaseRecord{}, 0, fmt.Errorf("SQLite quick_check = %s", quickCheck)
	}
	counts, err := tableCounts(ctx, transaction)
	if err != nil {
		return databaseRecord{}, 0, err
	}
	auditHash, err := auditChainHash(ctx, transaction)
	if err != nil {
		return databaseRecord{}, 0, err
	}
	references, err := verifyObjectReferences(ctx, transaction, objectsRoot)
	if err != nil {
		return databaseRecord{}, 0, err
	}
	return databaseRecord{QuickCheck: quickCheck, TableCounts: counts, AuditChainSHA256: auditHash}, references, nil
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
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(encoded)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(encoded)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyObjectReferences(ctx context.Context, transaction *sql.Tx, root string) (int64, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT storage_key, sha256 FROM documents
		UNION ALL
		SELECT derived_image_storage_key, sha256 FROM document_pages
		ORDER BY 1
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		var key, expectedHash string
		if err := rows.Scan(&key, &expectedHash); err != nil {
			return 0, err
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(key)))
		if clean != key || clean == "." || strings.HasPrefix(clean, "../") || filepath.IsAbs(filepath.FromSlash(key)) {
			return 0, fmt.Errorf("unsafe object key %q", key)
		}
		actualHash, _, err := hashFile(filepath.Join(root, "objects", filepath.FromSlash(key)))
		if err != nil {
			return 0, fmt.Errorf("referenced object %s: %w", key, err)
		}
		if actualHash != expectedHash {
			return 0, fmt.Errorf("referenced object %s hash mismatch", key)
		}
		count++
	}
	return count, rows.Err()
}

func checkpoint(database *sql.DB, ctx context.Context) error {
	var busy, logFrames, checkpointed int
	if err := database.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("checkpoint database: %w", err)
	}
	if busy != 0 || logFrames != checkpointed {
		return fmt.Errorf("database checkpoint incomplete (busy=%d log=%d checkpointed=%d); ensure the application is stopped", busy, logFrames, checkpointed)
	}
	return nil
}

func openDatabase(path string, readOnly bool) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	mode := "rw"
	if readOnly {
		mode = "ro"
	}
	location := &url.URL{Scheme: "file", Path: absolute}
	query := location.Query()
	query.Set("mode", mode)
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(1000)")
	if !readOnly {
		query.Set("_txlock", "exclusive")
	}
	location.RawQuery = query.Encode()
	dsn := location.String()
	database, err := sql.Open("sqlite", dsn)
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

func validateMasterKey(path string) error {
	key, err := cryptography.LoadMasterKeyFile(path)
	if err != nil {
		return fmt.Errorf("validate master key: %w", err)
	}
	clear(key)
	return nil
}

func copyTree(source, destination, prefix string) ([]fileRecord, error) {
	if err := requireDirectory(source); err != nil {
		return nil, err
	}
	if err := createOrUseEmptyDirectory(destination); err != nil {
		return nil, fmt.Errorf("create destination tree: %w", err)
	}
	var records []fileRecord
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		information, err := entry.Info()
		if err != nil {
			return err
		}
		if information.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in object tree: %s", relative)
		}
		if information.IsDir() {
			return os.Mkdir(target, 0o700)
		}
		if !information.Mode().IsRegular() {
			return fmt.Errorf("non-regular object entry: %s", relative)
		}
		if err := copyRegularFile(path, target); err != nil {
			return err
		}
		record, err := inspectFile(target, filepath.ToSlash(filepath.Join(prefix, relative)))
		if err != nil {
			return err
		}
		records = append(records, record)
		return nil
	})
	sort.Slice(records, func(left, right int) bool { return records[left].Path < records[right].Path })
	return records, err
}

func listTree(root, prefix string) ([]fileRecord, error) {
	if err := requireDirectory(root); err != nil {
		return nil, err
	}
	var records []fileRecord
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		information, err := entry.Info()
		if err != nil {
			return err
		}
		if !information.Mode().IsRegular() {
			return fmt.Errorf("non-regular backup object: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		record, err := inspectFile(path, filepath.ToSlash(filepath.Join(prefix, relative)))
		if err != nil {
			return err
		}
		records = append(records, record)
		return nil
	})
	sort.Slice(records, func(left, right int) bool { return records[left].Path < records[right].Path })
	return records, err
}

func copyRegularFile(source, destination string) error {
	if err := requireRegular(source, false); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
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

func inspectFile(path, recordedPath string) (fileRecord, error) {
	hash, size, err := hashFile(path)
	return fileRecord{Path: recordedPath, Size: size, SHA256: hash}, err
}

func hashFile(path string) (string, int64, error) {
	if err := requireRegular(path, false); err != nil {
		return "", 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func verifyRecordedFile(root string, record fileRecord) error {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(record.Path)))
	if clean != record.Path || clean == "." || strings.HasPrefix(clean, "../") || filepath.IsAbs(filepath.FromSlash(record.Path)) {
		return errors.New("manifest contains an unsafe path")
	}
	hash, size, err := hashFile(filepath.Join(root, filepath.FromSlash(record.Path)))
	if err != nil {
		return err
	}
	if hash != record.SHA256 || size != record.Size {
		return errors.New("hash or size differs from manifest")
	}
	return nil
}

func requireRegular(path string, ownerOnly bool) error {
	information, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !information.Mode().IsRegular() {
		return errors.New("must be a regular file without symlinks")
	}
	if ownerOnly && information.Mode().Perm()&0o077 != 0 {
		return errors.New("must be accessible only by its owner")
	}
	return nil
}

func requireDirectory(path string) error {
	information, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !information.IsDir() || information.Mode()&os.ModeSymlink != 0 {
		return errors.New("must be a directory without symlinks")
	}
	return nil
}

func createEmptyDirectory(path string) error {
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("backup output already exists")
		}
		return err
	}
	parent := filepath.Dir(path)
	if err := requireDirectory(parent); err != nil {
		return fmt.Errorf("backup parent: %w", err)
	}
	return os.Mkdir(path, 0o700)
}

func createOrUseEmptyDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := requireDirectory(path); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("destination directory is not empty")
	}
	return os.Chmod(path, 0o700)
}

func requireAbsent(path, label string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect %s restore target: %w", label, err)
	}
	return fmt.Errorf("%s restore target already exists", label)
}

func requireAbsentOrEmptyDirectory(path, label string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect %s restore target: %w", label, err)
	}
	if err := requireDirectory(path); err != nil {
		return fmt.Errorf("%s restore target: %w", label, err)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("%s restore target is not empty", label)
	}
	return nil
}

func writeManifest(root string, manifest backupManifest) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(filepath.Join(root, "manifest.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readManifest(root string) (backupManifest, error) {
	path := filepath.Join(root, "manifest.json")
	if err := requireRegular(path, false); err != nil {
		return backupManifest{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return backupManifest{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4*1024*1024))
	decoder.DisallowUnknownFields()
	var manifest backupManifest
	if err := decoder.Decode(&manifest); err != nil {
		return backupManifest{}, err
	}
	if manifest.ManifestKind != "smart-bill-manager-m1-backup" || manifest.ManifestVersion != manifestVersion || !manifest.ApplicationOffline {
		return backupManifest{}, errors.New("unsupported or incomplete backup manifest")
	}
	if manifest.Database.File.Path != "database/sbm.sqlite" || manifest.MasterKey.Path != "secrets/master-key" {
		return backupManifest{}, errors.New("backup manifest has unexpected core paths")
	}
	return manifest, nil
}

func equalCounts(left, right map[string]int64) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func equalFileRecords(left, right []fileRecord) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
