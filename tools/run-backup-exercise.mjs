#!/usr/bin/env node

import { createHash, randomUUID } from "node:crypto";
import { constants } from "node:fs";
import { open } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { deflateSync } from "node:zlib";

const stateKind = "m4-backup-exercise-state";
const stateVersion = 1;
const ordinaryDocumentCount = 997;
const expectedDocumentCount = 1000;
const recoveryTimeLimitMs = 30 * 60 * 1000;
const processingTerminalStates = new Set([
  "needs_review",
  "blocked",
  "failed",
  "cancelled",
  "completed",
  "rejected",
]);
let activeControllerStage = "startup";

class ApiFailure extends Error {
  constructor(status, body) {
    super(body?.error?.message ?? `HTTP ${status}`);
    this.status = status;
    this.code = body?.error?.code ?? "unknown_error";
  }
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const resultOutput = await reserveProtectedOutput(options.output);
  let sessionOutput;
  let baselineOutput;
  try {
    if (options.phase === "stage-processing") {
      sessionOutput = await reserveProtectedOutput(options.oldSessionOutput);
    }
    if (options.phase === "start-recovery") {
      await startRecoveryClock(options, resultOutput);
      return;
    }
    if (options.phase === "verify-restore") {
      baselineOutput = await reserveProtectedOutput(options.baselineOutput);
    }
    const password = await readProtectedFile(options.passwordFile, 1024);
    try {
      const client = createClient(options.server);
      if (options.phase === "verify-restore") {
        await verifyRestore(
          client,
          options,
          password,
          resultOutput,
          baselineOutput,
        );
        return;
      }
      const session = await client.login(options.email, password);
      if (options.phase === "seed-capacity") {
        await seedCapacity(client, session, password, resultOutput);
      } else if (options.phase === "seed-confirmed") {
        await seedConfirmed(client, options, password, resultOutput);
      } else {
        await stageProcessing(
          client,
          options,
          password,
          resultOutput,
          sessionOutput,
        );
      }
    } finally {
      password.fill(0);
    }
  } finally {
    await baselineOutput?.close();
    await sessionOutput?.close();
    await resultOutput.close();
  }
}

async function seedCapacity(client, session, password, resultOutput) {
  const [jobs, payments, providers, sources] = await Promise.all([
    client.get("/jobs"),
    client.get("/payments"),
    client.get("/provider-configs"),
    client.get("/email-sources"),
  ]);
  if (
    jobs.items.length ||
    payments.items.length ||
    providers.items.length ||
    sources.items.length
  ) {
    throw new Error("recovery capacity seed requires a fresh workspace");
  }
  const uploads = await mapConcurrent(
    Array.from({ length: ordinaryDocumentCount }, (_, index) => index + 1),
    8,
    (sequence) =>
      client.upload(
        `recovery-capacity-${String(sequence).padStart(4, "0")}.png`,
        syntheticPNG(sequence),
      ),
  );
  const failedJobs = await mapConcurrent(uploads, 16, (upload) =>
    waitForJob(client, upload.job_id, 120_000, (job) =>
      processingTerminalStates.has(job.status),
    ),
  );
  if (
    failedJobs.length !== ordinaryDocumentCount ||
    failedJobs.some(
      (job) =>
        job.status !== "failed" || job.error_code !== "provider_config_missing",
    )
  ) {
    throw new Error(
      "capacity jobs did not fail at the explicit Provider boundary",
    );
  }
  const emailSource = await client.mutate("/email-sources", {
    method: "POST",
    headers: { "Idempotency-Key": "m4-recovery-email-source" },
    json: {
      display_name: "M4 recovery fixture",
      mailbox_address: "archive@example.invalid",
      imap_host: "imap.example.invalid",
      imap_port: 993,
      transport_security: "implicit_tls",
    },
  });
  await assertDownload(
    client,
    `/documents/${encodeURIComponent(uploads[0].document_id)}/content`,
    uploads[0].sha256,
  );
  const state = {
    state_kind: stateKind,
    state_version: stateVersion,
    exercise_id: randomUUID(),
    tenant_id: session.tenant.id,
    user_id: session.user.id,
    email_source_id: emailSource.id,
    ordinary_document_count: ordinaryDocumentCount,
    ordinary_sample: {
      document_id: uploads[0].document_id,
      sha256: uploads[0].sha256,
    },
  };
  await resultOutput.writeJSON(state, [password]);
  printSafeJSON(safeControllerOutput("seed-capacity", state));
}

