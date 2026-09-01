import assert from "node:assert/strict";
import { execFile, spawn } from "node:child_process";
import { mkdir, mkdtemp, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
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

function execFileWithInput(file, args, input, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(file, args, { ...options, stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => { stdout += chunk; });
    child.stderr.on("data", (chunk) => { stderr += chunk; });
    child.on("error", reject);
    child.on("close", (code) => {
      if (code === 0) resolve({ stdout, stderr });
      else reject(new Error(`command exited ${code}: ${stderr}`));
    });
    child.stdin.end(input);
  });
}

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
    assert.match(environment, new RegExp(`^SBM_BACKUPS_DIRECTORY=${target}/backups$`, "m"));
    assert.match(environment, /^SBM_BIND_ADDRESS=127\.0\.0\.1$/m);
    assert.equal(environment.includes("SBM_IMAGE="), false);
    assert.equal(environment.includes("SBM_POSTGRES_IMAGE="), false);
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("deployment preparation supports distinct custom persistence directories and port", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-custom-deployment-test-"));
  const target = join(parent, "runtime");
  const postgres = join(parent, "postgres");
  const objects = join(parent, "objects");
  const backups = join(parent, "backups");
  try {
    await execFileAsync(script, [
      target,
      "--postgres-directory", postgres,
      "--objects-directory", objects,
      "--backups-directory", backups,
      "--http-port", "7476",
    ]);
    const environment = await readFile(join(target, "deployment.env"), "utf8");
    assert.match(environment, new RegExp(`^SBM_POSTGRES_DATA_SOURCE=${postgres}$`, "m"));
    assert.match(environment, new RegExp(`^SBM_OBJECTS_SOURCE=${objects}$`, "m"));
    assert.match(environment, new RegExp(`^SBM_BACKUPS_DIRECTORY=${backups}$`, "m"));
    assert.match(environment, /^SBM_HTTP_PORT=7476$/m);
    for (const directory of [target, postgres, objects, backups]) {
      assert.equal((await stat(directory)).mode & 0o777, 0o700);
    }
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("deployment preparation rejects duplicate persistence paths and invalid ports", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-invalid-deployment-test-"));
  try {
    const shared = join(parent, "shared");
    await assert.rejects(() => execFileAsync(script, [
      join(parent, "duplicate-runtime"),
      "--postgres-directory", shared,
      "--objects-directory", shared,
    ]));
    await assert.rejects(() => execFileAsync(script, [
      join(parent, "invalid-port-runtime"),
      "--http-port", "70000",
    ]));
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("guided installer preserves custom mappings and invokes the deployment lifecycle in order", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-guided-installer-test-"));
  const target = join(parent, "runtime");
  const postgres = join(parent, "postgres");
  const objects = join(parent, "objects");
  const backups = join(parent, "backups");
  const binaryDirectory = join(parent, "bin");
  const log = join(parent, "docker.log");
  const installer = join(toolsDirectory, "install-self-hosted.sh");
  try {
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
    const { stdout } = await execFileWithInput(installer, [
      "--runtime-directory", target,
      "--postgres-directory", postgres,
      "--objects-directory", objects,
      "--backups-directory", backups,
      "--owner-email", "owner@example.invalid",
      "--owner-display-name", "Owner",
      "--tenant-name", "Test Workspace",
      "--currency", "CNY",
      "--timezone", "Asia/Shanghai",
      "--http-port", "7476",
    ], "\n", { env: environment });
    assert.match(stdout, /http:\/\/127\.0\.0\.1:7476/);
    await assert.rejects(() => stat(join(target, "owner-password")));
    const environmentFile = await readFile(join(target, "deployment.env"), "utf8");
    assert.match(environmentFile, new RegExp(`^SBM_POSTGRES_DATA_SOURCE=${postgres}$`, "m"));
    assert.match(environmentFile, new RegExp(`^SBM_OBJECTS_SOURCE=${objects}$`, "m"));
    assert.match(environmentFile, new RegExp(`^SBM_BACKUPS_DIRECTORY=${backups}$`, "m"));

    const calls = await readFile(log, "utf8");
    const pull = calls.indexOf(" pull database provision migrate app");
    const bootstrap = calls.indexOf("/app/bootstrap-owner");
    const start = calls.lastIndexOf(" up -d --no-build --pull never --wait app");
    const status = calls.lastIndexOf(" ps");
    assert.ok(pull >= 0 && bootstrap > pull && start > bootstrap && status > start);
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("streamed installer downloads and verifies a versioned release bundle before installation", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-streamed-installer-test-"));
  const version = "v9.8.7";
  const archive = join(parent, `smart-bill-manager-docker-${version}.tar.gz`);
  const streamedInstaller = join(parent, "install.sh");
  const target = join(parent, "runtime");
  const postgres = join(parent, "postgres");
  const objects = join(parent, "objects");
  const backups = join(parent, "backups");
  const binaryDirectory = join(parent, "bin");
  const dockerLog = join(parent, "docker.log");
  try {
    await execFileAsync(join(toolsDirectory, "build-self-hosted-bundle.sh"), [archive]);
    await writeFile(streamedInstaller, await readFile(join(toolsDirectory, "install-self-hosted.sh")), { mode: 0o755 });
    await mkdir(binaryDirectory);
    await writeFile(
      join(binaryDirectory, "curl"),
      '#!/bin/sh\noutput=\nurl=\nwhile [ "$#" -gt 0 ]; do\n  if [ "$1" = -o ]; then output=$2; shift 2; else url=$1; shift; fi\ndone\ncase "$url" in\n  *.sha256) cp "$FAKE_RELEASE_CHECKSUM" "$output" ;;\n  *) cp "$FAKE_RELEASE_ARCHIVE" "$output" ;;\nesac\n',
      { mode: 0o755 },
    );
    await writeFile(
      join(binaryDirectory, "docker"),
      '#!/bin/sh\nif [ "$1" = compose ] && [ "$2" = version ]; then printf "%s\\n" "2.24.4"; exit 0; fi\nprintf "%s\\n" "$*" >>"$FAKE_DOCKER_LOG"\n',
      { mode: 0o755 },
    );
    const environment = {
      ...process.env,
      PATH: `${binaryDirectory}:${process.env.PATH}`,
      TMPDIR: parent,
      FAKE_RELEASE_ARCHIVE: archive,
      FAKE_RELEASE_CHECKSUM: `${archive}.sha256`,
      FAKE_DOCKER_LOG: dockerLog,
    };
    const { stdout } = await execFileWithInput(streamedInstaller, [
      "--release-version", version,
      "--runtime-directory", target,
      "--postgres-directory", postgres,
      "--objects-directory", objects,
      "--backups-directory", backups,
      "--owner-email", "owner@example.invalid",
      "--owner-display-name", "Owner",
      "--tenant-name", "Streamed Workspace",
      "--currency", "CNY",
      "--timezone", "Asia/Shanghai",
      "--http-port", "7476",
    ], "\n", { env: environment });
    assert.match(stdout, /smart-bill-manager-docker-v9\.8\.7\.tar\.gz: OK/);
    assert.match(stdout, /http:\/\/127\.0\.0\.1:7476/);
    assert.equal((await readdir(parent)).some((name) => name.startsWith("sbm-release-install.")), false);
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("streamed installer rejects invalid versions and checksum mismatches without leaving downloads", async () => {
  const parent = await mkdtemp(join(tmpdir(), "sbm-streamed-installer-failure-test-"));
  const streamedInstaller = join(parent, "install.sh");
  const binaryDirectory = join(parent, "bin");
  const fakeArchive = join(parent, "fake.tar.gz");
  const fakeChecksum = join(parent, "fake.tar.gz.sha256");
  try {
    await writeFile(streamedInstaller, await readFile(join(toolsDirectory, "install-self-hosted.sh")), { mode: 0o755 });
    await assert.rejects(() => execFileAsync(streamedInstaller, ["--release-version", "latest"]));

    await mkdir(binaryDirectory);
    await writeFile(fakeArchive, "not a release bundle");
    await writeFile(
      fakeChecksum,
      `${"0".repeat(64)}  smart-bill-manager-docker-v9.8.7.tar.gz\n`,
    );
    await writeFile(
      join(binaryDirectory, "curl"),
      '#!/bin/sh\noutput=\nurl=\nwhile [ "$#" -gt 0 ]; do\n  if [ "$1" = -o ]; then output=$2; shift 2; else url=$1; shift; fi\ndone\ncase "$url" in\n  *.sha256) cp "$FAKE_RELEASE_CHECKSUM" "$output" ;;\n  *) cp "$FAKE_RELEASE_ARCHIVE" "$output" ;;\nesac\n',
      { mode: 0o755 },
    );
    const environment = {
      ...process.env,
      PATH: `${binaryDirectory}:${process.env.PATH}`,
      TMPDIR: parent,
      FAKE_RELEASE_ARCHIVE: fakeArchive,
      FAKE_RELEASE_CHECKSUM: fakeChecksum,
    };
    await assert.rejects(() => execFileAsync(streamedInstaller, [
      "--release-version", "v9.8.7",
    ], { env: environment }));
    assert.equal((await readdir(parent)).some((name) => name.startsWith("sbm-release-install.")), false);
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
