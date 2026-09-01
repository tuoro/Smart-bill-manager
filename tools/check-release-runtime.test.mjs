import assert from "node:assert/strict";
import test from "node:test";

import {
  parseArguments,
  validatePostgreSQLRuntime,
  validateRuntimeSecurity,
} from "./check-release-runtime.mjs";

test("PostgreSQL runtime requires an internal read-only least-privilege shape", () => {
  const networkID = "e".repeat(64);
  const database = {
    Image:
      "sha256:18cfe3ef5e6815560c98237d6216d1e5119702fb0f3894c8785dd58b8bbe5d73",
    Config: { Image: "postgres:17-alpine" },
    State: { Health: { Status: "healthy" } },
    NetworkSettings: {
      Ports: { "5432/tcp": null },
      Networks: { database: { NetworkID: networkID } },
    },
    HostConfig: {
      ReadonlyRootfs: true,
      PortBindings: {},
      CapDrop: ["ALL"],
      CapAdd: [
        "CAP_CHOWN",
        "CAP_DAC_OVERRIDE",
        "CAP_FOWNER",
        "CAP_SETGID",
        "CAP_SETUID",
      ],
      SecurityOpt: ["no-new-privileges:true"],
      PidsLimit: 256,
      NanoCpus: 2_000_000_000,
      Memory: 2 * 1024 * 1024 * 1024,
      ShmSize: 256 * 1024 * 1024,
      Tmpfs: {
        "/tmp": "rw,noexec,nosuid,nodev,size=16777216",
        "/var/run/postgresql":
          "rw,noexec,nosuid,nodev,size=16777216,mode=3775",
      },
    },
    Mounts: [
      {
        Type: "volume",
        Destination: "/var/lib/postgresql/data",
        RW: true,
      },
      {
        Type: "bind",
        Destination: "/run/secrets/sbm_postgres_admin_password",
        RW: false,
      },
    ],
  };
  const roles =
    "sbm_migration:true:false:false:false:false,sbm_runtime:true:false:false:false:false\nsbm_migration\nt|f\n";
  assert.equal(
    validatePostgreSQLRuntime(database, { Id: networkID, Internal: true }, roles)
      .passed,
    true,
  );
  database.HostConfig.ReadonlyRootfs = false;
  assert.equal(
    validatePostgreSQLRuntime(database, { Id: networkID, Internal: true }, roles)
      .passed,
    false,
  );
});

test("runtime security accepts the frozen container shape", () => {
  const app = {
    Id: "d".repeat(64),
    Image: `sha256:${"c".repeat(64)}`,
    Config: {
      Image: "smart-bill-manager:local",
      Cmd: ["/opt/sbm/acceptance-start.sh"],
      Env: [
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
      ],
    },
    State: { Health: { Status: "healthy" } },
    NetworkSettings: { Ports: { "8080/tcp": null } },
    HostConfig: {
      ReadonlyRootfs: true,
      CapDrop: ["ALL"],
      CapAdd: ["CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_SETGID", "CAP_SETUID"],
      SecurityOpt: ["no-new-privileges"],
      PidsLimit: 256,
      NanoCpus: 2_000_000_000,
      Memory: 3584 * 1024 * 1024,
      PortBindings: {
        "8080/tcp": [{ HostIp: "127.0.0.1", HostPort: "8080" }],
      },
      Tmpfs: {
        "/tmp": "rw,noexec,nosuid,nodev,size=268435456",
        "/run/sbm-secrets": "rw,noexec,nosuid,nodev,size=65536,mode=0700",
      },
    },
    Mounts: [
      { Type: "volume", Destination: "/var/lib/sbm/objects", RW: true },
      { Type: "bind", Destination: "/run/secrets/sbm_master_key", RW: false },
      {
        Type: "bind",
        Destination: "/run/secrets/sbm_postgres_runtime_password",
        RW: false,
      },
      {
        Type: "bind",
        Destination: "/run/secrets/sbm_owner_password",
        RW: false,
      },
      {
        Type: "bind",
        Destination: "/opt/sbm/acceptance-start.sh",
        RW: false,
      },
    ],
  };
  const provider = {
    Image:
      "sha256:244cc2b53f46f9e876304391d17682b0ddae9ac33491f4857e25e35a36ba7995",
    Config: {
      Image: "node:24.19.0-alpine3.23",
      User: "node",
      Env: [
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
        "NODE_VERSION=24.19.0",
        "YARN_VERSION=1.22.22",
      ],
      Cmd: [
        "node",
        "/opt/sbm/synthetic-provider.mjs",
        "--listen",
        "127.0.0.1:19086",
        "--api-key-file",
        "/run/secrets/sbm_provider_key",
        "--model",
        "synthetic-local-release",
        "--exercise-id",
        "123e4567-e89b-42d3-a456-426614174000",
      ],
    },
    State: { Running: true },
    HostConfig: {
      ReadonlyRootfs: true,
      NetworkMode: `container:${app.Id}`,
      CapDrop: ["ALL"],
      SecurityOpt: ["no-new-privileges"],
      PidsLimit: 64,
      NanoCpus: 500_000_000,
      Memory: 256 * 1024 * 1024,
      Tmpfs: { "/tmp": "rw,noexec,nosuid,nodev,size=16777216" },
      PortBindings: {},
    },
    Mounts: [
      {
        Type: "bind",
        Destination: "/run/secrets/sbm_provider_key",
        RW: false,
      },
      {
        Type: "bind",
        Destination: "/opt/sbm/synthetic-provider.mjs",
        RW: false,
      },
    ],
  };
  const metadata =
    "Uid:\t10001\t10001\t10001\t10001\nGid:\t10001\t10001\t10001\t10001\n/app/server";
  const result = validateRuntimeSecurity(
    app,
    provider,
    { Internal: true },
    metadata,
    0,
    {
      image: "smart-bill-manager:local",
      imageID: app.Image,
      exerciseID: "123e4567-e89b-42d3-a456-426614174000",
      server: "http://127.0.0.1:8080",
    },
  );
  assert.equal(result.passed, true);
  app.HostConfig.ReadonlyRootfs = false;
  assert.equal(
    validateRuntimeSecurity(app, provider, { Internal: true }, metadata, 0, {
      image: "smart-bill-manager:local",
      imageID: app.Image,
      exerciseID: "123e4567-e89b-42d3-a456-426614174000",
      server: "http://127.0.0.1:8080",
    }).passed,
    false,
  );
});

test("runtime checker rejects non-loopback and unscoped projects", () => {
  const argumentsList = [
    "--project-name",
    "sbm-m4-123e4567-core",
    "--server",
    "http://127.0.0.1:8080",
    "--email",
    "owner@example.test",
    "--output",
    "/tmp/run/runtime.json",
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
    "--image",
    "smart-bill-manager:local",
    "--image-id",
    `sha256:${"c".repeat(64)}`,
  ];
  assert.equal(parseArguments(argumentsList).server, "http://127.0.0.1:8080");
  argumentsList[3] = "http://0.0.0.0:8080";
  assert.throws(() => parseArguments(argumentsList));
});
