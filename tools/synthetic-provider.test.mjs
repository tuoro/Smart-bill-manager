import assert from "node:assert/strict";
import test from "node:test";

import {
  parseArguments,
  paymentEnvelope,
  safeProviderErrorCode,
  summarizeLatency,
} from "./synthetic-provider.mjs";

test("synthetic provider accepts only loopback and synthetic identities", () => {
  const valid = [
    "--listen",
    "127.0.0.1:19086",
    "--api-key-file",
    "/tmp/provider-key",
    "--model",
    "synthetic-m4-recovery",
    "--mode",
    "hang-extractions",
    "--exercise-id",
    "00000000-0000-4000-8000-000000000001",
  ];
  assert.equal(parseArguments(valid).mode, "hang-extractions");
  assert.throws(
    () =>
      parseArguments(
        valid.map((value) =>
          value === "127.0.0.1:19086" ? "0.0.0.0:19086" : value,
        ),
      ),
    /loopback/,
  );
  assert.throws(
    () =>
      parseArguments(
        valid.map((value) =>
          value === "synthetic-m4-recovery" ? "real-model" : value,
        ),
      ),
    /synthetic/,
  );
  assert.throws(
    () => parseArguments([...valid, "--listen", "127.0.0.1:19087"]),
    /duplicate/,
  );
  assert.throws(
    () => parseArguments([...valid, "--external", "true"]),
    /unknown/,
  );
});

test("synthetic provider error codes never echo protected paths or keys", () => {
  const protectedDetail = "/private/provider-key contains secret-value";
  const code = safeProviderErrorCode(new Error(`API key ${protectedDetail}`));
  assert.equal(code, "protected_key_invalid");
  assert.doesNotMatch(code, /private|secret|provider-key/);
});

test("synthetic provider latency metrics expose only aggregate percentiles", () => {
  assert.deepEqual(summarizeLatency([]), {
    samples: 0,
    p50: null,
    p95: null,
    max: null,
  });
  assert.deepEqual(summarizeLatency([4, 1, 2, 3]), {
    samples: 4,
    p50: 2,
    p95: 4,
    max: 4,
  });
});

test("synthetic provider keeps the representative first result and separates memory facts", () => {
  assert.equal(
    paymentEnvelope(1).payment.merchant.text,
    "Synthetic Memory Merchant",
  );
  assert.equal(
    paymentEnvelope(2).payment.merchant.text,
    "Synthetic Memory Merchant 002",
  );
});
