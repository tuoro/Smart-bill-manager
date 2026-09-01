#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { deflateSync } from "node:zlib";

import {
  SafeToolError,
  parseStrictPairs,
  readProtectedSecret,
  requireDistinctPaths,
  requireGitSHA,
  requireImageID,
  requireLoopbackURL,
  requireSHA256,
  reserveProtectedFile,
  safeErrorCode,
} from "./lib/protected-output.mjs";
import {
  clearBuffers,
  hasExpectedLoopbackBinding,
  runCaptured,
} from "./lib/local-release-command.mjs";

const warmupJobs = 10;
const measuredJobs = 50;
const idleMilliseconds = 2_000;
const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const syntheticSource = resolve(
  repositoryRoot,
  "tests/evaluation/assets/m1-synthetic-v1/pay-001.png",
);
const terminalStates = new Set([
  "completed",
  "failed",
  "cancelled",
  "rejected",
]);

class ApiFailure extends Error {
  constructor(status, body) {
    super(body?.error?.message ?? `HTTP ${status}`);
    this.status = status;
    this.code = body?.error?.code ?? "unknown_error";
  }
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const output = await reserveProtectedFile(options.output, [
    options.passwordFile,
    options.providerApiKeyFile,
    options.source,
  ]);
  let password;
  let providerApiKey;
  try {
    password = await readProtectedSecret(options.passwordFile, 1024);
    providerApiKey = await readProtectedSecret(
      options.providerApiKeyFile,
      4096,
    );
    const source = await readFile(options.source);
    if (source.length < 16 || source.length >= 20 * 1024 * 1024 - 8) {
      throw new SafeToolError("source_input_invalid");
    }
    const client = createClient(options.server);
    await assertLinuxProcess(options.pid);
    await assertReleaseContainer(options);
    await client.login(options.email, password);
    const initialJobs = await client.get("/jobs");
    if (initialJobs.items.length !== 0)
      throw new Error("memory test requires a fresh workspace with zero jobs");
    const provider = await client.createProvider(
      options.providerBaseUrl,
      providerApiKey,
      options.model,
    );
    const detected = await client.mutate(
      `/provider-configs/${encodeURIComponent(provider.id)}/detect`,
      { method: "POST", timeoutMs: 90_000 },
    );
    if (detected.capability_status !== "passed")
      throw new Error("synthetic provider did not pass capability detection");
    const active = await client.mutate(
      `/provider-configs/${encodeURIComponent(provider.id)}/activate`,
      { method: "POST" },
    );
    if (active.active !== true)
      throw new Error("synthetic provider was not activated");

    const startedAt = new Date().toISOString();
    for (let index = 0; index < warmupJobs; index += 1) {
      await completeJob(client, memoryImageVariant(source, index), index);
      process.stderr.write(`warmup ${index + 1}/${warmupJobs}\n`);
    }
    await delay(idleMilliseconds);

    const samples = [];
    for (let index = 0; index < measuredJobs; index += 1) {
      const sequence = warmupJobs + index;
      const job = await completeJob(
        client,
        memoryImageVariant(source, sequence),
        sequence,
      );
      await delay(idleMilliseconds);
      const rssBytes = await readRSS(options.pid);
      samples.push({
        sequence: index + 1,
        job_id: job.id,
        rss_bytes: rssBytes,
      });
      process.stderr.write(
        `measure ${index + 1}/${measuredJobs}: ${(rssBytes / 1024 / 1024).toFixed(2)} MiB\n`,
      );
    }

    const jobs = await client.get("/jobs");
    const orphans = jobs.items.filter(
      (job) => job.status === "processing" || job.status === "cancel_requested",
    );
    if (jobs.items.length !== warmupJobs + measuredJobs) {
      throw new Error(
        `job count = ${jobs.items.length}, want ${warmupJobs + measuredJobs}`,
      );
    }
    const providerMetrics = await readProviderMetrics(options);
    const rssValues = samples.map((sample) => sample.rss_bytes);
    const firstMedian = median(rssValues.slice(0, 10));
    const lastMedian = median(rssValues.slice(-10));
    const slope = linearRegressionSlope(rssValues);
    const medianPassed = lastMedian <= firstMedian * 1.2;
    const slopePassed = slope <= 0.5 * 1024 * 1024;
    const orphanPassed = orphans.length === 0;
    const report = {
      report_kind: "m4-memory-stability-result",
      started_at: startedAt,
      finished_at: new Date().toISOString(),
      protocol: {
        warmup_jobs: warmupJobs,
        measured_jobs: measuredJobs,
        idle_after_terminal_ms: idleMilliseconds,
        first_and_last_window: 10,
        maximum_last_to_first_median_ratio: 1.2,
        maximum_slope_mib_per_job: 0.5,
      },
      environment: {
        platform: process.platform,
        architecture: process.arch,
        api_pid: options.pid,
        process_identity_passed: true,
      },
      reference_deployment: {
        build_sha: options.buildSha,
        release_input_sha256: options.releaseInputSha256,
        compose_version: options.composeVersion,
        compose_config_sha256: options.composeConfigSha256,
        image_id: options.imageID,
        container_identity_passed: true,
        server_cpu_limit: options.serverCPUs,
        server_memory_limit_bytes: options.serverMemoryBytes,
        database_location: options.databaseLocation,
        object_storage_location: options.objectStorageLocation,
      },
      provider: {
        base_url_host: new URL(options.providerBaseUrl).host,
        model: options.model,
        safe_fingerprint: active.safe_fingerprint,
        capability_probes: providerMetrics.probes,
        extractions: providerMetrics.extractions,
        probe_latency_ms: providerMetrics.probeLatency,
        extraction_latency_ms: providerMetrics.extractionLatency,
      },
      result: {
        first_10_median_rss_bytes: firstMedian,
        last_10_median_rss_bytes: lastMedian,
        last_to_first_median_ratio: round(lastMedian / firstMedian),
        linear_regression_slope_bytes_per_job: round(slope),
        linear_regression_slope_mib_per_job: round(slope / 1024 / 1024),
        orphan_processing_or_cancel_requested_jobs: orphans.map(
          (job) => job.id,
        ),
        median_gate_passed: medianPassed,
        slope_gate_passed: slopePassed,
        orphan_gate_passed: orphanPassed,
      },
      samples,
      passed: medianPassed && slopePassed && orphanPassed,
    };
    const encoded = JSON.stringify(report);
    if (
      encoded.includes(password.toString("utf8")) ||
      encoded.includes(providerApiKey.toString("utf8"))
    ) {
      throw new SafeToolError("secret_detected");
    }
    await output.writeJSON(report);
    process.stdout.write(
      `${JSON.stringify({
        report_kind: report.report_kind,
        measured_jobs: measuredJobs,
        last_to_first_median_ratio: report.result.last_to_first_median_ratio,
        slope_mib_per_job: report.result.linear_regression_slope_mib_per_job,
        provider_extraction_p95_ms: report.provider.extraction_latency_ms.p95,
        orphan_job_count: orphans.length,
        passed: report.passed,
      })}\n`,
    );
    if (!report.passed) process.exitCode = 1;
  } finally {
    password?.fill(0);
    providerApiKey?.fill(0);
    await output.close();
  }
}

