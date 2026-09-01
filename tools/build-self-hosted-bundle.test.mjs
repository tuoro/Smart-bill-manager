import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const toolsDirectory = dirname(fileURLToPath(import.meta.url));
const builder = join(toolsDirectory, "build-self-hosted-bundle.sh");

const expectedFiles = [
  "LICENSE",
  "README.md",
  "README_EN.md",
  "docs/backup-restore.md",
  "docs/deployment.md",
  "docs/local-operations.md",
  "infra/compose/compose.bootstrap.yaml",
  "infra/compose/compose.release.yaml",
  "infra/compose/compose.yaml",
  "infra/compose/release.env",
  "tools/prepare-self-hosted-deployment.sh",
  "tools/sbm-deploy.sh",
];

test("self-hosted bundle contains only the reviewed deployment allowlist", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-bundle-test-"));
  const output = join(parent, "smart-bill-manager-docker.tar.gz");
  try {
    await execFileAsync(builder, [output]);
    const { stdout } = await execFileAsync("tar", ["-tzf", output]);
    const files = stdout
      .trim()
      .split("\n")
      .filter((entry) => !entry.endsWith("/"))
      .map((entry) => entry.replace(/^smart-bill-manager-docker\//, ""));
    assert.deepEqual(files.sort(), expectedFiles.sort());

    const archive = await readFile(output);
    assert.ok(archive.length > 0);
    const checksum = await readFile(`${output}.sha256`, "utf8");
    assert.match(checksum, /^[0-9a-f]{64}  smart-bill-manager-docker\.tar\.gz\n$/);
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("self-hosted bundle rejects relative and existing outputs", async () => {
  await assert.rejects(() => execFileAsync(builder, ["bundle.tar.gz"]));
  const parent = await mkdtemp(join(tmpdir(), "sbm-bundle-existing-"));
  const output = join(parent, "bundle.tar.gz");
  try {
    await execFileAsync("touch", [output]);
    await assert.rejects(() => execFileAsync(builder, [output]));
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("self-hosted bundle generation is deterministic", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-bundle-deterministic-"));
  const first = join(parent, "first.tar.gz");
  const second = join(parent, "second.tar.gz");
  try {
    await execFileAsync(builder, [first]);
    await execFileAsync(builder, [second]);
    const digest = (content) => createHash("sha256").update(content).digest("hex");
    assert.equal(digest(await readFile(first)), digest(await readFile(second)));
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});
