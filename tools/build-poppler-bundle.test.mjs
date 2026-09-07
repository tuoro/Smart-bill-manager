import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const toolsDirectory = dirname(fileURLToPath(import.meta.url));
const script = await readFile(join(toolsDirectory, "build-poppler-bundle.sh"), "utf8");
const preparer = await readFile(
  join(toolsDirectory, "prepare-local-release-artifacts.mjs"),
  "utf8",
);

test("bundle recipe pins the same poppler identity the preparer verifies", () => {
  const expectedVersion = /const expectedPopplerVersion = "([^"]+)"/.exec(preparer)?.[1];
  const expectedSource = /const expectedPopplerSourceSHA256 =\s*"([0-9a-f]{64})"/.exec(
    preparer,
  )?.[1];
  assert.ok(expectedVersion, "preparer must pin a poppler version");
  assert.ok(expectedSource, "preparer must pin a poppler source digest");
  assert.match(script, new RegExp(`POPPLER_VERSION=${expectedVersion.replace(/\./g, "\\.")}\\b`));
  assert.match(script, new RegExp(`POPPLER_SOURCE_SHA256=${expectedSource}\\b`));
});

test("bundle recipe fails on a source checksum mismatch instead of continuing", () => {
  assert.match(script, /poppler source checksum mismatch/);
  const mismatch = script.indexOf("poppler source checksum mismatch");
  assert.match(script.slice(mismatch, mismatch + 300), /exit 1/);
});

test("bundle recipe pins the build image by digest and refuses to resolve new tags", () => {
  assert.match(script, /BUILD_IMAGE="debian@sha256:[0-9a-f]{64}"/);
  assert.match(script, /pinned build image is not present locally/);
  assert.doesNotMatch(script, /docker pull/);
});

test("bundle recipe encodes the three release-gate constraints", () => {
  // 1. lib 内不得有符号链接，否则 listRegularFiles 判定 artifact_tree_invalid。
  assert.match(script, /symlinks remain in lib/);
  assert.match(script, /if \[ -L "\$f" \]/);
  // 2. 库用 $ORIGIN，可执行文件用 $ORIGIN/../lib。
  assert.match(script, /lib\/\*\.so\*; do patchelf --set-rpath "\\\$ORIGIN"/);
  assert.match(script, /bin\/\*; do patchelf --set-rpath "\\\$ORIGIN\/\.\.\/lib"/);
  // 3. 依赖闭包迭代收集，且不得依赖 ldd 的 "not found"——构建容器里系统库全部
  //    可解析，据此判断会一个依赖都收集不到。
  assert.match(script, /awk "\/=> \\\/\/\{print \\\$3\}"/);
  // 依赖收集不得回退到 ldd 的 "not found" 判断：构建容器里系统库全部可解析，
  // 据此判断会一个依赖都收集不到。
  assert.doesNotMatch(script, /awk "\/not found\//);
  // 收集结果必须经硬性校验，缺失核心库立即失败而非留到发布门禁。
  assert.match(script, /dependency closure missing/);
  for (const library of [
    "libfreetype.so.6",
    "libfontconfig.so.1",
    "libpng16.so.16",
    "liblcms2.so.2",
  ]) {
    assert.ok(script.includes(library), `recipe should require ${library}`);
  }
});

test("bundle recipe self-checks without LD_LIBRARY_PATH under gate constraints", () => {
  const verify = script.slice(script.indexOf("verifying under release-gate constraints"));
  assert.match(verify, /--network none/);
  assert.match(verify, /--read-only/);
  assert.match(verify, /--user 10001:10001/);
  assert.match(verify, /--cap-drop ALL/);
  // 设置 LD_LIBRARY_PATH 会掩盖 RUNPATH 配置错误，自检阶段不得出现。
  assert.doesNotMatch(verify, /LD_LIBRARY_PATH/);
});

test("bundle recipe produces every regular file the preparer requires", () => {
  const required = ["bin/pdfinfo", "bin/pdftoppm", "lib/libpoppler.so.160", "etc/fonts/fonts.conf"];
  for (const entry of required) {
    assert.ok(preparer.includes(`"${entry}"`), `preparer should require ${entry}`);
    assert.ok(script.includes(entry), `recipe should verify ${entry}`);
  }
});