async function seedConfirmed(client, options, password, resultOutput) {
  const state = await readState(options.state);
  const emailFixture = await readEmailFixture(options.emailFixture);
  if (emailFixture.exercise_id !== state.exercise_id) {
    throw new Error("email fixture belongs to a different recovery exercise");
  }
  const emailJob = await waitForJob(
    client,
    emailFixture.job_id,
    30_000,
    (job) => processingTerminalStates.has(job.status),
  );
  if (
    emailJob.status !== "failed" ||
    emailJob.error_code !== "provider_config_missing"
  ) {
    throw new Error(
      `email fixture job stopped at ${emailJob.status}/${emailJob.error_code ?? "none"}`,
    );
  }
  const providerApiKey = await readProtectedFile(
    options.providerApiKeyFile,
    4096,
  );
  try {
    const provider = await client.createProvider(
      options.providerBaseUrl,
      providerApiKey,
      options.model,
    );
    const detected = await client.mutate(
      `/provider-configs/${encodeURIComponent(provider.id)}/detect`,
      { method: "POST", timeoutMs: 90_000 },
    );
    if (detected.capability_status !== "passed") {
      throw new Error("synthetic provider capability detection failed");
    }
    const active = await client.mutate(
      `/provider-configs/${encodeURIComponent(provider.id)}/activate`,
      { method: "POST" },
    );
    const uploaded = await client.upload(
      "recovery-confirmed.png",
      syntheticPNG(ordinaryDocumentCount + 1),
    );
    const reviewable = await waitForJob(
      client,
      uploaded.job_id,
      30_000,
      (job) => processingTerminalStates.has(job.status),
    );
    if (reviewable.status !== "needs_review") {
      throw new Error(`confirmed seed job stopped at ${reviewable.status}`);
    }
    const confirmation = await confirm(
      client,
      uploaded.job_id,
      "m4-recovery-confirmed",
    );
    if (confirmation.fact_type !== "payment") {
      throw new Error("confirmed recovery seed did not create a Payment Fact");
    }
    await assertDownload(
      client,
      `/documents/${encodeURIComponent(uploaded.document_id)}/content`,
      uploaded.sha256,
    );
    const seeded = {
      ...state,
      provider_config_id: provider.id,
      provider_safe_fingerprint: active.safe_fingerprint,
      provider_base_url_host: new URL(options.providerBaseUrl).host,
      model: options.model,
      email_fixture: emailFixture,
      confirmed: {
        document_id: uploaded.document_id,
        job_id: uploaded.job_id,
        fact_id: confirmation.fact_id,
        sha256: uploaded.sha256,
      },
    };
    await resultOutput.writeJSON(seeded, [password, providerApiKey]);
    printSafeJSON(safeControllerOutput("seed-confirmed", seeded));
  } finally {
    providerApiKey.fill(0);
  }
}

async function stageProcessing(
  client,
  options,
  password,
  resultOutput,
  sessionOutput,
) {
  const state = await readState(options.state, { requireConfirmed: true });
  requireMatchingProviderHealthOrigin(options.providerHealthUrl, state);
  const providerInstanceID = await waitForHangingProviderBaseline(
    options.providerHealthUrl,
    state,
    30_000,
  );
  const uploaded = await client.upload(
    "recovery-processing.png",
    syntheticPNG(ordinaryDocumentCount + 2),
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
      `recovery seed job stopped at ${processing.status}; use the hanging synthetic provider mode`,
    );
  }
  await waitForProviderExtraction(
    options.providerHealthUrl,
    state,
    providerInstanceID,
    30_000,
  );
  const [visibleJobs, payments] = await Promise.all([
    client.get("/jobs"),
    client.get("/payments"),
    assertDownload(
      client,
      `/documents/${encodeURIComponent(uploaded.document_id)}/content`,
      uploaded.sha256,
    ),
  ]);
  if (
    visibleJobs.items.length !== 200 ||
    payments.items.length !== 1 ||
    !visibleJobs.items.some((job) => job.id === uploaded.job_id)
  ) {
    throw new Error(
      `pre-backup API window = jobs:${visibleJobs.items.length} payments:${payments.items.length}`,
    );
  }
  const staged = {
    ...state,
    staged_at: new Date().toISOString(),
    expected_document_count: expectedDocumentCount,
    hanging_provider_extraction_observed: true,
    processing: {
      document_id: uploaded.document_id,
      job_id: uploaded.job_id,
      sha256: uploaded.sha256,
      attempt_count_at_backup: processing.attempt_count,
    },
  };
  const oldSession = Buffer.from(client.cookieHeader(), "utf8");
  try {
    await sessionOutput.writeBytes(oldSession);
  } finally {
    oldSession.fill(0);
  }
  await resultOutput.writeJSON(staged, [password]);
  printSafeJSON(safeControllerOutput("stage-processing", staged));
}

async function startRecoveryClock(options, resultOutput) {
  const state = await readState(options.state, { requireProcessing: true });
  const backup = await readBackupOperationResult(options.backupResult);
  const started = Date.now();
  const monotonicStarted = process.hrtime.bigint();
  if (backup.operation_finished_at_epoch_ms > started) {
    throw new Error("backup operation finished after the recovery clock");
  }
  const marker = `${options.state}.recovery-clock-started`;
  const clockMarker = await reserveProtectedOutput(marker);
  try {
    await clockMarker.writeBytes(
      Buffer.from("smart-bill-manager-recovery-clock-started/1\n", "utf8"),
    );
  } finally {
    await clockMarker.close();
  }
  const timed = {
    ...state,
    backup_set_id: backup.backup_set_id,
    backup_operation_finished_at_epoch_ms:
      backup.operation_finished_at_epoch_ms,
    recovery_started_at: new Date(started).toISOString(),
    recovery_started_at_epoch_ms: started,
    recovery_started_at_monotonic_ns: monotonicStarted.toString(),
  };
  await resultOutput.writeJSON(timed, []);
  printSafeJSON(safeControllerOutput("start-recovery", timed));
}

