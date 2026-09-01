#!/usr/bin/env node

import { createHash } from "node:crypto";
import { execFile as execFileCallback } from "node:child_process";
import { lstat, readFile, realpath } from "node:fs/promises";
import { promisify } from "node:util";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  SafeToolError,
  parseStrictPairs,
  requireDistinctPaths,
  requireGitSHA,
  requireImageID,
  requireSHA256,
  reserveProtectedFile,
  safeErrorCode,
} from "./lib/protected-output.mjs";

const execFile = promisify(execFileCallback);
const currentFile = fileURLToPath(import.meta.url);
const defaultRepositoryRoot = resolve(dirname(currentFile), "..");
const expectedProviderImageID =
  "sha256:244cc2b53f46f9e876304391d17682b0ddae9ac33491f4857e25e35a36ba7995";
const releaseRoots = ["apps/api", "apps/web", "contracts", "infra/migrations"];
const releaseFiles = [
  ".dockerignore",
  "infra/docker/app.Dockerfile",
  "infra/docker/entrypoint.sh",
  "tools/acceptance-start.sh",
  "tools/synthetic-provider.mjs",
];
const requiredImageFiles = [
  "/app/server",
  "/app/bootstrap-owner",
  "/app/backup",
  "/app/migrate",
  "/app/provision-postgresql",
  "/app/run-as-sbm",
  "/app/web/index.html",
  "/app/migrations/0001_initial.sql",
  "/app/contracts/bill-visible-text.schema.json",
  "/usr/local/bin/sbm-entrypoint",
  "/usr/local/bin/pg_dump",
  "/usr/local/bin/pg_restore",
  "/opt/sbm-poppler/bin/pdfinfo",
  "/opt/sbm-poppler/bin/pdftoppm",
  "/opt/sbm-poppler/lib/libpoppler.so.160",
  "/usr/share/zoneinfo/Asia/Shanghai",
  "/usr/share/zoneinfo/zone.tab",
];
const expectedImageEnvironment = [
  "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
  "SBM_POSTGRES_HOST=database",
  "SBM_POSTGRES_PORT=5432",
  "SBM_POSTGRES_DATABASE=smart_bill_manager",
  "SBM_POSTGRES_USER=sbm_runtime",
  "SBM_POSTGRES_PASSWORD_FILE=/run/sbm-secrets/postgres-runtime-password",
  "SBM_POSTGRES_SSL_MODE=disable",
  "SBM_POSTGRES_MAX_OPEN_CONNECTIONS=32",
  "SBM_MIGRATIONS_DIR=/app/migrations",
  "SBM_HTTP_ADDRESS=0.0.0.0:8080",
  "SBM_OBJECTS_PATH=/var/lib/sbm/objects",
  "SBM_PDFINFO_PATH=/opt/sbm-poppler/bin/pdfinfo",
  "SBM_PDFTOPPM_PATH=/opt/sbm-poppler/bin/pdftoppm",
  "SBM_MASTER_KEY_FILE=/run/sbm-secrets/master-key",
  "SBM_EXTRACTION_SCHEMA_PATH=/app/contracts/bill-visible-text.schema.json",
  "SBM_WEB_DIST_PATH=/app/web",
  "FONTCONFIG_FILE=/opt/sbm-poppler/etc/fonts/fonts.conf",
  "POPPLER_DATADIR=/opt/sbm-poppler/share/poppler",
];

