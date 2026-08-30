package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

type Config struct {
	DatabasePath  string
	MigrationsDir string
}

type Store struct {
	db *sql.DB
}

var memoryDatabaseSequence uint64

func Open(ctx context.Context, config Config) (*Store, error) {
	if config.DatabasePath == "" {
		return nil, errors.New("database path is required")
	}
	if config.MigrationsDir == "" {
		return nil, errors.New("migrations directory is required")
	}
	dsn, err := sqliteDSN(config.DatabasePath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrate(ctx, db, config.MigrationsDir); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.db.PingContext(ctx)
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func sqliteDSN(databasePath string) (string, error) {
	if databasePath == ":memory:" {
		sequence := atomic.AddUint64(&memoryDatabaseSequence, 1)
		return fmt.Sprintf(
			"file:sbm-m1-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate",
			sequence,
		), nil
	}
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return "", fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return "", fmt.Errorf("create database directory: %w", err)
	}
	location := &url.URL{Scheme: "file", Path: absolute}
	query := location.Query()
	query.Set("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Set("_txlock", "immediate")
	location.RawQuery = query.Encode()
	return location.String(), nil
}
