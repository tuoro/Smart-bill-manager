#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const toolDirectory = dirname(fileURLToPath(import.meta.url));
const projectDirectory = resolve(toolDirectory, "..");
const defaultManifest = resolve(
  projectDirectory,
  "tests/evaluation/manifest-v2.json",
);
const defaultTuningManifest = resolve(
  projectDirectory,
  "tests/evaluation/tuning/manifest-v2.json",
);
const releaseManifestSHA256 =
  "08ce3ea739eaa482ba8410ccf71b9f3f1806bbf613d0c960549ae9110566c91d";
const tuningManifestSHA256 = new Map([
  [
    "m1-prompt-dev-v2",
    "76c9eee0672ecad26bdc2940c81c25c8def9918542259374e4d26b8d418acacd",
  ],
  [
    "m1-real-dev-v5",
    "30680f8547da883d93f4850c2ec43720f470d2ea16a70a7f036461152b96bee4",
  ],
]);

const thresholds = {
  schema_valid_rate: 100,
  classification_accuracy: 97,
  amount_exact_rate: 98,
  invoice_number_exact_rate: 95,
  date_normalization_exact_rate: 95,
  name_normalization_exact_rate: 90,
  critical_evidence_coverage: 100,
  missing_conflict_recall: 100,
  manifest_assertion_rate: 100,
};

const preflightThresholds = {
  model_completion_rate: 100,
  claim_contract_rate: 100,
  ...thresholds,
};

const providerOutputRetryPolicy = "schema_validation_single_retry/1";

const criticalFields = {
  payment: ["amount_minor", "currency", "merchant", "transaction_time"],
  invoice: [
    "invoice_number",
    "invoice_date",
    "total_minor",
    "currency",
    "seller_name",
    "buyer_name",
  ],
  unknown: [],
};

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const manifestBytes = await readFile(options.manifest);
  const manifest = JSON.parse(manifestBytes);
  const manifestHash = sha256(manifestBytes);
  if (options.mode === "preflight") {
    validateTuningDatasetShape(manifest);
    if (tuningManifestSHA256.get(manifest.dataset_version) !== manifestHash) {
      throw new Error("tuning manifest hash is not an approved frozen version");
    }
    const run = options.selfTest
      ? perfectPreflightRun(manifest, manifestHash)
      : JSON.parse(await readFile(options.preflightRun, "utf8"));
    const report = scorePreflight(manifest, run, manifestHash);
    if (options.selfTest) {
      assertSemanticEquivalenceSelfTests();
      if (!report.passed) {
        const unexpectedFailures = report.failed_gates.filter(
          (gate) => report.run.metrics[gate.metric]?.denominator !== 0,
        );
        if (unexpectedFailures.length !== 0) {
          throw new Error(
            `tuning preflight scorer self-test failed: ${JSON.stringify(unexpectedFailures)}`,
          );
        }
      }
      assertPreflightNegativeSelfTests(manifest, run, manifestHash);
      const unassessed = report.failed_gates
        .filter((gate) => report.run.metrics[gate.metric]?.denominator === 0)
        .map((gate) => gate.metric);
      process.stdout.write(
        unassessed.length === 0
          ? "model tuning preflight scorer self-test passed\n"
          : `model tuning preflight scorer self-test passed; unassessed metrics: ${unassessed.join(",")}\n`,
      );
      return;
    }
    await writeReport(report, options.output);
    if (!report.passed) process.exitCode = 1;
    return;
  }

  validateDatasetShape(manifest);
  if (manifestHash !== releaseManifestSHA256) {
    throw new Error("release manifest hash is not the approved frozen v2");
  }
  if (options.selfTest) {
    assertSemanticEquivalenceSelfTests();
    const syntheticRuns = ["run-1", "run-2", "run-3"].map((runId) =>
      perfectRun(manifest, runId, manifestHash),
    );
    const report = scoreRelease(manifest, syntheticRuns, manifestHash);
    if (!report.passed) throw new Error("evaluation scorer self-test failed");
    const single = scoreSingleRelease(manifest, syntheticRuns[0], manifestHash);
    if (
      !single.passed ||
      single.release_gate_complete !== false ||
      single.completed_release_runs !== 1 ||
      single.required_release_runs !== 3
    ) {
      throw new Error("single release run scorer self-test failed");
    }
    process.stdout.write("model evaluation scorer self-test passed\n");
    return;
  }
  assertReleaseDatasetEligible(manifest);
  if (options.mode === "release-single") {
    const run = JSON.parse(await readFile(options.singleRun, "utf8"));
    const report = scoreSingleRelease(manifest, run, manifestHash);
    await writeReport(report, options.output);
    if (!report.passed) process.exitCode = 1;
    return;
  }
  const runs = await Promise.all(
    options.runFiles.map(async (path) =>
      JSON.parse(await readFile(path, "utf8")),
    ),
  );
  const report = scoreRelease(manifest, runs, manifestHash);
  await writeReport(report, options.output);
  if (!report.passed) process.exitCode = 1;
}

function scoreSingleRelease(manifest, run, manifestHash) {
  validateReleaseRun(manifest, run, manifestHash);
  const report = scoreRun(manifest, run);
  const failedGates = [];
  for (const [name, required] of Object.entries(thresholds)) {
    if (report.metrics[name].percentage < required) {
      failedGates.push({
        metric: name,
        actual_percentage: report.metrics[name].percentage,
        required_percentage: required,
        failed_sample_ids: report.metrics[name].failed_sample_ids,
      });
    }
  }
  if (report.ai_direct_fact_count !== 0) {
    failedGates.push({
      metric: "ai_direct_fact_count",
      actual: report.ai_direct_fact_count,
      required: 0,
    });
  }
  return {
    report_kind: "m1-model-evaluation-single-run-score",
    dataset_version: manifest.dataset_version,
    dataset_manifest_sha256: manifestHash,
    scoring_protocol: "m1-evaluation-score/1",
    counts_toward_release_runs: true,
    release_gate_complete: false,
    completed_release_runs: 1,
    required_release_runs: 3,
    thresholds,
    passed: failedGates.length === 0,
    failed_gates: failedGates,
    run: report,
  };
}

