#!/usr/bin/env node

import { createHash } from "node:crypto";
import { execFile as execFileCallback } from "node:child_process";
import { constants } from "node:fs";
import {
  chmod,
  cp,
  lstat,
  mkdir,
  mkdtemp,
  open,
  readdir,
  readFile,
  realpath,
  rm,
} from "node:fs/promises";
import {
  basename,
  dirname,
  isAbsolute,
  join,
  relative,
  resolve,
} from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";

import {
  SafeToolError,
  parseStrictPairs,
  requireGitSHA,
  requireSHA256,
  reserveProtectedDirectory,
  safeErrorCode,
} from "./lib/protected-output.mjs";
import { releaseInputPaths } from "./check-release-image.mjs";

const execFile = promisify(execFileCallback);
const currentFile = fileURLToPath(import.meta.url);
const defaultRepositoryRoot = resolve(dirname(currentFile), "..");
const expectedNodeVersion = "v24.19.0";
const expectedGoVersion = "go1.26.7";
const goImage = "golang:1.26.7-alpine3.23";
const expectedGoImageID =
  "sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57";
const glibcSourceImage = "golang:1.26.7";
const expectedGlibcSourceImageID =
  "sha256:dc2521c2a906db43073b8b4d99f491b6341cf15610b6ebbab187c45153f9959e";
const expectedPopplerVersion = "26.05.0";
const expectedPopplerSourceSHA256 =
  "6fef27ff04f37db43054c86bcdff6128c9fb1f6af4ef3c8b369a7e9abd68d0bb";

async function main() {
  const options = parseArguments(process.argv.slice(2));
  await validateInputs(options);
  const output = await reserveProtectedDirectory(options.output, [
    options.repositoryRoot,
    options.npmCache,
    options.goModuleCache,
    options.popplerBundle,
  ]);
  const workspace = await mkdtemp("/tmp/sbm-local-release-build.");
  await chmod(workspace, 0o700);
  let succeeded = false;
  try {
    await verifyRepositoryIdentity(options);
    await buildWeb(options, workspace, output);
    await buildGo(options, output);
    await copyPoppler(options, output);
    await writeIdentity(output, options);
    const artifactFiles = await writeChecksums(output);
    await validateArtifactTree(output, artifactFiles);
    succeeded = true;
    process.stdout.write(
      `${JSON.stringify({ report_kind: "m4-local-release-artifacts", artifact_file_count: artifactFiles.length, passed: true })}\n`,
    );
  } finally {
    await rm(workspace, { recursive: true, force: true }).catch(() => {});
    if (!succeeded) {
      await rm(output, { recursive: true, force: true }).catch(() => {});
    }
  }
}

export function parseArguments(argumentsList) {
  const values = parseStrictPairs(
    argumentsList,
    [
      "output-directory",
      "expected-head",
      "expected-release-input-sha256",
      "npm-cache",
      "go-module-cache",
      "poppler-bundle",
    ],
    ["repository-root"],
  );
  return {
    repositoryRoot: resolve(
      values.get("repository-root") ?? defaultRepositoryRoot,
    ),
    output: resolve(values.get("output-directory")),
    expectedHead: requireGitSHA(values.get("expected-head")),
    expectedReleaseInput: requireSHA256(
      values.get("expected-release-input-sha256"),
    ),
    npmCache: resolve(values.get("npm-cache")),
    goModuleCache: resolve(values.get("go-module-cache")),
    popplerBundle: resolve(values.get("poppler-bundle")),
  };
}

export function identityText({ expectedHead, expectedReleaseInput }) {
  return [
    `baseline_head=${expectedHead}`,
    `release_input_sha256=${expectedReleaseInput}`,
    `node_version=${expectedNodeVersion}`,
    `go_version=${expectedGoVersion}`,
    `go_image_id=${expectedGoImageID}`,
    `glibc_source_image_id=${expectedGlibcSourceImageID}`,
    `poppler_version=${expectedPopplerVersion}`,
    `poppler_source_sha256=${expectedPopplerSourceSHA256}`,
    "",
  ].join("\n");
}

export function checksumText(entries) {
  return [...entries]
    .sort(([left], [right]) => left.localeCompare(right, "en"))
    .map(([path, digest]) => `${digest}  ${path}\n`)
    .join("");
}

