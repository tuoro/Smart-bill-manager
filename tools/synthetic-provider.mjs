#!/usr/bin/env node

import { randomUUID, timingSafeEqual } from "node:crypto";
import { constants } from "node:fs";
import { open } from "node:fs/promises";
import { createServer } from "node:http";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const maxRequestBytes = 24 * 1024 * 1024;
const providerKind = "smart-bill-manager-synthetic-provider";
const providerVersion = 1;

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const apiKey = await readProtectedSecret(options.apiKeyFile, 4096);
  const instanceID = randomUUID();
  const counters = { requests: 0, probes: 0, extractions: 0 };
  const latencies = { probes: [], extractions: [] };
  const server = createServer(async (request, response) => {
    try {
      if (request.method === "GET" && request.url === "/healthz") {
        return writeJSON(response, 200, {
          kind: providerKind,
          version: providerVersion,
          status: "ok",
          model: options.model,
          mode: options.mode,
          exercise_id: options.exerciseID,
          instance_id: instanceID,
          ...counters,
        });
      }
      if (request.method === "GET" && request.url === "/metrics") {
        return writeJSON(response, 200, {
          kind: `${providerKind}-metrics`,
          version: providerVersion,
          model: options.model,
          mode: options.mode,
          exercise_id: options.exerciseID,
          instance_id: instanceID,
          ...counters,
          probe_latency_ms: summarizeLatency(latencies.probes),
          extraction_latency_ms: summarizeLatency(latencies.extractions),
        });
      }
      if (request.method !== "POST" || request.url !== "/v1/chat/completions") {
        return writeJSON(response, 404, { error: { message: "not found" } });
      }
      if (!authorized(request.headers.authorization, apiKey)) {
        return writeJSON(response, 401, { error: { message: "unauthorized" } });
      }
      const started = process.hrtime.bigint();
      const body = JSON.parse(
        (await readRequest(request, maxRequestBytes)).toString("utf8"),
      );
      if (
        body.model !== options.model ||
        body.temperature !== 0 ||
        body.response_format?.type !== "json_schema" ||
        body.response_format?.json_schema?.schema?.properties?.schema_version
          ?.const !== "bill-visible-text/2" ||
        !body.response_format?.json_schema?.schema?.properties?.payment ||
        !body.response_format?.json_schema?.schema?.properties?.trip ||
        body.response_format?.json_schema?.schema?.properties?.evidence ||
        body.response_format?.json_schema?.schema?.additionalProperties !==
          false ||
        JSON.stringify(body.response_format?.json_schema?.schema).includes(
          "amount_minor",
        ) ||
        JSON.stringify(body.messages).includes('"source"')
      ) {
        return writeJSON(response, 400, {
          error: { message: "invalid synthetic request" },
        });
      }
      counters.requests += 1;
      const probe = JSON.stringify(body.messages).includes(
        "If and only if the square is blue",
      );
      const envelope = probe
        ? capabilityEnvelope()
        : paymentEnvelope(counters.extractions + 1);
      if (probe) counters.probes += 1;
      else counters.extractions += 1;
      response.once("finish", () => {
        const milliseconds =
          Number(process.hrtime.bigint() - started) / 1_000_000;
        latencies[probe ? "probes" : "extractions"].push(milliseconds);
      });
      if (!probe && options.mode === "hang-extractions") {
        await new Promise((resolveClose) =>
          response.once("close", resolveClose),
        );
        return;
      }
      return writeJSON(response, 200, {
        id: `synthetic-${counters.requests}`,
        object: "chat.completion",
        created: 0,
        model: options.model,
        choices: [
          {
            index: 0,
            message: { role: "assistant", content: JSON.stringify(envelope) },
            finish_reason: "stop",
          },
        ],
        usage: { prompt_tokens: 100, completion_tokens: 80 },
      });
    } catch (error) {
      const status = error?.code === "request_too_large" ? 413 : 400;
      return writeJSON(response, status, {
        error: { message: "invalid request" },
      });
    }
  });
  server.requestTimeout = 65_000;
  server.headersTimeout = 10_000;
  server.listen(options.port, options.host, () => {
    process.stdout.write(
      `${JSON.stringify({ status: "ready", address: `${options.host}:${options.port}` })}\n`,
    );
  });
  let shuttingDown = false;
  const shutdown = () => {
    if (shuttingDown) return;
    shuttingDown = true;
    server.close((error) => {
      apiKey.fill(0);
      if (error) process.stderr.write("synthetic-provider: shutdown_failed\n");
      process.exitCode = error ? 1 : 0;
    });
    setTimeout(() => server.closeAllConnections(), 1_000).unref();
  };
  process.once("SIGINT", shutdown);
  process.once("SIGTERM", shutdown);
}

function capabilityEnvelope() {
  return {
    schema_version: "bill-visible-text/2",
    document_type: "unknown",
    payment: null,
    invoice: null,
    trip: null,
  };
}

function paymentEnvelope(sequence) {
  const suffix = sequence === 1 ? "" : ` ${String(sequence).padStart(3, "0")}`;
  return {
    schema_version: "bill-visible-text/2",
    document_type: "payment",
    payment: {
      amount: { text: "CNY 123.45", page: 1 },
      currency: { text: "CNY", page: 1 },
      merchant: { text: `Synthetic Memory Merchant${suffix}`, page: 1 },
      transaction_time: { text: "2026-08-28 09:00", page: 1 },
      timezone: null,
      payment_method: null,
      order_number: null,
      category: null,
    },
    invoice: null,
    trip: null,
  };
}