async function completeJob(client, content, sequence) {
  const uploaded = await client.upload(
    `memory-${String(sequence + 1).padStart(3, "0")}.png`,
    content,
  );
  const reviewable = await waitForState(
    client,
    uploaded.job_id,
    new Set(["needs_review", "blocked", ...terminalStates]),
  );
  if (reviewable.status !== "needs_review")
    throw new Error(`job ${uploaded.job_id} stopped at ${reviewable.status}`);
  const review = await client.get(
    `/reviews/${encodeURIComponent(uploaded.job_id)}`,
  );
  const confirmation = await client.mutate(
    `/reviews/${encodeURIComponent(uploaded.job_id)}/confirm`,
    {
      method: "POST",
      headers: {
        "Idempotency-Key": `memory-${String(sequence + 1).padStart(3, "0")}`,
      },
      json: memoryConfirmationRequest(review),
    },
  );
  if (confirmation.replayed !== false || confirmation.fact_type !== "payment") {
    throw new Error(
      `job ${uploaded.job_id} did not produce a first payment confirmation`,
    );
  }
  const completed = await waitForState(client, uploaded.job_id, terminalStates);
  if (completed.status !== "completed")
    throw new Error(
      `job ${uploaded.job_id} terminal state = ${completed.status}`,
    );
  return completed;
}

