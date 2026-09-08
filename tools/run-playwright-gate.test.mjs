import assert from "node:assert/strict";
import test from "node:test";

import { readdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  minimumPlaywrightScenarios,
  parseArguments,
  requiredPlaywrightSpecFiles,
  summarizePlaywright,
} from "./run-playwright-gate.mjs";

const scenariosPerSpecFile = Math.ceil(
  minimumPlaywrightScenarios / requiredPlaywrightSpecFiles,
);

test("Playwright summary requires every spec file and the minimum passing scenarios", () => {
  const report = {
    suites: Array.from({ length: requiredPlaywrightSpecFiles }, (_, fileIndex) => ({
      file: `e2e/spec-${fileIndex}.spec.ts`,
      specs: Array.from({ length: scenariosPerSpecFile }, (_, testIndex) => ({
        file: `e2e/spec-${fileIndex}.spec.ts`,
        tests: [{ results: [{ status: "passed", testIndex }] }],
      })),
    })),
    errors: [],
  };
  const result = summarizePlaywright(report, 0);
  assert.equal(result.spec_files, requiredPlaywrightSpecFiles);
  assert.ok(result.passed_scenarios >= minimumPlaywrightScenarios);
  assert.equal(result.passed, true);
  assert.equal(
    summarizePlaywright({ ...report, suites: report.suites.slice(0, 6) }, 0)
      .passed,
    false,
  );
  assert.equal(
    summarizePlaywright({ ...report, errors: ["synthetic runner error"] }, 0)
      .passed,
    false,
  );
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

// 常量与磁盘上的规格文件一旦脱节，门禁会以 spec_file_count 失败，而这只有在
// 发版时才会暴露。v0.4.0 之后新增两个规格却没更新常量就是这样漏过去的。
test("required spec file count matches the specs on disk", async () => {
  const specDirectory = resolve(
    dirname(fileURLToPath(import.meta.url)),
    "../apps/web/e2e",
  );
  const specFiles = (await readdir(specDirectory)).filter((name) =>
    name.endsWith(".spec.ts"),
  );
  assert.equal(
    specFiles.length,
    requiredPlaywrightSpecFiles,
    `apps/web/e2e 有 ${specFiles.length} 个规格文件，requiredPlaywrightSpecFiles 是 ${requiredPlaywrightSpecFiles}；新增或删除规格后必须同步这个常量。`,
  );
});
