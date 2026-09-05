import assert from "node:assert/strict";
import test from "node:test";

import {
  runtimeAssetReadCheck,
  validateCompose,
  validateImage,
} from "./check-release-image.mjs";

test("运行用户检查遍历并读取所有公开资产，不只确认入口存在", () => {
  assert.match(runtimeAssetReadCheck, /find .* -type f/);
  assert.match(runtimeAssetReadCheck, /test -r/);
  assert.match(runtimeAssetReadCheck, /pdfinfo -v/);
  assert.match(runtimeAssetReadCheck, /pdftoppm -v/);
});

const head = "a".repeat(40);
const releaseInput = "b".repeat(64);
const exerciseID = "123e4567-e89b-42d3-a456-426614174000";
const providerImageID =
  "sha256:244cc2b53f46f9e876304391d17682b0ddae9ac33491f4857e25e35a36ba7995";

function composeFixture() {
  const app = {
    image: "smart-bill-manager:local",
    pull_policy: "never",
    build: {
      context: "/repo",
      dockerfile: "infra/docker/app.Dockerfile",
      network: "none",
      additional_contexts: {
        release_artifacts: "/tmp/release-artifacts",
      },
      args: {
        GLIBC_SOURCE_IMAGE: "smart-bill-manager:go-glibc-source-local",
        VCS_REF: head,
        RELEASE_INPUT_SHA256: releaseInput,
      },
    },
    ports: [{ host_ip: "127.0.0.1", target: 8080, published: "8080" }],
    read_only: true,
    tmpfs: [
      "/tmp:rw,noexec,nosuid,nodev,size=268435456",
      "/run/sbm-secrets:rw,noexec,nosuid,nodev,size=65536,mode=0700",
    ],
    volumes: [{ target: "/var/lib/sbm/objects" }],
    cap_drop: ["ALL"],
    cap_add: ["CHOWN", "DAC_OVERRIDE", "SETGID", "SETUID"],
    security_opt: ["no-new-privileges:true"],
    pids_limit: 256,
    cpus: 2,
    mem_limit: "3758096384",
    stop_grace_period: "20s",
    init: true,
    restart: "unless-stopped",
    environment: {
      SBM_POSTGRES_HOST: "database",
      SBM_POSTGRES_PORT: "5432",
      SBM_POSTGRES_DATABASE: "smart_bill_manager",
      SBM_POSTGRES_USER: "sbm_runtime",
      SBM_POSTGRES_PASSWORD_FILE: "/run/sbm-secrets/postgres-runtime-password",
      SBM_POSTGRES_SSL_MODE: "disable",
      SBM_AI_CONCURRENCY: "2",
      SBM_COOKIE_SECURE: "false",
      SBM_DEPLOYMENT_MODE: "local",
      SBM_SESSION_TTL: "168h",
    },
    healthcheck: {
      test: [
        "CMD",
        "wget",
        "-q",
        "-O",
        "/dev/null",
        "http://127.0.0.1:8080/api/v1/ready",
      ],
      interval: "10s",
      timeout: "3s",
      start_period: "10s",
      retries: 6,
    },
    secrets: [
      { source: "sbm_master_key", target: "/run/secrets/sbm_master_key" },
      {
        source: "sbm_postgres_runtime_password",
        target: "/run/secrets/sbm_postgres_runtime_password",
      },
    ],
    depends_on: { migrate: { condition: "service_completed_successfully" } },
  };
  const database = {
    image: "postgres:17-alpine",
    pull_policy: "never",
    read_only: true,
    tmpfs: [
      "/tmp:rw,noexec,nosuid,nodev,size=16777216",
      "/var/run/postgresql:rw,noexec,nosuid,nodev,size=16777216,mode=3775",
    ],
    mem_limit: "2147483648",
    pids_limit: 256,
    networks: { database: null },
  };
  const provision = {
    depends_on: { database: { condition: "service_healthy" } },
  };
  const migrate = {
    depends_on: { provision: { condition: "service_completed_successfully" } },
  };
  const base = {
    name: "smart-bill-manager",
    services: { app, database, provision, migrate },
    volumes: { sbm_postgres_data: {}, sbm_objects: {} },
    networks: { database: { internal: true } },
    secrets: {
      sbm_master_key: {},
      sbm_postgres_admin_password: {},
      sbm_postgres_migration_password: {},
      sbm_postgres_runtime_password: {},
    },
  };
  const acceptance = structuredClone(base);
  acceptance.secrets.sbm_owner_password = {};
  acceptance.secrets.sbm_provider_key = {};
  acceptance.networks.default = { internal: true };
  acceptance.services.app.secrets.push({
    source: "sbm_owner_password",
    target: "sbm_owner_password",
  });
  acceptance.services.app.command = ["/opt/sbm/acceptance-start.sh"];
  acceptance.services.app.volumes.push({
    type: "bind",
    source: "/repo/tools/acceptance-start.sh",
    target: "/opt/sbm/acceptance-start.sh",
    read_only: true,
  });
  acceptance.services.provider = {
    image: "node:24.19.0-alpine3.23",
    pull_policy: "never",
    network_mode: "service:app",
    read_only: true,
    user: "node",
    cap_drop: ["ALL"],
    security_opt: ["no-new-privileges:true"],
    depends_on: { app: { condition: "service_started", required: true } },
    pids_limit: 64,
    cpus: 0.5,
    mem_limit: "268435456",
    restart: "no",
    tmpfs: ["/tmp:rw,noexec,nosuid,nodev,size=16777216"],
    command: [
      "node",
      "/opt/sbm/synthetic-provider.mjs",
      "--listen",
      "127.0.0.1:19086",
      "--api-key-file",
      "/run/secrets/sbm_provider_key",
      "--model",
      "synthetic-local-release",
      "--exercise-id",
      exerciseID,
    ],
    secrets: [{ source: "sbm_provider_key", target: "sbm_provider_key" }],
    volumes: [
      {
        type: "bind",
        source: "/repo/tools/synthetic-provider.mjs",
        target: "/opt/sbm/synthetic-provider.mjs",
        read_only: true,
      },
    ],
  };
  return { base, acceptance };
}

