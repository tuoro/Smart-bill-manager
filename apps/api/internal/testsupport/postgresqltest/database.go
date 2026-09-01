package postgresqltest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
)

const ConfigFileEnvironment = "SBM_TEST_POSTGRES_CONFIG_FILE"

type fileConfig struct {
	Host         string `json:"host"`
	Port         uint16 `json:"port"`
	AdminUser    string `json:"admin_user"`
	PasswordFile string `json:"password_file"`
}

func Open(t testing.TB) *postgresqladapter.Store {
	t.Helper()
	config := NewDatabase(t)
	if err := postgresqladapter.Migrate(context.Background(), config); err != nil {
		t.Fatalf("migrate PostgreSQL test database: %v", err)
	}
	store, err := postgresqladapter.Open(context.Background(), config)
	if err != nil {
		t.Fatalf("open PostgreSQL test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close PostgreSQL test store: %v", err)
		}
	})
	return store
}

func OpenEmptyDatabase(t testing.TB) *sql.DB {
	t.Helper()
	config := NewDatabase(t)
	database, err := openAdministratorDatabase(config)
	if err != nil {
		t.Fatalf("open empty PostgreSQL test database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close empty PostgreSQL test store: %v", err)
		}
	})
	return database
}

func NewDatabase(t testing.TB) postgresqladapter.Config {
	t.Helper()
	path := os.Getenv(ConfigFileEnvironment)
	if path == "" {
		t.Skipf("%s is required for PostgreSQL integration tests", ConfigFileEnvironment)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read PostgreSQL test configuration: %v", err)
	}
	var file fileConfig
	if err := json.Unmarshal(content, &file); err != nil {
		t.Fatalf("decode PostgreSQL test configuration: %v", err)
	}
	config := postgresqladapter.Config{
		Host: file.Host, Port: file.Port, Database: "postgres", User: file.AdminUser,
		PasswordFile: file.PasswordFile, SSLMode: "disable", MigrationsDir: migrationsDir(t),
		MaxOpenConnections: 8,
		RuntimeRole:        file.AdminUser,
	}
	admin, err := openAdministratorDatabase(config)
	if err != nil {
		t.Fatalf("open PostgreSQL test administrator connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	ctx := context.Background()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("create PostgreSQL test database name: %v", err)
	}
	databaseName := "sbm_test_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier); err != nil {
		t.Fatalf("create PostgreSQL test database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `
			SELECT pg_terminate_backend(pid) FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, databaseName)
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier); err != nil {
			t.Errorf("drop PostgreSQL test database: %v", err)
		}
	})
	config.Database = databaseName
	return config
}

func openAdministratorDatabase(config postgresqladapter.Config) (*sql.DB, error) {
	password, err := os.ReadFile(config.PasswordFile)
	if err != nil {
		return nil, err
	}
	defer clear(password)
	if len(password) > 0 && password[len(password)-1] == '\n' {
		password = password[:len(password)-1]
	}
	if len(password) > 0 && password[len(password)-1] == '\r' {
		password = password[:len(password)-1]
	}
	location := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))),
		Path:   config.Database,
		User:   url.User(config.User),
	}
	query := location.Query()
	query.Set("sslmode", config.SSLMode)
	location.RawQuery = query.Encode()
	connectionConfig, err := pgx.ParseConfig(location.String())
	if err != nil {
		return nil, err
	}
	connectionConfig.Password = string(password)
	database := sql.OpenDB(stdlib.GetConnector(*connectionConfig))
	database.SetMaxOpenConns(2)
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func migrationsDir(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../infra/migrations"))
}
