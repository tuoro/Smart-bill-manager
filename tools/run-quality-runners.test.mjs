import assert from "node:assert/strict";
import { resolve } from "node:path";
import test from "node:test";

import { parseArguments as parseLighthouse } from "../apps/web/e2e/run-lighthouse.mjs";
import { parseArguments as parseResponsive } from "../apps/web/e2e/run-responsive-a11y.mjs";
import {
  isExpectedReleaseContainer,
  isExpectedServerProcess,
  memoryImageVariant,
  memoryConfirmationRequest,
  topContainsExactServerPID,
  parseArguments as parseMemory,
} from "./run-memory-stability.mjs";
import {
  isExpectedPerformanceContainer,
  parseArguments as parsePerformance,
  performanceConfirmationRequest,
} from "./run-performance.mjs";

const head = "a".repeat(40);
const digest = "b".repeat(64);
const imageID = `sha256:${"c".repeat(64)}`;
const syntheticSource = resolve(
  "tests/evaluation/assets/m1-synthetic-v1/pay-001.png",
);

const performanceArguments = [
  "--server",
  "http://127.0.0.1:8080",
  "--email",
  "owner@example.test",
  "--password-file",
  "/tmp/password",
  "--seed-manifest",
  "/tmp/seed.json",
  "--output",
  "/tmp/performance.json",
  "--build-sha",
  head,
  "--release-input-sha256",
  digest,
  "--compose-version",
  "v2.39.1",
  "--compose-config-sha256",
  digest,
  "--image-id",
  imageID,
  "--container-id",
  "e".repeat(64),
  "--server-cpus",
  "2",
  "--server-memory-bytes",
  String(3584 * 1024 * 1024),
  "--database-location",
  "named-volume:sbm_postgres_data",
  "--object-storage-location",
  "named-volume:sbm_objects",
  "--provider-latency-note",
  "excluded-measured-by-memory-gate",
];

test("performance and memory runners reject aliases and remote origins", () => {
  assert.equal(parsePerformance(performanceArguments).serverCPUs, 2);
  assert.throws(() =>
    parsePerformance([
      ...performanceArguments,
      "--output",
      "/tmp/duplicate.json",
    ]),
  );
  const memoryArguments = [
    "--server",
    "http://127.0.0.1:8080",
    "--email",
    "owner@example.test",
    "--password-file",
    "/tmp/password",
    "--provider-base-url",
    "http://127.0.0.1:19086/v1",
    "--provider-api-key-file",
    "/tmp/provider-key",
    "--model",
    "synthetic-local-release",
    "--exercise-id",
    "123e4567-e89b-42d3-a456-426614174000",
    "--pid",
    "123",
    "--container-id",
    "d".repeat(64),
    "--source",
    syntheticSource,
    "--output",
    "/tmp/memory.json",
    "--build-sha",
    head,
    "--release-input-sha256",
    digest,
    "--compose-version",
    "v2.39.1",
    "--compose-config-sha256",
    digest,
    "--image-id",
    imageID,
    "--server-cpus",
    "2",
    "--server-memory-bytes",
    String(3584 * 1024 * 1024),
    "--database-location",
    "named-volume:sbm_postgres_data",
    "--object-storage-location",
    "named-volume:sbm_objects",
  ];
  assert.equal(parseMemory(memoryArguments).model, "synthetic-local-release");
  assert.equal(parseMemory(memoryArguments).containerID, "d".repeat(64));
  const remote = [...memoryArguments];
  remote[remote.indexOf("--provider-base-url") + 1] = "https://example.test/v1";
  assert.throws(() => parseMemory(remote));
  const wrongLoopbackPort = [...memoryArguments];
  wrongLoopbackPort[wrongLoopbackPort.indexOf("--provider-base-url") + 1] =
    "http://127.0.0.1:19087/v1";
  assert.throws(() => parseMemory(wrongLoopbackPort));
  const privateSource = [...memoryArguments];
  privateSource[privateSource.indexOf("--source") + 1] = "/tmp/private.png";
  assert.throws(() => parseMemory(privateSource));
  const invalidContainer = [...memoryArguments];
  invalidContainer[invalidContainer.indexOf("--container-id") + 1] = "short";
  assert.throws(() => parseMemory(invalidContainer));
});

test("performance confirmations carry the complete current review contract", () => {
  assert.deepEqual(performanceConfirmationRequest(), {
    expected_revision: 1,
    association_mode: "no_candidate",
    allocations: [],
    duplicate_resolutions: [],
  });
});

test("memory runner confirms every current duplicate candidate explicitly", () => {
  assert.deepEqual(
    memoryConfirmationRequest({
      revision: 3,
      duplicate_candidates: [{ id: "candidate-b" }, { id: "candidate-a" }],
    }),
    {
      expected_revision: 3,
      association_mode: "no_candidate",
      allocations: [],
      duplicate_resolutions: [
        { candidate_id: "candidate-b", action: "keep_distinct" },
        { candidate_id: "candidate-a", action: "keep_distinct" },
      ],
    },
  );
  assert.throws(() =>
    memoryConfirmationRequest({ revision: 0, duplicate_candidates: [] }),
  );
  assert.throws(() =>
    memoryConfirmationRequest({
      revision: 1,
      duplicate_candidates: [{ id: "" }],
    }),
  );
});