async function main() {
  const [mode, ...argumentsList] = process.argv.slice(2);
  if (mode === "digest") {
    const values = parseStrictPairs(argumentsList, [], ["repository-root"]);
    const repositoryRoot = resolve(
      values.get("repository-root") ?? defaultRepositoryRoot,
    );
    process.stdout.write(
      `${JSON.stringify({ release_input_sha256: await releaseInputDigest(repositoryRoot) })}\n`,
    );
    return;
  }
  if (mode !== "check") throw new SafeToolError("invalid_arguments");
  const options = parseCheckArguments(argumentsList);
  const output = await reserveProtectedFile(options.output, [
    options.repositoryRoot,
    options.releaseArtifactsSource,
    options.masterKeySource,
    options.ownerPasswordSource,
    options.providerKeySource,
    options.postgresAdminPasswordSource,
    options.postgresMigrationPasswordSource,
    options.postgresRuntimePasswordSource,
  ]);
  try {
    await validateReleaseArtifactsSource(options);
    const actualInputDigest = await releaseInputDigest(options.repositoryRoot);
    if (actualInputDigest !== options.expectedReleaseInput) {
      throw new SafeToolError("release_identity_invalid");
    }
    const actualHead = (
      await run("git", ["rev-parse", "HEAD"], options.repositoryRoot)
    ).trim();
    if (actualHead !== options.expectedHead) {
      throw new SafeToolError("release_identity_invalid");
    }
    const composeEnvironment = buildComposeEnvironment(options);
    const [
      composeVersion,
      baseText,
      acceptanceText,
      inspectText,
      providerInspectText,
      inventory,
    ] = await Promise.all([
      run("docker", ["compose", "version", "--short"], options.repositoryRoot),
      run(
        "docker",
        [
          "compose",
          "-f",
          "infra/compose/compose.yaml",
          "config",
          "--format",
          "json",
        ],
        options.repositoryRoot,
        composeEnvironment,
      ),
      run(
        "docker",
        [
          "compose",
          "-f",
          "infra/compose/compose.yaml",
          "-f",
          "infra/compose/compose.acceptance.yaml",
          "config",
          "--format",
          "json",
        ],
        options.repositoryRoot,
        composeEnvironment,
      ),
      run(
        "docker",
        ["image", "inspect", options.image],
        options.repositoryRoot,
      ),
      run(
        "docker",
        ["image", "inspect", "node:24.19.0-alpine3.23"],
        options.repositoryRoot,
      ),
      run(
        "docker",
        [
          "run",
          "--rm",
          "--network",
          "none",
          "--memory",
          "256m",
          "--pids-limit",
          "64",
          "--cpus",
          "0.5",
          "--read-only",
          "--entrypoint",
          "/bin/sh",
          options.image,
          "-c",
          "find / -xdev -type f -print 2>/dev/null | LC_ALL=C sort; printf '%s\\n' __SBM_COMMANDS__; for command_name in go node npm apk apt apt-get dpkg; do if command -v \"$command_name\" >/dev/null 2>&1; then printf '%s=present\\n' \"$command_name\"; else printf '%s=absent\\n' \"$command_name\"; fi; done; /opt/sbm-poppler/bin/pdfinfo -v 2>&1 | sed -n '1s/^/pdfinfo=/p'; /opt/sbm-poppler/bin/pdftoppm -v 2>&1 | sed -n '1s/^/pdftoppm=/p'; pg_dump --version | sed 's/^/pg_dump=/'; pg_restore --version | sed 's/^/pg_restore=/'",
        ],
        options.repositoryRoot,
      ),
    ]);
    const base = JSON.parse(baseText);
    const acceptance = JSON.parse(acceptanceText);
    const inspected = JSON.parse(inspectText);
    const providerInspected = JSON.parse(providerInspectText);
    if (!Array.isArray(inspected) || inspected.length !== 1) {
      throw new SafeToolError("image_inspection_failed");
    }
    if (!Array.isArray(providerInspected) || providerInspected.length !== 1) {
      throw new SafeToolError("image_inspection_failed");
    }
    const composeResult = validateCompose(
      base,
      acceptance,
      options,
      providerInspected[0]?.Id,
    );
    const imageResult = validateImage(
      inspected[0],
      inventory,
      options.expectedHead,
      options.expectedReleaseInput,
    );
    const failedGates = [
      ...composeResult.failed_gates.map((gate) => `compose:${gate}`),
      ...imageResult.failed_gates.map((gate) => `image:${gate}`),
    ];
    const report = {
      report_kind: "m4-local-release-image-result",
      protocol_version: 1,
      build_identity: {
        baseline_head: options.expectedHead,
        release_input_sha256: options.expectedReleaseInput,
        image_id: imageResult.image_id,
      },
      compose: {
        version: composeVersion.trim(),
        base_config_sha256: sha256(
          stableStringify(sanitizeCompose(base, options.repositoryRoot)),
        ),
        acceptance_config_sha256: sha256(
          stableStringify(sanitizeCompose(acceptance, options.repositoryRoot)),
        ),
        ...composeResult,
      },
      image: imageResult,
      failed_gates: failedGates,
      passed: failedGates.length === 0,
    };
    await output.writeJSON(report);
    process.stdout.write(
      `${JSON.stringify({ report_kind: report.report_kind, failed_gate_count: failedGates.length, passed: report.passed })}\n`,
    );
    if (!report.passed) process.exitCode = 1;
  } finally {
    await output.close();
  }
}

