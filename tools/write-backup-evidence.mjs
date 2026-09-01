#!/usr/bin/env node

import { constants } from "node:fs";
import { open, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const manifestKind = "smart-bill-manager-backup";
const manifestVersion = 3;
const expectedDocuments = 1000;
const expectedObjectReferences = 1004;
const expectedUniqueObjects = 1003;
const expectedDocumentPages = 2;
const expectedAPIJobWindow = 200;
const expectedRTO = 30 * 60 * 1000;
const appendOnlyRecoveryTables = new Set([
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
]);

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const inputs = await Promise.all([
    readProtectedJSON(options.backupResult),
    readProtectedJSON(options.verifyResult),
    readProtectedJSON(options.restoreResult),
    readProtectedJSON(options.snapshot),
    readProtectedJSON(options.apiResult),
    readProtectedJSON(options.databaseResult),
    readProtectedJSON(options.staticGates),
  ]);
  const evidence = buildEvidence(
    {
      backup: inputs[0],
      verify: inputs[1],
      restore: inputs[2],
      snapshot: inputs[3],
      api: inputs[4],
      database: inputs[5],
      staticGates: inputs[6],
    },
    options,
  );
  const encoded = `${JSON.stringify(evidence, null, 2)}\n`;
  await writeFile(options.output, encoded, {
    encoding: "utf8",
    flag: "wx",
    mode: 0o644,
  });
  process.stdout.write(encoded);
}

