package documents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type UploadInput struct {
	Tenant domain.TenantContext
	Name   string
	MIME   string
	Source io.Reader
}

type UploadResult struct {
	DocumentID string
	JobID      string
	Status     domain.JobStatus
	SHA256     string
}

type UploadService struct {
	objects   ports.ObjectStore
	inspector ports.DocumentInspector
	tx        ports.TransactionManager
	ids       ports.IDGenerator
	clock     ports.Clock
}

func NewUploadService(
	objects ports.ObjectStore,
	inspector ports.DocumentInspector,
	tx ports.TransactionManager,
	ids ports.IDGenerator,
	clock ports.Clock,
) UploadService {
	return UploadService{objects: objects, inspector: inspector, tx: tx, ids: ids, clock: clock}
}

func (s UploadService) Execute(ctx context.Context, input UploadInput) (UploadResult, error) {
	if err := input.Tenant.Require(domain.CapabilityDocumentsProcess); err != nil {
		return UploadResult{}, err
	}
	prepared, err := PrepareUpload(ctx, s.objects, s.inspector, input)
	if err != nil {
		return UploadResult{}, err
	}
	name, staged, inspection := prepared.Name, prepared.Staged, prepared.Inspection
	committed := false
	defer func() {
		if !committed {
			_ = s.objects.Abort(context.WithoutCancel(ctx), staged)
		}
	}()
	documentID, err := s.ids.NewID()
	if err != nil {
		return UploadResult{}, fmt.Errorf("generate document id: %w", err)
	}
	jobID, err := s.ids.NewID()
	if err != nil {
		return UploadResult{}, fmt.Errorf("generate job id: %w", err)
	}
	storageKey := "tenants/" + input.Tenant.TenantID + "/documents/" + documentID + "/original"
	now := s.clock.Now()
	document := ports.Document{
		ID:                  documentID,
		TenantID:            input.Tenant.TenantID,
		StorageKey:          storageKey,
		OriginalName:        name,
		DeclaredMIME:        input.MIME,
		DetectedMIME:        inspection.DetectedMIME,
		SizeBytes:           staged.Size,
		SHA256:              staged.SHA256,
		PageCount:           inspection.PageCount,
		Status:              "stored",
		IngestionKind:       domain.DocumentIngestionUpload,
		OriginalObjectOwner: domain.DocumentObjectOwnerDocument,
		CreatedByUserID:     input.Tenant.UserID,
		CreatedAt:           now,
	}
	job := ports.ProcessingJob{
		ID:           jobID,
		TenantID:     input.Tenant.TenantID,
		DocumentID:   documentID,
		Kind:         "document_process",
		Status:       domain.JobQueued,
		AttemptCount: 0,
		CreatedAt:    now,
		Version:      1,
	}
	err = s.tx.WithinReadCommittedTransaction(ctx, func(transaction ports.Transaction) error {
		existingID, findErr := transaction.FindDocumentIDBySHA(ctx, input.Tenant.TenantID, staged.SHA256)
		if findErr == nil {
			return &domain.DuplicateDocumentError{DocumentID: existingID}
		}
		if !errors.Is(findErr, domain.ErrNotFound) {
			return findErr
		}
		if err := transaction.InsertDocument(ctx, document); err != nil {
			return err
		}
		return transaction.InsertProcessingJob(ctx, job)
	})
	if err != nil {
		var existingID string
		lookupErr := s.tx.WithinReadCommittedTransaction(ctx, func(transaction ports.Transaction) error {
			var findErr error
			existingID, findErr = transaction.FindDocumentIDBySHA(ctx, input.Tenant.TenantID, staged.SHA256)
			return findErr
		})
		if lookupErr == nil {
			return UploadResult{}, &domain.DuplicateDocumentError{DocumentID: existingID}
		}
		return UploadResult{}, err
	}
	if err := s.objects.Commit(ctx, staged, storageKey); err != nil {
		compensationErr := s.tx.WithinReadCommittedTransaction(context.WithoutCancel(ctx), func(transaction ports.Transaction) error {
			return transaction.DeleteUnconfirmedDocument(context.WithoutCancel(ctx), input.Tenant.TenantID, documentID)
		})
		if compensationErr != nil {
			return UploadResult{}, fmt.Errorf("commit object: %w; compensate metadata: %v", err, compensationErr)
		}
		return UploadResult{}, fmt.Errorf("commit object: %w", err)
	}
	committed = true
	return UploadResult{DocumentID: documentID, JobID: jobID, Status: domain.JobQueued, SHA256: staged.SHA256}, nil
}

type PreparedUpload struct {
	Name       string
	Staged     ports.StagedObject
	Inspection ports.DocumentInspection
}

// 普通识别上传和发票辅助上传共享相同的文件边界，不共享任务创建语义。
func PrepareUpload(ctx context.Context, objects ports.ObjectStore, inspector ports.DocumentInspector, input UploadInput) (PreparedUpload, error) {
	var result PreparedUpload
	if input.Source == nil {
		return result, domain.NewRuleError("document_required", "请选择一个文件", domain.ErrInvalidInput)
	}
	var err error
	result.Name, err = NormalizeDocumentName(input.Name)
	if err != nil {
		return result, err
	}
	result.Staged, err = objects.Stage(ctx, input.Source, ports.MaxDocumentBytes)
	if err != nil {
		return result, err
	}
	result.Inspection, err = inspector.InspectStaged(ctx, result.Staged, result.Name, input.MIME)
	if err != nil {
		return result, errors.Join(err, objects.Abort(context.WithoutCancel(ctx), result.Staged))
	}
	return result, nil
}

func NormalizeDocumentName(value string) (string, error) {
	value = filepath.Base(strings.TrimSpace(norm.NFKC.String(value)))
	if value == "." || value == "" || len([]rune(value)) > 200 {
		return "", domain.NewRuleError("invalid_document_name", "文件名长度必须为 1–200 个字符", domain.ErrInvalidInput)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return "", domain.NewRuleError("invalid_document_name", "文件名包含不允许的控制字符", domain.ErrInvalidInput)
		}
	}
	return value, nil
}