async function validateInputs(options) {
  const repositoryRoot = await requireRealDirectory(options.repositoryRoot);
  if (repositoryRoot !== options.repositoryRoot) {
    throw new SafeToolError("repository_invalid");
  }
  for (const cache of [
    options.npmCache,
    options.goModuleCache,
    options.popplerBundle,
  ]) {
    if (!isAbsolute(cache) || (await requireRealDirectory(cache)) !== cache) {
      throw new SafeToolError("dependency_cache_invalid");
    }
    const nested = relative(repositoryRoot, cache);
    if (nested === "" || (!nested.startsWith("..") && !isAbsolute(nested))) {
      throw new SafeToolError("dependency_cache_invalid");
    }
  }
  const npmContent = join(options.npmCache, "_cacache");
  if ((await requireRealDirectory(npmContent)) !== npmContent) {
    throw new SafeToolError("dependency_cache_invalid");
  }
  if (basename(options.goModuleCache) !== "mod") {
    throw new SafeToolError("dependency_cache_invalid");
  }
  const popplerManifest = await readPopplerManifest(options.popplerBundle);
  if (
    popplerManifest.name !== "poppler" ||
    popplerManifest.version !== expectedPopplerVersion ||
    popplerManifest.targetPlatform !== "linux" ||
    popplerManifest.targetArch !== "x64" ||
    popplerManifest.sourceSha256 !== expectedPopplerSourceSHA256
  ) {
    throw new SafeToolError("dependency_cache_invalid");
  }
  await requireRealDirectory(join(options.popplerBundle, "poppler"));
}

async function verifyRepositoryIdentity(options) {
  const [head, nodeVersion] = await Promise.all([
    run("git", ["rev-parse", "HEAD"], options.repositoryRoot),
    run("node", ["--version"], options.repositoryRoot),
  ]);
  if (
    head.trim() !== options.expectedHead ||
    nodeVersion.trim() !== expectedNodeVersion
  ) {
    throw new SafeToolError("release_identity_invalid");
  }
  const digestOutput = await run(
    "node",
    [
      "tools/check-release-image.mjs",
      "digest",
      "--repository-root",
      options.repositoryRoot,
    ],
    options.repositoryRoot,
  );
  let digest;
  try {
    digest = JSON.parse(digestOutput).release_input_sha256;
  } catch {
    throw new SafeToolError("release_identity_invalid");
  }
  if (digest !== options.expectedReleaseInput) {
    throw new SafeToolError("release_identity_invalid");
  }
}

async function buildWeb(options, workspace, output) {
  const webRoot = join(workspace, "apps", "web");
  const buildInputs = (await releaseInputPaths(options.repositoryRoot)).filter(
    (path) => path.startsWith("apps/web/") || path.startsWith("contracts/"),
  );
  if (
    !buildInputs.includes("apps/web/package.json") ||
    !buildInputs.includes("apps/web/package-lock.json")
  ) {
    throw new SafeToolError("release_input_incomplete");
  }
  for (const path of buildInputs) {
    const destination = join(workspace, path);
    await mkdir(dirname(destination), { recursive: true, mode: 0o700 });
    await cp(join(options.repositoryRoot, path), destination, {
      errorOnExist: true,
      force: false,
      verbatimSymlinks: true,
    });
  }
  const npmLogs = join(workspace, "npm-logs");
  const npmHome = join(workspace, "npm-home");
  const npmUserConfig = join(workspace, "npmrc-user");
  const npmGlobalConfig = join(workspace, "npmrc-global");
  await mkdir(npmLogs, { mode: 0o700 });
  await mkdir(npmHome, { mode: 0o700 });
  await Promise.all([
    writeExclusive(npmUserConfig, ""),
    writeExclusive(npmGlobalConfig, ""),
  ]);
  const environment = selectedEnvironment({
    HOME: npmHome,
    NODE_OPTIONS: "--max-old-space-size=1536",
    npm_config_cache: options.npmCache,
    npm_config_offline: "true",
    npm_config_audit: "false",
    npm_config_fund: "false",
    npm_config_logs_dir: npmLogs,
    npm_config_userconfig: npmUserConfig,
    npm_config_globalconfig: npmGlobalConfig,
    npm_config_update_notifier: "false",
  });
  await run(
    "npm",
    ["ci", "--offline", "--no-audit", "--no-fund"],
    webRoot,
    environment,
    "web_dependencies_build_failed",
  );
  await run(
    "npm",
    ["run", "build"],
    webRoot,
    environment,
    "web_artifact_build_failed",
  );
  const dist = join(webRoot, "dist");
  await requireRegularTree(dist);
  await cp(dist, join(output, "web"), {
    recursive: true,
    verbatimSymlinks: true,
  });
}

