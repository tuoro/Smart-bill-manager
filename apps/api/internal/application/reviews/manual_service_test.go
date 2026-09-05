package reviews

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type manualHarness struct {
	ports.Transaction
	ports.TransactionManager
	ports.JobQueue
	ports.ObjectStore
	state                                   ports.ManualReviewState
	replay                                  *ports.ManualReviewReplay
	command                                 *ports.ManualReviewCommand
	prepared, persisted, deleted            []ports.NormalizedPage
	normalizeErr, transactionErr, lookupErr error
	normalizations, insertions              int
}

func (h *manualHarness) WithinReadCommittedTransaction(ctx context.Context, operation func(ports.Transaction) error) error {
	if err := operation(h); err != nil {
		return err
	}
	return h.transactionErr
}
func (h *manualHarness) LockManualReview(context.Context, string, string) (ports.ManualReviewState, error) {
	return h.state, nil
}
func (h *manualHarness) FindManualReviewReplay(context.Context, string, string) (ports.ManualReviewReplay, error) {
	if h.replay != nil {
		return *h.replay, nil
	}
	return ports.ManualReviewReplay{}, domain.ErrNotFound
}
func (h *manualHarness) PersistManualReview(_ context.Context, command ports.ManualReviewCommand) error {
	h.command = &command
	return nil
}
func (h *manualHarness) InsertDocumentPages(_ context.Context, pages []ports.DocumentPageRecord) error {
	h.insertions += len(pages)
	return nil
}
func (h *manualHarness) GetDocumentPages(context.Context, string, string) ([]ports.NormalizedPage, error) {
	return h.persisted, h.lookupErr
}
func (h *manualHarness) Normalize(_ context.Context, document ports.ProcessingDocument) ([]ports.NormalizedPage, error) {
	if !document.MetadataOnly || document.PageSetID == "" {
		return nil, errors.New("manual normalization was not isolated")
	}
	h.normalizations++
	return h.prepared, h.normalizeErr
}
func (h *manualHarness) DeleteNormalized(_ context.Context, pages []ports.NormalizedPage) error {
	h.deleted = append(h.deleted, pages...)
	return nil
}
func (h *manualHarness) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewBufferString("synthetic-source")), nil
}

func newManualHarness() (*manualHarness, Service, domain.TenantContext, ManualReviewInput) {
	hash := sha256.Sum256([]byte("synthetic-source"))
	h := &manualHarness{state: ports.ManualReviewState{Status: domain.JobFailed, Version: 3,
		Document: ports.Document{ID: "document", StorageKey: "original", SHA256: hex.EncodeToString(hash[:]), PageCount: 1, DetectedMIME: "image/png"}}}
	h.prepared = []ports.NormalizedPage{{PageImage: ports.PageImage{PageNumber: 1, SHA256: h.state.Document.SHA256}, StorageKey: "manual-page"}}
	service := NewService(nil, h, system.IDGenerator{}, fixedClock{now: time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)}).WithManualEntry(h, h, h)
	return h, service, domain.TenantContext{TenantID: "tenant", UserID: "reviewer", Role: domain.RoleOwner}, ManualReviewInput{ExpectedJobVersion: 3, DocumentType: domain.DocumentPayment, Reason: "人工核对", IdempotencyKey: "manual-unit-request"}
}

func TestManualStartBuildsOnlyExplicitUserClaim(t *testing.T) {
	for _, kind := range []domain.DocumentType{domain.DocumentPayment, domain.DocumentInvoice, domain.DocumentTrip} {
		t.Run(string(kind), func(t *testing.T) {
			h, service, tenant, input := newManualHarness()
			input.DocumentType = kind
			result, err := service.StartManualReview(context.Background(), tenant, "job", input)
			if err != nil {
				t.Fatal(err)
			}
			if result.ClaimSetID == "" || result.Replayed || h.command == nil || h.normalizations != 1 || h.insertions != 1 {
				t.Fatal("manual claim was not prepared once")
			}
			claim := h.command.Revision.ClaimSet
			if claim.OriginAiRunID != "" || claim.Status != domain.ClaimBlocked || claim.DocumentType != kind {
				t.Fatal("manual claim invented AI origin or success")
			}
			for _, field := range h.command.Revision.Fields {
				if field.Source != "user" || field.SourceUserID != tenant.UserID {
					t.Fatal("field lost user provenance")
				}
			}
		})
	}
}

func TestManualStartReusesPagesAndReplaysWithoutPreparingAgain(t *testing.T) {
	h, service, tenant, input := newManualHarness()
	h.state.Pages = h.prepared
	first, err := service.StartManualReview(context.Background(), tenant, "job", input)
	if err != nil {
		t.Fatal(err)
	}
	if h.normalizations != 0 || h.insertions != 0 {
		t.Fatal("existing pages were normalized again")
	}
	h.state.Status, h.state.HasClaim = domain.JobBlocked, true
	h.replay = &ports.ManualReviewReplay{JobID: "job", ClaimSetID: first.ClaimSetID, RequestHash: h.command.RequestHash}
	second, err := service.StartManualReview(context.Background(), tenant, "job", input)
	if err != nil || !second.Replayed || second.ClaimSetID != first.ClaimSetID {
		t.Fatal("identical request did not replay")
	}
	input.Reason = "不同理由"
	if _, err := service.StartManualReview(context.Background(), tenant, "job", input); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("different replay request accepted")
	}
}

func TestManualStartRejectsChangedStateBeforeObjectPreparation(t *testing.T) {
	for _, change := range []func(*manualHarness){
		func(h *manualHarness) { h.state.HasClaim = true },
		func(h *manualHarness) { h.state.Status = domain.JobProcessing },
		func(h *manualHarness) { h.state.Version++ },
		func(h *manualHarness) { h.state.Document.SHA256 = "changed" },
	} {
		h, service, tenant, input := newManualHarness()
		change(h)
		if _, err := service.StartManualReview(context.Background(), tenant, "job", input); err == nil {
			t.Fatal("invalid state accepted")
		}
		if h.normalizations != 0 || h.command != nil {
			t.Fatal("invalid state wrote a claim or prepared pages")
		}
	}
}

func TestManualStartCompensatesOnlyUnreferencedPreparedObjects(t *testing.T) {
	for _, phase := range []string{"normalize", "transaction", "committed", "unknown"} {
		t.Run(phase, func(t *testing.T) {
			h, service, tenant, input := newManualHarness()
			failure := errors.New("synthetic_manual_failure")
			if phase == "normalize" {
				h.normalizeErr = failure
			} else {
				h.transactionErr = failure
			}
			if phase == "committed" {
				h.persisted = h.prepared
			}
			if phase == "unknown" {
				h.lookupErr = errors.New("synthetic_lookup_failure")
			}
			if _, err := service.StartManualReview(context.Background(), tenant, "job", input); !errors.Is(err, failure) {
				t.Fatal("failure was hidden")
			}
			wantDeleted := phase == "normalize" || phase == "transaction"
			if (len(h.deleted) == 1) != wantDeleted {
				t.Fatal("compensation deleted referenced or uncertain objects")
			}
			if len(h.deleted) > 0 && h.deleted[0].StorageKey != "manual-page" {
				t.Fatal("compensation reached an unrelated object")
			}
		})
	}
}
