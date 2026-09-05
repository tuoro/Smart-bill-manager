#!/usr/bin/env node

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

import { requiredImageFiles } from "./check-release-image.mjs";
import { minimumLocalStaticCounts } from "./write-local-static-gates.mjs";
import {
  requiredPlaywrightSpecFiles,
  minimumPlaywrightScenarios,
} from "./run-playwright-gate.mjs";

import {
  SafeToolError,
  parseStrictPairs,
  readProtectedJSON,
  requireGitSHA,
  requireImageID,
  requireSHA256,
  reserveProtectedFile,
  safeErrorCode,
} from "./lib/protected-output.mjs";

const endpointNames = [
  "inbox_list",
  "document_detail",
  "claim_set_detail",
  "payment_list",
  "invoice_list",
  "fact_insights",
];
const pageNames = ["login", "inbox", "review", "payments"];
const entrypointCaseNames = [
  "valid_raw",
  "valid_raw_lf",
  "valid_raw_crlf",
  "empty",
  "too_large",
  "invalid_format",
  "broad_permissions",
  "symbolic_link",
  "multiple_hardlinks",
];
const staticGateNames = [
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
];

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const output = await reserveProtectedFile(
    options.output,
    Object.values(options.reports),
  );
  try {
    const reports = Object.fromEntries(
      await Promise.all(
        Object.entries(options.reports).map(async ([name, path]) => [
          name,
          await readProtectedJSON(
            path,
            name === "playwright" ? 64 * 1024 * 1024 : 8 * 1024 * 1024,
          ),
        ]),
      ),
    );
    const evidence = buildSafeEvidence(reports, options);
    await output.writeJSON(evidence);
    process.stdout.write(
      `${JSON.stringify({ evidence_kind: evidence.evidence_kind, passed: evidence.passed })}\n`,
    );
  } finally {
    await output.close();
  }
}

export function parseArguments(argumentsList) {
  const reportNames = [
    "image-report",
    "entrypoint-report",
    "bootstrap-report",
    "runtime-report",
    "performance-report",
    "memory-report",
    "lighthouse-report",
    "responsive-report",
    "playwright-report",
    "static-report",
  ];
  const values = parseStrictPairs(argumentsList, [
    "output",
    "expected-head",
    "expected-release-input-sha256",
    ...reportNames,
  ]);
  return {
    output: resolve(values.get("output")),
    expectedHead: requireGitSHA(values.get("expected-head")),
    expectedReleaseInput: requireSHA256(
      values.get("expected-release-input-sha256"),
    ),
    reports: Object.fromEntries(
      reportNames.map((name) => [
        name.replace("-report", "").replaceAll("-", "_"),
        resolve(values.get(name)),
      ]),
    ),
  };
}