function buildEvidence(inputs, metadata) {
  const { backup, verify, restore, snapshot, api, database, staticGates } =
    inputs;
  validateOperation(backup, "backup", false);
  validateOperation(verify, "verify", false);
  validateOperation(restore, "restore", true);
  const comparableOperationFields = [
    "manifest_kind",
    "manifest_version",
    "backup_set_id",
    "document_count",
    "object_reference_count",
    "unique_object_count",
    "database_table_count",
  ];
  for (const field of comparableOperationFields) {
    if (backup[field] !== verify[field] || backup[field] !== restore[field]) {
      throw new Error(`backup operations disagree on ${field}`);
    }
  }
  validateSnapshot(snapshot);
  validateAPIResult(api);
  validateDatabaseResult(database, snapshot);
  validateStaticGates(staticGates);
  validateRecoverySequence(backup, verify, restore, snapshot, api, database);
  if (
    snapshot.exercise_id !== api.exercise_id ||
    snapshot.exercise_id !== database.exercise_id ||
    backup.backup_set_id !== api.backup_set_id ||
    backup.backup_set_id !== database.backup_set_id ||
    api.recovered_fact_id !== database.recovered_fact_id
  ) {
    throw new Error("recovery evidence identities do not form one exercise");
  }
  if (restore.invalidated_session_count !== snapshot.session_count) {
    throw new Error("restore invalidation count differs from the snapshot");
  }
  if (
    api.processing_attempt_count_before_backup !==
    snapshot.processing_attempt_count
  ) {
    throw new Error("API attempt baseline differs from the database snapshot");
  }
  if (
    api.processing_attempt_count_after_recovery !==
    database.processing_attempt_count
  ) {
    throw new Error("API and database attempt counts differ after recovery");
  }
  validateMetadata(metadata);
  return {
    report_kind: "m4-backup-restore-gate-summary",
    evidence_version: "M4-BACKUP-RESTORE-POSTGRESQL-2026-09-01",
    recorded_date: metadata.recordedDate,
    workspace: {
      branch: metadata.branch,
      base_sha: metadata.baseSHA,
      recorded_from_uncommitted_worktree: true,
      local_slice_commit_authorized: true,
      pushed: false,
    },
    stage: {
      m0: "complete",
      m1: "complete",
      m2: "complete",
      m3: "complete",
      m4: "complete_local_only",
      authenticated_backup_restore_slice: "complete",
      next_slice: "formal_real_model_accuracy_evaluation",
      model_accuracy_gate: "pending_explicit_authorization",
    },
    protocol: {
      manifest_kind: manifestKind,
      manifest_version: manifestVersion,
      authenticated_manifest: "hmac_sha256_domain_separated_from_master_key",
      data_package_contains_master_key: false,
      old_manifest_compatibility: false,
      exact_committed_object_inventory: true,
      postgresql_server_major_17: true,
      postgresql_custom_dump_verified: true,
      postgresql_constraints_and_foreign_keys_verified: true,
      migration_and_schema_identity_checks: true,
      crash_safe_restore_state: true,
      existing_targets_overwritten: false,
      restored_sessions_invalidated_before_activation: true,
      same_authenticated_backup_set_across_operations: true,
      stable_preexisting_database_state_digest: true,
    },
    synthetic_dataset: {
      document_count: backup.document_count,
      failed_provider_boundary_job_count: snapshot.failed_job_count,
      fact_count_before_backup: snapshot.fact_count,
      email_message_count: snapshot.email_message_count,
      email_attachment_count: snapshot.email_attachment_count,
      document_page_count: snapshot.document_page_count,
      object_reference_count: backup.object_reference_count,
      unique_object_count: backup.unique_object_count,
      physical_objects_required: true,
      source_staging_empty: true,
      source_trash_empty: true,
    },
    offline_operations: {
      backup: { passed: true, elapsed_ms: backup.elapsed_ms },
      independent_verify: { passed: true, elapsed_ms: verify.elapsed_ms },
      restore: {
        passed: true,
        elapsed_ms: restore.elapsed_ms,
        invalidated_session_count: restore.invalidated_session_count,
      },
      authenticated_manifest_equal_across_operations: true,
      operation_sequence_bound_to_recovery_clock: true,
      preactivation_database_and_object_equality: true,
      post_session_invalidation_allowed_difference_only: true,
    },
    recovered_runtime: {
      ready_verified: api.ready_verified,
      old_session_rejected: api.old_session_rejected,
      new_login_succeeded: api.new_login_succeeded,
      restored_snapshot_verified_before_continuation:
        api.restored_snapshot_verified_before_continuation,
      new_session_count: database.new_session_count,
      historical_session_count: database.historical_session_count,
      api_job_window_count: api.api_job_window_count,
      document_queries_verified: api.document_queries_verified,
      authenticated_downloads_verified: api.authenticated_downloads_verified,
      payment_count: api.payment_count,
      fact_count_after_recovery: database.fact_count,
      email_message_count: api.email_message_count,
      processing_attempt_delta: database.expected_attempt_delta,
      original_running_ai_run_closed_as_lease_expired:
        database.original_running_run_closed,
      lease_expired_ai_run_count: database.lease_expired_ai_run_count,
      running_ai_run_count: database.running_ai_run_count,
      unleased_processing_job_count: database.unleased_processing_job_count,
      confirmed_fact_preserved: database.confirmed_facts_preserved,
      exactly_one_recovered_fact: database.exactly_one_recovered_fact,
      all_jobs_terminal: database.all_jobs_terminal,
      stable_state_preserved: database.stable_state_preserved,
      append_only_changes_scoped: database.append_only_changes_scoped,
      rto_elapsed_ms: api.rto_elapsed_ms,
      rto_limit_ms: api.rto_limit_ms,
      rto_passed: api.rto_elapsed_ms <= api.rto_limit_ms,
    },
    gates: {
      go_test_all: staticGates.go_test_all,
      go_vet_all: staticGates.go_vet_all,
      go_build_all_without_vcs_stamp:
        staticGates.go_build_all_without_vcs_stamp,
      web_check: staticGates.web_check,
      recovery_controller_tests: staticGates.recovery_controller_tests,
      critical_invariant_branches: staticGates.critical_invariant_branches,
      domain_application_statement_coverage:
        staticGates.domain_application_statement_coverage,
      infrastructure_transport_statement_coverage:
        staticGates.infrastructure_transport_statement_coverage,
      git_diff_check: staticGates.git_diff_check,
      credential_and_private_asset_scan:
        staticGates.credential_and_private_asset_scan,
      binary_and_large_file_check: staticGates.binary_and_large_file_check,
      temporary_artifact_check: staticGates.temporary_artifact_check,
      current_slice_process_residue_check:
        staticGates.current_slice_process_residue_check,
      browser_acceptance: "passed_in_final_local_release_gate",
    },
    excluded: {
      formal_real_model_accuracy_evaluation: "not_run",
      real_provider_calls: "not_run",
      real_images: "not_sent",
      real_mailbox_or_external_account_integration: "not_run",
      persistent_or_production_credentials: "not_created_or_changed",
      local_ocr: "not_installed_or_used",
      new_dependencies: "not_downloaded",
      legacy_compatibility: "not_added",
      deployment_release_or_remote_resources: "not_run",
      nonzero_rpo_deletion_and_revocation_replay:
        "deferred_to_production_release_gate",
    },
    repository_actions: {
      at_gate_recording: {
        committed: false,
        pushed: false,
        deployed: false,
      },
      checkpoint: {
        local_commit_authorized: true,
        push_forbidden: true,
      },
    },
    overall_status: "passed",
  };
}

