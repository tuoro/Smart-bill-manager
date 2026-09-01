import assert from "node:assert/strict";
import test from "node:test";

import {
  checksumText,
  identityText,
  isSafeArtifactPath,
  parseArguments,
} from "./prepare-local-release-artifacts.mjs";

const head = "a".repeat(40);
const releaseInput = "b".repeat(64);

test("release artifact arguments and manifests are deterministic", () => {
  const options = parseArguments([
    "--output-directory",
    "/tmp/output",
    "--expected-head",
    head,
    "--expected-release-input-sha256",
    releaseInput,
    "--npm-cache",
    "/tmp/npm",
    "--go-module-cache",
    "/tmp/go/pkg/mod",
    "--poppler-bundle",
    "/tmp/poppler",
  ]);
  assert.equal(options.expectedHead, head);
  assert.equal(
    identityText(options),
    `baseline_head=${head}\nrelease_input_sha256=${releaseInput}\nnode_version=v24.19.0\ngo_version=go1.26.7\ngo_image_id=sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57\nglibc_source_image_id=sha256:dc2521c2a906db43073b8b4d99f491b6341cf15610b6ebbab187c45153f9959e\npoppler_version=26.05.0\npoppler_source_sha256=6fef27ff04f37db43054c86bcdff6128c9fb1f6af4ef3c8b369a7e9abd68d0bb\n`,
  );
  assert.equal(
    checksumText([
      ["web/index.html", "2".repeat(64)],
      ["server", "1".repeat(64)],
    ]),
    `${"1".repeat(64)}  server\n${"2".repeat(64)}  web/index.html\n`,
  );
  assert.throws(
    () => parseArguments(["--output-directory", "/tmp/output"]),
    /invalid_arguments/,
  );
  assert.equal(isSafeArtifactPath("poppler/lib/libstdc++.so.6"), true);
  assert.equal(isSafeArtifactPath("../outside"), false);
  assert.equal(isSafeArtifactPath("poppler/lib/name with space"), false);
});
