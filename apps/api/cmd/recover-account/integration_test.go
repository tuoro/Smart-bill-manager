package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/bootstrap"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestRecoveryCommandAgainstIsolatedPostgreSQL(t *testing.T) {
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
	randomPassword := func() []byte {
		value := make([]byte, 32)
		if _, err := rand.Read(value); err != nil {
			t.Fatal(err)
		}
		result := []byte(base64.RawURLEncoding.EncodeToString(value))
		clear(value)
		t.Cleanup(func() { clear(result) })
		return result
	}
	oldPassword, nextPassword := randomPassword(), randomPassword()
	hasher, err := cryptography.NewPasswordHasher(cryptography.DefaultArgon2Params)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bootstrap.NewService(store, hasher, system.IDGenerator{}, system.Clock{}).Execute(ctx, bootstrap.Input{Email: "recovery@example.invalid", Password: oldPassword, DisplayName: "合成恢复", TenantName: "合成工作区", DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	login, err := auth.NewService(store, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := login.Login(ctx, auth.LoginInput{Email: "recovery@example.invalid", Password: oldPassword})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"HOST": config.Host, "PORT": strconv.Itoa(int(config.Port)), "DATABASE": config.Database, "USER": config.User, "PASSWORD_FILE": config.PasswordFile, "SSL_MODE": "disable"} {
		t.Setenv("SBM_POSTGRES_"+key, value)
	}
	t.Setenv("SBM_MIGRATIONS_DIR", config.MigrationsDir)
	t.Setenv("SBM_OBJECTS_PATH", t.TempDir())
	path := filepath.Join(t.TempDir(), "recovery-input")
	payload, err := json.Marshal(recoveryInput{Email: "recovery@example.invalid", Password: string(nextPassword), Reason: "合成本地恢复"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	clear(payload)
	var output bytes.Buffer
	if err := run([]string{"--confirm-all-workspaces", "--input-file", path}, os.Stdin, io.Discard, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "account recovered; all workspace sessions revoked\n" {
		t.Fatal("recovery output is not a safe aggregate")
	}
	if _, err := login.Authenticate(ctx, prior.SessionToken); err == nil {
		t.Fatal("old session survived CLI recovery")
	}
	if _, err := login.Login(ctx, auth.LoginInput{Email: "recovery@example.invalid", Password: oldPassword}); err == nil {
		t.Fatal("old password survived CLI recovery")
	}
	if _, err := login.Login(ctx, auth.LoginInput{Email: "recovery@example.invalid", Password: nextPassword}); err != nil {
		t.Fatal("new password cannot authenticate")
	}
	var events int
	if err := store.DB().QueryRow(`SELECT count(*) FROM account_events WHERE actor_kind='local_operator' AND action='password_recovered'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("CLI recovery audit missing")
	}
}