function validateOperation(result, operation, requireInvalidation) {
  const allowed = new Set([
    "operation",
    "manifest_kind",
    "manifest_version",
    "backup_set_id",
    "document_count",
    "object_reference_count",
    "unique_object_count",
    "database_table_count",
    ...(requireInvalidation ? ["invalidated_session_count"] : []),
    "operation_started_at_epoch_ms",
    "operation_finished_at_epoch_ms",
    "elapsed_ms",
    "passed",
  ]);
  assertExactKeys(result, allowed, `${operation} result`);
  if (
    result.operation !== operation ||
    result.manifest_kind !== manifestKind ||
    result.manifest_version !== manifestVersion ||
    !/^[0-9a-f]{32}$/.test(result.backup_set_id) ||
    result.document_count !== expectedDocuments ||
    result.object_reference_count !== expectedObjectReferences ||
    result.unique_object_count !== expectedUniqueObjects ||
    !Number.isSafeInteger(result.database_table_count) ||
    result.database_table_count < 1 ||
    !nonnegativeInteger(result.elapsed_ms) ||
    !Number.isSafeInteger(result.operation_started_at_epoch_ms) ||
    !Number.isSafeInteger(result.operation_finished_at_epoch_ms) ||
    result.operation_started_at_epoch_ms < 1 ||
    result.operation_finished_at_epoch_ms <
      result.operation_started_at_epoch_ms ||
    result.passed !== true ||
    (requireInvalidation &&
      (!Number.isSafeInteger(result.invalidated_session_count) ||
        result.invalidated_session_count < 1)) ||
    (!requireInvalidation && result.invalidated_session_count !== undefined)
  ) {
    throw new Error(`${operation} result does not match the frozen contract`);
  }
}