function parseCheckArguments(argumentsList) {
  const values = parseStrictPairs(
    argumentsList,
    [
      "output",
      "image",
      "expected-head",
      "expected-release-input-sha256",
      "master-key-source",
      "owner-password-source",
      "provider-key-source",
      "postgres-admin-password-source",
      "postgres-migration-password-source",
      "postgres-runtime-password-source",
      "release-artifacts-source",
      "exercise-id",
    ],
    ["repository-root"],
  );
  const image = values.get("image");
  if (image !== "smart-bill-manager:local") {
    throw new SafeToolError("image_identity_invalid");
  }
  const exerciseID = values.get("exercise-id");
  if (
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      exerciseID,
    )
  ) {
    throw new SafeToolError("synthetic_identity_invalid");
  }
  const [
    masterKeySource,
    ownerPasswordSource,
    providerKeySource,
    postgresAdminPasswordSource,
    postgresMigrationPasswordSource,
    postgresRuntimePasswordSource,
  ] =
    requireDistinctPaths([
      values.get("master-key-source"),
      values.get("owner-password-source"),
      values.get("provider-key-source"),
      values.get("postgres-admin-password-source"),
      values.get("postgres-migration-password-source"),
      values.get("postgres-runtime-password-source"),
    ]);
  return {
    repositoryRoot: resolve(
      values.get("repository-root") ?? defaultRepositoryRoot,
    ),
    output: resolve(values.get("output")),
    image,
    expectedHead: requireGitSHA(values.get("expected-head")),
    expectedReleaseInput: requireSHA256(
      values.get("expected-release-input-sha256"),
    ),
    masterKeySource,
    ownerPasswordSource,
    providerKeySource,
    postgresAdminPasswordSource,
    postgresMigrationPasswordSource,
    postgresRuntimePasswordSource,
    releaseArtifactsSource: resolve(values.get("release-artifacts-source")),
    exerciseID,
  };
}

function buildComposeEnvironment(options) {
  const forwarded = {};
  for (const name of [
    "PATH",
    "HOME",
    "DOCKER_HOST",
    "DOCKER_CONTEXT",
    "DOCKER_CONFIG",
    "XDG_CONFIG_HOME",
  ]) {
    if (process.env[name]) forwarded[name] = process.env[name];
  }
  return {
    ...forwarded,
    SBM_BUILD_SHA: options.expectedHead,
    SBM_RELEASE_INPUT_SHA256: options.expectedReleaseInput,
    SBM_MASTER_KEY_SOURCE: options.masterKeySource,
    SBM_POSTGRES_ADMIN_PASSWORD_SOURCE:
      options.postgresAdminPasswordSource,
    SBM_POSTGRES_MIGRATION_PASSWORD_SOURCE:
      options.postgresMigrationPasswordSource,
    SBM_POSTGRES_RUNTIME_PASSWORD_SOURCE:
      options.postgresRuntimePasswordSource,
    SBM_DEPLOYMENT_MODE: "local",
    SBM_COOKIE_SECURE: "false",
    SBM_BIND_ADDRESS: "127.0.0.1",
    SBM_HTTP_PORT: "8080",
    SBM_SESSION_TTL: "168h",
    SBM_AI_CONCURRENCY: "2",
    SBM_RELEASE_ARTIFACTS_SOURCE: options.releaseArtifactsSource,
    SBM_ACCEPTANCE_OWNER_PASSWORD_SOURCE: options.ownerPasswordSource,
    SBM_ACCEPTANCE_PROVIDER_KEY_SOURCE: options.providerKeySource,
    SBM_ACCEPTANCE_EXERCISE_ID: options.exerciseID,
  };
}

