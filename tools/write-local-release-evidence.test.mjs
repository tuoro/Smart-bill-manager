import assert from "node:assert/strict";
import test from "node:test";

import { buildSafeEvidence } from "./write-local-release-evidence.mjs";

const identity = {
  baseline_head: "a".repeat(40),
  release_input_sha256: "b".repeat(64),
  image_id: `sha256:${"c".repeat(64)}`,
  base_compose_config_sha256: "d".repeat(64),
  acceptance_compose_config_sha256: "e".repeat(64),
};
const pageNames = ["login", "inbox", "review", "payments"];
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

function reports() {
  const timing = (warmups, requests, concurrency, threshold) => ({
    warmups,
    requests,
    concurrency,
    p95_ms: 1,
    threshold_p95_ms: threshold,
    passed: true,
  });
  const deployment = {
    build_sha: identity.baseline_head,
    release_input_sha256: identity.release_input_sha256,
    compose_config_sha256: identity.acceptance_compose_config_sha256,
    image_id: identity.image_id,
    container_identity_passed: true,
    compose_version: "v2.39.1",
    server_cpu_limit: 2,
    server_memory_limit_bytes: 3584 * 1024 * 1024,
    database_location: "named-volume:sbm_postgres_data",
    object_storage_location: "named-volume:sbm_objects",
    provider_latency: "excluded-measured-by-memory-gate",
  };
  const browserIdentity = {
    baseline_head: identity.baseline_head,
    release_input_sha256: identity.release_input_sha256,
    compose_config_sha256: identity.acceptance_compose_config_sha256,
    image_id: identity.image_id,
  };
  return {
    image: {
      report_kind: "m4-local-release-image-result",
      passed: true,
      build_identity: identity,
      compose: {
        base_config_sha256: identity.base_compose_config_sha256,
        acceptance_config_sha256: identity.acceptance_compose_config_sha256,
        static_gate_count: 13,
      },
      image: {
        static_gate_count: 12,
        required_assets: Object.fromEntries(
          Array.from({ length: 17 }, (_, index) => [`asset_${index}`, true]),
        ),
        forbidden_assets_absent: true,
        toolchains_absent: true,
        package_managers_absent: true,
      },
    },
    bootstrap: {
      report_kind: "m4-bootstrap-owner-result",
      passed: true,
      build_identity: identity,
      first_bootstrap_passed: true,
      repeated_bootstrap_rejected: true,
      password_file_transport_only: true,
      terminal_secret_hits: 0,
    },
    entrypoint: {
      report_kind: "m4-entrypoint-failure-boundary-result",
      passed: true,
      build_identity: identity,
      cases: Object.fromEntries(
        [
          "valid_raw",
          "empty",
          "too_large",
          "invalid_format",
          "broad_permissions",
          "symbolic_link",
          "multiple_hardlinks",
        ].map((name) => [name, true]),
      ),
    },
    runtime: {
      report_kind: "m4-local-release-runtime-result",
      passed: true,
      build_identity: identity,
      authentication: { passed: true },
      runtime_security: { passed: true },
      secret_scan: {
        argv_environment_history_log_hits: 0,
        passed: true,
      },
    },
    performance: {
      report_kind: "m4-performance-result",
      passed: true,
      seed_kind: "m4-performance-10k-facts",
      data_shape: {
        payments_before_confirmation: 5_000,
        invoices_before_confirmation: 5_000,
        source_claim_chains: 10_000,
      },
      reference_deployment: deployment,
      non_ai_json_api: Object.fromEntries(
        [
          "inbox_list",
          "document_detail",
          "claim_set_detail",
          "payment_list",
          "invoice_list",
          "fact_insights",
        ].map((name) => [name, timing(100, 1_000, 10, 300)]),
      ),
      document_create_server_timing: timing(20, 200, 2, 500),
      review_confirm_server_timing: timing(20, 200, 2, 500),
    },
    memory: {
      report_kind: "m4-memory-stability-result",
      passed: true,
      reference_deployment: deployment,
      environment: { process_identity_passed: true },
      provider: {
        capability_probes: 1,
        extractions: 60,
        probe_latency_ms: { samples: 1, p50: 1, p95: 1, max: 1 },
        extraction_latency_ms: {
          samples: 60,
          p50: 1,
          p95: 2,
          max: 3,
        },
      },
      protocol: {
        warmup_jobs: 10,
        measured_jobs: 50,
        idle_after_terminal_ms: 2_000,
        first_and_last_window: 10,
        maximum_last_to_first_median_ratio: 1.2,
        maximum_slope_mib_per_job: 0.5,
      },
      result: {
        last_to_first_median_ratio: 1,
        linear_regression_slope_mib_per_job: 0,
        orphan_processing_or_cancel_requested_jobs: [],
        median_gate_passed: true,
        slope_gate_passed: true,
        orphan_gate_passed: true,
      },
      samples: Array.from({ length: 50 }, () => ({})),
    },
    lighthouse: {
      report_kind: "m4-lighthouse-result",
      passed: true,
      build_identity: browserIdentity,
      protocol: {
        runs_per_page: 3,
        network_policy: {
          loopback_origin_only: true,
          closed_loopback_proxy: true,
          background_networking_disabled: true,
        },
      },
      pages: Object.fromEntries(
        pageNames.map((name) => [
          name,
          {
            runs: [{}, {}, {}],
            worst_performance: 90,
            worst_accessibility: 100,
            performance_threshold: 85,
            accessibility_threshold: 95,
            passed: true,
          },
        ]),
      ),
    },
    responsive: {
      report_kind: "m4-responsive-accessibility-result",
      passed: true,
      build_identity: browserIdentity,
      protocol: {
        network_policy: {
          loopback_origin_only: true,
          closed_loopback_proxy: true,
          background_networking_disabled: true,
        },
      },
      formal: [768, 1024, 1440, 1920].flatMap((width) =>
        pageNames.map((page) => ({
          id: `formal:${width}:${page}`,
          passed: true,
        })),
      ),
      equivalent_reflow: [768, 1024, 1440, 1920].flatMap((width) =>
        pageNames.map((page) => ({
          id: `equivalent-200-percent:${width}-to-${width / 2}:${page}`,
          passed: true,
        })),
      ),
      keyboard: { passed: true },
      dark_theme: { passed: true },
      failed_checks: [],
    },
    playwright: {
      report_kind: "m4-playwright-result",
      passed: true,
      build_identity: browserIdentity,
      required_spec_files: 6,
      minimum_passed_scenarios: 33,
      network_policy: {
        loopback_origin_only: true,
        closed_loopback_proxy: true,
        background_networking_disabled: true,
      },
      spec_files: 6,
      total_scenarios: 33,
      passed_scenarios: 33,
      failed_scenarios: 0,
      skipped_scenarios: 0,
      failed_gates: [],
    },
    static: {
      report_kind: "m4-local-static-gates-result",
      passed: true,
      build_identity: identity,
      gates: Object.fromEntries(staticGateNames.map((name) => [name, true])),
      counts: {
        node_test_files: 13,
        web_test_files: 9,
        web_test_cases: 38,
        critical_invariants_passed: 140,
        critical_invariants_total: 140,
      },
      coverage: {
        domain_application_percent: 85,
        infrastructure_transport_percent: 70,
      },
    },
  };
}

