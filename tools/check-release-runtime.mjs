#!/usr/bin/env node

import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  SafeToolError,
  parseStrictPairs,
  readProtectedSecret,
  requireDistinctPaths,
  requireGitSHA,
  requireImageID,
  requireLoopbackURL,
  requireSHA256,
  reserveProtectedFile,
  safeErrorCode,
} from "./lib/protected-output.mjs";
import {
  clearBuffers,
  composeEnvironment,
  containsSecret,
  hasExpectedLoopbackBinding,
  runCaptured,
} from "./lib/local-release-command.mjs";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const expectedProviderImageID =
  "sha256:244cc2b53f46f9e876304391d17682b0ddae9ac33491f4857e25e35a36ba7995";
const expectedPostgreSQLImageID =
  "sha256:18cfe3ef5e6815560c98237d6216d1e5119702fb0f3894c8785dd58b8bbe5d73";
const expectedAppRuntimeEnvironment = [
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
  "SBM_AI_CONCURRENCY=2",
  "SBM_COOKIE_SECURE=false",
  "SBM_DEPLOYMENT_MODE=local",
  "SBM_SESSION_TTL=168h",
];
const expectedProviderEnvironment = [
  "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
  "NODE_VERSION=24.19.0",
  "YARN_VERSION=1.22.22",
];

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const output = await reserveProtectedFile(options.output, [
    options.masterKeySource,
    options.ownerPasswordSource,
    options.providerKeySource,
    options.postgresAdminPasswordSource,
    options.postgresMigrationPasswordSource,
    options.postgresRuntimePasswordSource,
    options.releaseArtifactsSource,
  ]);
  let masterKey;
  let ownerPassword;
  let providerKey;
  let postgresAdminPassword;
  let postgresMigrationPassword;
  let postgresRuntimePassword;
  let stage = "secret_loading";
  const commandOutputs = [];
  try {
    [
      masterKey,
      ownerPassword,
      providerKey,
      postgresAdminPassword,
      postgresMigrationPassword,
      postgresRuntimePassword,
    ] = await Promise.all([
      readProtectedSecret(options.masterKeySource, 128),
      readProtectedSecret(options.ownerPasswordSource, 1024),
      readProtectedSecret(options.providerKeySource, 4096),
      readProtectedSecret(options.postgresAdminPasswordSource, 1024),
      readProtectedSecret(options.postgresMigrationPasswordSource, 1024),
      readProtectedSecret(options.postgresRuntimePasswordSource, 1024),
    ]);
    stage = "compose_lookup";
    const environment = composeEnvironment(options);
    const appID = await composeServiceID(options, environment, "app");
    const providerID = await composeServiceID(options, environment, "provider");
    const databaseID = await composeServiceID(options, environment, "database");
    stage = "container_inspection";
    const [
      appInspect,
      providerInspect,
      databaseInspect,
      networkInspect,
      databaseNetworkInspect,
      processMetadata,
      permissions,
      history,
      databaseRoles,
    ] = await Promise.all([
      dockerJSON(["inspect", appID], environment, commandOutputs),
      dockerJSON(["inspect", providerID], environment, commandOutputs),
      dockerJSON(["inspect", databaseID], environment, commandOutputs),
      dockerJSON(
        ["network", "inspect", `${options.projectName}_default`],
        environment,
        commandOutputs,
      ),
      dockerJSON(
        ["network", "inspect", `${options.projectName}_database`],
        environment,
        commandOutputs,
      ),
      dockerCommand(
        [
          "exec",
          "--user",
          "10001:10001",
          appID,
          "/bin/sh",
          "-c",
          "set -eu; server_pid=''; for status_file in /proc/[0-9]*/status; do if grep -q '^Name:[[:space:]]*server$' \"$status_file\"; then server_pid=${status_file#/proc/}; server_pid=${server_pid%/status}; break; fi; done; test -n \"$server_pid\"; printf '%s\\n' __STATUS__; grep -E '^(Name|Uid|Gid):' \"/proc/$server_pid/status\"; printf '%s\\n' __CMDLINE__; tr '\\000' ' ' < \"/proc/$server_pid/cmdline\"; printf '\\n%s\\n' __ENVIRON__; tr '\\000' '\\n' < \"/proc/$server_pid/environ\"",
        ],
        environment,
        commandOutputs,
      ),
      dockerCommand(
        [
          "exec",
          "--user",
          "10001:10001",
          appID,
          "/bin/sh",
          "-c",
          "set -eu; test -r /run/sbm-secrets/master-key; test -r /run/sbm-secrets/postgres-runtime-password; test ! -w /run/sbm-secrets; test ! -e /run/sbm-secrets/owner-password; test ! -r /run/secrets/sbm_master_key; test ! -r /run/secrets/sbm_owner_password; test ! -r /run/secrets/sbm_postgres_runtime_password; test \"$(stat -c '%a:%u:%g' /run/sbm-secrets)\" = '710:0:10001'; test \"$(stat -c '%h:%a:%u:%g' /run/sbm-secrets/master-key)\" = '1:600:10001:10001'; test \"$(stat -c '%h:%a:%u:%g' /run/sbm-secrets/postgres-runtime-password)\" = '1:600:10001:10001'; test ! -w /app; test -w /tmp; test -w /var/lib/sbm/objects",
        ],
        environment,
        commandOutputs,
        true,
      ),
      dockerCommand(
        ["image", "history", "--no-trunc", options.image],
        environment,
        commandOutputs,
        true,
      ),
      dockerCommand(
        [
          "exec",
          databaseID,
          "psql",
          "-X",
          "-A",
          "-t",
          "-v",
          "ON_ERROR_STOP=1",
          "-U",
          "sbm_admin",
          "-d",
          "postgres",
          "-c",
          "SELECT string_agg(rolname || ':' || rolcanlogin::text || ':' || rolsuper::text || ':' || rolcreatedb::text || ':' || rolcreaterole::text || ':' || rolreplication::text, ',' ORDER BY rolname) FROM pg_roles WHERE rolname IN ('sbm_migration','sbm_runtime'); SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname='smart_bill_manager'; SELECT has_database_privilege('sbm_runtime','smart_bill_manager','CONNECT'), has_database_privilege('sbm_runtime','smart_bill_manager','CREATE');",
        ],
        environment,
        commandOutputs,
      ),
    ]);
    const app = single(appInspect);
    const provider = single(providerInspect);
    const database = single(databaseInspect);
    const network = single(networkInspect);
    const databaseNetwork = single(databaseNetworkInspect);
    stage = "authentication";
    const authentication = await checkAuthentication(options, ownerPassword);
    stage = "log_inspection";
    const [appLogs, providerLogs, databaseLogs] = await Promise.all([
      dockerCommand(["logs", appID], environment, commandOutputs, true),
      dockerCommand(["logs", providerID], environment, commandOutputs, true),
      dockerCommand(["logs", databaseID], environment, commandOutputs, true),
    ]);
    const sensitiveOutputs = [
      ...commandOutputs,
      Buffer.from(JSON.stringify(authentication.raw), "utf8"),
    ];
    const secretHits = containsSecret(sensitiveOutputs, [
      masterKey,
      ownerPassword,
      providerKey,
      postgresAdminPassword,
      postgresMigrationPassword,
      postgresRuntimePassword,
    ]);
    stage = "runtime_validation";
    const security = validateRuntimeSecurity(
      app,
      provider,
      network,
      processMetadata.toString("utf8"),
      permissions.code,
      options,
    );
    security.inspection_commands_passed =
      appLogs.code === 0 &&
      providerLogs.code === 0 &&
      databaseLogs.code === 0 &&
      history.code === 0;
    security.passed = security.passed && security.inspection_commands_passed;
    const postgresqlRuntime = validatePostgreSQLRuntime(
      database,
      databaseNetwork,
      databaseRoles.toString("utf8"),
    );
    const authenticationResult = {
      ready_passed: authentication.ready,
      owner_login_passed: authentication.login,
      owner_session_passed: authentication.session,
      logout_invalidation_passed: authentication.logout,
      passed:
        authentication.ready &&
        authentication.login &&
        authentication.session &&
        authentication.logout,
    };
    const report = {
      report_kind: "m4-local-release-runtime-result",
      protocol_version: 1,
      build_identity: {
        baseline_head: options.expectedHead,
        release_input_sha256: options.expectedReleaseInput,
        image_id: options.imageID,
      },
      authentication: authenticationResult,
      runtime_security: security,
      postgresql_runtime: postgresqlRuntime,
      secret_scan: {
        argv_environment_history_log_hits: secretHits ? 1 : 0,
        passed: !secretHits,
      },
      passed:
        authenticationResult.passed &&
        security.passed &&
        postgresqlRuntime.passed &&
        !secretHits,
    };
    stage = "evidence_write";
    await output.writeJSON(report);
    process.stdout.write(
      `${JSON.stringify({
        report_kind: report.report_kind,
        authentication_passed: authenticationResult.passed,
        runtime_security_passed: security.passed,
        postgresql_runtime_passed: postgresqlRuntime.passed,
        secret_hits: report.secret_scan.argv_environment_history_log_hits,
        passed: report.passed,
      })}\n`,
    );
    if (!report.passed) process.exitCode = 1;
    clearBuffers([
      processMetadata,
      permissions.stdout,
      permissions.stderr,
      appLogs.stdout,
      appLogs.stderr,
      providerLogs.stdout,
      providerLogs.stderr,
      databaseLogs.stdout,
      databaseLogs.stderr,
      history.stdout,
      history.stderr,
      databaseRoles,
    ]);
  } catch (error) {
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError(`runtime_${stage}_failed`);
  } finally {
    masterKey?.fill(0);
    ownerPassword?.fill(0);
    providerKey?.fill(0);
    postgresAdminPassword?.fill(0);
    postgresMigrationPassword?.fill(0);
    postgresRuntimePassword?.fill(0);
    clearBuffers(commandOutputs);
    await output.close();
  }
}

