package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
)

const (
	manifestKind       = "smart-bill-manager-backup"
	manifestVersion    = 2
	manifestName       = "manifest.json"
	manifestAuthName   = "manifest.hmac"
	manifestLimitBytes = 4 * 1024 * 1024
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	lowerHex64Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	lowerHex32Pattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type fileRecord struct {
	Path   string `json:"path"`
	Size   int64  `json:"size_bytes"`
	SHA256 string `json:"sha256"`
}

type databaseRecord struct {
	File                     fileRecord       `json:"file"`
	IntegrityCheck           string           `json:"integrity_check"`
	ForeignKeyViolationCount int64            `json:"foreign_key_violation_count"`
	SchemaSHA256             string           `json:"schema_sha256"`
	TableCounts              map[string]int64 `json:"table_counts"`
	AuditChainSHA256         string           `json:"audit_chain_sha256"`
}

type backupManifest struct {
	ManifestKind         string         `json:"manifest_kind"`
	ManifestVersion      int            `json:"manifest_version"`
	BackupSetID          string         `json:"backup_set_id"`
	CreatedAt            string         `json:"created_at"`
	ApplicationOffline   bool           `json:"application_offline_confirmed"`
	MigrationSetSHA256   string         `json:"migration_set_sha256"`
	Database             databaseRecord `json:"database"`
	Objects              []fileRecord   `json:"objects"`
	DocumentCount        int64          `json:"document_count"`
	ObjectReferenceCount int64          `json:"object_reference_count"`
	UniqueObjectCount    int64          `json:"unique_object_count"`
}

func newBackupSetID() (string, error) {
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return "", fmt.Errorf("generate backup set identity: %w", err)
	}
	return hex.EncodeToString(identifier), nil
}

func loadMasterKey(path string) ([]byte, error) {
	if err := requireRegular(path, true); err != nil {
		return nil, fmt.Errorf("master key: %w", err)
	}
	key, err := cryptography.LoadMasterKeyFile(path)
	if err != nil {
		return nil, fmt.Errorf("validate master key: %w", err)
	}
	return key, nil
}

func writeAuthenticatedManifest(root string, manifest backupManifest, masterKey []byte) error {
	if err := validateManifest(manifest); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backup manifest: %w", err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > manifestLimitBytes {
		return errors.New("backup manifest exceeds size limit")
	}
	tag := authenticateManifest(masterKey, encoded)
	authenticated := append([]byte(hex.EncodeToString(tag)), '\n')
	clear(tag)
	if err := writeExclusiveFile(filepath.Join(root, manifestName), encoded, 0o600); err != nil {
		return fmt.Errorf("write backup manifest: %w", err)
	}
	if err := writeExclusiveFile(filepath.Join(root, manifestAuthName), authenticated, 0o600); err != nil {
		return fmt.Errorf("write backup manifest authentication: %w", err)
	}
	return nil
}

