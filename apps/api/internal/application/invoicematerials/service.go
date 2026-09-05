package invoicematerials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type Service struct {
	repository   ports.InvoiceMaterialRepository
	tx           ports.TransactionManager
	objects      ports.ObjectStore
	publications ports.MaterialPublicationStore
	inspector    ports.DocumentInspector
	ids          ports.IDGenerator
	clock        ports.Clock
}

func NewService(repository ports.InvoiceMaterialRepository, tx ports.TransactionManager, objects ports.ObjectStore,
	publications ports.MaterialPublicationStore, inspector ports.DocumentInspector, ids ports.IDGenerator, clock ports.Clock) Service {
	return Service{repository: repository, tx: tx, objects: objects, publications: publications, inspector: inspector, ids: ids, clock: clock}
}

func (s Service) Workspace(ctx context.Context, tenant domain.TenantContext, invoiceID string) (ports.InvoiceMaterialWorkspace, error) {
	if err := domain.RequireInvoiceMaterials(tenant); err != nil {
		return ports.InvoiceMaterialWorkspace{}, err
	}
	return s.repository.GetInvoiceMaterials(ctx, tenant.TenantID, invoiceID)
}

func (s Service) Change(ctx context.Context, tenant domain.TenantContext, input domain.InvoiceMaterialRequest, requestID string) (ports.InvoiceMaterialResult, error) {
	if err := domain.RequireInvoiceMaterials(tenant); err != nil {
		return ports.InvoiceMaterialResult{}, err
	}
	if input.Action == "upload" {
		return ports.InvoiceMaterialResult{}, domain.ErrInvalidInput
	}
	command, err := s.command(tenant, input, requestID)
	if err != nil {
		return ports.InvoiceMaterialResult{}, err
	}
	return s.change(ctx, command, nil)
}

func (s Service) Upload(ctx context.Context, tenant domain.TenantContext, input domain.InvoiceMaterialRequest, file documents.UploadInput, requestID string) (result ports.InvoiceMaterialResult, resultErr error) {
	if err := domain.RequireInvoiceMaterials(tenant); err != nil {
		return result, err
	}
	if input.Action != "upload" || input.DocumentID != "" || input.LinkID != "" || input.UploadSHA256 != "" || input.UploadName != "" || input.UploadMIME != "" {
		return result, domain.ErrInvalidInput
	}
	prepared, err := documents.PrepareUpload(ctx, s.objects, s.inspector, file)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, s.objects.Abort(context.WithoutCancel(ctx), prepared.Staged))
	}()
	input.UploadSHA256, input.UploadName, input.UploadMIME = prepared.Staged.SHA256, prepared.Name, prepared.Inspection.DetectedMIME
	command, err := s.command(tenant, input, requestID)
	if err != nil {
		return result, err
	}
	documentID, err := s.ids.NewID()
	if err != nil {
		return result, err
	}
	publicationID, err := s.ids.NewID()
	if err != nil {
		return result, err
	}
	key := "tenants/" + tenant.TenantID + "/documents/" + documentID + "/original"
	publication := ports.MaterialPublication{ID: publicationID, TenantID: tenant.TenantID, DocumentID: documentID, StorageKey: key, Staged: prepared.Staged}
	command.UploadDocument = &ports.Document{ID: documentID, TenantID: tenant.TenantID, StorageKey: key, OriginalName: prepared.Name,
		DeclaredMIME: file.MIME, DetectedMIME: prepared.Inspection.DetectedMIME, SizeBytes: prepared.Staged.Size, SHA256: prepared.Staged.SHA256,
		PageCount: prepared.Inspection.PageCount, Status: "stored", IngestionKind: domain.DocumentIngestionUpload,
		OriginalObjectOwner: domain.DocumentObjectOwnerDocument, CreatedByUserID: tenant.UserID, CreatedAt: command.CreatedAt}
	result, changeErr := s.change(ctx, command, &publication)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	// 不猜测 Commit 的结果；同一发布锁释放后，下一语句读取已最终确定的身份。
	cleanupErr := s.reconcileOne(cleanupCtx, publication)
	return result, errors.Join(changeErr, cleanupErr)
}

