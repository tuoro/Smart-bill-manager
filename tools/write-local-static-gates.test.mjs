import assert from "node:assert/strict";
import test from "node:test";

import { buildReport } from "./write-local-static-gates.mjs";

const gateNames = [
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

test("static gate report enforces coverage, counts, and every boolean", () => {
  const options = {
    expectedHead: "a".repeat(40),
    expectedReleaseInput: "b".repeat(64),
    imageID: `sha256:${"c".repeat(64)}`,
    baseComposeConfigSha256: "d".repeat(64),
    acceptanceComposeConfigSha256: "e".repeat(64),
    nodeTestFiles: 13,
    webTestFiles: 9,
    webTestCases: 38,
    criticalInvariantsPassed: 140,
    criticalInvariantsTotal: 140,
    domainCoveragePercent: 85,
    transportCoveragePercent: 70,
    gates: Object.fromEntries(gateNames.map((name) => [name, true])),
  };
  assert.equal(buildReport(options).passed, true);
  options.transportCoveragePercent = 69.99;
  assert.equal(buildReport(options).passed, false);
  options.transportCoveragePercent = 70;
  options.criticalInvariantsTotal = 139;
  options.criticalInvariantsPassed = 139;
  assert.equal(buildReport(options).passed, false);
});
