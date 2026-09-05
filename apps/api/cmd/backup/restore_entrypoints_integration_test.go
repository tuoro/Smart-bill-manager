//go:build postgresql_tools

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/restorestate"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

// 运行实际编译入口，避免只测试连接帮助函数而漏掉启动调用链。
func TestRestoreEntrypointsRejectIncompleteAndUnpairedEmptyIdentity(t *testing.T) {
	backup, key, _ := activationBackupFixture(t, false)
	for _, phase := range []string{"database_restored", "before_activation", "missing_identity"} {
		t.Run(phase, func(t *testing.T) {
			target := postgresqltest.NewDatabase(t)
			setTripBackupEnvironment(t, target, true)
			root := t.TempDir()
			objects := filepath.Join(root, "objects")
			options := restoreOptions{Backup: backup, MasterKeySource: key, Migrations: target.MigrationsDir, Objects: objects, MasterKey: filepath.Join(root, "key"), Offline: true}
			sentinel := errors.New("synthetic-interruption")
			options.checkpoint = func(at string) error {
				if at == phase {
					return sentinel
				}
				return nil
			}
			_, _, err := restoreBackup(context.Background(), options)
			if phase == "missing_identity" {
				if err != nil {
					t.Fatal(err)
				}
				objects = filepath.Join(root, "absent-objects")
			} else if !errors.Is(err, sentinel) {
				t.Fatal("expected restore interruption")
			}
			configureEntrypoints(t, target, objects)
			for _, entry := range []string{"server", "bootstrap-owner", "recover-account"} {
				t.Run(entry, func(t *testing.T) {
					output, err := runEntrypoint(t, entry)
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 1 {
						t.Fatal("entrypoint did not reject restore before startup")
					}
					want := restorestate.ErrNotReady.Error()
					if entry == "recover-account" {
						want = "recover-account: operation_failed"
					}
					if !bytes.Contains(output, []byte(want)) {
						t.Fatal("entrypoint failed outside the restore guard")
					}
				})
			}
			var users int
			if err := postgresqltest.InspectDatabase(t, target).QueryRow("SELECT count(*) FROM users").Scan(&users); err != nil || users != 0 {
				t.Fatal("bootstrap wrote to rejected restore")
			}
			if phase == "missing_identity" {
				if _, err := os.Lstat(objects); !os.IsNotExist(err) {
					t.Fatal("startup created missing object root")
				}
			}
		})
	}
}

func TestRestoreGuardPreservesOrdinaryBootstrapAndForwardMigration(t *testing.T) {
	config := postgresqltest.NewDatabase(t)
	if err := postgres.Migrate(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	objects := t.TempDir()
	configureEntrypoints(t, config, objects)
	output, err := runEntrypoint(t, "bootstrap-owner")
	if err != nil || string(output) != "owner created\n" {
		t.Fatal("ordinary bootstrap failed")
	}
	if err := postgres.Migrate(context.Background(), config); err != nil {
		t.Fatal("ordinary forward migration rejected")
	}
	store, err := postgres.Open(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CheckObjectRoot(context.Background(), objects); err != nil {
		t.Fatal(err)
	}
	var users int
	if err := store.DB().QueryRow("SELECT count(*) FROM users").Scan(&users); err != nil || users != 1 {
		t.Fatal("ordinary bootstrap was not preserved")
	}
	if output, err := runEntrypoint(t, "recover-account"); err != nil || len(output) == 0 {
		t.Fatal("ordinary recovery CLI failed")
	}
}

func configureEntrypoints(t *testing.T, config postgres.Config, objects string) {
	t.Helper()
	setTripBackupEnvironment(t, config, false)
	for name, value := range map[string]string{
		"SBM_OBJECTS_PATH": objects, "SBM_HTTP_ADDRESS": "127.0.0.1:0",
		"SBM_COOKIE_SECURE": "false", "SBM_SESSION_TTL": "1h", "SBM_DEPLOYMENT_MODE": "local", "SBM_AI_CONCURRENCY": "1",
		"SBM_PDFINFO_PATH": "/usr/bin/pdfinfo", "SBM_PDFTOPPM_PATH": "/usr/bin/pdftoppm",
		"SBM_MASTER_KEY_FILE":        filepath.Join(t.TempDir(), "not-needed-before-guard"),
		"SBM_EXTRACTION_SCHEMA_PATH": "/workspace/contracts/extraction/bill-visible-text-v2.schema.json", "SBM_WEB_DIST_PATH": "/app/web",
	} {
		t.Setenv(name, value)
	}
}

func runEntrypoint(t *testing.T, entry string) ([]byte, error) {
	t.Helper()
	binaries := os.Getenv("SBM_TEST_ENTRYPOINTS_DIR")
	if binaries == "" {
		t.Fatal("SBM_TEST_ENTRYPOINTS_DIR with current compiled entrypoints is required")
	}
	var args []string
	if entry != "server" {
		material := make([]byte, 32)
		if _, err := rand.Read(material); err != nil {
			t.Fatal(err)
		}
		defer clear(material)
		password := hex.EncodeToString(material)
		path := filepath.Join(t.TempDir(), "input")
		payload := []byte(password)
		if entry == "bootstrap-owner" {
			args = []string{"-email", "synthetic@example.invalid", "-display-name", "synthetic", "-tenant-name", "synthetic", "-currency", "CNY", "-timezone", "UTC", "-password-file", path}
		} else {
			payload, _ = json.Marshal(map[string]string{"email": "synthetic@example.invalid", "new_password": password, "reason": "synthetic restore guard verification"})
			args = []string{"-input-file", path, "-confirm-all-workspaces"}
		}
		if err := os.WriteFile(path, payload, 0600); err != nil {
			t.Fatal(err)
		}
		clear(payload)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, filepath.Join(binaries, entry), args...).CombinedOutput()
}
