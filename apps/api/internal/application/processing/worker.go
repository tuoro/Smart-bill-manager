package processing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/claimmapping"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/claimsupport"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const (
	promptVersion           = "bill-visible-text-cn/1"
	extractionSchemaVersion = "bill-visible-text/1"
	inputProcessingVersion  = "document-normalize/2"
)

type WorkerConfig struct {
	Concurrency   int
	PollInterval  time.Duration
	JobTimeout    time.Duration
	LeaseDuration time.Duration
}

type Worker struct {
	queue      ports.JobQueue
	providers  ports.ActiveProviderRepository
	cipher     ports.SecretCipher
	normalizer ports.DocumentNormalizer
	objects    ports.ObjectStore
	extractor  ports.BillExtractor
	tx         ports.TransactionManager
	ids        ports.IDGenerator
	clock      ports.Clock
	logger     *slog.Logger
	config     WorkerConfig
	ready      atomic.Bool
}

func NewWorker(
	queue ports.JobQueue,
	providers ports.ActiveProviderRepository,
	cipher ports.SecretCipher,
	normalizer ports.DocumentNormalizer,
	objects ports.ObjectStore,
	extractor ports.BillExtractor,
	tx ports.TransactionManager,
	ids ports.IDGenerator,
	clock ports.Clock,
	logger *slog.Logger,
	config WorkerConfig,
) (*Worker, error) {
	if config.Concurrency < 1 || config.Concurrency > 8 {
		return nil, errors.New("AI concurrency must be between 1 and 8")
	}
	if config.JobTimeout != 150*time.Second {
		return nil, errors.New("M1 job timeout must be 150 seconds")
	}
	if config.LeaseDuration < config.JobTimeout || config.LeaseDuration > 2*config.JobTimeout {
		return nil, errors.New("lease duration must be between one and two job timeouts")
	}
	if config.PollInterval < 100*time.Millisecond || config.PollInterval > 5*time.Second {
		return nil, errors.New("job poll interval must be between 100ms and 5s")
	}
	return &Worker{
		queue:      queue,
		providers:  providers,
		cipher:     cipher,
		normalizer: normalizer,
		objects:    objects,
		extractor:  extractor,
		tx:         tx,
		ids:        ids,
		clock:      clock,
		logger:     logger,
		config:     config,
	}, nil
}

func (w *Worker) Run(ctx context.Context) {
	w.ready.Store(true)
	defer w.ready.Store(false)
	var wait sync.WaitGroup
	for slot := 0; slot < w.config.Concurrency; slot++ {
		wait.Add(1)
		go func(slot int) {
			defer wait.Done()
			w.runSlot(ctx, slot)
		}(slot)
	}
	wait.Wait()
}

func (w *Worker) Ready() bool {
	return w.ready.Load()
}

func (w *Worker) runSlot(ctx context.Context, slot int) {
	workerID := fmt.Sprintf("api-%d", slot)
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		now := w.clock.Now()
		job, err := w.queue.LeaseNextJob(ctx, workerID, now, now.Add(w.config.LeaseDuration))
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrConflict) {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			}
		}
		if err != nil {
			w.logger.Error("job lease failed", "worker", workerID, "error", err)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			}
		}
		if err := w.ProcessOne(ctx, job); err != nil {
			w.logger.Error("job processing ended with error", "job_id", job.ID, "tenant_id", job.TenantID, "error", err)
		}
	}
}

