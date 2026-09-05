import assert from "node:assert/strict";
import test from "node:test";
import {
  activationRequiredTests,
  buildActivationEvidence,
  assertExecutionIdentity,
} from "./write-restore-activation-evidence.mjs";

const image = {
  report_kind: "m4-local-release-image-result",
  passed: true,
  build_identity: {
    baseline_head: "a".repeat(40),
    release_input_sha256: "b".repeat(64),
    image_id: "sha256:" + "c".repeat(64),
  },
};
function events() {
  return [
    ...activationRequiredTests.flatMap(([Package, Test]) => [
      { Package, Test, Action: "run" },
      { Package, Test, Action: "pass" },
    ]),
    ...[...new Set(activationRequiredTests.map(([pkg]) => pkg))].map(
      (Package) => ({ Package, Action: "pass" }),
    ),
  ];
}
const stream = (events) =>
  events.map((event) => JSON.stringify(event)).join("\n");

test("activation evidence consumes executed tests and emits only safe identities and cases", () => {
  const input = events();
  input.push({
    Package: "synthetic",
    Action: "output",
    Output: "private-path-and-session",
  });
  const result = buildActivationEvidence([stream(input)], image);
  assert.equal(result.passed, true);
  assert.equal(Object.keys(result.cases).length, 9);
  assert.doesNotMatch(
    JSON.stringify(result),
    /private-path|session|TestRestore/,
  );
});

test("activation evidence rejects missing, skipped, failed, duplicated or unfinished tests", () => {
  for (const mutate of [
    (input) => input.splice(0, 2),
    (input) => {
      input[1].Action = "skip";
    },
    (input) => {
      input[1].Action = "fail";
    },
    (input) => input.push(input[0]),
    (input) => input.splice(1, 1),
    (input) => input.pop(),
  ]) {
    const input = events();
    mutate(input);
    assert.throws(() => buildActivationEvidence([stream(input)], image));
  }
  assert.throws(() => buildActivationEvidence(["not-json"], image));
  assert.throws(() =>
    buildActivationEvidence([stream(events())], { ...image, passed: false }),
  );
  assert.throws(() =>
    buildActivationEvidence([stream(events())], {
      ...image,
      build_identity: {},
    }),
  );
});

test("activation evidence rejects cache output and a changed candidate before or after execution", () => {
  const input = events();
  input.push({ Package: "synthetic", Action: "output", Output: "ok (cached)" });
  assert.throws(() => buildActivationEvidence([stream(input)], image));
  const identity = image.build_identity;
  assert.doesNotThrow(() =>
    assertExecutionIdentity(
      identity,
      identity.baseline_head,
      identity.release_input_sha256,
    ),
  );
  assert.throws(() =>
    assertExecutionIdentity(
      identity,
      "d".repeat(40),
      identity.release_input_sha256,
    ),
  );
  assert.throws(() =>
    assertExecutionIdentity(identity, identity.baseline_head, "d".repeat(64)),
  );
});