export function parseArguments(argumentsList) {
  const values = parseStrictPairs(argumentsList, [
    "project-name",
    "server",
    "email",
    "output",
    "master-key-source",
    "owner-password-source",
    "provider-key-source",
    "postgres-admin-password-source",
    "postgres-migration-password-source",
    "postgres-runtime-password-source",
    "release-artifacts-source",
    "exercise-id",
    "expected-head",
    "expected-release-input-sha256",
    "image",
    "image-id",
  ]);
  const projectName = values.get("project-name");
  const exerciseID = values.get("exercise-id");
  if (
    !/^sbm-m4-[0-9a-f]{8}(?:-[a-z]+)?$/.test(projectName) ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      exerciseID,
    ) ||
    values.get("image") !== "smart-bill-manager:local" ||
    !/^[^\s@]+@[^\s@]+$/.test(values.get("email"))
  ) {
    throw new SafeToolError("synthetic_identity_invalid");
  }
  requireLoopbackURL(values.get("server"), { allowPath: false });
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
    projectName,
    server: values.get("server"),
    email: values.get("email"),
    output: resolve(values.get("output")),
    masterKeySource,
    ownerPasswordSource,
    providerKeySource,
    postgresAdminPasswordSource,
    postgresMigrationPasswordSource,
    postgresRuntimePasswordSource,
    releaseArtifactsSource: resolve(values.get("release-artifacts-source")),
    exerciseID,
    expectedHead: requireGitSHA(values.get("expected-head")),
    expectedReleaseInput: requireSHA256(
      values.get("expected-release-input-sha256"),
    ),
    image: values.get("image"),
    imageID: requireImageID(values.get("image-id")),
  };
}

