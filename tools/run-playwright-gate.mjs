#!/usr/bin/env node

import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  SafeToolError,
  parseStrictPairs,
  readProtectedSecret,
  requireDistinctPaths,
  requireGitSHA,
  requireImageID,
  requireLoopbackURL,
  requireSHA256,
  reserveProtectedDirectory,
  reserveProtectedFile,
  safeErrorCode,
} from "./lib/protected-output.mjs";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const webDirectory = resolve(repositoryRoot, "apps/web");
const playwrightExecutable = resolve(
  webDirectory,
  "node_modules/.bin/playwright",
);
const maximumReportBytes = 64 * 1024 * 1024;
export const requiredPlaywrightSpecFiles = 15;
export const minimumPlaywrightScenarios = 120;

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const output = await reserveProtectedFile(options.output, [
    options.passwordFile,
    options.providerApiKeyFile,
    options.artifacts,
  ]);
  let password;
  let providerApiKey;
  let runnerOutput;
  try {
    await reserveProtectedDirectory(options.artifacts, [
      options.passwordFile,
      options.providerApiKeyFile,
      options.output,
    ]);
    password = await readProtectedSecret(options.passwordFile, 1024);
    providerApiKey = await readProtectedSecret(
      options.providerApiKeyFile,
      4096,
    );
    runnerOutput = await runPlaywright(options);
    if (
      [runnerOutput.stdout, runnerOutput.stderr].some(
        (content) =>
          content.includes(password) || content.includes(providerApiKey),
      )
    ) {
      throw new SafeToolError("secret_detected");
    }
    let playwright;
    try {
      playwright = JSON.parse(runnerOutput.stdout.toString("utf8"));
    } catch {
      throw new SafeToolError("playwright_report_invalid");
    }
    const summary = summarizePlaywright(playwright, runnerOutput.exitCode);
    const report = {
      report_kind: "m4-playwright-result",
      protocol_version: 1,
      build_identity: {
        baseline_head: options.buildSha,
        release_input_sha256: options.releaseInputSha256,
        compose_config_sha256: options.composeConfigSha256,
        image_id: options.imageID,
      },
      required_spec_files: requiredPlaywrightSpecFiles,
      minimum_passed_scenarios: minimumPlaywrightScenarios,
      network_policy: {
        loopback_origin_only: true,
        closed_loopback_proxy: true,
        background_networking_disabled: true,
      },
      ...summary,
      playwright,
    };
    await output.writeJSON(report);
    process.stdout.write(
      `${JSON.stringify({
        report_kind: report.report_kind,
        spec_files: report.spec_files,
        passed_scenarios: report.passed_scenarios,
        failed_scenarios: report.failed_scenarios,
        skipped_scenarios: report.skipped_scenarios,
        passed: report.passed,
      })}\n`,
    );
    if (!report.passed) process.exitCode = 1;
  } finally {
    password?.fill(0);
    providerApiKey?.fill(0);
    runnerOutput?.stdout.fill(0);
    runnerOutput?.stderr.fill(0);
    await output.close();
  }
}

export function parseArguments(argumentsList) {
  const values = parseStrictPairs(argumentsList, [
    "server",
    "email",
    "password-file",
    "provider-base-url",
    "provider-api-key-file",
    "provider-model",
    "output",
    "artifacts",
    "build-sha",
    "release-input-sha256",
    "compose-config-sha256",
    "image-id",
  ]);
  requireLoopbackURL(values.get("server"), { allowPath: false });
  const providerURL = requireLoopbackURL(values.get("provider-base-url"));
  if (
    providerURL.origin !== "http://127.0.0.1:19086" ||
    !/^\/v1\/?$/.test(providerURL.pathname) ||
    providerURL.search ||
    !/^[^\s@]+@[^\s@]+$/.test(values.get("email")) ||
    !/^synthetic-[a-z0-9._-]+$/.test(values.get("provider-model"))
  ) {
    throw new SafeToolError("synthetic_identity_invalid");
  }
  const output = resolve(values.get("output"));
  const artifacts = resolve(values.get("artifacts"));
  if (dirname(output) !== dirname(artifacts) || output === artifacts) {
    throw new SafeToolError("path_conflict");
  }
  const [passwordFile, providerApiKeyFile] = requireDistinctPaths([
    values.get("password-file"),
    values.get("provider-api-key-file"),
  ]);
  return {
    server: values.get("server"),
    email: values.get("email"),
    passwordFile,
    providerBaseURL: values.get("provider-base-url"),
    providerApiKeyFile,
    providerModel: values.get("provider-model"),
    output,
    artifacts,
    buildSha: requireGitSHA(values.get("build-sha")),
    releaseInputSha256: requireSHA256(values.get("release-input-sha256")),
    composeConfigSha256: requireSHA256(values.get("compose-config-sha256")),
    imageID: requireImageID(values.get("image-id")),
  };
}

