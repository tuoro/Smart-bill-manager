import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const toolsDirectory = dirname(fileURLToPath(import.meta.url));
const script = join(toolsDirectory, "prepare-self-hosted-deployment.sh");
const deployScript = join(toolsDirectory, "sbm-deploy.sh");
const releaseEnvironment = join(
  dirname(toolsDirectory),
  "infra",
  "compose",
  "release.env",
);

test("deployment preparation creates distinct owner-only secrets without printing them", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-deployment-test-"));
  const target = join(parent, "deployment");
  try {
    const { stdout, stderr } = await execFileAsync(script, [target]);
    assert.equal(stderr, "");
    assert.match(stdout, /owner-only permissions/);

    const targetStat = await stat(target);
    assert.equal(targetStat.mode & 0o777, 0o700);

    for (const name of ["data", "data/postgres", "data/objects", "backups"]) {
      const info = await stat(join(target, name));
      assert.equal(info.mode & 0o777, 0o700);
    }

    const secretNames = [
      "master-key",
      "postgres-admin-password",
      "postgres-migration-password",
      "postgres-runtime-password",
      "owner-password",
    ];
    const values = [];
    for (const name of secretNames) {
      const path = join(target, name);
      const info = await stat(path);
      assert.equal(info.mode & 0o777, 0o600);
      const value = await readFile(path, "utf8");
      assert.match(value, /^[0-9a-f]{64}$/);
      assert.equal(stdout.includes(value), false);
      values.push(value);
    }
    assert.equal(new Set(values).size, values.length);

    const environment = await readFile(join(target, "deployment.env"), "utf8");
    for (const value of values) {
      assert.equal(environment.includes(value), false);
    }
    assert.match(environment, /^SBM_STORAGE_TYPE=bind$/m);
    assert.match(environment, /^SBM_COMPOSE_PROJECT_NAME=smart-bill-manager$/m);
    assert.match(environment, new RegExp(`^SBM_POSTGRES_DATA_SOURCE=${target}/data/postgres$`, "m"));
    assert.match(environment, new RegExp(`^SBM_OBJECTS_SOURCE=${target}/data/objects$`, "m"));
    assert.match(environment, /^SBM_BIND_ADDRESS=127\.0\.0\.1$/m);
    assert.equal(environment.includes("SBM_IMAGE="), false);
    assert.equal(environment.includes("SBM_POSTGRES_IMAGE="), false);
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("release identity is separate from user deployment configuration", async () => {
  const environment = await readFile(releaseEnvironment, "utf8");
  assert.match(
    environment,
    /^SBM_IMAGE=ghcr\.io\/tuoro\/smart-bill-manager:v0\.3\.2@sha256:83bd3c795b3a7c2413a8f80279ab3fc8b9787e0e9b02cab5b08711c61d3ba6d1$/m,
  );
  assert.match(
    environment,
    /^SBM_POSTGRES_IMAGE=postgres:17-alpine@sha256:18cfe3ef5e6815560c98237d6216d1e5119702fb0f3894c8785dd58b8bbe5d73$/m,
  );
});

test("deployment preparation rejects relative and existing targets", async () => {
  await assert.rejects(() => execFileAsync(script, ["relative"]));

  const existing = await mkdtemp(join(tmpdir(), "sbm-deployment-existing-"));
  try {
    await assert.rejects(() => execFileAsync(script, [existing]));
  } finally {
    await rm(existing, { recursive: true, force: true });
  }
});

test("deployment preparation rejects a target inside the repository", async () => {
  const repositoryTarget = join(dirname(toolsDirectory), ".deployment-test");
  await assert.rejects(() => execFileAsync(script, [repositoryTarget]));
  const equivalentTarget = join(
    dirname(toolsDirectory),
    "docs",
    "..",
    ".deployment-test",
  );
  await assert.rejects(() => execFileAsync(script, [equivalentTarget]));
});

test("deployment wrapper rejects old Compose and accepts the supported boundary", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-deploy-wrapper-test-"));
  const target = join(parent, "deployment");
  const binaryDirectory = join(parent, "bin");
  try {
    await execFileAsync(script, [target]);
    await mkdir(binaryDirectory);
    const fakeDocker = join(binaryDirectory, "docker");
    await writeFile(
      fakeDocker,
      '#!/bin/sh\nif [ "$1" = compose ] && [ "$2" = version ]; then printf "%s\\n" "$FAKE_DOCKER_VERSION"; fi\n',
      { mode: 0o755 },
    );
    const environment = {
      ...process.env,
      PATH: `${binaryDirectory}:${process.env.PATH}`,
    };
    await assert.rejects(() =>
      execFileAsync(deployScript, [target, "status"], {
        env: { ...environment, FAKE_DOCKER_VERSION: "2.24.3" },
      }),
    );
    await execFileAsync(deployScript, [target, "status"], {
      env: { ...environment, FAKE_DOCKER_VERSION: "2.24.4" },
    });
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("upgrade requires backup confirmation and runs the ordered schema upgrade", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-deploy-upgrade-test-"));
  const target = join(parent, "deployment");
  const binaryDirectory = join(parent, "bin");
  const log = join(parent, "docker.log");
  try {
    await execFileAsync(script, [target]);
    await mkdir(binaryDirectory);
    const fakeDocker = join(binaryDirectory, "docker");
    await writeFile(
      fakeDocker,
      '#!/bin/sh\nif [ "$1" = compose ] && [ "$2" = version ]; then printf "%s\\n" "2.24.4"; exit 0; fi\nprintf "%s\\n" "$*" >>"$FAKE_DOCKER_LOG"\n',
      { mode: 0o755 },
    );
    const environment = {
      ...process.env,
      PATH: `${binaryDirectory}:${process.env.PATH}`,
      FAKE_DOCKER_LOG: log,
    };
    await assert.rejects(() =>
      execFileAsync(deployScript, [target, "upgrade"], { env: environment }),
    );
    await execFileAsync(deployScript, [target, "upgrade", "--backup-confirmed"], {
      env: environment,
    });
    const calls = await readFile(log, "utf8");
    assert.match(calls, /stop app/);
    assert.match(calls, /--wait database/);
    assert.match(calls, /run --rm --no-deps provision/);
    assert.match(calls, /run --rm --no-deps migrate/);
    assert.match(calls, /--wait app/);
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});