async function writeReport(report, output) {
  const encoded = `${JSON.stringify(report, null, 2)}\n`;
  if (output) {
    await writeFile(output, encoded, {
      encoding: "utf8",
      flag: "wx",
      mode: 0o600,
    });
  }
  process.stdout.write(encoded);
}

function scoreRelease(manifest, runs, manifestHash) {
  validateRuns(manifest, runs, manifestHash);
  const runReports = runs.map((run) => scoreRun(manifest, run));
  const worst = {};
  for (const name of Object.keys(thresholds)) {
    worst[name] = Math.min(
      ...runReports.map((report) => report.metrics[name].percentage),
    );
  }
  const failedGates = [];
  for (const [name, required] of Object.entries(thresholds)) {
    if (worst[name] < required) {
      failedGates.push({
        metric: name,
        actual_worst_percentage: worst[name],
        required_percentage: required,
      });
    }
  }
  for (const report of runReports) {
    if (report.ai_direct_fact_count !== 0) {
      failedGates.push({
        metric: "ai_direct_fact_count",
        run_id: report.run_id,
        actual: report.ai_direct_fact_count,
        required: 0,
      });
    }
  }
  return {
    report_kind: "m1-model-evaluation-release-score",
    dataset_version: manifest.dataset_version,
    dataset_manifest_sha256: manifestHash,
    scoring_protocol: "m1-evaluation-score/1",
    thresholds,
    worst_run_percentages: worst,
    passed: failedGates.length === 0,
    failed_gates: failedGates,
    runs: runReports,
  };
}

function scorePreflight(manifest, run, manifestHash) {
  validatePreflightRun(manifest, run, manifestHash);
  const report = scoreRun(manifest, run);
  const completion = newCounter();
  const contract = newCounter();
  const resultByID = new Map(
    run.samples.map((sample) => [sample.sample_id, sample]),
  );
  for (const expected of manifest.samples) {
    const observed = resultByID.get(expected.sample_id);
    count(
      completion,
      observed?.outcome === "local_claim_accepted" &&
        observed.schema_valid === true &&
        observed.claim !== null,
      expected.sample_id,
    );
    count(
      contract,
      claimMatchesTuningContract(expected, observed),
      expected.sample_id,
    );
  }
  const metrics = {
    model_completion_rate: summarize(completion),
    claim_contract_rate: summarize(contract),
    ...report.metrics,
  };
  const failedGates = [];
  for (const [name, required] of Object.entries(preflightThresholds)) {
    if (metrics[name].percentage < required) {
      failedGates.push({
        metric: name,
        actual_percentage: metrics[name].percentage,
        required_percentage: required,
        failed_sample_ids: metrics[name].failed_sample_ids,
      });
    }
  }
  if (report.ai_direct_fact_count !== 0) {
    failedGates.push({
      metric: "ai_direct_fact_count",
      actual: report.ai_direct_fact_count,
      required: 0,
    });
  }
  return {
    report_kind: "m1-model-tuning-preflight-score",
    dataset_version: manifest.dataset_version,
    dataset_manifest_sha256: manifestHash,
    scoring_protocol: "m1-tuning-preflight-score/1",
    eligible_for_release_evidence: false,
    thresholds: preflightThresholds,
    passed: failedGates.length === 0,
    failed_gates: failedGates,
    run: {
      ...report,
      metrics,
    },
  };
}

function scoreRun(manifest, run) {
  const resultByID = new Map(
    run.samples.map((sample) => [sample.sample_id, sample]),
  );
  const counters = Object.fromEntries(
    Object.keys(thresholds).map((name) => [
      name,
      { numerator: 0, denominator: 0, failures: new Set() },
    ]),
  );
  for (const expected of manifest.samples) {
    const observed = resultByID.get(expected.sample_id);
    if (!observed)
      throw new Error(`${run.run_id}: missing sample ${expected.sample_id}`);
    const claim = observed.claim;
    const fieldMap = new Map(
      (claim?.fields ?? []).map((field) => [field.path, field]),
    );

    if (observed.outcome === "local_claim_accepted") {
      count(
        counters.schema_valid_rate,
        observed.schema_valid === true,
        expected.sample_id,
      );
    }
    if (expected.model_stage_eligible) {
      count(
        counters.classification_accuracy,
        claim?.document_type === expected.document_type,
        expected.sample_id,
      );
    }
    for (const path of ["amount_minor", "total_minor"]) {
      if (Object.hasOwn(expected.expected_fields, path)) {
        const field = fieldMap.get(path);
        count(
          counters.amount_exact_rate,
          exactInteger(field?.value, expected.expected_fields[path]),
          expected.sample_id,
        );
      }
    }
    if (Object.hasOwn(expected.expected_fields, "invoice_number")) {
      const field = fieldMap.get("invoice_number");
      count(
        counters.invoice_number_exact_rate,
        field?.value === expected.expected_fields.invoice_number,
        expected.sample_id,
      );
    }
    for (const path of ["invoice_date", "transaction_time"]) {
      if (Object.hasOwn(expected.expected_fields, path)) {
        const field = fieldMap.get(path);
        const correct =
          path === "transaction_time"
            ? sameInstant(field?.value, expected.expected_fields[path])
            : field?.value === expected.expected_fields[path];
        count(
          counters.date_normalization_exact_rate,
          correct,
          expected.sample_id,
        );
      }
    }
    for (const path of ["merchant", "seller_name", "buyer_name"]) {
      if (Object.hasOwn(expected.expected_fields, path)) {
        const field = fieldMap.get(path);
        count(
          counters.name_normalization_exact_rate,
          typeof field?.value === "string" &&
            normalizeExact(field.value) ===
              normalizeExact(expected.expected_fields[path]),
          expected.sample_id,
        );
      }
    }
    if (claim) {
      for (const path of criticalFields[claim.document_type] ?? []) {
        const field = fieldMap.get(path);
        if (field?.presence !== "present") continue;
        const frozenEvidence = expected.expected_evidence[path];
        count(
          counters.critical_evidence_coverage,
          evidenceMatches(field.evidence, frozenEvidence, {
            path,
            valueType: field.value_type,
            expectedValue: expected.expected_fields[path],
            expectedFields: expected.expected_fields,
          }),
          expected.sample_id,
        );
      }
    }
    for (const event of expected.expected_events) {
      if (!event.startsWith("missing:") && !event.startsWith("conflict:"))
        continue;
      const recalled =
        observed.job_status === "blocked" ||
        observed.job_status === "failed" ||
        claim?.claim_status === "blocked" ||
        claim?.validations?.some((item) =>
          ["blocked", "error"].includes(item.status),
        );
      count(counters.missing_conflict_recall, recalled, expected.sample_id);
    }

    const assertions = [];
    if (expected.expected_failure_category) {
      assertions.push(
        observed.outcome === "rejected_before_model" &&
          observed.error_code === expected.expected_failure_category,
      );
    } else {
      assertions.push(observed.accepted_upload === true);
      assertions.push(observed.job_status === expected.expected_review_state);
    }
    for (const path of expected.expected_missing_fields) {
      assertions.push(fieldMap.get(path)?.presence !== "present");
    }
    for (const assertion of assertions) {
      count(counters.manifest_assertion_rate, assertion, expected.sample_id);
    }
  }
  return {
    run_id: run.run_id,
    frozen_configuration: run.frozen_configuration,
    ai_direct_fact_count: run.ai_direct_fact_count,
    metrics: Object.fromEntries(
      Object.entries(counters).map(([name, counter]) => [
        name,
        summarize(counter),
      ]),
    ),
  };
}