export function summarizePlaywright(report, exitCode) {
  const statuses = [];
  const specFiles = new Set();
  const visit = (suite) => {
    if (suite.file) specFiles.add(suite.file);
    for (const spec of suite.specs ?? []) {
      if (spec.file) specFiles.add(spec.file);
      for (const entry of spec.tests ?? []) {
        const results = entry.results ?? [];
        statuses.push(results.at(-1)?.status ?? "missing");
      }
    }
    for (const child of suite.suites ?? []) visit(child);
  };
  for (const suite of report.suites ?? []) visit(suite);
  const passed = statuses.filter((status) => status === "passed").length;
  const skipped = statuses.filter((status) => status === "skipped").length;
  const failed = statuses.length - passed - skipped;
  const failedGates = [];
  if (exitCode !== 0) failedGates.push("runner_exit");
  if (specFiles.size !== requiredPlaywrightSpecFiles)
    failedGates.push("spec_file_count");
  if (passed < minimumPlaywrightScenarios)
    failedGates.push("passed_scenario_count");
  if (failed !== 0) failedGates.push("failed_scenarios");
  if (skipped !== 0) failedGates.push("skipped_scenarios");
  if ((report.errors ?? []).length !== 0) failedGates.push("top_level_errors");
  return {
    spec_files: specFiles.size,
    total_scenarios: statuses.length,
    passed_scenarios: passed,
    failed_scenarios: failed,
    skipped_scenarios: skipped,
    failed_gates: failedGates,
    passed: failedGates.length === 0,
  };
}

async function runPlaywright(options) {
  const environment = selectedEnvironment({
    CI: "1",
    SBM_E2E_BASE_URL: options.server,
    SBM_E2E_EMAIL: options.email,
    SBM_E2E_PASSWORD_FILE: options.passwordFile,
    SBM_E2E_PROVIDER_BASE_URL: options.providerBaseURL,
    SBM_E2E_PROVIDER_API_KEY_FILE: options.providerApiKeyFile,
    SBM_E2E_PROVIDER_MODEL: options.providerModel,
    SBM_E2E_OUTPUT_DIR: options.artifacts,
  });
  return new Promise((resolveRun, rejectRun) => {
    const child = spawn(playwrightExecutable, ["test", "--reporter=json"], {
      cwd: webDirectory,
      env: environment,
      stdio: ["ignore", "pipe", "pipe"],
    });
    const stdout = [];
    const stderr = [];
    let stdoutBytes = 0;
    let stderrBytes = 0;
    let settled = false;
    const timeout = setTimeout(() => {
      child.kill("SIGTERM");
      setTimeout(() => child.kill("SIGKILL"), 5_000).unref();
    }, 15 * 60_000);
    child.stdout.on("data", (chunk) => {
      stdoutBytes += chunk.length;
      if (stdoutBytes > maximumReportBytes) {
        child.kill("SIGTERM");
        return;
      }
      stdout.push(chunk);
    });
    child.stderr.on("data", (chunk) => {
      stderrBytes += chunk.length;
      if (stderrBytes > maximumReportBytes) {
        child.kill("SIGTERM");
        return;
      }
      stderr.push(chunk);
    });
    child.once("error", () => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      rejectRun(new SafeToolError("playwright_runner_failed"));
    });
    child.once("close", (code, signal) => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      if (
        signal ||
        stdoutBytes > maximumReportBytes ||
        stderrBytes > maximumReportBytes
      ) {
        rejectRun(new SafeToolError("playwright_runner_failed"));
        return;
      }
      resolveRun({
        exitCode: code ?? 1,
        stdout: Buffer.concat(stdout, stdoutBytes),
        stderr: Buffer.concat(stderr, stderrBytes),
      });
    });
  });
}

function selectedEnvironment(additions) {
  const result = { ...additions };
  for (const name of [
    "PATH",
    "HOME",
    "XDG_CACHE_HOME",
    "XDG_CONFIG_HOME",
    "PLAYWRIGHT_BROWSERS_PATH",
  ]) {
    if (process.env[name]) result[name] = process.env[name];
  }
  return result;
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`playwright-gate: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