export async function releaseInputDigest(repositoryRoot) {
  const paths = await releaseInputPaths(repositoryRoot);
  const digest = createHash("sha256");
  for (const path of paths) {
    const absolute = resolve(repositoryRoot, path);
    const information = await lstat(absolute);
    const content = await readFile(absolute);
    digest.update(`${Buffer.byteLength(path)}:${path}:`);
    digest.update(information.mode & 0o111 ? "x:" : "-:");
    digest.update(`${content.length}:`);
    digest.update(content);
  }
  return digest.digest("hex");
}

export async function releaseInputPaths(repositoryRoot) {
  const pathsText = await run(
    "git",
    [
      "ls-files",
      "--cached",
      "--others",
      "--exclude-standard",
      "-z",
      "--",
      ...releaseRoots,
      ...releaseFiles,
    ],
    repositoryRoot,
    undefined,
    "buffer",
  );
  const listedPaths = pathsText
    .toString("utf8")
    .split("\0")
    .filter(Boolean)
    .filter(isDockerBuildInput)
    .sort();
  const paths = [];
  for (const path of listedPaths) {
    const absolute = resolve(repositoryRoot, path);
    if (
      relative(repositoryRoot, absolute).startsWith(`..${sep}`) ||
      relative(repositoryRoot, absolute) === ".."
    ) {
      throw new SafeToolError("release_input_invalid");
    }
    let information;
    try {
      information = await lstat(absolute);
    } catch (error) {
      if (error?.code === "ENOENT") continue;
      throw new SafeToolError("release_input_invalid");
    }
    if (!information.isFile() || information.isSymbolicLink()) {
      throw new SafeToolError("release_input_invalid");
    }
    paths.push(path);
  }
  if (!releaseFiles.every((path) => paths.includes(path))) {
    throw new SafeToolError("release_input_incomplete");
  }
  return paths;
}

function isDockerBuildInput(path) {
  if (path.includes("/node_modules/") || path.includes("/dist/")) return false;
  if (
    path.includes("/coverage/") ||
    path.includes("/test-results/") ||
    /(?:^|\/)\.env(?:\.|$)/.test(path)
  ) {
    return false;
  }
  if (/\.(?:pem|key|p12|pfx|db|log)$/.test(path)) return false;
  if (/\.(?:spec|test)\.ts$/.test(path)) return false;
  return true;
}

