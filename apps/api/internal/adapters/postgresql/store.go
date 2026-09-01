package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type Config struct {
	Host                string
	Port                uint16
	Database            string
	User                string
	PasswordFile        string
	SSLMode             string
	RootCertificateFile string
	MigrationsDir       string
	MaxOpenConnections  int
	RuntimeRole         string
}

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, config Config) (*Store, error) {
	db, err := openDatabase(config)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	if err := verifyMigrations(ctx, db, config.MigrationsDir); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func Migrate(ctx context.Context, config Config) error {
	db, err := openDatabase(config)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL for migration: %w", err)
	}
	return migrate(ctx, db, config.MigrationsDir, config.RuntimeRole)
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

// WithPGXConnection 为需要 PostgreSQL 批处理语义的离线工具提供受控原生连接。
// 回调期间连接保持独占，返回后立即归还 database/sql 连接池。
func (s *Store) WithPGXConnection(ctx context.Context, operation func(*pgx.Conn) error) error {
	connection, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	return connection.Raw(func(driverConnection any) error {
		rebinding, ok := driverConnection.(rebindingConnection)
		if !ok {
			return errors.New("PostgreSQL driver connection has an unexpected type")
		}
		native, ok := rebinding.inner.(*stdlib.Conn)
		if !ok {
			return errors.New("PostgreSQL native connection is unavailable")
		}
		return operation(native.Conn())
	})
}

func openDatabase(config Config) (*sql.DB, error) {
	if strings.TrimSpace(config.Host) == "" {
		return nil, errors.New("PostgreSQL host is required")
	}
	if config.Port == 0 {
		config.Port = 5432
	}
	if strings.TrimSpace(config.Database) == "" {
		return nil, errors.New("PostgreSQL database is required")
	}
	if strings.TrimSpace(config.User) == "" {
		return nil, errors.New("PostgreSQL user is required")
	}
	if strings.TrimSpace(config.PasswordFile) == "" {
		return nil, errors.New("PostgreSQL password file is required")
	}
	if strings.TrimSpace(config.MigrationsDir) == "" {
		return nil, errors.New("migrations directory is required")
	}
	if config.SSLMode == "" {
		config.SSLMode = "disable"
	}
	if config.SSLMode != "disable" && config.SSLMode != "verify-full" {
		return nil, errors.New("PostgreSQL ssl mode must be disable or verify-full")
	}
	if config.SSLMode == "verify-full" && strings.TrimSpace(config.RootCertificateFile) == "" {
		return nil, errors.New("PostgreSQL root certificate file is required for verify-full")
	}
	password, err := readPasswordFile(config.PasswordFile)
	if err != nil {
		return nil, err
	}
	defer clear(password)
	location := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))),
		Path:   config.Database,
		User:   url.User(config.User),
	}
	query := location.Query()
	query.Set("sslmode", config.SSLMode)
	query.Set("application_name", "smart-bill-manager")
	if config.RootCertificateFile != "" {
		query.Set("sslrootcert", config.RootCertificateFile)
	}
	location.RawQuery = query.Encode()
	connectionConfig, err := pgx.ParseConfig(location.String())
	if err != nil {
		return nil, errors.New("parse PostgreSQL connection configuration")
	}
	connectionConfig.Password = string(password)
	connectionConfig.RuntimeParams["timezone"] = "UTC"
	db := sql.OpenDB(rebindingConnector{inner: stdlib.GetConnector(*connectionConfig)})
	maxOpen := config.MaxOpenConnections
	if maxOpen <= 0 {
		maxOpen = 32
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(min(maxOpen, 8))
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, nil
}

func readPasswordFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect PostgreSQL password file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("PostgreSQL password file must be regular and accessible only by its owner")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return nil, errors.New("PostgreSQL password file must have exactly one hard link")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL password file: %w", err)
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, 1026))
	if err != nil {
		return nil, fmt.Errorf("read PostgreSQL password file: %w", err)
	}
	if len(value) > 1025 {
		clear(value)
		return nil, errors.New("PostgreSQL password file exceeds 1024 bytes")
	}
	if len(value) > 0 && value[len(value)-1] == '\n' {
		value = value[:len(value)-1]
	}
	if len(value) > 0 && value[len(value)-1] == '\r' {
		value = value[:len(value)-1]
	}
	if len(value) == 0 {
		return nil, errors.New("PostgreSQL password file is empty")
	}
	return value, nil
}
