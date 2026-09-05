#!/usr/bin/env node

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

import {
  SafeToolError,
  parseStrictPairs,
  requireGitSHA,
  requireImageID,
  requireSHA256,
  reserveProtectedFile,
  safeErrorCode,
} from "./lib/protected-output.mjs";

const booleanGates = [
  "go-test",
  "go-vet",
  "go-build",
  "web-check",
  "node-tests",
  "critical-invariants",
  "coverage",
  "diff-check",
  "sensitive-audit",
  "large-file-audit",
  "temporary-audit",
  "process-audit",
  "release-input-recheck",
];
const expectedGateKeys = booleanGates.map((name) => name.replaceAll("-", "_"));
export const minimumLocalStaticCounts = {
  node_test_files: 19,
  web_test_files: 11,
  web_test_cases: 51,
  critical_invariants_total: 245,
};

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const output = await reserveProtectedFile(options.output);
  try {
    const report = buildReport(options);
    await output.writeJSON(report);
    process.stdout.write(
      `${JSON.stringify({ report_kind: report.report_kind, gate_count: booleanGates.length, passed: report.passed })}\n`,
    );
    if (!report.passed) process.exitCode = 1;
  } finally {
    await output.close();
  }
}

export function parseArguments(argumentsList) {
  const required = [
    "output",
    "expected-head",
    "expected-release-input-sha256",
    "image-id",
    "base-compose-config-sha256",
    "acceptance-compose-config-sha256",
    "node-test-files",
    "web-test-files",
    "web-test-cases",
    "critical-invariants-passed",
    "critical-invariants-total",
    "domain-coverage-percent",
    "transport-coverage-percent",
    ...booleanGates,
  ];
  const values = parseStrictPairs(argumentsList, required);
  const gates = Object.fromEntries(
    booleanGates.map((name) => [
      name.replaceAll("-", "_"),
      parseBoolean(values.get(name)),
    ]),
  );
  return {
    output: resolve(values.get("output")),
    expectedHead: requireGitSHA(values.get("expected-head")),
    expectedReleaseInput: requireSHA256(
      values.get("expected-release-input-sha256"),
    ),
    imageID: requireImageID(values.get("image-id")),
    baseComposeConfigSha256: requireSHA256(
      values.get("base-compose-config-sha256"),
    ),
    acceptanceComposeConfigSha256: requireSHA256(
      values.get("acceptance-compose-config-sha256"),
    ),
    nodeTestFiles: positiveInteger(values.get("node-test-files")),
    webTestFiles: positiveInteger(values.get("web-test-files")),
    webTestCases: positiveInteger(values.get("web-test-cases")),
    criticalInvariantsPassed: positiveInteger(
      values.get("critical-invariants-passed"),
    ),
    criticalInvariantsTotal: positiveInteger(
      values.get("critical-invariants-total"),
    ),
    domainCoveragePercent: percentage(values.get("domain-coverage-percent")),
    transportCoveragePercent: percentage(
      values.get("transport-coverage-percent"),
    ),
    gates,
  };
}

export function buildReport(options) {
  const countsPassed =
    options.nodeTestFiles >= minimumLocalStaticCounts.node_test_files &&
    options.webTestFiles >= minimumLocalStaticCounts.web_test_files &&
    options.webTestCases >= minimumLocalStaticCounts.web_test_cases &&
    options.criticalInvariantsPassed === options.criticalInvariantsTotal &&
    options.criticalInvariantsTotal >=
      minimumLocalStaticCounts.critical_invariants_total;
  const coveragePassed =
    options.domainCoveragePercent >= 85 &&
    options.transportCoveragePercent >= 70;
  const passed =
    Object.keys(options.gates).length === expectedGateKeys.length &&
    expectedGateKeys.every((name) => options.gates[name] === true) &&
    Object.values(options.gates).every(Boolean) &&
    countsPassed &&
    coveragePassed;
  return {
    report_kind: "m4-local-static-gates-result",
    protocol_version: 1,
    build_identity: {
      baseline_head: options.expectedHead,
      release_input_sha256: options.expectedReleaseInput,
      image_id: options.imageID,
      base_compose_config_sha256: options.baseComposeConfigSha256,
      acceptance_compose_config_sha256: options.acceptanceComposeConfigSha256,
    },
    gates: options.gates,
    counts: {
      node_test_files: options.nodeTestFiles,
      web_test_files: options.webTestFiles,
      web_test_cases: options.webTestCases,
      critical_invariants_passed: options.criticalInvariantsPassed,
      critical_invariants_total: options.criticalInvariantsTotal,
    },
    coverage: {
      domain_application_percent: options.domainCoveragePercent,
      infrastructure_transport_percent: options.transportCoveragePercent,
      domain_application_threshold_percent: 85,
      infrastructure_transport_threshold_percent: 70,
    },
    passed,
  };
}

function parseBoolean(value) {
  if (value === "true") return true;
  if (value === "false") return false;
  throw new SafeToolError("invalid_arguments");
}

function positiveInteger(value) {
  const result = Number(value);
  if (!Number.isSafeInteger(result) || result < 1) {
    throw new SafeToolError("invalid_arguments");
  }
  return result;
}

function percentage(value) {
  const result = Number(value);
  if (!Number.isFinite(result) || result < 0 || result > 100) {
    throw new SafeToolError("invalid_arguments");
  }
  return Math.round(result * 100) / 100;
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`local-static-gates: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
