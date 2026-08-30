#!/usr/bin/env node

import { readFile, stat, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const warmupJobs = 10;
const measuredJobs = 50;
const idleMilliseconds = 2_000;
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
  const password = await readProtectedSecret(options.passwordFile, 1024);
  const providerApiKey = await readProtectedSecret(
    options.providerApiKeyFile,
    4096,
  );
  const source = await readFile(options.source);
  if (source.length < 16 || source.length >= 20 * 1024 * 1024 - 8)
    throw new Error("source PNG size is invalid");
  const client = createClient(options.server);
  try {
    await assertLinuxProcess(options.pid);
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
      await completeJob(client, variant(source, index), index);
      process.stderr.write(`warmup ${index + 1}/${warmupJobs}\n`);
    }
    await delay(idleMilliseconds);

    const samples = [];
    for (let index = 0; index < measuredJobs; index += 1) {
      const sequence = warmupJobs + index;
      const job = await completeJob(
        client,
        variant(source, sequence),
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
    const rssValues = samples.map((sample) => sample.rss_bytes);
    const firstMedian = median(rssValues.slice(0, 10));
    const lastMedian = median(rssValues.slice(-10));
    const slope = linearRegressionSlope(rssValues);
    const medianPassed = lastMedian <= firstMedian * 1.2;
    const slopePassed = slope <= 0.5 * 1024 * 1024;
    const orphanPassed = orphans.length === 0;
    const report = {
      report_kind: "m1-memory-stability-result",
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
      },
      reference_deployment: {
        build_sha: options.buildSha,
        compose_version: options.composeVersion,
        compose_config_sha256: options.composeConfigSha256,
        image_id: options.imageID,
        server_cpu_limit: options.serverCPUs,
        server_memory_limit_bytes: options.serverMemoryBytes,
        database_location: options.databaseLocation,
        object_storage_location: options.objectStorageLocation,
      },
      provider: {
        base_url_host: new URL(options.providerBaseUrl).host,
        model: options.model,
        safe_fingerprint: active.safe_fingerprint,
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
    const encoded = `${JSON.stringify(report, null, 2)}\n`;
    if (encoded.includes(password) || encoded.includes(providerApiKey))
      throw new Error("refusing to write memory output containing a secret");
    await writeFile(options.output, encoded, {
      encoding: "utf8",
      flag: "wx",
      mode: 0o600,
    });
    process.stdout.write(encoded);
    if (!report.passed) process.exitCode = 1;
  } finally {
    password.fill(0);
    providerApiKey.fill(0);
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
  const confirmation = await client.mutate(
    `/reviews/${encodeURIComponent(uploaded.job_id)}/confirm`,
    {
      method: "POST",
      headers: {
        "Idempotency-Key": `memory-${String(sequence + 1).padStart(3, "0")}`,
      },
      json: { expected_revision: 1, association_mode: "no_candidate" },
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

function variant(source, index) {
  const marker = Buffer.alloc(8);
  marker.writeBigUInt64BE(BigInt(index + 1));
  return Buffer.concat([source, marker]);
}

async function assertLinuxProcess(pid) {
  if (process.platform !== "linux")
    throw new Error(
      "memory stability protocol requires Linux /proc RSS sampling",
    );
  await readRSS(pid);
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

function parseArguments(argumentsList) {
  const values = new Map();
  for (let index = 0; index < argumentsList.length; index += 2) {
    const key = argumentsList[index];
    const value = argumentsList[index + 1];
    if (!key?.startsWith("--") || value === undefined)
      throw new Error(`invalid argument near ${key ?? "<end>"}`);
    values.set(key.slice(2), value);
  }
  for (const name of [
    "server",
    "email",
    "password-file",
    "provider-base-url",
    "provider-api-key-file",
    "model",
    "pid",
    "source",
    "output",
    "build-sha",
    "compose-version",
    "compose-config-sha256",
    "image-id",
    "server-cpus",
    "server-memory-bytes",
    "database-location",
    "object-storage-location",
  ]) {
    if (!values.get(name)) throw new Error(`--${name} is required`);
  }
  const pid = Number(values.get("pid"));
  if (!Number.isSafeInteger(pid) || pid < 1)
    throw new Error("--pid must be a positive process ID");
  const serverCPUs = Number(values.get("server-cpus"));
  const serverMemoryBytes = Number(values.get("server-memory-bytes"));
  if (!Number.isFinite(serverCPUs) || serverCPUs <= 0)
    throw new Error("--server-cpus must be a positive number");
  if (!Number.isSafeInteger(serverMemoryBytes) || serverMemoryBytes < 1)
    throw new Error("--server-memory-bytes must be a positive safe integer");
  return {
    server: values.get("server"),
    email: values.get("email"),
    passwordFile: resolve(values.get("password-file")),
    providerBaseUrl: values.get("provider-base-url"),
    providerApiKeyFile: resolve(values.get("provider-api-key-file")),
    model: values.get("model"),
    pid,
    source: resolve(values.get("source")),
    output: resolve(values.get("output")),
    buildSha: values.get("build-sha"),
    composeVersion: values.get("compose-version"),
    composeConfigSha256: values.get("compose-config-sha256"),
    imageID: values.get("image-id"),
    serverCPUs,
    serverMemoryBytes,
    databaseLocation: values.get("database-location"),
    objectStorageLocation: values.get("object-storage-location"),
  };
}

async function readProtectedSecret(path, maximumBytes) {
  const information = await stat(path);
  if (!information.isFile() || (information.mode & 0o077) !== 0)
    throw new Error(`secret file must be regular and owner-only: ${path}`);
  const content = await readFile(path);
  const end =
    content.at(-1) === 0x0a
      ? content.length - (content.at(-2) === 0x0d ? 2 : 1)
      : content.length;
  const result = Buffer.from(content.subarray(0, end));
  if (result.length < 1 || result.length > maximumBytes)
    throw new Error(`secret file size is invalid: ${path}`);
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

main().catch((error) => {
  process.stderr.write(
    `${error instanceof Error ? error.message : String(error)}\n`,
  );
  process.exitCode = 1;
});