export function validateRuntimeSecurity(
  app,
  provider,
  network,
  processMetadata,
  permissionExitCode,
  options,
) {
  const host = app.HostConfig ?? {};
  const mounts = app.Mounts ?? [];
  const providerHost = provider.HostConfig ?? {};
  const providerMounts = provider.Mounts ?? [];
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
  const gates = {
    healthy:
      app.State?.Health?.Status === "healthy" &&
      provider.State?.Running === true,
    image_identity:
      app.Image === options.imageID && app.Config?.Image === options.image,
    acceptance_startup_barrier:
      JSON.stringify(app.Config?.Cmd) ===
        JSON.stringify(["/opt/sbm/acceptance-start.sh"]),
    loopback_port_binding: hasExpectedLoopbackBinding(app, options.server),
    acceptance_loopback_bridge:
      app.NetworkSettings?.Ports?.["8080/tcp"] === null,
    environment: sameMembers(
      app.Config?.Env ?? [],
      expectedAppRuntimeEnvironment,
    ),
    process_identity:
      /^Uid:\s+10001\s+10001\s+10001\s+10001$/m.test(processMetadata) &&
      /^Gid:\s+10001\s+10001\s+10001\s+10001$/m.test(processMetadata) &&
      processMetadata.includes("/app/server"),
    read_only_root: host.ReadonlyRootfs === true && permissionExitCode === 0,
    capabilities:
      sameMembers(host.CapDrop ?? [], ["ALL"]) &&
      sameMembers(host.CapAdd ?? [], [
        "CAP_CHOWN",
        "CAP_DAC_OVERRIDE",
        "CAP_SETGID",
        "CAP_SETUID",
      ]) &&
      (host.SecurityOpt ?? []).some((entry) =>
        entry.startsWith("no-new-privileges"),
      ),
    resources:
      host.PidsLimit === 256 &&
      host.NanoCpus === 2_000_000_000 &&
      host.Memory === 3584 * 1024 * 1024,
    mounts:
      mounts.length === 5 &&
      requiredMount(mounts, "volume", "/var/lib/sbm/objects", true) &&
      requiredMount(mounts, "bind", "/run/secrets/sbm_master_key", false) &&
      requiredMount(mounts, "bind", "/run/secrets/sbm_postgres_runtime_password", false) &&
      requiredMount(mounts, "bind", "/run/secrets/sbm_owner_password", false) &&
      requiredMount(mounts, "bind", "/opt/sbm/acceptance-start.sh", false),
    tmpfs:
      sameMembers(Object.keys(host.Tmpfs ?? {}), [
        "/tmp",
        "/run/sbm-secrets",
      ]) &&
      tmpfsHas(host.Tmpfs, "/tmp", ["noexec", "nosuid", "nodev"]) &&
      tmpfsHas(host.Tmpfs, "/run/sbm-secrets", [
        "noexec",
        "nosuid",
        "nodev",
        "mode=0700",
      ]),
    internal_acceptance_network: network.Internal === true,
    provider_isolation:
      provider.Image === expectedProviderImageID &&
      provider.Config?.Image === "node:24.19.0-alpine3.23" &&
      provider.Config?.User === "node" &&
      sameMembers(provider.Config?.Env ?? [], expectedProviderEnvironment) &&
      JSON.stringify(provider.Config?.Cmd) ===
        JSON.stringify(expectedProviderCommand) &&
      providerHost.ReadonlyRootfs === true &&
      providerHost.NetworkMode === `container:${app.Id}` &&
      sameMembers(providerHost.CapDrop ?? [], ["ALL"]) &&
      (providerHost.SecurityOpt ?? []).some((entry) =>
        entry.startsWith("no-new-privileges"),
      ) &&
      providerHost.PidsLimit === 64 &&
      providerHost.NanoCpus === 500_000_000 &&
      providerHost.Memory === 256 * 1024 * 1024 &&
      sameMembers(Object.keys(providerHost.Tmpfs ?? {}), ["/tmp"]) &&
      tmpfsHas(providerHost.Tmpfs, "/tmp", ["noexec", "nosuid", "nodev"]) &&
      providerMounts.length === 2 &&
      requiredMount(
        providerMounts,
        "bind",
        "/run/secrets/sbm_provider_key",
        false,
      ) &&
      requiredMount(
        providerMounts,
        "bind",
        "/opt/sbm/synthetic-provider.mjs",
        false,
      ) &&
      Object.keys(providerHost.PortBindings ?? {}).length === 0,
  };
  return { ...gates, passed: Object.values(gates).every(Boolean) };
}

