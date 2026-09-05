#!/usr/bin/env node

import { pathToFileURL } from "node:url";
import { basename, dirname, resolve } from "node:path";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { releaseInputDigest } from "./check-release-image.mjs";
import { runCaptured } from "./lib/local-release-command.mjs";
import { activationCaseNames } from "./write-backup-evidence.mjs";
import {
  parseStrictPairs,
  readProtectedSecret,
  readProtectedJSON,
  requireGitSHA,
  requireSHA256,
  requireImageID,
  reserveProtectedFile,
  reserveProtectedDirectory,
  SafeToolError,
  safeErrorCode,
} from "./lib/protected-output.mjs";

const base = "github.com/tuoro/smart-bill-manager/apps/api/";
const backup = `${base}cmd/backup`;
const postgres = `${base}internal/adapters/postgresql`;
const objects = `${base}internal/adapters/restorestate`;
export const activationRequiredTests = [
  [objects, "TestOrdinaryObjectRootMustExistWithoutRestoreIdentity"],
  [postgres, "TestRestoreBeginPersistsBarrierAndRejectsExistingTargets"],
  [postgres, "TestRestoreRejectsNonTableObjectsInTarget"],
  [postgres, "TestRestoreRuntimeRoleCannotMutateActivation"],
  [postgres, "TestRestoreAndMigrationSerializeBeforeChoosingLifecycle"],
  [objects, "TestRestoreObjectIdentityRequiresExactPair"],
  [objects, "TestRestoreObjectIdentityRejectsCorruptionAndUnsafeFiles"],
  [backup, "TestRestoreFailureWindowsNeverActivateDatabase"],
  ...["database_restored", "key_publication", "before_activation"].map(
    (phase) => [
      backup,
      `TestRestoreFailureWindowsNeverActivateDatabase/${phase}`,
    ],
  ),
  [backup, "TestRestoreActivationPairsObjectsAndSupportsFreshRebackup"],
  [backup, "TestRestoreGuardPreservesOrdinaryBootstrapAndForwardMigration"],
  [backup, "TestRestoreEntrypointsRejectIncompleteAndUnpairedEmptyIdentity"],
  ...["database_restored", "before_activation", "missing_identity"].flatMap(
    (phase) =>
      ["server", "bootstrap-owner", "recover-account"].map((entry) => [
        backup,
        `TestRestoreEntrypointsRejectIncompleteAndUnpairedEmptyIdentity/${phase}/${entry}`,
      ]),
  ),
  [
    backup,
    "TestAccountAuthenticatedRestorePreservesHistoryAndInvalidatesCredentials",
  ],
  [backup, "TestManualTripAuthenticatedBackupRestore"],
];

export function buildActivationEvidence(streams, imageResult) {
  if (
    imageResult?.report_kind !== "m4-local-release-image-result" ||
    imageResult.passed !== true
  )
    throw new SafeToolError("candidate_image_gate_required");
  const identity = imageResult.build_identity;
  const buildIdentity = {
    baseline_head: requireGitSHA(identity?.baseline_head),
    release_input_sha256: requireSHA256(identity?.release_input_sha256),
    image_id: requireImageID(identity?.image_id),
  };
  const executed = new Map();
  const packages = new Set();
  for (const stream of streams) {
    for (const line of stream.split("\n").filter((line) => line.trim())) {
      let event;
      try {
        event = JSON.parse(line);
      } catch {
        throw new SafeToolError("go_test_stream_invalid");
      }
      if (
        typeof event.Package !== "string" ||
        ![
          "start",
          "run",
          "pause",
          "cont",
          "output",
          "pass",
          "fail",
          "skip",
        ].includes(event.Action)
      )
        throw new SafeToolError("go_test_stream_invalid");
      if (["fail", "skip"].includes(event.Action))
        throw new SafeToolError("go_tests_not_passed");
      if (event.Output?.includes("(cached)"))
        throw new SafeToolError("cached_tests_rejected");
      if (!event.Test) {
        if (event.Action === "pass") packages.add(event.Package);
        continue;
      }
      const key = `${event.Package}:${event.Test}`;
      if (event.Action === "run") {
        if (executed.has(key)) throw new SafeToolError("duplicate_test_result");
        executed.set(key, "run");
      } else if (event.Action === "pass") {
        if (executed.get(key) !== "run")
          throw new SafeToolError("go_test_stream_invalid");
        executed.set(key, "pass");
      }
    }
  }
  if (
    [...executed.values()].some((state) => state !== "pass") ||
    activationRequiredTests.some(
      ([pkg, test]) =>
        !packages.has(pkg) || executed.get(`${pkg}:${test}`) !== "pass",
    )
  )
    throw new SafeToolError("activation_test_missing");
  return {
    report_kind: "restore-activation-gate-result",
    protocol_version: 1,
    build_identity: buildIdentity,
    cases: Object.fromEntries(activationCaseNames.map((name) => [name, true])),
    passed: true,
  };
}