func (w *Worker) ProcessOne(parent context.Context, job ports.LeasedJob) error {
	ctx, cancel := context.WithTimeout(parent, w.config.JobTimeout)
	defer cancel()
	config, err := w.providers.GetActiveProviderConfig(ctx, job.TenantID)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return w.failJob(ctx, job, "provider_timeout", "任务超过 150 秒期限")
		}
		if errors.Is(err, domain.ErrNotFound) {
			return w.failJob(ctx, job, "provider_config_missing", "请先配置并激活通过检测的 AI Provider")
		}
		return w.failJob(ctx, job, "internal_error", "无法读取 AI Provider 配置")
	}
	extractionSchema := w.extractor.ProviderSchemaIdentity()
	if config.CapabilitySchemaVersion != extractionSchema.Version ||
		config.CapabilitySchemaSHA256 != extractionSchema.SHA256 {
		return w.failJob(ctx, job, "provider_capability_stale", "AI Provider 配置需重新检测")
	}
	apiKey, err := w.cipher.Decrypt(config.EncryptedAPIKey)
	if err != nil {
		return w.failJob(ctx, job, "internal_error", "AI Provider 密钥无法解密")
	}
	defer clear(apiKey)
	pages, err := w.loadOrNormalizePages(ctx, job)
	if err != nil {
		code, message := safeFailure(err, "document_quality_insufficient", "文档页面无法规范化")
		if code == "cancelled" {
			if w.userCancellationRequested(ctx, job) {
				return w.cancelJob(context.WithoutCancel(ctx), job)
			}
			return err
		}
		return w.failJob(ctx, job, code, message)
	}
	prepared, err := w.extractor.Prepare(ports.ProviderCredentials{
		BaseURL:                 config.BaseURL,
		APIKey:                  apiKey,
		Model:                   config.Model,
		OutputMode:              config.OutputMode,
		Version:                 config.Version,
		CapabilitySchemaVersion: config.CapabilitySchemaVersion,
		CapabilitySchemaSHA256:  config.CapabilitySchemaSHA256,
	}, pageImages(pages))
	if err != nil {
		return w.failJob(ctx, job, "internal_error", "AI 账单提取请求无法准备")
	}
	preparedSchema := prepared.ProviderSchemaIdentity()
	if preparedSchema != extractionSchema {
		return w.failJob(ctx, job, "provider_capability_stale", "AI Provider 配置需重新检测")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 1 {
			if err := w.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
				return transaction.IncrementJobAttempt(ctx, job.TenantID, job.ID)
			}); err != nil {
				return err
			}
		}
		runID, err := w.ids.NewID()
		if err != nil {
			return w.failJob(ctx, job, "internal_error", "AI Run 无法创建")
		}
		startedAt := w.clock.Now()
		run := ports.AiRun{
			ID:                        runID,
			TenantID:                  job.TenantID,
			JobID:                     job.ID,
			ProviderConfigID:          config.ID,
			ProviderConfigVersion:     config.Version,
			ProviderConfigFingerprint: config.SafeFingerprint,
			Model:                     config.Model,
			PromptVersion:             promptVersion,
			ExtractionSchemaVersion:   extractionSchemaVersion,
			ProviderSchemaVersion:     preparedSchema.Version,
			ProviderSchemaSHA256:      preparedSchema.SHA256,
			ClaimSchemaVersion:        claimmapping.ClaimSchemaVersion,
			ClaimMapperVersion:        claimmapping.Version,
			InputProcessingVersion:    inputProcessingVersion,
			RequestHash:               prepared.RequestHash(),
			Outcome:                   "running",
			StartedAt:                 startedAt,
		}
		if err := w.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
			return transaction.InsertAiRun(ctx, run)
		}); err != nil {
			return w.failJob(ctx, job, "internal_error", "AI Run 无法持久化")
		}
		result, callErr := w.executeWithCancellation(ctx, job, prepared)
		if callErr != nil {
			callError := providerCallError(callErr)
			if callError.Code == "cancelled" && !w.userCancellationRequested(ctx, job) {
				return callErr
			}
			if err := w.completeFailedRun(ctx, job, runID, callError, attempt == 0 && callError.Retryable); err != nil {
				return err
			}
			if callError.Code == "cancelled" {
				return nil
			}
			if attempt == 0 && callError.Retryable && ctx.Err() == nil {
				continue
			}
			return nil
		}
		if err := w.completeSuccessfulRun(ctx, job.TenantID, runID, result); err != nil {
			return w.failJob(ctx, job, "internal_error", "AI Run 结果无法持久化")
		}
		mapped, err := claimmapping.Map(result.Envelope)
		if err != nil {
			return w.failJob(ctx, job, "business_validation_failed", "AI 业务 JSON 无法映射")
		}
		stabilized, err := domain.StabilizeItemPaths(mapped, w.ids.NewID)
		if err != nil {
			return w.failJob(ctx, job, "business_validation_failed", "发票明细结构不合法")
		}
		validated := domain.ValidateClaim(stabilized, job.PageCount)
		built, err := buildClaimBundle(validated, job.TenantID, job.DocumentID, runID, pages, w.ids, w.clock.Now())
		if err != nil {
			return w.failJob(ctx, job, "internal_error", "Claim 记录无法构造")
		}
		if err := w.persistClaim(ctx, job, validated, &built); err != nil {
			if requested, checkErr := w.queue.CancellationRequested(context.WithoutCancel(ctx), job.TenantID, job.ID); checkErr == nil && requested {
				return w.cancelJob(context.WithoutCancel(ctx), job)
			}
			return w.failJob(context.WithoutCancel(ctx), job, "internal_error", "Claim 无法持久化")
		}
		return nil
	}
	return nil
}