export function memoryConfirmationRequest(review) {
  if (
    !Number.isSafeInteger(review?.revision) ||
    review.revision < 1 ||
    !Array.isArray(review.duplicate_candidates) ||
    review.duplicate_candidates.some(
      (candidate) =>
        typeof candidate?.id !== "string" || candidate.id.length === 0,
    )
  ) {
    throw new SafeToolError("review_contract_invalid");
  }
  return {
    expected_revision: review.revision,
    association_mode: "no_candidate",
    allocations: [],
    duplicate_resolutions: review.duplicate_candidates.map((candidate) => ({
      candidate_id: candidate.id,
      action: "keep_distinct",
    })),
  };
}

async function waitForState(client, jobID, acceptedStates) {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const job = await client.get(`/jobs/${encodeURIComponent(jobID)}`);
    if (acceptedStates.has(job.status)) return job;
    await delay(100);
  }
  throw new Error(
    `job ${jobID} did not reach the required state within 30 seconds`,
  );
}

async function readProviderMetrics(options) {
  const result = await runCaptured(
    "docker",
    [
      "exec",
      "--user",
      "10001:10001",
      options.containerID,
      "wget",
      "-q",
      "-O",
      "-",
      "http://127.0.0.1:19086/metrics",
    ],
    {
      cwd: repositoryRoot,
      env: selectedDockerEnvironment(),
      timeout: 10_000,
      maximumBytes: 64 * 1024,
    },
  );
  let body;
  try {
    if (result.code !== 0) {
      throw new SafeToolError("provider_metrics_invalid");
    }
    body = JSON.parse(result.stdout.toString("utf8"));
  } catch (error) {
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError("provider_metrics_invalid");
  } finally {
    clearBuffers([result.stdout, result.stderr]);
  }
  if (
    body?.kind !== "smart-bill-manager-synthetic-provider-metrics" ||
    body?.version !== 1 ||
    body?.model !== options.model ||
    body?.mode !== "normal" ||
    body?.exercise_id !== options.exerciseID ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      body?.instance_id ?? "",
    ) ||
    body?.requests !== warmupJobs + measuredJobs + 1 ||
    body?.probes !== 1 ||
    body?.extractions !== warmupJobs + measuredJobs ||
    !validLatencySummary(body?.probe_latency_ms, 1) ||
    !validLatencySummary(body?.extraction_latency_ms, warmupJobs + measuredJobs)
  ) {
    throw new SafeToolError("provider_metrics_invalid");
  }
  return {
    probes: body.probes,
    extractions: body.extractions,
    probeLatency: body.probe_latency_ms,
    extractionLatency: body.extraction_latency_ms,
  };
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

function createClient(server) {
  const base = new URL("/api/v1/", ensureTrailingSlash(server))
    .toString()
    .replace(/\/$/, "");
  let cookie = "";
  let csrf = "";
  async function request(path, options = {}) {
    const headers = new Headers(options.headers);
    if (cookie) headers.set("Cookie", cookie);
    if (options.csrf) headers.set("X-CSRF-Token", csrf);
    if (options.json !== undefined)
      headers.set("Content-Type", "application/json");
    const response = await fetch(base + path, {
      method: options.method ?? "GET",
      headers,
      body:
        options.json === undefined
          ? options.body
          : JSON.stringify(options.json),
      signal: AbortSignal.timeout(options.timeoutMs ?? 30_000),
      redirect: "error",
    });
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new ApiFailure(response.status, body);
    }
    return response.status === 204 ? undefined : response.json();
  }
  return {
    async login(email, passwordBytes) {
      const response = await fetch(base + "/session/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email,
          password: passwordBytes.toString("utf8"),
        }),
        signal: AbortSignal.timeout(30_000),
        redirect: "error",
      });
      if (!response.ok)
        throw new ApiFailure(
          response.status,
          await response.json().catch(() => ({})),
        );
      cookie = response.headers
        .getSetCookie()
        .map((entry) => entry.split(";", 1)[0])
        .join("; ");
      const body = await response.json();
      csrf = body.csrf_token;
      if (!cookie || !csrf)
        throw new Error("login did not establish session and CSRF state");
    },
    get(path) {
      return request(path);
    },
    mutate(path, options = {}) {
      return request(path, { ...options, csrf: true });
    },
    createProvider(providerBaseUrl, apiKeyBytes, model) {
      return request("/provider-configs", {
        method: "POST",
        csrf: true,
        json: {
          base_url: providerBaseUrl,
          api_key: apiKeyBytes.toString("utf8"),
          model,
          output_mode: "json_schema",
        },
      });
    },
    upload(name, content) {
      const form = new FormData();
      form.append("file", new Blob([content], { type: "image/png" }), name);
      return request("/documents", {
        method: "POST",
        csrf: true,
        body: form,
        timeoutMs: 60_000,
      });
    },
  };
}

