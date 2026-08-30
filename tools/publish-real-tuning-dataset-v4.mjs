#!/usr/bin/env node

import { createHash } from "node:crypto";
import { constants } from "node:fs";
import {
  chmod,
  copyFile,
  mkdir,
  readFile,
  stat,
  writeFile,
} from "node:fs/promises";
import { basename, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const toolDirectory = dirname(fileURLToPath(import.meta.url));
const projectDirectory = resolve(toolDirectory, "..");
const sourceManifestPath = resolve(
  projectDirectory,
  "tests/evaluation/real-local/development-v3/manifest.json",
);
const defaultOutputManifestPath = resolve(
  projectDirectory,
  "tests/evaluation/real-local/development-v4/manifest.json",
);
const expectedSourceManifestSHA256 =
  "0326adc8dab5473faed732e20080d7e0578d7116d46a62f63f3729277c814626";
const frozenAt = "2026-08-29T23:24:51+08:00";

async function main() {
  const outputManifestPath = parseOutputPath(process.argv.slice(2));
  const sourceBytes = await readFile(sourceManifestPath);
  if (sha256(sourceBytes) !== expectedSourceManifestSHA256) {
    throw new Error("real development v3 source manifest is not frozen");
  }
  const source = JSON.parse(sourceBytes);
  if (
    source.dataset_version !== "m1-real-dev-v3" ||
    source.samples?.length !== 16 ||
    source.synthetic_only !== false ||
    source.real_world !== true
  ) {
    throw new Error("real development v3 source identity is invalid");
  }

  const outputDirectory = dirname(outputManifestPath);
  const assetDirectory = resolve(outputDirectory, "assets");
  await mkdir(assetDirectory, { recursive: true, mode: 0o700 });
  await chmod(outputDirectory, 0o700);
  await chmod(assetDirectory, 0o700);

  const samples = [];
  for (const original of source.samples) {
    const sample = structuredClone(original);
    sample.sample_id = sample.sample_id.replace(/^V3-/, "V4-");
    sample.scenario_tags = [
      ...new Set([
        ...(sample.scenario_tags ?? []).filter(
          (tag) => tag !== "bill_extraction_v1",
        ),
        "bill_extraction_v2",
      ]),
    ];

    const sourceAssetPath = resolve(dirname(sourceManifestPath), sample.file);
    const sourceAsset = await readFile(sourceAssetPath);
    if (sha256(sourceAsset) !== sample.sha256) {
      throw new Error(`${sample.sample_id}: source asset hash mismatch`);
    }
    const sourceInfo = await stat(sourceAssetPath);
    if (!sourceInfo.isFile() || (sourceInfo.mode & 0o077) !== 0) {
      throw new Error(`${sample.sample_id}: source asset is not owner-only`);
    }
    const assetName = basename(sample.file);
    const outputAssetPath = resolve(assetDirectory, assetName);
    await copyFile(sourceAssetPath, outputAssetPath, constants.COPYFILE_EXCL);
    await chmod(outputAssetPath, 0o600);
    sample.file = `assets/${assetName}`;
    samples.push(sample);
  }

  const manifest = {
    ...source,
    dataset_version: "m1-real-dev-v4",
    created_at: frozenAt,
    frozen_at: frozenAt,
    supersedes_dataset_version: "m1-real-dev-v3",
    prompt_contract: "bill-extract/2",
    extraction_schema_contract: "bill-extraction/2",
    provider_schema_contract: "bill-extraction-provider/2",
    authoritative_schema_contract: "document-claim/2",
    claim_mapper_contract: "claim-mapper/2",
    input_processing_contract: "document-normalize/2",
    source_manifest_sha256: expectedSourceManifestSHA256,
    label_revision: "bill-extraction-v2-natural-json-with-separate-evidence",
    samples,
  };
  const encoded = `${JSON.stringify(manifest, null, 2)}\n`;
  await writeFile(outputManifestPath, encoded, {
    encoding: "utf8",
    flag: "wx",
    mode: 0o600,
  });
  process.stdout.write(
    `published ${samples.length} samples; sha256=${sha256(encoded)}\n`,
  );
}

function parseOutputPath(argumentsList) {
  if (argumentsList.length === 0) return defaultOutputManifestPath;
  if (
    argumentsList.length !== 2 ||
    argumentsList[0] !== "--output" ||
    !argumentsList[1]
  ) {
    throw new Error(
      "usage: publish-real-tuning-dataset-v4.mjs [--output path]",
    );
  }
  return resolve(argumentsList[1]);
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

main().catch((error) => {
  process.stderr.write(
    `${error instanceof Error ? error.message : String(error)}\n`,
  );
  process.exitCode = 1;
});
