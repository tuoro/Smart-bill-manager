package documents

import (
	"context"
	"fmt"
	"io"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type DocumentContent struct {
	Name string
	MIME string
	Body io.ReadCloser
}

type QueryService struct {
	jobs      ports.JobRepository
	documents ports.DocumentRepository
	objects   ports.ObjectStore
}

func NewQueryService(
	jobs ports.JobRepository,
	documents ports.DocumentRepository,
	objects ports.ObjectStore,
) QueryService {
	return QueryService{jobs: jobs, documents: documents, objects: objects}
}

func (s QueryService) ListJobs(
	ctx context.Context,
	tenant domain.TenantContext,
	status *domain.JobStatus,
) ([]ports.JobSummary, error) {
	if err := tenant.Require(domain.CapabilityDocumentsProcess); err != nil {
		return nil, err
	}
	if status != nil && !status.Valid() {
		return nil, domain.NewRuleError("invalid_job_status", "Job 状态不受支持", domain.ErrInvalidInput)
	}
	return s.jobs.ListJobs(ctx, tenant.TenantID, status)
}

func (s QueryService) GetJob(
	ctx context.Context,
	tenant domain.TenantContext,
	jobID string,
) (ports.JobSummary, error) {
	if err := tenant.Require(domain.CapabilityDocumentsProcess); err != nil {
		return ports.JobSummary{}, err
	}
	return s.jobs.GetJob(ctx, tenant.TenantID, jobID)
}

func (s QueryService) GetDocument(
	ctx context.Context,
	tenant domain.TenantContext,
	documentID string,
) (ports.Document, error) {
	if err := tenant.Require(domain.CapabilityDocumentsProcess); err != nil {
		return ports.Document{}, err
	}
	return s.documents.GetDocument(ctx, tenant.TenantID, documentID)
}

func (s QueryService) OpenDocument(
	ctx context.Context,
	tenant domain.TenantContext,
	documentID string,
) (DocumentContent, error) {
	if err := tenant.Require(domain.CapabilityReviewSourceRead); err != nil {
		return DocumentContent{}, err
	}
	object, err := s.documents.GetDocumentObject(ctx, tenant.TenantID, documentID)
	if err != nil {
		return DocumentContent{}, err
	}
	if tenant.Role == domain.RoleReviewer && object.ReviewState != domain.JobNeedsReview && object.ReviewState != domain.JobBlocked {
		return DocumentContent{}, domain.ErrNotFound
	}
	body, err := s.objects.Open(ctx, object.StorageKey)
	if err != nil {
		return DocumentContent{}, err
	}
	return DocumentContent{Name: object.Name, MIME: object.MIME, Body: body}, nil
}

func (s QueryService) OpenDocumentPage(
	ctx context.Context,
	tenant domain.TenantContext,
	documentID string,
	pageNumber int,
) (DocumentContent, error) {
	if err := tenant.Require(domain.CapabilityReviewSourceRead); err != nil {
		return DocumentContent{}, err
	}
	if pageNumber < 1 || pageNumber > 20 {
		return DocumentContent{}, domain.NewRuleError("invalid_page_number", "页码必须是 1 到 20 的整数", domain.ErrInvalidInput)
	}
	object, err := s.documents.GetDocumentPageObject(ctx, tenant.TenantID, documentID, pageNumber)
	if err != nil {
		return DocumentContent{}, err
	}
	if tenant.Role == domain.RoleReviewer && object.ReviewState != domain.JobNeedsReview && object.ReviewState != domain.JobBlocked {
		return DocumentContent{}, domain.ErrNotFound
	}
	body, err := s.objects.Open(ctx, object.StorageKey)
	if err != nil {
		return DocumentContent{}, err
	}
	return DocumentContent{Name: fmt.Sprintf("page-%d.png", object.PageNumber), MIME: "image/png", Body: body}, nil
}
