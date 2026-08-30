#!/usr/bin/env node

import { readFile, stat, writeFile } from "node:fs/promises";
import { cpus, freemem, platform, totalmem } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

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
  const seed = JSON.parse(await readFile(options.seedManifest, "utf8"));
  validateSeed(seed);
  const password = await readProtectedSecret(options.passwordFile, 1024);
  const client = createClient(options.server);
  try {
    await client.login(options.email, password);
    const endpoints = {
      inbox_list: "/jobs",
      document_detail: `/documents/${encodeURIComponent(seed.representative_document_id)}`,
      claim_set_detail: `/claim-sets/${encodeURIComponent(seed.representative_claim_set_id)}`,
      payment_list: "/payments",
      invoice_list: "/invoices",
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
      report_kind: "m1-performance-result",
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
        compose_version: options.composeVersion,
        compose_config_sha256: options.composeConfigSha256,
        image_id: options.imageID,
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
    const encoded = `${JSON.stringify(report, null, 2)}\n`;
    if (encoded.includes(password))
      throw new Error(
        "refusing to write performance output containing the owner password",
      );
    await writeFile(options.output, encoded, {
      encoding: "utf8",
      flag: "wx",
      mode: 0o600,
    });
    process.stdout.write(encoded);
    if (failed.length !== 0) process.exitCode = 1;
  } finally {
    password.fill(0);
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
      await client.mutate(
        `/documents/${encodeURIComponent(result.body.document_id)}`,
        { method: "DELETE" },
      );
    }
  });
  await Promise.all(workers);
  return samples;
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
        json: { expected_revision: 1, association_mode: "no_candidate" },
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
      });
      if (!response.ok) throw new Error(`${path}: HTTP ${response.status}`);
      await response.arrayBuffer();
      return Number(process.hrtime.bigint() - started) / 1_000_000;
    },
    mutate(path, options = {}) {
      return request(path, { ...options, csrf: true });
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
    seed.seed_kind !== "m1-performance-10k-facts" ||
    seed.payments !== 5_000 ||
    seed.invoices !== 5_000 ||
    seed.source_claim_chains < 1_000
  ) {
    throw new Error(
      "performance seed manifest does not satisfy the fixed 10,000 Fact shape",
    );
  }
}

async function readProtectedSecret(path, maximumBytes) {
  const information = await stat(path);
  if (!information.isFile() || (information.mode & 0o077) !== 0) {
    throw new Error("password file must be regular and owner-only");
  }
  const content = await readFile(path);
  const result =
    content.at(-1) === 0x0a
      ? Buffer.from(
          content.subarray(
            0,
            content.at(-2) === 0x0d ? content.length - 2 : content.length - 1,
          ),
        )
      : Buffer.from(content);
  if (result.length < 1 || result.length > maximumBytes)
    throw new Error("password file size is invalid");
  return result;
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
    "seed-manifest",
    "output",
    "build-sha",
    "compose-version",
    "compose-config-sha256",
    "image-id",
    "server-cpus",
    "server-memory-bytes",
    "database-location",
    "object-storage-location",
    "provider-latency-note",
  ]) {
    if (!values.get(name)) throw new Error(`--${name} is required`);
  }
  const serverCPUs = Number(values.get("server-cpus"));
  const serverMemoryBytes = Number(values.get("server-memory-bytes"));
  if (!Number.isFinite(serverCPUs) || serverCPUs <= 0) {
    throw new Error("--server-cpus must be a positive number");
  }
  if (!Number.isSafeInteger(serverMemoryBytes) || serverMemoryBytes < 1) {
    throw new Error("--server-memory-bytes must be a positive safe integer");
  }
  return {
    server: values.get("server"),
    email: values.get("email"),
    passwordFile: resolve(values.get("password-file")),
    seedManifest: resolve(values.get("seed-manifest")),
    output: resolve(values.get("output")),
    buildSha: values.get("build-sha"),
    composeVersion: values.get("compose-version"),
    composeConfigSha256: values.get("compose-config-sha256"),
    imageID: values.get("image-id"),
    serverCPUs,
    serverMemoryBytes,
    databaseLocation: values.get("database-location"),
    objectStorageLocation: values.get("object-storage-location"),
    providerLatencyNote: values.get("provider-latency-note"),
  };
}

function ensureTrailingSlash(value) {
  return value.endsWith("/") ? value : `${value}/`;
}

function round(value) {
  return Math.round(value * 100) / 100;
}

main().catch((error) => {
  process.stderr.write(
    `${error instanceof Error ? error.message : String(error)}\n`,
  );
  process.exitCode = 1;
});
