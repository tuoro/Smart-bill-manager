#!/usr/bin/env node

import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  SafeToolError,
  parseStrictPairs,
  readProtectedSecret,
  requireDistinctPaths,
  requireGitSHA,
  requireImageID,
  requireSHA256,
  reserveProtectedFile,
  safeErrorCode,
} from "./lib/protected-output.mjs";
import {
  clearBuffers,
  composeEnvironment,
  containsSecret,
  inspectedImageMatches,
  runCaptured,
} from "./lib/local-release-command.mjs";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const output = await reserveProtectedFile(options.output, [
    options.masterKeySource,
    options.ownerPasswordSource,
    options.providerKeySource,
    options.postgresAdminPasswordSource,
    options.postgresMigrationPasswordSource,
    options.postgresRuntimePasswordSource,
    options.releaseArtifactsSource,
  ]);
  let masterKey;
  let ownerPassword;
  let providerKey;
  let postgresAdminPassword;
  let postgresMigrationPassword;
  let postgresRuntimePassword;
  const commandOutputs = [];
  try {
    const imageInspect = await runCaptured(
      "docker",
      ["image", "inspect", "smart-bill-manager:local"],
      {
        cwd: repositoryRoot,
        env: composeEnvironment(options),
        timeout: 10_000,
      },
    );
    commandOutputs.push(imageInspect.stdout, imageInspect.stderr);
    if (
      imageInspect.code !== 0 ||
      !inspectedImageMatches(imageInspect.stdout, options.imageID)
    ) {
      throw new SafeToolError("image_identity_invalid");
    }
    [
      masterKey,
      ownerPassword,
      providerKey,
      postgresAdminPassword,
      postgresMigrationPassword,
      postgresRuntimePassword,
    ] = await Promise.all([
      readProtectedSecret(options.masterKeySource, 128),
      readProtectedSecret(options.ownerPasswordSource, 1024),
      readProtectedSecret(options.providerKeySource, 4096),
      readProtectedSecret(options.postgresAdminPasswordSource, 1024),
      readProtectedSecret(options.postgresMigrationPasswordSource, 1024),
      readProtectedSecret(options.postgresRuntimePasswordSource, 1024),
    ]);
    const first = await runBootstrap(options);
    commandOutputs.push(first.stdout, first.stderr);
    let repeated = {
      code: -1,
      stdout: Buffer.alloc(0),
      stderr: Buffer.alloc(0),
    };
    if (first.code === 0) {
      repeated = await runBootstrap(options);
      commandOutputs.push(repeated.stdout, repeated.stderr);
    }
    if (
      containsSecret(commandOutputs, [
        masterKey,
        ownerPassword,
        providerKey,
        postgresAdminPassword,
        postgresMigrationPassword,
        postgresRuntimePassword,
      ])
    ) {
      throw new SafeToolError("secret_detected");
    }
    const report = {
      report_kind: "m4-bootstrap-owner-result",
      protocol_version: 1,
      build_identity: {
        baseline_head: options.expectedHead,
        release_input_sha256: options.expectedReleaseInput,
        image_id: options.imageID,
      },
      first_bootstrap_passed: first.code === 0,
      repeated_bootstrap_rejected: first.code === 0 && repeated.code !== 0,
      password_file_transport_only: true,
      terminal_secret_hits: 0,
      passed: first.code === 0 && repeated.code !== 0,
    };
    await output.writeJSON(report);
    process.stdout.write(
      `${JSON.stringify({ report_kind: report.report_kind, passed: report.passed })}\n`,
    );
    if (!report.passed) process.exitCode = 1;
  } finally {
    masterKey?.fill(0);
    ownerPassword?.fill(0);
    providerKey?.fill(0);
    postgresAdminPassword?.fill(0);
    postgresMigrationPassword?.fill(0);
    postgresRuntimePassword?.fill(0);
    clearBuffers(commandOutputs);
    await output.close();
  }
}

export function parseArguments(argumentsList) {
  const values = parseStrictPairs(argumentsList, [
    "project-name",
    "output",
    "master-key-source",
    "owner-password-source",
    "provider-key-source",
    "postgres-admin-password-source",
    "postgres-migration-password-source",
    "postgres-runtime-password-source",
    "release-artifacts-source",
    "exercise-id",
    "expected-head",
    "expected-release-input-sha256",
    "image-id",
  ]);
  const projectName = values.get("project-name");
  const exerciseID = values.get("exercise-id");
  if (
    !/^sbm-m4-[0-9a-f]{8}(?:-[a-z]+)?$/.test(projectName) ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      exerciseID,
    )
  ) {
    throw new SafeToolError("synthetic_identity_invalid");
  }
  const [
    masterKeySource,
    ownerPasswordSource,
    providerKeySource,
    postgresAdminPasswordSource,
    postgresMigrationPasswordSource,
    postgresRuntimePasswordSource,
  ] =
    requireDistinctPaths([
      values.get("master-key-source"),
      values.get("owner-password-source"),
      values.get("provider-key-source"),
      values.get("postgres-admin-password-source"),
      values.get("postgres-migration-password-source"),
      values.get("postgres-runtime-password-source"),
    ]);
  return {
    projectName,
    output: resolve(values.get("output")),
    masterKeySource,
    ownerPasswordSource,
    providerKeySource,
    postgresAdminPasswordSource,
    postgresMigrationPasswordSource,
    postgresRuntimePasswordSource,
    releaseArtifactsSource: resolve(values.get("release-artifacts-source")),
    exerciseID,
    expectedHead: requireGitSHA(values.get("expected-head")),
    expectedReleaseInput: requireSHA256(
      values.get("expected-release-input-sha256"),
    ),
    imageID: requireImageID(values.get("image-id")),
  };
}

async function runBootstrap(options) {
  return runCaptured("docker", bootstrapComposeArguments(options), {
    cwd: repositoryRoot,
    env: composeEnvironment(options),
    timeout: 120_000,
  });
}

export function bootstrapComposeArguments(options) {
  return [
    "compose",
    "--project-name",
    options.projectName,
    "-f",
    "infra/compose/compose.yaml",
    "-f",
    "infra/compose/compose.acceptance.yaml",
    "run",
    "--rm",
    "--no-deps",
    "--pull",
    "never",
    "app",
    "/app/bootstrap-owner",
    "-email",
    "owner@example.invalid",
    "-display-name",
    "Local Owner",
    "-tenant-name",
    "Local Acceptance",
    "-currency",
    "CNY",
    "-timezone",
    "Asia/Shanghai",
    "-password-file",
    "/run/sbm-secrets/owner-password",
  ];
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`bootstrap-owner-gate: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