func (s Service) command(tenant domain.TenantContext, input domain.InvoiceMaterialRequest, requestID string) (ports.InvoiceMaterialCommand, error) {
	canonical, hash, err := domain.CanonicalInvoiceMaterialRequest(input)
	if err != nil {
		return ports.InvoiceMaterialCommand{}, err
	}
	ids := make([]string, 3)
	for index := range ids {
		ids[index], err = s.ids.NewID()
		if err != nil {
			return ports.InvoiceMaterialCommand{}, err
		}
	}
	return ports.InvoiceMaterialCommand{InvoiceMaterialRequest: canonical, TenantID: tenant.TenantID, ActorUserID: tenant.UserID,
		RequestHash: hash, DecisionID: ids[0], NewLinkID: ids[1], AuditEventID: ids[2], RequestID: requestID, CreatedAt: s.clock.Now()}, nil
}

func (s Service) change(ctx context.Context, command ports.InvoiceMaterialCommand, publication *ports.MaterialPublication) (ports.InvoiceMaterialResult, error) {
	var result ports.InvoiceMaterialResult
	err := s.tx.WithinReadCommittedTransaction(ctx, func(tx ports.Transaction) error {
		if publication != nil {
			if err := tx.LockMaterialPublication(ctx, publication.ID); err != nil {
				return err
			}
			if err := s.publications.RecordMaterialPublication(ctx, *publication); err != nil {
				return err
			}
			if err := s.objects.Commit(ctx, publication.Staged, publication.StorageKey); err != nil {
				return err
			}
		}
		var err error
		result, err = tx.ChangeInvoiceMaterial(ctx, command)
		if err != nil {
			return err
		}
		if command.Action != "remove" {
			return s.verifyObject(ctx, result.Document.StorageKey, result.Document.SHA256, result.Document.SizeBytes)
		}
		return nil
	})
	return result, err
}

func (s Service) verifyObject(ctx context.Context, key, expectedHash string, expectedSize int64) error {
	body, err := s.objects.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("open invoice material object: %w", err)
	}
	hash := sha256.New()
	written, readErr := copyMaterial(ctx, hash, io.LimitReader(body, ports.MaxDocumentBytes+1))
	if err := errors.Join(readErr, body.Close()); err != nil {
		return err
	}
	if written != expectedSize || hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		return domain.NewRuleError("invoice_material_object_changed", "辅助材料原件缺失或已改变，请核对文件存储", domain.ErrConflict)
	}
	return nil
}

func copyMaterial(ctx context.Context, target io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 64*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			count, err := target.Write(buffer[:n])
			written += int64(count)
			if err != nil {
				return written, err
			}
			if count != n {
				return written, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func (s Service) Reconcile(ctx context.Context) error {
	for {
		pending, err := s.publications.PendingMaterialPublications(ctx, 100)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		for _, publication := range pending {
			if err := s.reconcileOne(ctx, publication); err != nil {
				return err
			}
		}
	}
}

func (s Service) reconcileOne(ctx context.Context, publication ports.MaterialPublication) error {
	return s.tx.WithinReadCommittedTransaction(ctx, func(tx ports.Transaction) error {
		if err := tx.LockMaterialPublication(ctx, publication.ID); err != nil {
			return err
		}
		actual, err := s.publications.GetMaterialPublication(ctx, publication.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if actual != publication {
			return errors.New("material publication identity changed")
		}
		committed, err := tx.MaterialPublicationCommitted(ctx, publication)
		if err != nil {
			return err
		}
		if committed {
			if err := s.verifyObject(ctx, publication.StorageKey, publication.Staged.SHA256, publication.Staged.Size); err != nil {
				return err
			}
		} else {
			if err := s.verifyObject(ctx, publication.StorageKey, publication.Staged.SHA256, publication.Staged.Size); err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
			if err := s.objects.Delete(ctx, publication.StorageKey); err != nil {
				return err
			}
		}
		if err := s.objects.Abort(ctx, publication.Staged); err != nil {
			return err
		}
		return s.publications.FinishMaterialPublication(ctx, publication.ID)
	})
}