async function main() {
  const options = parseStrictPairs(process.argv.slice(2), [
    "repository-root",
    "workspace",
    "network",
    "postgres-test-config",
    "postgres-password-file",
    "go-module-cache",
    "image-result",
    "output",
  ]);
  const repository = resolve(options.get("repository-root"));
  const workspace = resolve(options.get("workspace"));
  const network = options.get("network");
  if (
    !/^[a-zA-Z0-9_-]+$/.test(network) ||
    dirname(workspace) !== dirname(resolve(options.get("output")))
  )
    throw new SafeToolError("invalid_arguments");
  const imageResult = await readProtectedJSON(
    options.get("image-result"),
    1024 * 1024,
  );
  if (!imageResult.passed)
    throw new SafeToolError("candidate_image_gate_required");
  const identity = imageResult.build_identity;
  requireGitSHA(identity?.baseline_head);
  requireSHA256(identity?.release_input_sha256);
  requireImageID(identity?.image_id);
  const database = await readProtectedJSON(
    options.get("postgres-test-config"),
    8192,
  );
  if (
    database.host !== "database" ||
    database.port !== 5432 ||
    database.password_file !== "/run/secrets/postgres-password"
  )
    throw new SafeToolError("isolated_database_required");
  const password = await readProtectedSecret(
    options.get("postgres-password-file"),
    1024,
  );
  password.fill(0);
  await reserveProtectedDirectory(workspace, [
    repository,
    options.get("output"),
    options.get("go-module-cache"),
  ]);
  const name = `sbm-activation-${basename(workspace)}`;
  const buffers = [];
  const command = async (executable, args, timeout = 600000) => {
    const result = await runCaptured(executable, args, {
      cwd: repository,
      timeout,
      maximumBytes: 16 * 1024 * 1024,
    });
    buffers.push(result.stdout, result.stderr);
    if (result.code !== 0) throw new SafeToolError("activation_command_failed");
    return result.stdout;
  };
  const verifyIdentity = async () => {
    const head = (await command("git", ["rev-parse", "HEAD"]))
      .toString()
      .trim();
    assertExecutionIdentity(
      identity,
      head,
      await releaseInputDigest(repository),
    );
    const inspected = JSON.parse(
      (
        await command("docker", ["image", "inspect", identity.image_id])
      ).toString(),
    );
    const labels = inspected[0]?.Config?.Labels;
    if (
      inspected.length !== 1 ||
      inspected[0].Id !== identity.image_id ||
      labels?.["org.opencontainers.image.revision"] !== head ||
      labels?.["com.smart-bill-manager.release-input-sha256"] !==
        identity.release_input_sha256
    )
      throw new SafeToolError("candidate_image_gate_required");
  };
  try {
    await verifyIdentity();
    const net = JSON.parse(
      (await command("docker", ["network", "inspect", network])).toString(),
    );
    if (net.length !== 1 || net[0].Internal !== true)
      throw new SafeToolError("isolated_database_required");
    const memory = await readFile("/proc/meminfo", "utf8");
    if (Number(memory.match(/^MemAvailable:\s+(\d+)/m)?.[1]) < 4 * 1024 * 1024)
      throw new SafeToolError("insufficient_memory");
    await mkdir(`${workspace}/test-tmp`, { mode: 0o700 });
    await writeFile(
      `${workspace}/postgresql-test.json`,
      JSON.stringify(database),
      { mode: 0o600, flag: "wx" },
    );
    const common = [
      "run",
      "--rm",
      "--name",
      name,
      "--pull",
      "never",
      "--network",
      network,
      "--memory",
      "2g",
      "--cpus",
      "2",
      "--pids-limit",
      "256",
      "--read-only",
      "--cap-drop",
      "ALL",
      "--security-opt",
      "no-new-privileges:true",
      "--tmpfs",
      "/tmp:rw,noexec,nosuid,nodev,size=268435456",
      "--user",
      `${process.getuid()}:${process.getgid()}`,
      "--mount",
      `type=bind,src=${repository},dst=/workspace,readonly`,
      "--mount",
      `type=bind,src=${workspace},dst=/out`,
      "--mount",
      `type=bind,src=${resolve(options.get("postgres-password-file"))},dst=/run/secrets/postgres-password,readonly`,
      "--env",
      "SBM_TEST_POSTGRES_CONFIG_FILE=/out/postgresql-test.json",
      "--env",
      "TMPDIR=/out/test-tmp",
      "--env",
      "GOMAXPROCS=2",
      "--env",
      "GOMEMLIMIT=1500MiB",
    ];
    const go = [
      ...common,
      "--mount",
      `type=bind,src=${resolve(options.get("go-module-cache"))},dst=/go/pkg/mod,readonly`,
      "--env",
      "GOPROXY=off",
      "--env",
      "GOSUMDB=off",
      "--env",
      "GOTOOLCHAIN=local",
      "--env",
      "GOCACHE=/out/go-cache",
      "--env",
      "CGO_ENABLED=0",
      "--workdir",
      "/workspace/apps/api",
      "--entrypoint",
      "/bin/sh",
      "sha256:dc2521c2a906db43073b8b4d99f491b6341cf15610b6ebbab187c45153f9959e",
      "-ec",
    ];
    const unit = await command("docker", [
      ...go,
      "go test -json -count=1 -p 1 -parallel 2 -timeout 60s ./internal/adapters/restorestate ./internal/adapters/postgresql",
    ]);
    await command("docker", [
      ...go,
      "go test -p 1 -tags postgresql_tools -c -o /out/backup.test ./cmd/backup; go build -p 1 -o /out/test2json cmd/test2json",
    ]);
    // 实际账号/服务入口只能取已核验候选内的 /app，不能接受旧外部二进制目录。
    const restored = await command("docker", [
      ...common,
      "--env",
      "SBM_TEST_ENTRYPOINTS_DIR=/app",
      "--env",
      "SBM_PG_DUMP_PATH=/usr/local/bin/pg_dump",
      "--env",
      "SBM_PG_RESTORE_PATH=/usr/local/bin/pg_restore",
      "--entrypoint",
      "/out/test2json",
      identity.image_id,
      "-p",
      backup,
      "/out/backup.test",
      "-test.v=test2json",
      "-test.count=1",
      "-test.parallel=2",
      "-test.timeout=60s",
      "-test.run",
      "Test(Restore|ManualTripAuthenticatedBackupRestore|AccountAuthenticatedRestorePreservesHistoryAndInvalidatesCredentials)",
    ]);
    await verifyIdentity();
    const result = buildActivationEvidence(
      [unit.toString(), restored.toString()],
      imageResult,
    );
    const output = await reserveProtectedFile(options.get("output"));
    try {
      await output.writeJSON(result);
    } finally {
      await output.close();
    }
    process.stdout.write(
      JSON.stringify({
        report_kind: result.report_kind,
        case_count: activationCaseNames.length,
        passed: true,
      }) + "\n",
    );
  } finally {
    for (const buffer of buffers) buffer.fill(0);
    const cleanup = await runCaptured(
      "docker",
      ["container", "ls", "-aq", "--filter", `name=^/${name}$`],
      { cwd: repository, timeout: 10000 },
    );
    if (cleanup.code !== 0)
      throw new SafeToolError("activation_cleanup_failed");
    if (cleanup.stdout.toString().trim()) {
      const removed = await runCaptured("docker", ["rm", "-f", name], {
        cwd: repository,
        timeout: 30000,
      });
      if (removed.code !== 0)
        throw new SafeToolError("activation_cleanup_failed");
    }
    await rm(workspace, { recursive: true });
  }
}

export function assertExecutionIdentity(identity, currentHead, currentInput) {
  if (
    identity?.baseline_head !== currentHead ||
    identity?.release_input_sha256 !== currentInput
  )
    throw new SafeToolError("stale_candidate_rejected");
}
if (pathToFileURL(process.argv[1]).href === import.meta.url)
  main().catch((error) => {
    process.stderr.write(
      `restore-activation-evidence: ${safeErrorCode(error)}\n`,
    );
    process.exitCode = 1;
  });
