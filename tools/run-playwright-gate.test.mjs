import assert from "node:assert/strict";
import test from "node:test";

import { parseArguments, summarizePlaywright } from "./run-playwright-gate.mjs";

test("Playwright summary requires six files and at least 33 passing scenarios", () => {
  const report = {
    suites: Array.from({ length: 6 }, (_, fileIndex) => ({
      file: `e2e/spec-${fileIndex}.spec.ts`,
      specs: Array.from({ length: fileIndex < 3 ? 6 : 5 }, (_, testIndex) => ({
        file: `e2e/spec-${fileIndex}.spec.ts`,
        tests: [{ results: [{ status: "passed", testIndex }] }],
      })),
    })),
    errors: [],
  };
  const result = summarizePlaywright(report, 0);
  assert.equal(result.passed_scenarios, 33);
  assert.equal(result.passed, true);
  report.suites[0].specs[0].tests[0].results[0].status = "skipped";
  assert.equal(summarizePlaywright(report, 0).passed, false);
});

test("Playwright gate accepts only loopback synthetic inputs and sibling outputs", () => {
  const argumentsList = [
    "--server",
    "http://127.0.0.1:8080",
    "--email",
    "owner@example.test",
    "--password-file",
    "/tmp/run/password",
    "--provider-base-url",
    "http://127.0.0.1:19086/v1",
    "--provider-api-key-file",
    "/tmp/run/provider-key",
    "--provider-model",
    "synthetic-local-release",
    "--output",
    "/tmp/run/playwright.json",
    "--artifacts",
    "/tmp/run/playwright-artifacts",
    "--build-sha",
    "a".repeat(40),
    "--release-input-sha256",
    "b".repeat(64),
    "--compose-config-sha256",
    "b".repeat(64),
    "--image-id",
    `sha256:${"c".repeat(64)}`,
  ];
  assert.equal(
    parseArguments(argumentsList).providerModel,
    "synthetic-local-release",
  );
  const wrongLoopbackPort = [...argumentsList];
  wrongLoopbackPort[wrongLoopbackPort.indexOf("--provider-base-url") + 1] =
    "http://127.0.0.1:19087/v1";
  assert.throws(() => parseArguments(wrongLoopbackPort));
  const remote = [...argumentsList];
  remote[1] = "https://example.test";
  assert.throws(() => parseArguments(remote));
});