test("safe evidence merger emits only approved aggregate fields", () => {
  const value = buildSafeEvidence(reports(), {
    expectedHead: identity.baseline_head,
    expectedReleaseInput: identity.release_input_sha256,
  });
  assert.equal(value.passed, true);
  assert.equal(value.performance.document_create.requests, 200);
  assert.equal(
    value.privacy_and_scope.final_candidate_build_network_isolated,
    true,
  );
  assert.equal(value.privacy_and_scope.final_acceptance_network_isolated, true);
  assert.equal(
    value.privacy_and_scope.prior_tooling_network_policy_incident_disclosed,
    true,
  );
  assert.equal("external_network_used" in value.privacy_and_scope, false);
  assert.equal(JSON.stringify(value).includes("/tmp/"), false);
});

test("safe evidence merger rejects threshold and identity drift", () => {
  const input = reports();
  input.performance.document_create_server_timing.p95_ms = 501;
  assert.throws(() =>
    buildSafeEvidence(input, {
      expectedHead: identity.baseline_head,
      expectedReleaseInput: identity.release_input_sha256,
    }),
  );
  const identityDrift = reports();
  identityDrift.playwright.build_identity.image_id = `sha256:${"f".repeat(64)}`;
  assert.throws(() =>
    buildSafeEvidence(identityDrift, {
      expectedHead: identity.baseline_head,
      expectedReleaseInput: identity.release_input_sha256,
    }),
  );
});

test("safe evidence merger rejects incomplete or duplicated gate shapes", () => {
  const expected = {
    expectedHead: identity.baseline_head,
    expectedReleaseInput: identity.release_input_sha256,
  };
  const mutations = [
    (input) => delete input.static.gates.coverage,
    (input) => {
      input.entrypoint.cases.unexpected = true;
      delete input.entrypoint.cases.empty;
    },
    (input) => delete input.memory.result.last_to_first_median_ratio,
    (input) => {
      input.responsive.formal[1].id = input.responsive.formal[0].id;
    },
    (input) => {
      input.performance.data_shape.source_claim_chains = 9_999;
    },
    (input) => delete input.image.image.required_assets.asset_16,
    (input) => {
      input.memory.environment.process_identity_passed = false;
    },
    (input) => {
      input.lighthouse.protocol.network_policy.closed_loopback_proxy = false;
    },
    (input) => {
      input.performance.reference_deployment.database_location = "host-path";
    },
    (input) => {
      input.memory.provider.extraction_latency_ms.samples = 59;
    },
  ];
  for (const mutate of mutations) {
    const input = reports();
    mutate(input);
    assert.throws(() => buildSafeEvidence(input, expected));
  }
});
