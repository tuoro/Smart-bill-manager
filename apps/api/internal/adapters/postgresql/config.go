package postgresqladapter

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	runtimeUserVariable         = "SBM_POSTGRES_USER"
	runtimePasswordFileVariable = "SBM_POSTGRES_PASSWORD_FILE"
	migrationUserVariable       = "SBM_POSTGRES_MIGRATION_USER"
	migrationPasswordVariable   = "SBM_POSTGRES_MIGRATION_PASSWORD_FILE"
)

func RuntimeConfigFromEnvironment() (Config, error) {
	return configFromEnvironment(runtimeUserVariable, runtimePasswordFileVariable)
}

func MigrationConfigFromEnvironment() (Config, error) {
	config, err := configFromEnvironment(migrationUserVariable, migrationPasswordVariable)
	if err != nil {
		return Config{}, err
	}
	config.RuntimeRole = os.Getenv(runtimeUserVariable)
	if strings.TrimSpace(config.RuntimeRole) == "" {
		return Config{}, errors.New("SBM_POSTGRES_USER is required for migration grants")
	}
	return config, nil
}

func ProvisionConfigFromEnvironment() (ProvisionConfig, error) {
	admin, err := configFromEnvironment("SBM_POSTGRES_ADMIN_USER", "SBM_POSTGRES_ADMIN_PASSWORD_FILE")
	if err != nil {
		return ProvisionConfig{}, err
	}
	admin.Database = os.Getenv("SBM_POSTGRES_ADMIN_DATABASE")
	if strings.TrimSpace(admin.Database) == "" {
		return ProvisionConfig{}, errors.New("SBM_POSTGRES_ADMIN_DATABASE is required")
	}
	result := ProvisionConfig{
		Admin:           admin,
		Database:        os.Getenv("SBM_POSTGRES_DATABASE"),
		MigrationRole:   os.Getenv(migrationUserVariable),
		MigrationSecret: os.Getenv(migrationPasswordVariable),
		RuntimeRole:     os.Getenv(runtimeUserVariable),
		RuntimeSecret:   os.Getenv(runtimePasswordFileVariable),
	}
	for name, value := range map[string]string{
		"SBM_POSTGRES_DATABASE":     result.Database,
		migrationUserVariable:       result.MigrationRole,
		migrationPasswordVariable:   result.MigrationSecret,
		runtimeUserVariable:         result.RuntimeRole,
		runtimePasswordFileVariable: result.RuntimeSecret,
	} {
		if strings.TrimSpace(value) == "" {
			return ProvisionConfig{}, fmt.Errorf("%s is required", name)
		}
	}
	return result, nil
}

func RestoreConfigFromEnvironment() (Config, error) {
	port, err := parseBoundedEnvironmentInteger("SBM_POSTGRES_RESTORE_PORT", 1, 65_535)
	if err != nil {
		return Config{}, err
	}
	config := Config{
		Host:                os.Getenv("SBM_POSTGRES_RESTORE_HOST"),
		Port:                uint16(port),
		Database:            os.Getenv("SBM_POSTGRES_RESTORE_DATABASE"),
		User:                os.Getenv("SBM_POSTGRES_RESTORE_USER"),
		PasswordFile:        os.Getenv("SBM_POSTGRES_RESTORE_PASSWORD_FILE"),
		SSLMode:             os.Getenv("SBM_POSTGRES_RESTORE_SSL_MODE"),
		RootCertificateFile: os.Getenv("SBM_POSTGRES_RESTORE_ROOT_CERTIFICATE_FILE"),
		MigrationsDir:       os.Getenv("SBM_MIGRATIONS_DIR"),
		MaxOpenConnections:  8,
		RuntimeRole:         os.Getenv("SBM_POSTGRES_RESTORE_RUNTIME_USER"),
	}
	for name, value := range map[string]string{
		"SBM_POSTGRES_RESTORE_HOST":          config.Host,
		"SBM_POSTGRES_RESTORE_DATABASE":      config.Database,
		"SBM_POSTGRES_RESTORE_USER":          config.User,
		"SBM_POSTGRES_RESTORE_PASSWORD_FILE": config.PasswordFile,
		"SBM_POSTGRES_RESTORE_SSL_MODE":      config.SSLMode,
		"SBM_POSTGRES_RESTORE_RUNTIME_USER":  config.RuntimeRole,
		"SBM_MIGRATIONS_DIR":                 config.MigrationsDir,
	} {
		if strings.TrimSpace(value) == "" {
			return Config{}, fmt.Errorf("%s is required", name)
		}
	}
	if config.SSLMode != "disable" && config.SSLMode != "verify-full" {
		return Config{}, errors.New("SBM_POSTGRES_RESTORE_SSL_MODE must be disable or verify-full")
	}
	if config.SSLMode == "verify-full" && strings.TrimSpace(config.RootCertificateFile) == "" {
		return Config{}, errors.New("SBM_POSTGRES_RESTORE_ROOT_CERTIFICATE_FILE is required for verify-full")
	}
	return config, nil
}

func configFromEnvironment(userVariable, passwordFileVariable string) (Config, error) {
	port, err := parseBoundedEnvironmentInteger("SBM_POSTGRES_PORT", 1, 65_535)
	if err != nil {
		return Config{}, err
	}
	maxConnections := 32
	if strings.TrimSpace(os.Getenv("SBM_POSTGRES_MAX_OPEN_CONNECTIONS")) != "" {
		maxConnections, err = parseBoundedEnvironmentInteger("SBM_POSTGRES_MAX_OPEN_CONNECTIONS", 1, 256)
		if err != nil {
			return Config{}, err
		}
	}
	config := Config{
		Host:                os.Getenv("SBM_POSTGRES_HOST"),
		Port:                uint16(port),
		Database:            os.Getenv("SBM_POSTGRES_DATABASE"),
		User:                os.Getenv(userVariable),
		PasswordFile:        os.Getenv(passwordFileVariable),
		SSLMode:             os.Getenv("SBM_POSTGRES_SSL_MODE"),
		RootCertificateFile: os.Getenv("SBM_POSTGRES_ROOT_CERTIFICATE_FILE"),
		MigrationsDir:       os.Getenv("SBM_MIGRATIONS_DIR"),
		MaxOpenConnections:  maxConnections,
	}
	for name, value := range map[string]string{
		"SBM_POSTGRES_HOST":     config.Host,
		"SBM_POSTGRES_DATABASE": config.Database,
		userVariable:            config.User,
		passwordFileVariable:    config.PasswordFile,
		"SBM_POSTGRES_SSL_MODE": config.SSLMode,
		"SBM_MIGRATIONS_DIR":    config.MigrationsDir,
	} {
		if strings.TrimSpace(value) == "" {
			return Config{}, fmt.Errorf("%s is required", name)
		}
	}
	if config.SSLMode != "disable" && config.SSLMode != "verify-full" {
		return Config{}, errors.New("SBM_POSTGRES_SSL_MODE must be disable or verify-full")
	}
	if config.SSLMode == "verify-full" && strings.TrimSpace(config.RootCertificateFile) == "" {
		return Config{}, errors.New("SBM_POSTGRES_ROOT_CERTIFICATE_FILE is required for verify-full")
	}
	return config, nil
}

func parseBoundedEnvironmentInteger(name string, minimum, maximum int) (int, error) {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return value, nil
}