export function buildSafeEvidence(reports, expected) {
  const image = reports.image;
  assertReport(image, "m4-local-release-image-result");
  const identity = {
    baseline_head: expected.expectedHead,
    release_input_sha256: expected.expectedReleaseInput,
    image_id: image.build_identity?.image_id,
    base_compose_config_sha256: image.compose?.base_config_sha256,
    acceptance_compose_config_sha256: image.compose?.acceptance_config_sha256,
  };
  requireIdentity(image.build_identity, identity, false);
  requireImageID(identity.image_id);
  requireSHA256(identity.base_compose_config_sha256);
  requireSHA256(identity.acceptance_compose_config_sha256);
  if (
    image.compose?.static_gate_count !== 13 ||
    image.image?.static_gate_count !== 12 ||
    !sameMembers(
      Object.keys(image.image?.required_assets ?? {}),
      requiredImageFiles.map((path) => path.slice(1).replaceAll("/", "_")),
    ) ||
    !Object.values(image.image.required_assets).every(Boolean) ||
    image.image.forbidden_assets_absent !== true ||
    image.image.toolchains_absent !== true ||
    image.image.package_managers_absent !== true ||
    image.image.runtime_assets_readable !== true
  ) {
    invalid();
  }

  assertReport(reports.bootstrap, "m4-bootstrap-owner-result");
  requireIdentity(reports.bootstrap.build_identity, identity, false);
  if (
    reports.bootstrap.first_bootstrap_passed !== true ||
    reports.bootstrap.repeated_bootstrap_rejected !== true ||
    reports.bootstrap.password_file_transport_only !== true ||
    reports.bootstrap.terminal_secret_hits !== 0
  ) {
    invalid();
  }

  assertReport(reports.entrypoint, "m4-entrypoint-failure-boundary-result");
  requireIdentity(reports.entrypoint.build_identity, identity, false);
  if (
    !sameMembers(
      Object.keys(reports.entrypoint.cases ?? {}),
      entrypointCaseNames,
    ) ||
    !Object.values(reports.entrypoint.cases ?? {}).every(Boolean)
  ) {
    invalid();
  }

  assertReport(reports.runtime, "m4-local-release-runtime-result");
  requireIdentity(reports.runtime.build_identity, identity, false);
  if (
    reports.runtime.authentication?.passed !== true ||
    reports.runtime.runtime_security?.passed !== true ||
    reports.runtime.secret_scan?.argv_environment_history_log_hits !== 0 ||
    reports.runtime.secret_scan?.passed !== true
  ) {
    invalid();
  }

  assertReport(reports.performance, "m4-performance-result");
  requireDeploymentIdentity(reports.performance.reference_deployment, identity);
  const performanceP95 = validatePerformance(reports.performance);

  assertReport(reports.memory, "m4-memory-stability-result");
  requireDeploymentIdentity(reports.memory.reference_deployment, identity);
  validateMemory(reports.memory);

  assertReport(reports.lighthouse, "m4-lighthouse-result");
  requireBrowserIdentity(reports.lighthouse.build_identity, identity);
  const lighthouseMinimums = validateLighthouse(reports.lighthouse);

  assertReport(reports.responsive, "m4-responsive-accessibility-result");
  requireBrowserIdentity(reports.responsive.build_identity, identity);
  validateResponsive(reports.responsive);

  assertReport(reports.playwright, "m4-playwright-result");
  requireBrowserIdentity(reports.playwright.build_identity, identity);
  validatePlaywright(reports.playwright);

  assertReport(reports.static, "m4-local-static-gates-result");
  requireStaticIdentity(reports.static.build_identity, identity);
  validateStatic(reports.static);

  const evidence = {
    evidence_kind: "m4-local-release-readiness-gate-summary",
    evidence_version: 1,
    status: "passed_local_only",
    build_identity: identity,
    release_artifact: {
      compose_static_gates: image.compose.static_gate_count,
      image_static_gates: image.image.static_gate_count,
      required_assets_present: Object.values(
        image.image.required_assets ?? {},
      ).every(Boolean),
      forbidden_assets_absent: image.image.forbidden_assets_absent,
      toolchains_absent: image.image.toolchains_absent,
      package_managers_absent: image.image.package_managers_absent,
      runtime_assets_readable: image.image.runtime_assets_readable,
      entrypoint_failure_cases_passed: Object.keys(reports.entrypoint.cases)
        .length,
      bootstrap_owner_atomic: reports.bootstrap.passed,
      authenticated_runtime_ready: reports.runtime.authentication.passed,
      runtime_least_privilege: reports.runtime.runtime_security.passed,
      credential_material_hits: 0,
    },
    performance: {
      non_ai_json_api: Object.fromEntries(
        endpointNames.map((name) => [
          name,
          {
            warmups: 100,
            requests: 1_000,
            concurrency: 10,
            p95_ms: performanceP95[name],
            threshold_p95_ms: 300,
          },
        ]),
      ),
      document_create: {
        warmups: 20,
        requests: 200,
        concurrency: 2,
        p95_ms: reports.performance.document_create_server_timing.p95_ms,
        threshold_p95_ms: 500,
      },
      review_confirm: {
        warmups: 20,
        requests: 200,
        concurrency: 2,
        p95_ms: reports.performance.review_confirm_server_timing.p95_ms,
        threshold_p95_ms: 500,
      },
    },
    provider_latency_ms: {
      capability_probe: reports.memory.provider.probe_latency_ms,
      extraction: reports.memory.provider.extraction_latency_ms,
    },
    memory_stability: {
      warmup_jobs: 10,
      measured_jobs: 50,
      idle_after_terminal_ms: 2_000,
      last_to_first_median_ratio:
        reports.memory.result.last_to_first_median_ratio,
      maximum_last_to_first_median_ratio: 1.2,
      linear_regression_slope_mib_per_job:
        reports.memory.result.linear_regression_slope_mib_per_job,
      maximum_slope_mib_per_job: 0.5,
      orphan_processing_or_cancel_requested_jobs: 0,
    },
    accessibility: {
      pages: Object.fromEntries(
        pageNames.map((name) => [
          name,
          {
            runs: 3,
            minimum_performance: lighthouseMinimums[name].performance,
            performance_threshold: 85,
            minimum_accessibility: lighthouseMinimums[name].accessibility,
            accessibility_threshold: 95,
          },
        ]),
      ),
      formal_responsive_passed: reports.responsive.formal.length,
      formal_responsive_total: 16,
      equivalent_reflow_passed: reports.responsive.equivalent_reflow.length,
      equivalent_reflow_total: 16,
      keyboard_passed: true,
      dark_theme_passed: true,
    },
    browser_acceptance: {
      spec_files: reports.playwright.spec_files,
      passed_scenarios: reports.playwright.passed_scenarios,
      failed_scenarios: 0,
      skipped_scenarios: 0,
      minimum_passed_scenarios: minimumPlaywrightScenarios,
    },
    static_verification: {
      gates: reports.static.gates,
      counts: reports.static.counts,
      coverage: reports.static.coverage,
    },
    privacy_and_scope: {
      raw_reports_committed: false,
      credentials_committed: false,
      real_provider_called: false,
      real_email_connected: false,
      final_candidate_build_network_isolated: true,
      final_acceptance_network_isolated: true,
      prior_tooling_network_policy_incident_disclosed: true,
      deployed_or_published: false,
    },
    remaining_release_gates: [
      "formal_real_model_evaluation",
      "real_external_system_integration",
      "production_deployment_and_release",
    ],
    passed: true,
  };
  assertSafeAggregate(evidence);
  return evidence;
}

