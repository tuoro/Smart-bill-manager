import assert from "node:assert/strict";
import test from "node:test";

import {
  parseArguments,
  validateInspection,
} from "./run-acceptance-loopback-bridge.mjs";

const containerID = "a".repeat(64);
const imageID = `sha256:${"b".repeat(64)}`;

test("acceptance loopback bridge only accepts the fixed container identity", () => {
  assert.deepEqual(
    parseArguments([
      "--container-id",
      containerID,
      "--image-id",
      imageID,
    ]),
    { containerID, imageID },
  );
  assert.throws(() =>
    parseArguments([
      "--container-id",
      "short",
      "--image-id",
      imageID,
    ]),
  );
});

test("acceptance loopback bridge requires an internal candidate network and inert Docker mapping", () => {
  const networkID = "c".repeat(64);
  const databaseNetworkID = "d".repeat(64);
  const container = {
    Id: containerID,
    Image: imageID,
    Config: { Image: "smart-bill-manager:local" },
    State: { Running: true },
    HostConfig: {
      PortBindings: {
        "8080/tcp": [{ HostIp: "127.0.0.1", HostPort: "8080" }],
      },
    },
    NetworkSettings: {
      Ports: { "8080/tcp": null },
      Networks: {
        "sbm-m4-123e4567-core_default": {
          NetworkID: networkID,
          IPAddress: "172.31.0.2",
        },
        "sbm-m4-123e4567-core_database": {
          NetworkID: databaseNetworkID,
          IPAddress: "172.30.0.3",
        },
      },
    },
  };
  const networks = [
    { Id: networkID, Internal: true },
    { Id: databaseNetworkID, Internal: true },
  ];
  assert.equal(
    validateInspection(container, networks, { containerID, imageID }),
    "172.31.0.2",
  );
  container.NetworkSettings.Ports["8080/tcp"] = [
    { HostIp: "127.0.0.1", HostPort: "8080" },
  ];
  assert.throws(() =>
    validateInspection(container, networks, { containerID, imageID }),
  );
});
