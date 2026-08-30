#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFile, stat, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const toolDirectory = dirname(fileURLToPath(import.meta.url));
const projectDirectory = resolve(toolDirectory, "..");
const defaultReleaseManifest = resolve(
  projectDirectory,
  "tests/evaluation/manifest-v2.json",
);
const defaultTuningManifest = resolve(
  projectDirectory,
  "tests/evaluation/tuning/manifest-v2.json",
);
const syntheticTuningManifest = defaultTuningManifest;
const historicalTuningManifest = resolve(
  projectDirectory,
  "tests/evaluation/tuning/manifest-v1.json",
);
const releaseManifestSHA256 =
  "08ce3ea739eaa482ba8410ccf71b9f3f1806bbf613d0c960549ae9110566c91d";
const approvedTuningManifestSHA256 = new Map([
  [
    "m1-prompt-dev-v2",
    "76c9eee0672ecad26bdc2940c81c25c8def9918542259374e4d26b8d418acacd",
  ],
]);
const blockedRealTuningDatasets = new Map([
  [
    "m1-real-dev-v5",
    "m1-real-dev-v5 is frozen diagnostic evidence; its copied v4 labels do not represent the visible-text currency boundary or the fixed supplementary_fields Claim path, so a corrected successor must be approved before another Provider preflight",
  ],
  [
    "m1-real-dev-v4",
    "m1-real-dev-v4 is frozen evidence for the retired bill-extraction/2 contract; publish and approve m1-real-dev-v5 before any visible-text Provider preflight",
  ],
  [
    "m1-real-dev-v3",
    "m1-real-dev-v3 is frozen historical diagnostic evidence and cannot be sent to a Provider",
  ],
  [
    "m1-real-dev-v1",
    "m1-real-dev-v1 is retired for a business-scope mismatch; freeze m1-real-dev-v2 before any new Provider preflight",
  ],
  [
    "m1-real-dev-v2-candidate",
    "m1-real-dev-v2 candidate snapshot is not the frozen development dataset; use the approved frozen v2 manifest",
  ],
  [
    "m1-real-dev-v2",
    "the legacy direct-Claim Provider path is retired and cannot be sent to a Provider",
  ],
  [
    "m1-real-dev-v2-prompt-v8-canary",
    "Prompt v8 is a frozen historical diagnostic and cannot be sent to a Provider",
  ],
  [
    "m1-real-dev-v2-prompt-v9-canary",
    "Prompt v9 is a frozen historical diagnostic and cannot be sent to a Provider",
  ],
  [
    "m1-real-dev-v2-prompt-v10-canary",
    "Prompt v10 was stopped before Provider execution and cannot be sent to a Provider",
  ],
]);
const frozenComparisonManifests = [
  {
    path: resolve(projectDirectory, "tests/evaluation/manifest-v1.json"),
    sha256: "1ca338b15f5c060819323f5dd584758bba6a7e7d65460514a47857cac1a550ed",
  },
  { path: defaultReleaseManifest, sha256: releaseManifestSHA256 },
  {
    path: historicalTuningManifest,
    sha256: "1bea5129832ea2e536792f7b023c1df61f4f9113ed180de66b67b7a7724d89f3",
  },
  {
    path: syntheticTuningManifest,
    sha256: "76c9eee0672ecad26bdc2940c81c25c8def9918542259374e4d26b8d418acacd",
  },
];
const currentPromptVersion = "bill-visible-text-cn/1";
const extractionSchemaVersion = "bill-visible-text/1";
const providerSchemaVersion = "bill-visible-text-provider/1";
const claimSchemaVersion = "document-claim/2";
const claimMapperVersion = "claim-mapper/3";
const providerOutputRetryPolicy = "schema_validation_single_retry/1";
const terminalJobStates = new Set([
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
    this.name = "ApiFailure";
    this.status = status;
    this.code = body?.error?.code ?? "unknown_error";
    this.requestId = body?.request_id ?? "";
  }
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const manifest = JSON.parse(await readFile(options.manifest, "utf8"));
  await validateManifest(manifest, options.manifest, options);
  const password = await readProtectedSecret(options.passwordFile, 1024);
  const providerApiKey = await readProtectedSecret(
    options.providerApiKeyFile,
    4096,
  );
  const client = createClient(options.server);
  try {
    await client.login(options.email, password);
    const initialJobs = await client.get("/jobs");
    if (initialJobs.items.length !== 0) {
      throw new Error(
        "model evaluation requires a fresh workspace with zero jobs",
      );
    }
    const provider = await client.createProvider(
      options.providerBaseUrl,
      providerApiKey,
      options.model,
      options.outputMode,
    );
    const detected = await client.mutate(
      `/provider-configs/${encodeURIComponent(provider.id)}/detect`,
      {
        method: "POST",
        timeoutMs: 90_000,
      },
    );
    if (detected.capability_status !== "passed") {
      throw new Error(
        `provider capability detection failed: ${detected.capability_status}: ${detected.capability_safe_message ?? "no safe message"}`,
      );
    }
    const active = await client.mutate(
      `/provider-configs/${encodeURIComponent(provider.id)}/activate`,
      {
        method: "POST",
      },
    );
    if (
      active.capability_schema_version !== providerSchemaVersion ||
      !/^[a-f0-9]{64}$/.test(active.capability_schema_sha256 ?? "")
    ) {
      throw new Error(
        "active provider is missing its frozen Provider schema identity",
      );
    }
    const startedAt = new Date().toISOString();
    const extractions = await mapLimit(
      manifest.samples,
      options.concurrency,
      async (sample, index) => {
        const extraction = await evaluateSample(
          client,
          sample,
          options.manifest,
        );
        process.stderr.write(
          `[${index + 1}/${manifest.samples.length}] ${sample.sample_id}: ${extraction.outcome}\n`,
        );
        return extraction;
      },
    );
    const [payments, invoices] = await Promise.all([
      client.get("/payments"),
      client.get("/invoices"),
    ]);
    const result = {
      result_kind:
        options.profile === "tuning-preflight"
          ? "m1-model-tuning-preflight-run"
          : "m1-model-evaluation-run",
      run_id: options.runId,
      dataset_version: manifest.dataset_version,
      dataset_manifest_sha256: sha256(await readFile(options.manifest)),
      eligible_for_release_evidence: options.profile === "release",
      started_at: startedAt,
      finished_at: new Date().toISOString(),
      frozen_configuration: {
        provider_config_safe_fingerprint: active.safe_fingerprint,
        provider_base_url_host: new URL(options.providerBaseUrl).host,
        model: options.model,
        output_mode: options.outputMode,
        prompt_version: currentPromptVersion,
        extraction_schema_version: extractionSchemaVersion,
        provider_schema_version: active.capability_schema_version,
        provider_schema_sha256: active.capability_schema_sha256,
        claim_schema_version: claimSchemaVersion,
        claim_mapper_version: claimMapperVersion,
        input_processing_version: "document-normalize/2",
        provider_output_retry_policy: providerOutputRetryPolicy,
        connection_timeout_seconds: 10,
        model_timeout_seconds: 60,
        job_timeout_seconds: 150,
        temperature: 0,
        seed: "provider_not_supported",
      },
      ai_direct_fact_count: payments.items.length + invoices.items.length,
      samples: extractions,
    };
    const encoded = `${JSON.stringify(result, null, 2)}\n`;
    if (encoded.includes(password) || encoded.includes(providerApiKey)) {
      throw new Error(
        "refusing to write evaluation output containing a secret",
      );
    }
    await writeFile(options.output, encoded, {
      encoding: "utf8",
      flag: "wx",
      mode: 0o600,
    });
    process.stdout.write(
      `wrote ${extractions.length} extraction results to ${options.output}\n`,
    );
  } finally {
    password.fill(0);
    providerApiKey.fill(0);
  }
}