function validateRuns(manifest, runs, manifestHash) {
  if (runs.length !== 3)
    throw new Error("exactly three complete evaluation runs are required");
  const runIDs = new Set(runs.map((run) => run.run_id));
  if (
    runIDs.size !== 3 ||
    !["run-1", "run-2", "run-3"].every((id) => runIDs.has(id))
  ) {
    throw new Error("evaluation runs must be run-1, run-2, and run-3");
  }
  const frozen = JSON.stringify(runs[0].frozen_configuration);
  for (const run of runs) {
    validateReleaseRun(manifest, run, manifestHash);
    if (JSON.stringify(run.frozen_configuration) !== frozen) {
      throw new Error(
        `${run.run_id}: frozen provider configuration changed between runs`,
      );
    }
  }
}

function validateReleaseRun(manifest, run, manifestHash) {
  if (
    run.result_kind !== "m1-model-evaluation-run" ||
    !/^run-[1-3]$/.test(run.run_id ?? "") ||
    run.dataset_version !== manifest.dataset_version ||
    run.dataset_manifest_sha256 !== manifestHash ||
    run.eligible_for_release_evidence !== true
  ) {
    throw new Error(`${run.run_id ?? "<unknown>"}: dataset identity mismatch`);
  }
  const promptVersion = run.frozen_configuration?.prompt_version;
  const outputMode = run.frozen_configuration?.output_mode;
  const deterministicConfiguration =
    promptVersion === "bill-visible-text-cn/1" &&
    run.frozen_configuration?.temperature === 0 &&
    new Set(["json_schema", "json_object"]).has(outputMode);
  if (
    !deterministicConfiguration ||
    run.frozen_configuration?.extraction_schema_version !==
      "bill-visible-text/1" ||
    run.frozen_configuration?.provider_schema_version !==
      "bill-visible-text-provider/1" ||
    run.frozen_configuration?.claim_schema_version !== "document-claim/2" ||
    run.frozen_configuration?.claim_mapper_version !== "claim-mapper/3" ||
    run.frozen_configuration?.provider_output_retry_policy !==
      providerOutputRetryPolicy ||
    !/^[a-f0-9]{64}$/.test(
      run.frozen_configuration?.provider_schema_sha256 ?? "",
    )
  ) {
    throw new Error(
      `${run.run_id}: frozen schema identity is missing or stale`,
    );
  }
  if (
    !Array.isArray(run.samples) ||
    run.samples.length !== manifest.samples.length
  ) {
    throw new Error(`${run.run_id}: incomplete sample set`);
  }
  const expectedIDs = new Set(
    manifest.samples.map((sample) => sample.sample_id),
  );
  const observedIDs = new Set();
  for (const sample of run.samples) {
    if (
      !expectedIDs.has(sample.sample_id) ||
      observedIDs.has(sample.sample_id)
    ) {
      throw new Error(
        `${run.run_id}: unknown or duplicate sample ${sample.sample_id}`,
      );
    }
    observedIDs.add(sample.sample_id);
  }
}

function validatePreflightRun(manifest, run, manifestHash) {
  if (
    run.result_kind !== "m1-model-tuning-preflight-run" ||
    run.run_id !== "preflight" ||
    run.dataset_version !== manifest.dataset_version ||
    run.dataset_manifest_sha256 !== manifestHash ||
    run.eligible_for_release_evidence !== false
  ) {
    throw new Error("tuning preflight run identity is invalid");
  }
  const currentConfiguration =
    tuningManifestSHA256.has(manifest.dataset_version) &&
    run.frozen_configuration?.prompt_version === "bill-visible-text-cn/1" &&
    run.frozen_configuration?.extraction_schema_version ===
      "bill-visible-text/1" &&
    run.frozen_configuration?.provider_schema_version ===
      "bill-visible-text-provider/1" &&
    run.frozen_configuration?.claim_schema_version === "document-claim/2" &&
    run.frozen_configuration?.claim_mapper_version === "claim-mapper/3" &&
    run.frozen_configuration?.temperature === 0 &&
    new Set(["json_schema", "json_object"]).has(
      run.frozen_configuration?.output_mode,
    ) &&
    run.frozen_configuration?.provider_output_retry_policy ===
      providerOutputRetryPolicy;
  if (
    !currentConfiguration ||
    !/^[a-f0-9]{64}$/.test(
      run.frozen_configuration?.provider_schema_sha256 ?? "",
    )
  ) {
    throw new Error("tuning preflight schema identity is missing or stale");
  }
  if (
    !Array.isArray(run.samples) ||
    run.samples.length !== manifest.samples.length
  ) {
    throw new Error("tuning preflight sample set is incomplete");
  }
  const expectedIDs = new Set(
    manifest.samples.map((sample) => sample.sample_id),
  );
  const observedIDs = new Set();
  for (const sample of run.samples) {
    if (
      !expectedIDs.has(sample.sample_id) ||
      observedIDs.has(sample.sample_id)
    ) {
      throw new Error(
        `tuning preflight has unknown or duplicate sample ${sample.sample_id}`,
      );
    }
    observedIDs.add(sample.sample_id);
  }
}

