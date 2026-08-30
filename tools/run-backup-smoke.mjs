#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFile, stat, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const stateKind = "m1-backup-smoke-state";
const processingTerminalStates = new Set([
  "needs_review",
  "blocked",
  "failed",
  "cancelled",
  "completed",
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
  const password = await readProtectedFile(options.passwordFile, 1024);
  const client = createClient(options.server);
  try {
    await client.login(options.email, password);
    if (options.phase === "seed") await seed(client, options, password);
    else if (options.phase === "stage-processing")
      await stageProcessing(client, options, password);
    else await verifyRestore(client, options, password);
  } finally {
    password.fill(0);
  }
}

async function seed(client, options, password) {
  const providerApiKey = await readProtectedFile(
    options.providerApiKeyFile,
    4096,
  );
  try {
    const [jobs, payments, providers] = await Promise.all([
      client.get("/jobs"),
      client.get("/payments"),
      client.get("/provider-configs"),
    ]);
    if (jobs.items.length || payments.items.length || providers.items.length) {
      throw new Error("backup smoke seed requires a fresh workspace");
    }
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
      throw new Error("synthetic provider capability detection failed");
    const active = await client.mutate(
      `/provider-configs/${encodeURIComponent(provider.id)}/activate`,
      { method: "POST" },
    );
    const source = await readFile(options.source);
    const uploaded = await client.upload(
      "backup-confirmed.png",
      variant(source, 1),
    );
    const reviewable = await waitForJob(
      client,
      uploaded.job_id,
      30_000,
      (job) => processingTerminalStates.has(job.status),
    );
    if (reviewable.status !== "needs_review")
      throw new Error(`confirmed seed job stopped at ${reviewable.status}`);
    const confirmation = await confirm(
      client,
      uploaded.job_id,
      "backup-confirmed",
    );
    const completed = await client.get(
      `/jobs/${encodeURIComponent(uploaded.job_id)}`,
    );
    if (
      completed.status !== "completed" ||
      confirmation.fact_type !== "payment"
    ) {
      throw new Error("confirmed seed did not create a completed payment");
    }
    await assertDownload(client, uploaded.document_id, uploaded.sha256);
    const state = {
      state_kind: stateKind,
      state_version: 1,
      provider_config_id: provider.id,
      provider_safe_fingerprint: active.safe_fingerprint,
      provider_base_url_host: new URL(options.providerBaseUrl).host,
      model: options.model,
      confirmed: {
        document_id: uploaded.document_id,
        job_id: uploaded.job_id,
        fact_id: confirmation.fact_id,
        sha256: uploaded.sha256,
      },
    };
    await writeProtectedJSON(options.output, state, [password, providerApiKey]);
    process.stdout.write(`${JSON.stringify(state, null, 2)}\n`);
  } finally {
    providerApiKey.fill(0);
  }
}

async function stageProcessing(client, options, password) {
  const state = await readState(options.state);
  const source = await readFile(options.source);
  const uploaded = await client.upload(
    "backup-processing.png",
    variant(source, 2),
  );
  const processing = await waitForJob(
    client,
    uploaded.job_id,
    30_000,
    (job) =>
      job.status === "processing" || processingTerminalStates.has(job.status),
  );
  if (processing.status !== "processing") {
    throw new Error(
      `recovery seed job stopped at ${processing.status}; use a hanging synthetic provider`,
    );
  }
  await assertDownload(client, uploaded.document_id, uploaded.sha256);
  const staged = {
    ...state,
    staged_at: new Date().toISOString(),
    processing: {
      document_id: uploaded.document_id,
      job_id: uploaded.job_id,
      sha256: uploaded.sha256,
      attempt_count_at_backup: processing.attempt_count,
    },
  };
  await writeProtectedJSON(options.output, staged, [password]);
  process.stdout.write(`${JSON.stringify(staged, null, 2)}\n`);
}

async function verifyRestore(client, options, password) {
  const state = await readState(options.state, true);
  const started = Date.now();
  const recovered = await waitForJob(
    client,
    state.processing.job_id,
    360_000,
    (job) => processingTerminalStates.has(job.status),
  );
  if (recovered.status !== "needs_review") {
    throw new Error(`restored processing job stopped at ${recovered.status}`);
  }
  const confirmation = await confirm(
    client,
    state.processing.job_id,
    "backup-recovered",
  );
  const finalJob = await client.get(
    `/jobs/${encodeURIComponent(state.processing.job_id)}`,
  );
  if (finalJob.status !== "completed")
    throw new Error("restored job did not reach completed");
  const [jobs, payments] = await Promise.all([
    client.get("/jobs"),
    client.get("/payments"),
    assertDownload(client, state.confirmed.document_id, state.confirmed.sha256),
    assertDownload(
      client,
      state.processing.document_id,
      state.processing.sha256,
    ),
  ]);
  const expectedFacts = new Set([
    state.confirmed.fact_id,
    confirmation.fact_id,
  ]);
  const visibleFacts = payments.items.filter((item) =>
    expectedFacts.has(item.id),
  );
  if (jobs.items.length !== 2 || visibleFacts.length !== 2) {
    throw new Error(
      `restored counts = jobs:${jobs.items.length} expected-facts:${visibleFacts.length}`,
    );
  }
  if (jobs.items.some((job) => job.status !== "completed")) {
    throw new Error("restored job inventory contains a non-completed job");
  }
  const result = {
    report_kind: "m1-backup-restore-smoke-result",
    verified_at: new Date().toISOString(),
    lease_recovery_elapsed_ms: Date.now() - started,
    confirmed_document_id: state.confirmed.document_id,
    recovered_document_id: state.processing.document_id,
    confirmed_fact_id: state.confirmed.fact_id,
    recovered_fact_id: confirmation.fact_id,
    job_count: jobs.items.length,
    payment_count: payments.items.length,
    downloads_verified: 2,
    processing_attempt_count_before_backup:
      state.processing.attempt_count_at_backup,
    processing_attempt_count_after_recovery: finalJob.attempt_count,
    passed: true,
  };
  await writeProtectedJSON(options.output, result, [password]);
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
}

async function confirm(client, jobID, idempotencyKey) {
  const result = await client.mutate(
    `/reviews/${encodeURIComponent(jobID)}/confirm`,
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      json: { expected_revision: 1, association_mode: "no_candidate" },
    },
  );
  if (result.replayed !== false)
    throw new Error(`confirmation ${jobID} was unexpectedly replayed`);
  return result;
}

