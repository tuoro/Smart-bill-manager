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

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/runtimeguard"
)

func createBackup(ctx context.Context, options backupOptions) (backupManifest, error) {
	if !options.Offline {
		return backupManifest{}, errors.New("offline confirmation is required")
	}
	if err := requireRegular(options.Database, false); err != nil {
		return backupManifest{}, fmt.Errorf("database: %w", err)
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
	for _, source := range []string{filepath.Dir(options.Database), options.Objects, options.Migrations} {
		overlap, err := pathsOverlap(options.Output, source)
		if err != nil {
			return backupManifest{}, err
		}
		if overlap {
			return backupManifest{}, errors.New("backup output target must be disjoint from database, object storage, and migrations")
		}
	}
	if overlap, err := pathsOverlap(options.MasterKey, options.Output); err != nil {
		return backupManifest{}, err
	} else if overlap {
		return backupManifest{}, errors.New("master key must not be stored inside the data backup")
	}
	for _, protectedData := range []string{options.Objects, filepath.Dir(options.Database)} {
		if overlap, err := pathsOverlap(options.MasterKey, protectedData); err != nil {
			return backupManifest{}, err
		} else if overlap {
			return backupManifest{}, errors.New("master key must be independently stored outside database and object storage")
		}
	}
	masterKey, err := loadMasterKey(options.MasterKey)
	if err != nil {
		return backupManifest{}, err
	}
	defer clear(masterKey)
	runtimeLock, err := runtimeguard.AcquireExclusive(options.Database)
	if err != nil {
		return backupManifest{}, err
	}
	defer runtimeLock.Close()

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

	database, err := openDatabase(options.Database, false)
	if err != nil {
		return backupManifest{}, err
	}
	defer database.Close()
	if err := checkpoint(ctx, database); err != nil {
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
	databaseState, objectState, migrationHash, err := inspectDatabase(ctx, transaction, committedObjects, options.Migrations)
	if err != nil {
		return backupManifest{}, err
	}
	databaseDirectory := filepath.Join(staging, "database")
	if err := os.Mkdir(databaseDirectory, 0o700); err != nil {
		return backupManifest{}, err
	}
	databaseDestination := filepath.Join(databaseDirectory, "sbm.sqlite")
	if err := copyRegularFile(options.Database, databaseDestination); err != nil {
		return backupManifest{}, fmt.Errorf("copy database snapshot: %w", err)
	}
	databaseState.File, err = inspectFile(databaseDestination, "database/sbm.sqlite")
	if err != nil {
		return backupManifest{}, err
	}
	copiedObjects, err := copyTree(committedObjects, filepath.Join(staging, "objects"), "objects")
	if err != nil {
		return backupManifest{}, fmt.Errorf("copy committed objects: %w", err)
	}
	if !equalFileRecords(copiedObjects, objectState.Objects) {
		return backupManifest{}, errors.New("object store changed while the offline snapshot was copied")
	}
	if err := transaction.Commit(); err != nil {
		return backupManifest{}, fmt.Errorf("finish backup transaction: %w", err)
	}
	committed = true
	backupSetID, err := newBackupSetID()
	if err != nil {
		return backupManifest{}, err
	}
	manifest := backupManifest{
		ManifestKind:         manifestKind,
		ManifestVersion:      manifestVersion,
		BackupSetID:          backupSetID,
		CreatedAt:            time.Now().UTC().Format(time.RFC3339Nano),
		ApplicationOffline:   true,
		MigrationSetSHA256:   migrationHash,
		Database:             databaseState,
		Objects:              copiedObjects,
		DocumentCount:        databaseState.TableCounts["documents"],
		ObjectReferenceCount: objectState.ReferenceCount,
		UniqueObjectCount:    int64(len(copiedObjects)),
	}
	if err := writeAuthenticatedManifest(staging, manifest, masterKey); err != nil {
		return backupManifest{}, err
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
		return backupManifest{}, fmt.Errorf("database file: %w", err)
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
	if err := verifyDatabaseAndObjects(ctx, filepath.Join(root, "database", "sbm.sqlite"), filepath.Join(root, "objects"), migrations, manifest, manifest.Database.TableCounts, true); err != nil {
		return backupManifest{}, err
	}
	return manifest, nil
}

func restoreBackup(ctx context.Context, options restoreOptions) (backupManifest, int64, error) {
	if !options.Offline {
		return backupManifest{}, 0, errors.New("offline confirmation is required")
	}
	manifest, err := verifyBackup(ctx, verifyOptions{Backup: options.Backup, MasterKey: options.MasterKeySource, Migrations: options.Migrations})
	if err != nil {
		return backupManifest{}, 0, err
	}
	for _, target := range []struct {
		path  string
		label string
	}{{options.Database, "database"}, {options.Objects, "object store"}, {options.MasterKey, "master key"}} {
		if err := requireAbsent(target.path, target.label); err != nil {
			return backupManifest{}, 0, err
		}
	}
	for _, pair := range [][2]string{
		{options.Backup, options.Database},
		{options.Backup, options.Objects},
		{options.Backup, options.MasterKey},
		{options.Migrations, options.Database},
		{options.Migrations, options.Objects},
		{options.Migrations, options.MasterKey},
		{options.MasterKeySource, filepath.Dir(options.Database)},
		{options.MasterKeySource, options.Objects},
		{options.MasterKeySource, filepath.Dir(options.MasterKey)},
		{options.Database, options.Objects},
		{options.Objects, options.MasterKey},
		{filepath.Dir(options.Database), filepath.Dir(options.MasterKey)},
	} {
		overlap, err := pathsOverlap(pair[0], pair[1])
		if err != nil {
			return backupManifest{}, 0, err
		}
		if overlap {
			return backupManifest{}, 0, errors.New("backup and restore targets must be disjoint")
		}
	}
	databaseAbsolute, err := filepath.Abs(options.Database)
	if err != nil {
		return backupManifest{}, 0, err
	}
	for _, target := range []string{options.Objects, options.MasterKey} {
		targetAbsolute, err := filepath.Abs(target)
		if err != nil {
			return backupManifest{}, 0, err
		}
		if targetAbsolute == runtimeguard.LockPath(databaseAbsolute) || targetAbsolute == runtimeguard.ActivationPath(databaseAbsolute) {
			return backupManifest{}, 0, errors.New("restore targets must not use reserved runtime guard paths")
		}
	}
	runtimeLock, err := runtimeguard.AcquireExclusive(options.Database)
	if err != nil {
		return backupManifest{}, 0, err
	}
	defer runtimeLock.Close()

	databaseStage, err := os.MkdirTemp(filepath.Dir(options.Database), ".sbm-restore-database-")
	if err != nil {
		return backupManifest{}, 0, err
	}
	objectStage, err := os.MkdirTemp(filepath.Dir(options.Objects), ".sbm-restore-objects-")
	if err != nil {
		_ = os.RemoveAll(databaseStage)
		return backupManifest{}, 0, err
	}
	keyStage, err := os.MkdirTemp(filepath.Dir(options.MasterKey), ".sbm-restore-key-")
	if err != nil {
		_ = os.RemoveAll(databaseStage)
		_ = os.RemoveAll(objectStage)
		return backupManifest{}, 0, err
	}
	for _, stage := range []string{databaseStage, objectStage, keyStage} {
		if err := os.Chmod(stage, 0o700); err != nil {
			_ = os.RemoveAll(databaseStage)
			_ = os.RemoveAll(objectStage)
			_ = os.RemoveAll(keyStage)
			return backupManifest{}, 0, err
		}
	}
	defer os.RemoveAll(databaseStage)
	defer os.RemoveAll(objectStage)
	defer os.RemoveAll(keyStage)
	stagedDatabase := filepath.Join(databaseStage, "sbm.sqlite")
	stagedKey := filepath.Join(keyStage, "master-key")
	if err := copyRegularFile(filepath.Join(options.Backup, "database", "sbm.sqlite"), stagedDatabase); err != nil {
		return backupManifest{}, 0, err
	}
	if _, err := createObjectStoreFromPackage(filepath.Join(options.Backup, "objects"), objectStage); err != nil {
		return backupManifest{}, 0, err
	}
	if err := copyRegularFile(options.MasterKeySource, stagedKey); err != nil {
		return backupManifest{}, 0, err
	}
	if err := verifyRestoredSnapshot(ctx, options.Backup, stagedDatabase, objectStage, stagedKey, options.Migrations, manifest, manifest.Database.TableCounts, true); err != nil {
		return backupManifest{}, 0, fmt.Errorf("verify staged restore: %w", err)
	}
	if err := runtimeguard.CreateIncompleteRestoreState(options.Database); err != nil {
		return backupManifest{}, 0, err
	}
	publish := options.publish
	if publish == nil {
		publish = publishNoReplace
	}
	for _, item := range []struct{ source, destination, label string }{
		{stagedDatabase, options.Database, "database"},
		{objectStage, options.Objects, "object store"},
		{stagedKey, options.MasterKey, "master key"},
	} {
		if err := publish(item.source, item.destination); err != nil {
			return backupManifest{}, 0, fmt.Errorf("publish restored %s: %w", item.label, err)
		}
	}
	if err := verifyRestoredSnapshot(ctx, options.Backup, options.Database, options.Objects, options.MasterKey, options.Migrations, manifest, manifest.Database.TableCounts, true); err != nil {
		return backupManifest{}, 0, fmt.Errorf("verify published restore: %w", err)
	}
	invalidatedSessions, err := invalidateSessions(ctx, options.Database)
	if err != nil {
		return backupManifest{}, 0, err
	}
	if invalidatedSessions != manifest.Database.TableCounts["sessions"] {
		return backupManifest{}, 0, errors.New("restored session invalidation count differs from manifest")
	}
	expectedCounts := make(map[string]int64, len(manifest.Database.TableCounts))
	for table, count := range manifest.Database.TableCounts {
		expectedCounts[table] = count
	}
	expectedCounts["sessions"] = 0
	if err := verifyRestoredSnapshot(ctx, options.Backup, options.Database, options.Objects, options.MasterKey, options.Migrations, manifest, expectedCounts, false); err != nil {
		return backupManifest{}, 0, fmt.Errorf("verify activated restore: %w", err)
	}
	if err := runtimeguard.MarkRestoreComplete(options.Database); err != nil {
		return backupManifest{}, 0, err
	}
	return manifest, invalidatedSessions, nil
}

func verifyRestoredSnapshot(
	ctx context.Context,
	backupRoot, databasePath, objectStore, masterKeyPath, migrations string,
	manifest backupManifest,
	expectedCounts map[string]int64,
	verifyDatabaseFile bool,
) error {
	masterKey, err := loadMasterKey(masterKeyPath)
	if err != nil {
		return err
	}
	defer clear(masterKey)
	authenticatedManifest, err := readAuthenticatedManifest(backupRoot, masterKey)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(authenticatedManifest, manifest) {
		return errors.New("authenticated manifest identity changed during restore")
	}
	committedObjects, err := validateObjectStore(objectStore)
	if err != nil {
		return err
	}
	if verifyDatabaseFile {
		actual, err := inspectFile(databasePath, manifest.Database.File.Path)
		if err != nil {
			return err
		}
		if actual != manifest.Database.File {
			return errors.New("restored database file differs from manifest")
		}
	}
	actualObjects, err := listTree(committedObjects, "objects")
	if err != nil {
		return err
	}
	if !equalFileRecords(actualObjects, manifest.Objects) {
		return errors.New("restored object inventory differs from manifest")
	}
	return verifyDatabaseAndObjects(ctx, databasePath, committedObjects, migrations, manifest, expectedCounts, false)
}

func verifyDatabaseAndObjects(
	ctx context.Context,
	databasePath, committedObjects, migrations string,
	manifest backupManifest,
	expectedCounts map[string]int64,
	verifyFile bool,
) error {
	if verifyFile {
		actual, err := inspectFile(databasePath, manifest.Database.File.Path)
		if err != nil {
			return err
		}
		if actual != manifest.Database.File {
			return errors.New("database file differs from manifest")
		}
	}
	database, err := openDatabase(databasePath, true)
	if err != nil {
		return err
	}
	defer database.Close()
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	actualDatabase, actualObjects, migrationHash, inspectErr := inspectDatabase(ctx, transaction, committedObjects, migrations)
	rollbackErr := transaction.Rollback()
	if inspectErr != nil {
		return inspectErr
	}
	if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		return rollbackErr
	}
	if migrationHash != manifest.MigrationSetSHA256 || !databaseStateEqual(actualDatabase, manifest.Database, expectedCounts) ||
		actualObjects.ReferenceCount != manifest.ObjectReferenceCount || !equalFileRecords(actualObjects.Objects, manifest.Objects) {
		return errors.New("database or object references differ from backup manifest")
	}
	return nil
}