async function buildGo(options, output) {
  const [inspected, glibcInspected] = await Promise.all(
    [goImage, glibcSourceImage].map(async (image) =>
      JSON.parse(
        await run(
          "docker",
          ["image", "inspect", image],
          options.repositoryRoot,
          undefined,
          "go_image_inspection_failed",
        ),
      ),
    ),
  );
  if (
    !Array.isArray(inspected) ||
    inspected.length !== 1 ||
    inspected[0]?.Id !== expectedGoImageID ||
    !Array.isArray(glibcInspected) ||
    glibcInspected.length !== 1 ||
    glibcInspected[0]?.Id !== expectedGlibcSourceImageID
  ) {
    throw new SafeToolError("release_identity_invalid");
  }
  const apiRoot = join(options.repositoryRoot, "apps", "api");
  const user = `${process.getuid()}:${process.getgid()}`;
  const argumentsList = [
    "run",
    "--rm",
    "--network",
    "none",
    "--read-only",
    "--tmpfs",
    "/tmp:rw,noexec,nosuid,nodev,size=1073741824",
    "--cap-drop",
    "ALL",
    "--security-opt",
    "no-new-privileges:true",
    "--pids-limit",
    "256",
    "--cpus",
    "2",
    "--memory",
    "4g",
    "--user",
    user,
    "--mount",
    `type=bind,src=${apiRoot},dst=/workspace/apps/api,readonly`,
    "--mount",
    `type=bind,src=${options.goModuleCache},dst=/go/pkg/mod,readonly`,
    "--mount",
    `type=bind,src=${output},dst=/out`,
    "--workdir",
    "/workspace/apps/api",
    "--env",
    "HOME=/tmp",
    "--env",
    "GOCACHE=/tmp/go-build",
    "--env",
    "GOPROXY=off",
    "--env",
    "GOSUMDB=off",
    "--env",
    "GOMAXPROCS=2",
    "--env",
    "GOFLAGS=-p=1",
    goImage,
    "sh",
    "-c",
    'test "$(go env GOVERSION)" = "go1.26.7" && go mod verify && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/bootstrap-owner ./cmd/bootstrap-owner && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/recover-account ./cmd/recover-account && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/backup ./cmd/backup && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/provision-postgresql ./cmd/provision-postgresql && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/recovery-exercise ./cmd/recovery-exercise && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/run-as-sbm ./cmd/run-as-sbm',
  ];
  await run(
    "docker",
    argumentsList,
    options.repositoryRoot,
    undefined,
    "go_artifact_build_failed",
  );
}

async function copyPoppler(options, output) {
  const source = join(options.popplerBundle, "poppler");
  const destination = join(output, "poppler");
  await cp(source, destination, { recursive: true, verbatimSymlinks: true });
  const files = await listRegularFiles(destination);
  for (const required of [
    "bin/pdfinfo",
    "bin/pdftoppm",
    "lib/libpoppler.so.160",
    "etc/fonts/fonts.conf",
  ]) {
    if (!files.includes(required)) {
      throw new SafeToolError("artifact_tree_invalid");
    }
  }
}

async function writeIdentity(output, options) {
  await writeExclusive(join(output, "identity.env"), identityText(options));
}

async function writeChecksums(output) {
  const files = await listRegularFiles(output);
  const entries = [];
  for (const path of files) {
    if (!isSafeArtifactPath(path)) {
      throw new SafeToolError("artifact_tree_invalid");
    }
    const content = await readFile(join(output, path));
    entries.push([path, createHash("sha256").update(content).digest("hex")]);
    content.fill(0);
  }
  await writeExclusive(join(output, "SHA256SUMS"), checksumText(entries));
  return files;
}

export function isSafeArtifactPath(path) {
  return path
    .split("/")
    .every(
      (segment) =>
        segment !== "." &&
        segment !== ".." &&
        /^[A-Za-z0-9_][A-Za-z0-9._+-]*$/.test(segment),
    );
}

