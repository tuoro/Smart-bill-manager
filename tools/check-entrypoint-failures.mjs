#!/usr/bin/env node

import { chmod, link, mkdir, rm, symlink, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

import {
  SafeToolError,
  parseStrictPairs,
  requireGitSHA,
  requireImageID,
  requireSHA256,
  reserveProtectedDirectory,
  reserveProtectedFile,
  safeErrorCode,
} from "./lib/protected-output.mjs";
import {
  clearBuffers,
  containsSecret,
  inspectedImageMatches,
  runCaptured,
} from "./lib/local-release-command.mjs";

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const output = await reserveProtectedFile(options.output, [
    options.workspace,
  ]);
  let workspaceCreated = false;
  const commandOutputs = [];
  try {
    await reserveProtectedDirectory(options.workspace, [options.output]);
    workspaceCreated = true;
    const imageInspect = await runCaptured(
      "docker",
      ["image", "inspect", options.image],
      {
        cwd: "/tmp",
        env: selectedDockerEnvironment(),
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
    const cases = await createCases(options.workspace);
    const results = {};
    for (const entry of cases) {
      const result = await runCase(options.image, entry.directory);
      commandOutputs.push(result.stdout, result.stderr);
      if (containsSecret([result.stdout, result.stderr], entry.scanValues)) {
        throw new SafeToolError("secret_detected");
      }
      results[entry.name] = entry.expectedSuccess
        ? result.code === 0
        : result.code !== 0;
    }
    const report = {
      report_kind: "m4-entrypoint-failure-boundary-result",
      protocol_version: 1,
      build_identity: {
        baseline_head: options.expectedHead,
        release_input_sha256: options.expectedReleaseInput,
        image_id: options.imageID,
      },
      cases: results,
      passed: Object.values(results).every(Boolean),
    };
    await output.writeJSON(report);
    process.stdout.write(
      `${JSON.stringify({ report_kind: report.report_kind, case_count: Object.keys(results).length, passed: report.passed })}\n`,
    );
    if (!report.passed) process.exitCode = 1;
  } finally {
    clearBuffers(commandOutputs);
    if (workspaceCreated) {
      await rm(options.workspace, { recursive: true, force: true });
    }
    await output.close();
  }
}

export function parseArguments(argumentsList) {
  const values = parseStrictPairs(argumentsList, [
    "output",
    "workspace",
    "image",
    "image-id",
    "expected-head",
    "expected-release-input-sha256",
  ]);
  if (values.get("image") !== "smart-bill-manager:local") {
    throw new SafeToolError("image_identity_invalid");
  }
  const output = resolve(values.get("output"));
  const workspace = resolve(values.get("workspace"));
  if (dirname(output) !== dirname(workspace) || output === workspace) {
    throw new SafeToolError("path_conflict");
  }
  return {
    output,
    workspace,
    image: values.get("image"),
    imageID: requireImageID(values.get("image-id")),
    expectedHead: requireGitSHA(values.get("expected-head")),
    expectedReleaseInput: requireSHA256(
      values.get("expected-release-input-sha256"),
    ),
  };
}

async function createCases(workspace) {
  const valid = Buffer.from("0123456789abcdef0123456789abcdef", "utf8");
  const databasePassword = Buffer.from(
    "synthetic-entrypoint-runtime-password",
    "utf8",
  );
  const entries = [];
  const addDatabasePassword = async (directory) => {
    await writeFile(
      join(directory, "sbm_postgres_runtime_password"),
      databasePassword,
      { flag: "wx", mode: 0o600 },
    );
  };
  const addFileCase = async (name, content, mode, expectedSuccess = false) => {
    const directory = join(workspace, name);
    await mkdir(directory, { mode: 0o700 });
    await addDatabasePassword(directory);
    await writeFile(join(directory, "sbm_master_key"), content, {
      flag: "wx",
      mode,
    });
    await chmod(join(directory, "sbm_master_key"), mode);
    entries.push({
      name,
      directory,
      expectedSuccess,
      scanValues: [content, databasePassword],
    });
  };
  await addFileCase("valid_raw", valid, 0o600, true);
  await addFileCase("empty", Buffer.alloc(0), 0o600);
  await addFileCase("too_large", Buffer.alloc(129, 0x61), 0o600);
  await addFileCase("invalid_format", Buffer.from("not-a-key", "utf8"), 0o600);
  await addFileCase("broad_permissions", valid, 0o644);

  const symlinkDirectory = join(workspace, "symbolic_link");
  await mkdir(symlinkDirectory, { mode: 0o700 });
  await addDatabasePassword(symlinkDirectory);
  await writeFile(join(symlinkDirectory, "target"), valid, {
    flag: "wx",
    mode: 0o600,
  });
  await symlink("target", join(symlinkDirectory, "sbm_master_key"));
  entries.push({
    name: "symbolic_link",
    directory: symlinkDirectory,
    expectedSuccess: false,
    scanValues: [valid, databasePassword],
  });

  const hardlinkDirectory = join(workspace, "multiple_hardlinks");
  await mkdir(hardlinkDirectory, { mode: 0o700 });
  await addDatabasePassword(hardlinkDirectory);
  await writeFile(join(hardlinkDirectory, "sbm_master_key"), valid, {
    flag: "wx",
    mode: 0o600,
  });
  await link(
    join(hardlinkDirectory, "sbm_master_key"),
    join(hardlinkDirectory, "alias"),
  );
  entries.push({
    name: "multiple_hardlinks",
    directory: hardlinkDirectory,
    expectedSuccess: false,
    scanValues: [valid, databasePassword],
  });
  return entries;
}

async function runCase(image, secretDirectory) {
  return runCaptured(
    "docker",
    [
      "run",
      "--pull",
      "never",
      "--rm",
      "--network",
      "none",
      "--memory",
      "512m",
      "--cpus",
      "1",
      "--pids-limit",
      "128",
      "--read-only",
      "--cap-drop",
      "ALL",
      "--cap-add",
      "CHOWN",
      "--cap-add",
      "DAC_OVERRIDE",
      "--cap-add",
      "SETGID",
      "--cap-add",
      "SETUID",
      "--tmpfs",
      "/tmp:rw,noexec,nosuid,nodev,size=16777216",
      "--tmpfs",
      "/run/sbm-secrets:rw,noexec,nosuid,nodev,size=65536,mode=0700",
      "--mount",
      `type=bind,src=${secretDirectory},dst=/run/secrets,readonly`,
      "--entrypoint",
      "/usr/local/bin/sbm-entrypoint",
      image,
      "/bin/sh",
      "-c",
      "set -eu; test -r /run/sbm-secrets/master-key; test -r /run/sbm-secrets/postgres-runtime-password; test ! -w /run/sbm-secrets; test \"$(stat -c '%a:%u:%g' /run/sbm-secrets)\" = '710:0:10001'; test \"$(stat -c '%h:%a:%u:%g' /run/sbm-secrets/master-key)\" = '1:600:10001:10001'; test \"$(stat -c '%h:%a:%u:%g' /run/sbm-secrets/postgres-runtime-password)\" = '1:600:10001:10001'",
    ],
    {
      cwd: "/tmp",
      env: selectedDockerEnvironment(),
      timeout: 60_000,
    },
  );
}

function selectedDockerEnvironment() {
  const result = {};
  for (const name of [
    "PATH",
    "HOME",
    "DOCKER_HOST",
    "DOCKER_CONTEXT",
    "DOCKER_CONFIG",
    "XDG_CONFIG_HOME",
  ]) {
    if (process.env[name]) result[name] = process.env[name];
  }
  return result;
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`entrypoint-boundary: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
