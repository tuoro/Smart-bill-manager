import assert from "node:assert/strict";
import test from "node:test";

import {
  activationCaseNames,
  bindRecoveryExecution,
  recoveryReportNames,
  buildEvidence,
  safeEvidenceErrorCode,
} from "./write-backup-evidence.mjs";
import { buildReport as buildStaticReport } from "./write-local-static-gates.mjs";

test("evidence merger emits only safe aggregates after every gate passes", () => {
  const inputs = validInputs();
  const evidence = buildEvidence(inputs, {
    branch: "codex/m1-ai-inbox",
    baseSHA: "a".repeat(40),
    releaseInputSHA256: "b".repeat(64),
    imageID: "sha256:" + "c".repeat(64),
    recordedDate: "2026-08-31",
  });
  assert.equal(evidence.overall_status, "passed");
  assert.equal(
    evidence.evidence_version,
    "M4-BACKUP-RESTORE-POSTGRESQL-ADR0033",
  );
  assert.equal(evidence.synthetic_dataset.document_count, 1000);
  assert.equal(evidence.recovered_runtime.rto_passed, true);
  assert.equal(evidence.stage.remaining_release_gates, "separately_required");
  assert.equal(
    evidence.stage.model_accuracy_gate,
    "pending_explicit_authorization",
  );
  assert.equal(evidence.gates.browser_acceptance, "separate_required_gate");
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
        releaseInputSHA256: "b".repeat(64),
        imageID: "sha256:" + "c".repeat(64),
        recordedDate: "2026-08-31",
      }),
    /database verification/,
  );
});

test("evidence merger rejects spliced backup identities, late clocks, and missing fields", () => {
  const metadata = {
    branch: "codex/m1-ai-inbox",
    baseSHA: "a".repeat(40),
    releaseInputSHA256: "b".repeat(64),
    imageID: "sha256:" + "c".repeat(64),
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
  delete incomplete.staticGates.counts.critical_invariants_total;
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
    manifest_version: 3,
    backup_set_id: "a".repeat(32),
    document_count: 1000,
    object_reference_count: 1004,
    unique_object_count: 1003,
    database_table_count: 40,
    operation_started_at_epoch_ms: started,
    operation_finished_at_epoch_ms: finished,
    elapsed_ms: finished - started,
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
  const result = {
    backup: operation("backup", recoveryStarted - 1_000, recoveryStarted - 1),
    verify: operation("verify", recoveryStarted, recoveryStarted + 100),
    scratchRestore: {
      ...operation("restore", recoveryStarted + 100, recoveryStarted + 300),
      invalidated_session_count: snapshot.session_count,
    },
    restore: {
      ...operation("restore", recoveryStarted + 300, recoveryStarted + 500),
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
    staticGates: validStaticGates(),
    activation: {
      report_kind: "restore-activation-gate-result",
      protocol_version: 1,
      build_identity: {
        baseline_head: "a".repeat(40),
        release_input_sha256: "b".repeat(64),
        image_id: "sha256:" + "c".repeat(64),
      },
      cases: Object.fromEntries(
        activationCaseNames.map((name) => [name, true]),
      ),
      passed: true,
    },
  };
  result.executionBinding = bindRecoveryExecution(
    result,
    result.activation.build_identity,
    Object.fromEntries(
      recoveryReportNames.map((name) => [
        name,
        result.activation.build_identity.image_id,
      ]),
    ),
  );
  return result;
}

function validStaticGates() {
  return buildStaticReport({
    expectedHead: "a".repeat(40),
    expectedReleaseInput: "b".repeat(64),
    imageID: "sha256:" + "c".repeat(64),
    baseComposeConfigSha256: "d".repeat(64),
    acceptanceComposeConfigSha256: "e".repeat(64),
    nodeTestFiles: 19,
    webTestFiles: 11,
    webTestCases: 51,
    criticalInvariantsPassed: 245,
    criticalInvariantsTotal: 245,
    domainCoveragePercent: 86.2,
    transportCoveragePercent: 76.42,
    gates: Object.fromEntries(
      [
        "go_test",
        "go_vet",
        "go_build",
        "web_check",
        "node_tests",
        "critical_invariants",
        "coverage",
        "diff_check",
        "sensitive_audit",
        "large_file_audit",
        "temporary_audit",
        "process_audit",
        "release_input_recheck",
      ].map((name) => [name, true]),
    ),
  });
}

test("restore evidence requires actual activation cases with the same candidate identity", () => {
  const metadata = {
    branch: "main",
    baseSHA: "a".repeat(40),
    releaseInputSHA256: "b".repeat(64),
    imageID: "sha256:" + "c".repeat(64),
    recordedDate: "2026-09-05",
  };
  for (const mutate of [
    (inputs) => {
      delete inputs.activation;
    },
    (inputs) => {
      inputs.activation.cases.database_restored_failure_blocks_runtime = false;
    },
    (inputs) => {
      delete inputs.activation.cases.server_and_account_cli_fail_closed;
    },
    (inputs) => {
      inputs.activation.build_identity.image_id = "sha256:" + "f".repeat(64);
    },
    (inputs) => {
      inputs.staticGates.build_identity.release_input_sha256 = "f".repeat(64);
    },
  ]) {
    const inputs = validInputs();
    mutate(inputs);
    assert.throws(() => buildEvidence(inputs, metadata), /activation/);
  }
});

test("restore evidence requires a separate completed scratch restore in the recovery clock", () => {
  const metadata = {
    branch: "main",
    baseSHA: "a".repeat(40),
    releaseInputSHA256: "b".repeat(64),
    imageID: "sha256:" + "c".repeat(64),
    recordedDate: "2026-09-05",
  };
  for (const mutate of [
    (inputs) => {
      delete inputs.scratchRestore;
    },
    (inputs) => {
      inputs.scratchRestore.passed = false;
    },
    (inputs) => {
      inputs.scratchRestore.backup_set_id = "f".repeat(32);
    },
    (inputs) => {
      inputs.scratchRestore.invalidated_session_count += 1;
    },
    (inputs) => {
      inputs.scratchRestore.operation_started_at_epoch_ms =
        inputs.restore.operation_finished_at_epoch_ms;
    },
    (inputs) => {
      inputs.scratchRestore.operation_finished_at_epoch_ms =
        inputs.restore.operation_started_at_epoch_ms + 1;
    },
  ]) {
    const inputs = validInputs();
    mutate(inputs);
    assert.throws(() => buildEvidence(inputs, metadata));
  }
});

test("recovery execution binding rejects old image steps and substituted otherwise valid reports", () => {
  const inputs = validInputs();
  const identity = inputs.activation.build_identity;
  const images = {
    ...inputs.executionBinding.executed_images,
    restore: "sha256:" + "d".repeat(64),
  };
  assert.throws(
    () => bindRecoveryExecution(inputs, identity, images),
    /candidate/,
  );
  const metadata = {
    branch: "main",
    baseSHA: identity.baseline_head,
    releaseInputSHA256: identity.release_input_sha256,
    imageID: identity.image_id,
    recordedDate: "2026-09-05",
  };
  inputs.backup.elapsed_ms += 1;
  assert.throws(() => buildEvidence(inputs, metadata), /execution/);
  delete inputs.executionBinding;
  assert.throws(() => buildEvidence(inputs, metadata));
});
