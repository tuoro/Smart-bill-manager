#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import { setTimeout as delay } from "node:timers/promises";
import { cpus, freemem, platform, totalmem } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  SafeToolError,
  parseStrictPairs,
  readProtectedJSON,
  readProtectedSecret,
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

const projectDirectory = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sourcePNG = resolve(
  projectDirectory,
  "tests/evaluation/assets/m1-synthetic-v1/pay-001.png",
);

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
    options.seedManifest,
    sourcePNG,
  ]);
  let password;
  try {
    await assertPerformanceContainer(options);
    const seed = await readProtectedJSON(options.seedManifest, 1024 * 1024);
    validateSeed(seed);
    password = await readProtectedSecret(options.passwordFile, 1024);
    const client = createClient(options.server);
    await client.login(options.email, password);
    const endpoints = {
      inbox_list: "/jobs",
      document_detail: `/documents/${encodeURIComponent(seed.representative_document_id)}`,
      claim_set_detail: `/claim-sets/${encodeURIComponent(seed.representative_claim_set_id)}`,
      payment_list: "/payments?limit=100",
      invoice_list: "/invoices?limit=100",
      fact_insights: "/insights?limit=100",
    };
    const apiResults = {};
    for (const [name, path] of Object.entries(endpoints)) {
      await runConcurrent(100, 1, async () => client.consume(path));
      const samples = await measureConcurrent(1_000, 10, async () =>
        client.consume(path),
      );
      apiResults[name] = summarize(samples, 300, {
        warmups: 100,
        requests: 1_000,
        concurrency: 10,
      });
      process.stderr.write(`${name}: p95=${apiResults[name].p95_ms} ms\n`);
    }

    const uploadVariants = await buildUploadVariants();
    await uploadBatch(client, uploadVariants, 20, false);
    const uploadSamples = await uploadBatch(client, uploadVariants, 200, true);
    const uploadResult = summarize(uploadSamples, 500, {
      warmups: 20,
      requests: 200,
      concurrency: 2,
    });
    process.stderr.write(`document_create: p95=${uploadResult.p95_ms} ms\n`);

    const warmJobs = seed.confirmation_job_ids.slice(0, 20);
    const measuredJobs = seed.confirmation_job_ids.slice(20);
    if (warmJobs.length !== 20 || measuredJobs.length !== 200) {
      throw new Error("seed manifest must contain 220 confirmation jobs");
    }
    await confirmBatch(client, warmJobs, false);
    const confirmSamples = await confirmBatch(client, measuredJobs, true);
    const confirmResult = summarize(confirmSamples, 500, {
      warmups: 20,
      requests: 200,
      concurrency: 2,
    });
    process.stderr.write(`review_confirm: p95=${confirmResult.p95_ms} ms\n`);

    const failed = [
      ...Object.entries(apiResults)
        .filter(([, value]) => !value.passed)
        .map(([name]) => name),
      ...(uploadResult.passed ? [] : ["document_create"]),
      ...(confirmResult.passed ? [] : ["review_confirm"]),
    ];
    const report = {
      report_kind: "m4-performance-result",
      measured_at: new Date().toISOString(),
      seed_kind: seed.seed_kind,
      data_shape: {
        payments_before_confirmation: seed.payments,
        invoices_before_confirmation: seed.invoices,
        source_claim_chains: seed.source_claim_chains,
      },
      environment: {
        platform: platform(),
        architecture: process.arch,
        logical_cpu_count_visible: cpus().length,
        total_memory_bytes_visible: totalmem(),
        free_memory_bytes_at_report: freemem(),
        node: process.version,
        server: new URL(options.server).origin,
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
        provider_latency: options.providerLatencyNote,
      },
      non_ai_json_api: apiResults,
      document_create_server_timing: uploadResult,
      review_confirm_server_timing: confirmResult,
      passed: failed.length === 0,
      failed_gates: failed,
    };
    const encoded = JSON.stringify(report);
    if (encoded.includes(password.toString("utf8"))) {
      throw new SafeToolError("secret_detected");
    }
    await output.writeJSON(report);
    process.stdout.write(
      `${JSON.stringify({
        report_kind: report.report_kind,
        non_ai_p95_ms: Object.fromEntries(
          Object.entries(apiResults).map(([name, result]) => [
            name,
            result.p95_ms,
          ]),
        ),
        document_create_p95_ms: uploadResult.p95_ms,
        review_confirm_p95_ms: confirmResult.p95_ms,
        passed: report.passed,
      })}\n`,
    );
    if (failed.length !== 0) process.exitCode = 1;
  } finally {
    password?.fill(0);
    await output.close();
  }
}

