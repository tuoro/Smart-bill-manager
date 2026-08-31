package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const exerciseTimeout = 10 * time.Minute

type archiveOptions struct {
	Database, Migrations, Objects, PDFInfo string
	TenantID, SourceID, ExerciseID, Output string
}

type snapshotOptions struct {
	Database, Objects, ProcessingJobID, ConfirmedFactID, ExerciseID, Output string
	ExpectedDocuments                                                       int64
}

type verifyOptions struct {
	Database, Snapshot, RecoveredFactID, ExerciseID, BackupSetID, Output string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "recovery-exercise:", safeRecoveryErrorCode(err))
		os.Exit(1)
	}
}

func safeRecoveryErrorCode(err error) string {
	message := strings.ToLower(err.Error())
	for _, category := range []struct {
		contains string
		code     string
	}{
		{"argument", "invalid_arguments"},
		{"runtime lock", "runtime_lock_unavailable"},
		{"snapshot", "protected_snapshot_invalid"},
		{"object", "object_inventory_invalid"},
		{"document", "document_shape_invalid"},
		{"job", "processing_shape_invalid"},
		{"ai run", "ai_run_shape_invalid"},
		{"session", "session_shape_invalid"},
		{"fact", "fact_shape_invalid"},
		{"email", "email_fixture_invalid"},
		{"output", "protected_output_failed"},
	} {
		if strings.Contains(message, category.contains) {
			return category.code
		}
	}
	return "operation_failed"
}

func run(arguments []string, output io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("subcommand archive-email, snapshot, or verify is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), exerciseTimeout)
	defer cancel()
	switch arguments[0] {
	case "archive-email":
		options, err := parseArchiveOptions(arguments[1:])
		if err != nil {
			return err
		}
		resultFile, err := reserveResult(options.Output)
		if err != nil {
			return err
		}
		defer resultFile.Close()
		result, err := archiveSyntheticEmail(ctx, options)
		if err != nil {
			return err
		}
		return writeResult(resultFile, output, result)
	case "snapshot":
		options, err := parseSnapshotOptions(arguments[1:])
		if err != nil {
			return err
		}
		resultFile, err := reserveResult(options.Output)
		if err != nil {
			return err
		}
		defer resultFile.Close()
		result, err := captureRecoverySnapshot(ctx, options)
		if err != nil {
			return err
		}
		return writeResult(resultFile, output, result)
	case "verify":
		options, err := parseVerifyOptions(arguments[1:])
		if err != nil {
			return err
		}
		resultFile, err := reserveResult(options.Output)
		if err != nil {
			return err
		}
		defer resultFile.Close()
		result, err := verifyRecoveryState(ctx, options)
		if err != nil {
			return err
		}
		return writeResult(resultFile, output, result)
	default:
		return fmt.Errorf("unknown subcommand %q", arguments[0])
	}
}

func parseArchiveOptions(arguments []string) (archiveOptions, error) {
	flags := flag.NewFlagSet("archive-email", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options archiveOptions
	flags.StringVar(&options.Database, "database", "", "offline SQLite database")
	flags.StringVar(&options.Migrations, "migrations", "", "current migrations")
	flags.StringVar(&options.Objects, "objects", "", "offline object store")
	flags.StringVar(&options.PDFInfo, "pdfinfo", "", "pdfinfo executable path")
	flags.StringVar(&options.TenantID, "tenant-id", "", "synthetic tenant id")
	flags.StringVar(&options.SourceID, "source-id", "", "registered synthetic email source id")
	flags.StringVar(&options.ExerciseID, "exercise-id", "", "opaque recovery exercise identity")
	flags.StringVar(&options.Output, "output", "", "new protected result JSON")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return archiveOptions{}, errors.New("invalid archive-email arguments")
	}
	if options.Database == "" || options.Migrations == "" || options.Objects == "" || options.PDFInfo == "" ||
		options.TenantID == "" || options.SourceID == "" || options.ExerciseID == "" || options.Output == "" {
		return archiveOptions{}, errors.New("archive-email requires -database, -migrations, -objects, -pdfinfo, -tenant-id, -source-id, -exercise-id, and -output")
	}
	return options, nil
}

func parseSnapshotOptions(arguments []string) (snapshotOptions, error) {
	flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options snapshotOptions
	flags.StringVar(&options.Database, "database", "", "offline SQLite database")
	flags.StringVar(&options.Objects, "objects", "", "offline object store")
	flags.StringVar(&options.ProcessingJobID, "processing-job-id", "", "job held at backup")
	flags.StringVar(&options.ConfirmedFactID, "confirmed-fact-id", "", "fact confirmed before backup")
	flags.StringVar(&options.ExerciseID, "exercise-id", "", "opaque recovery exercise identity")
	flags.StringVar(&options.Output, "output", "", "new protected snapshot JSON")
	flags.Int64Var(&options.ExpectedDocuments, "expected-documents", 0, "exact expected document count")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return snapshotOptions{}, errors.New("invalid snapshot arguments")
	}
	if options.Database == "" || options.Objects == "" || options.ProcessingJobID == "" || options.ConfirmedFactID == "" || options.ExerciseID == "" || options.Output == "" || options.ExpectedDocuments < 1 {
		return snapshotOptions{}, errors.New("snapshot requires -database, -objects, -processing-job-id, -confirmed-fact-id, -exercise-id, -expected-documents, and -output")
	}
	return options, nil
}

func parseVerifyOptions(arguments []string) (verifyOptions, error) {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options verifyOptions
	flags.StringVar(&options.Database, "database", "", "offline restored SQLite database")
	flags.StringVar(&options.Snapshot, "snapshot", "", "protected pre-backup snapshot JSON")
	flags.StringVar(&options.RecoveredFactID, "recovered-fact-id", "", "fact created after recovery")
	flags.StringVar(&options.ExerciseID, "exercise-id", "", "opaque recovery exercise identity")
	flags.StringVar(&options.BackupSetID, "backup-set-id", "", "authenticated backup set identity")
	flags.StringVar(&options.Output, "output", "", "new protected verification JSON")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return verifyOptions{}, errors.New("invalid verify arguments")
	}
	if options.Database == "" || options.Snapshot == "" || options.RecoveredFactID == "" || options.ExerciseID == "" || options.BackupSetID == "" || options.Output == "" {
		return verifyOptions{}, errors.New("verify requires -database, -snapshot, -recovered-fact-id, -exercise-id, -backup-set-id, and -output")
	}
	return options, nil
}
