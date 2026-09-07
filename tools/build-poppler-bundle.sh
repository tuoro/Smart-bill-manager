#!/usr/bin/env bash
# 构建 prepare-local-release-artifacts.mjs 所需的 Poppler bundle。
#
# 产出目录布局（与 prepare-local-release-artifacts.mjs 和 app.Dockerfile 的期望一致）：
#   manifest.json          名称、版本、目标平台/架构与源码 SHA-256
#   poppler/bin/           仅 pdfinfo 与 pdftoppm，应用只用这两个
#   poppler/lib/           上述二进制的完整依赖闭包，全部为真实文件
#   poppler/share/poppler/ cMap 等编码数据
#   poppler/fonts/         内置字体
#   poppler/etc/fonts/     fontconfig 配置
#
# 三处易错约束（缺一都会在发布门禁而非本地功能测试中失败）：
#   1. lib 内不得有符号链接。prepare-local-release-artifacts.mjs 用 listRegularFiles
#      检查必需文件，符号链接不计入，会报 artifact_tree_invalid。
#   2. 库的 RUNPATH 必须是 $ORIGIN，可执行文件才是 $ORIGIN/../lib。库与其依赖同在
#      lib 目录，若也设成 $ORIGIN/../lib 会指向别处，运行时报 cannot open shared
#      object file。
#   3. 依赖闭包需迭代求解。只对可执行文件求一次 ldd 会漏掉 libpoppler 自身的依赖，
#      以及 freetype/fontconfig 的二级依赖（libbrotlidec、libexpat 等）。
#
# 构建在钉死 digest 的 Debian 13 容器内进行，与 app.Dockerfile 使用的 glibc 来源
# 保持同一发行版。不同发行版会产出不同的二进制（例如 libjpeg SONAME 8 与 62 的差异），
# 因此更换基础镜像会改变发布输入摘要。
set -euo pipefail

TOOL_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_DIR=$(cd -- "${TOOL_DIR}/.." && pwd)

POPPLER_VERSION=26.05.0
POPPLER_SOURCE_SHA256=6fef27ff04f37db43054c86bcdff6128c9fb1f6af4ef3c8b369a7e9abd68d0bb
POPPLER_SOURCE_URL="https://poppler.freedesktop.org/poppler-${POPPLER_VERSION}.tar.xz"
BUILD_IMAGE="debian@sha256:f324c7ff54321e8d9c588493a20244965938ce0aa50bbd1022d38010e9ffc4b1"

usage() {
  cat >&2 <<'USAGE'
usage: build-poppler-bundle.sh --output-directory ABSOLUTE_NEW_DIRECTORY
                               [--source-archive EXISTING_TAR_XZ]

--source-archive 指向已下载并保留的源码包时不访问网络；未提供时从 poppler.freedesktop.org
下载。两种情况都强制核对 SHA-256，不匹配立即失败。
USAGE
  exit 2
}