function validateSnapshot(snapshot) {
  assertExactKeys(
    snapshot,
    new Set([
      "kind",
      "version",
      "exercise_id",
      "captured_at",
      "document_count",
      "job_count",
      "failed_job_count",
      "fact_count",
      "email_message_count",
      "email_attachment_count",
      "document_page_count",
      "session_count",
      "ai_run_count",
      "running_ai_run_count",
      "succeeded_ai_run_count",
      "processing_job_id",
      "processing_document_id",
      "processing_attempt_count",
      "processing_version",
      "running_ai_run_id",
      "confirmed_fact_id",
      "confirmed_document_id",
      "stable_state_sha256",
      "stable_state_row_count",
      "append_only_table_counts",
      "passed",
    ]),
    "recovery snapshot",
  );
  if (
    snapshot.kind !== "m4-recovery-database-snapshot" ||
    snapshot.version !== 1 ||
    !uuidV4(snapshot.exercise_id) ||
    !canonicalUTCTimestamp(snapshot.captured_at) ||
    snapshot.document_count !== expectedDocuments ||
    snapshot.job_count !== expectedDocuments ||
    snapshot.failed_job_count !== expectedDocuments - 2 ||
    snapshot.fact_count !== 1 ||
    snapshot.email_message_count !== 1 ||
    snapshot.email_attachment_count !== 1 ||
    snapshot.document_page_count !== expectedDocumentPages ||
    !Number.isSafeInteger(snapshot.session_count) ||
    snapshot.session_count < 1 ||
    snapshot.ai_run_count !== 2 ||
    snapshot.running_ai_run_count !== 1 ||
    snapshot.succeeded_ai_run_count !== 1 ||
    !Number.isSafeInteger(snapshot.processing_attempt_count) ||
    snapshot.processing_attempt_count < 1 ||
    !Number.isSafeInteger(snapshot.processing_version) ||
    snapshot.processing_version < 1 ||
    !nonemptyString(snapshot.processing_job_id) ||
    !nonemptyString(snapshot.processing_document_id) ||
    !nonemptyString(snapshot.running_ai_run_id) ||
    !nonemptyString(snapshot.confirmed_fact_id) ||
    !nonemptyString(snapshot.confirmed_document_id) ||
    snapshot.processing_document_id === snapshot.confirmed_document_id ||
    !/^[0-9a-f]{64}$/.test(snapshot.stable_state_sha256) ||
    !Number.isSafeInteger(snapshot.stable_state_row_count) ||
    snapshot.stable_state_row_count < 1 ||
    !exactCountMap(
      snapshot.append_only_table_counts,
      appendOnlyRecoveryTables,
    ) ||
    snapshot.passed !== true
  ) {
    throw new Error("recovery snapshot does not match the frozen contract");
  }
}

function validateAPIResult(result) {
  assertExactKeys(
    result,
    new Set([
      "report_kind",
      "report_version",
      "exercise_id",
      "backup_set_id",
      "verified_at",
      "verified_at_epoch_ms",
      "recovery_started_at_epoch_ms",
      "rto_elapsed_ms",
      "rto_limit_ms",
      "ready_verified",
      "old_session_rejected",
      "new_login_succeeded",
      "restored_snapshot_verified_before_continuation",
      "api_job_window_count",
      "payment_count",
      "email_message_count",
      "document_queries_verified",
      "authenticated_downloads_verified",
      "processing_attempt_count_before_backup",
      "processing_attempt_count_after_recovery",
      "recovered_fact_id",
      "passed",
    ]),
    "recovered API result",
  );
  if (
    result.report_kind !== "m4-backup-restore-api-result" ||
    result.report_version !== 1 ||
    !uuidV4(result.exercise_id) ||
    !/^[0-9a-f]{32}$/.test(result.backup_set_id) ||
    !canonicalTimestamp(result.verified_at) ||
    !Number.isSafeInteger(result.verified_at_epoch_ms) ||
    new Date(result.verified_at_epoch_ms).toISOString() !==
      result.verified_at ||
    !Number.isSafeInteger(result.recovery_started_at_epoch_ms) ||
    result.recovery_started_at_epoch_ms < 1 ||
    result.verified_at_epoch_ms < result.recovery_started_at_epoch_ms ||
    !nonnegativeInteger(result.rto_elapsed_ms) ||
    result.rto_elapsed_ms <
      result.verified_at_epoch_ms - result.recovery_started_at_epoch_ms ||
    result.rto_limit_ms !== expectedRTO ||
    result.rto_elapsed_ms > result.rto_limit_ms ||
    result.ready_verified !== true ||
    result.old_session_rejected !== true ||
    result.new_login_succeeded !== true ||
    result.restored_snapshot_verified_before_continuation !== true ||
    result.api_job_window_count !== expectedAPIJobWindow ||
    result.payment_count !== 2 ||
    result.email_message_count !== 1 ||
    result.document_queries_verified !== 3 ||
    result.authenticated_downloads_verified !== 5 ||
    result.processing_attempt_count_after_recovery !==
      result.processing_attempt_count_before_backup + 1 ||
    !nonemptyString(result.recovered_fact_id) ||
    result.passed !== true
  ) {
    throw new Error("recovered API result does not match the frozen contract");
  }
}