function validateDatasetShape(manifest) {
  if (
    manifest.dataset_version !== "m1-synthetic-v2" ||
    manifest.synthetic_only !== true ||
    manifest.intended_use !== "m1_release_model_evaluation" ||
    manifest.supersedes_dataset_version !== "m1-synthetic-v1" ||
    manifest.samples?.length !== 100
  ) {
    throw new Error(
      "manifest is not the frozen 100-sample M1 synthetic dataset",
    );
  }
  const typeCounts = new Map();
  const tagCounts = new Map();
  const ids = new Set();
  for (const sample of manifest.samples) {
    if (ids.has(sample.sample_id))
      throw new Error(`duplicate sample ID: ${sample.sample_id}`);
    ids.add(sample.sample_id);
    typeCounts.set(
      sample.document_type,
      (typeCounts.get(sample.document_type) ?? 0) + 1,
    );
    for (const tag of sample.scenario_tags)
      tagCounts.set(tag, (tagCounts.get(tag) ?? 0) + 1);
  }
  if (
    (typeCounts.get("payment") ?? 0) < 40 ||
    (typeCounts.get("invoice") ?? 0) < 40
  ) {
    throw new Error(
      "dataset does not contain at least 40 payment and 40 invoice samples",
    );
  }
  for (const tag of [
    "payment_screenshot",
    "single_item_invoice",
    "multi_item_invoice",
    "low_quality_conflict",
    "invalid_unsupported",
  ]) {
    if ((tagCounts.get(tag) ?? 0) < 15)
      throw new Error(`dataset scenario ${tag} has fewer than 15 samples`);
  }
}

function assertReleaseDatasetEligible(manifest) {
  if (manifest.dataset_version !== "m1-synthetic-v2") {
    throw new Error(
      "only the approved corrected m1-synthetic-v2 dataset is eligible for release scoring",
    );
  }
}

function validateTuningDatasetShape(manifest) {
  const commonIdentityInvalid =
    manifest.intended_use !== "prompt_provider_contract_tuning_only" ||
    !Array.isArray(manifest.source_dataset_versions) ||
    manifest.source_dataset_versions.length !== 0 ||
    manifest.excluded_from_release_evidence !== true ||
    manifest.samples?.length !== 16;
  const syntheticIdentityValid =
    manifest.dataset_version === "m1-prompt-dev-v2" &&
    manifest.synthetic_only === true &&
    manifest.supersedes_dataset_version === "m1-prompt-dev-v1";
  const realIdentityValid =
    manifest.dataset_version === "m1-real-dev-v5" &&
    manifest.synthetic_only === false &&
    manifest.real_world === true &&
    manifest.supersedes_dataset_version === "m1-real-dev-v4" &&
    manifest.prompt_contract === "bill-visible-text-cn/1" &&
    manifest.extraction_schema_contract === "bill-visible-text/1" &&
    manifest.provider_schema_contract === "bill-visible-text-provider/1" &&
    manifest.authoritative_schema_contract === "document-claim/2" &&
    manifest.claim_mapper_contract === "claim-mapper/3" &&
    manifest.input_processing_contract === "document-normalize/2";
  if (
    commonIdentityInvalid ||
    (!syntheticIdentityValid && !realIdentityValid)
  ) {
    throw new Error(
      "manifest is not the current isolated M1 visible-text tuning dataset",
    );
  }
  const counts = new Map();
  const tags = new Map();
  const ids = new Set();
  for (const sample of manifest.samples) {
    if (ids.has(sample.sample_id))
      throw new Error(`duplicate tuning sample ID: ${sample.sample_id}`);
    ids.add(sample.sample_id);
    counts.set(
      sample.document_type,
      (counts.get(sample.document_type) ?? 0) + 1,
    );
    for (const tag of sample.scenario_tags ?? []) {
      tags.set(tag, (tags.get(tag) ?? 0) + 1);
    }
  }
  const distributionValid = realIdentityValid
    ? counts.get("payment") === 10 &&
      counts.get("invoice") === 6 &&
      (counts.get("unknown") ?? 0) === 0
    : counts.get("payment") === 6 &&
      counts.get("invoice") === 8 &&
      counts.get("unknown") === 2;
  if (!distributionValid)
    throw new Error("tuning dataset type distribution is invalid");
  if (
    syntheticIdentityValid &&
    ((tags.get("compact_bitmap") ?? 0) !== 16 ||
      (tags.get("literal_evidence") ?? 0) !== 14 ||
      (tags.get("low_contrast") ?? 0) !== 5 ||
      (tags.get("root_key_guard") ?? 0) < 3)
  ) {
    throw new Error("tuning v2 scenario distribution is invalid");
  }
  if (
    realIdentityValid &&
    ((tags.get("bill_visible_text_v1") ?? 0) !== 16 ||
      (manifest.composition?.wechat_pay_detail ?? 0) !== 5 ||
      (manifest.composition?.alipay_detail ?? 0) !== 5 ||
      (manifest.composition?.invoice ?? 0) !== 6)
  ) {
    throw new Error("real tuning v5 scenario distribution is invalid");
  }
}

