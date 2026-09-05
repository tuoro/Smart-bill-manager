package reviews

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type manualDependencies struct {
	queue      ports.JobQueue
	normalizer ports.DocumentNormalizer
	objects    ports.ObjectStore
}

func (s Service) WithManualEntry(queue ports.JobQueue, normalizer ports.DocumentNormalizer, objects ports.ObjectStore) Service {
	s.manual = &manualDependencies{queue: queue, normalizer: normalizer, objects: objects}
	return s
}

type ManualReviewInput struct {
	ExpectedJobVersion int
	DocumentType       domain.DocumentType
	Reason             string
	IdempotencyKey     string
}

type ManualReviewResult struct {
	JobID      string `json:"job_id"`
	ClaimSetID string `json:"claim_set_id"`
	Replayed   bool   `json:"replayed"`
}

func (s Service) StartManualReview(ctx context.Context, tenant domain.TenantContext, jobID string, input ManualReviewInput) (ManualReviewResult, error) {
	var result ManualReviewResult
	if err := tenant.Require(domain.CapabilityClaimsReview); err != nil {
		return result, err
	}
	if err := domain.ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return result, err
	}
	reason, identity, err := domain.ManualReviewIdentity(jobID, input.ExpectedJobVersion, input.DocumentType, input.Reason)
	if err != nil {
		return result, err
	}
	if s.manual == nil {
		return result, domain.NewRuleError("manual_review_unavailable", "人工审核尚未装配", domain.ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var prepared []ports.NormalizedPage
	var documentID string
	err = s.tx.WithinReadCommittedTransaction(ctx, func(transaction ports.Transaction) error {
		state, err := transaction.LockManualReview(ctx, tenant.TenantID, jobID)
		if err != nil {
			return err
		}
		replay, err := transaction.FindManualReviewReplay(ctx, tenant.TenantID, input.IdempotencyKey)
		if err == nil {
			if replay.RequestHash != identity || replay.JobID != jobID {
				return domain.NewRuleError("idempotency_key_conflict", "同一幂等键不能用于不同接管请求", domain.ErrConflict)
			}
			result = ManualReviewResult{JobID: jobID, ClaimSetID: replay.ClaimSetID, Replayed: true}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if state.Status != domain.JobFailed || state.HasClaim {
			return domain.NewRuleError("job_not_manual_reviewable", "仅可接管尚未产生审核结果的失败单据", domain.ErrConflict)
		}
		if state.Version != input.ExpectedJobVersion {
			return domain.ErrVersionConflict
		}
		documentID = state.Document.ID
		if err := verifyManualObject(ctx, s.manual.objects, state.Document.StorageKey, state.Document.SHA256, ports.MaxDocumentBytes); err != nil {
			return err
		}
		auditID, err := s.ids.NewID()
		if err != nil {
			return err
		}
		if len(state.Pages) == 0 {
			prepared, err = s.manual.normalizer.Normalize(ctx, ports.ProcessingDocument{TenantID: tenant.TenantID, DocumentID: documentID,
				StorageKey: state.Document.StorageKey, MIME: state.Document.DetectedMIME, PageCount: state.Document.PageCount, PageSetID: auditID, MetadataOnly: true})
			if err != nil {
				return err
			}
			state.Pages = prepared
			for index := range state.Pages {
				state.Pages[index].ID, err = s.ids.NewID()
				if err != nil {
					return err
				}
				page := state.Pages[index]
				if err := transaction.InsertDocumentPages(ctx, []ports.DocumentPageRecord{{ID: page.ID, TenantID: tenant.TenantID, DocumentID: documentID,
					PageNumber: page.PageNumber, StorageKey: page.StorageKey, Width: page.Width, Height: page.Height, SHA256: page.SHA256,
					ProcessingVersion: "document-normalize/2", VisualFingerprint: page.VisualFingerprint, CreatedAt: s.clock.Now()}}); err != nil {
					return err
				}
			}
		}
		if len(state.Pages) != state.Document.PageCount {
			return domain.NewRuleError("manual_review_pages_incomplete", "原件页面不完整，无法进入人工审核", domain.ErrConflict)
		}
		for index, page := range state.Pages {
			if page.PageNumber != index+1 {
				return domain.ErrConflict
			}
			if err := verifyManualObject(ctx, s.manual.objects, page.StorageKey, page.SHA256, ports.MaxDerivedPageBytes); err != nil {
				return err
			}
		}
		validated, err := domain.EmptyManualClaim(input.DocumentType, state.Document.PageCount)
		if err != nil {
			return err
		}
		command, err := s.buildRevisionCommand(tenant, jobID, ports.ReviewSnapshot{DocumentID: documentID}, validated, nil)
		if err != nil {
			return err
		}
		if err := transaction.PersistManualReview(ctx, ports.ManualReviewCommand{Revision: command, ExpectedJobVersion: input.ExpectedJobVersion,
			Reason: reason, IdempotencyKey: input.IdempotencyKey, RequestHash: identity, AuditID: auditID}); err != nil {
			return err
		}
		result = ManualReviewResult{JobID: jobID, ClaimSetID: command.ClaimSet.ID}
		return nil
	})
	if err != nil && len(prepared) != 0 {
		cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		persisted, lookupErr := s.manual.queue.GetDocumentPages(cleanupCtx, tenant.TenantID, documentID)
		if lookupErr != nil {
			return result, errors.Join(err, fmt.Errorf("verify manual page compensation: %w", lookupErr))
		}
		used := make(map[string]bool, len(persisted))
		for _, page := range persisted {
			used[page.StorageKey] = true
		}
		unused := make([]ports.NormalizedPage, 0, len(prepared))
		for _, page := range prepared {
			if !used[page.StorageKey] {
				unused = append(unused, page)
			}
		}
		if cleanupErr := s.manual.normalizer.DeleteNormalized(cleanupCtx, unused); cleanupErr != nil {
			return result, errors.Join(err, cleanupErr)
		}
	}
	return result, err
}

func verifyManualObject(ctx context.Context, objects ports.ObjectStore, key, expectedHash string, maximum int64) error {
	reader, err := objects.Open(ctx, key)
	if err != nil {
		return domain.NewRuleError("manual_review_source_unavailable", "原件或页面无法读取，请检查文件存储", domain.ErrConflict)
	}
	hash := sha256.New()
	size, readErr := io.Copy(hash, io.LimitReader(reader, maximum+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || size > maximum || hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		return domain.NewRuleError("manual_review_source_changed", "原件或页面完整性校验失败", domain.ErrConflict)
	}
	return ctx.Err()
}