output_directory=
source_archive=
while [[ $# -gt 0 ]]; do
  [[ $# -ge 2 ]] || usage
  case "$1" in
    --output-directory) output_directory=$2 ;;
    --source-archive) source_archive=$2 ;;
    *) usage ;;
  esac
  shift 2
done

[[ -n "${output_directory}" ]] || usage
[[ "${output_directory}" = /* ]] || { echo "output directory must be absolute" >&2; exit 2; }
[[ ! -e "${output_directory}" ]] || { echo "output directory must not already exist: ${output_directory}" >&2; exit 1; }
parent=$(dirname -- "${output_directory}")
[[ -d "${parent}" ]] || { echo "output parent must exist: ${parent}" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || { echo "docker is required" >&2; exit 1; }
docker image inspect "${BUILD_IMAGE}" >/dev/null 2>&1 || {
  echo "pinned build image is not present locally: ${BUILD_IMAGE}" >&2
  echo "pull it explicitly before building; this tool does not resolve new tags" >&2
  exit 1
}

workspace=$(mktemp -d "${TMPDIR:-/tmp}/sbm-poppler-build.XXXXXX")
chmod 0700 "${workspace}"
cleanup() {
  # 清理失败不得改写脚本退出码：真实结果由后续检查决定。
  rm -rf -- "${workspace}" 2>/dev/null || \
    echo "warning: could not fully remove ${workspace}" >&2
}
trap cleanup EXIT HUP INT TERM

archive="${workspace}/source/poppler-${POPPLER_VERSION}.tar.xz"
mkdir -p "${workspace}/source" "${workspace}/out"
if [[ -n "${source_archive}" ]]; then
  [[ -f "${source_archive}" ]] || { echo "source archive not found: ${source_archive}" >&2; exit 1; }
  cp -- "${source_archive}" "${archive}"
else
  command -v curl >/dev/null 2>&1 || { echo "curl is required to download the source" >&2; exit 1; }
  echo "downloading ${POPPLER_SOURCE_URL}" >&2
  curl -fsSL --proto '=https' --tlsv1.2 -o "${archive}" "${POPPLER_SOURCE_URL}"
fi

actual=$(sha256sum "${archive}" | awk '{print $1}')
if [[ "${actual}" != "${POPPLER_SOURCE_SHA256}" ]]; then
  echo "poppler source checksum mismatch" >&2
  echo "  expected ${POPPLER_SOURCE_SHA256}" >&2
  echo "  actual   ${actual}" >&2
  exit 1
fi
echo "source checksum verified" >&2

# 以宿主 uid/gid 写出产物：容器内以 root 创建会让宿主无法清理工作目录，
# 且复制到输出目录后属主也不正确。构建仍以 root 安装依赖，仅落盘环节降权。
host_uid=$(id -u)
host_gid=$(id -g)
docker run --rm \
  -v "${workspace}/source":/src:ro \
  -v "${workspace}/out":/out \
  -w /build \
  -e "POPPLER_VERSION=${POPPLER_VERSION}" \
  -e "HOST_UID=${host_uid}" \
  -e "HOST_GID=${host_gid}" \
  "${BUILD_IMAGE}" bash -euo pipefail -c '
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq --no-install-recommends \
  build-essential cmake ninja-build pkg-config xz-utils patchelf \
  libfreetype-dev libfontconfig-dev libjpeg-dev libopenjp2-7-dev \
  libpng-dev zlib1g-dev liblcms2-dev \
  poppler-data fonts-dejavu-core fonts-inconsolata >/dev/null

mkdir -p /build && cd /build
tar -xf "/src/poppler-${POPPLER_VERSION}.tar.xz"
cd "poppler-${POPPLER_VERSION}"

# 关闭 qt/glib/gpgme/boost/curl/nss/tiff：应用只调用 pdfinfo 与 pdftoppm，
# 多余特性会引入无谓的运行时依赖。
cmake -S . -B build -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX=/build/install \
  -DBUILD_SHARED_LIBS=ON \
  -DENABLE_QT5=OFF -DENABLE_QT6=OFF -DENABLE_GLIB=OFF -DENABLE_GPGME=OFF \
  -DENABLE_BOOST=OFF -DENABLE_LIBCURL=OFF -DENABLE_NSS3=OFF -DENABLE_LIBTIFF=OFF \
  -DBUILD_GTK_TESTS=OFF -DBUILD_CPP_TESTS=OFF -DBUILD_MANUAL_TESTS=OFF \
  -DENABLE_UTILS=ON >/dev/null
cmake --build build --parallel >/dev/null
cmake --install build >/dev/null

B=/out/poppler
mkdir -p "$B/bin" "$B/lib" "$B/etc/fonts" "$B/share/poppler" "$B/fonts"
cp /build/install/bin/pdfinfo /build/install/bin/pdftoppm "$B/bin/"
cp /build/install/lib/libpoppler.so.* "$B/lib/"

# 约束 3：迭代收集已解析的依赖，直到不再新增。
#
# 不能靠 ldd 的 "not found" 判断：构建容器里 freetype、fontconfig 等都是系统安装的，
# ldd 全部能解析，一条 not found 都不会出现，据此判断会收集不到任何依赖。
# 正确做法是收集 "=> /路径" 的实际解析结果，并跳过由 glibc 来源镜像提供的核心库。
for _ in 1 2 3 4 5; do
  before=$(ls -1 "$B/lib" | wc -l)
  resolved=$(ldd "$B"/bin/* "$B"/lib/*.so* 2>/dev/null \
    | awk "/=> \//{print \$3}" | sort -u || true)
  for path in $resolved; do
    name=$(basename "$path")
    # libc/libm/libdl/libpthread 与动态加载器来自 app.Dockerfile 复制的 glibc，不重复打包。
    case "$name" in
      libc.so.6|libm.so.6|libdl.so.2|libpthread.so.0|ld-linux-x86-64.so.2) continue ;;
    esac
    [ -e "$B/lib/$name" ] && continue
    cp -Ln "$path" "$B/lib/$name"
  done
  after=$(ls -1 "$B/lib" | wc -l)
  [ "$before" = "$after" ] && break
done

# 收集结果必须包含 poppler 实际链接的图形与字体库，缺失说明收集逻辑失效。
for required_lib in libfreetype.so.6 libfontconfig.so.1 libpng16.so.16 liblcms2.so.2; do
  [ -e "$B/lib/$required_lib" ] || { echo "dependency closure missing $required_lib" >&2; exit 1; }
done

# 约束 1：解开全部符号链接，只保留真实文件；去掉开发用别名与版本化副本。
cd "$B/lib"
for f in *; do
  if [ -L "$f" ]; then target=$(readlink -f "$f"); rm "$f"; cp "$target" "$f"; fi
done
rm -f libpoppler.so libpoppler.so.[0-9]*.[0-9]*.[0-9]*
if [ -n "$(find . -type l)" ]; then echo "symlinks remain in lib" >&2; exit 1; fi

# 约束 2：库用 $ORIGIN，可执行文件用 $ORIGIN/../lib。
for f in "$B"/lib/*.so*; do patchelf --set-rpath "\$ORIGIN" "$f"; done
for f in "$B"/bin/*; do patchelf --set-rpath "\$ORIGIN/../lib" "$f"; done

cp -r /usr/share/poppler/. "$B/share/poppler/"
# 按目录收集，不写死扩展名：Debian 的 Inconsolata 是 .otf 而非 .ttf。
find /usr/share/fonts/truetype/dejavu /usr/share/fonts/truetype/inconsolata \
  -type f \( -name "*.ttf" -o -name "*.otf" \) -exec cp {} "$B/fonts/" \;
[ -n "$(ls -A "$B/fonts")" ] || { echo "no fonts collected" >&2; exit 1; }
cat > "$B/etc/fonts/fonts.conf" <<CONF
<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<fontconfig>
  <dir>/opt/sbm-poppler/fonts</dir>
  <cachedir>/tmp/fontconfig</cachedir>
  <config></config>
</fontconfig>
CONF

cat > /out/manifest.json <<JSON
{
  "name": "poppler",
  "version": "'"${POPPLER_VERSION}"'",
  "targetPlatform": "linux",
  "targetArch": "x64",
  "sourceSha256": "'"${POPPLER_SOURCE_SHA256}"'"
}
JSON
chmod -R a+rX /out
chown -R "${HOST_UID}:${HOST_GID}" /out
'

# 在发布门禁的实际约束下自检：只读根、UID 10001、无网络、无 capability，
# 且不设 LD_LIBRARY_PATH——设了会掩盖 RUNPATH 配置错误。
# 自检失败时保留工作目录并打印实际错误：这一步失败通常意味着 RUNPATH 或依赖
# 闭包有问题，删掉现场就无法定位。
echo "verifying under release-gate constraints" >&2
if ! docker run --rm --network none --read-only --user 10001:10001 \
  --cap-drop ALL --security-opt no-new-privileges:true \
  -v "${workspace}/out/poppler":/opt/sbm-poppler:ro \
  "${BUILD_IMAGE}" sh -ec '
/opt/sbm-poppler/bin/pdfinfo -v
/opt/sbm-poppler/bin/pdftoppm -v
for file in $(find /opt/sbm-poppler -type f); do test -r "$file"; done
'; then
  trap - EXIT HUP INT TERM
  echo "release-gate self-check failed; workspace kept at ${workspace}" >&2
  exit 1
fi

for required in bin/pdfinfo bin/pdftoppm lib/libpoppler.so.160 etc/fonts/fonts.conf; do
  [[ -f "${workspace}/out/poppler/${required}" ]] || {
    echo "bundle is missing a required regular file: ${required}" >&2
    exit 1
  }
done

mkdir -p -- "${output_directory}"
chmod 0700 -- "${output_directory}"
cp -a -- "${workspace}/out/." "${output_directory}/"
echo "poppler bundle ready: ${output_directory}" >&2
