//go:build postgresql_tools

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/restorestate"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func activationBackupFixture(t *testing.T, withIdentity bool) (string, string, postgres.Config) {
	t.Helper()
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if withIdentity {
		now := time.Now().UTC()
		for _, statement := range []string{
			`INSERT INTO users (id,email,password_hash,display_name,created_at,updated_at) VALUES ('user','synthetic@example.invalid','synthetic-nonlogin-hash','synthetic', $1,$1)`,
			`INSERT INTO tenants (id,name,default_currency,timezone,created_at,updated_at) VALUES ('tenant','synthetic','CNY','UTC',$1,$1)`,
			`INSERT INTO memberships (tenant_id,user_id,role,status,created_at,updated_at) VALUES ('tenant','user','owner','active',$1,$1)`,
		} {
			if _, err := store.DB().Exec(statement, now); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.DB().Exec(`INSERT INTO sessions (id,tenant_id,user_id,token_hash,csrf_token_hash,expires_at,created_at,last_seen_at)
		VALUES ('session','tenant','user','synthetic-session-hash','synthetic-csrf-hash',$1,$2,$2)`, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	objects := filepath.Join(root, "objects")
	key := filepath.Join(root, "key")
	if _, err := localstorage.New(objects); err != nil {
		t.Fatal(err)
	}
	material := make([]byte, 32)
	if _, err := rand.Read(material); err != nil {
		t.Fatal(err)
	}
	defer clear(material)
	if err := os.WriteFile(key, material, 0600); err != nil {
		t.Fatal(err)
	}
	setTripBackupEnvironment(t, config, false)
	backup := filepath.Join(root, "backup")
	if _, err := createBackup(ctx, backupOptions{Objects: objects, MasterKey: key, Migrations: config.MigrationsDir, Output: backup, Offline: true}); err != nil {
		t.Fatal(err)
	}
	return backup, key, config
}

func TestRestoreFailureWindowsNeverActivateDatabase(t *testing.T) {
	backup, key, _ := activationBackupFixture(t, true)
	for _, failure := range []string{"database_restored", "key_publication", "before_activation"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			target := postgresqltest.NewDatabase(t)
			setTripBackupEnvironment(t, target, true)
			root := t.TempDir()
			objects := filepath.Join(root, "objects")
			restoredKey := filepath.Join(root, "key")
			sentinel := errors.New("synthetic-interruption")
			options := restoreOptions{Backup: backup, MasterKeySource: key, Migrations: target.MigrationsDir, Objects: objects, MasterKey: restoredKey, Offline: true}
			options.checkpoint = func(phase string) error {
				if phase == failure {
					return sentinel
				}
				return nil
			}
			options.publish = func(source, destination string) error {
				if failure == "key_publication" && destination == restoredKey {
					return sentinel
				}
				return publishNoReplace(source, destination)
			}
			if _, _, err := restoreBackup(ctx, options); !errors.Is(err, sentinel) {
				t.Fatalf("injection did not run: %v", err)
			}
			if store, err := postgres.Open(ctx, target); err == nil {
				store.Close()
				t.Fatal("shared runtime open accepted failed restoration")
			}
			if err := postgres.Migrate(ctx, target); err == nil {
				t.Fatal("migration opened failed restoration")
			}
			var phase string
			if err := postgresqltest.InspectDatabase(t, target).QueryRow("SELECT phase FROM sbm_restore.state").Scan(&phase); err != nil || phase != "incomplete" {
				t.Fatal("failed restore lost barrier")
			}
			var sessions int
			if err := postgresqltest.InspectDatabase(t, target).QueryRow("SELECT count(*) FROM sessions").Scan(&sessions); err != nil {
				t.Fatal(err)
			}
			want := 0
			if failure == "database_restored" {
				want = 1
			}
			if sessions != want {
				t.Fatal("failure injection did not cover the expected session invalidation window")
			}
		})
	}
}

func TestRestoreActivationPairsObjectsAndSupportsFreshRebackup(t *testing.T) {
	backup, key, _ := activationBackupFixture(t, true)
	ctx := context.Background()
	target := postgresqltest.NewDatabase(t)
	setTripBackupEnvironment(t, target, true)
	root := t.TempDir()
	objects := filepath.Join(root, "objects")
	restoredKey := filepath.Join(root, "key")
	if _, _, err := restoreBackup(ctx, restoreOptions{Backup: backup, MasterKeySource: key, Migrations: target.MigrationsDir, Objects: objects, MasterKey: restoredKey, Offline: true}); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CheckObjectRoot(ctx, objects); err != nil {
		t.Fatal(err)
	}
	if store.CheckObjectRoot(ctx, t.TempDir()) == nil {
		t.Fatal("restored database accepted missing identity")
	}
	identity, err := restorestate.Read(objects)
	if err != nil {
		t.Fatal(err)
	}
	ordinary := postgresqltest.NewDatabase(t)
	if err := postgres.Migrate(ctx, ordinary); err != nil {
		t.Fatal(err)
	}
	ordinaryStore, err := postgres.Open(ctx, ordinary)
	if err != nil {
		t.Fatal(err)
	}
	defer ordinaryStore.Close()
	if ordinaryStore.CheckObjectRoot(ctx, objects) == nil {
		t.Fatal("ordinary database accepted orphan identity")
	}
	setTripBackupEnvironment(t, target, false)
	secondBackup := filepath.Join(root, "backup-again")
	if _, err := createBackup(ctx, backupOptions{Objects: objects, MasterKey: restoredKey, Migrations: target.MigrationsDir, Output: secondBackup, Offline: true}); err != nil {
		t.Fatal(err)
	}
	second := postgresqltest.NewDatabase(t)
	setTripBackupEnvironment(t, second, true)
	secondRoot := t.TempDir()
	secondObjects := filepath.Join(secondRoot, "objects")
	if _, _, err := restoreBackup(ctx, restoreOptions{Backup: secondBackup, MasterKeySource: restoredKey, Migrations: second.MigrationsDir, Objects: secondObjects, MasterKey: filepath.Join(secondRoot, "key"), Offline: true}); err != nil {
		t.Fatal(err)
	}
	other, err := restorestate.Read(secondObjects)
	if err != nil || other.RestoreID == identity.RestoreID {
		t.Fatal("restore copied an old lifecycle")
	}
	if store.CheckObjectRoot(ctx, secondObjects) == nil {
		t.Fatal("restored database accepted another restore identity")
	}
	for _, mutation := range []string{
		"UPDATE sbm_restore.state SET database_oid=database_oid+1",
		"UPDATE sbm_restore.state SET database_name='different_database'",
		"DELETE FROM sbm_restore.state",
		"ALTER TABLE sbm_restore.state DROP CONSTRAINT state_phase_check; UPDATE sbm_restore.state SET phase='unknown'",
	} {
		db := postgresqltest.InspectDatabase(t, target)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(mutation); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		// 损坏状态须对其他启动连接可见，单独保留原行以便下一个场景恢复。
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if opened, err := postgres.Open(ctx, target); err == nil {
			opened.Close()
			t.Fatal("corrupt activation accepted")
		}
		if _, err := db.Exec("DELETE FROM sbm_restore.state"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO sbm_restore.state (singleton,format_version,restore_id,database_oid,database_name,phase) VALUES (1,1,$1,$2,$3,'complete')", identity.RestoreID, identity.DatabaseOID, identity.DatabaseName); err != nil {
			t.Fatal(err)
		}
	}
}
