package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
)

const (
	defaultPGDumpPath    = "/usr/local/bin/pg_dump"
	defaultPGRestorePath = "/usr/local/bin/pg_restore"
)

type postgresToolFiles struct {
	root        string
	serviceFile string
	passFile    string
}

func preparePostgresToolFiles(config postgresqladapter.Config) (postgresToolFiles, error) {
	root, err := os.MkdirTemp("", "sbm-postgresql-tools-")
	if err != nil {
		return postgresToolFiles{}, fmt.Errorf("create PostgreSQL tool configuration: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return postgresToolFiles{}, err
	}
	result := postgresToolFiles{
		root: root, serviceFile: filepath.Join(root, "pg_service.conf"), passFile: filepath.Join(root, "pgpass"),
	}
	password, err := readProtectedPassword(config.PasswordFile)
	if err != nil {
		_ = os.RemoveAll(root)
		return postgresToolFiles{}, err
	}
	defer clear(password)
	serviceValues := map[string]string{
		"host": config.Host, "port": fmt.Sprint(config.Port), "dbname": config.Database,
		"user": config.User, "sslmode": config.SSLMode,
	}
	if config.RootCertificateFile != "" {
		serviceValues["sslrootcert"] = config.RootCertificateFile
	}
	var service strings.Builder
	service.WriteString("[smart_bill_manager]\n")
	for _, name := range []string{"host", "port", "dbname", "user", "sslmode", "sslrootcert"} {
		value, present := serviceValues[name]
		if !present {
			continue
		}
		encoded, err := encodeServiceValue(value)
		if err != nil {
			_ = os.RemoveAll(root)
			return postgresToolFiles{}, err
		}
		service.WriteString(name + "=" + encoded + "\n")
	}
	passHost, err := encodePassValue(config.Host)
	if err != nil {
		_ = os.RemoveAll(root)
		return postgresToolFiles{}, err
	}
	passDatabase, err := encodePassValue(config.Database)
	if err != nil {
		_ = os.RemoveAll(root)
		return postgresToolFiles{}, err
	}
	passUser, err := encodePassValue(config.User)
	if err != nil {
		_ = os.RemoveAll(root)
		return postgresToolFiles{}, err
	}
	passPassword, err := encodePassValue(string(password))
	if err != nil {
		_ = os.RemoveAll(root)
		return postgresToolFiles{}, err
	}
	pass := []byte(fmt.Sprintf("%s:%d:%s:%s:%s\n", passHost, config.Port, passDatabase, passUser, passPassword))
	if err := writeExclusiveFile(result.serviceFile, []byte(service.String()), 0o600); err != nil {
		clear(pass)
		_ = os.RemoveAll(root)
		return postgresToolFiles{}, err
	}
	if err := writeExclusiveFile(result.passFile, pass, 0o600); err != nil {
		clear(pass)
		_ = os.RemoveAll(root)
		return postgresToolFiles{}, err
	}
	clear(pass)
	return result, nil
}

func (files postgresToolFiles) cleanup() {
	_ = os.RemoveAll(files.root)
}

func (files postgresToolFiles) environment() []string {
	return []string{
		"PATH=/nonexistent",
		"PGSERVICEFILE=" + files.serviceFile,
		"PGPASSFILE=" + files.passFile,
		"PGCONNECT_TIMEOUT=10",
	}
}

func createPostgresDump(ctx context.Context, config postgresqladapter.Config, destination string) error {
	tool := environmentOrDefault("SBM_PG_DUMP_PATH", defaultPGDumpPath)
	if err := requirePostgreSQL17Tool(ctx, tool, "pg_dump"); err != nil {
		return err
	}
	files, err := preparePostgresToolFiles(config)
	if err != nil {
		return err
	}
	defer files.cleanup()
	command := exec.CommandContext(ctx, tool,
		"--dbname=service=smart_bill_manager",
		"--format=custom",
		"--compress=none",
		"--no-owner",
		"--no-privileges",
		"--serializable-deferrable",
		"--lock-wait-timeout=5000",
		"--file="+destination,
	)
	command.Env = files.environment()
	if err := command.Run(); err != nil {
		return errors.New("PostgreSQL custom dump failed")
	}
	return nil
}

func verifyPostgresDump(ctx context.Context, location string) error {
	tool := environmentOrDefault("SBM_PG_RESTORE_PATH", defaultPGRestorePath)
	if err := requirePostgreSQL17Tool(ctx, tool, "pg_restore"); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, tool, "--list", location)
	command.Env = []string{"PATH=/nonexistent"}
	output, err := command.Output()
	if err != nil {
		return errors.New("PostgreSQL custom dump inventory is invalid")
	}
	if !bytes.Contains(output, []byte("TABLE DATA public documents")) ||
		!bytes.Contains(output, []byte("TABLE DATA public schema_migrations")) {
		return errors.New("PostgreSQL custom dump inventory is incomplete")
	}
	return nil
}

func restorePostgresDump(ctx context.Context, config postgresqladapter.Config, location string) error {
	tool := environmentOrDefault("SBM_PG_RESTORE_PATH", defaultPGRestorePath)
	if err := requirePostgreSQL17Tool(ctx, tool, "pg_restore"); err != nil {
		return err
	}
	files, err := preparePostgresToolFiles(config)
	if err != nil {
		return err
	}
	defer files.cleanup()
	command := exec.CommandContext(ctx, tool,
		"--dbname=service=smart_bill_manager",
		"--exit-on-error",
		"--single-transaction",
		"--no-owner",
		"--no-privileges",
		location,
	)
	command.Env = files.environment()
	if err := command.Run(); err != nil {
		return errors.New("PostgreSQL custom dump restore failed")
	}
	return nil
}

func requirePostgreSQL17Tool(ctx context.Context, path, expectedName string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s path must be absolute", expectedName)
	}
	command := exec.CommandContext(ctx, path, "--version")
	command.Env = []string{"PATH=/nonexistent"}
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("validate %s: unavailable", expectedName)
	}
	prefix := expectedName + " (PostgreSQL) 17."
	if !strings.HasPrefix(string(output), prefix) {
		return fmt.Errorf("%s must be PostgreSQL 17", expectedName)
	}
	return nil
}

func environmentOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func readProtectedPassword(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect PostgreSQL password file: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !ok || stat.Nlink != 1 {
		return nil, errors.New("PostgreSQL password file must be owner-only, regular, and single-linked")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL password file: %w", err)
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, 1026))
	if err != nil {
		return nil, errors.New("read PostgreSQL password file")
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

func encodeServiceValue(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("PostgreSQL service configuration contains an invalid value")
	}
	return value, nil
}

func encodePassValue(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("PostgreSQL passfile contains an invalid value")
	}
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ":", `\:`)
	return value, nil
}
