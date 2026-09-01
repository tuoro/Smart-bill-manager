package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate: PostgreSQL migration failed")
		os.Exit(1)
	}
}

func run() error {
	config, err := postgresqladapter.MigrationConfigFromEnvironment()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return postgresqladapter.Migrate(ctx, config)
}
