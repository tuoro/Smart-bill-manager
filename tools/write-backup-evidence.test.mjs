import assert from "node:assert/strict";
import test from "node:test";

import {
  buildEvidence,
  safeEvidenceErrorCode,
} from "./write-backup-evidence.mjs";

test("evidence merger emits only safe aggregates after every gate passes", () => {
  const inputs = validInputs();
  const evidence = buildEvidence(inputs, {
    branch: "codex/m1-ai-inbox",
    baseSHA: "a".repeat(40),
    recordedDate: "2026-08-31",
  });
  assert.equal(evidence.overall_status, "passed");
  assert.equal(evidence.synthetic_dataset.document_count, 1000);
  assert.equal(evidence.recovered_runtime.rto_passed, true);
  const encoded = JSON.stringify(evidence);
  for (const forbidden of [
    "job-processing",
    "run-before",
    "payment-before",
    "payment-after",
    "manifest_sha256",
    "master-key",
    "cookie",
    "api_key",
  ]) {
    assert.equal(encoded.includes(forbidden), false, forbidden);
  }
});

test("evidence merger rejects a passing label with an invalid exact increment", () => {
  const inputs = validInputs();
  inputs.database.processing_attempt_count += 1;
  assert.throws(
    () =>
      buildEvidence(inputs, {
        branch: "codex/m1-ai-inbox",
        baseSHA: "a".repeat(40),
        recordedDate: "2026-08-31",
      }),
    /database verification/,
  );
});

test("evidence merger rejects spliced backup identities, late clocks, and missing fields", () => {
  const metadata = {
    branch: "codex/m1-ai-inbox",
    baseSHA: "a".repeat(40),
    recordedDate: "2026-08-31",
  };
  const spliced = validInputs();
  spliced.verify.backup_set_id = "b".repeat(32);
  assert.throws(() => buildEvidence(spliced, metadata), /disagree/);

  const lateClock = validInputs();
  lateClock.api.recovery_started_at_epoch_ms =
    lateClock.verify.operation_finished_at_epoch_ms + 1;
  assert.throws(() => buildEvidence(lateClock, metadata), /recovery clock/);

  const incomplete = validInputs();
  delete incomplete.staticGates.recovery_controller_tests.total;
  assert.throws(() => buildEvidence(incomplete, metadata), /missing/);
});

test("evidence merger error output does not repeat protected paths", () => {
  const code = safeEvidenceErrorCode(
    new Error("evidence input must be owner-only: /secure/private/state.json"),
  );
  assert.equal(code, "protected_input_invalid");
  assert.doesNotMatch(code, /secure|private|state/);
});

function validInputs() {
  const recoveryStarted = Date.parse("2026-08-31T00:00:01.000Z");
  const operation = (name, started, finished) => ({
    operation: name,
    manifest_kind: "smart-bill-manager-backup",
    manifest_version: 2,
    backup_set_id: "a".repeat(32),
    document_count: 1000,
    object_reference_count: 1004,
    unique_object_count: 1003,
    database_table_count: 40,
    operation_started_at_epoch_ms: started,
    operation_finished_at_epoch_ms: finished,
    elapsed_ms: 100,
    passed: true,
  });
  const snapshot = {
    kind: "m4-recovery-database-snapshot",
    version: 1,
    exercise_id: "00000000-0000-4000-8000-000000000001",
    captured_at: "2026-08-31T00:00:00Z",
    document_count: 1000,
    job_count: 1000,
    failed_job_count: 998,
    fact_count: 1,
    email_message_count: 1,
    email_attachment_count: 1,
    document_page_count: 2,
    session_count: 3,
    ai_run_count: 2,
    running_ai_run_count: 1,
    succeeded_ai_run_count: 1,
    processing_job_id: "job-processing",
    processing_document_id: "document-processing",
    processing_attempt_count: 1,
    processing_version: 2,
    running_ai_run_id: "run-before",
    confirmed_fact_id: "payment-before",
    confirmed_document_id: "document-confirmed",
    stable_state_sha256: "c".repeat(64),
    stable_state_row_count: 2000,
    append_only_table_counts: Object.fromEntries(
      [
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
      ].map((name) => [name, 1]),
    ),
    passed: true,
  };
  return {
    backup: operation("backup", recoveryStarted - 1_000, recoveryStarted - 1),
    verify: operation("verify", recoveryStarted, recoveryStarted + 100),
    restore: {
      ...operation("restore", recoveryStarted + 100, recoveryStarted + 500),
      invalidated_session_count: snapshot.session_count,
    },
    snapshot,
    api: {
      report_kind: "m4-backup-restore-api-result",
      report_version: 1,
      exercise_id: snapshot.exercise_id,
      backup_set_id: "a".repeat(32),
      verified_at: "2026-08-31T00:10:00.000Z",
      verified_at_epoch_ms: Date.parse("2026-08-31T00:10:00.000Z"),
      recovery_started_at_epoch_ms: recoveryStarted,
      rto_elapsed_ms: 600000,
      rto_limit_ms: 1800000,
      ready_verified: true,
      old_session_rejected: true,
      new_login_succeeded: true,
      restored_snapshot_verified_before_continuation: true,
      api_job_window_count: 200,
      payment_count: 2,
      email_message_count: 1,
      document_queries_verified: 3,
      authenticated_downloads_verified: 5,
      processing_attempt_count_before_backup: 1,
      processing_attempt_count_after_recovery: 2,
      recovered_fact_id: "payment-after",
      passed: true,
    },
    database: {
      kind: "m4-recovery-database-verification",
      version: 1,
      exercise_id: snapshot.exercise_id,
      backup_set_id: "a".repeat(32),
      recovered_fact_id: "payment-after",
      verified_at: "2026-08-31T00:10:01Z",
      document_count: 1000,
      job_count: 1000,
      fact_count: 2,
      email_message_count: 1,
      email_attachment_count: 1,
      document_page_count: 2,
      new_session_count: 1,
      historical_session_count: 0,
      processing_attempt_count: 2,
      expected_attempt_delta: 1,
      processing_version: 5,
      expected_version_delta: 3,
      lease_expired_ai_run_count: 1,
      running_ai_run_count: 0,
      unleased_processing_job_count: 0,
      confirmed_facts_preserved: true,
      exactly_one_recovered_fact: true,
      original_running_run_closed: true,
      all_jobs_terminal: true,
      stable_state_preserved: true,
      append_only_changes_scoped: true,
      passed: true,
    },
    staticGates: {
      kind: "m4-backup-static-gates",
      version: 1,
      go_test_all: "passed",
      go_vet_all: "passed",
      go_build_all_without_vcs_stamp: "passed",
      web_check: "passed",
      recovery_controller_tests: { passed: 14, failed: 0, total: 14 },
      critical_invariant_branches: {
        passed: 136,
        total: 136,
        percentage: 100,
      },
      domain_application_statement_coverage: {
        percentage: 85.71,
        covered: 3101,
        total: 3618,
        required_percentage: 85,
        passed: true,
      },
      infrastructure_transport_statement_coverage: {
        percentage: 75.18,
        covered: 3625,
        total: 4822,
        required_percentage: 70,
        passed: true,
      },
      git_diff_check: "passed",
      credential_and_private_asset_scan: "passed",
      binary_and_large_file_check: "passed",
      temporary_artifact_check: "passed",
      current_slice_process_residue_check: "passed",
    },
  };
}