async function evaluateSample(client, sample, manifestPath) {
  const assetPath = resolve(dirname(manifestPath), sample.file);
  const content = await readFile(assetPath);
  const startedAt = Date.now();
  let uploaded;
  try {
    uploaded = await client.upload(
      sample.original_name,
      sample.declared_mime,
      content,
    );
  } catch (error) {
    if (!(error instanceof ApiFailure)) throw error;
    return {
      sample_id: sample.sample_id,
      outcome: "rejected_before_model",
      accepted_upload: false,
      error_code: error.code,
      http_status: error.status,
      request_id: error.requestId,
      elapsed_ms: Date.now() - startedAt,
    };
  }
  if (uploaded.sha256 !== sample.sha256) {
    throw new Error(`${sample.sample_id}: upload hash mismatch`);
  }
  const job = await waitForTerminalJob(client, uploaded.job_id);
  let review = null;
  if (job.status === "needs_review" || job.status === "blocked") {
    review = await client.get(
      `/reviews/${encodeURIComponent(uploaded.job_id)}`,
    );
  }
  return {
    sample_id: sample.sample_id,
    outcome: review ? "local_claim_accepted" : "job_terminal_without_claim",
    accepted_upload: true,
    document_id: uploaded.document_id,
    job_id: uploaded.job_id,
    job_status: job.status,
    attempt_count: job.attempt_count,
    error_code: job.error_code ?? "",
    safe_error_message: job.safe_error_message ?? "",
    elapsed_ms: Date.now() - startedAt,
    schema_valid: review !== null,
    claim:
      review === null
        ? null
        : {
            claim_set_id: review.claim_set_id,
            document_type: review.document_type,
            revision: review.revision,
            claim_status: review.claim_status,
            fields: review.fields.map((field) => ({
              path: field.path,
              value_type: field.value_type,
              presence: field.presence,
              value: field.value,
              normalized_value: field.normalized_value,
              source: field.source,
              evidence: field.evidence.map((entry) => ({
                page: entry.page,
                quote: entry.quote ?? "",
                region: entry.region ?? null,
              })),
            })),
            validations: review.validations.map((entry) => ({
              field_claim_id: entry.field_claim_id ?? "",
              rule_code: entry.rule_code,
              severity: entry.severity,
              status: entry.status,
            })),
          },
  };
}