function validatePerformance(report) {
  if (
    report.seed_kind !== "m4-performance-10k-facts" ||
    report.data_shape?.payments_before_confirmation !== 5_000 ||
    report.data_shape?.invoices_before_confirmation !== 5_000 ||
    report.data_shape?.source_claim_chains !== 10_000 ||
    report.reference_deployment?.provider_latency !==
      "excluded-measured-by-memory-gate"
  ) {
    invalid();
  }
  if (!sameMembers(Object.keys(report.non_ai_json_api ?? {}), endpointNames)) {
    invalid();
  }
  const result = {};
  for (const name of endpointNames) {
    const metric = report.non_ai_json_api[name];
    validateTiming(metric, 100, 1_000, 10, 300);
    result[name] = metric.p95_ms;
  }
  validateTiming(report.document_create_server_timing, 20, 200, 2, 500);
  validateTiming(report.review_confirm_server_timing, 20, 200, 2, 500);
  return result;
}

function validateTiming(metric, warmups, requests, concurrency, threshold) {
  if (
    metric?.warmups !== warmups ||
    metric?.requests !== requests ||
    metric?.concurrency !== concurrency ||
    metric?.threshold_p95_ms !== threshold ||
    typeof metric?.p95_ms !== "number" ||
    metric.p95_ms < 0 ||
    metric.p95_ms > threshold ||
    metric.passed !== true
  ) {
    invalid();
  }
}