function authorized(value, expected) {
  if (!value?.startsWith("Bearer ")) return false;
  const provided = Buffer.from(value.slice("Bearer ".length), "utf8");
  try {
    return (
      provided.length === expected.length && timingSafeEqual(provided, expected)
    );
  } finally {
    provided.fill(0);
  }
}

async function readRequest(request, maximumBytes) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > maximumBytes) {
      const error = new Error("request too large");
      error.code = "request_too_large";
      throw error;
    }
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, size);
}

function writeJSON(response, status, body) {
  if (response.headersSent) return;
  const encoded = Buffer.from(JSON.stringify(body));
  response.writeHead(status, {
    "Content-Type": "application/json",
    "Content-Length": encoded.length,
    "Cache-Control": "no-store",
  });
  response.end(encoded);
}

function summarizeLatency(values) {
  if (values.length === 0) {
    return { samples: 0, p50: null, p95: null, max: null };
  }
  const sorted = [...values].sort((left, right) => left - right);
  const percentile = (ratio) =>
    sorted[Math.max(0, Math.ceil(sorted.length * ratio) - 1)];
  const round = (value) => Math.round(value * 1_000) / 1_000;
  return {
    samples: sorted.length,
    p50: round(percentile(0.5)),
    p95: round(percentile(0.95)),
    max: round(sorted.at(-1)),
  };
}

function parseArguments(argumentsList) {
  const values = new Map();
  for (let index = 0; index < argumentsList.length; index += 2) {
    const key = argumentsList[index];
    const value = argumentsList[index + 1];
    if (!key?.startsWith("--") || value === undefined)
      throw new Error(`invalid argument near ${key ?? "<end>"}`);
    const name = key.slice(2);
    if (values.has(name)) throw new Error(`duplicate --${name}`);
    values.set(name, value);
  }
  const allowed = new Set([
    "listen",
    "api-key-file",
    "model",
    "mode",
    "exercise-id",
  ]);
  for (const name of values.keys()) {
    if (!allowed.has(name)) throw new Error(`unknown --${name}`);
  }
  for (const name of ["listen", "api-key-file", "model", "exercise-id"]) {
    if (!values.get(name)) throw new Error(`--${name} is required`);
  }
  const match = /^(127\.0\.0\.1|::1):(\d+)$/.exec(values.get("listen"));
  if (!match)
    throw new Error("--listen must use loopback address and an explicit port");
  const port = Number(match[2]);
  if (!Number.isInteger(port) || port < 1024 || port > 65535)
    throw new Error("--listen port is invalid");
  const mode = values.get("mode") ?? "normal";
  if (mode !== "normal" && mode !== "hang-extractions")
    throw new Error("--mode must be normal or hang-extractions");
  if (!/^synthetic-[a-z0-9._-]+$/.test(values.get("model"))) {
    throw new Error("--model must use a synthetic-* identity");
  }
  if (
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      values.get("exercise-id"),
    )
  ) {
    throw new Error("--exercise-id must be a UUIDv4");
  }
  return {
    host: match[1],
    port,
    apiKeyFile: values.get("api-key-file"),
    model: values.get("model"),
    mode,
    exerciseID: values.get("exercise-id"),
  };
}

async function readProtectedSecret(path, maximumBytes) {
  const handle = await open(
    path,
    constants.O_RDONLY | constants.O_NOFOLLOW,
  ).catch(() => {
    throw new Error("open protected API key file");
  });
  const information = await handle.stat().catch(async () => {
    await handle.close().catch(() => {});
    throw new Error("inspect protected API key file");
  });
  if (
    !information.isFile() ||
    (information.mode & 0o077) !== 0 ||
    information.nlink !== 1
  ) {
    await handle.close();
    throw new Error("API key file must be regular and owner-only");
  }
  let content;
  try {
    content = await handle.readFile();
  } finally {
    await handle.close();
  }
  const end =
    content.at(-1) === 0x0a
      ? content.length - (content.at(-2) === 0x0d ? 2 : 1)
      : content.length;
  const result = Buffer.from(content.subarray(0, end));
  content.fill(0);
  if (result.length < 1 || result.length > maximumBytes) {
    result.fill(0);
    throw new Error("API key file size is invalid");
  }
  return result;
}

function safeProviderErrorCode(error) {
  const message = error instanceof Error ? error.message.toLowerCase() : "";
  if (
    message.includes("argument") ||
    message.includes("required") ||
    message.includes("listen") ||
    message.includes("model") ||
    message.includes("mode") ||
    message.includes("unknown") ||
    message.includes("duplicate")
  ) {
    return "invalid_arguments";
  }
  if (message.includes("api key") || message.includes("protected")) {
    return "protected_key_invalid";
  }
  return "startup_failed";
}

export { parseArguments, paymentEnvelope, safeProviderErrorCode, summarizeLatency };

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  main().catch((error) => {
    process.stderr.write(
      `synthetic-provider: ${safeProviderErrorCode(error)}\n`,
    );
    process.exitCode = 1;
  });
}
