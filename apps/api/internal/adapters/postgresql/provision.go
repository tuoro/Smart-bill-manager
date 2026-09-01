package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var postgresIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type ProvisionConfig struct {
	Admin           Config
	Database        string
	MigrationRole   string
	MigrationSecret string
	RuntimeRole     string
	RuntimeSecret   string
}

func Provision(ctx context.Context, config ProvisionConfig) error {
	for name, value := range map[string]string{
		"database": config.Database, "migration role": config.MigrationRole, "runtime role": config.RuntimeRole,
	} {
		if !postgresIdentifierPattern.MatchString(value) {
			return fmt.Errorf("PostgreSQL %s must be a lowercase safe identifier", name)
		}
	}
	if config.MigrationRole == config.RuntimeRole {
		return errors.New("PostgreSQL migration and runtime roles must be distinct")
	}
	migrationPassword, err := readPasswordFile(config.MigrationSecret)
	if err != nil {
		return fmt.Errorf("read migration role password: %w", err)
	}
	defer clear(migrationPassword)
	runtimePassword, err := readPasswordFile(config.RuntimeSecret)
	if err != nil {
		return fmt.Errorf("read runtime role password: %w", err)
	}
	defer clear(runtimePassword)

	database, err := openDatabase(config.Admin)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL for provisioning: %w", err)
	}
	if err := ensureLoginRole(ctx, database, config.MigrationRole, migrationPassword); err != nil {
		return err
	}
	if err := ensureLoginRole(ctx, database, config.RuntimeRole, runtimePassword); err != nil {
		return err
	}
	if err := ensureDatabase(ctx, database, config.Database, config.MigrationRole); err != nil {
		return err
	}
	databaseIdentifier := quoteIdentifier(config.Database)
	if _, err := database.ExecContext(ctx, "REVOKE CONNECT, TEMPORARY ON DATABASE "+databaseIdentifier+" FROM PUBLIC"); err != nil {
		return fmt.Errorf("revoke public database access: %w", err)
	}
	if _, err := database.ExecContext(ctx, "GRANT CONNECT ON DATABASE "+databaseIdentifier+" TO "+quoteIdentifier(config.RuntimeRole)); err != nil {
		return fmt.Errorf("grant runtime database access: %w", err)
	}
	return nil
}

func ensureLoginRole(ctx context.Context, database *sql.DB, role string, password []byte) error {
	var exists bool
	if err := database.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&exists); err != nil {
		return fmt.Errorf("inspect PostgreSQL role: %w", err)
	}
	statement := "CREATE ROLE "
	if exists {
		statement = "ALTER ROLE "
	}
	statement += quoteIdentifier(role) + " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD " + quoteLiteral(password)
	if _, err := database.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("provision PostgreSQL role: %w", err)
	}
	return nil
}

func ensureDatabase(ctx context.Context, connection *sql.DB, name, owner string) error {
	var persistedOwner string
	err := connection.QueryRowContext(ctx, `
		SELECT owner.rolname
		FROM pg_database database
		JOIN pg_roles owner ON owner.oid = database.datdba
		WHERE database.datname = $1
	`, name).Scan(&persistedOwner)
	if err == nil {
		if persistedOwner != owner {
			return errors.New("PostgreSQL database exists with an unexpected owner")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect PostgreSQL database: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(name)+" OWNER "+quoteIdentifier(owner)); err != nil {
		return fmt.Errorf("create PostgreSQL database: %w", err)
	}
	return nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value []byte) string {
	return `'` + strings.ReplaceAll(string(value), `'`, `''`) + `'`
}
