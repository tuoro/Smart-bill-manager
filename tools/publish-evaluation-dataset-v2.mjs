#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const projectDirectory = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sourceManifestPath = resolve(
  projectDirectory,
  "tests/evaluation/manifest-v1.json",
);
const defaultOutputPath = resolve(
  projectDirectory,
  "tests/evaluation/manifest-v2.json",
);
const sourceManifestSHA256 =
  "1ca338b15f5c060819323f5dd584758bba6a7e7d65460514a47857cac1a550ed";

const outputPath = parseOutputPath(process.argv.slice(2));
const sourceBytes = await readFile(sourceManifestPath);
if (sha256(sourceBytes) !== sourceManifestSHA256) {
  throw new Error("frozen v1 source manifest hash changed");
}
const source = JSON.parse(sourceBytes);
if (
  source.dataset_version !== "m1-synthetic-v1" ||
  source.synthetic_only !== true ||
  source.samples?.length !== 100
) {
  throw new Error("source is not the approved frozen v1 dataset");
}

let correctedPayments = 0;
const samples = source.samples.map((sample) => {
  if (sample.document_type !== "payment") return structuredClone(sample);

  const match = /^PAY-([0-9]{3})$/.exec(sample.sample_id);
  if (
    !match ||
    Object.hasOwn(sample.expected_fields, "order_number") ||
    !sample.expected_missing_fields.includes("order_number") ||
    Object.hasOwn(sample.expected_evidence, "order_number")
  ) {
    throw new Error(
      `${sample.sample_id}: frozen v1 order_number contradiction shape changed`,
    );
  }
  const orderNumber = `SYN-PAY-${String(Number(match[1])).padStart(4, "0")}`;
  correctedPayments += 1;
  return {
    ...structuredClone(sample),
    expected_fields: sortedObject({
      ...sample.expected_fields,
      order_number: orderNumber,
    }),
    expected_missing_fields: sample.expected_missing_fields.filter(
      (path) => path !== "order_number",
    ),
    expected_evidence: sortedObject({
      ...sample.expected_evidence,
      order_number: { page: 1, quote: orderNumber },
    }),
  };
});
if (correctedPayments !== 40) {
  throw new Error(`corrected payment count = ${correctedPayments}, want 40`);
}

const result = {
  dataset_version: "m1-synthetic-v2",
  frozen_at: "2026-08-29T00:00:00Z",
  synthetic_only: true,
  intended_use: "m1_release_model_evaluation",
  generator: "tools/publish-evaluation-dataset-v2.mjs",
  supersedes_dataset_version: source.dataset_version,
  source_manifest_sha256: sourceManifestSHA256,
  asset_policy: "reuse_immutable_m1_synthetic_v1_assets",
  correction_scope: [
    "payment.order_number expected value",
    "payment.order_number expected evidence",
    "payment.order_number removed from expected missing fields",
  ],
  samples,
};
const encoded = `${JSON.stringify(result, null, 2)}\n`;
await writeFile(outputPath, encoded, {
  encoding: "utf8",
  flag: "wx",
  mode: 0o600,
});
process.stdout.write(
  `published ${result.dataset_version}: ${correctedPayments} corrected payment annotations\n`,
);
process.stdout.write(`manifest_sha256: ${sha256(encoded)}\n`);

function parseOutputPath(argumentsList) {
  if (argumentsList.length === 0) return defaultOutputPath;
  if (
    argumentsList.length !== 2 ||
    argumentsList[0] !== "--output" ||
    !argumentsList[1]
  ) {
    throw new Error("usage: publish-evaluation-dataset-v2.mjs [--output PATH]");
  }
  const result = resolve(argumentsList[1]);
  if (result === sourceManifestPath) {
    throw new Error("refusing to overwrite the frozen v1 manifest");
  }
  return result;
}

function sortedObject(value) {
  return Object.fromEntries(
    Object.entries(value).sort(([left], [right]) => left.localeCompare(right)),
  );
}

function sha256(content) {
  return createHash("sha256").update(content).digest("hex");
}
