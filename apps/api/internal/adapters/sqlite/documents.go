package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (t transaction) FindDocumentIDBySHA(ctx context.Context, tenantID, sha256 string) (string, error) {
	var id string
	err := t.tx.QueryRowContext(
		ctx,
		"SELECT id FROM documents WHERE tenant_id = ? AND sha256 = ?",
		tenantID,
		sha256,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find document by hash: %w", err)
	}
	return id, nil
}

func (t transaction) InsertDocument(ctx context.Context, document ports.Document) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO documents (
			id, tenant_id, storage_key, original_name, declared_mime, detected_mime,
			size_bytes, sha256, page_count, status, ingestion_kind, original_object_owner,
			created_by_user_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		document.ID,
		document.TenantID,
		document.StorageKey,
		document.OriginalName,
		document.DeclaredMIME,
		document.DetectedMIME,
		document.SizeBytes,
		document.SHA256,
		document.PageCount,
		document.Status,
		document.IngestionKind,
		document.OriginalObjectOwner,
		document.CreatedByUserID,
		document.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func (t transaction) InsertProcessingJob(ctx context.Context, job ports.ProcessingJob) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO processing_jobs (
			id, tenant_id, document_id, kind, status, attempt_count, created_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		job.ID,
		job.TenantID,
		job.DocumentID,
		job.Kind,
		job.Status,
		job.AttemptCount,
		job.CreatedAt.UTC().Format(time.RFC3339Nano),
		job.Version,
	)
	if err != nil {
		return fmt.Errorf("insert processing job: %w", err)
	}
	return nil
}

func (t transaction) DeleteUnconfirmedDocument(ctx context.Context, tenantID, documentID string) error {
	result, err := t.tx.ExecContext(ctx, `
		DELETE FROM documents
		WHERE tenant_id = ? AND id = ?
		  AND NOT EXISTS (
		      SELECT 1
		      FROM claim_sets c
		      JOIN review_decisions r
		        ON r.tenant_id = c.tenant_id AND r.claim_set_id = c.id
		      WHERE c.tenant_id = documents.tenant_id
		        AND c.document_id = documents.id
		        AND r.action = 'confirm'
		  )
	`, tenantID, documentID)
	if err != nil {
		return fmt.Errorf("delete unconfirmed document: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) ListJobs(ctx context.Context, tenantID string, status *domain.JobStatus) ([]ports.JobSummary, error) {
	query := `
		SELECT j.id, j.document_id, d.original_name, d.ingestion_kind, d.detected_mime, j.status,
		       j.attempt_count, coalesce(j.error_code, ''), coalesce(j.safe_error_message, ''),
		       j.created_at, j.version
		FROM processing_jobs j
		JOIN documents d ON d.tenant_id = j.tenant_id AND d.id = j.document_id
		WHERE j.tenant_id = ?`
	arguments := []any{tenantID}
	if status != nil {
		query += " AND j.status = ?"
		arguments = append(arguments, *status)
	}
	query += " ORDER BY j.created_at DESC, j.id DESC LIMIT 200"
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	items := make([]ports.JobSummary, 0)
	for rows.Next() {
		item, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return items, nil
}

func (s *Store) GetJob(ctx context.Context, tenantID, jobID string) (ports.JobSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT j.id, j.document_id, d.original_name, d.ingestion_kind, d.detected_mime, j.status,
		       j.attempt_count, coalesce(j.error_code, ''), coalesce(j.safe_error_message, ''),
		       j.created_at, j.version
		FROM processing_jobs j
		JOIN documents d ON d.tenant_id = j.tenant_id AND d.id = j.document_id
		WHERE j.tenant_id = ? AND j.id = ?
	`, tenantID, jobID)
	item, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.JobSummary{}, domain.ErrNotFound
	}
	return item, err
}

func (s *Store) GetDocument(ctx context.Context, tenantID, documentID string) (ports.Document, error) {
	var document ports.Document
	var createdAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, storage_key, original_name, declared_mime, detected_mime,
		       size_bytes, sha256, page_count, status, ingestion_kind, original_object_owner,
		       created_by_user_id, created_at
		FROM documents
		WHERE tenant_id = ? AND id = ?
	`, tenantID, documentID).Scan(
		&document.ID,
		&document.TenantID,
		&document.StorageKey,
		&document.OriginalName,
		&document.DeclaredMIME,
		&document.DetectedMIME,
		&document.SizeBytes,
		&document.SHA256,
		&document.PageCount,
		&document.Status,
		&document.IngestionKind,
		&document.OriginalObjectOwner,
		&document.CreatedByUserID,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.Document{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.Document{}, fmt.Errorf("get document: %w", err)
	}
	document.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ports.Document{}, fmt.Errorf("parse document created_at: %w", err)
	}
	return document, nil
}

func (s *Store) GetDocumentObject(ctx context.Context, tenantID, documentID string) (ports.DocumentObject, error) {
	var object ports.DocumentObject
	err := s.db.QueryRowContext(ctx, `
		SELECT d.storage_key, d.original_name, d.detected_mime, j.status
		FROM documents d
		JOIN processing_jobs j ON j.tenant_id = d.tenant_id AND j.document_id = d.id
		WHERE d.tenant_id = ? AND d.id = ?
	`, tenantID, documentID).Scan(&object.StorageKey, &object.Name, &object.MIME, &object.ReviewState)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.DocumentObject{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.DocumentObject{}, fmt.Errorf("get document object: %w", err)
	}
	return object, nil
}

func (s *Store) GetDocumentPageObject(
	ctx context.Context,
	tenantID, documentID string,
	pageNumber int,
) (ports.DocumentPageObject, error) {
	var object ports.DocumentPageObject
	err := s.db.QueryRowContext(ctx, `
		SELECT p.derived_image_storage_key, p.page_number, j.status
		FROM document_pages p
		JOIN processing_jobs j
		  ON j.tenant_id = p.tenant_id AND j.document_id = p.document_id
		WHERE p.tenant_id = ? AND p.document_id = ? AND p.page_number = ?
	`, tenantID, documentID, pageNumber).Scan(
		&object.StorageKey,
		&object.PageNumber,
		&object.ReviewState,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.DocumentPageObject{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.DocumentPageObject{}, fmt.Errorf("get document page object: %w", err)
	}
	return object, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(source scanner) (ports.JobSummary, error) {
	var item ports.JobSummary
	var createdAt string
	if err := source.Scan(
		&item.ID,
		&item.DocumentID,
		&item.OriginalName,
		&item.IngestionKind,
		&item.DetectedMIME,
		&item.Status,
		&item.AttemptCount,
		&item.ErrorCode,
		&item.SafeErrorMessage,
		&createdAt,
		&item.Version,
	); err != nil {
		return ports.JobSummary{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ports.JobSummary{}, fmt.Errorf("parse job created_at: %w", err)
	}
	item.CreatedAt = parsed
	return item, nil
}