export function validatePostgreSQLRuntime(database, network, roleOutput) {
  const host = database.HostConfig ?? {};
  const mounts = database.Mounts ?? [];
  const attachedNetworks = Object.values(
    database.NetworkSettings?.Networks ?? {},
  );
  const gates = {
    healthy: database.State?.Health?.Status === "healthy",
    image_identity:
      database.Image === expectedPostgreSQLImageID &&
      database.Config?.Image === "postgres:17-alpine",
    no_host_port:
      database.NetworkSettings?.Ports?.["5432/tcp"] === null &&
      Object.keys(host.PortBindings ?? {}).length === 0,
    read_only_root: host.ReadonlyRootfs === true,
    capabilities:
      sameMembers(host.CapDrop ?? [], ["ALL"]) &&
      sameMembers(host.CapAdd ?? [], [
        "CAP_CHOWN",
        "CAP_DAC_OVERRIDE",
        "CAP_FOWNER",
        "CAP_SETGID",
        "CAP_SETUID",
      ]) &&
      (host.SecurityOpt ?? []).some((entry) =>
        entry.startsWith("no-new-privileges"),
      ),
    resources:
      host.PidsLimit === 256 &&
      host.NanoCpus === 2_000_000_000 &&
      host.Memory === 2 * 1024 * 1024 * 1024 &&
      host.ShmSize === 256 * 1024 * 1024,
    writable_surface:
      mounts.length === 2 &&
      requiredMount(mounts, "volume", "/var/lib/postgresql/data", true) &&
      requiredMount(
        mounts,
        "bind",
        "/run/secrets/sbm_postgres_admin_password",
        false,
      ) &&
      sameMembers(Object.keys(host.Tmpfs ?? {}), [
        "/tmp",
        "/var/run/postgresql",
      ]) &&
      tmpfsHas(host.Tmpfs, "/tmp", ["noexec", "nosuid", "nodev"]) &&
      tmpfsHas(host.Tmpfs, "/var/run/postgresql", [
        "noexec",
        "nosuid",
        "nodev",
        "mode=3775",
      ]),
    internal_database_network:
      network.Internal === true &&
      attachedNetworks.length === 1 &&
      attachedNetworks[0]?.NetworkID === network.Id,
    least_privilege_roles:
      roleOutput.trim() ===
      "sbm_migration:true:false:false:false:false,sbm_runtime:true:false:false:false:false\nsbm_migration\nt|f",
  };
  return { ...gates, passed: Object.values(gates).every(Boolean) };
}