export function memoryImageVariant(source, index) {
  if (!Buffer.isBuffer(source) || source.length < 16 || !Number.isSafeInteger(index) || index < 0) {
    throw new SafeToolError("source_input_invalid");
  }
  const width = 256;
  const height = 256;
  const blockSize = 32;
  const pattern = createHash("sha256")
    .update(source)
    .update(Buffer.from(`memory-visual-${index}`, "utf8"))
    .digest()
    .subarray(0, 8);
  const scanlines = Buffer.alloc(height * (1 + width * 3));
  for (let y = 0; y < height; y += 1) {
    const row = y * (1 + width * 3);
    scanlines[row] = 0;
    for (let x = 0; x < width; x += 1) {
      const bitIndex = Math.floor(y / blockSize) * 8 + Math.floor(x / blockSize);
      const enabled = (pattern[Math.floor(bitIndex / 8)] >> (7 - (bitIndex % 8))) & 1;
      const value = enabled ? 245 : 10;
      const offset = row + 1 + x * 3;
      scanlines[offset] = value;
      scanlines[offset + 1] = value;
      scanlines[offset + 2] = value;
    }
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 2;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    pngChunk("IHDR", header),
    pngChunk("IDAT", deflateSync(scanlines, { level: 9 })),
    pngChunk("IEND", Buffer.alloc(0)),
  ]);
}

function pngChunk(type, data) {
  const name = Buffer.from(type, "ascii");
  const body = Buffer.concat([name, data]);
  const result = Buffer.alloc(12 + data.length);
  result.writeUInt32BE(data.length, 0);
  body.copy(result, 4);
  result.writeUInt32BE(crc32(body), 8 + data.length);
  return result;
}

function crc32(content) {
  let checksum = 0xffffffff;
  for (const value of content) {
    checksum ^= value;
    for (let bit = 0; bit < 8; bit += 1) {
      checksum = (checksum >>> 1) ^ (checksum & 1 ? 0xedb88320 : 0);
    }
  }
  return (checksum ^ 0xffffffff) >>> 0;
}

async function assertLinuxProcess(pid) {
  if (process.platform !== "linux")
    throw new Error(
      "memory stability protocol requires Linux /proc RSS sampling",
    );
  const [status, commandLine] = await Promise.all([
    readFile(`/proc/${pid}/status`, "utf8"),
    readFile(`/proc/${pid}/cmdline`),
  ]);
  if (!isExpectedServerProcess(status, commandLine)) {
    throw new SafeToolError("process_identity_invalid");
  }
  await readRSS(pid);
}

