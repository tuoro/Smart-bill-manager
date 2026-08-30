#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFile, readdir, stat } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const projectDirectory = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const tuningManifestPath = resolve(
  projectDirectory,
  "tests/evaluation/tuning/manifest-v2.json",
);
const historicalManifestPath = resolve(
  projectDirectory,
  "tests/evaluation/tuning/manifest-v1.json",
);
const releaseManifestPath = resolve(
  projectDirectory,
  "tests/evaluation/manifest-v2.json",
);
const frozenHistoricalManifestSHA256 =
  "1bea5129832ea2e536792f7b023c1df61f4f9113ed180de66b67b7a7724d89f3";
const frozenTuningManifestSHA256 =
  "76c9eee0672ecad26bdc2940c81c25c8def9918542259374e4d26b8d418acacd";
const tuningManifestBytes = await readFile(tuningManifestPath);
const tuning = JSON.parse(tuningManifestBytes);
const historicalManifestBytes = await readFile(historicalManifestPath);
const historical = JSON.parse(historicalManifestBytes);
const release = JSON.parse(await readFile(releaseManifestPath, "utf8"));

if (
  release.dataset_version !== "m1-synthetic-v2" ||
  release.synthetic_only !== true ||
  release.samples?.length !== 100
) {
  throw new Error("release dataset identity is not the approved frozen v2");
}

if (
  sha256(historicalManifestBytes) !== frozenHistoricalManifestSHA256 ||
  historical.dataset_version !== "m1-prompt-dev-v1" ||
  historical.synthetic_only !== true ||
  historical.samples?.length !== 16
) {
  throw new Error("historical tuning v1 is not the frozen independent dataset");
}

if (
  sha256(tuningManifestBytes) !== frozenTuningManifestSHA256 ||
  tuning.dataset_version !== "m1-prompt-dev-v2" ||
  tuning.synthetic_only !== true ||
  tuning.intended_use !== "prompt_provider_contract_tuning_only" ||
  tuning.excluded_from_release_evidence !== true ||
  tuning.source_dataset_versions?.length !== 0 ||
  tuning.supersedes_dataset_version !== "m1-prompt-dev-v1" ||
  tuning.generator !== "tests/evaluation/tuning/generator/generate_v2.py" ||
  tuning.generator_dependencies?.v1_manifest_sha256 !==
    frozenHistoricalManifestSHA256 ||
  tuning.render_profile?.width !== 600 ||
  tuning.render_profile?.height !== 400 ||
  tuning.render_profile?.low_contrast_assets !== 5
) {
  throw new Error(
    "tuning dataset identity or isolation declaration is invalid",
  );
}
if (!Array.isArray(tuning.samples) || tuning.samples.length !== 16) {
  throw new Error(
    `tuning sample count = ${tuning.samples?.length ?? 0}, want 16`,
  );
}

const releaseHashes = new Set(release.samples.map((sample) => sample.sha256));
const historicalHashes = new Set();
for (const sample of historical.samples) {
  const assetPath = resolve(dirname(historicalManifestPath), sample.file);
  const content = await readFile(assetPath);
  if (sha256(content) !== sample.sha256) {
    throw new Error(
      `historical tuning asset hash mismatch: ${sample.sample_id}`,
    );
  }
  historicalHashes.add(sample.sha256);
}
const ids = new Set();
const files = new Set();
const hashes = new Set();
const types = new Map();
const tags = new Map();
for (const sample of tuning.samples) {
  if (ids.has(sample.sample_id))
    throw new Error(`duplicate tuning sample ID: ${sample.sample_id}`);
  if (files.has(sample.file))
    throw new Error(`duplicate tuning sample file: ${sample.file}`);
  if (hashes.has(sample.sha256))
    throw new Error(`duplicate tuning asset hash: ${sample.sample_id}`);
  ids.add(sample.sample_id);
  files.add(sample.file);
  hashes.add(sample.sha256);
  types.set(sample.document_type, (types.get(sample.document_type) ?? 0) + 1);
  for (const tag of sample.scenario_tags ?? []) {
    tags.set(tag, (tags.get(tag) ?? 0) + 1);
  }

  if (
    !/^TUNE2-(PAY|INV|UNK)-[0-9]{3}$/.test(sample.sample_id) ||
    !sample.file.startsWith("assets/m1-prompt-dev-v2/") ||
    sample.model_stage_eligible !== true ||
    sample.declared_mime !== "image/png" ||
    sample.expected_failure_category !== undefined ||
    !sample.scenario_tags?.includes("compact_bitmap") ||
    (sample.document_type !== "unknown" &&
      !sample.scenario_tags.includes("literal_evidence"))
  ) {
    throw new Error(
      `${sample.sample_id}: every tuning sample must reach the model`,
    );
  }
  const assetPath = resolve(dirname(tuningManifestPath), sample.file);
  const information = await stat(assetPath);
  if (!information.isFile() || (information.mode & 0o077) !== 0) {
    throw new Error(`${sample.sample_id}: tuning asset must be owner-only`);
  }
  const content = await readFile(assetPath);
  if (sha256(content) !== sample.sha256)
    throw new Error(`asset hash mismatch: ${sample.sample_id}`);
  if (releaseHashes.has(sample.sha256))
    throw new Error(
      `${sample.sample_id}: tuning asset reuses a frozen release asset`,
    );
  if (historicalHashes.has(sample.sha256))
    throw new Error(
      `${sample.sample_id}: tuning v2 reuses a frozen tuning v1 asset`,
    );
  assertPNG(content, sample.sample_id, 600, 400);
  validateExpectation(sample);
}

