import assert from "node:assert/strict";
import { chmod, mkdtemp, readFile, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  SafeToolError,
  parseStrictPairs,
  requireDistinctPaths,
  requireGitSHA,
  requireImageID,
  requireLoopbackURL,
  reserveProtectedDirectory,
  reserveProtectedFile,
  writeProtectedChild,
} from "./lib/protected-output.mjs";

test("credential source paths must remain distinct", () => {
  assert.deepEqual(requireDistinctPaths(["/tmp/a", "/tmp/b"]), [
    "/tmp/a",
    "/tmp/b",
  ]);
  assert.throws(() => requireDistinctPaths(["/tmp/a", "/tmp/../tmp/a"]));
});

async function isolatedDirectory() {
  const directory = await mkdtemp(join(tmpdir(), "sbm-protected-output-test."));
  await chmod(directory, 0o700);
  return directory;
}

test("protected file is exclusive and owner-only", async () => {
  const directory = await isolatedDirectory();
  try {
    const location = join(directory, "result.json");
    const output = await reserveProtectedFile(location);
    await output.writeJSON({ passed: true });
    await output.close();
    assert.deepEqual(JSON.parse(await readFile(location, "utf8")), {
      passed: true,
    });
    await assert.rejects(
      reserveProtectedFile(location),
      (error) =>
        error instanceof SafeToolError &&
        error.code === "output_reservation_failed",
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("protected directory rejects broad or symlinked parents", async () => {
  const directory = await isolatedDirectory();
  const linked = `${directory}-link`;
  try {
    const output = await reserveProtectedDirectory(join(directory, "reports"));
    await writeProtectedChild(output, "summary.json", { passed: true });
    assert.equal(
      JSON.parse(await readFile(join(output, "summary.json"))).passed,
      true,
    );
    await symlink(directory, linked);
    await assert.rejects(
      reserveProtectedDirectory(join(linked, "other")),
      (error) =>
        error instanceof SafeToolError &&
        error.code === "output_parent_invalid",
    );
    await chmod(directory, 0o755);
    await assert.rejects(
      reserveProtectedFile(join(directory, "broad.json")),
      (error) =>
        error instanceof SafeToolError &&
        error.code === "output_parent_invalid",
    );
  } finally {
    await rm(linked, { force: true });
    await rm(directory, { recursive: true, force: true });
  }
});

test("argument and release identities are strict", () => {
  assert.equal(
    parseStrictPairs(["--output", "/tmp/value"], ["output"]).get("output"),
    "/tmp/value",
  );
  assert.throws(() =>
    parseStrictPairs(["--output", "a", "--output", "b"], ["output"]),
  );
  assert.throws(() => parseStrictPairs(["--unknown", "x"], ["output"]));
  assert.equal(requireGitSHA("a".repeat(40)), "a".repeat(40));
  assert.equal(
    requireImageID(`sha256:${"b".repeat(64)}`),
    `sha256:${"b".repeat(64)}`,
  );
  assert.equal(requireLoopbackURL("http://127.0.0.1:8080").port, "8080");
  assert.throws(() => requireLoopbackURL("https://example.test"));
  assert.throws(() =>
    requireLoopbackURL("http://127.0.0.1:8080/path", { allowPath: false }),
  );
});