async function verifyRestore(
  client,
  options,
  password,
  resultOutput,
  baselineOutput,
) {
  activeControllerStage = "restore_state";
  const state = await readState(options.state, {
    requireProcessing: true,
    requireRecoveryClock: true,
  });
  activeControllerStage = "ready";
  await client.ready();
  activeControllerStage = "old_session";
  const oldCookie = await readProtectedFile(options.oldSessionFile, 16 * 1024);
  try {
    await client.assertSessionRejected(oldCookie.toString("utf8"));
  } finally {
    oldCookie.fill(0);
  }
  activeControllerStage = "new_login";
  await client.login(options.email, password);
  activeControllerStage = "baseline_reads";
  const [
    baselinePayments,
    baselineEmailMessages,
    ordinaryDocument,
    confirmedDocument,
    processingDocument,
  ] = await Promise.all([
    client.get("/payments"),
    client.get(
      `/email-sources/${encodeURIComponent(state.email_source_id)}/messages?limit=50`,
    ),
    client.get(
      `/documents/${encodeURIComponent(state.ordinary_sample.document_id)}`,
    ),
    client.get(`/documents/${encodeURIComponent(state.confirmed.document_id)}`),
    client.get(
      `/documents/${encodeURIComponent(state.processing.document_id)}`,
    ),
    assertDownload(
      client,
      `/documents/${encodeURIComponent(state.ordinary_sample.document_id)}/content`,
      state.ordinary_sample.sha256,
    ),
    assertDownload(
      client,
      `/documents/${encodeURIComponent(state.confirmed.document_id)}/content`,
      state.confirmed.sha256,
    ),
    assertDownload(
      client,
      `/documents/${encodeURIComponent(state.processing.document_id)}/content`,
      state.processing.sha256,
    ),
    assertDownload(
      client,
      `/email-messages/${encodeURIComponent(state.email_fixture.message_id)}/raw`,
      state.email_fixture.raw_sha256,
    ),
    assertDownload(
      client,
      `/email-attachments/${encodeURIComponent(state.email_fixture.attachment_id)}/content`,
      state.email_fixture.attachment_sha256,
    ),
  ]);
  const processingJobBeforeContinuation = await client.get(
    `/jobs/${encodeURIComponent(state.processing.job_id)}`,
  );
  activeControllerStage = "baseline_shape";
  validateRestoredSnapshotBeforeContinuation(state, {
    payments: baselinePayments,
    emailMessages: baselineEmailMessages,
    ordinaryDocument,
    confirmedDocument,
    processingDocument,
    processingJob: processingJobBeforeContinuation,
  });
  activeControllerStage = "baseline_output";
  await baselineOutput.writeJSON(
    {
      report_kind: "m4-backup-restore-baseline",
      report_version: 1,
      restored_snapshot_verified_before_continuation: true,
      passed: true,
    },
    [password],
  );
  activeControllerStage = "lease_recovery";
  const recovered = await waitForJob(
    client,
    state.processing.job_id,
    360_000,
    (job) => processingTerminalStates.has(job.status),
  );
  if (recovered.status !== "needs_review") {
    throw new Error(`restored processing job stopped at ${recovered.status}`);
  }
  activeControllerStage = "confirmation";
  const confirmation = await confirm(
    client,
    state.processing.job_id,
    "m4-recovery-restored",
  );
  activeControllerStage = "final_reads";
  const finalJob = await client.get(
    `/jobs/${encodeURIComponent(state.processing.job_id)}`,
  );
  const [jobs, payments, emailMessages] = await Promise.all([
    client.get("/jobs"),
    client.get("/payments"),
    client.get(
      `/email-sources/${encodeURIComponent(state.email_source_id)}/messages?limit=50`,
    ),
  ]);
  const expectedFacts = new Set([
    state.confirmed.fact_id,
    confirmation.fact_id,
  ]);
  const visibleFacts = payments.items.filter((item) =>
    expectedFacts.has(item.id),
  );
  const verifiedAtEpochMs = Date.now();
  activeControllerStage = "recovery_clock";
  const elapsed = calculateRecoveryElapsed(
    state,
    process.hrtime.bigint(),
    verifiedAtEpochMs,
  );
  if (
    finalJob.status !== "completed" ||
    finalJob.attempt_count !== state.processing.attempt_count_at_backup + 1 ||
    jobs.items.length !== 200 ||
    jobs.items.some((job) => !processingTerminalStates.has(job.status)) ||
    ordinaryDocument.id !== state.ordinary_sample.document_id ||
    confirmedDocument.id !== state.confirmed.document_id ||
    processingDocument.id !== state.processing.document_id ||
    visibleFacts.length !== 2 ||
    payments.items.length !== 2 ||
    emailMessages.items.length !== 1 ||
    elapsed > recoveryTimeLimitMs
  ) {
    throw new Error(
      "restored API state does not match the frozen recovery shape",
    );
  }
  activeControllerStage = "result_output";
  const result = {
    report_kind: "m4-backup-restore-api-result",
    report_version: 1,
    exercise_id: state.exercise_id,
    backup_set_id: state.backup_set_id,
    verified_at: new Date(verifiedAtEpochMs).toISOString(),
    verified_at_epoch_ms: verifiedAtEpochMs,
    recovery_started_at_epoch_ms: state.recovery_started_at_epoch_ms,
    rto_elapsed_ms: elapsed,
    rto_limit_ms: recoveryTimeLimitMs,
    ready_verified: true,
    old_session_rejected: true,
    new_login_succeeded: true,
    restored_snapshot_verified_before_continuation: true,
    api_job_window_count: jobs.items.length,
    payment_count: payments.items.length,
    email_message_count: emailMessages.items.length,
    document_queries_verified: 3,
    authenticated_downloads_verified: 5,
    processing_attempt_count_before_backup:
      state.processing.attempt_count_at_backup,
    processing_attempt_count_after_recovery: finalJob.attempt_count,
    recovered_fact_id: confirmation.fact_id,
    passed: true,
  };
  await resultOutput.writeJSON(result, [password]);
  printSafeJSON(safeControllerOutput("verify-restore", result));
}

