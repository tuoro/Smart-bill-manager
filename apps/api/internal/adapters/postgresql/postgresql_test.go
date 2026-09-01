package postgresqladapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

const testPostgreSQLConfigFileEnvironment = "SBM_TEST_POSTGRES_CONFIG_FILE"

type testPostgreSQLConfig struct {
	Host         string `json:"host"`
	Port         uint16 `json:"port"`
	AdminUser    string `json:"admin_user"`
	PasswordFile string `json:"password_file"`
}

func newTestDatabaseConfig(t testing.TB) Config {
	t.Helper()
	path := os.Getenv(testPostgreSQLConfigFileEnvironment)
	if path == "" {
		t.Skipf("%s is required for PostgreSQL integration tests", testPostgreSQLConfigFileEnvironment)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read PostgreSQL test configuration: %v", err)
	}
	var testConfig testPostgreSQLConfig
	if err := json.Unmarshal(content, &testConfig); err != nil {
		t.Fatalf("decode PostgreSQL test configuration: %v", err)
	}
	config := Config{
		Host:               testConfig.Host,
		Port:               testConfig.Port,
		Database:           "postgres",
		User:               testConfig.AdminUser,
		PasswordFile:       testConfig.PasswordFile,
		SSLMode:            "disable",
		MigrationsDir:      migrationsDir(t),
		MaxOpenConnections: 8,
		RuntimeRole:        testConfig.AdminUser,
	}
	admin, err := openDatabase(config)
	if err != nil {
		t.Fatalf("open PostgreSQL test administrator connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	ctx := context.Background()
	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL test administrator connection: %v", err)
	}
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
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = ? AND pid <> pg_backend_pid()
		`, databaseName)
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier); err != nil {
			t.Errorf("drop PostgreSQL test database: %v", err)
		}
	})
	config.Database = databaseName
	return config
}

func openEmptyTestDatabase(t testing.TB) (*Store, Config) {
	t.Helper()
	config := newTestDatabaseConfig(t)
	database, err := openDatabase(config)
	if err != nil {
		t.Fatalf("open empty PostgreSQL test database: %v", err)
	}
	store := &Store{db: database}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close PostgreSQL test database: %v", err)
		}
	})
	return store, config
}
