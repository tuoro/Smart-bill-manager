package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type DeletionService struct {
	repository ports.DocumentDeletionRepository
	trash      ports.RecoverableDeletionStore
	tx         ports.TransactionManager
	ids        ports.IDGenerator
	clock      ports.Clock
}

func NewDeletionService(
	repository ports.DocumentDeletionRepository,
	trash ports.RecoverableDeletionStore,
	tx ports.TransactionManager,
	ids ports.IDGenerator,
	clock ports.Clock,
) DeletionService {
	return DeletionService{repository: repository, trash: trash, tx: tx, ids: ids, clock: clock}
}

func (s DeletionService) Delete(
	ctx context.Context,
	tenant domain.TenantContext,
	documentID, requestID string,
) error {
	if err := tenant.Require(domain.CapabilityResourcesDelete); err != nil {
		return err
	}
	if documentID == "" || requestID == "" {
		return domain.ErrInvalidInput
	}
	plan, err := s.repository.PrepareUnconfirmedDocumentDeletion(ctx, tenant.TenantID, documentID)
	if err != nil {
		return err
	}
	tombstoneID, err := s.ids.NewID()
	if err != nil {
		return err
	}
	objectHashesJSON, err := json.Marshal(plan.ObjectHashes)
	if err != nil {
		return fmt.Errorf("encode document object hashes: %w", err)
	}
	resourceCountsJSON, err := json.Marshal(plan.ResourceCounts)
	if err != nil {
		return fmt.Errorf("encode document resource counts: %w", err)
	}
	resourceHash := sha256.Sum256([]byte(tenant.TenantID + "\x00" + documentID))
	command := ports.DocumentDeleteCommand{
		TenantID: tenant.TenantID, DocumentID: documentID, ActorUserID: tenant.UserID,
		ExpectedJobVersion: plan.JobVersion,
		TombstoneID:        tombstoneID, ResourceIDHash: hex.EncodeToString(resourceHash[:]),
		ObjectHashesJSON: string(objectHashesJSON), ResourceCountsJSON: string(resourceCountsJSON),
		RequestID: requestID, DeletedAt: s.clock.Now(),
	}
	if err := s.trash.StageDeletion(ctx, tombstoneID, plan.StorageKeys); err != nil {
		return err
	}
	if err := s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.DeleteDocumentAggregate(ctx, command)
	}); err != nil {
		restoreErr := s.trash.RestoreDeletion(context.WithoutCancel(ctx), tombstoneID)
		if restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore document objects: %w", restoreErr))
		}
		return err
	}
	if err := s.trash.PurgeDeletion(context.WithoutCancel(ctx), tombstoneID); err != nil {
		return fmt.Errorf("purge deleted document objects: %w", err)
	}
	return nil
}

func (s DeletionService) Reconcile(ctx context.Context) error {
	pending, err := s.trash.PendingDeletions(ctx)
	if err != nil {
		return err
	}
	for _, deletionID := range pending {
		committed, err := s.repository.DeletionTombstoneExists(ctx, deletionID)
		if err != nil {
			return err
		}
		if committed {
			if err := s.trash.PurgeDeletion(ctx, deletionID); err != nil {
				return err
			}
			continue
		}
		if err := s.trash.RestoreDeletion(ctx, deletionID); err != nil {
			return err
		}
	}
	return nil
}