function validateRestoredSnapshotBeforeContinuation(state, restored) {
  if (
    restored.payments?.items?.length !== 1 ||
    restored.payments.items[0]?.id !== state.confirmed.fact_id ||
    restored.emailMessages?.items?.length !== 1 ||
    restored.ordinaryDocument?.id !== state.ordinary_sample.document_id ||
    restored.ordinaryDocument?.status !== "failed" ||
    restored.confirmedDocument?.id !== state.confirmed.document_id ||
    restored.confirmedDocument?.status !== "completed" ||
    restored.processingDocument?.id !== state.processing.document_id ||
    restored.processingDocument?.status !== "processing" ||
    restored.processingJob?.id !== state.processing.job_id ||
    restored.processingJob?.status !== "processing" ||
    restored.processingJob?.attempt_count !==
      state.processing.attempt_count_at_backup
  ) {
    throw new Error(
      "restored snapshot was not verified before task continuation",
    );
  }
}

async function confirm(client, jobID, idempotencyKey) {
  const review = await client.get(`/reviews/${encodeURIComponent(jobID)}`);
  if (
    !Number.isSafeInteger(review.revision) ||
    review.revision < 1 ||
    !Array.isArray(review.duplicate_candidates)
  ) {
    throw new Error(`review ${jobID} has an invalid confirmation contract`);
  }
  const result = await client.mutate(
    `/reviews/${encodeURIComponent(jobID)}/confirm`,
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      json: {
        expected_revision: review.revision,
        association_mode: "no_candidate",
        allocations: [],
        duplicate_resolutions: review.duplicate_candidates.map((candidate) => ({
          candidate_id: candidate.id,
          action: "keep_distinct",
        })),
      },
    },
  );
  if (result.replayed !== false) {
    throw new Error(`confirmation ${jobID} was unexpectedly replayed`);
  }
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

async function waitForHangingProviderBaseline(healthURL, state, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(healthURL, {
        signal: AbortSignal.timeout(3_000),
      });
      if (response.ok) {
        const body = await response.json();
        if (
          validHangingProviderHealth(body, healthURL, state) &&
          body.requests === 0 &&
          body.probes === 0 &&
          body.extractions === 0
        ) {
          return body.instance_id;
        }
      }
    } catch {
      // 挂起 Provider 进程可能仍在启动；只在总期限结束后失败。
    }
    await delay(250);
  }
  throw new Error("hanging synthetic provider baseline is not clean");
}

async function waitForProviderExtraction(
  healthURL,
  state,
  providerInstanceID,
  timeoutMs,
) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(healthURL, {
        signal: AbortSignal.timeout(3_000),
      });
      if (response.ok) {
        const body = await response.json();
        if (
          validHangingProviderHealth(body, healthURL, state) &&
          body.instance_id === providerInstanceID &&
          body.requests === 1 &&
          body.probes === 0 &&
          body.extractions === 1
        ) {
          return;
        }
      }
    } catch {
      // 挂起 Provider 进程可能仍在启动；只在总期限结束后失败。
    }
    await delay(250);
  }
  throw new Error("hanging synthetic provider did not receive an extraction");
}

function requireMatchingProviderHealthOrigin(healthURL, state) {
  const health = new URL(healthURL);
  if (health.host !== state.provider_base_url_host) {
    throw new Error(
      "provider health origin differs from the configured synthetic provider",
    );
  }
}

function validHangingProviderHealth(body, healthURL, state) {
  requireMatchingProviderHealthOrigin(healthURL, state);
  const exactFields = new Set([
    "kind",
    "version",
    "status",
    "model",
    "mode",
    "exercise_id",
    "instance_id",
    "requests",
    "probes",
    "extractions",
  ]);
  if (
    !body ||
    typeof body !== "object" ||
    Array.isArray(body) ||
    Object.keys(body).length !== exactFields.size ||
    Object.keys(body).some((field) => !exactFields.has(field))
  ) {
    return false;
  }
  return (
    body?.kind === "smart-bill-manager-synthetic-provider" &&
    body.version === 1 &&
    body.status === "ok" &&
    body.model === state.model &&
    body.mode === "hang-extractions" &&
    body.exercise_id === state.exercise_id &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      body.instance_id,
    ) &&
    Number.isSafeInteger(body.requests) &&
    body.requests >= 0 &&
    Number.isSafeInteger(body.probes) &&
    body.probes >= 0 &&
    Number.isSafeInteger(body.extractions) &&
    body.extractions >= 0
  );
}

async function assertDownload(client, path, expectedHash) {
  const content = await client.download(path);
  const actual = createHash("sha256").update(content).digest("hex");
  if (actual !== expectedHash) {
    throw new Error(`download hash differs for ${path}`);
  }
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
    if (options.json !== undefined) {
      headers.set("Content-Type", "application/json");
    }
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
    async ready() {
      const response = await fetch(base + "/ready", {
        signal: AbortSignal.timeout(30_000),
      });
      if (!response.ok) {
        throw new ApiFailure(
          response.status,
          await response.json().catch(() => ({})),
        );
      }
      const body = await response.json();
      if (body.status !== "ready")
        throw new Error("restored server is not ready");
    },
    async assertSessionRejected(candidateCookie) {
      const response = await fetch(base + "/session", {
        headers: { Cookie: candidateCookie },
        signal: AbortSignal.timeout(30_000),
      });
      if (response.status !== 401) {
        throw new Error(
          `old restored session returned HTTP ${response.status}`,
        );
      }
    },
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
        throw new ApiFailure(
          response.status,
          await response.json().catch(() => ({})),
        );
      }
      cookie = response.headers
        .getSetCookie()
        .map((entry) => entry.split(";", 1)[0])
        .join("; ");
      const body = await response.json();
      csrf = body.csrf_token;
      if (!cookie || !csrf || !body.user?.id || !body.tenant?.id) {
        throw new Error("login did not establish a complete session");
      }
      return body;
    },
    cookieHeader() {
      if (!cookie) throw new Error("session cookie is not established");
      return cookie;
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

const crc32Table = Array.from({ length: 256 }, (_, entry) => {
  let value = entry;
  for (let bit = 0; bit < 8; bit += 1) {
    value = (value >>> 1) ^ (value & 1 ? 0xedb88320 : 0);
  }
  return value >>> 0;
});

function syntheticPNG(sequence) {
  const width = 64;
  const height = 64;
  const scanlines = Buffer.alloc(height * (width + 1));
  let state = (0x9e3779b9 ^ sequence) >>> 0;
  for (let y = 0; y < height; y += 1) {
    const row = y * (width + 1);
    scanlines[row] = 0;
    for (let x = 0; x < width; x += 1) {
      state ^= state << 13;
      state ^= state >>> 17;
      state ^= state << 5;
      scanlines[row + x + 1] = state & 0xff;
    }
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 0;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    pngChunk("IHDR", header),
    pngChunk("IDAT", deflateSync(scanlines, { level: 1 })),
    pngChunk("IEND", Buffer.alloc(0)),
  ]);
}

