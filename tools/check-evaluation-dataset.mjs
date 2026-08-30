#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFile, readdir, stat } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const projectDirectory = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const manifestPath = resolve(
  projectDirectory,
  "tests/evaluation/manifest-v2.json",
);
const sourceManifestPath = resolve(
  projectDirectory,
  "tests/evaluation/manifest-v1.json",
);
const sourceManifestSHA256 =
  "1ca338b15f5c060819323f5dd584758bba6a7e7d65460514a47857cac1a550ed";
const manifestBytes = await readFile(manifestPath);
const manifest = JSON.parse(manifestBytes);
const sourceManifestBytes = await readFile(sourceManifestPath);
const sourceManifest = JSON.parse(sourceManifestBytes);

if (
  manifest.dataset_version !== "m1-synthetic-v2" ||
  manifest.synthetic_only !== true ||
  manifest.intended_use !== "m1_release_model_evaluation" ||
  manifest.supersedes_dataset_version !== "m1-synthetic-v1" ||
  manifest.source_manifest_sha256 !== sourceManifestSHA256 ||
  manifest.asset_policy !== "reuse_immutable_m1_synthetic_v1_assets"
) {
  throw new Error("dataset identity or synthetic-only declaration is invalid");
}
if (
  sha256(sourceManifestBytes) !== sourceManifestSHA256 ||
  sourceManifest.dataset_version !== "m1-synthetic-v1" ||
  sourceManifest.samples?.length !== 100
) {
  throw new Error("frozen v1 source manifest changed");
}
if (!Array.isArray(manifest.samples) || manifest.samples.length !== 100) {
  throw new Error(`sample count = ${manifest.samples?.length ?? 0}, want 100`);
}

const ids = new Set();
const files = new Set();
const hashes = new Set();
const types = new Map();
const tags = new Map();
const sourceByID = new Map(
  sourceManifest.samples.map((sample) => [sample.sample_id, sample]),
);
let correctedPayments = 0;
for (const sample of manifest.samples) {
  if (ids.has(sample.sample_id))
    throw new Error(`duplicate sample ID: ${sample.sample_id}`);
  if (files.has(sample.file))
    throw new Error(`duplicate sample file: ${sample.file}`);
  if (hashes.has(sample.sha256))
    throw new Error(`duplicate sample hash: ${sample.sample_id}`);
  ids.add(sample.sample_id);
  files.add(sample.file);
  hashes.add(sample.sha256);
  types.set(sample.document_type, (types.get(sample.document_type) ?? 0) + 1);
  for (const tag of sample.scenario_tags)
    tags.set(tag, (tags.get(tag) ?? 0) + 1);
  const assetPath = resolve(dirname(manifestPath), sample.file);
  const information = await stat(assetPath);
  if (!information.isFile() || (information.mode & 0o077) !== 0) {
    throw new Error(`${sample.sample_id}: release asset must be owner-only`);
  }
  const content = await readFile(assetPath);
  if (sha256(content) !== sample.sha256)
    throw new Error(`asset hash mismatch: ${sample.sample_id}`);
  const source = sourceByID.get(sample.sample_id);
  if (
    !source ||
    source.file !== sample.file ||
    source.sha256 !== sample.sha256
  ) {
    throw new Error(`${sample.sample_id}: immutable v1 asset identity changed`);
  }
  assertOnlyApprovedCorrection(source, sample);
  for (const path of Object.keys(sample.expected_evidence)) {
    if (!Object.hasOwn(sample.expected_fields, path)) {
      throw new Error(
        `${sample.sample_id}: evidence exists for an unexpected field ${path}`,
      );
    }
  }
  for (const path of Object.keys(sample.expected_fields)) {
    if (!Object.hasOwn(sample.expected_evidence, path)) {
      throw new Error(
        `${sample.sample_id}: expected field has no evidence: ${path}`,
      );
    }
  }
  for (const path of sample.expected_missing_fields) {
    if (Object.hasOwn(sample.expected_fields, path)) {
      throw new Error(
        `${sample.sample_id}: field is both expected and missing: ${path}`,
      );
    }
  }
  if (sample.document_type === "payment") {
    const index = Number(sample.sample_id.slice(4));
    const orderNumber = `SYN-PAY-${String(index).padStart(4, "0")}`;
    if (
      sample.expected_fields.order_number !== orderNumber ||
      sample.expected_missing_fields.includes("order_number") ||
      sample.expected_evidence.order_number?.page !== 1 ||
      sample.expected_evidence.order_number?.quote !== orderNumber
    ) {
      throw new Error(
        `${sample.sample_id}: order_number correction is invalid`,
      );
    }
    correctedPayments += 1;
  }
  if (
    sample.model_stage_eligible === Boolean(sample.expected_failure_category)
  ) {
    throw new Error(
      `${sample.sample_id}: model eligibility and failure category conflict`,
    );
  }
}
if (correctedPayments !== 40) {
  throw new Error(`corrected payment count = ${correctedPayments}, want 40`);
}

const assetDirectory = resolve(
  projectDirectory,
  "tests/evaluation/assets/m1-synthetic-v1",
);
const diskFiles = await readdir(assetDirectory);
if (
  diskFiles.length !== files.size ||
  diskFiles.some((name) => !files.has(`assets/m1-synthetic-v1/${name}`))
) {
  throw new Error(
    "evaluation asset directory contains missing or unlisted files",
  );
}
if ((types.get("payment") ?? 0) < 40 || (types.get("invoice") ?? 0) < 40) {
  throw new Error("payment or invoice sample count is below 40");
}
for (const tag of [
  "payment_screenshot",
  "single_item_invoice",
  "multi_item_invoice",
  "low_quality_conflict",
  "invalid_unsupported",
]) {
  if ((tags.get(tag) ?? 0) < 15)
    throw new Error(`${tag} sample count is below 15`);
}

const eligible = manifest.samples.filter(
  (sample) => sample.model_stage_eligible,
).length;
process.stdout.write(`dataset: ${manifest.dataset_version}\n`);
process.stdout.write(`manifest_sha256: ${sha256(manifestBytes)}\n`);
process.stdout.write(
  `samples: ${manifest.samples.length} (payment=${types.get("payment")}, invoice=${types.get("invoice")}, unknown=${types.get("unknown")})\n`,
);
process.stdout.write(
  `model_stage_eligible: ${eligible}; rejected_before_model: ${manifest.samples.length - eligible}\n`,
);
process.stdout.write(`source_manifest_sha256: ${sourceManifestSHA256}\n`);
process.stdout.write(
  `corrected_payment_order_annotations: ${correctedPayments}\n`,
);
process.stdout.write(
  `scenarios: ${[...tags.entries()]
    .sort()
    .map(([name, count]) => `${name}=${count}`)
    .join(", ")}\n`,
);
process.stdout.write("evaluation dataset gate passed\n");

function assertOnlyApprovedCorrection(source, current) {
  if (current.document_type !== "payment") {
    if (JSON.stringify(current) !== JSON.stringify(source)) {
      throw new Error(`${current.sample_id}: non-payment annotation changed`);
    }
    return;
  }
  const reverted = structuredClone(current);
  delete reverted.expected_fields.order_number;
  delete reverted.expected_evidence.order_number;
  reverted.expected_missing_fields = source.expected_missing_fields;
  if (JSON.stringify(reverted) !== JSON.stringify(source)) {
    throw new Error(
      `${current.sample_id}: change exceeds approved correction scope`,
    );
  }
}

function sha256(content) {
  return createHash("sha256").update(content).digest("hex");
}