test("Compose release policy accepts only the frozen shape", () => {
  const { base, acceptance } = composeFixture();
  const options = {
    expectedHead: head,
    expectedReleaseInput: releaseInput,
    exerciseID,
    releaseArtifactsSource: "/tmp/release-artifacts",
    repositoryRoot: "/repo",
  };
  assert.equal(
    validateCompose(base, acceptance, options, providerImageID).passed,
    true,
  );
  base.services.app.ports[0].host_ip = "0.0.0.0";
  assert.deepEqual(
    validateCompose(base, acceptance, options, providerImageID).failed_gates,
    ["loopback_port"],
  );
});

test("Compose release policy rejects remote dependency contexts", () => {
  const { base, acceptance } = composeFixture();
  const options = {
    expectedHead: head,
    expectedReleaseInput: releaseInput,
    exerciseID,
    releaseArtifactsSource: "/tmp/release-artifacts",
    repositoryRoot: "/repo",
  };
  base.services.app.build.additional_contexts.release_artifacts =
    "https://example.invalid/modules";
  assert.deepEqual(
    validateCompose(base, acceptance, options, providerImageID).failed_gates,
    ["build_identity"],
  );
});

test("image release policy requires runtime assets and excludes toolchains", () => {
  const inspected = {
    Id: `sha256:${"c".repeat(64)}`,
    Config: {
      Labels: {
        "org.opencontainers.image.title": "Smart Bill Manager",
        "org.opencontainers.image.revision": head,
        "com.smart-bill-manager.release-input-sha256": releaseInput,
        "com.smart-bill-manager.node-build-version": "24.19.0",
        "com.smart-bill-manager.go-build-version": "go1.26.7",
        "com.smart-bill-manager.glibc-source-image-id":
          "sha256:dc2521c2a906db43073b8b4d99f491b6341cf15610b6ebbab187c45153f9959e",
        "com.smart-bill-manager.runtime-contract":
          "alpine-3.23-glibc-2.41-poppler-26.05-tzdata/1",
      },
      Entrypoint: ["/usr/local/bin/sbm-entrypoint"],
      Cmd: ["/app/server"],
      WorkingDir: "/app",
      User: "root",
      ExposedPorts: { "8080/tcp": {} },
      Volumes: {
        "/var/lib/sbm/objects": {},
      },
      Healthcheck: {
        Test: [
          "CMD-SHELL",
          "wget -q -O /dev/null http://127.0.0.1:8080/api/v1/ready || exit 1",
        ],
      },
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
      ],
    },
  };
  const inventory = `${[
    "/app/server",
    "/app/bootstrap-owner",
    "/app/recover-account",
    "/app/backup",
    "/app/migrate",
    "/app/provision-postgresql",
    "/app/run-as-sbm",
    "/app/web/index.html",
    "/app/migrations/0001_initial.sql",
    "/app/migrations/0002_manual_trip_workspaces.sql",
    "/app/migrations/0003_explicit_manual_review.sql",
    "/app/migrations/0004_confirmed_fact_corrections.sql",
    "/app/migrations/0005_fact_management_indexes.sql",
    "/app/migrations/0006_invoice_supporting_materials.sql",
    "/app/migrations/0007_member_account_lifecycle.sql",
    "/app/migrations/0008_allocation_search_and_bad_debt.sql",
    "/app/contracts/bill-visible-text.schema.json",
    "/usr/local/bin/sbm-entrypoint",
    "/usr/local/bin/pg_dump",
    "/usr/local/bin/pg_restore",
    "/opt/sbm-poppler/bin/pdfinfo",
    "/opt/sbm-poppler/bin/pdftoppm",
    "/opt/sbm-poppler/lib/libpoppler.so.160",
    "/usr/share/zoneinfo/Asia/Shanghai",
    "/usr/share/zoneinfo/zone.tab",
  ].join(
    "\n",
  )}\n__SBM_COMMANDS__\ngo=absent\nnode=absent\nnpm=absent\napk=absent\napt=absent\napt-get=absent\ndpkg=absent\npdfinfo=pdfinfo version 26.05.0\npdftoppm=pdftoppm version 26.05.0\npg_dump=pg_dump (PostgreSQL) 17.6\npg_restore=pg_restore (PostgreSQL) 17.6\n`;
  assert.equal(
    validateImage(inspected, inventory, head, releaseInput).passed,
    true,
  );
  const missingAllocationMigration = inventory.replace(
    "/app/migrations/0008_allocation_search_and_bad_debt.sql\n",
    "",
  );
  assert.equal(
    validateImage(inspected, missingAllocationMigration, head, releaseInput)
      .passed,
    false,
  );
  const contaminated = inventory.replace(
    "__SBM_COMMANDS__",
    "/app/seed-performance\n__SBM_COMMANDS__",
  );
  assert.equal(
    validateImage(inspected, contaminated, head, releaseInput).passed,
    false,
  );
});
