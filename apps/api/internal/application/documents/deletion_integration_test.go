package documents

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestDocumentDeletionRestoresObjectsWhenDatabaseTransactionFails(t *testing.T) {
	ctx := context.Background()
	store, objects, tenant := newDeletionFixture(t)
	result := uploadDeletionFixture(t, store, objects, tenant)
	document, err := store.GetDocument(ctx, tenant.TenantID, result.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		CREATE FUNCTION pg_temp.fail_document_tombstone() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic_deletion_failure'; END; $$;
		CREATE TRIGGER fail_document_tombstone
		BEFORE INSERT ON deletion_tombstones
		FOR EACH ROW EXECUTE FUNCTION pg_temp.fail_document_tombstone()
	`); err != nil {
		t.Fatal(err)
	}
	service := NewDeletionService(store, objects, store, system.IDGenerator{}, fixedClock{
		now: time.Date(2026, 8, 28, 1, 0, 0, 0, time.UTC),
	})
	if err := service.Delete(ctx, tenant, result.DocumentID, "delete-failure"); err == nil {
		t.Fatal("injected tombstone failure was ignored")
	}
	if _, err := store.GetDocument(ctx, tenant.TenantID, result.DocumentID); err != nil {
		t.Fatalf("database rollback did not preserve document: %v", err)
	}
	reader, err := objects.Open(ctx, document.StorageKey)
	if err != nil {
		t.Fatalf("failed deletion did not restore original object: %v", err)
	}
	_ = reader.Close()
	pending, err := objects.PendingDeletions(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("failed deletion left trash batches: %#v, error=%v", pending, err)
	}
	if _, err := store.DB().ExecContext(ctx, "DROP TRIGGER fail_document_tombstone ON deletion_tombstones"); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, tenant, result.DocumentID, "delete-success"); err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Open(ctx, document.StorageKey); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("committed deletion left original object readable: %v", err)
	}
}

func TestDocumentDeletionReconcileRestoresUncommittedAndPurgesCommittedBatches(t *testing.T) {
	ctx := context.Background()
	store, objects, tenant := newDeletionFixture(t)
	service := NewDeletionService(store, objects, store, system.IDGenerator{}, fixedClock{
		now: time.Date(2026, 8, 28, 1, 0, 0, 0, time.UTC),
	})

	uncommittedKey := "tenants/" + tenant.TenantID + "/documents/reconcile-uncommitted/original"
	commitDeletionFixtureObject(t, objects, uncommittedKey, []byte("restore me"))
	uncommittedID := "00000000-0000-4000-8000-000000000301"
	if err := objects.StageDeletion(ctx, uncommittedID, []string{uncommittedKey}); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	reader, err := objects.Open(ctx, uncommittedKey)
	if err != nil {
		t.Fatalf("reconcile did not restore uncommitted deletion: %v", err)
	}
	_ = reader.Close()

	committedKey := "tenants/" + tenant.TenantID + "/documents/reconcile-committed/original"
	commitDeletionFixtureObject(t, objects, committedKey, []byte("purge me"))
	committedID := "00000000-0000-4000-8000-000000000302"
	if err := objects.StageDeletion(ctx, committedID, []string{committedKey}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO deletion_tombstones (
			id, tenant_id, actor_user_id, resource_type, resource_id_hash,
			object_hashes_json, resource_counts_json, request_id, created_at
		) VALUES (?, ?, ?, 'document_aggregate', ?, '["synthetic"]', '{"documents":1}', ?, ?)
	`, committedID, tenant.TenantID, tenant.UserID, "synthetic-resource-hash", "reconcile-test", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Open(ctx, committedKey); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("reconcile did not purge committed deletion: %v", err)
	}
	pending, err := objects.PendingDeletions(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("reconcile left trash batches: %#v, error=%v", pending, err)
	}
}

func newDeletionFixture(t *testing.T) (*postgresqladapter.Store, *localstorage.Store, domain.TenantContext) {
	t.Helper()
	store := postgresqltest.Open(t)
	owner := ports.BootstrapOwner{
		UserID: "00000000-0000-4000-8000-000000000201", TenantID: "00000000-0000-4000-8000-000000000202",
		Email: "deletion@example.test", PasswordHash: "test-only", DisplayName: "Owner", TenantName: "Tenant",
		DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC", CreatedAt: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
	}
	if err := store.BootstrapOwner(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	objects, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store, objects, domain.TenantContext{TenantID: owner.TenantID, UserID: owner.UserID, Role: domain.RoleOwner}
}

func uploadDeletionFixture(
	t *testing.T,
	store *postgresqladapter.Store,
	objects *localstorage.Store,
	tenant domain.TenantContext,
) UploadResult {
	t.Helper()
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	service := NewUploadService(objects, inspector, store, system.IDGenerator{}, fixedClock{
		now: time.Date(2026, 8, 28, 0, 30, 0, 0, time.UTC),
	})
	content, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(context.Background(), UploadInput{
		Tenant: tenant, Name: "delete-me.png", MIME: "image/png", Source: bytes.NewReader(content),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func commitDeletionFixtureObject(t *testing.T, objects *localstorage.Store, key string, content []byte) {
	t.Helper()
	staged, err := objects.Stage(context.Background(), bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Commit(context.Background(), staged, key); err != nil {
		t.Fatal(err)
	}
}
