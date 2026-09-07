package postgresqladapter

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const migrationLockID int64 = 7_343_209_117

type migrationDescriptor struct {
	version int
	name    string
	sha256  string
	content []byte
}

func migrate(ctx context.Context, db *sql.DB, migrationsDir, runtimeRole string) error {
	if strings.TrimSpace(runtimeRole) == "" {
		return errors.New("PostgreSQL runtime role is required for migration grants")
	}
	migrations, err := loadMigrations(migrationsDir)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin PostgreSQL migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(?)", migrationLockID); err != nil {
		return fmt.Errorf("lock PostgreSQL migrations: %w", err)
	}
	if _, err := readRestoreState(ctx, tx, false); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			content_sha256 TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CHECK (length(content_sha256) = 64 AND content_sha256 ~ '^[0-9a-f]+$')
		)
	`); err != nil {
		return fmt.Errorf("create PostgreSQL migration ledger: %w", err)
	}
	applied, err := readAppliedMigrations(ctx, tx)
	if err != nil {
		return err
	}
	if err := validateAppliedMigrations(migrations, applied, true); err != nil {
		return err
	}
	for _, migration := range migrations[len(applied):] {
		if _, err := tx.ExecContext(ctx, string(migration.content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.name, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO schema_migrations (version, name, content_sha256) VALUES (?, ?, ?)",
			migration.version,
			migration.name,
			migration.sha256,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", migration.name, err)
		}
	}
	if err := applyRuntimePrivileges(ctx, tx, runtimeRole); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL migrations: %w", err)
	}
	return nil
}

func applyRuntimePrivileges(ctx context.Context, tx *sql.Tx, runtimeRole string) error {
	var sessionUser string
	if err := tx.QueryRowContext(ctx, "SELECT current_user").Scan(&sessionUser); err != nil {
		return fmt.Errorf("read PostgreSQL session user: %w", err)
	}
	// 单角色部署里迁移身份与运行身份相同。此时授予是多余的，而针对
	// schema_migrations 的 REVOKE 会撤销自己的写权限，使后续迁移无法记录版本。
	if sessionUser == runtimeRole {
		for _, statement := range []string{
			"REVOKE CREATE ON SCHEMA public FROM PUBLIC",
			"REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA public FROM PUBLIC",
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC",
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply PostgreSQL runtime privileges: %w", err)
			}
		}
		return nil
	}
	role := pgx.Identifier{runtimeRole}.Sanitize()
	statements := []string{
		"REVOKE CREATE ON SCHEMA public FROM PUBLIC",
		"GRANT USAGE ON SCHEMA public TO " + role,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + role,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + role,
		"REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA public FROM PUBLIC",
		"GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO " + role,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + role,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO " + role,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO " + role,
		"REVOKE INSERT, UPDATE, DELETE ON schema_migrations FROM " + role,
		"GRANT SELECT ON schema_migrations TO " + role,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply PostgreSQL runtime privileges: %w", err)
		}
	}
	return nil
}

func verifyMigrations(ctx context.Context, db *sql.DB, migrationsDir string) error {
	migrations, err := loadMigrations(migrationsDir)
	if err != nil {
		return err
	}
	var ledgerExists bool
	if err := db.QueryRowContext(
		ctx,
		"SELECT to_regclass('public.schema_migrations') IS NOT NULL",
	).Scan(&ledgerExists); err != nil {
		return fmt.Errorf("inspect PostgreSQL migration ledger: %w", err)
	}
	if !ledgerExists {
		return errors.New("PostgreSQL migration ledger is missing")
	}
	applied, err := readAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	return validateAppliedMigrations(migrations, applied, false)
}

type rowQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readAppliedMigrations(ctx context.Context, database rowQueryer) ([]migrationDescriptor, error) {
	rows, err := database.QueryContext(
		ctx,
		"SELECT version, name, content_sha256 FROM schema_migrations ORDER BY version",
	)
	if err != nil {
		return nil, fmt.Errorf("read PostgreSQL migration ledger: %w", err)
	}
	defer rows.Close()
	var migrations []migrationDescriptor
	for rows.Next() {
		var migration migrationDescriptor
		if err := rows.Scan(&migration.version, &migration.name, &migration.sha256); err != nil {
			return nil, fmt.Errorf("scan PostgreSQL migration ledger: %w", err)
		}
		migrations = append(migrations, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PostgreSQL migration ledger: %w", err)
	}
	return migrations, nil
}

func validateAppliedMigrations(
	expected []migrationDescriptor,
	applied []migrationDescriptor,
	allowPrefix bool,
) error {
	if len(applied) > len(expected) {
		return errors.New("PostgreSQL database has unknown migrations")
	}
	if !allowPrefix && len(applied) != len(expected) {
		return errors.New("PostgreSQL database migration set is incomplete")
	}
	for index, actual := range applied {
		want := expected[index]
		if actual.version != want.version || actual.name != want.name || actual.sha256 != want.sha256 {
			return fmt.Errorf("PostgreSQL migration identity mismatch at version %d", actual.version)
		}
	}
	return nil
}

func loadMigrations(migrationsDir string) ([]migrationDescriptor, error) {
	if strings.TrimSpace(migrationsDir) == "" {
		return nil, errors.New("migrations directory is required")
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if _, err := migrationVersion(entry.Name()); err != nil {
			return nil, err
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, errors.New("no migration files found")
	}
	migrations := make([]migrationDescriptor, 0, len(files))
	for index, name := range files {
		version, err := migrationVersion(name)
		if err != nil {
			return nil, err
		}
		if version != index+1 {
			return nil, fmt.Errorf("migration versions must be contiguous from 0001, got %q", name)
		}
		content, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		digest := sha256.Sum256(content)
		migrations = append(migrations, migrationDescriptor{
			version: version,
			name:    name,
			sha256:  hex.EncodeToString(digest[:]),
			content: content,
		})
	}
	return migrations, nil
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok || len(prefix) != 4 {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}
	return version, nil
}

// MigrationStatus 报告目标库相对镜像内迁移集合的状态，供启动流程判断
// 「全新库」「已就绪」「有未执行迁移」三种情形。
type MigrationStatus struct {
	LedgerMissing bool
	Applied       int
	Expected      int
}

// Pending 报告是否存在尚未执行的迁移。
func (s MigrationStatus) Pending() bool {
	return !s.LedgerMissing && s.Applied < s.Expected
}

// InspectMigrations 只读地检查迁移状态。已应用集合必须是镜像内集合的前缀，
// 否则返回错误——多出未知迁移或身份不匹配意味着镜像与数据库不配对，
// 此时不允许继续，也不允许自动修复。
func InspectMigrations(ctx context.Context, config Config) (MigrationStatus, error) {
	migrations, err := loadMigrations(config.MigrationsDir)
	if err != nil {
		return MigrationStatus{}, err
	}
	db, err := openDatabase(config)
	if err != nil {
		return MigrationStatus{}, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return MigrationStatus{}, fmt.Errorf("ping PostgreSQL for migration inspection: %w", err)
	}
	var ledgerExists bool
	if err := db.QueryRowContext(
		ctx, "SELECT to_regclass('public.schema_migrations') IS NOT NULL",
	).Scan(&ledgerExists); err != nil {
		return MigrationStatus{}, fmt.Errorf("inspect PostgreSQL migration ledger: %w", err)
	}
	if !ledgerExists {
		return MigrationStatus{LedgerMissing: true, Expected: len(migrations)}, nil
	}
	applied, err := readAppliedMigrations(ctx, db)
	if err != nil {
		return MigrationStatus{}, err
	}
	if err := validateAppliedMigrations(migrations, applied, true); err != nil {
		return MigrationStatus{}, err
	}
	return MigrationStatus{Applied: len(applied), Expected: len(migrations)}, nil
}