func readAuthenticatedManifest(root string, masterKey []byte) (backupManifest, error) {
	if err := requireDirectory(root); err != nil {
		return backupManifest{}, fmt.Errorf("backup root: %w", err)
	}
	manifestBytes, err := readLimitedRegular(filepath.Join(root, manifestName), manifestLimitBytes)
	if err != nil {
		return backupManifest{}, fmt.Errorf("read backup manifest: %w", err)
	}
	authBytes, err := readLimitedRegular(filepath.Join(root, manifestAuthName), 65)
	if err != nil {
		return backupManifest{}, fmt.Errorf("read backup manifest authentication: %w", err)
	}
	if len(authBytes) != 65 || authBytes[64] != '\n' || !lowerHex64Pattern.Match(authBytes[:64]) {
		return backupManifest{}, errors.New("backup manifest authentication is invalid")
	}
	expected := authenticateManifest(masterKey, manifestBytes)
	actual := make([]byte, sha256.Size)
	if _, err := hex.Decode(actual, authBytes[:64]); err != nil {
		clear(expected)
		return backupManifest{}, errors.New("backup manifest authentication is invalid")
	}
	matched := subtle.ConstantTimeCompare(expected, actual) == 1
	clear(expected)
	clear(actual)
	if !matched {
		return backupManifest{}, errors.New("backup manifest authentication failed")
	}
	if err := rejectDuplicateJSONKeys(manifestBytes); err != nil {
		return backupManifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	var manifest backupManifest
	if err := decoder.Decode(&manifest); err != nil {
		return backupManifest{}, fmt.Errorf("decode backup manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return backupManifest{}, errors.New("backup manifest contains trailing JSON")
	}
	if err := validateManifest(manifest); err != nil {
		return backupManifest{}, err
	}
	return manifest, nil
}

func rejectDuplicateJSONKeys(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	var readValue func() error
	readValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("backup manifest contains an invalid object key")
				}
				if _, duplicate := seen[key]; duplicate {
					return errors.New("backup manifest contains a duplicate JSON field")
				}
				seen[key] = struct{}{}
				if err := readValue(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return errors.New("backup manifest contains an invalid JSON object")
			}
		case '[':
			for decoder.More() {
				if err := readValue(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return errors.New("backup manifest contains an invalid JSON array")
			}
		default:
			return errors.New("backup manifest contains an invalid JSON delimiter")
		}
		return nil
	}
	if err := readValue(); err != nil {
		return fmt.Errorf("validate backup manifest JSON keys: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("backup manifest contains trailing JSON")
	}
	return nil
}

func authenticateManifest(masterKey, manifest []byte) []byte {
	derivation := hmac.New(sha256.New, masterKey)
	_, _ = derivation.Write([]byte("smart-bill-manager/backup-manifest-auth-key/v1"))
	derivedKey := derivation.Sum(nil)
	authenticator := hmac.New(sha256.New, derivedKey)
	clear(derivedKey)
	_, _ = authenticator.Write([]byte("smart-bill-manager/backup-manifest/v2\x00"))
	_, _ = authenticator.Write(manifest)
	return authenticator.Sum(nil)
}

func validateManifest(manifest backupManifest) error {
	if manifest.ManifestKind != manifestKind || manifest.ManifestVersion != manifestVersion || !manifest.ApplicationOffline {
		return errors.New("unsupported or incomplete backup manifest")
	}
	if !lowerHex32Pattern.MatchString(manifest.BackupSetID) {
		return errors.New("backup manifest contains an invalid backup set identity")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, manifest.CreatedAt)
	_, offset := createdAt.Zone()
	if err != nil || offset != 0 || createdAt.Format(time.RFC3339Nano) != manifest.CreatedAt {
		return errors.New("backup manifest creation time is not canonical UTC")
	}
	if !lowerHex64Pattern.MatchString(manifest.MigrationSetSHA256) ||
		!lowerHex64Pattern.MatchString(manifest.Database.SchemaSHA256) ||
		!lowerHex64Pattern.MatchString(manifest.Database.AuditChainSHA256) {
		return errors.New("backup manifest contains an invalid identity hash")
	}
	if manifest.Database.IntegrityCheck != "ok" || manifest.Database.ForeignKeyViolationCount != 0 {
		return errors.New("backup manifest does not record a valid database")
	}
	if err := validateFileRecord(manifest.Database.File, "database/sbm.sqlite"); err != nil {
		return fmt.Errorf("database record: %w", err)
	}
	if manifest.Database.TableCounts == nil || len(manifest.Database.TableCounts) == 0 {
		return errors.New("backup manifest table counts are empty")
	}
	for table, count := range manifest.Database.TableCounts {
		if !identifierPattern.MatchString(table) || count < 0 {
			return errors.New("backup manifest contains an invalid table count")
		}
	}
	documents, hasDocuments := manifest.Database.TableCounts["documents"]
	_, hasSessions := manifest.Database.TableCounts["sessions"]
	if !hasDocuments || !hasSessions || documents != manifest.DocumentCount || manifest.DocumentCount < 0 {
		return errors.New("backup manifest document or session count is inconsistent")
	}
	if manifest.ObjectReferenceCount < 0 || manifest.UniqueObjectCount < 0 ||
		manifest.ObjectReferenceCount < manifest.UniqueObjectCount ||
		int64(len(manifest.Objects)) != manifest.UniqueObjectCount {
		return errors.New("backup manifest object counts are inconsistent")
	}
	previous := ""
	for _, object := range manifest.Objects {
		if err := validateFileRecord(object, ""); err != nil {
			return fmt.Errorf("object record: %w", err)
		}
		if len(object.Path) <= len("objects/") || object.Path[:len("objects/")] != "objects/" || object.Path <= previous {
			return errors.New("backup manifest object records are not unique and sorted")
		}
		previous = object.Path
	}
	return nil
}

func validateFileRecord(record fileRecord, requiredPath string) error {
	if requiredPath != "" && record.Path != requiredPath {
		return errors.New("unexpected path")
	}
	if !safeRelativePath(record.Path) || record.Size < 1 || !lowerHex64Pattern.MatchString(record.SHA256) {
		return errors.New("invalid path, size, or SHA-256")
	}
	return nil
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

func sortedTableNames(counts map[string]int64) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