async function uploadBatch(client, variants, count, measured) {
  const samples = [];
  const workers = variants.map(async (content, workerIndex) => {
    for (let index = workerIndex; index < count; index += variants.length) {
      const result = await client.upload(
        `performance-${workerIndex}.png`,
        content,
      );
      const duration = parseServerTiming(
        result.serverTiming,
        "document-create",
      );
      if (measured) samples.push(duration);
      // 上传计时只读 Server-Timing；清理前等造数 Provider 在能力摘要边界终止，避免与 Worker 的版本 CAS 竞争。
      await waitForUploadCleanup(client, result.body.job_id);
      await client.mutate(
        `/documents/${encodeURIComponent(result.body.document_id)}`,
        { method: "DELETE" },
      );
    }
  });
  await Promise.all(workers);
  return samples;
}

export async function waitForUploadCleanup(client, jobID) {
  const deadline = performance.now() + 30_000;
  while (performance.now() < deadline) {
    const result = await client.read(`/jobs/${encodeURIComponent(jobID)}`);
    const job = result.body;
    if (
      job.status === "failed" &&
      job.error_code === "provider_capability_stale"
    )
      return;
    if (!["queued", "processing"].includes(job.status))
      throw new SafeToolError("upload_cleanup_state_invalid");
    await delay(25);
  }
  throw new SafeToolError("upload_cleanup_timeout");
}

async function confirmBatch(client, jobIDs, measured) {
  const samples = [];
  await runConcurrent(jobIDs.length, 2, async (index) => {
    const jobID = jobIDs[index];
    const result = await client.mutate(
      `/reviews/${encodeURIComponent(jobID)}/confirm`,
      {
        method: "POST",
        headers: {
          "Idempotency-Key": `performance-confirm-${jobID.slice(-12)}`,
        },
        json: performanceConfirmationRequest(),
      },
    );
    if (result.body.replayed !== false || result.body.fact_type !== "payment") {
      throw new Error(
        `confirmation ${jobID} was not a first payment confirmation`,
      );
    }
    if (measured)
      samples.push(parseServerTiming(result.serverTiming, "review-confirm"));
  });
  return samples;
}

export function performanceConfirmationRequest() {
  return {
    expected_revision: 1,
    association_mode: "no_candidate",
    allocations: [],
    duplicate_resolutions: [],
  };
}