async function waitForTerminalJob(client, jobId) {
  const deadline = Date.now() + 170_000;
  while (Date.now() < deadline) {
    const job = await client.get(`/jobs/${encodeURIComponent(jobId)}`);
    if (terminalJobStates.has(job.status)) return job;
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 500));
  }
  throw new Error(
    `job ${jobId} did not reach a terminal state within 170 seconds`,
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
    if (response.status === 204) return undefined;
    return response.json();
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
      const setCookies = response.headers.getSetCookie();
      cookie = setCookies.map((entry) => entry.split(";", 1)[0]).join("; ");
      const body = await response.json();
      csrf = body.csrf_token;
      if (!cookie || !csrf)
        throw new Error(
          "login response did not establish session and CSRF cookies",
        );
      return body;
    },
    get(path) {
      return request(path);
    },
    mutate(path, options = {}) {
      return request(path, { ...options, csrf: true });
    },
    createProvider(providerBaseUrl, apiKeyBytes, model, outputMode) {
      return request("/provider-configs", {
        method: "POST",
        csrf: true,
        json: {
          base_url: providerBaseUrl,
          api_key: apiKeyBytes.toString("utf8"),
          model,
          output_mode: outputMode,
        },
      });
    },
    upload(name, mime, content) {
      const form = new FormData();
      form.append("file", new Blob([content], { type: mime }), name);
      return request("/documents", {
        method: "POST",
        csrf: true,
        body: form,
        timeoutMs: 60_000,
      });
    },
  };
}

async function validateManifest(manifest, manifestPath, options) {
  const { profile } = options;
  const manifestHash = sha256(await readFile(manifestPath));
  if (profile === "tuning-preflight") {
    const blockedReason = blockedRealTuningDatasets.get(
      manifest.dataset_version,
    );
    if (blockedReason) throw new Error(blockedReason);
    const approvedHash = approvedTuningManifestSHA256.get(
      manifest.dataset_version,
    );
    const approvedIdentityValid = approvedHash === manifestHash;
    const commonTuningIdentityValid =
      manifest.samples?.length === 16 &&
      manifest.excluded_from_release_evidence === true &&
      manifest.intended_use === "prompt_provider_contract_tuning_only" &&
      Array.isArray(manifest.source_dataset_versions) &&
      manifest.source_dataset_versions.length === 0;
    const syntheticIdentityValid =
      manifest.dataset_version === "m1-prompt-dev-v2" &&
      manifest.synthetic_only === true &&
      manifest.supersedes_dataset_version === "m1-prompt-dev-v1";
    const realIdentityValid =
      manifest.dataset_version === "m1-real-dev-v5" &&
      manifest.synthetic_only === false &&
      manifest.real_world === true &&
      manifest.supersedes_dataset_version === "m1-real-dev-v4" &&
      manifest.prompt_contract === currentPromptVersion &&
      manifest.extraction_schema_contract === extractionSchemaVersion &&
      manifest.provider_schema_contract === providerSchemaVersion &&
      manifest.authoritative_schema_contract === claimSchemaVersion &&
      manifest.claim_mapper_contract === claimMapperVersion &&
      manifest.input_processing_contract === "document-normalize/2";
    if (
      !approvedIdentityValid ||
      !commonTuningIdentityValid ||
      (!syntheticIdentityValid && !realIdentityValid)
    ) {
      throw new Error("manifest is not an approved isolated M1 tuning dataset");
    }
  } else if (
    manifestHash !== releaseManifestSHA256 ||
    manifest.dataset_version !== "m1-synthetic-v2" ||
    manifest.synthetic_only !== true ||
    manifest.intended_use !== "m1_release_model_evaluation" ||
    manifest.supersedes_dataset_version !== "m1-synthetic-v1" ||
    manifest.samples?.length !== 100
  ) {
    throw new Error(
      "evaluation manifest is not the frozen M1 synthetic dataset",
    );
  }
  const forbiddenTuningHashes = new Set();
  if (profile === "tuning-preflight") {
    for (const compared of frozenComparisonManifests) {
      if (compared.sha256 === manifestHash) continue;
      const comparedBytes = await readFile(compared.path);
      if (sha256(comparedBytes) !== compared.sha256) {
        throw new Error(`comparison manifest is not frozen: ${compared.path}`);
      }
      const comparedManifest = JSON.parse(comparedBytes);
      for (const sample of comparedManifest.samples ?? []) {
        forbiddenTuningHashes.add(sample.sha256);
      }
    }
  }
  const ids = new Set();
  for (const sample of manifest.samples) {
    if (ids.has(sample.sample_id))
      throw new Error(`duplicate sample ID: ${sample.sample_id}`);
    ids.add(sample.sample_id);
    const assetPath = resolve(dirname(manifestPath), sample.file);
    const content = await readFile(assetPath);
    if (sha256(content) !== sample.sha256)
      throw new Error(`asset hash mismatch: ${sample.sample_id}`);
    if (manifest.synthetic_only === false) {
      await assertOwnerOnlyFile(assetPath, sample.sample_id);
    }
    if (forbiddenTuningHashes.has(sample.sha256))
      throw new Error(
        `tuning sample reuses a frozen release or historical tuning asset: ${sample.sample_id}`,
      );
  }
}