export function validateCompose(base, acceptance, options, providerImageID) {
  const failed = [];
  const app = base.services?.app;
  const database = base.services?.database;
  const provision = base.services?.provision;
  const migrate = base.services?.migrate;
  const overlayApp = acceptance.services?.app;
  const provider = acceptance.services?.provider;
  gate(
    failed,
    "base_service_set",
    sameMembers(Object.keys(base.services ?? {}), [
      "app",
      "database",
      "provision",
      "migrate",
    ]),
  );
  gate(
    failed,
    "project_name",
    base.name === "smart-bill-manager" && acceptance.name === base.name,
  );
  gate(
    failed,
    "build_identity",
    app?.image === "smart-bill-manager:local" &&
      app?.pull_policy === "never" &&
      app?.build?.context === options.repositoryRoot &&
      app?.build?.dockerfile === "infra/docker/app.Dockerfile" &&
      app?.build?.network === "none" &&
      validLocalBuildContexts(
        app?.build?.additional_contexts,
        options.releaseArtifactsSource,
      ) &&
      app?.build?.args?.GLIBC_SOURCE_IMAGE ===
        "smart-bill-manager:go-glibc-source-local" &&
      app?.build?.args?.VCS_REF === options.expectedHead &&
      app?.build?.args?.RELEASE_INPUT_SHA256 === options.expectedReleaseInput,
  );
  gate(
    failed,
    "loopback_port",
    app?.ports?.length === 1 &&
      app.ports[0]?.host_ip === "127.0.0.1" &&
      app.ports[0]?.target === 8080 &&
      app.ports[0]?.published === "8080",
  );
  gate(
    failed,
    "filesystem",
    app?.read_only === true &&
      sameMembers(app.tmpfs ?? [], [
        "/tmp:rw,noexec,nosuid,nodev,size=268435456",
        "/run/sbm-secrets:rw,noexec,nosuid,nodev,size=65536,mode=0700",
      ]) &&
      sameMembers(
        (app.volumes ?? []).map((volume) => volume.target),
        ["/var/lib/sbm/objects"],
      ) &&
      sameMembers(Object.keys(base.volumes ?? {}), [
        "sbm_postgres_data",
        "sbm_objects",
      ]) &&
      sameMembers(Object.keys(acceptance.volumes ?? {}), [
        "sbm_postgres_data",
        "sbm_objects",
      ]),
  );
  gate(
    failed,
    "privileges",
    sameMembers(app?.cap_drop ?? [], ["ALL"]) &&
      sameMembers(app?.cap_add ?? [], [
        "CHOWN",
        "DAC_OVERRIDE",
        "SETGID",
        "SETUID",
      ]) &&
      sameMembers(app?.security_opt ?? [], ["no-new-privileges:true"]),
  );
  gate(
    failed,
    "resources",
    app?.pids_limit === 256 &&
      app?.cpus === 2 &&
      app?.mem_limit === "3758096384" &&
      app?.stop_grace_period === "20s" &&
      app?.init === true &&
      app?.restart === "unless-stopped",
  );
  gate(
    failed,
    "environment",
    app?.environment?.SBM_POSTGRES_HOST === "database" &&
      app.environment.SBM_POSTGRES_PORT === "5432" &&
      app.environment.SBM_POSTGRES_DATABASE === "smart_bill_manager" &&
      app.environment.SBM_POSTGRES_USER === "sbm_runtime" &&
      app.environment.SBM_POSTGRES_PASSWORD_FILE === "/run/sbm-secrets/postgres-runtime-password" &&
      app.environment.SBM_POSTGRES_SSL_MODE === "disable" &&
      app.environment.SBM_AI_CONCURRENCY === "2" &&
      app.environment.SBM_COOKIE_SECURE === "false" &&
      app.environment.SBM_DEPLOYMENT_MODE === "local" &&
      app.environment.SBM_SESSION_TTL === "168h",
  );
  gate(
    failed,
    "healthcheck",
    JSON.stringify(app?.healthcheck?.test) ===
      JSON.stringify([
        "CMD",
        "wget",
        "-q",
        "-O",
        "/dev/null",
        "http://127.0.0.1:8080/api/v1/ready",
      ]) &&
      app?.healthcheck?.interval === "10s" &&
      app?.healthcheck?.timeout === "3s" &&
      app?.healthcheck?.start_period === "10s" &&
      app?.healthcheck?.retries === 6,
  );
  gate(
    failed,
    "master_key_secret",
    sameMembers((app?.secrets ?? []).map((secret) => secret.source), [
      "sbm_master_key",
      "sbm_postgres_runtime_password",
    ]) &&
      sameMembers(Object.keys(base.secrets ?? {}), [
        "sbm_master_key",
        "sbm_postgres_admin_password",
        "sbm_postgres_migration_password",
        "sbm_postgres_runtime_password",
      ]) &&
      sameMembers(Object.keys(acceptance.secrets ?? {}), [
        "sbm_master_key",
        "sbm_postgres_admin_password",
        "sbm_postgres_migration_password",
        "sbm_postgres_runtime_password",
        "sbm_owner_password",
        "sbm_provider_key",
      ]),
  );
  gate(
    failed,
    "postgresql_service",
    database?.image === "postgres:17-alpine" &&
      database?.pull_policy === "never" &&
      database?.ports === undefined &&
      database?.read_only === true &&
      sameMembers(database?.tmpfs ?? [], [
        "/tmp:rw,noexec,nosuid,nodev,size=16777216",
        "/var/run/postgresql:rw,noexec,nosuid,nodev,size=16777216,mode=3775",
      ]) &&
      database?.mem_limit === "2147483648" &&
      database?.pids_limit === 256 &&
      database?.networks?.database !== undefined &&
      base.networks?.database?.internal === true &&
      provision?.depends_on?.database?.condition === "service_healthy" &&
      migrate?.depends_on?.provision?.condition === "service_completed_successfully" &&
      app?.depends_on?.migrate?.condition === "service_completed_successfully",
  );
  const command = provider?.command ?? [];
  const acceptanceAppVolumes = overlayApp?.volumes ?? [];
  const expectedProviderCommand = [
    "node",
    "/opt/sbm/synthetic-provider.mjs",
    "--listen",
    "127.0.0.1:19086",
    "--api-key-file",
    "/run/secrets/sbm_provider_key",
    "--model",
    "synthetic-local-release",
    "--exercise-id",
    options.exerciseID,
  ];
  const providerVolumes = provider?.volumes ?? [];
  gate(
    failed,
    "acceptance_provider",
    sameMembers(Object.keys(acceptance.services ?? {}), [
      "app",
      "database",
      "provision",
      "migrate",
      "provider",
    ]) &&
      provider?.image === "node:24.19.0-alpine3.23" &&
      provider?.pull_policy === "never" &&
      providerImageID === expectedProviderImageID &&
      provider?.network_mode === "service:app" &&
      provider?.read_only === true &&
      provider?.user === "node" &&
      sameMembers(provider?.cap_drop ?? [], ["ALL"]) &&
      sameMembers(provider?.security_opt ?? [], ["no-new-privileges:true"]) &&
      provider?.ports === undefined &&
      JSON.stringify(command) === JSON.stringify(expectedProviderCommand) &&
      JSON.stringify(overlayApp?.command) ===
        JSON.stringify(["/opt/sbm/acceptance-start.sh"]) &&
      acceptanceAppVolumes.length === 2 &&
      acceptanceAppVolumes.some(
        (volume) =>
          volume?.type === "bind" &&
          volume?.source ===
            resolve(options.repositoryRoot, "tools/acceptance-start.sh") &&
          volume?.target === "/opt/sbm/acceptance-start.sh" &&
          volume?.read_only === true,
      ) &&
      provider?.depends_on?.app?.condition === "service_started" &&
      provider?.depends_on?.app?.required === true &&
      provider?.pids_limit === 64 &&
      provider?.cpus === 0.5 &&
      provider?.mem_limit === "268435456" &&
      provider?.restart === "no" &&
      sameMembers(provider?.tmpfs ?? [], [
        "/tmp:rw,noexec,nosuid,nodev,size=16777216",
      ]) &&
      providerVolumes.length === 1 &&
      providerVolumes[0]?.type === "bind" &&
      providerVolumes[0]?.source ===
        resolve(options.repositoryRoot, "tools/synthetic-provider.mjs") &&
      providerVolumes[0]?.target === "/opt/sbm/synthetic-provider.mjs" &&
      providerVolumes[0]?.read_only === true,
  );
  gate(
    failed,
    "acceptance_network_isolation",
    sameMembers(Object.keys(acceptance.networks ?? {}), ["default", "database"]) &&
      acceptance.networks.default?.internal === true &&
      acceptance.networks.database?.internal === true,
  );
  gate(
    failed,
    "acceptance_secret_separation",
    overlayApp?.secrets?.length === 3 &&
      overlayApp.secrets.some(
        (secret) =>
          secret.source === "sbm_owner_password" &&
          secret.target === "sbm_owner_password" &&
          secret.uid === undefined &&
          secret.gid === undefined &&
          secret.mode === undefined,
      ) &&
      overlayApp.secrets.some(
        (secret) =>
          secret.source === "sbm_postgres_runtime_password" &&
          secret.target === "/run/secrets/sbm_postgres_runtime_password" &&
          secret.uid === undefined &&
          secret.gid === undefined &&
          secret.mode === undefined,
      ) &&
      overlayApp.secrets.some(
        (secret) =>
          secret.source === "sbm_master_key" &&
          secret.target === "/run/secrets/sbm_master_key" &&
          secret.uid === undefined &&
          secret.gid === undefined &&
          secret.mode === undefined,
      ) &&
      provider?.secrets?.length === 1 &&
      provider.secrets[0]?.source === "sbm_provider_key" &&
      provider.secrets[0]?.target === "sbm_provider_key" &&
      provider.secrets[0]?.uid === undefined &&
      provider.secrets[0]?.gid === undefined &&
      provider.secrets[0]?.mode === undefined,
  );
  return {
    static_gate_count: 13,
    failed_gates: failed,
    passed: failed.length === 0,
  };
}

