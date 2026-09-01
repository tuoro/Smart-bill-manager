import assert from "node:assert/strict";
import test from "node:test";

import {
  bootstrapComposeArguments,
  parseArguments,
} from "./run-bootstrap-owner-gate.mjs";

test("bootstrap gate accepts only isolated M4 identities", () => {
  const argumentsList = [
    "--project-name",
    "sbm-m4-123e4567-core",
    "--output",
    "/tmp/run/bootstrap.json",
    "--master-key-source",
    "/tmp/run/master-key",
    "--owner-password-source",
    "/tmp/run/owner-password",
    "--provider-key-source",
    "/tmp/run/provider-key",
    "--postgres-admin-password-source",
    "/tmp/run/postgres-admin-password",
    "--postgres-migration-password-source",
    "/tmp/run/postgres-migration-password",
    "--postgres-runtime-password-source",
    "/tmp/run/postgres-runtime-password",
    "--release-artifacts-source",
    "/tmp/run/release-artifacts",
    "--exercise-id",
    "123e4567-e89b-42d3-a456-426614174000",
    "--expected-head",
    "a".repeat(40),
    "--expected-release-input-sha256",
    "b".repeat(64),
    "--image-id",
    `sha256:${"c".repeat(64)}`,
  ];
  assert.equal(
    parseArguments(argumentsList).projectName,
    "sbm-m4-123e4567-core",
  );
  assert.equal(
    parseArguments(argumentsList).imageID,
    `sha256:${"c".repeat(64)}`,
  );
  const command = bootstrapComposeArguments({
    projectName: "sbm-m4-123e4567-core",
  });
  assert.deepEqual(
    command.slice(command.indexOf("run"), command.indexOf("app") + 1),
    ["run", "--rm", "--no-deps", "--pull", "never", "app"],
  );
  assert.equal(command.includes("--no-build"), false);
  assert.equal(command.at(-1), "/run/sbm-secrets/owner-password");
  argumentsList[1] = "smart-bill-manager";
  assert.throws(() => parseArguments(argumentsList));
});
