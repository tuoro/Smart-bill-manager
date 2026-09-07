import assert from "node:assert/strict";
import test from "node:test";

import {
  parseArguments,
  paymentEnvelope,
  reviewAllocationEnvelope,
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

test("review-allocation is explicit and preserves the existing default and hang modes", () => {
  const argumentsList = [
    "--listen",
    "127.0.0.1:19086",
    "--api-key-file",
    "/tmp/provider-key",
    "--model",
    "synthetic-throughflow",
    "--exercise-id",
    "00000000-0000-4000-8000-000000000001",
  ];
  assert.equal(parseArguments(argumentsList).mode, "normal");
  for (const mode of ["normal", "hang-extractions", "review-allocation"]) {
    assert.equal(parseArguments([...argumentsList, "--mode", mode]).mode, mode);
  }
  assert.throws(
    () => parseArguments([...argumentsList, "--mode", "unknown-mode"]),
    /--mode must be/,
  );
});

test("review-allocation starts with the complete fixed payment envelope", () => {
  assert.deepEqual(reviewAllocationEnvelope(1), {
    schema_version: "bill-visible-text/2",
    document_type: "payment",
    payment: {
      amount: { text: "CNY 100.00", page: 1 },
      currency: { text: "CNY", page: 1 },
      merchant: { text: "Synthetic Throughflow Merchant", page: 1 },
      transaction_time: { text: "2026-09-05 09:00", page: 1 },
      timezone: null,
      payment_method: null,
      order_number: { text: "SYNTHETIC-THROUGHFLOW-P-001", page: 1 },
      category: null,
    },
    invoice: null,
    trip: null,
  });
});

for (const [sequence, invoiceNumber, invoiceDate] of [
  [2, "90000000000000000001", "2026-09-04"],
  [3, "90000000000000000002", "2026-09-05"],
]) {
  test(`review-allocation response ${sequence} contains its distinct complete invoice`, () => {
    assert.deepEqual(reviewAllocationEnvelope(sequence), {
      schema_version: "bill-visible-text/2",
      document_type: "invoice",
      payment: null,
      invoice: {
        invoice_number: { text: invoiceNumber, page: 1 },
        invoice_date: { text: invoiceDate, page: 1 },
        amount_without_tax: null,
        tax_amount: null,
        amount_with_tax: { text: "CNY 60.00", page: 1 },
        currency: { text: "CNY", page: 1 },
        seller_name: { text: "Synthetic Throughflow Merchant", page: 1 },
        buyer_name: { text: "Synthetic Throughflow Buyer", page: 1 },
        items: [],
      },
      trip: null,
    });
  });
}

test("review-allocation fails explicitly after the third extraction without repeating a fixture", () => {
  for (const sequence of [
    4,
    5,
    Number.MAX_SAFE_INTEGER,
    0,
    -1,
    1.5,
    Number.NaN,
  ]) {
    assert.throws(() => reviewAllocationEnvelope(sequence), {
      name: "RangeError",
      code: "review_allocation_sequence_out_of_range",
      message: "review-allocation sequence must be an integer from 1 to 3",
    });
  }
});

test("review-allocation responses are independent and do not change legacy payment results", () => {
  const originalPayment = paymentEnvelope(1);
  const firstInvoice = reviewAllocationEnvelope(2);
  firstInvoice.invoice.invoice_number.text = "changed-synthetic-number";
  firstInvoice.invoice.items.push({
    name: { text: "changed-synthetic-item", page: 1 },
  });

  const repeatedInvoice = reviewAllocationEnvelope(2);
  assert.equal(
    repeatedInvoice.invoice.invoice_number.text,
    "90000000000000000001",
  );
  assert.deepEqual(repeatedInvoice.invoice.items, []);
  assert.deepEqual(paymentEnvelope(1), originalPayment);
  assert.equal(paymentEnvelope(1).payment.amount.text, "CNY 123.45");
  assert.equal(
    paymentEnvelope(1).payment.merchant.text,
    "Synthetic Memory Merchant",
  );
  assert.equal(
    paymentEnvelope(4).payment.merchant.text,
    "Synthetic Memory Merchant 004",
  );
});
