package documents

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/sqlite"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestUploadCreatesOneImmutableDocumentAndJob(t *testing.T) {
	ctx := context.Background()
	store, err := sqliteadapter.Open(ctx, sqliteadapter.Config{
		DatabasePath:  ":memory:",
		MigrationsDir: testMigrationsDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner := ports.BootstrapOwner{
		UserID:          "00000000-0000-4000-8000-000000000101",
		TenantID:        "00000000-0000-4000-8000-000000000102",
		Email:           "owner@example.test",
		PasswordHash:    "test-only",
		DisplayName:     "Owner",
		TenantName:      "Tenant",
		DefaultCurrency: domain.CurrencyCNY,
		Timezone:        "UTC",
		CreatedAt:       time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	objects, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	service := NewUploadService(objects, inspector, store, system.IDGenerator{}, fixedClock{
		now: time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC),
	})
	content, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	tenant := domain.TenantContext{TenantID: owner.TenantID, UserID: owner.UserID, Role: domain.RoleOwner}
	result, err := service.Execute(ctx, UploadInput{
		Tenant: tenant,
		Name:   "receipt.png",
		MIME:   "image/png",
		Source: bytes.NewReader(content),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.JobQueued || result.DocumentID == "" || result.JobID == "" {
		t.Fatalf("result = %#v", result)
	}
	_, err = service.Execute(ctx, UploadInput{
		Tenant: tenant,
		Name:   "same.png",
		MIME:   "image/png",
		Source: bytes.NewReader(content),
	})
	var duplicate *domain.DuplicateDocumentError
	if !errors.As(err, &duplicate) || duplicate.DocumentID != result.DocumentID {
		t.Fatalf("duplicate error = %v", err)
	}
	jobs, err := store.ListJobs(ctx, owner.TenantID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != result.JobID {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func TestUploadCommitFailureCompensatesMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := sqliteadapter.Open(ctx, sqliteadapter.Config{
		DatabasePath:  ":memory:",
		MigrationsDir: testMigrationsDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner := ports.BootstrapOwner{
		UserID:          "00000000-0000-4000-8000-000000000201",
		TenantID:        "00000000-0000-4000-8000-000000000202",
		Email:           "owner@example.test",
		PasswordHash:    "test-only",
		DisplayName:     "Owner",
		TenantName:      "Tenant",
		DefaultCurrency: domain.CurrencyCNY,
		Timezone:        "UTC",
		CreatedAt:       time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	objects, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	service := NewUploadService(commitFailStore{ObjectStore: objects}, inspector, store, system.IDGenerator{}, fixedClock{
		now: time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC),
	})
	content, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	tenant := domain.TenantContext{TenantID: owner.TenantID, UserID: owner.UserID, Role: domain.RoleOwner}
	if _, err := service.Execute(ctx, UploadInput{
		Tenant: tenant, Name: "receipt.png", MIME: "image/png", Source: bytes.NewReader(content),
	}); err == nil {
		t.Fatal("object commit failure was ignored")
	}
	var documents, jobs int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM documents WHERE tenant_id = ?),
		       (SELECT count(*) FROM processing_jobs WHERE tenant_id = ?)
	`, owner.TenantID, owner.TenantID).Scan(&documents, &jobs); err != nil {
		t.Fatal(err)
	}
	if documents != 0 || jobs != 0 {
		t.Fatalf("upload compensation left metadata: documents=%d jobs=%d", documents, jobs)
	}
}

func TestUploadRejectsUnauthorizedMissingAndUnsafeInputs(t *testing.T) {
	service := NewUploadService(nil, nil, nil, nil, nil)
	viewer := domain.TenantContext{TenantID: "tenant", UserID: "viewer", Role: domain.RoleViewer}
	if _, err := service.Execute(context.Background(), UploadInput{Tenant: viewer}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer upload error = %v", err)
	}
	owner := domain.TenantContext{TenantID: "tenant", UserID: "owner", Role: domain.RoleOwner}
	if _, err := service.Execute(context.Background(), UploadInput{Tenant: owner}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("missing document error = %v", err)
	}
	for _, value := range []string{"", ".", "bad\x00name.png", strings.Repeat("a", 201)} {
		if _, err := safeDocumentName(value); err == nil {
			t.Fatalf("unsafe document name accepted: %q", value)
		}
	}
	if value, err := safeDocumentName(" /tmp/receipt.png "); err != nil || value != "receipt.png" {
		t.Fatalf("safe document name = %q, error=%v", value, err)
	}
}

type commitFailStore struct{ ports.ObjectStore }

func (commitFailStore) Commit(context.Context, ports.StagedObject, string) error {
	return errors.New("synthetic commit failure")
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func testMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../infra/migrations"))
}