function pngChunk(type, content) {
  const name = Buffer.from(type, "ascii");
  const result = Buffer.alloc(12 + content.length);
  result.writeUInt32BE(content.length, 0);
  name.copy(result, 4);
  content.copy(result, 8);
  result.writeUInt32BE(
    crc32(Buffer.concat([name, content])),
    8 + content.length,
  );
  return result;
}

function crc32(content) {
  let value = 0xffffffff;
  for (const byte of content) {
    value = crc32Table[(value ^ byte) & 0xff] ^ (value >>> 8);
  }
  return (value ^ 0xffffffff) >>> 0;
}

async function mapConcurrent(values, concurrency, operation) {
  const result = new Array(values.length);
  let next = 0;
  async function worker() {
    while (true) {
      const index = next;
      next += 1;
      if (index >= values.length) return;
      result[index] = await operation(values[index], index);
    }
  }
  await Promise.all(
    Array.from({ length: Math.min(concurrency, values.length) }, () =>
      worker(),
    ),
  );
  return result;
}

function parseArguments(argumentsList) {
  const values = new Map();
  for (let index = 0; index < argumentsList.length; index += 2) {
    const key = argumentsList[index];
    const value = argumentsList[index + 1];
    if (!key?.startsWith("--") || value === undefined) {
      throw new Error(`invalid argument near ${key ?? "<end>"}`);
    }
    const normalized = key.slice(2);
    if (values.has(normalized)) {
      throw new Error(`duplicate --${normalized}`);
    }
    values.set(normalized, value);
  }
  const phase = values.get("phase");
  if (
    ![
      "seed-capacity",
      "seed-confirmed",
      "stage-processing",
      "start-recovery",
      "verify-restore",
    ].includes(phase)
  ) {
    throw new Error(
      "--phase must be seed-capacity, seed-confirmed, stage-processing, start-recovery, or verify-restore",
    );
  }
  const allowedByPhase = {
    "seed-capacity": ["phase", "output", "server", "email", "password-file"],
    "seed-confirmed": [
      "phase",
      "output",
      "server",
      "email",
      "password-file",
      "state",
      "email-fixture",
      "provider-base-url",
      "provider-api-key-file",
      "model",
    ],
    "stage-processing": [
      "phase",
      "output",
      "server",
      "email",
      "password-file",
      "state",
      "old-session-output",
      "provider-health-url",
    ],
    "start-recovery": ["phase", "output", "state", "backup-result"],
    "verify-restore": [
      "phase",
      "output",
      "baseline-output",
      "server",
      "email",
      "password-file",
      "state",
      "old-session-file",
    ],
  };
  const allowed = new Set(allowedByPhase[phase]);
  for (const name of values.keys()) {
    if (!allowed.has(name)) {
      throw new Error(`--${name} is not valid for ${phase}`);
    }
  }
  const required = new Set(["output"]);
  if (phase === "start-recovery") {
    required.add("state");
    required.add("backup-result");
  } else {
    for (const name of ["server", "email", "password-file"]) {
      required.add(name);
    }
  }
  if (phase === "seed-confirmed") {
    for (const name of [
      "state",
      "email-fixture",
      "provider-base-url",
      "provider-api-key-file",
      "model",
    ]) {
      required.add(name);
    }
  }
  if (phase === "stage-processing") {
    for (const name of ["state", "old-session-output", "provider-health-url"]) {
      required.add(name);
    }
  }
  if (phase === "verify-restore") {
    for (const name of ["state", "old-session-file", "baseline-output"])
      required.add(name);
  }
  for (const name of required) {
    if (!values.get(name))
      throw new Error(`--${name} is required for ${phase}`);
  }
  const server = values.get("server");
  const email = values.get("email");
  if (server) requireLoopbackURL(server, "server");
  if (email && !/^[^@\s]+@[^@\s]+\.invalid$/i.test(email)) {
    throw new Error("recovery exercise email must use the .invalid domain");
  }
  const providerBaseUrl = values.get("provider-base-url");
  if (providerBaseUrl) requireLoopbackURL(providerBaseUrl, "provider");
  const providerHealthUrl = values.get("provider-health-url");
  if (providerHealthUrl) {
    const health = requireLoopbackURL(providerHealthUrl, "provider health");
    if (health.pathname !== "/healthz" || health.search || health.hash) {
      throw new Error("provider health URL must end at /healthz");
    }
  }
  const model = values.get("model");
  if (model && !/^synthetic-[a-z0-9._-]+$/.test(model)) {
    throw new Error("recovery exercise model must use a synthetic-* identity");
  }
  const output = resolve(values.get("output"));
  const baselineOutput = absoluteOptional(values.get("baseline-output"));
  if (baselineOutput === output) {
    throw new Error("--baseline-output must differ from --output");
  }
  return {
    phase,
    server,
    email,
    passwordFile: absoluteOptional(values.get("password-file")),
    output,
    baselineOutput,
    state: absoluteOptional(values.get("state")),
    emailFixture: absoluteOptional(values.get("email-fixture")),
    providerBaseUrl,
    providerHealthUrl,
    providerApiKeyFile: absoluteOptional(values.get("provider-api-key-file")),
    model,
    oldSessionOutput: absoluteOptional(values.get("old-session-output")),
    oldSessionFile: absoluteOptional(values.get("old-session-file")),
    backupResult: absoluteOptional(values.get("backup-result")),
  };
}