async function waitForJob(client, jobID, timeoutMs, accepted) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const job = await client.get(`/jobs/${encodeURIComponent(jobID)}`);
    if (accepted(job)) return job;
    await delay(250);
  }
  throw new Error(
    `job ${jobID} did not reach the required state within ${timeoutMs} ms`,
  );
}

async function assertDownload(client, documentID, expectedHash) {
  const content = await client.download(
    `/documents/${encodeURIComponent(documentID)}/content`,
  );
  const actual = createHash("sha256").update(content).digest("hex");
  if (actual !== expectedHash)
    throw new Error(`downloaded document ${documentID} hash differs`);
}

function createClient(server) {
  const base = new URL("/api/v1/", ensureTrailingSlash(server))
    .toString()
    .replace(/\/$/, "");
  let cookie = "";
  let csrf = "";
  async function raw(path, options = {}) {
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
    return response;
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
    async get(path) {
      return (await raw(path)).json();
    },
    async mutate(path, options = {}) {
      const response = await raw(path, { ...options, csrf: true });
      return response.status === 204 ? undefined : response.json();
    },
    createProvider(providerBaseUrl, apiKeyBytes, model) {
      return this.mutate("/provider-configs", {
        method: "POST",
        json: {
          base_url: providerBaseUrl,
          api_key: apiKeyBytes.toString("utf8"),
          model,
          output_mode: "json_schema",
        },
      });
    },
    async upload(name, content) {
      const form = new FormData();
      form.append("file", new Blob([content], { type: "image/png" }), name);
      return (
        await raw("/documents", {
          method: "POST",
          csrf: true,
          body: form,
          timeoutMs: 60_000,
        })
      ).json();
    },
    async download(path) {
      return Buffer.from(await (await raw(path)).arrayBuffer());
    },
  };
}