function assertPreflightNegativeSelfTests(manifest, perfect, manifestHash) {
  const missingField = structuredClone(perfect);
  const firstWithBusinessFields = missingField.samples.find(
    (sample) => sample.claim?.fields?.length > 1,
  );
  firstWithBusinessFields.claim.fields.splice(1, 1);
  const missingFieldReport = scorePreflight(
    manifest,
    missingField,
    manifestHash,
  );
  if (
    missingFieldReport.passed ||
    !missingFieldReport.failed_gates.some(
      (gate) => gate.metric === "claim_contract_rate",
    )
  ) {
    throw new Error("preflight scorer accepted an incomplete Claim contract");
  }

  const incompleteRun = structuredClone(perfect);
  incompleteRun.samples[0].outcome = "job_terminal_without_claim";
  incompleteRun.samples[0].schema_valid = false;
  incompleteRun.samples[0].claim = null;
  const incompleteReport = scorePreflight(
    manifest,
    incompleteRun,
    manifestHash,
  );
  if (
    incompleteReport.passed ||
    !incompleteReport.failed_gates.some(
      (gate) => gate.metric === "model_completion_rate",
    )
  ) {
    throw new Error("preflight scorer accepted an incomplete model run");
  }

  const sampleWithMultipleEvidence = manifest.samples.find((sample) =>
    Object.values(sample.expected_evidence).some(
      (evidence) => Array.isArray(evidence) && evidence.length > 1,
    ),
  );
  if (sampleWithMultipleEvidence) {
    const incompleteEvidenceRun = structuredClone(perfect);
    const expectedEntry = Object.entries(
      sampleWithMultipleEvidence.expected_evidence,
    ).find(([, evidence]) => Array.isArray(evidence) && evidence.length > 1);
    const [expectedPath] = expectedEntry;
    const observed = incompleteEvidenceRun.samples.find(
      (sample) => sample.sample_id === sampleWithMultipleEvidence.sample_id,
    );
    const field = observed.claim.fields.find(
      (candidate) => candidate.path === stableSelfTestPath(expectedPath),
    );
    field.evidence.pop();
    const incompleteEvidenceReport = scorePreflight(
      manifest,
      incompleteEvidenceRun,
      manifestHash,
    );
    if (
      incompleteEvidenceReport.passed ||
      !incompleteEvidenceReport.failed_gates.some(
        (gate) => gate.metric === "claim_contract_rate",
      )
    ) {
      throw new Error(
        "preflight scorer accepted an incomplete multi-fragment evidence set",
      );
    }
  }
}

function perfectRun(manifest, runId, manifestHash) {
  return {
    result_kind: "m1-model-evaluation-run",
    run_id: runId,
    dataset_version: manifest.dataset_version,
    dataset_manifest_sha256: manifestHash,
    eligible_for_release_evidence: true,
    frozen_configuration: {
      safe_fingerprint: "scorer-self-test-only",
      output_mode: "json_schema",
      prompt_version: "bill-visible-text-cn/1",
      extraction_schema_version: "bill-visible-text/1",
      provider_schema_version: "bill-visible-text-provider/1",
      provider_schema_sha256:
        "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
      claim_schema_version: "document-claim/2",
      claim_mapper_version: "claim-mapper/3",
      provider_output_retry_policy: providerOutputRetryPolicy,
      temperature: 0,
    },
    ai_direct_fact_count: 0,
    samples: manifest.samples.map((sample) => {
      if (sample.expected_failure_category) {
        return {
          sample_id: sample.sample_id,
          outcome: "rejected_before_model",
          accepted_upload: false,
          error_code: sample.expected_failure_category,
        };
      }
      const missing = new Set(sample.expected_missing_fields);
      const fields = Object.entries(sample.expected_fields).map(
        ([path, value]) => ({
          path,
          presence: "present",
          value,
          evidence: cloneExpectedEvidence(sample.expected_evidence[path]),
        }),
      );
      for (const path of missing)
        fields.push({ path, presence: "absent", evidence: [] });
      return {
        sample_id: sample.sample_id,
        outcome: "local_claim_accepted",
        accepted_upload: true,
        job_status: sample.expected_review_state,
        schema_valid: true,
        claim: {
          document_type: sample.document_type,
          claim_status:
            sample.expected_review_state === "blocked"
              ? "blocked"
              : "ready_for_review",
          fields,
          validations: sample.expected_events.map((event) => ({
            rule_code: event,
            status: "blocked",
          })),
        },
      };
    }),
  };
}

function perfectPreflightRun(manifest, manifestHash) {
  return {
    result_kind: "m1-model-tuning-preflight-run",
    run_id: "preflight",
    dataset_version: manifest.dataset_version,
    dataset_manifest_sha256: manifestHash,
    eligible_for_release_evidence: false,
    frozen_configuration: {
      safe_fingerprint: "preflight-scorer-self-test-only",
      output_mode: "json_schema",
      prompt_version: "bill-visible-text-cn/1",
      extraction_schema_version: "bill-visible-text/1",
      provider_schema_version: "bill-visible-text-provider/1",
      claim_schema_version: "document-claim/2",
      claim_mapper_version: "claim-mapper/3",
      provider_schema_sha256:
        "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
      provider_output_retry_policy: providerOutputRetryPolicy,
      temperature: 0,
    },
    ai_direct_fact_count: 0,
    samples: manifest.samples.map((sample) => {
      const fields = [
        {
          path: "document_type",
          value_type: "document_type",
          presence: "present",
          value: sample.document_type,
          evidence: [],
        },
      ];
      for (const [expectedPath, valueType] of Object.entries(
        sample.expected_value_types,
      )) {
        const path = stableSelfTestPath(expectedPath);
        if (Object.hasOwn(sample.expected_fields, expectedPath)) {
          fields.push({
            path,
            value_type: valueType,
            presence: "present",
            value: sample.expected_fields[expectedPath],
            evidence: cloneExpectedEvidence(
              sample.expected_evidence[expectedPath],
            ),
          });
        } else {
          fields.push({
            path,
            value_type: valueType,
            presence: "absent",
            evidence: [],
          });
        }
      }
      return {
        sample_id: sample.sample_id,
        outcome: "local_claim_accepted",
        accepted_upload: true,
        job_status: sample.expected_review_state,
        schema_valid: true,
        claim: {
          document_type: sample.document_type,
          claim_status:
            sample.expected_review_state === "blocked"
              ? "blocked"
              : "ready_for_review",
          fields,
          validations: sample.expected_events.map((event) => ({
            rule_code: expectedEventRule(event),
            status: "blocked",
          })),
        },
      };
    }),
  };
}