async function checkAuthentication(options, password) {
  const raw = [];
  let step = "ready_request";
  try {
  const readyResponse = await fetch(`${options.server}/api/v1/ready`, {
    signal: AbortSignal.timeout(10_000),
    redirect: "error",
  });
  const readyBody = await readyResponse.text();
  raw.push(readyBody);
  const ready =
    readyResponse.status === 200 && JSON.parse(readyBody).status === "ready";
  step = "login_request";
  const loginResponse = await fetch(`${options.server}/api/v1/session/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      email: options.email,
      password: password.toString("utf8"),
    }),
    signal: AbortSignal.timeout(15_000),
    redirect: "error",
  });
  const loginBodyText = await loginResponse.text();
  raw.push(loginBodyText);
  step = "login_response";
  const loginBody = JSON.parse(loginBodyText);
  const cookies = loginResponse.headers
    .getSetCookie()
    .map((entry) => entry.split(";", 1)[0]);
  const cookie = cookies.join("; ");
  raw.push(cookie);
  const login =
    loginResponse.status === 200 &&
    cookies.length >= 2 &&
      loginBody.role === "owner";
  step = "session_request";
  const sessionResponse = await fetch(`${options.server}/api/v1/session`, {
    headers: { Cookie: cookie },
    signal: AbortSignal.timeout(10_000),
    redirect: "error",
  });
  const sessionBodyText = await sessionResponse.text();
  raw.push(sessionBodyText);
  step = "session_response";
  const sessionBody = JSON.parse(sessionBodyText);
  const session =
    sessionResponse.status === 200 &&
    sessionBody.role === "owner" &&
    sessionBody.user?.email === options.email &&
    Array.isArray(sessionBody.capabilities) &&
      sessionBody.capabilities.includes("members.manage");
  step = "logout_request";
  const logoutResponse = await fetch(`${options.server}/api/v1/session`, {
    method: "DELETE",
    headers: {
      Cookie: cookie,
      "X-CSRF-Token": loginBody.csrf_token,
    },
    signal: AbortSignal.timeout(10_000),
    redirect: "error",
  });
  const invalidatedResponse = await fetch(`${options.server}/api/v1/session`, {
    headers: { Cookie: cookie },
    signal: AbortSignal.timeout(10_000),
    redirect: "error",
  });
  return {
    ready,
    login,
    session,
    logout: logoutResponse.status === 204 && invalidatedResponse.status === 401,
    raw,
  };
  } catch (error) {
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError(`runtime_authentication_${step}_failed`);
  }
}

async function composeServiceID(options, environment, service) {
  const result = await runCaptured(
    "docker",
    [
      "compose",
      "--project-name",
      options.projectName,
      "-f",
      "infra/compose/compose.yaml",
      "-f",
      "infra/compose/compose.acceptance.yaml",
      "ps",
      "--quiet",
      service,
    ],
    { cwd: repositoryRoot, env: environment },
  );
  if (result.code !== 0) throw new SafeToolError("runtime_inspection_failed");
  const id = result.stdout.toString("utf8").trim();
  clearBuffers([result.stdout, result.stderr]);
  if (!/^[0-9a-f]{64}$/.test(id)) {
    throw new SafeToolError("runtime_inspection_failed");
  }
  return id;
}

async function dockerJSON(argumentsList, environment, outputs) {
  const result = await dockerCommand(argumentsList, environment, outputs);
  try {
    return JSON.parse(result.toString("utf8"));
  } catch {
    throw new SafeToolError("runtime_inspection_failed");
  }
}

async function dockerCommand(
  argumentsList,
  environment,
  outputs,
  allowFailure = false,
) {
  const result = await runCaptured("docker", argumentsList, {
    cwd: repositoryRoot,
    env: environment,
  });
  outputs.push(result.stdout, result.stderr);
  if (!allowFailure && result.code !== 0) {
    throw new SafeToolError("runtime_inspection_failed");
  }
  return allowFailure ? result : result.stdout;
}

function single(value) {
  if (!Array.isArray(value) || value.length !== 1) {
    throw new SafeToolError("runtime_inspection_failed");
  }
  return value[0];
}

function requiredMount(mounts, type, destination, readWrite) {
  return mounts.some(
    (mount) =>
      mount.Type === type &&
      mount.Destination === destination &&
      mount.RW === readWrite,
  );
}

function tmpfsHas(tmpfs, target, options) {
  const value = tmpfs?.[target];
  return (
    typeof value === "string" &&
    options.every((option) => value.includes(option))
  );
}

function sameMembers(actual, expected) {
  return (
    actual.length === expected.length &&
    [...actual].sort().join("\0") === [...expected].sort().join("\0")
  );
}

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`release-runtime: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