async function assertReleaseContainer(options) {
  const inspectResult = await runCaptured("docker", ["inspect", options.containerID], {
    cwd: repositoryRoot,
    env: selectedDockerEnvironment(),
    timeout: 10_000,
    maximumBytes: 1024 * 1024,
  });
  let topResult;
  let namespaceResult;
  try {
    if (inspectResult.code !== 0) {
      throw new SafeToolError("process_identity_invalid");
    }
    const inspected = JSON.parse(inspectResult.stdout.toString("utf8"));
    if (!Array.isArray(inspected) || inspected.length !== 1) {
      throw new SafeToolError("process_identity_invalid");
    }
    const [container] = inspected;
    if (!isExpectedReleaseContainer(container, options)) {
      throw new SafeToolError("process_identity_invalid");
    }
    topResult = await runCaptured(
      "docker",
      ["top", options.containerID, "-eo", "pid,comm,args"],
      {
        cwd: repositoryRoot,
        env: selectedDockerEnvironment(),
        timeout: 10_000,
        maximumBytes: 64 * 1024,
      },
    );
    if (
      topResult.code !== 0 ||
      !topContainsExactServerPID(topResult.stdout.toString("utf8"), options.pid)
    ) {
      throw new SafeToolError("process_identity_invalid");
    }
    namespaceResult = await runCaptured(
      "docker",
      [
        "exec",
        "--user",
        "10001:10001",
        options.containerID,
        "/bin/sh",
        "-c",
        "set -eu; server_pid=''; for status_file in /proc/[0-9]*/status; do if grep -q '^Name:[[:space:]]*server$' \"$status_file\"; then server_pid=${status_file#/proc/}; server_pid=${server_pid%/status}; break; fi; done; test -n \"$server_pid\"; grep -Eq '^Uid:[[:space:]]+10001[[:space:]]+10001[[:space:]]+10001[[:space:]]+10001$' \"/proc/$server_pid/status\"; grep -Eq '^Gid:[[:space:]]+10001[[:space:]]+10001[[:space:]]+10001[[:space:]]+10001$' \"/proc/$server_pid/status\"; test \"$(tr '\\000' '\\n' < \"/proc/$server_pid/cmdline\")\" = '/app/server'; test \"$(readlink \"/proc/$server_pid/ns/pid\")\" = \"$(readlink /proc/self/ns/pid)\"",
      ],
      {
        cwd: repositoryRoot,
        env: selectedDockerEnvironment(),
        timeout: 10_000,
        maximumBytes: 64 * 1024,
      },
    );
    if (namespaceResult.code !== 0) {
      throw new SafeToolError("process_identity_invalid");
    }
  } catch (error) {
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError("process_identity_invalid");
  } finally {
    clearBuffers([
      inspectResult.stdout,
      inspectResult.stderr,
      topResult?.stdout,
      topResult?.stderr,
      namespaceResult?.stdout,
      namespaceResult?.stderr,
    ]);
  }
}

export function isExpectedReleaseContainer(inspected, options) {
  return (
    inspected?.Id === options.containerID &&
    inspected?.Image === options.imageID &&
    inspected?.Config?.Image === "smart-bill-manager:local" &&
    inspected?.State?.Running === true &&
    Number.isSafeInteger(inspected?.State?.Pid) &&
    inspected.State.Pid > 0 &&
    hasExpectedLoopbackBinding(inspected, options.server)
  );
}

export function topContainsExactServerPID(content, expectedPID) {
  const matches = content
    .split("\n")
    .slice(1)
    .map((line) => /^\s*(\d+)\s+(\S+)\s+(.+?)\s*$/.exec(line))
    .filter(
      (match) =>
        match && match[2] === "server" && match[3] === "/app/server",
    );
  return matches.length === 1 && Number(matches[0][1]) === expectedPID;
}

export function isExpectedServerProcess(status, commandLine) {
  const argumentsList = commandLine
    .toString("utf8")
    .split("\0")
    .filter(Boolean);
  return (
    /^Name:\s+server$/m.test(status) &&
    /^Uid:\s+10001\s+10001\s+10001\s+10001$/m.test(status) &&
    /^Gid:\s+10001\s+10001\s+10001\s+10001$/m.test(status) &&
    argumentsList[0] === "/app/server"
  );
}

async function readRSS(pid) {
  const status = await readFile(`/proc/${pid}/status`, "utf8");
  const match = /^VmRSS:\s+(\d+)\s+kB$/m.exec(status);
  if (!match) throw new Error(`VmRSS is unavailable for PID ${pid}`);
  return Number(match[1]) * 1024;
}