function stableSelfTestPath(path) {
  return path.replace(/^items\[(\d+)\]/, (_, rawIndex) => {
    const suffix = (Number(rawIndex) + 1).toString(16).padStart(12, "0");
    return `items[00000000-0000-0000-0000-${suffix}]`;
  });
}

function claimMatchesTuningContract(expected, observed) {
  if (
    observed?.outcome !== "local_claim_accepted" ||
    observed.schema_valid !== true ||
    observed.job_status !== expected.expected_review_state ||
    observed.claim?.document_type !== expected.document_type
  ) {
    return false;
  }
  const documentFields = observed.claim.fields.filter(
    (field) => field.path === "document_type",
  );
  if (
    documentFields.length !== 1 ||
    documentFields[0].value_type !== "document_type" ||
    documentFields[0].presence !== "present" ||
    documentFields[0].value !== expected.document_type
  ) {
    return false;
  }
  const fields = normalizeTuningFieldPaths(observed.claim.fields);
  if (fields === null) return false;
  const expectedPaths = Object.keys(expected.expected_value_types).sort();
  if (
    fields.size !== expectedPaths.length ||
    !expectedPaths.every((path) => fields.has(path))
  ) {
    return false;
  }
  for (const path of expectedPaths) {
    const field = fields.get(path);
    const valueType = expected.expected_value_types[path];
    if (field.value_type !== valueType) return false;
    if (Object.hasOwn(expected.expected_fields, path)) {
      if (
        field.presence !== "present" ||
        !sameExpectedValue(
          field.value,
          expected.expected_fields[path],
          valueType,
        ) ||
        (path !== "source_timezone" &&
          !evidenceMatches(field.evidence, expected.expected_evidence[path], {
            path,
            valueType,
            expectedValue: expected.expected_fields[path],
            expectedFields: expected.expected_fields,
          }))
      ) {
        return false;
      }
    } else if (
      field.presence !== "absent" ||
      (field.value !== undefined && field.value !== null) ||
      (Array.isArray(field.evidence) && field.evidence.length !== 0)
    ) {
      return false;
    }
  }
  const validationRules = new Set(
    observed.claim.validations.map((validation) => validation.rule_code),
  );
  return expected.expected_events.every((event) =>
    validationRules.has(expectedEventRule(event)),
  );
}

function normalizeTuningFieldPaths(rawFields) {
  const result = new Map();
  const itemGroups = new Map();
  for (const field of rawFields) {
    if (field.path === "document_type") continue;
    const match = /^items\[([a-f0-9-]{36})\]\.([a-z][a-z0-9_]*)$/.exec(
      field.path,
    );
    if (match) {
      const group = itemGroups.get(match[1]) ?? [];
      group.push({ field, suffix: match[2] });
      itemGroups.set(match[1], group);
      continue;
    }
    if (result.has(field.path)) return null;
    result.set(field.path, field);
  }
  const sortOrders = new Set();
  for (const group of itemGroups.values()) {
    const sortFields = group.filter(
      ({ field, suffix }) =>
        suffix === "sort_order" &&
        field.presence === "present" &&
        Number.isSafeInteger(field.value),
    );
    if (sortFields.length !== 1 || sortOrders.has(sortFields[0].field.value)) {
      return null;
    }
    const sortOrder = sortFields[0].field.value;
    sortOrders.add(sortOrder);
    for (const { field, suffix } of group) {
      const path = `items[${sortOrder}].${suffix}`;
      if (result.has(path)) return null;
      result.set(path, field);
    }
  }
  for (let index = 0; index < sortOrders.size; index += 1) {
    if (!sortOrders.has(index)) return null;
  }
  return result;
}

function sameExpectedValue(actual, expected, valueType) {
  if (valueType === "instant") return sameInstant(actual, expected);
  if (valueType === "decimal")
    return canonicalDecimal(actual) !== null &&
      canonicalDecimal(actual) === canonicalDecimal(expected);
  if (valueType === "string")
    return normalizeBusinessText(actual) === normalizeBusinessText(expected);
  return actual === expected;
}

function assertSemanticEquivalenceSelfTests() {
  if (
    !sameExpectedValue("1.0", "1", "decimal") ||
    !sameExpectedValue("（详见销货清单）", "详见销货清单", "string") ||
    !evidenceMatches(
      [{ page: 1, quote: "¥" }],
      { page: 1, quote: "¥68.00" },
      {
        path: "currency",
        valueType: "string",
        expectedValue: "CNY",
        expectedFields: { currency: "CNY" },
      },
    ) ||
    !evidenceMatches(
      [{ page: 1, quote: "¥68.00" }],
      { page: 1, quote: "价税合计（小写）：¥68.00" },
      {
        path: "total_minor",
        valueType: "money_minor",
        expectedValue: 6800,
        expectedFields: { currency: "CNY" },
      },
    ) ||
    evidenceMatches(
      [{ page: 1, quote: "1" }],
      { page: 1, quote: "100.00" },
      {
        path: "total_minor",
        valueType: "money_minor",
        expectedValue: 10000,
        expectedFields: { currency: "CNY" },
      },
    )
  ) {
    throw new Error("semantic value/evidence equivalence self-test failed");
  }
}

