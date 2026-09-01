package postgresqladapter

import "testing"

func TestRuntimeAndMigrationConfigurationUseDistinctCredentialFiles(t *testing.T) {
	values := map[string]string{
		"SBM_POSTGRES_HOST":                    "postgres",
		"SBM_POSTGRES_PORT":                    "5432",
		"SBM_POSTGRES_DATABASE":                "smart_bill_manager",
		"SBM_POSTGRES_USER":                    "sbm_runtime",
		"SBM_POSTGRES_PASSWORD_FILE":           "/run/secrets/runtime-password",
		"SBM_POSTGRES_MIGRATION_USER":          "sbm_migration",
		"SBM_POSTGRES_MIGRATION_PASSWORD_FILE": "/run/secrets/migration-password",
		"SBM_POSTGRES_SSL_MODE":                "disable",
		"SBM_MIGRATIONS_DIR":                   "/app/migrations",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	runtimeConfig, err := RuntimeConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	migrationConfig, err := MigrationConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if runtimeConfig.User != "sbm_runtime" || migrationConfig.User != "sbm_migration" {
		t.Fatalf("credential roles were mixed: runtime=%q migration=%q", runtimeConfig.User, migrationConfig.User)
	}
	if runtimeConfig.PasswordFile == migrationConfig.PasswordFile {
		t.Fatal("runtime and migration password files unexpectedly match")
	}
}

func TestPostgreSQLEnvironmentConfigurationFailsClosed(t *testing.T) {
	t.Setenv("SBM_POSTGRES_HOST", "postgres")
	t.Setenv("SBM_POSTGRES_PORT", "0")
	if _, err := RuntimeConfigFromEnvironment(); err == nil {
		t.Fatal("invalid PostgreSQL port was accepted")
	}
	t.Setenv("SBM_POSTGRES_PORT", "5432")
	t.Setenv("SBM_POSTGRES_DATABASE", "smart_bill_manager")
	t.Setenv("SBM_POSTGRES_USER", "sbm_runtime")
	t.Setenv("SBM_POSTGRES_PASSWORD_FILE", "/run/secrets/runtime-password")
	t.Setenv("SBM_POSTGRES_SSL_MODE", "verify-full")
	t.Setenv("SBM_MIGRATIONS_DIR", "/app/migrations")
	if _, err := RuntimeConfigFromEnvironment(); err == nil {
		t.Fatal("verify-full without a root certificate was accepted")
	}
}