function validateMemory(report) {
  const protocol = report.protocol;
  const result = report.result;
  if (
    protocol?.warmup_jobs !== 10 ||
    protocol?.measured_jobs !== 50 ||
    protocol?.idle_after_terminal_ms !== 2_000 ||
    protocol?.first_and_last_window !== 10 ||
    protocol?.maximum_last_to_first_median_ratio !== 1.2 ||
    protocol?.maximum_slope_mib_per_job !== 0.5 ||
    !Number.isFinite(result?.last_to_first_median_ratio) ||
    result.last_to_first_median_ratio < 0 ||
    result?.last_to_first_median_ratio > 1.2 ||
    !Number.isFinite(result?.linear_regression_slope_mib_per_job) ||
    result?.linear_regression_slope_mib_per_job > 0.5 ||
    result?.orphan_processing_or_cancel_requested_jobs?.length !== 0 ||
    result?.median_gate_passed !== true ||
    result?.slope_gate_passed !== true ||
    result?.orphan_gate_passed !== true ||
    report.environment?.process_identity_passed !== true ||
    report.provider?.capability_probes !== 1 ||
    report.provider?.extractions !== 60 ||
    !validLatencySummary(report.provider?.probe_latency_ms, 1) ||
    !validLatencySummary(report.provider?.extraction_latency_ms, 60) ||
    report.samples?.length !== 50
  ) {
    invalid();
  }
}

function validLatencySummary(value, samples) {
  return (
    value?.samples === samples &&
    Number.isFinite(value?.p50) &&
    Number.isFinite(value?.p95) &&
    Number.isFinite(value?.max) &&
    value.p50 >= 0 &&
    value.p50 <= value.p95 &&
    value.p95 <= value.max
  );
}

function validateLighthouse(report) {
  if (
    report.protocol?.runs_per_page !== 3 ||
    !validBrowserNetworkPolicy(report.protocol?.network_policy) ||
    !sameMembers(Object.keys(report.pages ?? {}), pageNames)
  ) {
    invalid();
  }
  const result = {};
  for (const name of pageNames) {
    const page = report.pages[name];
    if (
      page?.runs?.length !== 3 ||
      page?.performance_threshold !== 85 ||
      page?.accessibility_threshold !== 95 ||
      !Number.isFinite(page?.worst_performance) ||
      !Number.isFinite(page?.worst_accessibility) ||
      page.worst_performance > 100 ||
      page.worst_accessibility > 100 ||
      page?.worst_performance < 85 ||
      page?.worst_accessibility < 95 ||
      page?.passed !== true
    ) {
      invalid();
    }
    result[name] = {
      performance: page.worst_performance,
      accessibility: page.worst_accessibility,
    };
  }
  return result;
}

function validateResponsive(report) {
  const expectedFormal = [];
  const expectedReflow = [];
  for (const width of [768, 1024, 1440, 1920]) {
    for (const page of pageNames) {
      expectedFormal.push(`formal:${width}:${page}`);
      expectedReflow.push(
        `equivalent-200-percent:${width}-to-${width / 2}:${page}`,
      );
    }
  }
  if (
    report.formal?.length !== 16 ||
    report.equivalent_reflow?.length !== 16 ||
    !validBrowserNetworkPolicy(report.protocol?.network_policy) ||
    !sameMembers(
      report.formal.map((entry) => entry.id),
      expectedFormal,
    ) ||
    !sameMembers(
      report.equivalent_reflow.map((entry) => entry.id),
      expectedReflow,
    ) ||
    !report.formal.every((entry) => entry.passed === true) ||
    !report.equivalent_reflow.every((entry) => entry.passed === true) ||
    report.keyboard?.passed !== true ||
    report.dark_theme?.passed !== true ||
    report.failed_checks?.length !== 0
  ) {
    invalid();
  }
}

function validatePlaywright(report) {
  if (
    report.required_spec_files !== requiredPlaywrightSpecFiles ||
    report.minimum_passed_scenarios !== minimumPlaywrightScenarios ||
    report.spec_files !== requiredPlaywrightSpecFiles ||
    report.passed_scenarios < minimumPlaywrightScenarios ||
    report.total_scenarios !== report.passed_scenarios ||
    report.failed_scenarios !== 0 ||
    report.skipped_scenarios !== 0 ||
    !validBrowserNetworkPolicy(report.network_policy) ||
    report.failed_gates?.length !== 0
  ) {
    invalid();
  }
}

