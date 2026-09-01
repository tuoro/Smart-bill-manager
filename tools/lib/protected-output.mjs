import { constants } from "node:fs";
import { lstat, mkdir, open, realpath, stat } from "node:fs/promises";
import { dirname, isAbsolute, relative, resolve } from "node:path";

const temporaryRoot = "/tmp";

export class SafeToolError extends Error {
  constructor(code) {
    super(code);
    this.code = code;
  }
}

export function parseStrictPairs(argumentsList, required, optional = []) {
  if (argumentsList.length % 2 !== 0)
    throw new SafeToolError("invalid_arguments");
  const allowed = new Set([...required, ...optional]);
  const values = new Map();
  for (let index = 0; index < argumentsList.length; index += 2) {
    const key = argumentsList[index];
    const value = argumentsList[index + 1];
    if (!key?.startsWith("--") || !value) {
      throw new SafeToolError("invalid_arguments");
    }
    const name = key.slice(2);
    if (!allowed.has(name) || values.has(name)) {
      throw new SafeToolError("invalid_arguments");
    }
    values.set(name, value);
  }
  for (const name of required) {
    if (!values.has(name)) throw new SafeToolError("invalid_arguments");
  }
  return values;
}

export function requireLoopbackURL(value, { allowPath = true } = {}) {
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw new SafeToolError("loopback_url_required");
  }
  if (
    parsed.protocol !== "http:" ||
    (parsed.hostname !== "127.0.0.1" && parsed.hostname !== "[::1]") ||
    parsed.username ||
    parsed.password ||
    parsed.hash ||
    (!allowPath && (parsed.pathname !== "/" || parsed.search))
  ) {
    throw new SafeToolError("loopback_url_required");
  }
  return parsed;
}

export function requireGitSHA(value) {
  if (!/^[0-9a-f]{40}$/.test(value)) {
    throw new SafeToolError("release_identity_invalid");
  }
  return value;
}

export function requireSHA256(value) {
  if (!/^[0-9a-f]{64}$/.test(value)) {
    throw new SafeToolError("release_identity_invalid");
  }
  return value;
}

export function requireImageID(value) {
  if (!/^sha256:[0-9a-f]{64}$/.test(value)) {
    throw new SafeToolError("release_identity_invalid");
  }
  return value;
}

export function requireDistinctPaths(paths) {
  const normalized = paths.map((path) => resolve(path));
  if (new Set(normalized).size !== normalized.length) {
    throw new SafeToolError("credential_input_invalid");
  }
  return normalized;
}

export async function reserveProtectedFile(location, conflicts = []) {
  const output = resolve(location);
  await requireProtectedParent(output);
  await requireDisjoint(output, conflicts);
  let handle;
  try {
    handle = await open(
      output,
      constants.O_WRONLY |
        constants.O_CREAT |
        constants.O_EXCL |
        constants.O_NOFOLLOW,
      0o600,
    );
    const information = await handle.stat();
    if (
      !information.isFile() ||
      information.nlink !== 1 ||
      (information.mode & 0o777) !== 0o600 ||
      information.uid !== process.getuid()
    ) {
      throw new SafeToolError("output_reservation_failed");
    }
    await replaceFileContent(handle, '{"status":"reserved"}\n');
  } catch (error) {
    await handle?.close().catch(() => {});
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError("output_reservation_failed");
  }
  let closed = false;
  return {
    path: output,
    async writeJSON(value) {
      if (closed) throw new SafeToolError("output_write_failed");
      try {
        await replaceFileContent(handle, `${JSON.stringify(value, null, 2)}\n`);
      } catch {
        throw new SafeToolError("output_write_failed");
      }
    },
    async close() {
      if (closed) return;
      closed = true;
      await handle.close();
    },
  };
}

export async function reserveProtectedDirectory(location, conflicts = []) {
  const output = resolve(location);
  await requireProtectedParent(output);
  await requireDisjoint(output, conflicts);
  try {
    await mkdir(output, { mode: 0o700 });
    const [information, resolved] = await Promise.all([
      lstat(output),
      realpath(output),
    ]);
    if (
      !information.isDirectory() ||
      information.isSymbolicLink() ||
      (information.mode & 0o777) !== 0o700 ||
      information.uid !== process.getuid() ||
      resolved !== output
    ) {
      throw new SafeToolError("output_reservation_failed");
    }
  } catch (error) {
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError("output_reservation_failed");
  }
  return output;
}

