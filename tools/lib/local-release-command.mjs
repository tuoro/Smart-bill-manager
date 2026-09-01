import { execFile as execFileCallback } from "node:child_process";
import { promisify } from "node:util";

import { SafeToolError } from "./protected-output.mjs";

const execFile = promisify(execFileCallback);

export function composeEnvironment(options) {
  const result = {
    SBM_BUILD_SHA: options.expectedHead,
    SBM_RELEASE_INPUT_SHA256: options.expectedReleaseInput,
    SBM_MASTER_KEY_SOURCE: options.masterKeySource,
    SBM_POSTGRES_ADMIN_PASSWORD_SOURCE: options.postgresAdminPasswordSource,
    SBM_POSTGRES_MIGRATION_PASSWORD_SOURCE:
      options.postgresMigrationPasswordSource,
    SBM_POSTGRES_RUNTIME_PASSWORD_SOURCE:
      options.postgresRuntimePasswordSource,
    SBM_DEPLOYMENT_MODE: "local",
    SBM_COOKIE_SECURE: "false",
    SBM_BIND_ADDRESS: "127.0.0.1",
    SBM_HTTP_PORT: "8080",
    SBM_SESSION_TTL: "168h",
    SBM_AI_CONCURRENCY: "2",
    SBM_RELEASE_ARTIFACTS_SOURCE: options.releaseArtifactsSource,
    SBM_ACCEPTANCE_OWNER_PASSWORD_SOURCE: options.ownerPasswordSource,
    SBM_ACCEPTANCE_PROVIDER_KEY_SOURCE: options.providerKeySource,
    SBM_ACCEPTANCE_EXERCISE_ID: options.exerciseID,
  };
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

export async function runCaptured(
  command,
  argumentsList,
  { cwd, env, timeout = 120_000, maximumBytes = 32 * 1024 * 1024 },
) {
  try {
    const result = await execFile(command, argumentsList, {
      cwd,
      env,
      encoding: "buffer",
      timeout,
      maxBuffer: maximumBytes,
    });
    return { code: 0, stdout: result.stdout, stderr: result.stderr };
  } catch (error) {
    if (
      typeof error?.code === "number" &&
      Buffer.isBuffer(error.stdout) &&
      Buffer.isBuffer(error.stderr)
    ) {
      return { code: error.code, stdout: error.stdout, stderr: error.stderr };
    }
    throw new SafeToolError("local_command_failed");
  }
}

export function containsSecret(outputs, secrets) {
  for (const output of outputs) {
    for (const secret of secrets) {
      if (secret.length > 0 && output.includes(secret)) return true;
    }
  }
  return false;
}

export function clearBuffers(buffers) {
  for (const buffer of buffers) buffer?.fill(0);
}

export function inspectedImageMatches(content, expectedImageID) {
  try {
    const inspected = JSON.parse(content.toString("utf8"));
    return (
      Array.isArray(inspected) &&
      inspected.length === 1 &&
      inspected[0]?.Id === expectedImageID
    );
  } catch {
    return false;
  }
}

export function hasExpectedLoopbackBinding(inspected, server) {
  let parsed;
  try {
    parsed = new URL(server);
  } catch {
    return false;
  }
  if (parsed.protocol !== "http:" || parsed.hostname !== "127.0.0.1") {
    return false;
  }
  const expectedPort = parsed.port || "80";
  const portBindings = inspected?.HostConfig?.PortBindings;
  const bindings = portBindings?.["8080/tcp"];
  return (
    portBindings !== null &&
    typeof portBindings === "object" &&
    Object.keys(portBindings).length === 1 &&
    Array.isArray(bindings) &&
    bindings.length === 1 &&
    bindings[0]?.HostIp === "127.0.0.1" &&
    bindings[0]?.HostPort === expectedPort
  );
}