async function readState(path, requirements = {}) {
  const content = await readProtectedFile(path, 4 * 1024 * 1024);
  const state = JSON.parse(content.toString("utf8"));
  content.fill(0);
  validateExerciseState(state, requirements);
  return state;
}

async function readEmailFixture(path) {
  const content = await readProtectedFile(path, 1024 * 1024);
  const result = JSON.parse(content.toString("utf8"));
  content.fill(0);
  validateEmailFixture(result);
  return result;
}

async function readBackupOperationResult(path) {
  const content = await readProtectedFile(path, 1024 * 1024);
  const result = JSON.parse(content.toString("utf8"));
  content.fill(0);
  const fields = new Set([
    "operation",
    "manifest_kind",
    "manifest_version",
    "backup_set_id",
    "document_count",
    "object_reference_count",
    "unique_object_count",
    "database_table_count",
    "operation_started_at_epoch_ms",
    "operation_finished_at_epoch_ms",
    "elapsed_ms",
    "passed",
  ]);
  assertExactKeys(result, fields, "backup operation result");
  if (
    result.operation !== "backup" ||
    result.manifest_kind !== "smart-bill-manager-backup" ||
    result.manifest_version !== 3 ||
    !/^[0-9a-f]{32}$/.test(result.backup_set_id) ||
    result.document_count !== expectedDocumentCount ||
    result.object_reference_count !== 1004 ||
    result.unique_object_count !== 1003 ||
    !Number.isSafeInteger(result.database_table_count) ||
    result.database_table_count < 1 ||
    !Number.isSafeInteger(result.operation_started_at_epoch_ms) ||
    !Number.isSafeInteger(result.operation_finished_at_epoch_ms) ||
    result.operation_started_at_epoch_ms < 1 ||
    result.operation_finished_at_epoch_ms <
      result.operation_started_at_epoch_ms ||
    !Number.isSafeInteger(result.elapsed_ms) ||
    result.elapsed_ms < 0 ||
    result.passed !== true
  ) {
    throw new Error("invalid backup operation result");
  }
  return result;
}

function validateExerciseState(state, requirements = {}) {
  const confirmedRequired =
    requirements.requireConfirmed ||
    requirements.requireProcessing ||
    requirements.requireRecoveryClock;
  const processingRequired =
    requirements.requireProcessing || requirements.requireRecoveryClock;
  const allowed = new Set([
    "state_kind",
    "state_version",
    "exercise_id",
    "tenant_id",
    "user_id",
    "email_source_id",
    "ordinary_document_count",
    "ordinary_sample",
  ]);
  if (confirmedRequired) {
    for (const name of [
      "provider_config_id",
      "provider_safe_fingerprint",
      "provider_base_url_host",
      "model",
      "email_fixture",
      "confirmed",
    ]) {
      allowed.add(name);
    }
  }
  if (processingRequired) {
    for (const name of [
      "staged_at",
      "expected_document_count",
      "hanging_provider_extraction_observed",
      "processing",
    ]) {
      allowed.add(name);
    }
  }
  if (requirements.requireRecoveryClock) {
    for (const name of [
      "recovery_started_at",
      "recovery_started_at_epoch_ms",
      "recovery_started_at_monotonic_ns",
      "backup_set_id",
      "backup_operation_finished_at_epoch_ms",
    ]) {
      allowed.add(name);
    }
  }
  assertExactKeys(state, allowed, "exercise state");
  assertExactKeys(
    state.ordinary_sample,
    new Set(["document_id", "sha256"]),
    "ordinary sample",
  );
  if (
    state.state_kind !== stateKind ||
    state.state_version !== stateVersion ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      state.exercise_id,
    ) ||
    !nonemptyString(state.tenant_id) ||
    !nonemptyString(state.user_id) ||
    !nonemptyString(state.email_source_id) ||
    state.ordinary_document_count !== ordinaryDocumentCount ||
    !nonemptyString(state.ordinary_sample.document_id) ||
    !isSHA256(state.ordinary_sample.sha256)
  ) {
    throw new Error("invalid M4 backup exercise state file");
  }
  if (confirmedRequired) {
    assertExactKeys(
      state.confirmed,
      new Set(["document_id", "job_id", "fact_id", "sha256"]),
      "confirmed state",
    );
    validateEmailFixture(state.email_fixture);
    if (
      !nonemptyString(state.provider_config_id) ||
      !nonemptyString(state.provider_safe_fingerprint) ||
      !/^(127\.0\.0\.1|localhost|\[::1\]):[0-9]{4,5}$/i.test(
        state.provider_base_url_host,
      ) ||
      !/^synthetic-[a-z0-9._-]+$/.test(state.model) ||
      !nonemptyString(state.confirmed.document_id) ||
      !nonemptyString(state.confirmed.job_id) ||
      !nonemptyString(state.confirmed.fact_id) ||
      !isSHA256(state.confirmed.sha256)
    ) {
      throw new Error("invalid confirmed recovery exercise state");
    }
  }
  if (processingRequired) {
    assertExactKeys(
      state.processing,
      new Set(["document_id", "job_id", "sha256", "attempt_count_at_backup"]),
      "processing state",
    );
    if (
      state.expected_document_count !== expectedDocumentCount ||
      state.hanging_provider_extraction_observed !== true ||
      !canonicalTimestamp(state.staged_at) ||
      !nonemptyString(state.processing.document_id) ||
      !nonemptyString(state.processing.job_id) ||
      !isSHA256(state.processing.sha256) ||
      !Number.isSafeInteger(state.processing.attempt_count_at_backup) ||
      state.processing.attempt_count_at_backup < 1
    ) {
      throw new Error("invalid processing recovery exercise state");
    }
  }
  if (requirements.requireRecoveryClock) {
    if (
      !Number.isSafeInteger(state.recovery_started_at_epoch_ms) ||
      state.recovery_started_at_epoch_ms < 1 ||
      !/^[1-9][0-9]*$/.test(state.recovery_started_at_monotonic_ns) ||
      !canonicalTimestamp(state.recovery_started_at) ||
      new Date(state.recovery_started_at_epoch_ms).toISOString() !==
        state.recovery_started_at ||
      !/^[0-9a-f]{32}$/.test(state.backup_set_id) ||
      !Number.isSafeInteger(state.backup_operation_finished_at_epoch_ms) ||
      state.backup_operation_finished_at_epoch_ms < 1 ||
      state.backup_operation_finished_at_epoch_ms >
        state.recovery_started_at_epoch_ms
    ) {
      throw new Error("invalid recovery clock state");
    }
  }
}