function validateStatic(report) {
  if (
    !sameMembers(Object.keys(report.gates ?? {}), staticGateNames) ||
    !Object.values(report.gates ?? {}).every((value) => value === true) ||
    report.counts?.node_test_files < minimumLocalStaticCounts.node_test_files ||
    report.counts?.web_test_files < minimumLocalStaticCounts.web_test_files ||
    report.counts?.web_test_cases < minimumLocalStaticCounts.web_test_cases ||
    report.counts?.critical_invariants_passed !==
      report.counts?.critical_invariants_total ||
    report.counts?.critical_invariants_total <
      minimumLocalStaticCounts.critical_invariants_total ||
    report.coverage?.domain_application_percent < 85 ||
    report.coverage?.infrastructure_transport_percent < 70
  ) {
    invalid();
  }
}

function requireIdentity(actual, expected, partial) {
  if (
    actual?.baseline_head !== expected.baseline_head ||
    actual?.release_input_sha256 !== expected.release_input_sha256 ||
    (!partial && actual?.image_id !== expected.image_id)
  ) {
    invalid();
  }
}

function requireDeploymentIdentity(actual, expected) {
  if (
    actual?.build_sha !== expected.baseline_head ||
    actual?.release_input_sha256 !== expected.release_input_sha256 ||
    actual?.compose_config_sha256 !==
      expected.acceptance_compose_config_sha256 ||
    actual?.image_id !== expected.image_id ||
    actual?.container_identity_passed !== true ||
    actual?.server_cpu_limit !== 2 ||
    actual?.server_memory_limit_bytes !== 3584 * 1024 * 1024 ||
    actual?.database_location !== "named-volume:sbm_postgres_data" ||
    actual?.object_storage_location !== "named-volume:sbm_objects" ||
    !/^v?\d+\.\d+(?:\.\d+)?(?:[-+][a-z0-9.-]+)?$/i.test(
      actual?.compose_version ?? "",
    )
  ) {
    invalid();
  }
}

function requireBrowserIdentity(actual, expected) {
  if (
    actual?.baseline_head !== expected.baseline_head ||
    actual?.release_input_sha256 !== expected.release_input_sha256 ||
    actual?.compose_config_sha256 !==
      expected.acceptance_compose_config_sha256 ||
    actual?.image_id !== expected.image_id
  ) {
    invalid();
  }
}

function requireStaticIdentity(actual, expected) {
  if (
    actual?.baseline_head !== expected.baseline_head ||
    actual?.release_input_sha256 !== expected.release_input_sha256 ||
    actual?.image_id !== expected.image_id ||
    actual?.base_compose_config_sha256 !==
      expected.base_compose_config_sha256 ||
    actual?.acceptance_compose_config_sha256 !==
      expected.acceptance_compose_config_sha256
  ) {
    invalid();
  }
}

function assertReport(report, kind) {
  if (report?.report_kind !== kind || report?.passed !== true) invalid();
}

function validBrowserNetworkPolicy(policy) {
  return (
    policy?.loopback_origin_only === true &&
    policy?.closed_loopback_proxy === true &&
    policy?.background_networking_disabled === true
  );
}

function assertSafeAggregate(value) {
  const encoded = JSON.stringify(value);
  for (const forbidden of [
    "/tmp/",
    "/home/",
    "cookie",
    "password",
    "api_key",
    "provider_key",
    "job_id",
    "document_id",
    "container_id",
    "process_id",
  ]) {
    if (encoded.toLowerCase().includes(forbidden)) {
      throw new SafeToolError("unsafe_evidence_aggregate");
    }
  }
}

function sameMembers(actual, expected) {
  return (
    actual.length === expected.length &&
    [...actual].sort().join("\0") === [...expected].sort().join("\0")
  );
}

function invalid() {
  throw new SafeToolError("evidence_inconsistent");
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`local-release-evidence: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