export function validateImage(
  inspected,
  inventoryText,
  expectedHead,
  expectedReleaseInput,
) {
  const [filesText, commandsText = ""] =
    inventoryText.split("__SBM_COMMANDS__\n");
  const files = new Set(filesText.split("\n").filter(Boolean));
  const required = Object.fromEntries(
    requiredImageFiles.map((path) => [
      path.slice(1).replaceAll("/", "_"),
      files.has(path),
    ]),
  );
  const forbiddenFiles = [...files].filter(
    (path) =>
      /^\/app\/seed-performance(?:\/|$)/.test(path) ||
      /^\/app\/recovery-exercise(?:\/|$)/.test(path) ||
      /^\/app\/(?:tools|tests|docs|evidence)(?:\/|$)/.test(path) ||
      (/^\/app\//.test(path) &&
        /\.(?:go|ts|tsx|vue|map|db|log)$/.test(path)) ||
      /^\/var\/lib\/sbm\/.*\.(?:db|log)$/.test(path) ||
      /(?:master-key|owner-password|provider-key)/i.test(path) ||
      path === "/usr/local/bin/node" ||
      path.startsWith("/usr/local/go/"),
  );
  const labels = inspected.Config?.Labels ?? {};
  const failed = [];
  gate(failed, "image_id", /^sha256:[0-9a-f]{64}$/.test(inspected.Id ?? ""));
  gate(
    failed,
    "labels",
    labels["org.opencontainers.image.title"] === "Smart Bill Manager" &&
      labels["org.opencontainers.image.revision"] === expectedHead &&
      labels["com.smart-bill-manager.release-input-sha256"] ===
        expectedReleaseInput &&
      labels["com.smart-bill-manager.node-build-version"] === "24.19.0" &&
      labels["com.smart-bill-manager.go-build-version"] === "go1.26.7" &&
      labels["com.smart-bill-manager.glibc-source-image-id"] ===
        "sha256:dc2521c2a906db43073b8b4d99f491b6341cf15610b6ebbab187c45153f9959e" &&
      labels["com.smart-bill-manager.runtime-contract"] ===
        "alpine-3.23-glibc-2.41-poppler-26.05-tzdata/1",
  );
  gate(
    failed,
    "entrypoint",
    JSON.stringify(inspected.Config?.Entrypoint) ===
      JSON.stringify(["/usr/local/bin/sbm-entrypoint"]) &&
      JSON.stringify(inspected.Config?.Cmd) === JSON.stringify(["/app/server"]),
  );
  gate(failed, "required_assets", Object.values(required).every(Boolean));
  gate(failed, "forbidden_assets", forbiddenFiles.length === 0);
  gate(
    failed,
    "toolchains_absent",
    ["go=absent", "node=absent", "npm=absent"].every((entry) =>
      commandsText.split("\n").includes(entry),
    ),
  );
  gate(
    failed,
    "package_managers_absent",
    ["apk=absent", "apt=absent", "apt-get=absent", "dpkg=absent"].every(
      (entry) => commandsText.split("\n").includes(entry),
    ),
  );
  gate(
    failed,
    "pdf_runtime",
    commandsText.split("\n").includes("pdfinfo=pdfinfo version 26.05.0") &&
      commandsText.split("\n").includes("pdftoppm=pdftoppm version 26.05.0"),
  );
  gate(
    failed,
    "postgresql_tools",
    commandsText.split("\n").some((entry) => /^pg_dump=pg_dump \(PostgreSQL\) 17\./.test(entry)) &&
      commandsText.split("\n").some((entry) => /^pg_restore=pg_restore \(PostgreSQL\) 17\./.test(entry)),
  );
  gate(
    failed,
    "runtime_metadata",
    inspected.Config?.WorkingDir === "/app" &&
      inspected.Config?.User === "root" &&
      inspected.Config?.ExposedPorts?.["8080/tcp"] !== undefined &&
      sameMembers(Object.keys(inspected.Config?.Volumes ?? {}), [
        "/var/lib/sbm/objects",
      ]),
  );
  gate(
    failed,
    "runtime_environment",
    sameMembers(inspected.Config?.Env ?? [], expectedImageEnvironment),
  );
  gate(
    failed,
    "healthcheck",
    JSON.stringify(inspected.Config?.Healthcheck?.Test) ===
      JSON.stringify([
        "CMD-SHELL",
        "wget -q -O /dev/null http://127.0.0.1:8080/api/v1/ready || exit 1",
      ]),
  );
  return {
    image_id: requireImageID(inspected.Id),
    required_assets: required,
    forbidden_assets_absent: forbiddenFiles.length === 0,
    toolchains_absent: !failed.includes("toolchains_absent"),
    package_managers_absent: !failed.includes("package_managers_absent"),
    static_gate_count: 12,
    failed_gates: failed,
    passed: failed.length === 0,
  };
}