async function assertOwnerOnlyFile(path, label) {
  const information = await stat(path);
  if (!information.isFile() || (information.mode & 0o077) !== 0) {
    throw new Error(`${label} must be a regular owner-only file`);
  }
}

async function readProtectedSecret(path, maximumBytes) {
  const information = await stat(path);
  if (!information.isFile() || (information.mode & 0o077) !== 0) {
    throw new Error(`secret file must be regular and owner-only: ${path}`);
  }
  const content = await readFile(path);
  if (content.length < 1 || content.length > maximumBytes + 2) {
    throw new Error(`secret file size is invalid: ${path}`);
  }
  if (content.at(-1) === 0x0a) {
    const end =
      content.at(-2) === 0x0d ? content.length - 2 : content.length - 1;
    const result = Buffer.from(content.subarray(0, end));
    if (result.length < 1 || result.length > maximumBytes) {
      throw new Error(`secret file size is invalid: ${path}`);
    }
    return result;
  }
  if (content.length > maximumBytes)
    throw new Error(`secret file size is invalid: ${path}`);
  return Buffer.from(content);
}

async function mapLimit(items, limit, operation) {
  const result = new Array(items.length);
  let next = 0;
  async function worker() {
    while (true) {
      const index = next;
      next += 1;
      if (index >= items.length) return;
      result[index] = await operation(items[index], index);
    }
  }
  await Promise.all(
    Array.from({ length: Math.min(limit, items.length) }, () => worker()),
  );
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
  const required = [
    "server",
    "email",
    "password-file",
    "provider-base-url",
    "provider-api-key-file",
    "model",
    "output-mode",
    "run-id",
    "output",
  ];
  for (const name of required) {
    if (!values.get(name)) throw new Error(`--${name} is required`);
  }
  const profile = values.get("profile") ?? "release";
  if (!new Set(["release", "tuning-preflight"]).has(profile)) {
    throw new Error("--profile must be release or tuning-preflight");
  }
  const concurrency = Number(values.get("concurrency") ?? "2");
  if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 8) {
    throw new Error("--concurrency must be an integer between 1 and 8");
  }
  const outputMode = values.get("output-mode");
  if (!new Set(["json_schema", "json_object"]).has(outputMode)) {
    throw new Error("--output-mode must be json_schema or json_object");
  }
  const runId = values.get("run-id");
  if (
    (profile === "release" && !/^run-[1-3]$/.test(runId)) ||
    (profile === "tuning-preflight" && runId !== "preflight")
  ) {
    throw new Error(
      profile === "release"
        ? "--run-id must be run-1, run-2, or run-3"
        : "--run-id must be preflight for the tuning profile",
    );
  }
  return {
    server: values.get("server"),
    email: values.get("email"),
    passwordFile: resolve(values.get("password-file")),
    providerBaseUrl: values.get("provider-base-url"),
    providerApiKeyFile: resolve(values.get("provider-api-key-file")),
    model: values.get("model"),
    outputMode,
    runId,
    output: resolve(values.get("output")),
    manifest: resolve(
      values.get("manifest") ??
        (profile === "tuning-preflight"
          ? defaultTuningManifest
          : defaultReleaseManifest),
    ),
    concurrency,
    profile,
  };
}

function ensureTrailingSlash(value) {
  return value.endsWith("/") ? value : `${value}/`;
}

function sha256(content) {
  return createHash("sha256").update(content).digest("hex");
}

main().catch((error) => {
  process.stderr.write(
    `${error instanceof Error ? error.message : String(error)}\n`,
  );
  process.exitCode = 1;
});