async function buildUploadVariants() {
  const source = await readFile(sourcePNG);
  if (source.length >= 1024 * 1024)
    throw new Error("performance PNG source unexpectedly exceeds 1 MiB");
  return [0x41, 0x42].map((marker) => {
    const result = Buffer.alloc(1024 * 1024, 0);
    source.copy(result);
    result[result.length - 1] = marker;
    return result;
  });
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
    const serverTiming = response.headers.get("server-timing") ?? "";
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new ApiFailure(response.status, body);
    }
    const body = response.status === 204 ? undefined : await response.json();
    return { body, serverTiming };
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
      if (!response.ok) {
        const body = await response.json().catch(() => ({}));
        throw new ApiFailure(response.status, body);
      }
      cookie = response.headers
        .getSetCookie()
        .map((entry) => entry.split(";", 1)[0])
        .join("; ");
      const body = await response.json();
      csrf = body.csrf_token;
      if (!cookie || !csrf)
        throw new Error("login did not establish session and CSRF state");
    },
    async consume(path) {
      const started = process.hrtime.bigint();
      const response = await fetch(base + path, {
        headers: { Cookie: cookie },
        signal: AbortSignal.timeout(30_000),
        redirect: "error",
      });
      if (!response.ok) throw new Error(`${path}: HTTP ${response.status}`);
      await response.arrayBuffer();
      return Number(process.hrtime.bigint() - started) / 1_000_000;
    },
    mutate(path, options = {}) {
      return request(path, { ...options, csrf: true });
    },
    read(path) {
      return request(path);
    },
    async upload(name, content) {
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

async function measureConcurrent(count, concurrency, operation) {
  const values = new Array(count);
  await runConcurrent(count, concurrency, async (index) => {
    values[index] = await operation(index);
  });
  return values;
}

async function runConcurrent(count, concurrency, operation) {
  let next = 0;
  async function worker() {
    while (true) {
      const index = next;
      next += 1;
      if (index >= count) return;
      await operation(index);
    }
  }
  await Promise.all(
    Array.from({ length: Math.min(count, concurrency) }, () => worker()),
  );
}

function summarize(values, threshold, protocol) {
  if (values.length !== protocol.requests) {
    throw new Error(
      `measurement sample count = ${values.length}, want ${protocol.requests}`,
    );
  }
  const sorted = [...values].sort((left, right) => left - right);
  const p95 = percentile(sorted, 0.95);
  return {
    ...protocol,
    min_ms: round(sorted[0]),
    p50_ms: round(percentile(sorted, 0.5)),
    p95_ms: round(p95),
    max_ms: round(sorted.at(-1)),
    threshold_p95_ms: threshold,
    passed: p95 <= threshold,
  };
}

function percentile(sorted, ratio) {
  return sorted[Math.max(0, Math.ceil(sorted.length * ratio) - 1)];
}

function parseServerTiming(value, metric) {
  const match = new RegExp(
    `(?:^|,)\\s*${metric};dur=([0-9]+(?:\\.[0-9]+)?)`,
  ).exec(value);
  if (!match)
    throw new Error(`missing ${metric} Server-Timing metric: ${value}`);
  return Number(match[1]);
}

function validateSeed(seed) {
  if (
    seed.seed_kind !== "m4-performance-10k-facts" ||
    seed.payments !== 5_000 ||
    seed.invoices !== 5_000 ||
    seed.source_claim_chains !== 10_000 ||
    seed.ready_confirmation_reviews !== 220 ||
    seed.confirmation_job_ids?.length !== 220
  ) {
    throw new Error(
      "performance seed manifest does not satisfy the fixed 10,000 Fact shape",
    );
  }
}

export function parseArguments(argumentsList) {
  const required = [
    "server",
    "email",
    "password-file",
    "seed-manifest",
    "output",
    "build-sha",
    "release-input-sha256",
    "compose-version",
    "compose-config-sha256",
    "image-id",
    "container-id",
    "server-cpus",
    "server-memory-bytes",
    "database-location",
    "object-storage-location",
    "provider-latency-note",
  ];
  const values = parseStrictPairs(argumentsList, required);
  requireLoopbackURL(values.get("server"), { allowPath: false });
  if (!/^[^\s@]+@[^\s@]+$/.test(values.get("email"))) {
    throw new SafeToolError("invalid_arguments");
  }
  const serverCPUs = Number(values.get("server-cpus"));
  const serverMemoryBytes = Number(values.get("server-memory-bytes"));
  if (serverCPUs !== 2 || serverMemoryBytes !== 3584 * 1024 * 1024) {
    throw new SafeToolError("reference_limits_invalid");
  }
  if (
    values.get("database-location") !== "named-volume:sbm_postgres_data" ||
    values.get("object-storage-location") !== "named-volume:sbm_objects" ||
    values.get("provider-latency-note") !==
      "excluded-measured-by-memory-gate" ||
    !/^v?\d+\.\d+(?:\.\d+)?(?:[-+][a-z0-9.-]+)?$/i.test(
      values.get("compose-version"),
    )
  ) {
    throw new SafeToolError("reference_identity_invalid");
  }
  const containerID = values.get("container-id");
  if (!/^[0-9a-f]{64}$/.test(containerID)) {
    throw new SafeToolError("process_identity_invalid");
  }
  return {
    server: values.get("server"),
    email: values.get("email"),
    passwordFile: resolve(values.get("password-file")),
    seedManifest: resolve(values.get("seed-manifest")),
    output: resolve(values.get("output")),
    buildSha: requireGitSHA(values.get("build-sha")),
    releaseInputSha256: requireSHA256(values.get("release-input-sha256")),
    composeVersion: values.get("compose-version"),
    composeConfigSha256: requireSHA256(values.get("compose-config-sha256")),
    imageID: requireImageID(values.get("image-id")),
    containerID,
    serverCPUs,
    serverMemoryBytes,
    databaseLocation: values.get("database-location"),
    objectStorageLocation: values.get("object-storage-location"),
    providerLatencyNote: values.get("provider-latency-note"),
  };
}

async function assertPerformanceContainer(options) {
  const result = await runCaptured("docker", ["inspect", options.containerID], {
    cwd: projectDirectory,
    env: selectedDockerEnvironment(),
    timeout: 10_000,
    maximumBytes: 1024 * 1024,
  });
  try {
    if (result.code !== 0) throw new SafeToolError("process_identity_invalid");
    const inspected = JSON.parse(result.stdout.toString("utf8"));
    if (
      !Array.isArray(inspected) ||
      inspected.length !== 1 ||
      !isExpectedPerformanceContainer(inspected[0], options)
    ) {
      throw new SafeToolError("process_identity_invalid");
    }
  } catch (error) {
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError("process_identity_invalid");
  } finally {
    clearBuffers([result.stdout, result.stderr]);
  }
}

export function isExpectedPerformanceContainer(inspected, options) {
  return (
    inspected?.Id === options.containerID &&
    inspected?.Image === options.imageID &&
    inspected?.Config?.Image === "smart-bill-manager:local" &&
    inspected?.State?.Running === true &&
    hasExpectedLoopbackBinding(inspected, options.server)
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

function ensureTrailingSlash(value) {
  return value.endsWith("/") ? value : `${value}/`;
}

function round(value) {
  return Math.round(value * 100) / 100;
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`performance: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
