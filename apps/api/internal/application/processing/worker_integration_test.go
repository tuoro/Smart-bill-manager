package processing

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/sqlite"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestWorkerPersistsRetryAttemptsAndReviewClaim(t *testing.T) {
	fixture := newWorkerFixture(t)
	var calls atomic.Int32
	extractor := fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		if calls.Add(1) == 1 {
			return ports.BillExtractionResult{}, &ports.ProviderCallError{
				Code: "provider_unavailable", SafeMessage: "Provider 暂时不可用", Retryable: true,
			}
		}
		return ports.BillExtractionResult{
			Envelope: paymentExtractionEnvelope(), ResponseHash: "response-hash",
			InputTokens: 10, OutputTokens: 20, Latency: 50 * time.Millisecond,
		}, nil
	}}
	worker := fixture.worker(t, extractor)
	if err := worker.ProcessOne(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), fixture.tenant.TenantID, fixture.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobNeedsReview || job.AttemptCount != 2 || calls.Load() != 2 {
		t.Fatalf("job/calls = %#v / %d", job, calls.Load())
	}
	var pages, runs, claims, failedRuns int
	if err := fixture.store.DB().QueryRowContext(context.Background(), `
		SELECT (SELECT count(*) FROM document_pages WHERE tenant_id = ?),
		       (SELECT count(*) FROM ai_runs WHERE tenant_id = ?),
		       (SELECT count(*) FROM claim_sets WHERE tenant_id = ?),
		       (SELECT count(*) FROM ai_runs WHERE tenant_id = ? AND outcome = 'failed')
	`,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
		fixture.tenant.TenantID,
	).Scan(&pages, &runs, &claims, &failedRuns); err != nil {
		t.Fatal(err)
	}
	if pages != 1 || runs != 2 || claims != 1 || failedRuns != 1 {
		t.Fatalf("processing persistence = pages:%d runs:%d claims:%d failed:%d", pages, runs, claims, failedRuns)
	}
}

func TestWorkerCancellationStopsBeforeClaimPersistence(t *testing.T) {
	fixture := newWorkerFixture(t)
	started := make(chan struct{})
	extractor := fakeExtractor{execute: func(ctx context.Context) (ports.BillExtractionResult, error) {
		close(started)
		<-ctx.Done()
		return ports.BillExtractionResult{}, &ports.ProviderCallError{
			Code: "cancelled", SafeMessage: "任务已取消", Cause: ctx.Err(),
		}
	}}
	worker := fixture.worker(t, extractor)
	finished := make(chan error, 1)
	go func() { finished <- worker.ProcessOne(context.Background(), fixture.job) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider execution did not start")
	}
	actions := documents.NewActionService(fixture.store, fixture.store, system.IDGenerator{}, workerClock{now: fixture.now.Add(time.Minute)})
	if _, err := actions.Cancel(context.Background(), fixture.tenant, fixture.job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled worker did not stop")
	}
	job, err := fixture.store.GetJob(context.Background(), fixture.tenant.TenantID, fixture.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobCancelled {
		t.Fatalf("cancelled job status = %s", job.Status)
	}
	var claims int
	if err := fixture.store.DB().QueryRowContext(context.Background(), `
		SELECT count(*) FROM claim_sets WHERE tenant_id = ?
	`, fixture.tenant.TenantID).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if claims != 0 {
		t.Fatalf("claims after cancellation = %d", claims)
	}
}

func TestWorkerRunLeasesQueuedJobAndStopsCleanly(t *testing.T) {
	fixture := newWorkerFixture(t)
	if _, err := fixture.store.DB().Exec(
		"UPDATE processing_jobs SET status = 'queued', attempt_count = 0, lease_owner = NULL, lease_expires_at = NULL, started_at = NULL WHERE id = ?",
		fixture.job.ID,
	); err != nil {
		t.Fatal(err)
	}
	worker := fixture.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		return ports.BillExtractionResult{Envelope: paymentExtractionEnvelope(), ResponseHash: "response", Latency: time.Millisecond}, nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := fixture.store.GetJob(context.Background(), fixture.tenant.TenantID, fixture.job.ID)
		if err == nil && job.Status == domain.JobNeedsReview && worker.Ready() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := fixture.store.GetJob(context.Background(), fixture.tenant.TenantID, fixture.job.ID)
	if err != nil || job.Status != domain.JobNeedsReview || !worker.Ready() {
		cancel()
		t.Fatalf("worker did not complete queued job: job=%#v ready=%v err=%v", job, worker.Ready(), err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop")
	}
	if worker.Ready() {
		t.Fatal("stopped worker remained ready")
	}
}

func TestWorkerMarksNonRetryableProviderFailure(t *testing.T) {
	fixture := newWorkerFixture(t)
	worker := fixture.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		return ports.BillExtractionResult{}, &ports.ProviderCallError{
			Code: "provider_unauthorized", SafeMessage: "Provider 认证失败", Retryable: false,
		}
	}})
	if err := worker.ProcessOne(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), fixture.tenant.TenantID, fixture.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobFailed || job.ErrorCode != "provider_unauthorized" {
		t.Fatalf("failed job = %#v", job)
	}
}