function sanitizeCompose(value, repositoryRoot) {
  const result = structuredClone(value);
  for (const [name, secret] of Object.entries(result.secrets ?? {})) {
    if (secret.file) secret.file = `[PROTECTED_FILE:${name}]`;
  }
  for (const service of Object.values(result.services ?? {})) {
    if (service.build?.context === repositoryRoot) {
      service.build.context = "[REPOSITORY_ROOT]";
    }
    for (const name of Object.keys(service.build?.additional_contexts ?? {})) {
      service.build.additional_contexts[name] = `[LOCAL_CACHE_CONTEXT:${name}]`;
    }
    for (const volume of service.volumes ?? []) {
      if (
        volume.type === "bind" &&
        volume.source?.startsWith(`${repositoryRoot}${sep}`)
      ) {
        volume.source = `[REPOSITORY_ROOT]/${relative(repositoryRoot, volume.source)}`;
      }
    }
    const command = service.command ?? [];
    const exerciseIndex = command.indexOf("--exercise-id");
    if (exerciseIndex >= 0 && command[exerciseIndex + 1]) {
      command[exerciseIndex + 1] = "[SYNTHETIC_EXERCISE_UUIDV4]";
    }
  }
  return result;
}

function validLocalBuildContexts(contexts, expectedReleaseArtifactsSource) {
  if (!contexts || typeof contexts !== "object" || Array.isArray(contexts)) {
    return false;
  }
  if (!sameMembers(Object.keys(contexts), ["release_artifacts"])) {
    return false;
  }
  const context = contexts.release_artifacts;
  return (
    typeof context === "string" &&
    isAbsolute(context) &&
    context === expectedReleaseArtifactsSource &&
    !context.includes("://") &&
    !context.startsWith("git@")
  );
}

