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
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"golang.org/x/sys/unix"
)

const totalFacts = 10_000
const confirmationReviews = 220
const seedTimeout = 10 * time.Minute

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed-performance:", safeSeedErrorCode(err))
		os.Exit(1)
	}
}

type seedOptions struct {
	outputPath string
}

func run() error {
	options, err := parseSeedArguments(os.Args[1:])
	if err != nil {
		return err
	}
	databaseConfig, err := postgresqladapter.RuntimeConfigFromEnvironment()
	if err != nil {
		return err
	}
	outputAbsolute, err := filepath.Abs(options.outputPath)
	if err != nil {
		return err
	}
	if err := requireRealSeedOutputParent(outputAbsolute); err != nil {
		return err
	}
	insideMigrations, err := pathContains(databaseConfig.MigrationsDir, outputAbsolute)
	if err != nil {
		return err
	}
	if insideMigrations {
		return errors.New("performance seed output must be disjoint from migrations")
	}
	resultFile, err := reserveSeedOutput(options.outputPath)
	if err != nil {
		return err
	}
	defer resultFile.Close()
	ctx, cancel := context.WithTimeout(context.Background(), seedTimeout)
	defer cancel()
	store, err := postgresqladapter.Open(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer store.Close()
	tenantID, userID, err := performanceOwner(ctx, store.DB())
	if err != nil {
		return err
	}
	var existing int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM payments) + (SELECT count(*) FROM invoices) + (SELECT count(*) FROM processing_jobs)
	`).Scan(&existing); err != nil {
		return fmt.Errorf("inspect performance database: %w", err)
	}
	if existing != 0 {
		return errors.New("performance seeding requires an owner-only database with no facts or jobs")
	}
	started := time.Now()
	providerID := makeID(0x01000001, 1)
	now := time.Date(2026, 8, 28, 2, 0, 0, 0, time.UTC)
	confirmationJobIDs := make([]string, 0, confirmationReviews)
	err = store.WithPGXConnection(ctx, func(connection *pgx.Conn) error {
		tx, err := connection.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin performance seed: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback(context.Background())
			}
		}()
		if _, err := tx.Exec(ctx, postgresqladapter.RebindQuery(`
			INSERT INTO provider_configs (
				id, tenant_id, base_url, encrypted_api_key, model, output_mode, capability_status,
				capability_checked_at, capability_safe_message,
				capability_schema_version, capability_schema_sha256, active, version,
				safe_fingerprint, created_by_user_id, created_at, updated_at
			) VALUES (?, ?, 'https://performance.invalid/v1', ?, 'synthetic-performance-model', 'json_schema',
			          'passed', ?, 'synthetic performance seed', 'bill-visible-text-provider/2', ?, TRUE, 1,
			          'synthetic-performance-fingerprint', ?, ?, ?)
		`), providerID, tenantID, []byte{0x01, 0x02, 0x03}, formatTime(now), strings.Repeat("c", 64), userID, formatTime(now), formatTime(now)); err != nil {
			return fmt.Errorf("insert performance provider: %w", err)
		}
		if err := seedPerformanceData(ctx, tx, tenantID, userID, providerID, now); err != nil {
			return err
		}
		for index := 0; index < confirmationReviews; index++ {
			confirmationJobIDs = append(confirmationJobIDs, makeID(0x20000001, totalFacts+index))
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit performance seed: %w", err)
		}
		committed = true
		return nil
	})
	if err != nil {
		return err
	}
	manifest := map[string]any{
		"seed_kind":                   "m4-performance-10k-facts",
		"payments":                    5_000,
		"invoices":                    5_000,
		"source_claim_chains":         totalFacts,
		"ready_confirmation_reviews":  confirmationReviews,
		"confirmation_job_ids":        confirmationJobIDs,
		"tenant_id":                   tenantID,
		"representative_document_id":  makeID(0x10000001, totalFacts-1),
		"representative_claim_set_id": makeID(0x40000001, totalFacts-1),
		"representative_job_id":       makeID(0x20000001, totalFacts-1),
		"seed_duration_ms":            time.Since(started).Milliseconds(),
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := resultFile.file.Truncate(0); err != nil {
		return fmt.Errorf("reset performance seed manifest: %w", err)
	}
	if err := writeSeedOutput(resultFile.file, encoded); err != nil {
		return fmt.Errorf("write performance seed manifest: %w", err)
	}
	if err := resultFile.file.Truncate(int64(len(encoded))); err != nil {
		return fmt.Errorf("truncate performance seed manifest: %w", err)
	}
	if err := resultFile.file.Sync(); err != nil {
		return fmt.Errorf("sync performance seed manifest: %w", err)
	}
	return resultFile.Close()
}

func parseSeedArguments(arguments []string) (seedOptions, error) {
	if len(arguments) != 2 {
		return seedOptions{}, errors.New("invalid performance seed arguments")
	}
	allowed := map[string]bool{"-output": true}
	seen := make(map[string]bool, len(allowed))
	for index := 0; index < len(arguments); index += 2 {
		name := arguments[index]
		if !allowed[name] || seen[name] || arguments[index+1] == "" {
			return seedOptions{}, errors.New("invalid performance seed arguments")
		}
		seen[name] = true
	}
	flags := flag.NewFlagSet("seed-performance", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options seedOptions
	flags.StringVar(&options.outputPath, "output", "", "new seed manifest path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return seedOptions{}, errors.New("invalid performance seed arguments")
	}
	return options, nil
}

func safeSeedErrorCode(err error) string {
	message := strings.ToLower(err.Error())
	for _, category := range []struct {
		contains string
		code     string
	}{
		{"argument", "invalid_arguments"},
		{"output", "output_reservation_failed"},
		{"symlink", "output_parent_invalid"},
		{"owner-only", "output_parent_invalid"},
		{"runtime lock", "runtime_lock_unavailable"},
		{"deadline exceeded", "seed_timeout"},
		{"requires an owner", "seed_state_invalid"},
		{"database", "database_operation_failed"},
		{"migration", "database_operation_failed"},
	} {
		if strings.Contains(message, category.contains) {
			return category.code
		}
	}
	return "seed_failed"
}

func pathContains(parent, candidate string) (bool, error) {
	parentAbsolute, err := filepath.Abs(parent)
	if err != nil {
		return false, err
	}
	candidateAbsolute, err := filepath.Abs(candidate)
	if err != nil {
		return false, err
	}
	relative, err := filepath.Rel(parentAbsolute, candidateAbsolute)
	if err != nil {
		return false, err
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))), nil
}

func requireRealSeedOutputParent(location string) error {
	if location == "/tmp" || !strings.HasPrefix(location, "/tmp"+string(filepath.Separator)) {
		return errors.New("performance seed output must be in /tmp")
	}
	parent := filepath.Dir(location)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve performance seed output parent: %w", err)
	}
	resolvedAbsolute, err := filepath.Abs(resolved)
	if err != nil {
		return err
	}
	if resolvedAbsolute != parent {
		return errors.New("performance seed output parent must not contain symlinks")
	}
	information, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	stat, ok := information.Sys().(*syscall.Stat_t)
	if !information.IsDir() || information.Mode()&os.ModeSymlink != 0 ||
		information.Mode().Perm()&0o077 != 0 || !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("performance seed output parent must be a real owner-only directory")
	}
	return nil
}

type seedOutput struct {
	file *os.File
}

func reserveSeedOutput(location string) (*seedOutput, error) {
	fd, err := unix.Open(location, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("reserve performance seed manifest: %w", err)
	}
	file := os.NewFile(uintptr(fd), location)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("reserve performance seed manifest: invalid file descriptor")
	}
	information, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, errors.New("reserve performance seed manifest: inspect output")
	}
	stat, ok := information.Sys().(*syscall.Stat_t)
	if !information.Mode().IsRegular() || information.Mode().Perm() != 0o600 ||
		!ok || stat.Nlink != 1 || int(stat.Uid) != os.Geteuid() {
		_ = file.Close()
		return nil, errors.New("reserve performance seed manifest: unsafe output")
	}
	result := &seedOutput{file: file}
	marker := []byte("{\"seed_kind\":\"m4-performance-seed-in-progress\"}\n")
	if err := writeSeedOutput(file, marker); err != nil {
		_ = result.Close()
		return nil, fmt.Errorf("reserve performance seed manifest: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = result.Close()
		return nil, fmt.Errorf("sync reserved performance seed manifest: %w", err)
	}
	return result, nil
}

func (s *seedOutput) Close() error {
	if s == nil || s.file == nil {
		return nil
	}
	file := s.file
	s.file = nil
	return file.Close()
}

func writeSeedOutput(file *os.File, content []byte) error {
	for offset := 0; offset < len(content); {
		written, err := file.WriteAt(content[offset:], int64(offset))
		if err != nil {
			return err
		}
		if written == 0 {
			return errors.New("performance seed manifest write made no progress")
		}
		offset += written
	}
	return nil
}

func performanceOwner(ctx context.Context, database *sql.DB) (string, string, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT tenant_id, user_id FROM memberships
		WHERE role = 'owner' AND status = 'active'
		ORDER BY tenant_id, user_id
	`)
	if err != nil {
		return "", "", fmt.Errorf("list performance owners: %w", err)
	}
	defer rows.Close()
	var tenantID, userID string
	count := 0
	for rows.Next() {
		if err := rows.Scan(&tenantID, &userID); err != nil {
			return "", "", err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	if count != 1 {
		return "", "", fmt.Errorf("performance database must have exactly one active owner, found %d", count)
	}
	return tenantID, userID, nil
}

type performanceFieldValue struct {
	path, valueType, presence string
	value, normalized         any
}

func fieldValue(path, valueType string, value, normalized any) performanceFieldValue {
	return performanceFieldValue{path: path, valueType: valueType, presence: "present", value: value, normalized: normalized}
}

func absentField(path, valueType string) performanceFieldValue {
	return performanceFieldValue{path: path, valueType: valueType, presence: "absent"}
}

func originIndex(path string) int {
	indexes := map[string]int{
		"amount_minor": 1, "currency": 2, "merchant": 3, "transaction_time": 4, "source_timezone": 5,
		"invoice_number": 6, "invoice_date": 7, "total_minor": 8, "seller_name": 9, "buyer_name": 10,
	}
	if index, exists := indexes[path]; exists {
		return index
	}
	for index, suffix := range []string{"name", "quantity", "unit", "unit_price_minor", "amount_minor", "sort_order"} {
		if strings.HasPrefix(path, "items[") && strings.HasSuffix(path, "]."+suffix) {
			return 16 + index
		}
	}
	panic("unsupported performance origin path: " + path)
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func makeID(prefix uint32, sequence int) string {
	return fmt.Sprintf("%08x-0000-4000-8000-%012x", prefix, sequence)
}

func hashString(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func syntheticPageFingerprint(value string) (string, string, [4]int) {
	digest := sha256.Sum256([]byte(value))
	dhash := binary.BigEndian.Uint64(digest[:8])
	ahash := binary.BigEndian.Uint64(digest[8:16])
	return fmt.Sprintf("%016x", dhash), fmt.Sprintf("%016x", ahash), [4]int{
		int((dhash >> 48) & 0xffff),
		int((dhash >> 32) & 0xffff),
		int((dhash >> 16) & 0xffff),
		int(dhash & 0xffff),
	}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
