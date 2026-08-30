package documents

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type queryJobRepository struct {
	items []ports.JobSummary
	item  ports.JobSummary
	err   error
}

func (r queryJobRepository) ListJobs(context.Context, string, *domain.JobStatus) ([]ports.JobSummary, error) {
	return r.items, r.err
}
func (r queryJobRepository) GetJob(context.Context, string, string) (ports.JobSummary, error) {
	return r.item, r.err
}

type queryDocumentRepository struct {
	document ports.Document
	object   ports.DocumentObject
	err      error
}

func (r queryDocumentRepository) GetDocument(context.Context, string, string) (ports.Document, error) {
	return r.document, r.err
}
func (r queryDocumentRepository) GetDocumentObject(context.Context, string, string) (ports.DocumentObject, error) {
	return r.object, r.err
}

type queryObjectStore struct{ err error }

func (queryObjectStore) Stage(context.Context, io.Reader, int64) (ports.StagedObject, error) {
	return ports.StagedObject{}, errors.New("unused")
}
func (queryObjectStore) Commit(context.Context, ports.StagedObject, string) error {
	return errors.New("unused")
}
func (queryObjectStore) Abort(context.Context, ports.StagedObject) error { return errors.New("unused") }
func (s queryObjectStore) Open(context.Context, string) (io.ReadCloser, error) {
	if s.err != nil {
		return nil, s.err
	}
	return io.NopCloser(bytes.NewBufferString("document")), nil
}
func (queryObjectStore) Delete(context.Context, string) error { return errors.New("unused") }

func TestQueryServiceAuthorizationAndReviewSourceBoundary(t *testing.T) {
	owner := domain.TenantContext{TenantID: "tenant", UserID: "owner", Role: domain.RoleOwner}
	viewer := domain.TenantContext{TenantID: "tenant", UserID: "viewer", Role: domain.RoleViewer}
	reviewer := domain.TenantContext{TenantID: "tenant", UserID: "reviewer", Role: domain.RoleReviewer}
	jobs := queryJobRepository{
		items: []ports.JobSummary{{ID: "job"}},
		item:  ports.JobSummary{ID: "job"},
	}
	documents := queryDocumentRepository{object: ports.DocumentObject{
		StorageKey: "tenants/tenant/document", Name: "receipt.png", MIME: "image/png", ReviewState: domain.JobNeedsReview,
	}}
	service := NewQueryService(jobs, documents, queryObjectStore{})
	if _, err := service.ListJobs(context.Background(), viewer, nil); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer inbox error = %v", err)
	}
	invalid := domain.JobStatus("invalid")
	if _, err := service.ListJobs(context.Background(), owner, &invalid); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("invalid status error = %v", err)
	}
	if items, err := service.ListJobs(context.Background(), owner, nil); err != nil || len(items) != 1 {
		t.Fatalf("list jobs = %#v, error=%v", items, err)
	}
	if item, err := service.GetJob(context.Background(), owner, "job"); err != nil || item.ID != "job" {
		t.Fatalf("get job = %#v, error=%v", item, err)
	}
	content, err := service.OpenDocument(context.Background(), reviewer, "document")
	if err != nil {
		t.Fatal(err)
	}
	defer content.Body.Close()
	if content.Name != "receipt.png" || content.MIME != "image/png" {
		t.Fatalf("document content = %#v", content)
	}

	documents.object.ReviewState = domain.JobCompleted
	service = NewQueryService(jobs, documents, queryObjectStore{})
	if _, err := service.OpenDocument(context.Background(), reviewer, "document"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("reviewer completed source error = %v", err)
	}
	if _, err := service.OpenDocument(context.Background(), viewer, "document"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer source error = %v", err)
	}
	if content, err := service.OpenDocument(context.Background(), owner, "document"); err != nil {
		t.Fatal(err)
	} else {
		_ = content.Body.Close()
	}
}

func TestQueryServiceDoesNotHideRepositoryOrStorageFailures(t *testing.T) {
	owner := domain.TenantContext{TenantID: "tenant", UserID: "owner", Role: domain.RoleOwner}
	repositoryErr := errors.New("repository failure")
	service := NewQueryService(
		queryJobRepository{err: repositoryErr},
		queryDocumentRepository{err: repositoryErr},
		queryObjectStore{},
	)
	if _, err := service.ListJobs(context.Background(), owner, nil); !errors.Is(err, repositoryErr) {
		t.Fatalf("list repository error = %v", err)
	}
	if _, err := service.GetJob(context.Background(), owner, "job"); !errors.Is(err, repositoryErr) {
		t.Fatalf("get repository error = %v", err)
	}
	if _, err := service.OpenDocument(context.Background(), owner, "document"); !errors.Is(err, repositoryErr) {
		t.Fatalf("document repository error = %v", err)
	}
	service = NewQueryService(
		queryJobRepository{},
		queryDocumentRepository{object: ports.DocumentObject{StorageKey: "key"}},
		queryObjectStore{err: repositoryErr},
	)
	if _, err := service.OpenDocument(context.Background(), owner, "document"); !errors.Is(err, repositoryErr) {
		t.Fatalf("object storage error = %v", err)
	}
}
