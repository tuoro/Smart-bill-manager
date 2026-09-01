import assert from "node:assert/strict";
import test from "node:test";

import { parseArguments } from "./check-entrypoint-failures.mjs";
import { inspectedImageMatches } from "./lib/local-release-command.mjs";

test("entrypoint boundary checker requires a sibling isolated workspace", () => {
  const argumentsList = [
    "--output",
    "/tmp/run/entrypoint.json",
    "--workspace",
    "/tmp/run/entrypoint-cases",
    "--image",
    "smart-bill-manager:local",
    "--image-id",
    `sha256:${"c".repeat(64)}`,
    "--expected-head",
    "a".repeat(40),
    "--expected-release-input-sha256",
    "b".repeat(64),
  ];
  assert.equal(parseArguments(argumentsList).image, "smart-bill-manager:local");
  argumentsList[3] = "/tmp/other/entrypoint-cases";
  assert.throws(() => parseArguments(argumentsList));
});

test("entrypoint checker binds execution to the inspected image ID", () => {
  const imageID = `sha256:${"c".repeat(64)}`;
  assert.equal(
    inspectedImageMatches(
      Buffer.from(JSON.stringify([{ Id: imageID }])),
      imageID,
    ),
    true,
  );
  assert.equal(
    inspectedImageMatches(
      Buffer.from(JSON.stringify([{ Id: `sha256:${"d".repeat(64)}` }])),
      imageID,
    ),
    false,
  );
  assert.equal(inspectedImageMatches(Buffer.from("not-json"), imageID), false);
});