function linearRegressionSlope(values) {
  const xMean = (values.length + 1) / 2;
  const yMean = values.reduce((sum, value) => sum + value, 0) / values.length;
  let numerator = 0;
  let denominator = 0;
  for (let index = 0; index < values.length; index += 1) {
    const xDelta = index + 1 - xMean;
    numerator += xDelta * (values[index] - yMean);
    denominator += xDelta * xDelta;
  }
  return numerator / denominator;
}

function median(values) {
  const sorted = [...values].sort((left, right) => left - right);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 0
    ? (sorted[middle - 1] + sorted[middle]) / 2
    : sorted[middle];
}

export function parseArguments(argumentsList) {
  const required = [
    "server",
    "email",
    "password-file",
    "provider-base-url",
    "provider-api-key-file",
    "model",
    "exercise-id",
    "pid",
    "container-id",
    "source",
    "output",
    "build-sha",
    "release-input-sha256",
    "compose-version",
    "compose-config-sha256",
    "image-id",
    "server-cpus",
    "server-memory-bytes",
    "database-location",
    "object-storage-location",
  ];
  const values = parseStrictPairs(argumentsList, required);
  requireLoopbackURL(values.get("server"), { allowPath: false });
  const providerURL = requireLoopbackURL(values.get("provider-base-url"));
  if (
    providerURL.origin !== "http://127.0.0.1:19086" ||
    !/^\/v1\/?$/.test(providerURL.pathname) ||
    providerURL.search
  ) {
    throw new SafeToolError("loopback_url_required");
  }
  if (
    !/^[^\s@]+@[^\s@]+$/.test(values.get("email")) ||
    !/^synthetic-[a-z0-9._-]+$/.test(values.get("model")) ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      values.get("exercise-id"),
    ) ||
    resolve(values.get("source")) !== syntheticSource
  ) {
    throw new SafeToolError("synthetic_identity_invalid");
  }
  const pid = Number(values.get("pid"));
  if (!Number.isSafeInteger(pid) || pid < 1)
    throw new SafeToolError("process_identity_invalid");
  const containerID = values.get("container-id");
  if (!/^[0-9a-f]{64}$/.test(containerID)) {
    throw new SafeToolError("process_identity_invalid");
  }
  const serverCPUs = Number(values.get("server-cpus"));
  const serverMemoryBytes = Number(values.get("server-memory-bytes"));
  if (serverCPUs !== 2 || serverMemoryBytes !== 3584 * 1024 * 1024) {
    throw new SafeToolError("reference_limits_invalid");
  }
  if (
    values.get("database-location") !== "named-volume:sbm_postgres_data" ||
    values.get("object-storage-location") !== "named-volume:sbm_objects" ||
    !/^v?\d+\.\d+(?:\.\d+)?(?:[-+][a-z0-9.-]+)?$/i.test(
      values.get("compose-version"),
    )
  ) {
    throw new SafeToolError("reference_identity_invalid");
  }
  const [passwordFile, providerApiKeyFile] = requireDistinctPaths([
    values.get("password-file"),
    values.get("provider-api-key-file"),
  ]);
  return {
    server: values.get("server"),
    email: values.get("email"),
    passwordFile,
    providerBaseUrl: values.get("provider-base-url"),
    providerApiKeyFile,
    model: values.get("model"),
    exerciseID: values.get("exercise-id"),
    pid,
    containerID,
    source: syntheticSource,
    output: resolve(values.get("output")),
    buildSha: requireGitSHA(values.get("build-sha")),
    releaseInputSha256: requireSHA256(values.get("release-input-sha256")),
    composeVersion: values.get("compose-version"),
    composeConfigSha256: requireSHA256(values.get("compose-config-sha256")),
    imageID: requireImageID(values.get("image-id")),
    serverCPUs,
    serverMemoryBytes,
    databaseLocation: values.get("database-location"),
    objectStorageLocation: values.get("object-storage-location"),
  };
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

function ensureTrailingSlash(value) {
  return value.endsWith("/") ? value : `${value}/`;
}
function delay(milliseconds) {
  return new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));
}
function round(value) {
  return Math.round(value * 10_000) / 10_000;
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`memory-stability: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
