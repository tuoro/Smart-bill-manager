package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/bootstrap"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type config struct {
	email        string
	displayName  string
	tenantName   string
	currency     string
	timezone     string
	passwordFile string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap-owner:", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := parseFlags()
	if err != nil {
		return err
	}
	databaseConfig, err := postgresqladapter.RuntimeConfigFromEnvironment()
	if err != nil {
		return err
	}
	password, err := readPassword(config.passwordFile)
	if err != nil {
		return err
	}
	defer clear(password)
	hasher, err := cryptography.NewPasswordHasher(cryptography.DefaultArgon2Params)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := postgresqladapter.Open(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.CheckObjectRoot(ctx, os.Getenv("SBM_OBJECTS_PATH")); err != nil {
		return err
	}
	service := bootstrap.NewService(store, hasher, system.IDGenerator{}, system.Clock{})
	_, err = service.Execute(ctx, bootstrap.Input{
		Email:           config.email,
		Password:        password,
		DisplayName:     config.displayName,
		TenantName:      config.tenantName,
		DefaultCurrency: domain.Currency(config.currency),
		Timezone:        config.timezone,
	})
	if err != nil {
		return err
	}
	fmt.Println("owner created")
	return nil
}

func parseFlags() (config, error) {
	var value config
	flag.StringVar(&value.email, "email", "", "owner login email")
	flag.StringVar(&value.displayName, "display-name", "", "owner display name")
	flag.StringVar(&value.tenantName, "tenant-name", "", "tenant name")
	flag.StringVar(&value.currency, "currency", "", "default currency: CNY, USD, EUR, or JPY")
	flag.StringVar(&value.timezone, "timezone", "", "IANA timezone")
	flag.StringVar(&value.passwordFile, "password-file", "", "optional mode-0600 password file")
	flag.Parse()
	if flag.NArg() != 0 {
		return config{}, errors.New("unexpected positional arguments")
	}
	required := map[string]string{
		"email":        value.email,
		"display-name": value.displayName,
		"tenant-name":  value.tenantName,
		"currency":     value.currency,
		"timezone":     value.timezone,
	}
	for name, entry := range required {
		if strings.TrimSpace(entry) == "" {
			return config{}, fmt.Errorf("-%s is required", name)
		}
	}
	return value, nil
}

func readPassword(path string) ([]byte, error) {
	if path == "" {
		fd := int(os.Stdin.Fd())
		if !term.IsTerminal(fd) {
			return nil, errors.New("password requires an interactive terminal or -password-file")
		}
		fmt.Fprint(os.Stderr, "Owner password: ")
		password, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return nil, fmt.Errorf("read password: %w", err)
		}
		return password, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect password file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("password file must be regular and accessible only by its owner")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open password file: %w", err)
	}
	defer file.Close()
	password, err := io.ReadAll(io.LimitReader(file, 1026))
	if err != nil {
		return nil, fmt.Errorf("read password file: %w", err)
	}
	if len(password) > 1025 {
		return nil, errors.New("password file exceeds 1024 bytes")
	}
	password = trimSingleLineEnding(password)
	return password, nil
}

func trimSingleLineEnding(value []byte) []byte {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		value = value[:len(value)-1]
		if len(value) > 0 && value[len(value)-1] == '\r' {
			value = value[:len(value)-1]
		}
	}
	return value
}