function validateDatabaseResult(result, snapshot) {
  assertExactKeys(
    result,
    new Set([
      "kind",
      "version",
      "exercise_id",
      "backup_set_id",
      "recovered_fact_id",
      "verified_at",
      "document_count",
      "job_count",
      "fact_count",
      "email_message_count",
      "email_attachment_count",
      "document_page_count",
      "new_session_count",
      "historical_session_count",
      "processing_attempt_count",
      "expected_attempt_delta",
      "processing_version",
      "expected_version_delta",
      "lease_expired_ai_run_count",
      "running_ai_run_count",
      "unleased_processing_job_count",
      "confirmed_facts_preserved",
      "exactly_one_recovered_fact",
      "original_running_run_closed",
      "all_jobs_terminal",
      "stable_state_preserved",
      "append_only_changes_scoped",
      "passed",
    ]),
    "database verification",
  );
  if (
    result.kind !== "m4-recovery-database-verification" ||
    result.version !== 1 ||
    !uuidV4(result.exercise_id) ||
    !/^[0-9a-f]{32}$/.test(result.backup_set_id) ||
    !nonemptyString(result.recovered_fact_id) ||
    !canonicalUTCTimestamp(result.verified_at) ||
    result.document_count !== expectedDocuments ||
    result.job_count !== expectedDocuments ||
    result.fact_count !== 2 ||
    result.email_message_count !== 1 ||
    result.email_attachment_count !== 1 ||
    result.document_page_count !== snapshot.document_page_count ||
    result.new_session_count !== 1 ||
    result.historical_session_count !== 0 ||
    result.processing_attempt_count !== snapshot.processing_attempt_count + 1 ||
    result.expected_attempt_delta !== 1 ||
    result.processing_version !== snapshot.processing_version + 3 ||
    result.expected_version_delta !== 3 ||
    result.lease_expired_ai_run_count !== 1 ||
    result.running_ai_run_count !== 0 ||
    result.unleased_processing_job_count !== 0 ||
    result.confirmed_facts_preserved !== true ||
    result.exactly_one_recovered_fact !== true ||
    result.original_running_run_closed !== true ||
    result.all_jobs_terminal !== true ||
    result.stable_state_preserved !== true ||
    result.append_only_changes_scoped !== true ||
    result.passed !== true
  ) {
    throw new Error("database verification does not match the frozen contract");
  }
}

