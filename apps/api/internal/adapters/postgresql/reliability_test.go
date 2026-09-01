package postgresqladapter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestPersistentQueueLeasesFiftyJobsExactlyOnce(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	owner := queueOwner(now)
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		for index := 0; index < 50; index++ {
			documentID := fmt.Sprintf("document-%02d", index)
			if err := transaction.InsertDocument(ctx, ports.Document{
				ID: documentID, TenantID: owner.TenantID,
				StorageKey:   fmt.Sprintf("tenants/%s/documents/%s/original", owner.TenantID, documentID),
				OriginalName: fmt.Sprintf("receipt-%02d.png", index), DeclaredMIME: "image/png", DetectedMIME: "image/png",
				SizeBytes: 100, SHA256: fmt.Sprintf("%064x", index+1), PageCount: 1,
				Status: "stored", IngestionKind: domain.DocumentIngestionUpload,
				OriginalObjectOwner: domain.DocumentObjectOwnerDocument,
				CreatedByUserID:     owner.UserID, CreatedAt: now.Add(time.Duration(index) * time.Millisecond),
			}); err != nil {
				return err
			}
			if err := transaction.InsertProcessingJob(ctx, ports.ProcessingJob{
				ID: fmt.Sprintf("job-%02d", index), TenantID: owner.TenantID, DocumentID: documentID,
				Kind: "document_process", Status: domain.JobQueued, CreatedAt: now.Add(time.Duration(index) * time.Millisecond), Version: 1,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	leasedJobs := make(chan ports.LeasedJob, 50)
	errorsFound := make(chan error, 50)
	var workers sync.WaitGroup
	for index := 0; index < 50; index++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			job, err := store.LeaseNextJob(ctx, fmt.Sprintf("worker-%02d", worker), now, now.Add(165*time.Second))
			if err != nil {
				errorsFound <- err
				return
			}
			leasedJobs <- job
		}(index)
	}
	workers.Wait()
	close(leasedJobs)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent lease: %v", err)
	}
	seen := make(map[string]struct{}, 50)
	for job := range leasedJobs {
		if _, duplicate := seen[job.ID]; duplicate {
			t.Fatalf("job leased twice: %s", job.ID)
		}
		seen[job.ID] = struct{}{}
		if job.AttemptCount != 1 || job.LeaseOwner == "" {
			t.Fatalf("leased job = %#v", job)
		}
	}
	if _, err := store.LeaseNextJob(ctx, "worker", now, now.Add(165*time.Second)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("queue after 50 leases = %v", err)
	}
	var jobs, documents, validLeases int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT count(*),
		       (SELECT count(*) FROM documents WHERE tenant_id = ?),
		       sum(CASE WHEN status = 'processing' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL THEN 1 ELSE 0 END)
		FROM processing_jobs WHERE tenant_id = ?
	`, owner.TenantID, owner.TenantID).Scan(&jobs, &documents, &validLeases); err != nil {
		t.Fatal(err)
	}
	if jobs != 50 || documents != 50 || validLeases != 50 || len(seen) != 50 {
		t.Fatalf("queue counts = jobs:%d documents:%d leases:%d unique:%d", jobs, documents, validLeases, len(seen))
	}
}

func TestReadCommittedDocumentTransactionsSurviveConcurrentUniqueInserts(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC)
	owner := queueOwner(now)
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}

	const insertCount = 8
	start := make(chan struct{})
	errorsFound := make(chan error, insertCount)
	var workers sync.WaitGroup
	for index := 0; index < insertCount; index++ {
		workers.Add(1)
		go func(sequence int) {
			defer workers.Done()
			<-start
			documentID := fmt.Sprintf("concurrent-document-%02d", sequence)
			sha256 := fmt.Sprintf("%064x", sequence+1000)
			err := store.WithinReadCommittedTransaction(ctx, func(transaction ports.Transaction) error {
				if _, err := transaction.FindDocumentIDBySHA(ctx, owner.TenantID, sha256); !errors.Is(err, domain.ErrNotFound) {
					return fmt.Errorf("unique document lookup: %w", err)
				}
				if err := transaction.InsertDocument(ctx, ports.Document{
					ID: documentID, TenantID: owner.TenantID,
					StorageKey:   fmt.Sprintf("tenants/%s/documents/%s/original", owner.TenantID, documentID),
					OriginalName: fmt.Sprintf("concurrent-%02d.png", sequence), DeclaredMIME: "image/png", DetectedMIME: "image/png",
					SizeBytes: 100, SHA256: sha256, PageCount: 1, Status: "stored",
					IngestionKind: domain.DocumentIngestionUpload, OriginalObjectOwner: domain.DocumentObjectOwnerDocument,
					CreatedByUserID: owner.UserID, CreatedAt: now,
				}); err != nil {
					return err
				}
				return transaction.InsertProcessingJob(ctx, ports.ProcessingJob{
					ID: fmt.Sprintf("concurrent-job-%02d", sequence), TenantID: owner.TenantID, DocumentID: documentID,
					Kind: "document_process", Status: domain.JobQueued, CreatedAt: now, Version: 1,
				})
			})
			if err != nil {
				errorsFound <- err
			}
		}(index)
	}
	close(start)
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent unique insert: %v", err)
	}
	var documents, jobs int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT count(*), (SELECT count(*) FROM processing_jobs WHERE tenant_id = ?)
		FROM documents WHERE tenant_id = ?
	`, owner.TenantID, owner.TenantID).Scan(&documents, &jobs); err != nil {
		t.Fatal(err)
	}
	if documents != insertCount || jobs != insertCount {
		t.Fatalf("concurrent inserts = documents:%d jobs:%d", documents, jobs)
	}
}