func TestWorkerPersistsSafeProviderDiagnosticCode(t *testing.T) {
	fixture := newWorkerFixture(t)
	const safeMessage = "结构化输出不符合当前 Provider 传输契约；诊断[stage=provider_schema; violations=/fields/0/value#type]"
	var calls atomic.Int32
	worker := fixture.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		calls.Add(1)
		return ports.BillExtractionResult{}, &ports.ProviderCallError{
			Code:           "schema_validation_failed",
			DiagnosticCode: "provider_output_contract_invalid",
			SafeMessage:    safeMessage,
			Retryable:      true,
		}
	}})
	if err := worker.ProcessOne(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), fixture.tenant.TenantID, fixture.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobFailed || job.ErrorCode != "schema_validation_failed" || job.SafeErrorMessage != safeMessage ||
		job.AttemptCount != 2 || calls.Load() != 2 {
		t.Fatalf("failed job = %#v", job)
	}
	var validationCount int
	var minimumRuleCode, maximumRuleCode, minimumMessage, maximumMessage string
	if err := fixture.store.DB().QueryRowContext(context.Background(), `
		SELECT count(*), min(rule_code), max(rule_code), min(safe_message), max(safe_message)
		FROM validation_results
		WHERE tenant_id = ? AND ai_run_id IS NOT NULL
	`, fixture.tenant.TenantID).Scan(
		&validationCount,
		&minimumRuleCode,
		&maximumRuleCode,
		&minimumMessage,
		&maximumMessage,
	); err != nil {
		t.Fatal(err)
	}
	if validationCount != 2 || minimumRuleCode != "provider_output_contract_invalid" ||
		maximumRuleCode != minimumRuleCode || minimumMessage != safeMessage || maximumMessage != minimumMessage {
		t.Fatalf(
			"diagnostic validations = %d / %q / %q / %q / %q",
			validationCount,
			minimumRuleCode,
			maximumRuleCode,
			minimumMessage,
			maximumMessage,
		)
	}
}

func TestWorkerFailsSafelyWithoutActiveProvider(t *testing.T) {
	fixture := newWorkerFixture(t)
	if _, err := fixture.store.DB().Exec(
		"UPDATE provider_configs SET active = 0 WHERE tenant_id = ?",
		fixture.tenant.TenantID,
	); err != nil {
		t.Fatal(err)
	}
	worker := fixture.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		t.Fatal("extractor called without an active provider")
		return ports.BillExtractionResult{}, nil
	}})
	if err := worker.ProcessOne(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), fixture.tenant.TenantID, fixture.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobFailed || job.ErrorCode != "provider_config_missing" {
		t.Fatalf("missing-provider job = %#v", job)
	}
}

func TestWorkerRejectsStaleProviderSchemaBeforeModelCall(t *testing.T) {
	fixture := newWorkerFixture(t)
	if _, err := fixture.store.DB().Exec(
		"UPDATE provider_configs SET capability_schema_sha256 = ? WHERE tenant_id = ? AND active = 1",
		strings.Repeat("b", 64),
		fixture.tenant.TenantID,
	); err != nil {
		t.Fatal(err)
	}
	worker := fixture.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		t.Fatal("extractor called with stale Provider schema capability")
		return ports.BillExtractionResult{}, nil
	}})
	if err := worker.ProcessOne(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), fixture.tenant.TenantID, fixture.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobFailed || job.ErrorCode != "provider_capability_stale" || job.SafeErrorMessage != "AI Provider 配置需重新检测" {
		t.Fatalf("stale-provider job = %#v", job)
	}
}