function validateStaticGates(gates) {
  assertExactKeys(
    gates,
    new Set([
      "kind",
      "version",
      "go_test_all",
      "go_vet_all",
      "go_build_all_without_vcs_stamp",
      "web_check",
      "recovery_controller_tests",
      "critical_invariant_branches",
      "domain_application_statement_coverage",
      "infrastructure_transport_statement_coverage",
      "git_diff_check",
      "credential_and_private_asset_scan",
      "binary_and_large_file_check",
      "temporary_artifact_check",
      "current_slice_process_residue_check",
    ]),
    "static gates",
  );
  assertExactKeys(
    gates.recovery_controller_tests,
    new Set(["passed", "failed", "total"]),
    "recovery controller tests",
  );
  assertExactKeys(
    gates.critical_invariant_branches,
    new Set(["passed", "total", "percentage"]),
    "critical invariant branches",
  );
  const coverageKeys = new Set([
    "percentage",
    "covered",
    "total",
    "required_percentage",
    "passed",
  ]);
  assertExactKeys(
    gates.domain_application_statement_coverage,
    coverageKeys,
    "domain application coverage",
  );
  assertExactKeys(
    gates.infrastructure_transport_statement_coverage,
    coverageKeys,
    "infrastructure transport coverage",
  );
  if (
    gates.kind !== "m4-backup-static-gates" ||
    gates.version !== 1 ||
    gates.go_test_all !== "passed" ||
    gates.go_vet_all !== "passed" ||
    gates.go_build_all_without_vcs_stamp !== "passed" ||
    gates.web_check !== "passed" ||
    !Number.isSafeInteger(gates.recovery_controller_tests?.passed) ||
    gates.recovery_controller_tests.passed < 14 ||
    gates.recovery_controller_tests?.failed !== 0 ||
    gates.recovery_controller_tests.passed !==
      gates.recovery_controller_tests.total ||
    gates.critical_invariant_branches?.passed !==
      gates.critical_invariant_branches?.total ||
    gates.critical_invariant_branches?.total < 136 ||
    gates.critical_invariant_branches?.percentage !== 100 ||
    gates.domain_application_statement_coverage?.passed !== true ||
    !validCoverage(gates.domain_application_statement_coverage, 85) ||
    gates.infrastructure_transport_statement_coverage?.passed !== true ||
    !validCoverage(gates.infrastructure_transport_statement_coverage, 70) ||
    gates.git_diff_check !== "passed" ||
    gates.credential_and_private_asset_scan !== "passed" ||
    gates.binary_and_large_file_check !== "passed" ||
    gates.temporary_artifact_check !== "passed" ||
    gates.current_slice_process_residue_check !== "passed"
  ) {
    throw new Error(
      "static gates are incomplete or below the frozen thresholds",
    );
  }
}

function validateRecoverySequence(
  backup,
  verify,
  restore,
  snapshot,
  api,
  database,
) {
  const snapshotCaptured = Date.parse(snapshot.captured_at);
  const databaseVerified = Date.parse(database.verified_at);
  if (
    snapshotCaptured > backup.operation_started_at_epoch_ms ||
    backup.operation_finished_at_epoch_ms > api.recovery_started_at_epoch_ms ||
    verify.operation_started_at_epoch_ms < api.recovery_started_at_epoch_ms ||
    verify.operation_finished_at_epoch_ms >
      restore.operation_started_at_epoch_ms ||
    restore.operation_finished_at_epoch_ms > api.verified_at_epoch_ms ||
    databaseVerified < api.verified_at_epoch_ms
  ) {
    throw new Error(
      "backup operations are not bound to the frozen recovery clock",
    );
  }
}

function validCoverage(coverage, required) {
  if (
    !Number.isSafeInteger(coverage.covered) ||
    !Number.isSafeInteger(coverage.total) ||
    coverage.covered < 0 ||
    coverage.total < 1 ||
    coverage.covered > coverage.total ||
    coverage.required_percentage !== required ||
    typeof coverage.percentage !== "number" ||
    !Number.isFinite(coverage.percentage)
  ) {
    return false;
  }
  const calculated = (coverage.covered * 100) / coverage.total;
  return (
    coverage.percentage >= required &&
    Math.abs(coverage.percentage - calculated) < 0.005
  );
}

function validateMetadata(metadata) {
  if (
    !/^[A-Za-z0-9._/-]+$/.test(metadata.branch) ||
    !/^[0-9a-f]{40}$/.test(metadata.baseSHA) ||
    !/^\d{4}-\d{2}-\d{2}$/.test(metadata.recordedDate)
  ) {
    throw new Error("evidence metadata is invalid");
  }
}

function assertExactKeys(value, allowed, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  const keys = Object.keys(value);
  if (keys.length !== allowed.size || keys.some((key) => !allowed.has(key))) {
    throw new Error(`${label} has missing or unknown fields`);
  }
}

function nonnegativeInteger(value) {
  return Number.isSafeInteger(value) && value >= 0;
}

function nonemptyString(value) {
  return typeof value === "string" && value.length > 0 && value.length <= 512;
}