func (w *Worker) loadOrNormalizePages(ctx context.Context, job ports.LeasedJob) ([]ports.NormalizedPage, error) {
	pages, err := w.queue.GetDocumentPages(ctx, job.TenantID, job.DocumentID)
	if err != nil {
		return nil, err
	}
	if len(pages) != job.PageCount {
		pages, err = w.normalizer.Normalize(ctx, ports.ProcessingDocument{
			TenantID:   job.TenantID,
			DocumentID: job.DocumentID,
			StorageKey: job.StorageKey,
			MIME:       job.MIME,
			PageCount:  job.PageCount,
		})
		if err != nil {
			return nil, err
		}
		records := make([]ports.DocumentPageRecord, 0, len(pages))
		now := w.clock.Now()
		for index := range pages {
			id, err := w.ids.NewID()
			if err != nil {
				return nil, err
			}
			pages[index].ID = id
			records = append(records, ports.DocumentPageRecord{
				ID:                id,
				TenantID:          job.TenantID,
				DocumentID:        job.DocumentID,
				PageNumber:        pages[index].PageNumber,
				StorageKey:        pages[index].StorageKey,
				Width:             pages[index].Width,
				Height:            pages[index].Height,
				SHA256:            pages[index].SHA256,
				ProcessingVersion: inputProcessingVersion,
				VisualFingerprint: pages[index].VisualFingerprint,
				CreatedAt:         now,
			})
		}
		if err := w.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
			return transaction.InsertDocumentPages(ctx, records)
		}); err != nil {
			return nil, err
		}
		pages, err = w.queue.GetDocumentPages(ctx, job.TenantID, job.DocumentID)
		if err != nil {
			return nil, err
		}
	}
	if len(pages) != job.PageCount {
		return nil, errors.New("persisted page count mismatch")
	}
	for index := range pages {
		reader, err := w.objects.Open(ctx, pages[index].StorageKey)
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, ports.MaxDerivedPageBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || int64(len(content)) > ports.MaxDerivedPageBytes {
			return nil, errors.New("read normalized page")
		}
		hash := sha256.Sum256(content)
		if hex.EncodeToString(hash[:]) != pages[index].SHA256 {
			return nil, errors.New("normalized page hash mismatch")
		}
		pages[index].Data = content
		pages[index].MIME = "image/png"
	}
	return pages, nil
}

func (w *Worker) executeWithCancellation(
	ctx context.Context,
	job ports.LeasedJob,
	prepared ports.PreparedBillExtraction,
) (ports.BillExtractionResult, error) {
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-callCtx.Done():
				return
			case <-ticker.C:
				requested, err := w.queue.CancellationRequested(callCtx, job.TenantID, job.ID)
				if err == nil && requested {
					cancel()
					return
				}
			}
		}
	}()
	result, err := prepared.Execute(callCtx)
	close(done)
	return result, err
}

func (w *Worker) completeFailedRun(
	ctx context.Context,
	job ports.LeasedJob,
	runID string,
	callError *ports.ProviderCallError,
	willRetry bool,
) error {
	now := w.clock.Now()
	return w.tx.WithinTransaction(context.WithoutCancel(ctx), func(transaction ports.Transaction) error {
		if err := transaction.CompleteAiRun(context.WithoutCancel(ctx), ports.AiRunCompletion{
			TenantID:   job.TenantID,
			AiRunID:    runID,
			Outcome:    map[bool]string{true: "cancelled", false: "failed"}[callError.Code == "cancelled"],
			Latency:    callError.Latency,
			ErrorCode:  callError.Code,
			FinishedAt: now,
		}); err != nil {
			return err
		}
		validationID, err := w.ids.NewID()
		if err != nil {
			return err
		}
		validationCode := callError.DiagnosticCode
		if validationCode == "" {
			validationCode = callError.Code
		}
		if err := transaction.InsertAiRunValidation(context.WithoutCancel(ctx), ports.ValidationRecord{
			ID:          validationID,
			TenantID:    job.TenantID,
			AiRunID:     runID,
			RuleCode:    validationCode,
			Severity:    "error",
			Status:      "error",
			SafeMessage: callError.SafeMessage,
			RuleVersion: claimsupport.ValidationRuleVersion,
			CreatedAt:   now,
		}); err != nil {
			return err
		}
		if willRetry {
			return nil
		}
		if callError.Code == "cancelled" {
			return transaction.MarkJobCancelled(context.WithoutCancel(ctx), job.TenantID, job.ID, now)
		}
		return transaction.MarkJobFailed(
			context.WithoutCancel(ctx),
			job.TenantID,
			job.ID,
			callError.Code,
			callError.SafeMessage,
			now,
		)
	})
}