export async function writeProtectedChild(directory, name, value) {
  if (!/^[a-z0-9][a-z0-9.-]*\.json$/.test(name)) {
    throw new SafeToolError("output_write_failed");
  }
  const path = resolve(directory, name);
  if (dirname(path) !== directory)
    throw new SafeToolError("output_write_failed");
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
    const information = await handle.stat();
    if (
      !information.isFile() ||
      information.nlink !== 1 ||
      (information.mode & 0o777) !== 0o600 ||
      information.uid !== process.getuid()
    ) {
      throw new SafeToolError("output_write_failed");
    }
    await replaceFileContent(handle, `${JSON.stringify(value, null, 2)}\n`);
  } catch (error) {
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError("output_write_failed");
  } finally {
    await handle?.close().catch(() => {});
  }
}

export async function readProtectedSecret(location, maximumBytes) {
  let handle;
  try {
    handle = await open(
      resolve(location),
      constants.O_RDONLY | constants.O_NOFOLLOW,
    );
    const information = await handle.stat();
    if (
      !information.isFile() ||
      information.nlink !== 1 ||
      (information.mode & 0o077) !== 0 ||
      information.uid !== process.getuid() ||
      information.size < 1 ||
      information.size > maximumBytes + 2
    ) {
      throw new SafeToolError("credential_input_invalid");
    }
    const content = await handle.readFile();
    const end =
      content.at(-1) === 0x0a
        ? content.length - (content.at(-2) === 0x0d ? 2 : 1)
        : content.length;
    const result = Buffer.from(content.subarray(0, end));
    content.fill(0);
    if (result.length < 1 || result.length > maximumBytes) {
      result.fill(0);
      throw new SafeToolError("credential_input_invalid");
    }
    return result;
  } catch (error) {
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError("credential_input_invalid");
  } finally {
    await handle?.close().catch(() => {});
  }
}

export async function readProtectedJSON(location, maximumBytes) {
  let handle;
  try {
    handle = await open(
      resolve(location),
      constants.O_RDONLY | constants.O_NOFOLLOW,
    );
    const information = await handle.stat();
    if (
      !information.isFile() ||
      information.nlink !== 1 ||
      (information.mode & 0o077) !== 0 ||
      information.uid !== process.getuid() ||
      information.size < 2 ||
      information.size > maximumBytes
    ) {
      throw new SafeToolError("protected_report_invalid");
    }
    return JSON.parse(await handle.readFile({ encoding: "utf8" }));
  } catch (error) {
    if (error instanceof SafeToolError) throw error;
    throw new SafeToolError("protected_report_invalid");
  } finally {
    await handle?.close().catch(() => {});
  }
}

export function safeErrorCode(error) {
  return error instanceof SafeToolError ? error.code : "run_failed";
}

async function requireProtectedParent(output) {
  if (output === temporaryRoot || !output.startsWith(`${temporaryRoot}/`)) {
    throw new SafeToolError("output_location_invalid");
  }
  const parent = dirname(output);
  let information;
  let resolvedParent;
  try {
    [information, resolvedParent] = await Promise.all([
      lstat(parent),
      realpath(parent),
    ]);
  } catch {
    throw new SafeToolError("output_parent_invalid");
  }
  if (
    !information.isDirectory() ||
    information.isSymbolicLink() ||
    (information.mode & 0o077) !== 0 ||
    information.uid !== process.getuid() ||
    resolvedParent !== parent
  ) {
    throw new SafeToolError("output_parent_invalid");
  }
}

async function requireDisjoint(output, conflicts) {
  for (const conflict of conflicts) {
    const candidate = resolve(conflict);
    if (candidate === output) throw new SafeToolError("path_conflict");
    let resolvedConflict = candidate;
    let information;
    try {
      [resolvedConflict, information] = await Promise.all([
        realpath(candidate),
        stat(candidate),
      ]);
    } catch {
      // Missing optional inputs are rejected by their own readers.
    }
    if (resolvedConflict === output) throw new SafeToolError("path_conflict");
    const nested = relative(resolvedConflict, output);
    if (
      information?.isDirectory() &&
      (nested === "" || (!nested.startsWith("..") && !isAbsolute(nested)))
    ) {
      throw new SafeToolError("path_conflict");
    }
  }
}

async function replaceFileContent(handle, text) {
  const content = Buffer.from(text, "utf8");
  await handle.truncate(0);
  let written = 0;
  while (written < content.length) {
    const result = await handle.write(
      content,
      written,
      content.length - written,
      written,
    );
    if (result.bytesWritten < 1) throw new Error("write made no progress");
    written += result.bytesWritten;
  }
  await handle.truncate(content.length);
  await handle.sync();
}