func TestProviderActivationRequiresSchemaIdentity(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	owner := queueOwner(now)
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	config := ports.ProviderConfig{
		ID: "provider", TenantID: owner.TenantID, BaseURL: "https://provider.example/v1",
		EncryptedAPIKey: []byte("encrypted"), Model: "model", CapabilityStatus: "passed",
		OutputMode: ports.ProviderOutputModeJSONSchema,
		Active:     true, Version: 1, SafeFingerprint: "fingerprint",
		CreatedByUserID: owner.UserID, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.InsertProviderConfig(ctx, config)
	}); err == nil {
		t.Fatal("active Provider config without a schema identity was accepted")
	}
	config.CapabilitySchemaVersion = "bill-visible-text-provider/2"
	config.CapabilitySchemaSHA256 = fmt.Sprintf("%064x", 1)
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.InsertProviderConfig(ctx, config)
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.RecordProviderCapability(
			ctx,
			owner.TenantID,
			config.ID,
			config.Version,
			"failed",
			"synthetic capability failure",
			ports.ProviderSchemaIdentity{Version: config.CapabilitySchemaVersion, SHA256: config.CapabilitySchemaSHA256},
			now.Add(time.Minute),
		)
	}); err != nil {
		t.Fatal(err)
	}
	var active bool
	var status string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT active, capability_status FROM provider_configs WHERE tenant_id = ? AND id = ?
	`, owner.TenantID, config.ID).Scan(&active, &status); err != nil {
		t.Fatal(err)
	}
	if active || status != "failed" {
		t.Fatalf("failed re-detection state = active:%t status:%s", active, status)
	}
}

func TestExpiredLeaseIsRecoveredAndRunningAIRunIsClosed(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	owner := queueOwner(now)
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		if err := transaction.InsertProviderConfig(ctx, ports.ProviderConfig{
			ID: "provider", TenantID: owner.TenantID, BaseURL: "https://provider.example/v1",
			EncryptedAPIKey: []byte("encrypted"), Model: "model", CapabilityStatus: "passed",
			OutputMode:              ports.ProviderOutputModeJSONSchema,
			CapabilitySchemaVersion: "bill-visible-text-provider/2", CapabilitySchemaSHA256: fmt.Sprintf("%064x", 1),
			Version: 1, SafeFingerprint: "fingerprint", CreatedByUserID: owner.UserID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		if err := transaction.InsertDocument(ctx, ports.Document{
			ID: "document", TenantID: owner.TenantID, StorageKey: "tenants/tenant/documents/document/original",
			OriginalName: "receipt.png", DeclaredMIME: "image/png", DetectedMIME: "image/png",
			SizeBytes: 100, SHA256: fmt.Sprintf("%064x", 1), PageCount: 1,
			Status: "stored", IngestionKind: domain.DocumentIngestionUpload,
			OriginalObjectOwner: domain.DocumentObjectOwnerDocument,
			CreatedByUserID:     owner.UserID, CreatedAt: now,
		}); err != nil {
			return err
		}
		return transaction.InsertProcessingJob(ctx, ports.ProcessingJob{
			ID: "job", TenantID: owner.TenantID, DocumentID: "document", Kind: "document_process",
			Status: domain.JobQueued, CreatedAt: now, Version: 1,
		})
	}); err != nil {
		t.Fatal(err)
	}
	first, err := store.LeaseNextJob(ctx, "worker-before-restart", now, now.Add(165*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.InsertAiRun(ctx, ports.AiRun{
			ID: "run", TenantID: owner.TenantID, JobID: first.ID, ProviderConfigID: "provider",
			ProviderConfigVersion: 1, ProviderConfigFingerprint: "fingerprint", Model: "model",
			PromptVersion: "bill-visible-text-cn/2", ExtractionSchemaVersion: "bill-visible-text/2",
			ProviderSchemaVersion: "bill-visible-text-provider/2", ProviderSchemaSHA256: fmt.Sprintf("%064x", 1),
			ClaimSchemaVersion: "document-claim/3", ClaimMapperVersion: "claim-mapper/4",
			InputProcessingVersion: "document-normalize/1", Outcome: "running", StartedAt: now,
		})
	}); err != nil {
		t.Fatal(err)
	}
	recoveredAt := now.Add(166 * time.Second)
	recovered, err := store.LeaseNextJob(ctx, "worker-after-restart", recoveredAt, recoveredAt.Add(165*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID != first.ID || recovered.AttemptCount != 2 || recovered.LeaseOwner != "worker-after-restart" {
		t.Fatalf("recovered job = %#v", recovered)
	}
	var outcome, errorCode string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT outcome, error_code FROM ai_runs WHERE tenant_id = ? AND id = 'run'
	`, owner.TenantID).Scan(&outcome, &errorCode); err != nil {
		t.Fatal(err)
	}
	if outcome != "failed" || errorCode != "lease_expired" {
		t.Fatalf("expired AI run = %s/%s", outcome, errorCode)
	}
}

func queueOwner(now time.Time) ports.BootstrapOwner {
	return ports.BootstrapOwner{
		UserID: "owner", TenantID: "tenant", Email: "owner@example.test", PasswordHash: "test-only",
		DisplayName: "Owner", TenantName: "Tenant", DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC", CreatedAt: now,
	}
}
