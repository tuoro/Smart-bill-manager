package main

import (
	"context"
	"fmt"
	"os"
	"time"

	postgresqladapter "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "provision-postgresql: database provisioning failed")
		os.Exit(1)
	}
}

func run() error {
	config, err := postgresqladapter.ProvisionConfigFromEnvironment()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return postgresqladapter.Provision(ctx, config)
}
