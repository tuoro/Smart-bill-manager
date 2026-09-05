package postgresqladapter

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/restorestate"
)

const RestoreSchema = "sbm_restore"

type activationQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readRestoreState(ctx context.Context, db activationQuery, allowIncomplete bool) (restorestate.Identity, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT to_regnamespace('sbm_restore') IS NOT NULL").Scan(&exists); err != nil {
		return restorestate.Identity{}, restorestate.ErrNotReady
	}
	if !exists {
		return restorestate.Identity{}, nil
	}
	var identity restorestate.Identity
	var phase string
	var count, actualOID int64
	var actualName string
	err := db.QueryRowContext(ctx, `
		SELECT format_version, restore_id, database_oid, database_name, phase,
		       (SELECT count(*) FROM sbm_restore.state),
		       (SELECT oid::bigint FROM pg_database WHERE datname=current_database()), current_database()
		FROM sbm_restore.state WHERE singleton=1
	`).Scan(&identity.Version, &identity.RestoreID, &identity.DatabaseOID, &identity.DatabaseName, &phase, &count, &actualOID, &actualName)
	if err != nil || count != 1 || !identity.Valid() || identity.DatabaseOID != actualOID || identity.DatabaseName != actualName {
		return restorestate.Identity{}, restorestate.ErrNotReady
	}
	if phase != "complete" && !(allowIncomplete && phase == "incomplete") {
		return restorestate.Identity{}, restorestate.ErrNotReady
	}
	return identity, nil
}

// RestoreActivation 只能由 BeginRestore 创建；普通连接没有未激活访问开关。
type RestoreActivation struct {
	db       *sql.DB
	config   Config
	identity restorestate.Identity
}

func BeginRestore(ctx context.Context, config Config) (*RestoreActivation, error) {
	db, err := openDatabase(config)
	if err != nil {
		return nil, err
	}
	activation, err := beginRestore(ctx, db, config)
	if err != nil {
		db.Close()
		return nil, err
	}
	return activation, nil
}

func beginRestore(ctx context.Context, db *sql.DB, config Config) (*RestoreActivation, error) {
	if config.RuntimeRole == "" {
		return nil, errors.New("restore runtime role is required")
	}
	// READ COMMITTED 在取得迁移锁后读最新对象，不复用等待锁前的旧快照。
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(?)", migrationLockID); err != nil {
		return nil, err
	}
	var occupied bool
	if err := tx.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM pg_namespace WHERE nspname NOT IN ('public','information_schema') AND nspname NOT LIKE 'pg_%') OR
		EXISTS(SELECT 1 FROM pg_depend WHERE refclassid='pg_namespace'::regclass AND refobjid='public'::regnamespace)
	`).Scan(&occupied); err != nil {
		return nil, err
	}
	if occupied {
		return nil, errors.New("restore target PostgreSQL database is not empty")
	}
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return nil, err
	}
	identity := restorestate.Identity{Version: 1, RestoreID: hex.EncodeToString(identifier)}
	if err := tx.QueryRowContext(ctx, "SELECT oid::bigint, datname FROM pg_database WHERE datname=current_database()").Scan(&identity.DatabaseOID, &identity.DatabaseName); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE SCHEMA sbm_restore;
		REVOKE ALL ON SCHEMA sbm_restore FROM PUBLIC;
		CREATE TABLE sbm_restore.state (
		 singleton integer PRIMARY KEY CHECK(singleton=1),
		 format_version integer NOT NULL CHECK(format_version=1),
		 restore_id text NOT NULL CHECK(restore_id ~ '^[0-9a-f]{32}$'),
		 database_oid bigint NOT NULL CHECK(database_oid>0), database_name text NOT NULL,
		 phase text NOT NULL CHECK(phase IN ('incomplete','complete'))
		);
		REVOKE ALL ON sbm_restore.state FROM PUBLIC;
	`); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO sbm_restore.state VALUES (1,1,?,?,?,'incomplete')", identity.RestoreID, identity.DatabaseOID, identity.DatabaseName); err != nil {
		return nil, err
	}
	role := pgx.Identifier{config.RuntimeRole}.Sanitize()
	for _, statement := range []string{"GRANT USAGE ON SCHEMA sbm_restore TO " + role, "GRANT SELECT ON sbm_restore.state TO " + role} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RestoreActivation{db: db, config: config, identity: identity}, nil
}

func (activation *RestoreActivation) Close() error                    { return activation.db.Close() }
func (activation *RestoreActivation) DB() *sql.DB                     { return activation.db }
func (activation *RestoreActivation) Identity() restorestate.Identity { return activation.identity }

func (activation *RestoreActivation) VerifyMigrations(ctx context.Context) error {
	if err := verifyMigrations(ctx, activation.db, activation.config.MigrationsDir); err != nil {
		return err
	}
	tx, err := activation.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	identity, err := readRestoreState(ctx, tx, true)
	if err != nil || identity != activation.identity {
		return restorestate.ErrNotReady
	}
	if err := applyRuntimePrivileges(ctx, tx, activation.config.RuntimeRole); err != nil {
		return err
	}
	return tx.Commit()
}

// Complete 是跨资源发布的最后一步；调用方必须先完成文件同步及后检查。
func (activation *RestoreActivation) Complete(ctx context.Context, objects string) error {
	if err := restorestate.CheckObjects(objects, activation.identity); err != nil {
		return err
	}
	tx, err := activation.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	identity, err := readRestoreState(ctx, tx, true)
	if err != nil || identity != activation.identity {
		return restorestate.ErrNotReady
	}
	result, err := tx.ExecContext(ctx, "UPDATE sbm_restore.state SET phase='complete' WHERE singleton=1 AND restore_id=? AND phase='incomplete'", identity.RestoreID)
	if err != nil {
		return restorestate.ErrNotReady
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return restorestate.ErrNotReady
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit restore activation: %w", err)
	}
	return nil
}

func (s *Store) CheckObjectRoot(ctx context.Context, root string) error {
	identity, err := readRestoreState(ctx, s.db, false)
	if err != nil {
		return err
	}
	return restorestate.CheckObjects(root, identity)
}
