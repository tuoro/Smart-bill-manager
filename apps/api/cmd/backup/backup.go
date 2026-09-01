package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
)

const restoredProcessingLeaseGrace = 2 * time.Minute

func createBackup(ctx context.Context, options backupOptions) (backupManifest, error) {
	if !options.Offline {
		return backupManifest{}, errors.New("offline confirmation is required")
	}
	committedObjects, err := validateObjectStore(options.Objects)
	if err != nil {
		return backupManifest{}, fmt.Errorf("object store: %w", err)
	}
	if err := requireDirectory(options.Migrations); err != nil {
		return backupManifest{}, fmt.Errorf("migrations: %w", err)
	}
	if err := requireAbsent(options.Output, "backup output"); err != nil {
		return backupManifest{}, err
	}
	for _, source := range []string{options.Objects, options.Migrations} {
		overlap, err := pathsOverlap(options.Output, source)
		if err != nil {
			return backupManifest{}, err
		}
		if overlap {
			return backupManifest{}, errors.New("backup output target must be disjoint from object storage and migrations")
		}
	}
	for _, protectedData := range []string{options.Objects, options.Output} {
		if overlap, err := pathsOverlap(options.MasterKey, protectedData); err != nil {
			return backupManifest{}, err
		} else if overlap {
			return backupManifest{}, errors.New("master key must be independently stored outside data and backup paths")
		}
	}
	masterKey, err := loadMasterKey(options.MasterKey)
	if err != nil {
		return backupManifest{}, err
	}
	defer clear(masterKey)
	runtimeConfig, err := postgresqladapter.RuntimeConfigFromEnvironment()
	if err != nil {
		return backupManifest{}, err
	}
	dumpConfig, err := postgresqladapter.MigrationConfigFromEnvironment()
	if err != nil {
		return backupManifest{}, err
	}
	if !samePostgreSQLEndpoint(runtimeConfig, dumpConfig) {
		return backupManifest{}, errors.New("runtime and dump PostgreSQL endpoints differ")
	}
	store, err := postgresqladapter.Open(ctx, runtimeConfig)
	if err != nil {
		return backupManifest{}, err
	}
	defer store.Close()

	staging, err := os.MkdirTemp(filepath.Dir(options.Output), ".sbm-backup-")
	if err != nil {
		return backupManifest{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	if err := os.Chmod(staging, 0o700); err != nil {
		_ = os.RemoveAll(staging)
		return backupManifest{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()

	transaction, err := store.DB().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return backupManifest{}, fmt.Errorf("begin PostgreSQL backup inspection: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	databaseState, objectState, migrationHash, err := inspectDatabase(ctx, transaction, committedObjects, options.Migrations)
	if err != nil {
		return backupManifest{}, fmt.Errorf("inspect PostgreSQL backup state: %w", err)
	}
	databaseDirectory := filepath.Join(staging, "database")
	if err := os.Mkdir(databaseDirectory, 0o700); err != nil {
		return backupManifest{}, err
	}
	dumpPath := filepath.Join(databaseDirectory, "sbm.pgcustom")
	if err := createPostgresDump(ctx, dumpConfig, dumpPath); err != nil {
		return backupManifest{}, fmt.Errorf("create PostgreSQL backup dump: %w", err)
	}
	if err := verifyPostgresDump(ctx, dumpPath); err != nil {
		return backupManifest{}, fmt.Errorf("verify PostgreSQL backup dump: %w", err)
	}
	databaseState.File, err = inspectFile(dumpPath, "database/sbm.pgcustom")
	if err != nil {
		return backupManifest{}, fmt.Errorf("record PostgreSQL backup dump: %w", err)
	}
	copiedObjects, err := copyTree(committedObjects, filepath.Join(staging, "objects"), "objects")
	if err != nil {
		return backupManifest{}, fmt.Errorf("copy committed objects: %w", err)
	}
	if !equalFileRecords(copiedObjects, objectState.Objects) {
		return backupManifest{}, errors.New("object store changed while the offline snapshot was copied")
	}
	if err := transaction.Commit(); err != nil {
		return backupManifest{}, fmt.Errorf("finish PostgreSQL backup inspection: %w", err)
	}
	committed = true
	backupSetID, err := newBackupSetID()
	if err != nil {
		return backupManifest{}, err
	}
	manifest := backupManifest{
		ManifestKind: manifestKind, ManifestVersion: manifestVersion, BackupSetID: backupSetID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), ApplicationOffline: true,
		MigrationSetSHA256: migrationHash, Database: databaseState, Objects: copiedObjects,
		DocumentCount: databaseState.TableCounts["documents"], ObjectReferenceCount: objectState.ReferenceCount,
		UniqueObjectCount: int64(len(copiedObjects)),
	}
	if err := writeAuthenticatedManifest(staging, manifest, masterKey); err != nil {
		return backupManifest{}, fmt.Errorf("authenticate backup package: %w", err)
	}
	if err := syncTreeDirectories(staging); err != nil {
		return backupManifest{}, fmt.Errorf("sync backup staging tree: %w", err)
	}
	verified, err := verifyPackage(ctx, staging, masterKey, options.Migrations)
	if err != nil {
		return backupManifest{}, fmt.Errorf("verify staged backup: %w", err)
	}
	if err := publishNoReplace(staging, options.Output); err != nil {
		return backupManifest{}, fmt.Errorf("publish data backup: %w", err)
	}
	published = true
	return verified, nil
}

func verifyBackup(ctx context.Context, options verifyOptions) (backupManifest, error) {
	if overlap, err := pathsOverlap(options.MasterKey, options.Backup); err != nil {
		return backupManifest{}, err
	} else if overlap {
		return backupManifest{}, errors.New("master key must be independently stored outside the data backup")
	}
	masterKey, err := loadMasterKey(options.MasterKey)
	if err != nil {
		return backupManifest{}, err
	}
	defer clear(masterKey)
	return verifyPackage(ctx, options.Backup, masterKey, options.Migrations)
}

func verifyPackage(ctx context.Context, root string, masterKey []byte, migrations string) (backupManifest, error) {
	if err := verifyPackageLayout(root); err != nil {
		return backupManifest{}, err
	}
	manifest, err := readAuthenticatedManifest(root, masterKey)
	if err != nil {
		return backupManifest{}, err
	}
	migrationHash, _, err := migrationSetIdentity(migrations)
	if err != nil {
		return backupManifest{}, err
	}
	if migrationHash != manifest.MigrationSetSHA256 {
		return backupManifest{}, errors.New("current migration set differs from backup manifest")
	}
	if err := verifyRecordedFile(root, manifest.Database.File); err != nil {
		return backupManifest{}, fmt.Errorf("database dump: %w", err)
	}
	if err := verifyPostgresDump(ctx, filepath.Join(root, "database", "sbm.pgcustom")); err != nil {
		return backupManifest{}, err
	}
	for _, object := range manifest.Objects {
		if err := verifyRecordedFile(root, object); err != nil {
			return backupManifest{}, fmt.Errorf("object %s: %w", object.Path, err)
		}
	}
	actualObjects, err := listTree(filepath.Join(root, "objects"), "objects")
	if err != nil {
		return backupManifest{}, err
	}
	if !equalFileRecords(actualObjects, manifest.Objects) {
		return backupManifest{}, errors.New("backup object inventory differs from manifest")
	}
	return manifest, nil
}

func restoreBackup(ctx context.Context, options restoreOptions) (backupManifest, int64, error) {
	if !options.Offline {
		return backupManifest{}, 0, errors.New("offline confirmation is required")
	}
	manifest, err := verifyBackup(ctx, verifyOptions{
		Backup: options.Backup, MasterKey: options.MasterKeySource, Migrations: options.Migrations,
	})
	if err != nil {
		return backupManifest{}, 0, err
	}
	for _, target := range []struct{ path, label string }{{options.Objects, "object store"}, {options.MasterKey, "master key"}} {
		if err := requireAbsent(target.path, target.label); err != nil {
			return backupManifest{}, 0, err
		}
	}
	for _, pair := range [][2]string{
		{options.Backup, options.Objects}, {options.Backup, options.MasterKey},
		{options.Migrations, options.Objects}, {options.Migrations, options.MasterKey},
		{options.MasterKeySource, options.Objects}, {options.MasterKeySource, filepath.Dir(options.MasterKey)},
		{options.Objects, options.MasterKey},
	} {
		overlap, err := pathsOverlap(pair[0], pair[1])
		if err != nil {
			return backupManifest{}, 0, err
		}
		if overlap {
			return backupManifest{}, 0, errors.New("backup and restore targets must be disjoint")
		}
	}
	restoreConfig, err := postgresqladapter.RestoreConfigFromEnvironment()
	if err != nil {
		return backupManifest{}, 0, err
	}
	objectStage, err := os.MkdirTemp(filepath.Dir(options.Objects), ".sbm-restore-objects-")
	if err != nil {
		return backupManifest{}, 0, err
	}
	keyStage, err := os.MkdirTemp(filepath.Dir(options.MasterKey), ".sbm-restore-key-")
	if err != nil {
		_ = os.RemoveAll(objectStage)
		return backupManifest{}, 0, err
	}
	defer os.RemoveAll(objectStage)
	defer os.RemoveAll(keyStage)
	for _, stage := range []string{objectStage, keyStage} {
		if err := os.Chmod(stage, 0o700); err != nil {
			return backupManifest{}, 0, err
		}
	}
	if _, err := createObjectStoreFromPackage(filepath.Join(options.Backup, "objects"), objectStage); err != nil {
		return backupManifest{}, 0, err
	}
	stagedKey := filepath.Join(keyStage, "master-key")
	if err := copyRegularFile(options.MasterKeySource, stagedKey); err != nil {
		return backupManifest{}, 0, err
	}
	if err := restorePostgresDump(ctx, restoreConfig, filepath.Join(options.Backup, "database", "sbm.pgcustom")); err != nil {
		return backupManifest{}, 0, err
	}
	if err := postgresqladapter.Migrate(ctx, restoreConfig); err != nil {
		return backupManifest{}, 0, fmt.Errorf("verify restored migrations: %w", err)
	}
	store, err := postgresqladapter.Open(ctx, restoreConfig)
	if err != nil {
		return backupManifest{}, 0, err
	}
	defer store.Close()
	if err := verifyRestoredState(ctx, store.DB(), objectStage, options.Migrations, manifest, manifest.Database.TableCounts); err != nil {
		return backupManifest{}, 0, fmt.Errorf("verify restored PostgreSQL state: %w", err)
	}
	invalidatedSessions, err := invalidateSessions(ctx, store.DB())
	if err != nil {
		return backupManifest{}, 0, err
	}
	if invalidatedSessions != manifest.Database.TableCounts["sessions"] {
		return backupManifest{}, 0, errors.New("restored session invalidation count differs from manifest")
	}
	if err := deferRestoredProcessingLeases(ctx, store.DB(), time.Now().UTC()); err != nil {
		return backupManifest{}, 0, err
	}
	expectedCounts := cloneCounts(manifest.Database.TableCounts)
	expectedCounts["sessions"] = 0
	if err := verifyRestoredState(ctx, store.DB(), objectStage, options.Migrations, manifest, expectedCounts); err != nil {
		return backupManifest{}, 0, fmt.Errorf("verify activated PostgreSQL state: %w", err)
	}
	publish := options.publish
	if publish == nil {
		publish = publishNoReplace
	}
	for _, item := range []struct{ source, destination, label string }{
		{objectStage, options.Objects, "object store"}, {stagedKey, options.MasterKey, "master key"},
	} {
		if err := publish(item.source, item.destination); err != nil {
			return backupManifest{}, 0, fmt.Errorf("publish restored %s: %w", item.label, err)
		}
	}
	return manifest, invalidatedSessions, nil
}

// deferRestoredProcessingLeases 保留恢复快照中的 attempt/version，同时给只读
// 基线验证留下确定性窗口；窗口结束后仍由原有过期租约竞争语义接管。
func deferRestoredProcessingLeases(ctx context.Context, database *sql.DB, now time.Time) error {
	_, err := database.ExecContext(ctx, `
		UPDATE processing_jobs
		SET lease_expires_at = GREATEST(lease_expires_at, ?)
		WHERE status = 'processing'
	`, now.Add(restoredProcessingLeaseGrace).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("defer restored processing leases: %w", err)
	}
	return nil
}

func verifyRestoredState(ctx context.Context, database *sql.DB, objectStore, migrations string, manifest backupManifest, expectedCounts map[string]int64) error {
	committedObjects, err := validateObjectStore(objectStore)
	if err != nil {
		return err
	}
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	actualDatabase, actualObjects, migrationHash, err := inspectDatabase(ctx, transaction, committedObjects, migrations)
	if err != nil {
		return err
	}
	if migrationHash != manifest.MigrationSetSHA256 || !databaseStateEqual(actualDatabase, manifest.Database, expectedCounts) {
		return errors.New("restored PostgreSQL state differs from authenticated manifest")
	}
	if actualObjects.ReferenceCount != manifest.ObjectReferenceCount || !equalFileRecords(actualObjects.Objects, manifest.Objects) {
		return errors.New("restored object state differs from authenticated manifest")
	}
	return transaction.Commit()
}

func samePostgreSQLEndpoint(left, right postgresqladapter.Config) bool {
	return left.Host == right.Host && left.Port == right.Port && left.Database == right.Database && left.SSLMode == right.SSLMode &&
		left.RootCertificateFile == right.RootCertificateFile
}

func cloneCounts(source map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(source))
	for table, count := range source {
		result[table] = count
	}
	return result
}

func verifyManifestIdentity(left, right backupManifest) bool {
	return reflect.DeepEqual(left, right)
}