async function validateReleaseArtifactsSource(options) {
  const source = options.releaseArtifactsSource;
  if (source === "/tmp" || !source.startsWith("/tmp/")) {
    throw new SafeToolError("release_artifacts_invalid");
  }
  let information;
  let resolved;
  let identity;
  try {
    [information, resolved, identity] = await Promise.all([
      lstat(source),
      realpath(source),
      readFile(join(source, "identity.env"), "utf8"),
    ]);
  } catch {
    throw new SafeToolError("release_artifacts_invalid");
  }
  const lines = new Set(identity.split("\n").filter(Boolean));
  if (
    !information.isDirectory() ||
    information.isSymbolicLink() ||
    information.uid !== process.getuid() ||
    (information.mode & 0o077) !== 0 ||
    resolved !== source ||
    !lines.has(`baseline_head=${options.expectedHead}`) ||
    !lines.has(`release_input_sha256=${options.expectedReleaseInput}`)
  ) {
    throw new SafeToolError("release_artifacts_invalid");
  }
}

function stableStringify(value) {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

function sameMembers(actual, expected) {
  return (
    actual.length === expected.length &&
    [...actual].sort().join("\0") === [...expected].sort().join("\0")
  );
}

function gate(failed, name, passed) {
  if (!passed) failed.push(name);
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

async function run(
  command,
  argumentsList,
  cwd,
  env = process.env,
  encoding = "utf8",
) {
  try {
    const result = await execFile(command, argumentsList, {
      cwd,
      env,
      encoding,
      maxBuffer: 32 * 1024 * 1024,
      timeout: 120_000,
    });
    return result.stdout;
  } catch {
    throw new SafeToolError("release_inspection_failed");
  }
}

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`release-image: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