test("memory runner binds RSS sampling to the unprivileged release server", () => {
  const status = [
    "Name:\tserver",
    "Uid:\t10001\t10001\t10001\t10001",
    "Gid:\t10001\t10001\t10001\t10001",
    "",
  ].join("\n");
  assert.equal(
    isExpectedServerProcess(status, Buffer.from("/app/server\0", "utf8")),
    true,
  );
  assert.equal(
    isExpectedServerProcess(
      status,
      Buffer.from("/bin/sleep\0" + "100\0", "utf8"),
    ),
    false,
  );
  assert.equal(
    isExpectedServerProcess(
      status.replaceAll("10001", "0"),
      Buffer.from("/app/server\0", "utf8"),
    ),
    false,
  );
  const containerOptions = {
    containerID: "d".repeat(64),
    imageID,
    pid: 123,
    server: "http://127.0.0.1:8080",
  };
  assert.equal(
    isExpectedReleaseContainer(
      {
        Id: containerOptions.containerID,
        Image: imageID,
        Config: { Image: "smart-bill-manager:local" },
        State: { Running: true, Pid: 456 },
        HostConfig: {
          PortBindings: {
            "8080/tcp": [{ HostIp: "127.0.0.1", HostPort: "8080" }],
          },
        },
      },
      containerOptions,
    ),
    true,
  );
  assert.equal(
    isExpectedReleaseContainer(
      {
        Id: containerOptions.containerID,
        Image: imageID,
        Config: { Image: "smart-bill-manager:local" },
        State: { Running: true, Pid: 0 },
        HostConfig: {
          PortBindings: {
            "8080/tcp": [{ HostIp: "127.0.0.1", HostPort: "8080" }],
          },
        },
      },
      containerOptions,
    ),
    false,
  );
  const top = [
    "PID COMMAND COMMAND",
    "456 docker-init /sbin/docker-init -- /usr/local/bin/sbm-entrypoint /app/server",
    "123 server /app/server",
    "",
  ].join("\n");
  assert.equal(topContainsExactServerPID(top, 123), true);
  assert.equal(topContainsExactServerPID(top, 456), false);
  assert.equal(
    topContainsExactServerPID(`${top}124 server /app/server\n`, 123),
    false,
  );

  const performanceOptions = {
    containerID: "e".repeat(64),
    imageID,
    server: "http://127.0.0.1:8080",
  };
  assert.equal(
    isExpectedPerformanceContainer(
      {
        Id: performanceOptions.containerID,
        Image: imageID,
        Config: { Image: "smart-bill-manager:local" },
        State: { Running: true },
        HostConfig: {
          PortBindings: {
            "8080/tcp": [{ HostIp: "127.0.0.1", HostPort: "8080" }],
          },
        },
      },
      performanceOptions,
    ),
    true,
  );
});

test("memory runner creates deterministic valid visual variants instead of trailing-byte aliases", () => {
  const source = Buffer.from("synthetic-source-with-enough-bytes", "utf8");
  const variants = Array.from({ length: 60 }, (_, index) =>
    memoryImageVariant(source, index),
  );
  const signatures = new Set(
    variants.map((variant) => variant.toString("hex")),
  );
  assert.equal(signatures.size, 60);
  for (const variant of variants) {
    assert.equal(variant.subarray(0, 8).toString("hex"), "89504e470d0a1a0a");
    assert.equal(variant.readUInt32BE(16), 256);
    assert.equal(variant.readUInt32BE(20), 256);
    assert.ok(variant.length > 100);
  }
});

test("browser quality runners require loopback and the frozen synthetic source", () => {
  const lighthouseArguments = [
    "--server",
    "http://127.0.0.1:8080",
    "--email",
    "owner@example.test",
    "--password-file",
    "/tmp/password",
    "--source",
    syntheticSource,
    "--chrome-path",
    "/usr/bin/chromium",
    "--output",
    "/tmp/lighthouse",
    "--build-sha",
    head,
    "--release-input-sha256",
    digest,
    "--compose-config-sha256",
    digest,
    "--image-id",
    imageID,
  ];
  assert.equal(parseLighthouse(lighthouseArguments).source, syntheticSource);
  const wrongSource = [...lighthouseArguments];
  wrongSource[wrongSource.indexOf("--source") + 1] = "/tmp/private.png";
  assert.throws(() => parseLighthouse(wrongSource));

  const responsiveArguments = [
    "--server",
    "http://127.0.0.1:8080",
    "--email",
    "owner@example.test",
    "--password-file",
    "/tmp/password",
    "--chrome-path",
    "/usr/bin/chromium",
    "--output",
    "/tmp/responsive.json",
    "--build-sha",
    head,
    "--release-input-sha256",
    digest,
    "--compose-config-sha256",
    digest,
    "--image-id",
    imageID,
  ];
  assert.equal(
    parseResponsive(responsiveArguments).server,
    "http://127.0.0.1:8080",
  );
  responsiveArguments[1] = "http://0.0.0.0:8080";
  assert.throws(() => parseResponsive(responsiveArguments));
});
