package postgresqladapter_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestRestoreRejectsNonTableObjectsInTarget(t *testing.T) {
	for name, statement := range map[string]string{
		"collation":   "CREATE COLLATION public.synthetic (provider=libc, locale='C')",
		"text_search": "CREATE TEXT SEARCH CONFIGURATION public.synthetic (COPY=pg_catalog.simple)",
		"function":    "CREATE FUNCTION public.synthetic() RETURNS integer LANGUAGE sql AS 'SELECT 1'",
		"type":        "CREATE TYPE public.synthetic AS ENUM ('synthetic')",
	} {
		t.Run(name, func(t *testing.T) {
			config := postgresqltest.NewDatabase(t)
			db := postgresqltest.InspectDatabase(t, config)
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if activation, err := postgres.BeginRestore(context.Background(), config); err == nil {
				activation.Close()
				t.Fatal("restore accepted nonempty target")
			}
			var stateExists bool
			if err := db.QueryRow("SELECT to_regnamespace('sbm_restore') IS NOT NULL").Scan(&stateExists); err != nil || stateExists {
				t.Fatal("rejected target was mutated")
			}
		})
	}
}

func TestRestoreBeginPersistsBarrierAndRejectsExistingTargets(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	activation, err := postgres.BeginRestore(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer activation.Close()
	if err := postgres.Migrate(ctx, config); err == nil {
		t.Fatal("migration opened incomplete restore")
	}
	if store, err := postgres.Open(ctx, config); err == nil {
		store.Close()
		t.Fatal("runtime opened incomplete restore")
	}
	if next, err := postgres.BeginRestore(ctx, config); err == nil {
		next.Close()
		t.Fatal("restore reused target")
	}
	if err := activation.Close(); err != nil {
		t.Fatal(err)
	}
	var phase string
	if err := postgresqltest.InspectDatabase(t, config).QueryRow("SELECT phase FROM sbm_restore.state").Scan(&phase); err != nil || phase != "incomplete" {
		t.Fatal("close cleared durable barrier")
	}
	ordinary := postgresqltest.NewDatabase(t)
	if err := postgres.Migrate(ctx, ordinary); err != nil {
		t.Fatal(err)
	}
	if next, err := postgres.BeginRestore(ctx, ordinary); err == nil {
		next.Close()
		t.Fatal("restore overwrote ordinary database")
	}
}

func TestRestoreRuntimeRoleCannotMutateActivation(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	admin := postgresqltest.InspectDatabase(t, config)
	role := "guard_" + config.Database
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := admin.Exec("CREATE ROLE " + quoted + " NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP OWNED BY " + quoted); err != nil {
			t.Error(err)
		}
		if _, err := admin.Exec("DROP ROLE " + quoted); err != nil {
			t.Error(err)
		}
	})
	config.RuntimeRole = role
	activation, err := postgres.BeginRestore(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer activation.Close()
	connection, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "SET ROLE "+quoted); err != nil {
		t.Fatal(err)
	}
	defer connection.ExecContext(ctx, "RESET ROLE")
	var phase string
	if err := connection.QueryRowContext(ctx, "SELECT phase FROM sbm_restore.state").Scan(&phase); err != nil || phase != "incomplete" {
		t.Fatal("runtime could not read activation")
	}
	for _, statement := range []string{
		"UPDATE sbm_restore.state SET phase='complete'",
		"DELETE FROM sbm_restore.state",
		"TRUNCATE sbm_restore.state",
		"DROP TABLE sbm_restore.state",
		"CREATE TABLE sbm_restore.bypass (id integer)",
	} {
		_, err := connection.ExecContext(ctx, statement)
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "42501" {
			t.Fatal("runtime activation mutation was not denied by PostgreSQL privileges")
		}
	}
}

func TestRestoreAndMigrationSerializeBeforeChoosingLifecycle(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var restoreErr, migrationErr error
	go func() {
		defer wg.Done()
		<-start
		var activation *postgres.RestoreActivation
		activation, restoreErr = postgres.BeginRestore(ctx, config)
		if activation != nil {
			activation.Close()
		}
	}()
	go func() { defer wg.Done(); <-start; migrationErr = postgres.Migrate(ctx, config) }()
	close(start)
	wg.Wait()
	if (restoreErr == nil) == (migrationErr == nil) {
		t.Fatal("restore and migration did not choose exactly one lifecycle")
	}
	store, err := postgres.Open(ctx, config)
	if restoreErr == nil && err == nil {
		store.Close()
		t.Fatal("restore race exposed incomplete database")
	}
	if migrationErr == nil {
		if err != nil {
			t.Fatal(err)
		}
		store.Close()
	}
}