function expectedEventRule(event) {
  if (event.startsWith("missing:")) return "missing_required_field";
  if (event.startsWith("conflict:")) return "conflicting_values";
  return event;
}

function newCounter() {
  return { numerator: 0, denominator: 0, failures: new Set() };
}

function count(counter, passed, sampleID) {
  counter.denominator += 1;
  if (passed) counter.numerator += 1;
  else counter.failures.add(sampleID);
}

function summarize(counter) {
  return {
    numerator: counter.numerator,
    denominator: counter.denominator,
    percentage:
      counter.denominator === 0
        ? 0
        : round((counter.numerator * 100) / counter.denominator),
    failed_sample_ids: [...counter.failures].sort(),
  };
}

function exactInteger(actual, expected) {
  return (
    Number.isSafeInteger(actual) &&
    Number.isSafeInteger(expected) &&
    actual === expected
  );
}

function sameInstant(actual, expected) {
  if (typeof actual !== "string" || typeof expected !== "string") return false;
  const actualTime = Date.parse(actual);
  const expectedTime = Date.parse(expected);
  return Number.isFinite(actualTime) && actualTime === expectedTime;
}

function normalizeExact(value) {
  return String(value)
    .normalize("NFKC")
    .trim()
    .replace(/\s+/gu, " ")
    .replace(/[A-Z]/g, (character) => character.toLowerCase());
}

function normalizeBusinessText(value) {
  let normalized = normalizeExact(value);
  const pairs = [
    ["(", ")"],
    ["（", "）"],
  ];
  for (const [open, close] of pairs) {
    if (normalized.startsWith(open) && normalized.endsWith(close)) {
      normalized = normalized.slice(open.length, -close.length).trim();
      break;
    }
  }
  return normalized;
}

function canonicalDecimal(value) {
  if (typeof value !== "string" && typeof value !== "number") return null;
  const text = String(value).normalize("NFKC").trim();
  if (!/^[0-9]+(?:\.[0-9]+)?$/.test(text)) return null;
  const [rawWhole, rawFraction = ""] = text.split(".");
  const whole = rawWhole.replace(/^0+(?=[0-9])/, "");
  const fraction = rawFraction.replace(/0+$/, "");
  return fraction === "" ? whole : `${whole}.${fraction}`;
}

function evidenceMatches(observed, expected, context = {}) {
  if (!expected || !Array.isArray(observed)) return false;
  const expectations = Array.isArray(expected) ? expected : [expected];
  return (
    expectations.length > 0 &&
    expectations.every((expectation) =>
      observed.some((entry) => {
        if (entry.page !== expectation.page) return false;
        if (expectation.quote && typeof entry.quote === "string") {
          const observedQuote = normalizeExact(entry.quote);
          const expectedQuote = normalizeExact(expectation.quote);
          return (
            observedQuote.includes(expectedQuote) ||
            evidenceSupportsExpectedValue(entry.quote, context)
          );
        }
        return expectation.region !== undefined && entry.region !== null;
      }),
    )
  );
}

function evidenceSupportsExpectedValue(quote, context) {
  const { path, valueType, expectedValue, expectedFields = {} } = context;
  if (typeof quote !== "string" || expectedValue === undefined) return false;
  if (path === "currency") {
    return currencyEvidenceValues(quote).has(expectedValue);
  }
  if (valueType === "string") {
    return normalizeBusinessText(quote).includes(
      normalizeBusinessText(expectedValue),
    );
  }
  if (valueType === "decimal") {
    const expectedDecimal = canonicalDecimal(expectedValue);
    return numericTokens(quote).some(
      (token) => canonicalDecimal(token) === expectedDecimal,
    );
  }
  if (valueType === "money_minor") {
    return moneyEvidenceMatches(
      quote,
      expectedValue,
      expectedFields.currency,
    );
  }
  if (valueType === "date") {
    return visibleDate(quote) === expectedValue;
  }
  if (valueType === "instant") {
    return visibleInstantMatches(
      quote,
      expectedValue,
      expectedFields.source_timezone,
    );
  }
  if (valueType === "integer") {
    return numericTokens(quote).some(
      (token) => Number.isSafeInteger(expectedValue) && token === String(expectedValue),
    );
  }
  return false;
}

function currencyEvidenceValues(quote) {
  const value = String(quote).normalize("NFKC").toUpperCase();
  const result = new Set();
  if (/CNY|RMB|人民币|人民币元|(?:^|\s)元(?:$|\s)|¥/u.test(value))
    result.add("CNY");
  if (/USD|美元|\$/u.test(value)) result.add("USD");
  if (/EUR|欧元|€/u.test(value)) result.add("EUR");
  if (/JPY|日元|円/u.test(value)) result.add("JPY");
  return result;
}

function numericTokens(value) {
  return String(value).normalize("NFKC").match(/[0-9][0-9.,]*/g) ?? [];
}

function moneyEvidenceMatches(quote, expectedMinor, currency) {
  if (!Number.isSafeInteger(expectedMinor)) return false;
  const exponent = currency === "JPY" ? 0 : 2;
  return numericTokens(quote).some((token) => {
    const decimal = normalizeLocalizedNumber(token, exponent);
    if (decimal === null) return false;
    const [whole, fraction = ""] = decimal.split(".");
    const padded = fraction.padEnd(exponent, "0");
    try {
      const multiplier = 10n ** BigInt(exponent);
      const minor = BigInt(whole) * multiplier + BigInt(padded || "0");
      return minor === BigInt(expectedMinor);
    } catch {
      return false;
    }
  });
}

