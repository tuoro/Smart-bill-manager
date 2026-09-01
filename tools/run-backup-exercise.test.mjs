import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { inflateSync } from "node:zlib";

import {
  ApiFailure,
  calculateRecoveryElapsed,
  crc32,
  parseArguments,
  reserveProtectedOutput,
  safeControllerErrorCode,
  safeControllerOutput,
  syntheticPNG,
  validHangingProviderHealth,
  validateExerciseState,
  validateRestoredSnapshotBeforeContinuation,
} from "./run-backup-exercise.mjs";

test("capacity generator emits 1,000 distinct valid grayscale PNG files", () => {
  const hashes = new Set();
  for (let sequence = 1; sequence <= 1000; sequence += 1) {
    const content = syntheticPNG(sequence);
    const parsed = parsePNG(content);
    assert.deepEqual(parsed.signature, [137, 80, 78, 71, 13, 10, 26, 10]);
    assert.equal(parsed.width, 64);
    assert.equal(parsed.height, 64);
    assert.equal(parsed.bitDepth, 8);
    assert.equal(parsed.colorType, 0);
    assert.equal(parsed.scanlines.length, 64 * 65);
    for (let row = 0; row < 64; row += 1) {
      assert.equal(parsed.scanlines[row * 65], 0);
    }
    hashes.add(createHash("sha256").update(content).digest("hex"));
  }
  assert.equal(hashes.size, 1000);
});