function validateEmailFixture(result) {
  assertExactKeys(
    result,
    new Set([
      "kind",
      "version",
      "exercise_id",
      "message_id",
      "attachment_id",
      "document_id",
      "job_id",
      "raw_sha256",
      "attachment_sha256",
      "passed",
    ]),
    "email fixture",
  );
  if (
    result.kind !== "m4-recovery-email-fixture" ||
    result.version !== 1 ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      result.exercise_id,
    ) ||
    result.passed !== true ||
    !nonemptyString(result.message_id) ||
    !nonemptyString(result.attachment_id) ||
    !nonemptyString(result.document_id) ||
    !nonemptyString(result.job_id) ||
    !isSHA256(result.raw_sha256) ||
    !isSHA256(result.attachment_sha256)
  ) {
    throw new Error("invalid M4 recovery email fixture result");
  }
}

function assertExactKeys(value, allowed, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  const keys = Object.keys(value);
  if (keys.length !== allowed.size || keys.some((key) => !allowed.has(key))) {
    throw new Error(`${label} has missing or unknown fields`);
  }
}

function nonemptyString(value) {
  return typeof value === "string" && value.length > 0 && value.length <= 512;
}

function canonicalTimestamp(value) {
  if (typeof value !== "string") return false;
  const milliseconds = Date.parse(value);
  return (
    Number.isFinite(milliseconds) &&
    new Date(milliseconds).toISOString() === value
  );
}

async function reserveProtectedOutput(path) {
  const handle = await open(
    path,
    constants.O_WRONLY |
      constants.O_CREAT |
      constants.O_EXCL |
      constants.O_NOFOLLOW,
    0o600,
  );
  let closed = false;
  const marker = Buffer.from(
    '{"kind":"smart-bill-manager-protected-output-in-progress","version":1}\n',
    "utf8",
  );
  try {
    await writeAllAt(handle, marker);
    await handle.truncate(marker.length);
    await handle.sync();
  } catch (error) {
    await handle.close().catch(() => {});
    closed = true;
    throw error;
  }

  async function finish(value) {
    if (closed) throw new Error("protected recovery output is already closed");
    await handle.truncate(0);
    await writeAllAt(handle, value);
    await handle.truncate(value.length);
    await handle.sync();
    await handle.close();
    closed = true;
  }

  return {
    async writeJSON(value, secrets) {
      const encoded = Buffer.from(
        `${JSON.stringify(value, null, 2)}\n`,
        "utf8",
      );
      try {
        for (const secret of secrets) {
          if (secret.length && encoded.includes(secret)) {
            throw new Error(
              "refusing to write recovery output containing a secret",
            );
          }
        }
        await finish(encoded);
      } finally {
        encoded.fill(0);
      }
    },
    async writeBytes(value) {
      if (!value.length || value.length > 16 * 1024) {
        throw new Error("protected recovery value has an invalid size");
      }
      await finish(value);
    },
    async close() {
      if (closed) return;
      await handle.close();
      closed = true;
    },
  };
}

async function writeAllAt(handle, value) {
  let offset = 0;
  while (offset < value.length) {
    const { bytesWritten } = await handle.write(
      value,
      offset,
      value.length - offset,
      offset,
    );
    if (bytesWritten < 1) {
      throw new Error("protected recovery output write made no progress");
    }
    offset += bytesWritten;
  }
}