async function validateArtifactTree(output, expectedFiles) {
  const actual = await listRegularFiles(output);
  const expected = [...expectedFiles, "SHA256SUMS"].sort((left, right) =>
    left.localeCompare(right, "en"),
  );
  if (actual.join("\0") !== expected.join("\0")) {
    throw new SafeToolError("artifact_tree_invalid");
  }
  const executables = new Set([
    "server",
    "bootstrap-owner",
    "recover-account",
    "backup",
    "migrate",
    "provision-postgresql",
    "recovery-exercise",
    "run-as-sbm",
    "poppler/bin/pdfinfo",
    "poppler/bin/pdftoppm",
  ]);
  for (const path of actual) {
    await chmod(join(output, path), executables.has(path) ? 0o755 : 0o644);
  }
  for (const name of [
    "server",
    "bootstrap-owner",
    "recover-account",
    "backup",
    "migrate",
    "provision-postgresql",
    "recovery-exercise",
    "run-as-sbm",
    "identity.env",
  ]) {
    if (!actual.includes(name))
      throw new SafeToolError("artifact_tree_invalid");
  }
  if (!actual.includes("web/index.html")) {
    throw new SafeToolError("artifact_tree_invalid");
  }
}

async function readPopplerManifest(bundle) {
  try {
    return JSON.parse(await readFile(join(bundle, "manifest.json"), "utf8"));
  } catch {
    throw new SafeToolError("dependency_cache_invalid");
  }
}

async function listRegularFiles(root) {
  const result = [];
  async function visit(directory, prefix) {
    const entries = await readdir(directory, { withFileTypes: true });
    entries.sort((left, right) => left.name.localeCompare(right.name, "en"));
    for (const entry of entries) {
      const absolute = join(directory, entry.name);
      const path = prefix ? `${prefix}/${entry.name}` : entry.name;
      const information = await lstat(absolute);
      if (information.isSymbolicLink()) {
        throw new SafeToolError("artifact_tree_invalid");
      }
      if (information.isDirectory()) {
        await visit(absolute, path);
      } else if (information.isFile() && information.nlink === 1) {
        result.push(path);
      } else {
        throw new SafeToolError("artifact_tree_invalid");
      }
    }
  }
  await visit(root, "");
  return result.sort((left, right) => left.localeCompare(right, "en"));
}

async function requireRegularTree(root) {
  if ((await requireRealDirectory(root)) !== root) {
    throw new SafeToolError("artifact_tree_invalid");
  }
  const files = await listRegularFiles(root);
  if (files.length === 0 || !files.includes("index.html")) {
    throw new SafeToolError("artifact_tree_invalid");
  }
}

async function requireRealDirectory(path) {
  try {
    const [information, resolved] = await Promise.all([
      lstat(path),
      realpath(path),
    ]);
    if (!information.isDirectory() || information.isSymbolicLink()) {
      throw new Error("not a directory");
    }
    return resolved;
  } catch {
    throw new SafeToolError("dependency_cache_invalid");
  }
}

async function writeExclusive(path, text) {
  let handle;
  try {
    handle = await open(
      path,
      constants.O_WRONLY |
        constants.O_CREAT |
        constants.O_EXCL |
        constants.O_NOFOLLOW,
      0o600,
    );
    await handle.writeFile(text, "utf8");
    await handle.sync();
  } catch {
    throw new SafeToolError("artifact_write_failed");
  } finally {
    await handle?.close().catch(() => {});
  }
}

function selectedEnvironment(extra = {}) {
  const environment = { ...extra };
  for (const name of [
    "PATH",
    "HOME",
    "DOCKER_HOST",
    "DOCKER_CONTEXT",
    "DOCKER_CONFIG",
    "XDG_CONFIG_HOME",
  ]) {
    if (process.env[name] && environment[name] === undefined) {
      environment[name] = process.env[name];
    }
  }
  return environment;
}

async function run(
  command,
  argumentsList,
  cwd,
  env = selectedEnvironment(),
  failureCode = "artifact_build_failed",
) {
  try {
    return (
      await execFile(command, argumentsList, {
        cwd,
        env,
        encoding: "utf8",
        maxBuffer: 16 * 1024 * 1024,
        timeout: 180_000,
      })
    ).stdout;
  } catch {
    throw new SafeToolError(failureCode);
  }
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`release-artifacts: ${safeErrorCode(error)}\n`);
    process.exitCode = 1;
  });
}