func (w *Worker) completeSuccessfulRun(
	ctx context.Context,
	tenantID, runID string,
	result ports.BillExtractionResult,
) error {
	return w.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.CompleteAiRun(ctx, ports.AiRunCompletion{
			TenantID:     tenantID,
			AiRunID:      runID,
			Outcome:      "succeeded",
			ResponseHash: result.ResponseHash,
			InputTokens:  result.InputTokens,
			OutputTokens: result.OutputTokens,
			Latency:      result.Latency,
			FinishedAt:   w.clock.Now(),
		})
	})
}

func (w *Worker) persistClaim(
	ctx context.Context,
	job ports.LeasedJob,
	validated domain.ValidatedClaim,
	built *builtClaim,
) error {
	return w.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		if validated.DocumentType == domain.DocumentInvoice {
			number := claimsupport.NormalizedInvoiceNumber(validated.Fields)
			if number != "" {
				exists, err := transaction.InvoiceNumberExists(ctx, job.TenantID, number)
				if err != nil {
					return err
				}
				if exists {
					validation := domain.ClaimValidation{
						FieldPath:   "invoice_number",
						RuleCode:    "duplicate_invoice_number",
						Severity:    "blocked",
						Status:      "blocked",
						SafeMessage: "同一工作区已存在规范化号码完全一致的发票",
					}
					record, err := claimsupport.NewValidationRecord(
						validation,
						job.TenantID,
						built.Bundle.ClaimSet.ID,
						built.FieldIDs,
						w.ids,
						w.clock.Now(),
					)
					if err != nil {
						return err
					}
					built.Bundle.Validations = append(built.Bundle.Validations, record)
					built.Bundle.ClaimSet.Status = domain.ClaimBlocked
				}
			}
		}
		if input, ok := claimsupport.LinkInputFromValidated(validated); ok {
			targets, err := transaction.ListEligibleLinkTargets(
				ctx,
				job.TenantID,
				input.DocumentType,
				input.Currency,
			)
			if err != nil {
				return err
			}
			candidates, err := claimsupport.BuildLinkCandidates(
				input,
				targets,
				job.TenantID,
				built.Bundle.ClaimSet.ID,
				w.ids,
				w.clock.Now(),
			)
			if err != nil {
				return err
			}
			built.Bundle.Candidates = candidates
		}
		duplicateCandidates, limitExceeded, err := claimsupport.BuildDuplicateCandidates(
			ctx,
			transaction,
			validated,
			job.TenantID,
			job.DocumentID,
			built.Bundle.ClaimSet.ID,
			w.ids,
			w.clock.Now(),
		)
		if err != nil {
			return err
		}
		built.Bundle.DuplicateCandidates = duplicateCandidates
		if limitExceeded {
			validation, err := claimsupport.NewDuplicateCandidateLimitValidation(
				job.TenantID,
				built.Bundle.ClaimSet.ID,
				w.ids,
				w.clock.Now(),
			)
			if err != nil {
				return err
			}
			built.Bundle.Validations = append(built.Bundle.Validations, validation)
			built.Bundle.ClaimSet.Status = domain.ClaimBlocked
		}
		return transaction.PersistInitialClaim(ctx, job.ID, built.Bundle)
	})
}

func (w *Worker) failJob(ctx context.Context, job ports.LeasedJob, code, message string) error {
	return w.tx.WithinTransaction(context.WithoutCancel(ctx), func(transaction ports.Transaction) error {
		return transaction.MarkJobFailed(context.WithoutCancel(ctx), job.TenantID, job.ID, code, message, w.clock.Now())
	})
}

func (w *Worker) cancelJob(ctx context.Context, job ports.LeasedJob) error {
	return w.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		return transaction.MarkJobCancelled(ctx, job.TenantID, job.ID, w.clock.Now())
	})
}

func (w *Worker) userCancellationRequested(ctx context.Context, job ports.LeasedJob) bool {
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	requested, err := w.queue.CancellationRequested(checkCtx, job.TenantID, job.ID)
	return err == nil && requested
}

func pageImages(pages []ports.NormalizedPage) []ports.PageImage {
	result := make([]ports.PageImage, 0, len(pages))
	for _, page := range pages {
		result = append(result, page.PageImage)
	}
	return result
}

func providerCallError(err error) *ports.ProviderCallError {
	var callError *ports.ProviderCallError
	if errors.As(err, &callError) {
		return callError
	}
	return &ports.ProviderCallError{
		Code:        "internal_error",
		SafeMessage: "AI 提取发生内部错误",
		Retryable:   false,
		Cause:       err,
	}
}

func safeFailure(err error, defaultCode, defaultMessage string) (string, string) {
	var ruleError *domain.RuleError
	if errors.As(err, &ruleError) {
		return ruleError.Code, ruleError.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "provider_timeout", "任务超过 150 秒期限"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled", "任务已取消"
	}
	return defaultCode, defaultMessage
}