if (
  (types.get("payment") ?? 0) !== 6 ||
  (types.get("invoice") ?? 0) !== 8 ||
  (types.get("unknown") ?? 0) !== 2
) {
  throw new Error(
    `tuning type distribution is invalid: ${JSON.stringify(Object.fromEntries(types))}`,
  );
}
if (
  (tags.get("compact_bitmap") ?? 0) !== 16 ||
  (tags.get("literal_evidence") ?? 0) !== 14 ||
  (tags.get("low_contrast") ?? 0) !== 5 ||
  (tags.get("root_key_guard") ?? 0) < 3
) {
  throw new Error(
    `tuning scenario distribution is invalid: ${JSON.stringify(Object.fromEntries(tags))}`,
  );
}
const assetDirectory = resolve(
  projectDirectory,
  "tests/evaluation/tuning/assets/m1-prompt-dev-v2",
);
const diskFiles = await readdir(assetDirectory);
if (
  diskFiles.length !== files.size ||
  diskFiles.some((name) => !files.has(`assets/m1-prompt-dev-v2/${name}`))
) {
  throw new Error("tuning asset directory contains missing or unlisted files");
}

process.stdout.write(`dataset: ${tuning.dataset_version}\n`);
process.stdout.write(`manifest_sha256: ${sha256(tuningManifestBytes)}\n`);
process.stdout.write(
  `samples: 16 (payment=${types.get("payment")}, invoice=${types.get("invoice")}, unknown=${types.get("unknown")})\n`,
);
process.stdout.write("release_asset_hash_overlap: 0\n");
process.stdout.write("historical_tuning_asset_hash_overlap: 0\n");
process.stdout.write("model tuning dataset gate passed\n");

function validateExpectation(sample) {
  const fields = sample.expected_fields;
  const missing = sample.expected_missing_fields;
  const valueTypes = sample.expected_value_types;
  const evidence = sample.expected_evidence;
  if (
    !fields ||
    !Array.isArray(missing) ||
    !valueTypes ||
    !evidence ||
    !Array.isArray(sample.expected_events) ||
    !["needs_review", "blocked"].includes(sample.expected_review_state)
  ) {
    throw new Error(`${sample.sample_id}: expectation shape is incomplete`);
  }
  const presentPaths = Object.keys(fields);
  const missingPaths = new Set(missing);
  for (const path of presentPaths) {
    if (missingPaths.has(path))
      throw new Error(
        `${sample.sample_id}: ${path} is both present and missing`,
      );
    if (!Object.hasOwn(valueTypes, path))
      throw new Error(
        `${sample.sample_id}: ${path} has no expected value type`,
      );
    if (!Object.hasOwn(evidence, path))
      throw new Error(`${sample.sample_id}: ${path} has no expected evidence`);
    assertTypedValue(fields[path], valueTypes[path], sample.sample_id, path);
    const frozenEvidence = evidence[path];
    if (
      frozenEvidence?.page !== 1 ||
      typeof frozenEvidence.quote !== "string" ||
      frozenEvidence.quote.length === 0
    ) {
      throw new Error(`${sample.sample_id}: ${path} evidence is invalid`);
    }
  }
  for (const path of missingPaths) {
    if (!Object.hasOwn(valueTypes, path))
      throw new Error(
        `${sample.sample_id}: missing path ${path} has no value type`,
      );
  }
  if (
    presentPaths.length + missingPaths.size !==
    Object.keys(valueTypes).length
  ) {
    throw new Error(`${sample.sample_id}: field snapshot is not complete`);
  }
  if (
    sample.document_type === "unknown" &&
    Object.keys(valueTypes).length !== 0
  ) {
    throw new Error(
      `${sample.sample_id}: unknown document declares business fields`,
    );
  }
}

function assertTypedValue(value, valueType, sampleID, path) {
  const integer = Number.isSafeInteger(value) && value >= 0;
  const string = typeof value === "string" && value.length > 0;
  if (
    (["money_minor", "integer"].includes(valueType) && !integer) ||
    (["string", "date", "instant", "decimal"].includes(valueType) && !string)
  ) {
    throw new Error(`${sampleID}: ${path} does not match ${valueType}`);
  }
  if (valueType === "decimal" && !/^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(value)) {
    throw new Error(`${sampleID}: ${path} is not a canonical decimal string`);
  }
}

function assertPNG(content, sampleID, width, height) {
  const signature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
  if (content.length < 24 || !content.subarray(0, 8).equals(signature))
    throw new Error(`${sampleID}: asset is not a PNG`);
  if (content.readUInt32BE(16) !== width || content.readUInt32BE(20) !== height)
    throw new Error(`${sampleID}: PNG dimensions are not ${width}x${height}`);
}

function sha256(content) {
  return createHash("sha256").update(content).digest("hex");
}