async function readProtectedFile(path, maximumBytes) {
  const handle = await open(
    path,
    constants.O_RDONLY | constants.O_NOFOLLOW,
  ).catch(() => {
    throw new Error("open protected owner-only file");
  });
  const information = await handle.stat().catch(async () => {
    await handle.close().catch(() => {});
    throw new Error("inspect protected owner-only file");
  });
  if (
    !information.isFile() ||
    (information.mode & 0o077) !== 0 ||
    information.nlink !== 1
  ) {
    await handle.close();
    throw new Error(
      "protected file must be regular, owner-only, and singly linked",
    );
  }
  let content;
  try {
    content = await handle.readFile();
  } finally {
    await handle.close();
  }
  if (content.length < 1 || content.length > maximumBytes + 2) {
    throw new Error(`protected file size is invalid: ${path}`);
  }
  const end =
    content.at(-1) === 0x0a
      ? content.length - (content.at(-2) === 0x0d ? 2 : 1)
      : content.length;
  const result = Buffer.from(content.subarray(0, end));
  content.fill(0);
  if (result.length < 1 || result.length > maximumBytes) {
    result.fill(0);
    throw new Error(`protected file size is invalid: ${path}`);
  }
  return result;
}

function printSafeJSON(value) {
  process.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
}

function safeControllerOutput(phase, value) {
  switch (phase) {
    case "seed-capacity":
      return {
        phase,
        ordinary_document_count: value.ordinary_document_count,
        email_source_count: 1,
        passed: true,
      };
    case "seed-confirmed":
      return {
        phase,
        email_message_count: 1,
        email_attachment_count: 1,
        confirmed_fact_count: 1,
        passed: true,
      };
    case "stage-processing":
      return {
        phase,
        expected_document_count: value.expected_document_count,
        provider_extraction_observed:
          value.hanging_provider_extraction_observed === true,
        processing_job_count: 1,
        passed: true,
      };
    case "start-recovery":
      return { phase, recovery_clock_started: true, passed: true };
    case "verify-restore":
      return {
        phase,
        rto_elapsed_ms: value.rto_elapsed_ms,
        rto_limit_ms: value.rto_limit_ms,
        document_queries_verified: value.document_queries_verified,
        authenticated_downloads_verified:
          value.authenticated_downloads_verified,
        payment_count: value.payment_count,
        email_message_count: value.email_message_count,
        processing_attempt_delta:
          value.processing_attempt_count_after_recovery -
          value.processing_attempt_count_before_backup,
        passed: value.passed === true,
      };
    default:
      return { phase: "unknown", passed: false };
  }
}

function safeControllerErrorCode(error) {
  if (error instanceof ApiFailure) {
    const code = /^[a-z][a-z0-9_]{0,63}$/.test(error.code)
      ? error.code
      : "unknown_error";
    return `api_${error.status}_${code}`;
  }
  const message = error instanceof Error ? error.message.toLowerCase() : "";
  for (const category of [
    ["argument", "invalid_arguments"],
    ["required", "invalid_arguments"],
    ["protected", "protected_state_invalid"],
    ["provider", "synthetic_provider_failed"],
    ["login", "authentication_gate_failed"],
    ["session", "authentication_gate_failed"],
    ["timeout", "operation_timeout"],
    ["within", "operation_timeout"],
    ["recovery", "recovery_shape_invalid"],
    ["restored", "recovery_shape_invalid"],
    ["job", "processing_shape_invalid"],
    ["download", "download_verification_failed"],
  ]) {
    if (message.includes(category[0])) return category[1];
  }
  return "operation_failed";
}

function calculateRecoveryElapsed(
  state,
  monotonicNow = process.hrtime.bigint(),
  wallNow = Date.now(),
) {
  let monotonicStarted;
  try {
    monotonicStarted = BigInt(state.recovery_started_at_monotonic_ns);
  } catch {
    throw new Error("recovery clock is invalid");
  }
  const monotonicNanoseconds = monotonicNow - monotonicStarted;
  const maximumNanoseconds = BigInt(Number.MAX_SAFE_INTEGER) * 1_000_000n;
  const wallElapsed = wallNow - state.recovery_started_at_epoch_ms;
  if (
    monotonicNanoseconds < 0n ||
    monotonicNanoseconds > maximumNanoseconds ||
    !Number.isSafeInteger(wallElapsed) ||
    wallElapsed < 0
  ) {
    throw new Error("recovery clock is invalid or crossed a host reboot");
  }
  const monotonicElapsed = Number(monotonicNanoseconds / 1_000_000n);
  return Math.max(monotonicElapsed, wallElapsed);
}

function absoluteOptional(value) {
  return value ? resolve(value) : "";
}

function isSHA256(value) {
  return typeof value === "string" && /^[0-9a-f]{64}$/.test(value);
}

function ensureTrailingSlash(value) {
  return value.endsWith("/") ? value : `${value}/`;
}

function requireLoopbackURL(value, label) {
  const parsed = new URL(value);
  const host = parsed.hostname.toLowerCase();
  if (
    parsed.protocol !== "http:" ||
    !["127.0.0.1", "::1", "localhost"].includes(host) ||
    parsed.username ||
    parsed.password
  ) {
    throw new Error(`${label} URL must be credential-free loopback HTTP`);
  }
  return parsed;
}

function delay(milliseconds) {
  return new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));
}

export {
  ApiFailure,
  calculateRecoveryElapsed,
  crc32,
  parseArguments,
  safeControllerErrorCode,
  safeControllerOutput,
  syntheticPNG,
  validHangingProviderHealth,
  validateExerciseState,
  validateRestoredSnapshotBeforeContinuation,
  reserveProtectedOutput,
};

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  main().catch((error) => {
    process.stderr.write(
      `backup-exercise: ${safeControllerErrorCode(error)}:${activeControllerStage}\n`,
    );
    process.exitCode = 1;
  });
}
