package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const commandTimeout = 30 * time.Minute

type backupOptions struct {
	Objects    string
	MasterKey  string
	Migrations string
	Output     string
	Offline    bool
}

type verifyOptions struct {
	Backup     string
	MasterKey  string
	Migrations string
}

type restoreOptions struct {
	Backup          string
	MasterKeySource string
	Migrations      string
	Objects         string
	MasterKey       string
	Offline         bool
	publish         func(string, string) error
	checkpoint      func(string) error
}

type operationResult struct {
	Operation               string `json:"operation"`
	ManifestKind            string `json:"manifest_kind"`
	ManifestVersion         int    `json:"manifest_version"`
	BackupSetID             string `json:"backup_set_id"`
	DocumentCount           int64  `json:"document_count"`
	ObjectReferenceCount    int64  `json:"object_reference_count"`
	UniqueObjectCount       int64  `json:"unique_object_count"`
	DatabaseTableCount      int    `json:"database_table_count"`
	InvalidatedSessionCount int64  `json:"invalidated_session_count,omitempty"`
	OperationStartedAtMS    int64  `json:"operation_started_at_epoch_ms"`
	OperationFinishedAtMS   int64  `json:"operation_finished_at_epoch_ms"`
	ElapsedMilliseconds     int64  `json:"elapsed_ms"`
	Passed                  bool   `json:"passed"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "backup:", safeErrorCode(err))
		os.Exit(1)
	}
}

func safeErrorCode(err error) string {
	message := strings.ToLower(err.Error())
	for _, category := range []struct {
		contains string
		code     string
	}{
		{"offline", "offline_confirmation_required"},
		{"runtime lock", "runtime_lock_unavailable"},
		{"master key", "master_key_invalid_or_not_independent"},
		{"authentication", "manifest_authentication_failed"},
		{"manifest", "manifest_invalid"},
		{"migration", "migration_identity_mismatch"},
		{"schema", "schema_identity_mismatch"},
		{"backup staging", "backup_staging_failed"},
		{"backup package", "backup_package_failed"},
		{"backup dump", "postgresql_dump_failed"},
		{"publish data backup", "backup_publish_failed"},
		{"sync backup", "backup_sync_failed"},
		{"server identity", "postgresql_server_identity_failed"},
		{"constraints", "postgresql_constraint_validation_failed"},
		{"table inventory", "postgresql_table_inventory_failed"},
		{"audit identity", "postgresql_audit_identity_failed"},
		{"query postgresql object references", "postgresql_object_reference_query_failed"},
		{"scan postgresql object references", "postgresql_object_reference_scan_failed"},
		{"iterate postgresql object references", "postgresql_object_reference_iteration_failed"},
		{"committed object inventory", "object_inventory_invalid"},
		{"object references", "postgresql_object_reference_failed"},
		{"backup state", "postgresql_inspection_failed"},
		{"backup inspection", "postgresql_inspection_failed"},
		{"pg_dump", "postgresql_dump_failed"},
		{"pg_restore", "postgresql_restore_failed"},
		{"postgresql", "postgresql_operation_failed"},
		{"object", "object_inventory_invalid"},
		{"target", "target_not_empty_or_not_independent"},
		{"activation state", "restore_activation_failed"},
		{"session", "session_invalidation_failed"},
	} {
		if strings.Contains(message, category.contains) {
			return category.code
		}
	}
	return "operation_failed"
}

func run(arguments []string, output io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("subcommand backup, verify, or restore is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	started := time.Now()
	startedAtMilliseconds := started.UnixMilli()
	var manifest backupManifest
	var invalidatedSessions int64
	var err error
	switch arguments[0] {
	case "backup":
		var options backupOptions
		options, err = parseBackupOptions(arguments[1:])
		if err == nil {
			manifest, err = createBackup(ctx, options)
		}
	case "verify":
		var options verifyOptions
		options, err = parseVerifyOptions(arguments[1:])
		if err == nil {
			manifest, err = verifyBackup(ctx, options)
		}
	case "restore":
		var options restoreOptions
		options, err = parseRestoreOptions(arguments[1:])
		if err == nil {
			manifest, invalidatedSessions, err = restoreBackup(ctx, options)
		}
	default:
		return fmt.Errorf("unknown subcommand %q", arguments[0])
	}
	if err != nil {
		return err
	}
	finishedAtMilliseconds := time.Now().UnixMilli()
	result := operationResult{
		Operation:               arguments[0],
		ManifestKind:            manifest.ManifestKind,
		ManifestVersion:         manifest.ManifestVersion,
		BackupSetID:             manifest.BackupSetID,
		DocumentCount:           manifest.DocumentCount,
		ObjectReferenceCount:    manifest.ObjectReferenceCount,
		UniqueObjectCount:       manifest.UniqueObjectCount,
		DatabaseTableCount:      len(manifest.Database.TableCounts),
		InvalidatedSessionCount: invalidatedSessions,
		OperationStartedAtMS:    startedAtMilliseconds,
		OperationFinishedAtMS:   finishedAtMilliseconds,
		ElapsedMilliseconds:     time.Since(started).Milliseconds(),
		Passed:                  true,
	}
	return json.NewEncoder(output).Encode(result)
}

func parseBackupOptions(arguments []string) (backupOptions, error) {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options backupOptions
	flags.StringVar(&options.Objects, "objects", "", "offline object store root")
	flags.StringVar(&options.MasterKey, "master-key", "", "independently stored master key")
	flags.StringVar(&options.Migrations, "migrations", "", "current migration directory")
	flags.StringVar(&options.Output, "output", "", "new data backup directory")
	flags.BoolVar(&options.Offline, "offline-confirmed", false, "confirm local writers are stopped")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return backupOptions{}, errors.New("invalid backup arguments")
	}
	if options.Objects == "" || options.MasterKey == "" || options.Migrations == "" || options.Output == "" {
		return backupOptions{}, errors.New("-objects, -master-key, -migrations, and -output are required")
	}
	if !options.Offline {
		return backupOptions{}, errors.New("stop local writers and pass -offline-confirmed")
	}
	return options, nil
}

func parseVerifyOptions(arguments []string) (verifyOptions, error) {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options verifyOptions
	flags.StringVar(&options.Backup, "backup", "", "data backup directory")
	flags.StringVar(&options.MasterKey, "master-key", "", "independently stored master key")
	flags.StringVar(&options.Migrations, "migrations", "", "current migration directory")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return verifyOptions{}, errors.New("invalid verify arguments")
	}
	if options.Backup == "" || options.MasterKey == "" || options.Migrations == "" {
		return verifyOptions{}, errors.New("verify requires -backup, -master-key, and -migrations")
	}
	return options, nil
}

func parseRestoreOptions(arguments []string) (restoreOptions, error) {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options restoreOptions
	flags.StringVar(&options.Backup, "backup", "", "data backup directory")
	flags.StringVar(&options.MasterKeySource, "master-key-source", "", "independently stored source master key")
	flags.StringVar(&options.Migrations, "migrations", "", "current migration directory")
	flags.StringVar(&options.Objects, "objects", "", "new object store root")
	flags.StringVar(&options.MasterKey, "master-key", "", "new runtime master key path")
	flags.BoolVar(&options.Offline, "offline-confirmed", false, "confirm the target application is stopped")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return restoreOptions{}, errors.New("invalid restore arguments")
	}
	if options.Backup == "" || options.MasterKeySource == "" || options.Migrations == "" || options.Objects == "" || options.MasterKey == "" {
		return restoreOptions{}, errors.New("-backup, -master-key-source, -migrations, -objects, and -master-key are required")
	}
	if !options.Offline {
		return restoreOptions{}, errors.New("stop the target application and pass -offline-confirmed")
	}
	return options, nil
}
