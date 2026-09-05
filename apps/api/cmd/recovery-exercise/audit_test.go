package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

const (
	recoveryExerciseID  = "00000000-0000-4000-8000-000000000001"
	recoveryBackupSetID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestProtectedResultIsReservedBeforeExerciseMutation(t *testing.T) {
	location := filepath.Join(t.TempDir(), "result.json")
	result, err := reserveResult(location)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	marker, err := os.ReadFile(location)
	if err != nil || !bytes.Contains(marker, []byte("protected-output-in-progress")) {
		t.Fatalf("reserved marker = %q, %v", marker, err)
	}
	if second, err := reserveResult(location); err == nil {
		_ = second.Close()
		t.Fatal("existing protected output was reserved twice")
	}
	var terminal bytes.Buffer
	value := emailFixtureResult{Kind: "m4-recovery-email-fixture", Version: 1, ExerciseID: recoveryExerciseID, Passed: true}
	if err := writeResult(result, &terminal, value); err != nil {
		t.Fatal(err)
	}
	var persisted emailFixtureResult
	content, err := os.ReadFile(location)
	if err != nil || json.Unmarshal(content, &persisted) != nil || persisted.ExerciseID != recoveryExerciseID {
		t.Fatalf("persisted protected result = %#v, %v", persisted, err)
	}
}

func TestRecoverySnapshotAndExactPostRestoreIncrements(t *testing.T) {
	root := t.TempDir()
	objects := filepath.Join(root, "object-store")
	if _, err := localstorage.New(objects); err != nil {
		t.Fatal(err)
	}
	config := postgresqltest.NewDatabase(t)
	config.RuntimeRole = config.User
	if err := postgresqladapter.Migrate(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	store, err := postgresqladapter.Open(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	setRecoveryRuntimeEnvironment(t, config)
	database := store.DB()
	seedRecoveryDatabase(t, database)

	snapshot, err := captureRecoverySnapshot(context.Background(), snapshotOptions{
		Objects: objects, ProcessingJobID: "job-processing",
		ConfirmedFactID: "payment-before", ExerciseID: recoveryExerciseID, ExpectedDocuments: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Passed || snapshot.DocumentCount != 3 || snapshot.FailedJobCount != 1 || snapshot.RunningAIRunID != "run-before" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if _, err := database.Exec(`UPDATE ai_runs SET job_id = 'job-processing' WHERE id = 'run-confirmed'`); err != nil {
		t.Fatal(err)
	}
	if _, err := captureRecoverySnapshot(context.Background(), snapshotOptions{
		Objects: objects, ProcessingJobID: "job-processing",
		ConfirmedFactID: "payment-before", ExerciseID: recoveryExerciseID, ExpectedDocuments: 3,
	}); err == nil || !strings.Contains(err.Error(), "frozen shape") {
		t.Fatalf("ambiguous target AI run error = %v", err)
	}
	if _, err := database.Exec(`UPDATE ai_runs SET job_id = 'job-confirmed' WHERE id = 'run-confirmed'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE documents SET status = 'stored' WHERE id = 'document-email'`); err != nil {
		t.Fatal(err)
	}
	if _, err := captureRecoverySnapshot(context.Background(), snapshotOptions{
		Objects: objects, ProcessingJobID: "job-processing",
		ConfirmedFactID: "payment-before", ExerciseID: recoveryExerciseID, ExpectedDocuments: 3,
	}); err == nil || !strings.Contains(err.Error(), "frozen shape") {
		t.Fatalf("unexpected document status error = %v", err)
	}
	if _, err := database.Exec(`UPDATE documents SET status = 'failed' WHERE id = 'document-email'`); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(root, "snapshot.json")
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshotPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	applyRecoveredState(t, database)
	verified, err := verifyRecoveryState(context.Background(), verifyOptions{
		Snapshot: snapshotPath, RecoveredFactID: "payment-after",
		ExerciseID: recoveryExerciseID, BackupSetID: recoveryBackupSetID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Passed || verified.ProcessingAttemptCount != 2 || verified.LeaseExpiredAIRunCount != 1 || verified.FactCount != 2 ||
		!verified.StableStatePreserved || !verified.AppendOnlyChangesScoped || verified.HistoricalSessionCount != 0 {
		t.Fatalf("verification = %#v", verified)
	}

	if _, err := database.Exec(`UPDATE ai_runs SET prompt_version = 'changed' WHERE id = 'run-after'`); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRecoveryState(context.Background(), verifyOptions{
		Snapshot: snapshotPath, RecoveredFactID: "payment-after",
		ExerciseID: recoveryExerciseID, BackupSetID: recoveryBackupSetID,
	}); err == nil || !strings.Contains(err.Error(), "frozen recovery increments") {
		t.Fatalf("recovered AI run contract error = %v", err)
	}
	if _, err := database.Exec(`UPDATE ai_runs SET prompt_version = 'prompt' WHERE id = 'run-after'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE users SET display_name = 'Changed' WHERE id = 'user'`); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRecoveryState(context.Background(), verifyOptions{
		Snapshot: snapshotPath, RecoveredFactID: "payment-after",
		ExerciseID: recoveryExerciseID, BackupSetID: recoveryBackupSetID,
	}); err == nil || !strings.Contains(err.Error(), "frozen recovery increments") {
		t.Fatalf("unrelated mutation error = %v", err)
	}
	if _, err := database.Exec(`UPDATE users SET display_name = 'Owner' WHERE id = 'user'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO sessions (id, tenant_id, user_id, token_hash, csrf_token_hash, expires_at, created_at, last_seen_at)
		VALUES ('session-extra', 'tenant', 'user', 'token-extra', 'csrf-extra', '2026-09-02T00:00:00Z', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRecoveryState(context.Background(), verifyOptions{
		Snapshot: snapshotPath, RecoveredFactID: "payment-after",
		ExerciseID: recoveryExerciseID, BackupSetID: recoveryBackupSetID,
	}); err == nil || !strings.Contains(err.Error(), "frozen recovery increments") {
		t.Fatalf("extra session error = %v", err)
	}

	if err := os.Chmod(snapshotPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRecoveryState(context.Background(), verifyOptions{
		Snapshot: snapshotPath, RecoveredFactID: "payment-after",
		ExerciseID: recoveryExerciseID, BackupSetID: recoveryBackupSetID,
	}); err == nil || !strings.Contains(err.Error(), "protected regular") {
		t.Fatalf("insecure snapshot error = %v", err)
	}
}

func seedRecoveryDatabase(t *testing.T, database *sql.DB) {
	t.Helper()
	// 本测试只验证恢复审计器的聚合与增量，不复制 Review/Claim 业务夹具。
	if _, err := database.Exec(`DROP TRIGGER payments_require_confirmed_review ON payments`); err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at) VALUES ('user', 'owner@example.invalid', 'hash', 'Owner', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at) VALUES ('tenant', 'Synthetic', 'CNY', 'Asia/Shanghai', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at) VALUES ('tenant', 'user', 'owner', 'active', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO sessions (id, tenant_id, user_id, token_hash, csrf_token_hash, expires_at, created_at, last_seen_at) VALUES ('session', 'tenant', 'user', 'token', 'csrf', '2026-09-01T00:00:00Z', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO provider_configs (id, tenant_id, base_url, encrypted_api_key, model, output_mode, capability_status, capability_checked_at, capability_safe_message, capability_schema_version, capability_schema_sha256, active, version, safe_fingerprint, created_by_user_id, created_at, updated_at) VALUES ('provider', 'tenant', 'http://127.0.0.1:19086/v1', NULL, 'synthetic-m4', 'json_schema', 'passed', '2026-08-31T00:00:00Z', NULL, 'bill-visible-text/2', ?, TRUE, 1, 'safe', 'user', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z')`, []any{strings.Repeat("a", 64)}},
		{`INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at) VALUES ('audit-email', 'tenant', 'user', 'email_archived', 'email_message', 'message', 'request-email', '{}', '2026-08-31T00:00:00Z')`, nil},
		{`INSERT INTO email_sources (id, tenant_id, display_name, mailbox_address_normalized, imap_host_normalized, imap_port, transport_security, status, idempotency_key, request_hash, created_by_user_id, created_at, last_archived_at, version) VALUES ('source', 'tenant', 'Synthetic', 'archive@example.invalid', 'imap.example.invalid', 993, 'implicit_tls', 'active', 'source-key', ?, 'user', '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z', 1)`, []any{strings.Repeat("b", 64)}},
		{`INSERT INTO email_messages (id, tenant_id, email_source_id, external_message_key, raw_storage_key, raw_sha256, raw_size_bytes, subject, sender_address, received_at, status, audit_event_id, created_at) VALUES ('message', 'tenant', 'source', ?, 'mail/raw', ?, 1, 'Synthetic', 'sender@example.invalid', '2026-08-31T00:00:00Z', 'archived', 'audit-email', '2026-08-31T00:00:00Z')`, []any{strings.Repeat("c", 64), strings.Repeat("d", 64)}},
	}
	for index, status := range []string{"failed", "completed", "processing"} {
		documentID := []string{"document-email", "document-confirmed", "document-processing"}[index]
		jobID := []string{"job-failed", "job-confirmed", "job-processing"}[index]
		statements = append(statements,
			struct {
				query string
				args  []any
			}{`INSERT INTO documents (id, tenant_id, storage_key, original_name, declared_mime, detected_mime, size_bytes, sha256, page_count, status, ingestion_kind, original_object_owner, created_by_user_id, created_at) VALUES (?, 'tenant', ?, ?, 'image/png', 'image/png', 1, ?, 1, ?, ?, ?, 'user', '2026-08-31T00:00:00Z')`, []any{documentID, "objects/" + documentID, documentID + ".png", strings.Repeat(string(rune('e'+index)), 64), status, map[bool]string{true: "email_attachment", false: "upload"}[index == 0], map[bool]string{true: "email_attachment", false: "document"}[index == 0]}},
		)
		jobArgs := []any{jobID, documentID, status}
		if status == "processing" {
			statements = append(statements, struct {
				query string
				args  []any
			}{`INSERT INTO processing_jobs (id, tenant_id, document_id, kind, status, attempt_count, lease_owner, lease_expires_at, created_at, started_at, version) VALUES (?, 'tenant', ?, 'document_process', ?, 1, 'worker', ?, '2026-08-31T00:00:00Z', '2026-08-31T00:00:00Z', 2)`, append(jobArgs, future)})
		} else {
			errorCode := any(nil)
			if status == "failed" {
				errorCode = "provider_config_missing"
			}
			statements = append(statements, struct {
				query string
				args  []any
			}{`INSERT INTO processing_jobs (id, tenant_id, document_id, kind, status, attempt_count, error_code, created_at, finished_at, version) VALUES (?, 'tenant', ?, 'document_process', ?, 1, ?, '2026-08-31T00:00:00Z', '2026-08-31T00:00:01Z', 2)`, append(jobArgs, errorCode)})
		}
	}
	statements = append(statements,
		struct {
			query string
			args  []any
		}{`INSERT INTO email_attachments (id, tenant_id, email_message_id, part_index, storage_key, original_name, declared_mime, disposition, size_bytes, sha256, processing_status, document_id, created_at) VALUES ('attachment', 'tenant', 'message', 1, 'objects/document-email', 'email.png', 'image/png', 'attachment', 1, ?, 'queued', 'document-email', '2026-08-31T00:00:00Z')`, []any{strings.Repeat("e", 64)}},
		struct {
			query string
			args  []any
		}{`INSERT INTO document_pages (id, tenant_id, document_id, page_number, derived_image_storage_key, width, height, sha256, processing_version, visual_fingerprint_version, dhash64, ahash64, dhash_band_0, dhash_band_1, dhash_band_2, dhash_band_3, created_at) VALUES ('page-confirmed', 'tenant', 'document-confirmed', 1, 'pages/one', 1, 1, ?, 'document-normalize/2', 'page-visual-dedup/1', '0000000000000000', 'ffffffffffffffff', 0, 0, 0, 0, '2026-08-31T00:00:00Z')`, []any{strings.Repeat("1", 64)}},
		struct {
			query string
			args  []any
		}{`INSERT INTO document_pages (id, tenant_id, document_id, page_number, derived_image_storage_key, width, height, sha256, processing_version, visual_fingerprint_version, dhash64, ahash64, dhash_band_0, dhash_band_1, dhash_band_2, dhash_band_3, created_at) VALUES ('page-processing', 'tenant', 'document-processing', 1, 'pages/two', 64, 64, ?, 'document-normalize/2', 'page-visual-dedup/1', '1111111111111111', 'eeeeeeeeeeeeeeee', 4369, 4369, 4369, 4369, '2026-08-31T00:00:00Z')`, []any{strings.Repeat("3", 64)}},
		struct {
			query string
			args  []any
		}{`INSERT INTO ai_runs (id, tenant_id, job_id, provider_config_id, provider_config_version, provider_config_fingerprint, model, prompt_version, extraction_schema_version, provider_schema_version, provider_schema_sha256, claim_schema_version, claim_mapper_version, input_processing_version, request_hash, outcome, started_at) VALUES ('run-before', 'tenant', 'job-processing', 'provider', 1, 'safe', 'synthetic-m4', 'prompt', 'extraction', 'bill-visible-text/2', ?, 'claim', 'mapper', 'normalize', ?, 'running', '2026-08-31T00:00:00Z')`, []any{strings.Repeat("a", 64), strings.Repeat("2", 64)}},
		struct {
			query string
			args  []any
		}{`INSERT INTO ai_runs (id, tenant_id, job_id, provider_config_id, provider_config_version, provider_config_fingerprint, model, prompt_version, extraction_schema_version, provider_schema_version, provider_schema_sha256, claim_schema_version, claim_mapper_version, input_processing_version, request_hash, response_hash, input_tokens, output_tokens, latency_ms, outcome, started_at, finished_at) VALUES ('run-confirmed', 'tenant', 'job-confirmed', 'provider', 1, 'safe', 'synthetic-m4', 'prompt', 'extraction', 'bill-visible-text/2', ?, 'claim', 'mapper', 'normalize', ?, ?, 10, 8, 1, 'succeeded', '2026-08-31T00:00:00Z', '2026-08-31T00:00:01Z')`, []any{strings.Repeat("a", 64), strings.Repeat("4", 64), strings.Repeat("5", 64)}},
		struct {
			query string
			args  []any
		}{`INSERT INTO claim_sets (id, tenant_id, document_id, origin_ai_run_id, produced_by_ai_run_id, document_type, status, revision, optimistic_version, created_at) VALUES ('claim-before', 'tenant', 'document-confirmed', 'run-confirmed', 'run-confirmed', 'payment', 'confirmed', 1, 2, '2026-08-31T00:00:01Z')`, nil},
		struct {
			query string
			args  []any
		}{`INSERT INTO field_claims (id, tenant_id, claim_set_id, field_path, value_type, presence, typed_value_json, normalized_value, source, created_at) VALUES ('field-before', 'tenant', 'claim-before', 'payment.amount_minor', 'money_minor', 'present', '12345', '12345', 'ai', '2026-08-31T00:00:01Z')`, nil},
		struct {
			query string
			args  []any
		}{`INSERT INTO evidence (id, tenant_id, field_claim_id, document_page_id, quote, evidence_hash, created_at) VALUES ('evidence-before', 'tenant', 'field-before', 'page-confirmed', 'CNY 123.45', ?, '2026-08-31T00:00:01Z')`, []any{strings.Repeat("6", 64)}},
		struct {
			query string
			args  []any
		}{`INSERT INTO validation_results (id, tenant_id, claim_set_id, field_claim_id, rule_code, severity, status, safe_message, rule_version, created_at) VALUES ('validation-before', 'tenant', 'claim-before', 'field-before', 'synthetic', 'info', 'passed', 'passed', 'test/1', '2026-08-31T00:00:01Z')`, nil},
		struct {
			query string
			args  []any
		}{`INSERT INTO review_decisions (id, tenant_id, claim_set_id, actor_user_id, action, fact_type, association_mode, duplicate_plan_hash, idempotency_key, expected_revision, created_at) VALUES ('decision-before', 'tenant', 'claim-before', 'user', 'confirm', 'payment', 'no_candidate', ?, 'decision-before-key', 1, '2026-08-31T00:00:01Z')`, []any{strings.Repeat("7", 64)}},
		struct {
			query string
			args  []any
		}{`INSERT INTO payments (id, tenant_id, source_review_decision_id, amount_minor, currency, merchant, transaction_time, source_timezone, business_date, created_at, updated_at) VALUES ('payment-before', 'tenant', 'decision-before', 12345, 'CNY', 'Synthetic', '2026-08-31T00:00:00Z', 'Asia/Shanghai', '2026-08-31', '2026-08-31T00:00:01Z', '2026-08-31T00:00:01Z')`, nil},
		struct {
			query string
			args  []any
		}{`INSERT INTO fact_field_origins (id, tenant_id, payment_id, field_path, field_claim_id, review_decision_id, created_at) VALUES ('origin-before', 'tenant', 'payment-before', 'amount_minor', 'field-before', 'decision-before', '2026-08-31T00:00:01Z')`, nil},
		struct {
			query string
			args  []any
		}{`INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at) VALUES ('audit-confirmed', 'tenant', 'user', 'fact_confirmed', 'payment', 'payment-before', 'request-confirmed', '{}', '2026-08-31T00:00:01Z')`, nil},
	)
	for _, statement := range statements {
		if _, err := database.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed recovery database: %v\n%s", err, statement.query)
		}
	}
}

func applyRecoveredState(t *testing.T, database *sql.DB) {
	t.Helper()
	createdAt := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	statements := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM sessions`, nil},
		{`INSERT INTO sessions (id, tenant_id, user_id, token_hash, csrf_token_hash, expires_at, created_at, last_seen_at) VALUES ('session-after', 'tenant', 'user', 'token-after', 'csrf-after', '2026-09-02T00:00:00Z', ?, ?)`, []any{createdAt, createdAt}},
		{`UPDATE documents SET status = 'completed' WHERE id = 'document-processing'`, nil},
		{`UPDATE processing_jobs SET status = 'completed', attempt_count = 2, lease_owner = NULL, lease_expires_at = NULL, finished_at = ?, version = version + 3 WHERE id = 'job-processing'`, []any{createdAt}},
		{`UPDATE ai_runs SET outcome = 'failed', error_code = 'lease_expired', finished_at = ? WHERE id = 'run-before'`, []any{createdAt}},
		{`INSERT INTO ai_runs (id, tenant_id, job_id, provider_config_id, provider_config_version, provider_config_fingerprint, model, prompt_version, extraction_schema_version, provider_schema_version, provider_schema_sha256, claim_schema_version, claim_mapper_version, input_processing_version, request_hash, response_hash, input_tokens, output_tokens, latency_ms, outcome, started_at, finished_at) VALUES ('run-after', 'tenant', 'job-processing', 'provider', 1, 'safe', 'synthetic-m4', 'prompt', 'extraction', 'bill-visible-text/2', ?, 'claim', 'mapper', 'normalize', ?, ?, 10, 8, 1, 'succeeded', ?, ?)`, []any{strings.Repeat("a", 64), strings.Repeat("2", 64), strings.Repeat("9", 64), createdAt, createdAt}},
		{`INSERT INTO claim_sets (id, tenant_id, document_id, origin_ai_run_id, produced_by_ai_run_id, document_type, status, revision, optimistic_version, created_at) VALUES ('claim-after', 'tenant', 'document-processing', 'run-after', 'run-after', 'payment', 'confirmed', 1, 2, ?)`, []any{createdAt}},
		{`INSERT INTO field_claims (id, tenant_id, claim_set_id, field_path, value_type, presence, typed_value_json, normalized_value, source, created_at) VALUES ('field-after', 'tenant', 'claim-after', 'payment.amount_minor', 'money_minor', 'present', '12345', '12345', 'ai', ?)`, []any{createdAt}},
		{`INSERT INTO evidence (id, tenant_id, field_claim_id, document_page_id, quote, evidence_hash, created_at) VALUES ('evidence-after', 'tenant', 'field-after', 'page-processing', 'CNY 123.45', ?, ?)`, []any{strings.Repeat("a", 64), createdAt}},
		{`INSERT INTO validation_results (id, tenant_id, claim_set_id, field_claim_id, rule_code, severity, status, safe_message, rule_version, created_at) VALUES ('validation-after', 'tenant', 'claim-after', 'field-after', 'synthetic', 'info', 'passed', 'passed', 'test/1', ?)`, []any{createdAt}},
		{`INSERT INTO review_decisions (id, tenant_id, claim_set_id, actor_user_id, action, fact_type, association_mode, duplicate_plan_hash, idempotency_key, expected_revision, created_at) VALUES ('decision-after', 'tenant', 'claim-after', 'user', 'confirm', 'payment', 'no_candidate', ?, 'decision-after-key', 1, ?)`, []any{strings.Repeat("b", 64), createdAt}},
		{`INSERT INTO payments (id, tenant_id, source_review_decision_id, amount_minor, currency, merchant, transaction_time, source_timezone, business_date, created_at, updated_at) VALUES ('payment-after', 'tenant', 'decision-after', 12345, 'CNY', 'Synthetic', '2026-08-31T00:01:00Z', 'Asia/Shanghai', '2026-08-31', ?, ?)`, []any{createdAt, createdAt}},
		{`INSERT INTO fact_field_origins (id, tenant_id, payment_id, field_path, field_claim_id, review_decision_id, created_at) VALUES ('origin-after', 'tenant', 'payment-after', 'amount_minor', 'field-after', 'decision-after', ?)`, []any{createdAt}},
		{`INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at) VALUES ('audit-after', 'tenant', 'user', 'fact_confirmed', 'payment', 'payment-after', 'request-after', '{}', ?)`, []any{createdAt}},
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("apply recovery state: %v\n%s", err, statement.query)
		}
	}
}

func setRecoveryRuntimeEnvironment(t *testing.T, config postgresqladapter.Config) {
	t.Helper()
	t.Setenv("SBM_POSTGRES_HOST", config.Host)
	t.Setenv("SBM_POSTGRES_PORT", strconv.Itoa(int(config.Port)))
	t.Setenv("SBM_POSTGRES_DATABASE", config.Database)
	t.Setenv("SBM_POSTGRES_USER", config.User)
	t.Setenv("SBM_POSTGRES_PASSWORD_FILE", config.PasswordFile)
	t.Setenv("SBM_POSTGRES_SSL_MODE", config.SSLMode)
	t.Setenv("SBM_MIGRATIONS_DIR", config.MigrationsDir)
}
