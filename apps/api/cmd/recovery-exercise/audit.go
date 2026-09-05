package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"golang.org/x/sys/unix"
)

const (
	snapshotKind                  = "m4-recovery-database-snapshot"
	snapshotVersion               = 1
	expectedRecoveryDocumentPages = 2
)

var (
	exerciseIDPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	backupSetIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	sha256Pattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

var appendOnlyRecoveryTables = []string{
	"claim_sets",
	"field_claims",
	"evidence",
	"validation_results",
	"payment_invoice_link_candidates",
	"review_decisions",
	"payments",
	"duplicate_candidates",
	"duplicate_candidate_decisions",
	"fact_field_origins",
	"audit_events",
}

var fullyStableRecoveryTables = []string{
	"fact_bad_debt_decisions",
	"account_events",
	"member_invitations",
	"invoice_material_decisions",
	"invoice_material_links",
	"reimbursement_material_snapshots",
	"fact_corrections",
	"schema_migrations",
	"users",
	"tenants",
	"memberships",
	"provider_configs",
	"email_sources",
	"email_messages",
	"email_attachments",
	"document_pages",
	"invoices",
	"invoice_items",
	"trips",
	"trip_evidence_facts",
	"trip_management_decisions",
	"trip_material_decisions",
	"trip_material_links",
	"payment_invoice_link_decisions",
	"payment_invoice_allocation_adjustments",
	"payment_invoice_links",
	"trip_fact_assignment_decisions",
	"trip_fact_assignments",
	"reimbursements",
	"reimbursement_items",
	"reimbursement_policy_findings",
	"reimbursement_status_decisions",
	"deletion_tombstones",
}

type recoverySnapshot struct {
	Kind                   string           `json:"kind"`
	Version                int              `json:"version"`
	ExerciseID             string           `json:"exercise_id"`
	CapturedAt             string           `json:"captured_at"`
	DocumentCount          int64            `json:"document_count"`
	JobCount               int64            `json:"job_count"`
	FailedJobCount         int64            `json:"failed_job_count"`
	FactCount              int64            `json:"fact_count"`
	EmailMessageCount      int64            `json:"email_message_count"`
	EmailAttachmentCount   int64            `json:"email_attachment_count"`
	DocumentPageCount      int64            `json:"document_page_count"`
	SessionCount           int64            `json:"session_count"`
	AIRunCount             int64            `json:"ai_run_count"`
	RunningAIRunCount      int64            `json:"running_ai_run_count"`
	SucceededAIRunCount    int64            `json:"succeeded_ai_run_count"`
	ProcessingJobID        string           `json:"processing_job_id"`
	ProcessingDocumentID   string           `json:"processing_document_id"`
	ProcessingAttemptCount int64            `json:"processing_attempt_count"`
	ProcessingVersion      int64            `json:"processing_version"`
	RunningAIRunID         string           `json:"running_ai_run_id"`
	ConfirmedFactID        string           `json:"confirmed_fact_id"`
	ConfirmedDocumentID    string           `json:"confirmed_document_id"`
	StableStateSHA256      string           `json:"stable_state_sha256"`
	StableStateRowCount    int64            `json:"stable_state_row_count"`
	AppendOnlyTableCounts  map[string]int64 `json:"append_only_table_counts"`
	Passed                 bool             `json:"passed"`
}

type recoveryVerification struct {
	Kind                       string `json:"kind"`
	Version                    int    `json:"version"`
	ExerciseID                 string `json:"exercise_id"`
	BackupSetID                string `json:"backup_set_id"`
	RecoveredFactID            string `json:"recovered_fact_id"`
	VerifiedAt                 string `json:"verified_at"`
	DocumentCount              int64  `json:"document_count"`
	JobCount                   int64  `json:"job_count"`
	FactCount                  int64  `json:"fact_count"`
	EmailMessageCount          int64  `json:"email_message_count"`
	EmailAttachmentCount       int64  `json:"email_attachment_count"`
	DocumentPageCount          int64  `json:"document_page_count"`
	NewSessionCount            int64  `json:"new_session_count"`
	HistoricalSessionCount     int64  `json:"historical_session_count"`
	ProcessingAttemptCount     int64  `json:"processing_attempt_count"`
	ExpectedAttemptDelta       int64  `json:"expected_attempt_delta"`
	ProcessingVersion          int64  `json:"processing_version"`
	ExpectedVersionDelta       int64  `json:"expected_version_delta"`
	LeaseExpiredAIRunCount     int64  `json:"lease_expired_ai_run_count"`
	RunningAIRunCount          int64  `json:"running_ai_run_count"`
	UnleasedProcessingJobCount int64  `json:"unleased_processing_job_count"`
	ConfirmedFactsPreserved    bool   `json:"confirmed_facts_preserved"`
	ExactlyOneRecoveredFact    bool   `json:"exactly_one_recovered_fact"`
	OriginalRunningRunClosed   bool   `json:"original_running_run_closed"`
	AllJobsTerminal            bool   `json:"all_jobs_terminal"`
	StableStatePreserved       bool   `json:"stable_state_preserved"`
	AppendOnlyChangesScoped    bool   `json:"append_only_changes_scoped"`
	Passed                     bool   `json:"passed"`
}

func captureRecoverySnapshot(ctx context.Context, options snapshotOptions) (recoverySnapshot, error) {
	if !exerciseIDPattern.MatchString(options.ExerciseID) {
		return recoverySnapshot{}, errors.New("recovery snapshot exercise identity is invalid")
	}
	if err := requireEmptyRuntimeDirectories(options.Objects); err != nil {
		return recoverySnapshot{}, err
	}
	store, err := openRecoveryStore(ctx)
	if err != nil {
		return recoverySnapshot{}, err
	}
	defer store.Close()
	database := store.DB()
	result := recoverySnapshot{
		Kind: snapshotKind, Version: snapshotVersion, ExerciseID: options.ExerciseID,
		CapturedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		ProcessingJobID: options.ProcessingJobID, ConfirmedFactID: options.ConfirmedFactID,
	}
	if err := database.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM documents),
		       (SELECT count(*) FROM processing_jobs),
		       (SELECT count(*) FROM processing_jobs WHERE status = 'failed'),
		       (SELECT count(*) FROM payments) + (SELECT count(*) FROM invoices) + (SELECT count(*) FROM trip_evidence_facts),
		       (SELECT count(*) FROM email_messages),
		       (SELECT count(*) FROM email_attachments WHERE storage_key IS NOT NULL),
		       (SELECT count(*) FROM document_pages),
		       (SELECT count(*) FROM sessions)
	`).Scan(
		&result.DocumentCount, &result.JobCount, &result.FailedJobCount, &result.FactCount,
		&result.EmailMessageCount, &result.EmailAttachmentCount, &result.DocumentPageCount, &result.SessionCount,
	); err != nil {
		return recoverySnapshot{}, err
	}
	var status, leaseOwner string
	var leaseExpires time.Time
	if err := database.QueryRowContext(ctx, `
		SELECT document_id, status, attempt_count, coalesce(lease_owner, ''), lease_expires_at, version
		FROM processing_jobs WHERE id = ?
	`, options.ProcessingJobID).Scan(&result.ProcessingDocumentID, &status, &result.ProcessingAttemptCount, &leaseOwner, &leaseExpires, &result.ProcessingVersion); err != nil {
		return recoverySnapshot{}, err
	}
	if err := database.QueryRowContext(ctx, `
		SELECT count(*),
		       coalesce(sum(CASE WHEN outcome = 'running' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN outcome = 'succeeded' THEN 1 ELSE 0 END), 0),
		       coalesce(min(CASE WHEN outcome = 'running' THEN id END), '')
		FROM ai_runs
	`).Scan(&result.AIRunCount, &result.RunningAIRunCount, &result.SucceededAIRunCount, &result.RunningAIRunID); err != nil {
		return recoverySnapshot{}, err
	}
	var targetAIRunCount, targetRunningCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT count(*),
		       coalesce(sum(CASE WHEN outcome = 'running' THEN 1 ELSE 0 END), 0)
		FROM ai_runs WHERE job_id = ?
	`, options.ProcessingJobID).Scan(&targetAIRunCount, &targetRunningCount); err != nil {
		return recoverySnapshot{}, err
	}
	var confirmedFactCount, activeProviderCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM payments WHERE id = ? AND deleted_at IS NULL),
		       (SELECT count(*) FROM provider_configs WHERE active = TRUE AND deleted_at IS NULL)
	`, options.ConfirmedFactID).Scan(&confirmedFactCount, &activeProviderCount); err != nil {
		return recoverySnapshot{}, err
	}
	if err := database.QueryRowContext(ctx, `
		SELECT c.document_id
		FROM payments p
		JOIN review_decisions r ON r.tenant_id = p.tenant_id AND r.id = p.source_review_decision_id
		JOIN claim_sets c ON c.tenant_id = r.tenant_id AND c.id = r.claim_set_id
		WHERE p.id = ? AND p.deleted_at IS NULL AND r.action = 'confirm' AND c.status = 'confirmed'
	`, options.ConfirmedFactID).Scan(&result.ConfirmedDocumentID); err != nil {
		return recoverySnapshot{}, errors.New("confirmed fact does not have one confirmed Source/Claim chain")
	}
	var confirmedSucceededRunCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT count(*)
		FROM ai_runs r
		JOIN processing_jobs j ON j.tenant_id = r.tenant_id AND j.id = r.job_id
		WHERE j.document_id = ? AND j.status = 'completed' AND r.outcome = 'succeeded'
	`, result.ConfirmedDocumentID).Scan(&confirmedSucceededRunCount); err != nil {
		return recoverySnapshot{}, err
	}
	var failedBoundaryCount, completedJobCount, processingJobCount, invalidJobCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT coalesce(sum(CASE WHEN status = 'failed' AND error_code = 'provider_config_missing' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN status = 'processing' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN status NOT IN ('failed', 'completed', 'processing') THEN 1 ELSE 0 END), 0)
		FROM processing_jobs
	`).Scan(&failedBoundaryCount, &completedJobCount, &processingJobCount, &invalidJobCount); err != nil {
		return recoverySnapshot{}, err
	}
	var failedDocumentCount, completedDocumentCount, processingDocumentCount, invalidDocumentCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT coalesce(sum(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN status = 'processing' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN status NOT IN ('failed', 'completed', 'processing') THEN 1 ELSE 0 END), 0)
		FROM documents
	`).Scan(&failedDocumentCount, &completedDocumentCount, &processingDocumentCount, &invalidDocumentCount); err != nil {
		return recoverySnapshot{}, err
	}
	var confirmedCompletedJobCount, confirmedPageCount, processingPageCount, processingClaimCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM processing_jobs WHERE document_id = ? AND status = 'completed'),
		       (SELECT count(*) FROM document_pages WHERE document_id = ?),
		       (SELECT count(*) FROM document_pages WHERE document_id = ?),
		       (SELECT count(*) FROM claim_sets WHERE document_id = ?)
	`, result.ConfirmedDocumentID, result.ConfirmedDocumentID, result.ProcessingDocumentID, result.ProcessingDocumentID).
		Scan(&confirmedCompletedJobCount, &confirmedPageCount, &processingPageCount, &processingClaimCount); err != nil {
		return recoverySnapshot{}, err
	}
	var runProviderID, runModel, providerModel, providerBaseURL string
	var runStartedAt time.Time
	var runRequestHash sql.NullString
	if err := database.QueryRowContext(ctx, `
		SELECT r.provider_config_id, r.model, p.model, p.base_url, r.request_hash, r.started_at
		FROM ai_runs r
		JOIN provider_configs p ON p.tenant_id = r.tenant_id AND p.id = r.provider_config_id
		WHERE r.id = ? AND p.active = TRUE AND p.deleted_at IS NULL
	`, result.RunningAIRunID).Scan(&runProviderID, &runModel, &providerModel, &providerBaseURL, &runRequestHash, &runStartedAt); err != nil {
		return recoverySnapshot{}, errors.New("running AI run is not bound to the sole active Provider")
	}
	if !leaseExpires.After(time.Now().UTC()) {
		return recoverySnapshot{}, errors.New("processing job does not hold a current lease at the backup boundary")
	}
	if result.DocumentCount != options.ExpectedDocuments || result.JobCount != options.ExpectedDocuments ||
		result.FailedJobCount != options.ExpectedDocuments-2 || result.FactCount != 1 ||
		result.EmailMessageCount != 1 || result.EmailAttachmentCount != 1 || result.DocumentPageCount != expectedRecoveryDocumentPages || result.SessionCount < 1 ||
		status != "processing" || result.ProcessingAttemptCount < 1 || result.ProcessingVersion < 1 || leaseOwner == "" || targetAIRunCount != 1 || targetRunningCount != 1 || result.RunningAIRunID == "" ||
		result.AIRunCount != 2 || result.RunningAIRunCount != 1 || result.SucceededAIRunCount != 1 ||
		failedBoundaryCount != options.ExpectedDocuments-2 || completedJobCount != 1 || processingJobCount != 1 || invalidJobCount != 0 ||
		failedDocumentCount != options.ExpectedDocuments-2 || completedDocumentCount != 1 || processingDocumentCount != 1 || invalidDocumentCount != 0 ||
		confirmedCompletedJobCount != 1 || confirmedPageCount != 1 || processingPageCount != 1 || processingClaimCount != 0 || result.ConfirmedDocumentID == result.ProcessingDocumentID ||
		confirmedFactCount != 1 || confirmedSucceededRunCount != 1 || activeProviderCount != 1 || runProviderID == "" || runModel != providerModel ||
		!runRequestHash.Valid || !sha256Pattern.MatchString(runRequestHash.String) || !canonicalTimeAtOrBefore(runStartedAt.UTC().Format(time.RFC3339Nano), result.CapturedAt) ||
		!isSyntheticLoopbackProvider(providerBaseURL, runModel) {
		return recoverySnapshot{}, errors.New("pre-backup recovery dataset does not match the frozen shape")
	}
	result.AppendOnlyTableCounts, err = appendOnlyTableCounts(ctx, database)
	if err != nil {
		return recoverySnapshot{}, err
	}
	result.StableStateSHA256, result.StableStateRowCount, err = stableStateDigest(
		ctx,
		database,
		result.ProcessingJobID,
		result.ProcessingDocumentID,
		result.RunningAIRunID,
		result.AppendOnlyTableCounts,
	)
	if err != nil {
		return recoverySnapshot{}, err
	}
	result.Passed = true
	return result, nil
}

func verifyRecoveryState(ctx context.Context, options verifyOptions) (recoveryVerification, error) {
	snapshot, err := readRecoverySnapshot(options.Snapshot)
	if err != nil {
		return recoveryVerification{}, err
	}
	if options.ExerciseID != snapshot.ExerciseID || !exerciseIDPattern.MatchString(options.ExerciseID) || !backupSetIDPattern.MatchString(options.BackupSetID) {
		return recoveryVerification{}, errors.New("recovery exercise or backup set identity differs from the protected snapshot")
	}
	store, err := openRecoveryStore(ctx)
	if err != nil {
		return recoveryVerification{}, err
	}
	defer store.Close()
	database := store.DB()
	result := recoveryVerification{
		Kind: "m4-recovery-database-verification", Version: 1,
		ExerciseID: options.ExerciseID, BackupSetID: options.BackupSetID, RecoveredFactID: options.RecoveredFactID,
		VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), ExpectedAttemptDelta: 1, ExpectedVersionDelta: 3,
	}
	if err := database.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM documents),
		       (SELECT count(*) FROM processing_jobs),
		       (SELECT count(*) FROM payments) + (SELECT count(*) FROM invoices) + (SELECT count(*) FROM trip_evidence_facts),
		       (SELECT count(*) FROM email_messages),
		       (SELECT count(*) FROM email_attachments WHERE storage_key IS NOT NULL),
		       (SELECT count(*) FROM document_pages),
		       (SELECT count(*) FROM sessions),
		       (SELECT count(*) FROM sessions WHERE created_at <= ?::timestamptz),
		       (SELECT count(*) FROM ai_runs WHERE outcome = 'running'),
		       (SELECT count(*) FROM processing_jobs WHERE status = 'processing' AND (lease_owner IS NULL OR lease_expires_at IS NULL))
	`, snapshot.CapturedAt).Scan(
		&result.DocumentCount, &result.JobCount, &result.FactCount,
		&result.EmailMessageCount, &result.EmailAttachmentCount, &result.DocumentPageCount,
		&result.NewSessionCount, &result.HistoricalSessionCount, &result.RunningAIRunCount, &result.UnleasedProcessingJobCount,
	); err != nil {
		return recoveryVerification{}, err
	}
	var jobDocumentID, jobStatus, jobLeaseOwner, jobErrorCode, jobSafeError string
	var jobLeaseExpires, jobCancelRequested, jobFinishedAt sql.NullTime
	if err := database.QueryRowContext(ctx, `
		SELECT document_id, status, attempt_count, version,
		       coalesce(lease_owner, ''), lease_expires_at,
		       coalesce(error_code, ''), coalesce(safe_error_message, ''), cancel_requested_at,
		       finished_at
		FROM processing_jobs WHERE id = ?
	`, snapshot.ProcessingJobID).Scan(
		&jobDocumentID, &jobStatus, &result.ProcessingAttemptCount, &result.ProcessingVersion,
		&jobLeaseOwner, &jobLeaseExpires, &jobErrorCode, &jobSafeError, &jobCancelRequested, &jobFinishedAt,
	); err != nil {
		return recoveryVerification{}, err
	}
	originalRun, err := readRecoveryAIRun(ctx, database, snapshot.RunningAIRunID)
	if err != nil {
		return recoveryVerification{}, err
	}
	var aiRunCount, succeededAIRunCount, unexpectedAIRunCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT count(*),
		       coalesce(sum(CASE WHEN outcome = 'succeeded' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN outcome = 'failed' AND error_code = 'lease_expired' THEN 1 ELSE 0 END), 0),
		       coalesce(sum(CASE WHEN outcome NOT IN ('succeeded', 'failed') OR (outcome = 'failed' AND coalesce(error_code, '') <> 'lease_expired') THEN 1 ELSE 0 END), 0)
		FROM ai_runs
	`).Scan(&aiRunCount, &succeededAIRunCount, &result.LeaseExpiredAIRunCount, &unexpectedAIRunCount); err != nil {
		return recoveryVerification{}, err
	}
	var newRunCount int64
	var newRunID string
	if err := database.QueryRowContext(ctx, `
		SELECT count(*), coalesce(min(id), '') FROM ai_runs WHERE job_id = ? AND id <> ?
	`, snapshot.ProcessingJobID, snapshot.RunningAIRunID).Scan(&newRunCount, &newRunID); err != nil {
		return recoveryVerification{}, err
	}
	var newRun recoveryAIRunAudit
	if newRunCount == 1 {
		newRun, err = readRecoveryAIRun(ctx, database, newRunID)
		if err != nil {
			return recoveryVerification{}, err
		}
	}
	var originalFactCount, recoveredFactCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM payments WHERE id = ? AND deleted_at IS NULL),
		       (SELECT count(*) FROM payments WHERE id = ? AND deleted_at IS NULL)
	`, snapshot.ConfirmedFactID, options.RecoveredFactID).Scan(&originalFactCount, &recoveredFactCount); err != nil {
		return recoveryVerification{}, err
	}
	var nonterminalJobs int64
	if err := database.QueryRowContext(ctx, `
		SELECT count(*) FROM processing_jobs
		WHERE status NOT IN ('needs_review', 'blocked', 'failed', 'cancelled', 'completed', 'rejected')
	`).Scan(&nonterminalJobs); err != nil {
		return recoveryVerification{}, err
	}
	var processingDocumentStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM documents WHERE id = ?`, snapshot.ProcessingDocumentID).
		Scan(&processingDocumentStatus); err != nil {
		return recoveryVerification{}, err
	}
	stableDigest, stableRows, err := stableStateDigest(
		ctx,
		database,
		snapshot.ProcessingJobID,
		snapshot.ProcessingDocumentID,
		snapshot.RunningAIRunID,
		snapshot.AppendOnlyTableCounts,
	)
	if err != nil {
		return recoveryVerification{}, err
	}
	result.StableStatePreserved = stableDigest == snapshot.StableStateSHA256 && stableRows == snapshot.StableStateRowCount
	result.AppendOnlyChangesScoped, err = appendOnlyChangesAreScoped(ctx, database, snapshot, newRunID, options.RecoveredFactID)
	if err != nil {
		return recoveryVerification{}, err
	}
	result.ConfirmedFactsPreserved = originalFactCount == 1
	result.ExactlyOneRecoveredFact = recoveredFactCount == 1 && options.RecoveredFactID != snapshot.ConfirmedFactID && result.FactCount == snapshot.FactCount+1
	result.OriginalRunningRunClosed = originalRun.Outcome == "failed" && originalRun.ErrorCode.Valid && originalRun.ErrorCode.String == "lease_expired" &&
		!originalRun.ResponseHash.Valid && !originalRun.InputTokens.Valid && !originalRun.OutputTokens.Valid && !originalRun.LatencyMS.Valid && originalRun.FinishedAt.Valid &&
		canonicalTimeAtOrAfter(originalRun.FinishedAt.Time.UTC().Format(time.RFC3339Nano), snapshot.CapturedAt) &&
		result.LeaseExpiredAIRunCount == 1
	newRunSucceeded := newRunCount == 1 && sameRecoveryAIRunContract(originalRun, newRun) && newRun.Outcome == "succeeded" &&
		!newRun.ErrorCode.Valid && newRun.ResponseHash.Valid && sha256Pattern.MatchString(newRun.ResponseHash.String) &&
		newRun.InputTokens.Valid && newRun.InputTokens.Int64 >= 0 && newRun.OutputTokens.Valid && newRun.OutputTokens.Int64 >= 0 &&
		newRun.LatencyMS.Valid && newRun.LatencyMS.Int64 >= 0 && newRun.FinishedAt.Valid &&
		canonicalTimeAtOrAfter(newRun.StartedAt.UTC().Format(time.RFC3339Nano), snapshot.CapturedAt) &&
		canonicalTimeAtOrAfter(newRun.FinishedAt.Time.UTC().Format(time.RFC3339Nano), newRun.StartedAt.UTC().Format(time.RFC3339Nano))
	result.AllJobsTerminal = nonterminalJobs == 0 && jobStatus == "completed"
	if result.DocumentCount != snapshot.DocumentCount || result.JobCount != snapshot.JobCount ||
		result.EmailMessageCount != snapshot.EmailMessageCount || result.EmailAttachmentCount != snapshot.EmailAttachmentCount ||
		result.DocumentPageCount != snapshot.DocumentPageCount || result.NewSessionCount != 1 || result.HistoricalSessionCount != 0 ||
		jobDocumentID != snapshot.ProcessingDocumentID || processingDocumentStatus != "completed" ||
		result.ProcessingAttemptCount != snapshot.ProcessingAttemptCount+result.ExpectedAttemptDelta ||
		result.ProcessingVersion != snapshot.ProcessingVersion+result.ExpectedVersionDelta ||
		jobLeaseOwner != "" || jobLeaseExpires.Valid || jobErrorCode != "" || jobSafeError != "" || jobCancelRequested.Valid ||
		!jobFinishedAt.Valid || !canonicalTimeAtOrAfter(jobFinishedAt.Time.UTC().Format(time.RFC3339Nano), snapshot.CapturedAt) ||
		result.RunningAIRunCount != 0 || result.UnleasedProcessingJobCount != 0 || aiRunCount != snapshot.AIRunCount+1 ||
		succeededAIRunCount != snapshot.SucceededAIRunCount+1 || unexpectedAIRunCount != 0 || !newRunSucceeded ||
		!result.ConfirmedFactsPreserved || !result.ExactlyOneRecoveredFact || !result.OriginalRunningRunClosed || !result.AllJobsTerminal ||
		!result.StableStatePreserved || !result.AppendOnlyChangesScoped {
		return recoveryVerification{}, errors.New("restored database does not match the frozen recovery increments")
	}
	result.Passed = true
	return result, nil
}

type recoveryAIRunAudit struct {
	ProviderConfigID          string
	ProviderConfigVersion     int64
	ProviderConfigFingerprint string
	Model                     string
	PromptVersion             string
	ExtractionSchemaVersion   string
	ProviderSchemaVersion     string
	ProviderSchemaSHA256      string
	ClaimSchemaVersion        string
	ClaimMapperVersion        string
	InputProcessingVersion    string
	RequestHash               sql.NullString
	ResponseHash              sql.NullString
	InputTokens               sql.NullInt64
	OutputTokens              sql.NullInt64
	LatencyMS                 sql.NullInt64
	Outcome                   string
	ErrorCode                 sql.NullString
	StartedAt                 time.Time
	FinishedAt                sql.NullTime
}

func readRecoveryAIRun(ctx context.Context, database *sql.DB, id string) (recoveryAIRunAudit, error) {
	var result recoveryAIRunAudit
	err := database.QueryRowContext(ctx, `
		SELECT provider_config_id, provider_config_version, provider_config_fingerprint,
		       model, prompt_version, extraction_schema_version, provider_schema_version,
		       provider_schema_sha256, claim_schema_version, claim_mapper_version,
		       input_processing_version, request_hash, response_hash,
		       input_tokens, output_tokens, latency_ms, outcome, error_code, started_at, finished_at
		FROM ai_runs WHERE id = ?
	`, id).Scan(
		&result.ProviderConfigID, &result.ProviderConfigVersion, &result.ProviderConfigFingerprint,
		&result.Model, &result.PromptVersion, &result.ExtractionSchemaVersion, &result.ProviderSchemaVersion,
		&result.ProviderSchemaSHA256, &result.ClaimSchemaVersion, &result.ClaimMapperVersion,
		&result.InputProcessingVersion, &result.RequestHash, &result.ResponseHash,
		&result.InputTokens, &result.OutputTokens, &result.LatencyMS, &result.Outcome, &result.ErrorCode,
		&result.StartedAt, &result.FinishedAt,
	)
	return result, err
}

func sameRecoveryAIRunContract(original, recovered recoveryAIRunAudit) bool {
	return original.ProviderConfigID != "" && original.ProviderConfigID == recovered.ProviderConfigID &&
		original.ProviderConfigVersion >= 1 && original.ProviderConfigVersion == recovered.ProviderConfigVersion &&
		original.ProviderConfigFingerprint != "" && original.ProviderConfigFingerprint == recovered.ProviderConfigFingerprint &&
		original.Model != "" && original.Model == recovered.Model &&
		original.PromptVersion != "" && original.PromptVersion == recovered.PromptVersion &&
		original.ExtractionSchemaVersion != "" && original.ExtractionSchemaVersion == recovered.ExtractionSchemaVersion &&
		original.ProviderSchemaVersion != "" && original.ProviderSchemaVersion == recovered.ProviderSchemaVersion &&
		sha256Pattern.MatchString(original.ProviderSchemaSHA256) && original.ProviderSchemaSHA256 == recovered.ProviderSchemaSHA256 &&
		original.ClaimSchemaVersion != "" && original.ClaimSchemaVersion == recovered.ClaimSchemaVersion &&
		original.ClaimMapperVersion != "" && original.ClaimMapperVersion == recovered.ClaimMapperVersion &&
		original.InputProcessingVersion != "" && original.InputProcessingVersion == recovered.InputProcessingVersion &&
		original.RequestHash.Valid && recovered.RequestHash.Valid && sha256Pattern.MatchString(original.RequestHash.String) &&
		original.RequestHash.String == recovered.RequestHash.String
}

func canonicalTimeAtOrAfter(value, boundary string) bool {
	parsedValue, valueOK := parseCanonicalUTCTime(value)
	parsedBoundary, boundaryOK := parseCanonicalUTCTime(boundary)
	return valueOK && boundaryOK && !parsedValue.Before(parsedBoundary)
}

func canonicalTimeAtOrBefore(value, boundary string) bool {
	parsedValue, valueOK := parseCanonicalUTCTime(value)
	parsedBoundary, boundaryOK := parseCanonicalUTCTime(boundary)
	return valueOK && boundaryOK && !parsedValue.After(parsedBoundary)
}

func parseCanonicalUTCTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	_, offset := parsed.Zone()
	return parsed, offset == 0 && parsed.Format(time.RFC3339Nano) == value
}

func appendOnlyTableCounts(ctx context.Context, database *sql.DB) (map[string]int64, error) {
	counts := make(map[string]int64, len(appendOnlyRecoveryTables))
	for _, table := range appendOnlyRecoveryTables {
		var count int64
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&count); err != nil {
			return nil, fmt.Errorf("count stable recovery table %s: %w", table, err)
		}
		counts[table] = count
	}
	return counts, nil
}

type recoveryDigestQuery struct {
	label string
	query string
	args  []any
}

func stableStateDigest(
	ctx context.Context,
	database *sql.DB,
	processingJobID, processingDocumentID, originalAIRunID string,
	appendOnlyCounts map[string]int64,
) (string, int64, error) {
	if err := validateAppendOnlyCounts(appendOnlyCounts); err != nil {
		return "", 0, err
	}
	queries := make([]recoveryDigestQuery, 0, len(fullyStableRecoveryTables)+len(appendOnlyRecoveryTables)+6)
	for _, table := range fullyStableRecoveryTables {
		queries = append(queries, recoveryDigestQuery{label: table, query: `SELECT * FROM "` + table + `"`})
	}
	queries = append(queries,
		recoveryDigestQuery{label: "documents_except_processing", query: `SELECT * FROM documents WHERE id <> ?`, args: []any{processingDocumentID}},
		recoveryDigestQuery{label: "processing_jobs_except_target", query: `SELECT * FROM processing_jobs WHERE id <> ?`, args: []any{processingJobID}},
		recoveryDigestQuery{label: "ai_runs_except_target_job", query: `SELECT * FROM ai_runs WHERE job_id <> ?`, args: []any{processingJobID}},
		recoveryDigestQuery{
			label: "processing_document_identity",
			query: `SELECT id, tenant_id, storage_key, original_name, declared_mime, detected_mime,
			               size_bytes, sha256, page_count, ingestion_kind, original_object_owner,
			               created_by_user_id, created_at
			        FROM documents WHERE id = ?`,
			args: []any{processingDocumentID},
		},
		recoveryDigestQuery{
			label: "processing_job_identity",
			query: `SELECT id, tenant_id, document_id, kind, cancel_requested_at, created_at, started_at
			        FROM processing_jobs WHERE id = ?`,
			args: []any{processingJobID},
		},
		recoveryDigestQuery{
			label: "original_ai_run_identity",
			query: `SELECT id, tenant_id, job_id, provider_config_id, provider_config_version,
			               provider_config_fingerprint, model, prompt_version, extraction_schema_version,
			               provider_schema_version, provider_schema_sha256, claim_schema_version,
			               claim_mapper_version, input_processing_version, request_hash, started_at
			        FROM ai_runs WHERE id = ?`,
			args: []any{originalAIRunID},
		},
	)
	for _, table := range appendOnlyRecoveryTables {
		queries = append(queries, recoveryDigestQuery{
			label: "append_prefix_" + table,
			query: `SELECT * FROM "` + table + `" ORDER BY created_at, id LIMIT ?`,
			args:  []any{appendOnlyCounts[table]},
		})
	}

	digest := sha256.New()
	var totalRows int64
	for _, query := range queries {
		rowCount, err := hashRecoveryRows(ctx, database, digest, query)
		if err != nil {
			return "", 0, err
		}
		totalRows += rowCount
	}
	return hex.EncodeToString(digest.Sum(nil)), totalRows, nil
}

func hashRecoveryRows(ctx context.Context, database *sql.DB, digest hash.Hash, specification recoveryDigestQuery) (int64, error) {
	rows, err := database.QueryContext(ctx, specification.query, specification.args...)
	if err != nil {
		return 0, fmt.Errorf("read stable recovery state %s: %w", specification.label, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	encodedRows := make([]string, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return 0, err
		}
		encoded, err := encodeRecoveryDigestRow(values)
		if err != nil {
			return 0, fmt.Errorf("encode stable recovery state %s: %w", specification.label, err)
		}
		encodedRows = append(encodedRows, encoded)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	sort.Strings(encodedRows)
	writeDigestFrame(digest, []byte(specification.label))
	for _, column := range columns {
		writeDigestFrame(digest, []byte(column))
	}
	for _, encoded := range encodedRows {
		writeDigestFrame(digest, []byte(encoded))
	}
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(encodedRows)))
	writeDigestFrame(digest, count[:])
	return int64(len(encodedRows)), nil
}

type recoveryDigestCell struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

func encodeRecoveryDigestRow(values []any) (string, error) {
	cells := make([]recoveryDigestCell, len(values))
	for index, value := range values {
		switch typed := value.(type) {
		case nil:
			cells[index] = recoveryDigestCell{Kind: "null"}
		case int64:
			cells[index] = recoveryDigestCell{Kind: "integer", Value: strconv.FormatInt(typed, 10)}
		case float64:
			cells[index] = recoveryDigestCell{Kind: "real", Value: strconv.FormatFloat(typed, 'g', -1, 64)}
		case bool:
			cells[index] = recoveryDigestCell{Kind: "boolean", Value: strconv.FormatBool(typed)}
		case string:
			cells[index] = recoveryDigestCell{Kind: "text", Value: typed}
		case []byte:
			cells[index] = recoveryDigestCell{Kind: "blob", Value: hex.EncodeToString(typed)}
		case time.Time:
			cells[index] = recoveryDigestCell{Kind: "time", Value: typed.UTC().Format(time.RFC3339Nano)}
		default:
			return "", fmt.Errorf("unsupported PostgreSQL value type %T", value)
		}
	}
	encoded, err := json.Marshal(cells)
	return string(encoded), err
}

func writeDigestFrame(digest hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write(value)
}

func validateAppendOnlyCounts(counts map[string]int64) error {
	if len(counts) != len(appendOnlyRecoveryTables) {
		return errors.New("recovery snapshot append-only table counts are incomplete")
	}
	for _, table := range appendOnlyRecoveryTables {
		count, ok := counts[table]
		if !ok || count < 0 {
			return errors.New("recovery snapshot append-only table counts are invalid")
		}
	}
	return nil
}

func appendOnlyChangesAreScoped(
	ctx context.Context,
	database *sql.DB,
	snapshot recoverySnapshot,
	newAIRunID, recoveredFactID string,
) (bool, error) {
	current, err := appendOnlyTableCounts(ctx, database)
	if err != nil {
		return false, err
	}
	delta := make(map[string]int64, len(appendOnlyRecoveryTables))
	for _, table := range appendOnlyRecoveryTables {
		delta[table] = current[table] - snapshot.AppendOnlyTableCounts[table]
		if delta[table] < 0 {
			return false, nil
		}
	}
	if delta["claim_sets"] != 1 || delta["review_decisions"] != 1 || delta["payments"] != 1 || delta["audit_events"] != 1 ||
		delta["field_claims"] < 1 || delta["validation_results"] < 1 || delta["fact_field_origins"] < 1 ||
		delta["duplicate_candidate_decisions"] != delta["duplicate_candidates"] {
		return false, nil
	}

	var claimCount int64
	var claimID string
	if err := database.QueryRowContext(ctx, `
		SELECT count(*), coalesce(min(id), '')
		FROM claim_sets
		WHERE document_id = ? AND origin_ai_run_id = ? AND produced_by_ai_run_id = ?
		  AND status = 'confirmed' AND revision = 1
	`, snapshot.ProcessingDocumentID, newAIRunID, newAIRunID).Scan(&claimCount, &claimID); err != nil {
		return false, err
	}
	var decisionCount int64
	var decisionID string
	if err := database.QueryRowContext(ctx, `
		SELECT count(*), coalesce(min(id), '')
		FROM review_decisions
		WHERE claim_set_id = ? AND action = 'confirm' AND fact_type = 'payment' AND association_mode = 'no_candidate'
	`, claimID).Scan(&decisionCount, &decisionID); err != nil {
		return false, err
	}
	var fieldCount, evidenceCount, validationCount, linkCandidateCount, duplicateCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM field_claims WHERE claim_set_id = ?),
		       (SELECT count(*) FROM evidence e JOIN field_claims f ON f.tenant_id = e.tenant_id AND f.id = e.field_claim_id WHERE f.claim_set_id = ?),
		       (SELECT count(*) FROM validation_results WHERE claim_set_id = ? OR ai_run_id = ?),
		       (SELECT count(*) FROM payment_invoice_link_candidates WHERE claim_set_id = ?),
		       (SELECT count(*) FROM duplicate_candidates WHERE claim_set_id = ?)
	`, claimID, claimID, claimID, newAIRunID, claimID, claimID).
		Scan(&fieldCount, &evidenceCount, &validationCount, &linkCandidateCount, &duplicateCount); err != nil {
		return false, err
	}
	var paymentCount, duplicateDecisionCount, originCount, auditCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM payments WHERE id = ? AND source_review_decision_id = ? AND deleted_at IS NULL),
		       (SELECT count(*) FROM duplicate_candidate_decisions d
		        JOIN duplicate_candidates c ON c.tenant_id = d.tenant_id AND c.id = d.candidate_id
		        WHERE c.claim_set_id = ? AND d.review_decision_id = ?),
		       (SELECT count(*) FROM fact_field_origins o
		        JOIN field_claims f ON f.tenant_id = o.tenant_id AND f.id = o.field_claim_id
		        WHERE o.payment_id = ? AND o.review_decision_id = ? AND f.claim_set_id = ?),
		       (SELECT count(*) FROM audit_events
		        WHERE action = 'fact_confirmed' AND resource_type = 'payment' AND resource_id = ?)
	`, recoveredFactID, decisionID, claimID, decisionID, recoveredFactID, decisionID, claimID, recoveredFactID).
		Scan(&paymentCount, &duplicateDecisionCount, &originCount, &auditCount); err != nil {
		return false, err
	}
	return claimCount == 1 && decisionCount == 1 && paymentCount == 1 && auditCount == 1 &&
		fieldCount == delta["field_claims"] && evidenceCount == delta["evidence"] &&
		validationCount == delta["validation_results"] && linkCandidateCount == delta["payment_invoice_link_candidates"] &&
		duplicateCount == delta["duplicate_candidates"] && duplicateDecisionCount == delta["duplicate_candidate_decisions"] &&
		originCount == delta["fact_field_origins"], nil
}

func isSyntheticLoopbackProvider(rawURL, model string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "/v1" && parsed.Path != "/v1/") || !regexp.MustCompile(`^synthetic-[a-z0-9._-]+$`).MatchString(model) {
		return false
	}
	host := parsed.Hostname()
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func openRecoveryStore(ctx context.Context) (*postgresqladapter.Store, error) {
	config, err := postgresqladapter.RuntimeConfigFromEnvironment()
	if err != nil {
		return nil, err
	}
	return postgresqladapter.Open(ctx, config)
}

func requireEmptyRuntimeDirectories(root string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("object store must be a real directory")
	}
	for _, name := range []string{"staging", "trash", "material-publications"} {
		location := filepath.Join(root, name)
		information, err := os.Lstat(location)
		if err != nil || !information.IsDir() || information.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("object store %s must be a real directory", name)
		}
		entries, err := os.ReadDir(location)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("object store %s directory is not empty", name)
		}
	}
	// 导出尚未启动的当前版本可以没有该非权威目录；一旦存在必须为空。
	exports := filepath.Join(root, "export-spool")
	if info, err := os.Lstat(exports); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("export spool must be a real directory")
		}
		entries, err := os.ReadDir(exports)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return errors.New("export spool directory is not empty")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readRecoverySnapshot(location string) (recoverySnapshot, error) {
	fd, err := unix.Open(location, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return recoverySnapshot{}, errors.New("open protected recovery snapshot")
	}
	file := os.NewFile(uintptr(fd), location)
	if file == nil {
		_ = unix.Close(fd)
		return recoverySnapshot{}, errors.New("open protected recovery snapshot")
	}
	defer file.Close()
	information, err := file.Stat()
	if err != nil {
		return recoverySnapshot{}, errors.New("inspect protected recovery snapshot")
	}
	if !information.Mode().IsRegular() || information.Mode().Perm()&0o077 != 0 || information.Size() < 1 || information.Size() > 1024*1024 {
		return recoverySnapshot{}, errors.New("recovery snapshot must be a protected regular JSON file")
	}
	stat, ok := information.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return recoverySnapshot{}, errors.New("recovery snapshot must have exactly one hard link")
	}
	content, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return recoverySnapshot{}, errors.New("read protected recovery snapshot")
	}
	if len(content) == 0 || len(content) > 1024*1024 {
		return recoverySnapshot{}, errors.New("recovery snapshot size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var snapshot recoverySnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return recoverySnapshot{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return recoverySnapshot{}, errors.New("recovery snapshot contains trailing JSON")
	}
	if _, err := time.Parse(time.RFC3339Nano, snapshot.CapturedAt); err != nil {
		return recoverySnapshot{}, errors.New("recovery snapshot timestamp is invalid")
	}
	if err := validateAppendOnlyCounts(snapshot.AppendOnlyTableCounts); err != nil {
		return recoverySnapshot{}, err
	}
	if snapshot.Kind != snapshotKind || snapshot.Version != snapshotVersion || !exerciseIDPattern.MatchString(snapshot.ExerciseID) || !snapshot.Passed ||
		snapshot.DocumentCount < 1 || snapshot.JobCount != snapshot.DocumentCount || snapshot.SessionCount < 1 ||
		snapshot.AIRunCount != 2 || snapshot.RunningAIRunCount != 1 || snapshot.SucceededAIRunCount != 1 ||
		snapshot.ProcessingAttemptCount < 1 || snapshot.ProcessingVersion < 1 ||
		snapshot.ProcessingJobID == "" || snapshot.ProcessingDocumentID == "" || snapshot.RunningAIRunID == "" ||
		snapshot.ConfirmedFactID == "" || snapshot.ConfirmedDocumentID == "" || snapshot.ProcessingDocumentID == snapshot.ConfirmedDocumentID ||
		!sha256Pattern.MatchString(snapshot.StableStateSHA256) || snapshot.StableStateRowCount < 1 {
		return recoverySnapshot{}, errors.New("recovery snapshot is invalid")
	}
	return snapshot, nil
}

type protectedResult struct {
	file *os.File
}

func reserveResult(location string) (*protectedResult, error) {
	file, err := os.OpenFile(location, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	result := &protectedResult{file: file}
	marker := []byte("{\"kind\":\"smart-bill-manager-protected-output-in-progress\",\"version\":1}\n")
	if err := writeAllAt(file, marker); err != nil {
		_ = result.Close()
		return nil, err
	}
	if err := file.Truncate(int64(len(marker))); err != nil {
		_ = result.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = result.Close()
		return nil, err
	}
	return result, nil
}

func (r *protectedResult) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	file := r.file
	r.file = nil
	return file.Close()
}

func writeResult(result *protectedResult, output io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if result == nil || result.file == nil {
		return errors.New("protected result output is not reserved")
	}
	if err := result.file.Truncate(0); err != nil {
		return err
	}
	if err := writeAllAt(result.file, encoded); err != nil {
		return err
	}
	if err := result.file.Truncate(int64(len(encoded))); err != nil {
		return err
	}
	if err := result.file.Sync(); err != nil {
		return err
	}
	if err := result.Close(); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(safeRecoveryOutput(value))
}

func writeAllAt(file *os.File, content []byte) error {
	for offset := 0; offset < len(content); {
		written, err := file.WriteAt(content[offset:], int64(offset))
		if err != nil {
			return err
		}
		if written == 0 {
			return errors.New("protected result output write made no progress")
		}
		offset += written
	}
	return nil
}

func safeRecoveryOutput(value any) any {
	switch result := value.(type) {
	case emailFixtureResult:
		return map[string]any{
			"kind": result.Kind, "version": result.Version,
			"message_count": 1, "attachment_count": 1,
			"document_count": 1, "job_count": 1, "passed": result.Passed,
		}
	case recoverySnapshot:
		return map[string]any{
			"kind": result.Kind, "version": result.Version,
			"document_count": result.DocumentCount, "job_count": result.JobCount,
			"failed_job_count": result.FailedJobCount, "fact_count": result.FactCount,
			"email_message_count": result.EmailMessageCount, "email_attachment_count": result.EmailAttachmentCount,
			"document_page_count": result.DocumentPageCount, "session_count": result.SessionCount,
			"ai_run_count": result.AIRunCount, "running_ai_run_count": result.RunningAIRunCount,
			"succeeded_ai_run_count":   result.SucceededAIRunCount,
			"processing_attempt_count": result.ProcessingAttemptCount, "processing_version": result.ProcessingVersion,
			"stable_state_row_count": result.StableStateRowCount,
			"passed":                 result.Passed,
		}
	case recoveryVerification:
		return map[string]any{
			"kind": result.Kind, "version": result.Version,
			"document_count": result.DocumentCount, "job_count": result.JobCount,
			"fact_count": result.FactCount, "email_message_count": result.EmailMessageCount,
			"email_attachment_count": result.EmailAttachmentCount, "document_page_count": result.DocumentPageCount,
			"new_session_count": result.NewSessionCount, "historical_session_count": result.HistoricalSessionCount,
			"processing_attempt_count": result.ProcessingAttemptCount, "expected_attempt_delta": result.ExpectedAttemptDelta,
			"processing_version": result.ProcessingVersion, "expected_version_delta": result.ExpectedVersionDelta,
			"lease_expired_ai_run_count": result.LeaseExpiredAIRunCount,
			"running_ai_run_count":       result.RunningAIRunCount, "unleased_processing_job_count": result.UnleasedProcessingJobCount,
			"confirmed_facts_preserved": result.ConfirmedFactsPreserved, "exactly_one_recovered_fact": result.ExactlyOneRecoveredFact,
			"original_running_run_closed": result.OriginalRunningRunClosed, "all_jobs_terminal": result.AllJobsTerminal,
			"stable_state_preserved": result.StableStatePreserved, "append_only_changes_scoped": result.AppendOnlyChangesScoped,
			"passed": result.Passed,
		}
	default:
		return map[string]any{"passed": false}
	}
}