test("protected outputs are reserved before mutation and finalized in place", async () => {
  const root = await mkdtemp(join(tmpdir(), "sbm-recovery-output-"));
  try {
    const output = join(root, "result.json");
    const reservation = await reserveProtectedOutput(output);
    assert.match(
      await readFile(output, "utf8"),
      /protected-output-in-progress/,
    );
    await assert.rejects(() => reserveProtectedOutput(output));
    await reservation.writeJSON({ kind: "complete", passed: true }, []);
    assert.deepEqual(JSON.parse(await readFile(output, "utf8")), {
      kind: "complete",
      passed: true,
    });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("exercise argument boundary rejects external destinations and unknown flags", () => {
  const base = [
    "--phase",
    "seed-capacity",
    "--output",
    "/tmp/output",
    "--server",
    "http://127.0.0.1:18080",
    "--email",
    "owner@example.invalid",
    "--password-file",
    "/tmp/password",
  ];
  assert.equal(parseArguments(base).server, "http://127.0.0.1:18080");
  assert.throws(
    () =>
      parseArguments(
        base.map((value) =>
          value === "http://127.0.0.1:18080"
            ? "https://provider.example.com"
            : value,
        ),
      ),
    /loopback/,
  );
  assert.throws(
    () => parseArguments([...base, "--unexpected", "value"]),
    /not valid/,
  );
  assert.throws(
    () =>
      parseArguments(
        base.map((value) =>
          value === "owner@example.invalid" ? "owner@example.com" : value,
        ),
      ),
    /\.invalid/,
  );
});

test("restore verification requires a distinct protected baseline output", () => {
  const base = [
    "--phase",
    "verify-restore",
    "--output",
    "/tmp/result.json",
    "--baseline-output",
    "/tmp/baseline.json",
    "--server",
    "http://127.0.0.1:18080",
    "--email",
    "owner@example.invalid",
    "--password-file",
    "/tmp/password",
    "--state",
    "/tmp/state.json",
    "--old-session-file",
    "/tmp/session",
  ];
  assert.equal(parseArguments(base).baselineOutput, "/tmp/baseline.json");
  assert.throws(
    () =>
      parseArguments(
        base.map((value) =>
          value === "/tmp/baseline.json" ? "/tmp/result.json" : value,
        ),
      ),
    /must differ/,
  );
  assert.throws(
    () =>
      parseArguments(
        base.filter(
          (value, index) =>
            value !== "--baseline-output" &&
            base[index - 1] !== "--baseline-output",
        ),
      ),
    /baseline-output.*required/,
  );
});

test("controller terminal output and errors omit protected identifiers and hashes", () => {
  const hash = "a".repeat(64);
  const state = {
    ordinary_document_count: 997,
    expected_document_count: 1000,
    hanging_provider_extraction_observed: true,
    tenant_id: "tenant-private",
    confirmed: { fact_id: "fact-private", sha256: hash },
  };
  for (const phase of [
    "seed-capacity",
    "seed-confirmed",
    "stage-processing",
    "start-recovery",
  ]) {
    const encoded = JSON.stringify(safeControllerOutput(phase, state));
    assert.doesNotMatch(encoded, /private/);
    assert.equal(encoded.includes(hash), false);
  }
  const verification = {
    recovered_fact_id: "fact-private",
    rto_elapsed_ms: 100,
    rto_limit_ms: 1000,
    document_queries_verified: 3,
    authenticated_downloads_verified: 5,
    payment_count: 2,
    email_message_count: 1,
    processing_attempt_count_before_backup: 1,
    processing_attempt_count_after_recovery: 2,
    passed: true,
  };
  const encoded = JSON.stringify(
    safeControllerOutput("verify-restore", verification),
  );
  assert.doesNotMatch(encoded, /private/);
  assert.equal(encoded.includes(hash), false);
  assert.equal(
    safeControllerErrorCode(
      new Error(`job job-private failed at /secure/private with ${hash}`),
    ),
    "processing_shape_invalid",
  );
  assert.equal(
    safeControllerErrorCode(
      new ApiFailure(409, { error: { code: "transaction_conflict" } }),
    ),
    "api_409_transaction_conflict",
  );
  assert.equal(
    safeControllerErrorCode(
      new ApiFailure(500, { error: { code: "unsafe/code" } }),
    ),
    "api_500_unknown_error",
  );
});

test("recovery clock is conservative across processes and rejects reset monotonic time", () => {
  const state = {
    recovery_started_at_epoch_ms: 1_000,
    recovery_started_at_monotonic_ns: "10000000000",
  };
  assert.equal(calculateRecoveryElapsed(state, 12_000_000_000n, 3_500), 2_500);
  assert.throws(
    () => calculateRecoveryElapsed(state, 9_000_000_000n, 3_500),
    /reboot/,
  );
  assert.throws(
    () => calculateRecoveryElapsed(state, 12_000_000_000n, 500),
    /reboot/,
  );
});

test("protected controller state has an exact phase-specific shape", () => {
  const state = {
    state_kind: "m4-backup-exercise-state",
    state_version: 1,
    exercise_id: "00000000-0000-4000-8000-000000000001",
    tenant_id: "tenant",
    user_id: "user",
    email_source_id: "source",
    ordinary_document_count: 997,
    ordinary_sample: { document_id: "ordinary", sha256: "a".repeat(64) },
  };
  assert.doesNotThrow(() => validateExerciseState(state));
  assert.throws(
    () => validateExerciseState({ ...state, unexpected: true }),
    /unknown/,
  );
  assert.throws(
    () => validateExerciseState(state, { requireConfirmed: true }),
    /missing/,
  );
});

test("hanging Provider evidence is bound to configured origin, model, and mode", () => {
  const state = {
    exercise_id: "00000000-0000-4000-8000-000000000001",
    provider_base_url_host: "127.0.0.1:19086",
    model: "synthetic-m4-recovery",
  };
  const health = {
    kind: "smart-bill-manager-synthetic-provider",
    version: 1,
    status: "ok",
    model: state.model,
    mode: "hang-extractions",
    exercise_id: state.exercise_id,
    instance_id: "00000000-0000-4000-8000-000000000001",
    requests: 1,
    probes: 0,
    extractions: 1,
  };
  assert.equal(
    validHangingProviderHealth(health, "http://127.0.0.1:19086/healthz", state),
    true,
  );
  assert.equal(
    validHangingProviderHealth(
      { ...health, requests: 0, probes: 0, extractions: 0 },
      "http://127.0.0.1:19086/healthz",
      state,
    ),
    true,
  );
  assert.equal(
    validHangingProviderHealth(
      { ...health, unexpected: true },
      "http://127.0.0.1:19086/healthz",
      state,
    ),
    false,
  );
  assert.throws(
    () =>
      validHangingProviderHealth(
        health,
        "http://127.0.0.1:19087/healthz",
        state,
      ),
    /origin/,
  );
  assert.equal(
    validHangingProviderHealth(
      { ...health, model: "synthetic-other" },
      "http://127.0.0.1:19086/healthz",
      state,
    ),
    false,
  );
  assert.equal(
    validHangingProviderHealth(
      { ...health, mode: "normal" },
      "http://127.0.0.1:19086/healthz",
      state,
    ),
    false,
  );
});

test("restored snapshot is verified before the worker can continue the leased job", () => {
  const state = {
    ordinary_sample: { document_id: "ordinary" },
    confirmed: { document_id: "confirmed", fact_id: "payment-before" },
    processing: {
      document_id: "processing",
      job_id: "job-processing",
      attempt_count_at_backup: 1,
    },
  };
  const restored = {
    payments: { items: [{ id: "payment-before" }] },
    emailMessages: { items: [{ id: "message" }] },
    ordinaryDocument: { id: "ordinary", status: "failed" },
    confirmedDocument: { id: "confirmed", status: "completed" },
    processingDocument: { id: "processing", status: "processing" },
    processingJob: {
      id: "job-processing",
      status: "processing",
      attempt_count: 1,
    },
  };
  assert.doesNotThrow(() =>
    validateRestoredSnapshotBeforeContinuation(state, restored),
  );
  assert.throws(
    () =>
      validateRestoredSnapshotBeforeContinuation(state, {
        ...restored,
        processingJob: { ...restored.processingJob, attempt_count: 2 },
      }),
    /before task continuation/,
  );
  assert.throws(
    () =>
      validateRestoredSnapshotBeforeContinuation(state, {
        ...restored,
        processingDocument: {
          ...restored.processingDocument,
          status: "completed",
        },
      }),
    /before task continuation/,
  );
});

function parsePNG(content) {
  const signature = [...content.subarray(0, 8)];
  const idat = [];
  let width;
  let height;
  let bitDepth;
  let colorType;
  let offset = 8;
  while (offset < content.length) {
    const size = content.readUInt32BE(offset);
    const type = content.subarray(offset + 4, offset + 8);
    const data = content.subarray(offset + 8, offset + 8 + size);
    const expectedCRC = content.readUInt32BE(offset + 8 + size);
    assert.equal(crc32(Buffer.concat([type, data])), expectedCRC);
    const name = type.toString("ascii");
    if (name === "IHDR") {
      width = data.readUInt32BE(0);
      height = data.readUInt32BE(4);
      bitDepth = data[8];
      colorType = data[9];
    } else if (name === "IDAT") {
      idat.push(data);
    } else if (name === "IEND") {
      offset += size + 12;
      break;
    }
    offset += size + 12;
  }
  assert.equal(offset, content.length);
  return {
    signature,
    width,
    height,
    bitDepth,
    colorType,
    scanlines: inflateSync(Buffer.concat(idat)),
  };
}
