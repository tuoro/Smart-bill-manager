#!/usr/bin/env node

import { execFile as execFileCallback } from "node:child_process";
import net from "node:net";
import { pathToFileURL } from "node:url";
import { promisify } from "node:util";

import {
  SafeToolError,
  parseStrictPairs,
  requireImageID,
  safeErrorCode,
} from "./lib/protected-output.mjs";
import { hasExpectedLoopbackBinding } from "./lib/local-release-command.mjs";

const execFile = promisify(execFileCallback);
const listenAddress = "127.0.0.1";
const listenPort = 8080;
const targetPort = 8080;

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const inspected = await inspectJSON(["inspect", options.containerID]);
  const container = single(inspected);
  const attachedNetworks = Object.values(
    container.NetworkSettings?.Networks ?? {},
  );
  if (
    attachedNetworks.length !== 2 ||
    attachedNetworks.some((network) => !network?.NetworkID)
  ) {
    throw new SafeToolError("bridge_identity_invalid");
  }
  const networks = await Promise.all(
    attachedNetworks.map(async (network) =>
      single(await inspectJSON(["network", "inspect", network.NetworkID])),
    ),
  );
  const targetAddress = validateInspection(container, networks, options);
  await probe(targetAddress);
  await serve(targetAddress);
}

export function parseArguments(argumentsList) {
  const values = parseStrictPairs(argumentsList, ["container-id", "image-id"]);
  const containerID = values.get("container-id");
  if (!/^[0-9a-f]{64}$/.test(containerID)) {
    throw new SafeToolError("bridge_identity_invalid");
  }
  return {
    containerID,
    imageID: requireImageID(values.get("image-id")),
  };
}

export function validateInspection(container, networks, options) {
  const attachedNetworks = Object.entries(
    container.NetworkSettings?.Networks ?? {},
  );
  const defaultEntry = attachedNetworks.find(([name]) =>
    /^sbm-m4-[0-9a-f]{8}(?:-[a-z]+)?_default$/.test(name),
  );
  const databaseEntry = defaultEntry
    ? attachedNetworks.find(
        ([name]) => name === defaultEntry[0].replace(/_default$/, "_database"),
      )
    : undefined;
  const networkByID = new Map(networks.map((network) => [network.Id, network]));
  const address = defaultEntry?.[1]?.IPAddress;
  if (
    container.Id !== options.containerID ||
    container.Image !== options.imageID ||
    container.Config?.Image !== "smart-bill-manager:local" ||
    container.State?.Running !== true ||
    attachedNetworks.length !== 2 ||
    !defaultEntry ||
    !databaseEntry ||
    networks.length !== 2 ||
    networkByID.get(defaultEntry[1]?.NetworkID)?.Internal !== true ||
    networkByID.get(databaseEntry[1]?.NetworkID)?.Internal !== true ||
    !hasExpectedLoopbackBinding(container, "http://127.0.0.1:8080") ||
    container.NetworkSettings?.Ports?.["8080/tcp"] !== null ||
    typeof address !== "string" ||
    !/^(?:10\.|172\.(?:1[6-9]|2\d|3[01])\.|192\.168\.)\d{1,3}(?:\.\d{1,3}){1,2}$/.test(
      address,
    )
  ) {
    throw new SafeToolError("bridge_identity_invalid");
  }
  return address;
}

async function inspectJSON(argumentsList) {
  let stdout;
  try {
    ({ stdout } = await execFile("docker", argumentsList, {
      env: selectedDockerEnvironment(),
      encoding: "buffer",
      timeout: 10_000,
      maxBuffer: 2 * 1024 * 1024,
    }));
    return JSON.parse(stdout.toString("utf8"));
  } catch {
    throw new SafeToolError("bridge_inspection_failed");
  } finally {
    stdout?.fill(0);
  }
}

function single(value) {
  if (!Array.isArray(value) || value.length !== 1) {
    throw new SafeToolError("bridge_inspection_failed");
  }
  return value[0];
}

function probe(targetAddress) {
  return new Promise((resolveProbe, rejectProbe) => {
    const socket = net.createConnection({ host: targetAddress, port: targetPort });
    const timeout = setTimeout(() => {
      socket.destroy();
      rejectProbe(new SafeToolError("bridge_target_unavailable"));
    }, 5_000);
    socket.once("connect", () => {
      clearTimeout(timeout);
      socket.destroy();
      resolveProbe();
    });
    socket.once("error", () => {
      clearTimeout(timeout);
      rejectProbe(new SafeToolError("bridge_target_unavailable"));
    });
  });
}

function serve(targetAddress) {
  return new Promise((resolveServe, rejectServe) => {
    const sockets = new Set();
    const server = net.createServer((client) => {
      const target = net.createConnection({ host: targetAddress, port: targetPort });
      sockets.add(client);
      sockets.add(target);
      client.pause();
      client.setNoDelay(true);
      target.setNoDelay(true);
      target.once("connect", () => {
        client.pipe(target);
        target.pipe(client);
        client.resume();
      });
      const closePair = () => {
        client.destroy();
        target.destroy();
        sockets.delete(client);
        sockets.delete(target);
      };
      client.once("error", closePair);
      target.once("error", closePair);
      client.once("close", () => sockets.delete(client));
      target.once("close", () => sockets.delete(target));
    });
    server.maxConnections = 256;
    server.once("error", () => rejectServe(new SafeToolError("bridge_listen_failed")));
    server.listen({ host: listenAddress, port: listenPort, exclusive: true }, () => {
      process.stdout.write(
        `${JSON.stringify({ report_kind: "m4-acceptance-loopback-bridge", ready: true })}\n`,
      );
    });
    const stop = () => {
      for (const socket of sockets) socket.destroy();
      server.close(() => resolveServe());
    };
    process.once("SIGINT", stop);
    process.once("SIGTERM", stop);
  });
}

function selectedDockerEnvironment() {
  const result = {};
  for (const name of [
    "PATH",
    "HOME",
    "DOCKER_HOST",
    "DOCKER_CONTEXT",
    "DOCKER_CONFIG",
    "XDG_CONFIG_HOME",
  ]) {
    if (process.env[name]) result[name] = process.env[name];
  }
  return result;
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`acceptance-loopback-bridge: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