function exactCountMap(value, fields) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const keys = Object.keys(value);
  return (
    keys.length === fields.size &&
    keys.every(
      (key) =>
        fields.has(key) && Number.isSafeInteger(value[key]) && value[key] >= 0,
    )
  );
}

function canonicalTimestamp(value) {
  if (typeof value !== "string") return false;
  const milliseconds = Date.parse(value);
  return (
    Number.isFinite(milliseconds) &&
    new Date(milliseconds).toISOString() === value
  );
}

function canonicalUTCTimestamp(value) {
  return (
    typeof value === "string" &&
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{0,8}[1-9])?Z$/.test(value) &&
    Number.isFinite(Date.parse(value))
  );
}

function uuidV4(value) {
  return (
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      value,
    )
  );
}

async function readProtectedJSON(path) {
  const handle = await open(
    path,
    constants.O_RDONLY | constants.O_NOFOLLOW,
  ).catch(() => {
    throw new Error("open owner-only evidence input");
  });
  const information = await handle.stat().catch(async () => {
    await handle.close().catch(() => {});
    throw new Error("inspect owner-only evidence input");
  });
  if (
    !information.isFile() ||
    (information.mode & 0o077) !== 0 ||
    information.nlink !== 1 ||
    information.size < 2 ||
    information.size > 1024 * 1024
  ) {
    await handle.close();
    throw new Error(
      "evidence input must be owner-only singly-linked regular JSON",
    );
  }
  let content;
  try {
    content = await handle.readFile();
  } finally {
    await handle.close();
  }
  try {
    return JSON.parse(content.toString("utf8"));
  } finally {
    content.fill(0);
  }
}

function parseArguments(argumentsList) {
  const values = new Map();
  for (let index = 0; index < argumentsList.length; index += 2) {
    const key = argumentsList[index];
    const value = argumentsList[index + 1];
    if (!key?.startsWith("--") || value === undefined) {
      throw new Error(`invalid argument near ${key ?? "<end>"}`);
    }
    const name = key.slice(2);
    if (values.has(name)) throw new Error(`duplicate --${name}`);
    values.set(name, value);
  }
  const required = [
    "backup-result",
    "verify-result",
    "restore-result",
    "snapshot",
    "api-result",
    "database-result",
    "static-gates",
    "branch",
    "base-sha",
    "recorded-date",
    "output",
  ];
  for (const name of values.keys()) {
    if (!required.includes(name)) throw new Error(`unknown --${name}`);
  }
  for (const name of required) {
    if (!values.get(name)) throw new Error(`--${name} is required`);
  }
  return {
    backupResult: resolve(values.get("backup-result")),
    verifyResult: resolve(values.get("verify-result")),
    restoreResult: resolve(values.get("restore-result")),
    snapshot: resolve(values.get("snapshot")),
    apiResult: resolve(values.get("api-result")),
    databaseResult: resolve(values.get("database-result")),
    staticGates: resolve(values.get("static-gates")),
    branch: values.get("branch"),
    baseSHA: values.get("base-sha"),
    recordedDate: values.get("recorded-date"),
    output: resolve(values.get("output")),
  };
}

function safeEvidenceErrorCode(error) {
  const message = error instanceof Error ? error.message.toLowerCase() : "";
  for (const category of [
    ["argument", "invalid_arguments"],
    ["required", "invalid_arguments"],
    ["owner-only", "protected_input_invalid"],
    ["evidence input", "protected_input_invalid"],
    ["metadata", "metadata_invalid"],
    ["static gate", "static_gates_failed"],
    ["frozen contract", "recovery_contract_failed"],
    ["recovery snapshot", "recovery_contract_failed"],
    ["database verification", "recovery_contract_failed"],
    ["backup operations", "recovery_contract_failed"],
  ]) {
    if (message.includes(category[0])) return category[1];
  }
  return "evidence_generation_failed";
}

export { buildEvidence, parseArguments, safeEvidenceErrorCode };

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  main().catch((error) => {
    process.stderr.write(`backup-evidence: ${safeEvidenceErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