function variant(source, sequence) {
  const marker = Buffer.alloc(8);
  marker.writeBigUInt64BE(BigInt(sequence));
  return Buffer.concat([source, marker]);
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
  const phase = values.get("phase");
  if (!["seed", "stage-processing", "verify-restore"].includes(phase)) {
    throw new Error(
      "--phase must be seed, stage-processing, or verify-restore",
    );
  }
  for (const name of ["server", "email", "password-file", "output"]) {
    if (!values.get(name)) throw new Error(`--${name} is required`);
  }
  if (phase === "seed") {
    for (const name of [
      "provider-base-url",
      "provider-api-key-file",
      "model",
      "source",
    ]) {
      if (!values.get(name)) throw new Error(`--${name} is required for seed`);
    }
  } else {
    if (!values.get("state"))
      throw new Error(`--state is required for ${phase}`);
    if (phase === "stage-processing" && !values.get("source")) {
      throw new Error("--source is required for stage-processing");
    }
  }
  return {
    phase,
    server: values.get("server"),
    email: values.get("email"),
    passwordFile: resolve(values.get("password-file")),
    output: resolve(values.get("output")),
    state: values.get("state") ? resolve(values.get("state")) : "",
    providerBaseUrl: values.get("provider-base-url"),
    providerApiKeyFile: values.get("provider-api-key-file")
      ? resolve(values.get("provider-api-key-file"))
      : "",
    model: values.get("model"),
    source: values.get("source") ? resolve(values.get("source")) : "",
  };
}

async function readState(path, requireProcessing = false) {
  const content = await readProtectedFile(path, 1024 * 1024);
  const state = JSON.parse(content.toString("utf8"));
  content.fill(0);
  if (
    state.state_kind !== stateKind ||
    state.state_version !== 1 ||
    !state.confirmed?.document_id ||
    (requireProcessing && !state.processing?.document_id)
  ) {
    throw new Error("invalid backup smoke state file");
  }
  return state;
}

async function writeProtectedJSON(path, value, secrets) {
  const encoded = `${JSON.stringify(value, null, 2)}\n`;
  for (const secret of secrets) {
    if (secret.length && encoded.includes(secret.toString("utf8"))) {
      throw new Error(
        "refusing to write backup smoke output containing a secret",
      );
    }
  }
  await writeFile(path, encoded, { encoding: "utf8", flag: "wx", mode: 0o600 });
}

async function readProtectedFile(path, maximumBytes) {
  const information = await stat(path);
  if (!information.isFile() || (information.mode & 0o077) !== 0)
    throw new Error(`protected file must be regular and owner-only: ${path}`);
  const content = await readFile(path);
  if (content.length < 1 || content.length > maximumBytes + 2)
    throw new Error(`protected file size is invalid: ${path}`);
  const end =
    content.at(-1) === 0x0a
      ? content.length - (content.at(-2) === 0x0d ? 2 : 1)
      : content.length;
  const result = Buffer.from(content.subarray(0, end));
  if (result.length < 1 || result.length > maximumBytes)
    throw new Error(`protected file size is invalid: ${path}`);
  return result;
}

function ensureTrailingSlash(value) {
  return value.endsWith("/") ? value : `${value}/`;
}
function delay(milliseconds) {
  return new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));
}

main().catch((error) => {
  process.stderr.write(
    `${error instanceof Error ? error.message : String(error)}\n`,
  );
  process.exitCode = 1;
});