function normalizeLocalizedNumber(value, exponent) {
  const dotCount = (value.match(/\./g) ?? []).length;
  const commaCount = (value.match(/,/g) ?? []).length;
  if (dotCount > 0 && commaCount > 0) {
    const decimal = value.lastIndexOf(",") > value.lastIndexOf(".") ? "," : ".";
    const thousands = decimal === "," ? "." : ",";
    if (exponent === 0 || value.split(decimal).length !== 2) return null;
    const [whole, fraction] = value.split(decimal);
    const normalizedWhole = normalizeGroupedNumber(whole, thousands);
    if (
      normalizedWhole === null ||
      !/^[0-9]+$/.test(fraction) ||
      fraction.length < 1 ||
      fraction.length > exponent
    ) {
      return null;
    }
    return `${normalizedWhole}.${fraction}`;
  }
  const separator = dotCount > 0 ? "." : commaCount > 0 ? "," : "";
  const count = separator === "." ? dotCount : commaCount;
  if (separator === "") return /^[0-9]+$/.test(value) ? canonicalWhole(value) : null;
  if (count > 1) return normalizeGroupedNumber(value, separator);
  const parts = value.split(separator);
  if (parts.length !== 2 || !parts.every((part) => /^[0-9]+$/.test(part)))
    return null;
  if (exponent > 0 && parts[1].length <= exponent)
    return `${canonicalWhole(parts[0])}.${parts[1]}`;
  if (parts[1].length === 3 && parts[0].length >= 1 && parts[0].length <= 3)
    return canonicalWhole(parts.join(""));
  return null;
}

function normalizeGroupedNumber(value, separator) {
  const parts = value.split(separator);
  if (
    parts.length < 2 ||
    parts[0].length < 1 ||
    parts[0].length > 3 ||
    !parts.every((part, index) =>
      index === 0 ? /^[0-9]+$/.test(part) : /^[0-9]{3}$/.test(part),
    )
  ) {
    return null;
  }
  return canonicalWhole(parts.join(""));
}

function canonicalWhole(value) {
  return value.replace(/^0+(?=[0-9])/, "");
}

function visibleDate(value) {
  const match = String(value)
    .normalize("NFKC")
    .match(/([0-9]{4})[-/.年]([0-9]{1,2})[-/.月]([0-9]{1,2})(?:日)?/u);
  if (!match) return null;
  return `${match[1]}-${match[2].padStart(2, "0")}-${match[3].padStart(2, "0")}`;
}

function visibleInstantMatches(quote, expected, timezone) {
  if (sameInstant(String(quote).trim(), expected)) return true;
  const match = String(quote)
    .normalize("NFKC")
    .match(
      /([0-9]{4})[-/.年]([0-9]{1,2})[-/.月]([0-9]{1,2})(?:日)?[ T]([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?/u,
    );
  if (!match || typeof timezone !== "string") return false;
  const expectedDate = new Date(expected);
  if (!Number.isFinite(expectedDate.getTime())) return false;
  try {
    const parts = Object.fromEntries(
      new Intl.DateTimeFormat("en-US", {
        timeZone: timezone,
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hourCycle: "h23",
      })
        .formatToParts(expectedDate)
        .filter((part) => part.type !== "literal")
        .map((part) => [part.type, part.value]),
    );
    return (
      match[1] === parts.year &&
      match[2].padStart(2, "0") === parts.month &&
      match[3].padStart(2, "0") === parts.day &&
      match[4].padStart(2, "0") === parts.hour &&
      match[5] === parts.minute &&
      (match[6] ?? "00") === parts.second
    );
  } catch {
    return false;
  }
}

function cloneExpectedEvidence(expected) {
  if (!expected) return [];
  return (Array.isArray(expected) ? expected : [expected]).map((entry) => ({
    ...entry,
  }));
}

function parseArguments(argumentsList) {
  if (argumentsList.length === 1 && argumentsList[0] === "--self-test") {
    return {
      mode: "release",
      selfTest: true,
      manifest: defaultManifest,
      runFiles: [],
      singleRun: "",
      preflightRun: "",
      output: "",
    };
  }
  if (argumentsList[0] === "--preflight-self-test") {
    if (
      argumentsList.length !== 1 &&
      !(
        argumentsList.length === 3 &&
        argumentsList[1] === "--manifest" &&
        argumentsList[2]
      )
    ) {
      throw new Error(
        "--preflight-self-test accepts only an optional --manifest path",
      );
    }
    return {
      mode: "preflight",
      selfTest: true,
      manifest: resolve(argumentsList[2] ?? defaultTuningManifest),
      runFiles: [],
      singleRun: "",
      preflightRun: "",
      output: "",
    };
  }
  const values = new Map();
  for (let index = 0; index < argumentsList.length; index += 2) {
    const key = argumentsList[index];
    const value = argumentsList[index + 1];
    if (!key?.startsWith("--") || value === undefined)
      throw new Error(`invalid argument near ${key ?? "<end>"}`);
    values.set(key.slice(2), value);
  }
  if (values.has("preflight-run")) {
    return {
      mode: "preflight",
      selfTest: false,
      manifest: resolve(values.get("manifest") ?? defaultTuningManifest),
      runFiles: [],
      singleRun: "",
      preflightRun: resolve(values.get("preflight-run")),
      output: values.get("output") ? resolve(values.get("output")) : "",
    };
  }
  if (values.has("single-run")) {
    return {
      mode: "release-single",
      selfTest: false,
      manifest: resolve(values.get("manifest") ?? defaultManifest),
      runFiles: [],
      singleRun: resolve(values.get("single-run")),
      preflightRun: "",
      output: values.get("output") ? resolve(values.get("output")) : "",
    };
  }
  const runFiles = ["run-1", "run-2", "run-3"].map((name) => {
    const path = values.get(name);
    if (!path) throw new Error(`--${name} is required`);
    return resolve(path);
  });
  return {
    mode: "release",
    selfTest: false,
    manifest: resolve(values.get("manifest") ?? defaultManifest),
    runFiles,
    singleRun: "",
    preflightRun: "",
    output: values.get("output") ? resolve(values.get("output")) : "",
  };
}

function sha256(content) {
  return createHash("sha256").update(content).digest("hex");
}

function round(value) {
  return Math.round(value * 100) / 100;
}

main().catch((error) => {
  process.stderr.write(
    `${error instanceof Error ? error.message : String(error)}\n`,
  );
  process.exitCode = 1;
});
