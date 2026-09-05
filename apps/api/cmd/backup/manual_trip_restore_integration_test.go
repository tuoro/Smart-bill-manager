//go:build postgresql_tools

package main

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

// 此显式工具集成门禁需要真实 PostgreSQL 17 pg_dump/pg_restore；缺失工具必须失败，不能跳过或伪造 dump。
func TestManualTripAuthenticatedBackupRestore(t *testing.T) {
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
	now := time.Now().UTC()
	for _, statement := range []string{
		`INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at) VALUES ('synthetic-owner', 'restore@example.invalid', 'synthetic-nonlogin-hash', '合成用户', $1, $1)`,
		`INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at) VALUES ('synthetic-tenant', '合成租户', 'CNY', 'Asia/Shanghai', $1, $1)`,
		`INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at) VALUES ('synthetic-tenant', 'synthetic-owner', 'owner', 'active', $1, $1)`,
	} {
		if _, err := store.DB().ExecContext(ctx, statement, now); err != nil {
			t.Fatal(err)
		}
	}
	tenant := domain.TenantContext{TenantID: "synthetic-tenant", UserID: "synthetic-owner", Role: domain.RoleOwner}
	trips := tripapp.NewService(store, store, system.IDGenerator{}, system.Clock{})
	input := tripapp.ManagementInput{Details: domain.TripDetails{Name: "合成恢复行程", StartDate: "2026-09-01", EndDate: "2026-09-03", Timezone: "Asia/Shanghai"},
		Reason: "合成恢复验证", IdempotencyKey: "restore-trip-create", RequestID: "restore-trip-request"}
	created, err := trips.Manage(ctx, tenant, "", "create", input)
	if err != nil {
		t.Fatal(err)
	}
	input.Details.Notes = "管理历史必须完整恢复"
	input.ExpectedVersion = created.Version
	input.IdempotencyKey = "restore-trip-edit"
	if _, err := trips.Manage(ctx, tenant, created.TripID, "edit", input); err != nil {
		t.Fatal(err)
	}
	before, err := trips.List(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	objects := filepath.Join(root, "source-objects")
	objectStore, err := localstorage.New(objects)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(objects, "export-spool"), 0700); err != nil {
		t.Fatal(err)
	}
	manualBefore := seedManualRestoreReview(t, store, objectStore, tenant)
	correctionBefore := seedCorrectedRestoreFact(t, store, tenant, manualBefore)
	materialsBefore := seedInvoiceMaterialRestore(t, store, objectStore, tenant, created.TripID)
	var invoiceID string
	if err := store.DB().QueryRow(`SELECT id FROM invoices ORDER BY id LIMIT 1`).Scan(&invoiceID); err != nil {
		t.Fatal(err)
	}
	factService := reviews.NewFactService(store, store, system.IDGenerator{}, system.Clock{})
	for _, target := range []struct {
		kind domain.DocumentType
		id   string
	}{{domain.DocumentPayment, correctionBefore.State.FactID}, {domain.DocumentInvoice, invoiceID}} {
		detail, err := factService.Detail(ctx, tenant, target.kind, target.id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := factService.SetBadDebt(ctx, tenant, target.kind, target.id, domain.BadDebtInput{Marked: true, ExpectedVersion: detail.Version, Reason: "合成坏账恢复验证"}, "restore-bad-debt-"+string(target.kind), "restore-bad-debt-request"); err != nil {
			t.Fatal(err)
		}
	}
	correctionBefore, err = reviews.NewService(store, store, system.IDGenerator{}, system.Clock{}).GetCorrection(ctx, tenant, domain.DocumentPayment, correctionBefore.State.FactID)
	if err != nil {
		t.Fatal(err)
	}
	const badDebtHistorySQL = `SELECT jsonb_agg(to_jsonb(d) ORDER BY id)::text FROM fact_bad_debt_decisions d`
	var badDebtBefore string
	if err := store.DB().QueryRow(badDebtHistorySQL).Scan(&badDebtBefore); err != nil {
		t.Fatal(err)
	}
	before, err = trips.List(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	manualBefore, err = reviews.NewService(store, store, system.IDGenerator{}, system.Clock{}).GetClaimSet(ctx, tenant, manualBefore.ClaimSetID)
	if err != nil {
		t.Fatal(err)
	}
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "source-master")
	if err := os.WriteFile(masterPath, masterKey, 0600); err != nil {
		t.Fatal(err)
	}
	clear(masterKey)
	setTripBackupEnvironment(t, config, false)
	backupPath := filepath.Join(root, "authenticated-backup")
	manifest, err := createBackup(ctx, backupOptions{Objects: objects, MasterKey: masterPath, Migrations: config.MigrationsDir, Output: backupPath, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"trip_evidence_facts", "trip_material_links", "trip_material_decisions", "trip_management_decisions", "fact_corrections", "invoice_material_links", "invoice_material_decisions", "reimbursement_material_snapshots"} {
		if _, exists := manifest.Database.TableCounts[table]; !exists {
			t.Fatalf("backup omitted new table %s", table)
		}
	}
	if manifest.Database.TableCounts["trips"] != 1 || manifest.Database.TableCounts["trip_management_decisions"] != 2 {
		t.Fatal("wrong manual workspace inventory")
	}
	if manifest.Database.TableCounts["fact_corrections"] != 1 {
		t.Fatal("correction history missing from authenticated backup")
	}
	if manifest.Database.TableCounts["fact_bad_debt_decisions"] != 2 {
		t.Fatal("bad debt history omitted from backup")
	}
	if manifest.Database.TableCounts["invoice_material_links"] != 2 || manifest.Database.TableCounts["invoice_material_decisions"] != 3 || manifest.Database.TableCounts["reimbursement_material_snapshots"] != 1 {
		t.Fatal("nonempty shared material history missing from backup")
	}
	restoredConfig := postgresqltest.NewDatabase(t)
	setTripBackupEnvironment(t, restoredConfig, true)
	keyDirectory := filepath.Join(root, "restored-secrets")
	if err := os.Mkdir(keyDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	restoredObjects := filepath.Join(root, "restored-objects")
	restoredManifest, invalidated, err := restoreBackup(ctx, restoreOptions{Backup: backupPath, MasterKeySource: masterPath, Migrations: config.MigrationsDir,
		Objects: restoredObjects, MasterKey: filepath.Join(keyDirectory, "master"), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	spoolEntries, err := os.ReadDir(filepath.Join(restoredObjects, "export-spool"))
	if err != nil || len(spoolEntries) != 0 {
		t.Fatalf("restored export spool must exist and be empty: %v", err)
	}
	if invalidated != 0 || !reflect.DeepEqual(manifest.Database.TableCounts, restoredManifest.Database.TableCounts) {
		t.Fatal("restored inventory mismatch")
	}
	restored, err := postgres.Open(ctx, restoredConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var badDebtAfter string
	if err := restored.DB().QueryRow(badDebtHistorySQL).Scan(&badDebtAfter); err != nil || badDebtAfter != badDebtBefore {
		t.Fatal("bad debt history changed on restore")
	}
	for _, target := range []struct {
		kind domain.DocumentType
		id   string
	}{{domain.DocumentPayment, correctionBefore.State.FactID}, {domain.DocumentInvoice, invoiceID}} {
		var marked bool
		if err := restored.DB().QueryRow(`SELECT sbm_fact_bad_debt(?,?,?)`, tenant.TenantID, target.kind, target.id).Scan(&marked); err != nil || !marked {
			t.Fatal("restored bad debt state lost")
		}
	}
	var materialsAfter string
	if err := restored.DB().QueryRow(materialRestoreHistorySQL).Scan(&materialsAfter); err != nil || materialsAfter != materialsBefore {
		t.Fatalf("restored material decisions, shared links or fixed snapshot differ: %v", err)
	}
	manualAfter, err := reviews.NewService(restored, restored, system.IDGenerator{}, system.Clock{}).GetClaimSet(ctx, tenant, manualBefore.ClaimSetID)
	if err != nil || !reflect.DeepEqual(manualBefore, manualAfter) {
		t.Fatal("restored manual source, revision, evidence or failure history differs")
	}
	correctionAfter, err := reviews.NewService(restored, restored, system.IDGenerator{}, system.Clock{}).GetCorrection(ctx, tenant, domain.DocumentPayment, correctionBefore.State.FactID)
	if err != nil || !reflect.DeepEqual(correctionBefore, correctionAfter) {
		t.Fatal("restored correction projection/source differs")
	}
	var originalCorrections, restoredCorrections string
	const correctionsSQL = `SELECT jsonb_agg(to_jsonb(c) ORDER BY review_decision_id)::text FROM fact_corrections c`
	if err := store.DB().QueryRowContext(ctx, correctionsSQL).Scan(&originalCorrections); err != nil {
		t.Fatal(err)
	}
	if err := restored.DB().QueryRowContext(ctx, correctionsSQL).Scan(&restoredCorrections); err != nil {
		t.Fatal(err)
	}
	if originalCorrections != restoredCorrections {
		t.Fatal("restored correction decisions differ")
	}
	restoredTrips := tripapp.NewService(restored, restored, system.IDGenerator{}, system.Clock{})
	after, err := restoredTrips.List(ctx, tenant)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("restored manual workspace differs: %v", err)
	}
	var oldHistory, newHistory string
	const historySQL = `SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM trip_management_decisions d`
	if err := store.DB().QueryRowContext(ctx, historySQL).Scan(&oldHistory); err != nil {
		t.Fatal(err)
	}
	if err := restored.DB().QueryRowContext(ctx, historySQL).Scan(&newHistory); err != nil {
		t.Fatal(err)
	}
	if oldHistory != newHistory {
		t.Fatal("restored management history differs")
	}
}

func setTripBackupEnvironment(t *testing.T, config postgres.Config, restoring bool) {
	t.Helper()
	values := map[string]string{"SBM_MIGRATIONS_DIR": config.MigrationsDir}
	if restoring {
		for key, value := range map[string]string{"HOST": config.Host, "PORT": strconv.Itoa(int(config.Port)), "DATABASE": config.Database,
			"USER": config.User, "PASSWORD_FILE": config.PasswordFile, "SSL_MODE": "disable", "RUNTIME_USER": config.User} {
			values["SBM_POSTGRES_RESTORE_"+key] = value
		}
	} else {
		for key, value := range map[string]string{"HOST": config.Host, "PORT": strconv.Itoa(int(config.Port)), "DATABASE": config.Database,
			"USER": config.User, "PASSWORD_FILE": config.PasswordFile, "MIGRATION_USER": config.User, "MIGRATION_PASSWORD_FILE": config.PasswordFile, "SSL_MODE": "disable"} {
			values["SBM_POSTGRES_"+key] = value
		}
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
}