func TestWorkerReusesPersistedPagesAndRejectsTampering(t *testing.T) {
	fixture := newWorkerFixture(t)
	worker := fixture.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		return ports.BillExtractionResult{Envelope: paymentExtractionEnvelope(), ResponseHash: "response", Latency: time.Millisecond}, nil
	}})
	if err := worker.ProcessOne(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	pages, err := worker.loadOrNormalizePages(context.Background(), fixture.job)
	if err != nil || len(pages) != 1 || len(pages[0].Data) == 0 {
		t.Fatalf("persisted pages = %#v, error=%v", pages, err)
	}
	if _, err := fixture.store.DB().Exec(
		"UPDATE document_pages SET sha256 = ? WHERE tenant_id = ? AND document_id = ?",
		strings.Repeat("0", 64),
		fixture.tenant.TenantID,
		fixture.job.DocumentID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.loadOrNormalizePages(context.Background(), fixture.job); err == nil {
		t.Fatal("tampered normalized page was accepted")
	}
}

func TestWorkerBlocksNormalizedDuplicateInvoiceNumber(t *testing.T) {
	fixture := newWorkerFixture(t)
	ctx := context.Background()
	firstWorker := fixture.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		return ports.BillExtractionResult{Envelope: invoiceExtractionEnvelope("INV 001"), ResponseHash: "first-response", Latency: time.Millisecond}, nil
	}})
	if err := firstWorker.ProcessOne(ctx, fixture.job); err != nil {
		t.Fatal(err)
	}
	reviewService := reviews.NewService(fixture.store, fixture.store, system.IDGenerator{}, workerClock{now: fixture.now.Add(2 * time.Minute)})
	firstReview, err := reviewService.Get(ctx, fixture.tenant, fixture.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reviewService.Confirm(ctx, fixture.tenant, fixture.job.ID, reviews.ConfirmInput{
		ExpectedRevision: firstReview.Revision,
		AssociationMode:  reviews.AssociationNoCandidate,
		IdempotencyKey:   "confirm-first-invoice",
		RequestID:        "confirm-first-invoice-request",
	}); err != nil {
		t.Fatal(err)
	}

	inspector, err := localstorage.NewInspector(fixture.objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	upload := documents.NewUploadService(fixture.objects, inspector, fixture.store, system.IDGenerator{}, workerClock{now: fixture.now.Add(3 * time.Minute)})
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.SetRGBA(0, 0, color.RGBA{R: 20, G: 80, B: 180, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatal(err)
	}
	secondUpload, err := upload.Execute(ctx, documents.UploadInput{
		Tenant: fixture.tenant, Name: "duplicate-invoice.png", MIME: "image/png", Source: bytes.NewReader(encoded.Bytes()),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondJob, err := fixture.store.LeaseNextJob(ctx, "duplicate-worker", fixture.now.Add(4*time.Minute), fixture.now.Add(4*time.Minute+165*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.ID != secondUpload.JobID {
		t.Fatalf("leased job = %s, want %s", secondJob.ID, secondUpload.JobID)
	}
	secondWorker := fixture.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		return ports.BillExtractionResult{Envelope: invoiceExtractionEnvelope("  ＩＮＶ   001  "), ResponseHash: "second-response", Latency: time.Millisecond}, nil
	}})
	if err := secondWorker.ProcessOne(ctx, secondJob); err != nil {
		t.Fatal(err)
	}
	blocked, err := reviewService.Get(ctx, fixture.tenant, secondJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != domain.ClaimBlocked || blocked.Job.Status != domain.JobBlocked {
		t.Fatalf("duplicate invoice review = %#v", blocked)
	}
	found := false
	for _, validation := range blocked.Validations {
		if validation.RuleCode == "duplicate_invoice_number" && validation.Status == "blocked" {
			found = true
		}
	}
	if !found {
		t.Fatalf("duplicate validation missing: %#v", blocked.Validations)
	}
}

func TestWorkerPersistsLocalDocumentConflictAsOneBlockedClaim(t *testing.T) {
	fixture := newWorkerFixture(t)
	envelope := paymentExtractionEnvelope()
	envelope.Invoice = json.RawMessage(`{"invoice_number":{"text":"INV-OTHER","page":1}}`)
	worker := fixture.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
		return ports.BillExtractionResult{Envelope: envelope, ResponseHash: "response", Latency: time.Millisecond}, nil
	}})
	if err := worker.ProcessOne(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), fixture.tenant.TenantID, fixture.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobBlocked || job.ErrorCode != "" {
		t.Fatalf("duplicate-path job = %#v", job)
	}
	var claims, duplicatePaths, conflictValidations int
	if err := fixture.store.DB().QueryRowContext(context.Background(), `
		SELECT
			(SELECT count(*) FROM claim_sets WHERE tenant_id = ?),
			(SELECT count(*) FROM (
				SELECT field_path FROM field_claims WHERE tenant_id = ?
				GROUP BY claim_set_id, field_path HAVING count(*) > 1
			)),
			(SELECT count(*) FROM validation_results
			 WHERE tenant_id = ? AND rule_code = 'conflicting_business_sections' AND status = 'blocked')
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID).Scan(
		&claims,
		&duplicatePaths,
		&conflictValidations,
	); err != nil {
		t.Fatal(err)
	}
	if claims != 1 || duplicatePaths != 0 || conflictValidations != 1 {
		t.Fatalf("conflict persistence = claims:%d duplicatePaths:%d validations:%d", claims, duplicatePaths, conflictValidations)
	}
}

func TestWorkerConfigurationAndSafeFailureBoundaries(t *testing.T) {
	valid := WorkerConfig{Concurrency: 1, PollInterval: 100 * time.Millisecond, JobTimeout: 150 * time.Second, LeaseDuration: 165 * time.Second}
	tests := []WorkerConfig{
		{Concurrency: 0, PollInterval: valid.PollInterval, JobTimeout: valid.JobTimeout, LeaseDuration: valid.LeaseDuration},
		{Concurrency: 1, PollInterval: valid.PollInterval, JobTimeout: time.Second, LeaseDuration: valid.LeaseDuration},
		{Concurrency: 1, PollInterval: valid.PollInterval, JobTimeout: valid.JobTimeout, LeaseDuration: 149 * time.Second},
		{Concurrency: 1, PollInterval: 99 * time.Millisecond, JobTimeout: valid.JobTimeout, LeaseDuration: valid.LeaseDuration},
	}
	for _, config := range tests {
		if _, err := NewWorker(nil, nil, nil, nil, nil, nil, nil, nil, nil, slog.Default(), config); err == nil {
			t.Fatalf("invalid config accepted: %#v", config)
		}
	}
	rule := domain.NewRuleError("corrupt_pdf", "PDF 损坏", domain.ErrInvalidInput)
	if code, message := safeFailure(rule, "fallback", "fallback"); code != "corrupt_pdf" || message != "PDF 损坏" {
		t.Fatalf("rule failure = %s/%s", code, message)
	}
	if code, _ := safeFailure(context.DeadlineExceeded, "fallback", "fallback"); code != "provider_timeout" {
		t.Fatalf("deadline code = %s", code)
	}
	if code, _ := safeFailure(context.Canceled, "fallback", "fallback"); code != "cancelled" {
		t.Fatalf("cancel code = %s", code)
	}
	if code, message := safeFailure(errors.New("opaque"), "fallback", "safe"); code != "fallback" || message != "safe" {
		t.Fatalf("fallback = %s/%s", code, message)
	}
}

type workerFixture struct {
	store      *sqliteadapter.Store
	objects    *localstorage.Store
	normalizer localstorage.Normalizer
	tenant     domain.TenantContext
	job        ports.LeasedJob
	now        time.Time
}

func newWorkerFixture(t *testing.T) workerFixture {
	t.Helper()
	ctx := context.Background()
	store, err := sqliteadapter.Open(ctx, sqliteadapter.Config{
		DatabasePath:  ":memory:",
		MigrationsDir: workerMigrationsDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ids := system.IDGenerator{}
	userID := workerID(t, ids)
	tenantID := workerID(t, ids)
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	if err := store.BootstrapOwner(ctx, ports.BootstrapOwner{
		UserID: userID, TenantID: tenantID, Email: "owner@example.test", PasswordHash: "test-only",
		DisplayName: "Owner", TenantName: "Tenant", DefaultCurrency: domain.CurrencyCNY,
		Timezone: "Asia/Shanghai", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	providerID := workerID(t, ids)
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.InsertProviderConfig(ctx, ports.ProviderConfig{
			ID: providerID, TenantID: tenantID, BaseURL: "https://provider.example/v1",
			EncryptedAPIKey: []byte("encrypted-test-key"), Model: "test-model",
			OutputMode:       ports.ProviderOutputModeJSONSchema,
			CapabilityStatus: "passed", Active: true, Version: 1,
			CapabilitySchemaVersion: "bill-visible-text-provider/1",
			CapabilitySchemaSHA256:  strings.Repeat("c", 64),
			SafeFingerprint:         "test-fingerprint", CreatedByUserID: userID,
			CreatedAt: now, UpdatedAt: now,
		})
	}); err != nil {
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
	normalizer, err := localstorage.NewNormalizer(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	upload := documents.NewUploadService(objects, inspector, store, ids, workerClock{now: now})
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	tenant := domain.TenantContext{TenantID: tenantID, UserID: userID, Role: domain.RoleOwner}
	result, err := upload.Execute(ctx, documents.UploadInput{
		Tenant: tenant, Name: "payment.png", MIME: "image/png", Source: bytes.NewReader(png),
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.LeaseNextJob(ctx, "integration-worker", now, now.Add(165*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != result.JobID {
		t.Fatalf("leased job = %s, uploaded = %s", job.ID, result.JobID)
	}
	return workerFixture{store: store, objects: objects, normalizer: normalizer, tenant: tenant, job: job, now: now}
}

func (f workerFixture) worker(t *testing.T, extractor ports.BillExtractor) *Worker {
	t.Helper()
	worker, err := NewWorker(
		f.store,
		f.store,
		fakeCipher{},
		f.normalizer,
		f.objects,
		extractor,
		f.store,
		system.IDGenerator{},
		workerClock{now: f.now.Add(time.Minute)},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WorkerConfig{Concurrency: 1, PollInterval: 100 * time.Millisecond, JobTimeout: 150 * time.Second, LeaseDuration: 165 * time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

type fakeCipher struct{}

func (fakeCipher) Encrypt(value []byte) ([]byte, error) { return bytes.Clone(value), nil }
func (fakeCipher) Decrypt([]byte) ([]byte, error)       { return []byte("test-api-key"), nil }
func (fakeCipher) Fingerprint(...[]byte) string         { return "test-fingerprint" }

type fakeExtractor struct {
	execute func(context.Context) (ports.BillExtractionResult, error)
}

func (fakeExtractor) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/1", SHA256: strings.Repeat("c", 64)}
}

func (f fakeExtractor) Prepare(_ ports.ProviderCredentials, pages []ports.PageImage) (ports.PreparedBillExtraction, error) {
	if len(pages) != 1 || pages[0].PageNumber != 1 || len(pages[0].Data) == 0 {
		return nil, domain.ErrInvalidInput
	}
	return fakePrepared{execute: f.execute}, nil
}

type fakePrepared struct {
	execute func(context.Context) (ports.BillExtractionResult, error)
}

func (fakePrepared) RequestHash() string { return "same-request-hash" }
func (fakePrepared) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/1", SHA256: strings.Repeat("c", 64)}
}
func (f fakePrepared) Execute(ctx context.Context) (ports.BillExtractionResult, error) {
	return f.execute(ctx)
}

type workerClock struct{ now time.Time }

func (c workerClock) Now() time.Time { return c.now }

func workerID(t *testing.T, ids system.IDGenerator) string {
	t.Helper()
	id, err := ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func workerMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../infra/migrations"))
}

func paymentExtractionEnvelope() domain.BillVisibleTextEnvelope {
	return domain.BillVisibleTextEnvelope{
		SchemaVersion: "bill-visible-text/1",
		DocumentType:  "payment",
		Payment:       json.RawMessage(`{"amount":{"text":"CNY 123.45","page":1},"currency":{"text":"CNY","page":1},"merchant":{"text":"Example Merchant","page":1},"transaction_time":{"text":"2026-08-27 12:00","page":1},"timezone":null,"payment_method":null,"order_number":null,"category":null}`),
		Invoice:       json.RawMessage(`null`),
	}
}

func invoiceExtractionEnvelope(number string) domain.BillVisibleTextEnvelope {
	invoice, _ := json.Marshal(map[string]any{
		"invoice_number":     map[string]any{"text": number, "page": 1},
		"invoice_date":       map[string]any{"text": "2026-08-27", "page": 1},
		"amount_without_tax": nil,
		"tax_amount":         nil,
		"amount_with_tax":    map[string]any{"text": "CNY 123.45", "page": 1},
		"currency":           map[string]any{"text": "CNY", "page": 1},
		"seller_name":        map[string]any{"text": "Example Seller", "page": 1},
		"buyer_name":         map[string]any{"text": "Example Buyer", "page": 1},
		"items":              []any{},
	})
	return domain.BillVisibleTextEnvelope{
		SchemaVersion: "bill-visible-text/1",
		DocumentType:  "invoice",
		Payment:       json.RawMessage(`null`),
		Invoice:       invoice,
	}
}
