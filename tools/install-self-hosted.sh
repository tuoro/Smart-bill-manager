#!/bin/sh
set -eu

usage() {
  cat >&2 <<'EOF'
usage: install-self-hosted.sh [options]

options:
  --release-version vMAJOR.MINOR.PATCH
  --runtime-directory ABSOLUTE_NEW_DIRECTORY
  --postgres-directory ABSOLUTE_NEW_DIRECTORY
  --objects-directory ABSOLUTE_NEW_DIRECTORY
  --backups-directory ABSOLUTE_NEW_DIRECTORY
  --http-port PORT
EOF
  exit 2
}

release_version=
runtime_directory=
postgres_directory=
objects_directory=
backups_directory=
http_port=

while [ "$#" -gt 0 ]; do
  [ "$#" -ge 2 ] || usage
  case "$1" in
    --release-version) release_version=$2 ;;
    --runtime-directory) runtime_directory=$2 ;;
    --postgres-directory) postgres_directory=$2 ;;
    --objects-directory) objects_directory=$2 ;;
    --backups-directory) backups_directory=$2 ;;
    --http-port) http_port=$2 ;;
    *) usage ;;
  esac
  shift 2
done

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
if [ -x "${script_directory}/prepare-self-hosted-deployment.sh" ]; then
  tools_directory=$script_directory
  bundle_root=$(dirname -- "$script_directory")
elif [ -x "${script_directory}/tools/prepare-self-hosted-deployment.sh" ]; then
  tools_directory=${script_directory}/tools
  bundle_root=$script_directory
else
  [ -n "$release_version" ] || {
    printf '%s\n' "--release-version is required when the installer is streamed" >&2
    exit 2
  }
  printf '%s\n' "$release_version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || {
    printf '%s\n' "release version must use vMAJOR.MINOR.PATCH" >&2
    exit 2
  }
  for downloader_command in curl sha256sum tar; do
    command -v "$downloader_command" >/dev/null 2>&1 || {
      printf '%s\n' "$downloader_command is required for one-command installation" >&2
      exit 1
    }
  done

  remote_directory=$(mktemp -d "${TMPDIR:-/tmp}/sbm-release-install.XXXXXX")
  chmod 0700 "$remote_directory"
  cleanup_remote() {
    case "$remote_directory" in
      "${TMPDIR:-/tmp}"/sbm-release-install.*) rm -rf -- "$remote_directory" ;;
    esac
  }
  trap cleanup_remote EXIT HUP INT TERM

  archive_name=smart-bill-manager-docker-${release_version}.tar.gz
  release_url=https://github.com/tuoro/Smart-bill-manager/releases/download/${release_version}
  curl -fL --proto '=https' --tlsv1.2 \
    -o "${remote_directory}/${archive_name}" "${release_url}/${archive_name}"
  curl -fL --proto '=https' --tlsv1.2 \
    -o "${remote_directory}/${archive_name}.sha256" "${release_url}/${archive_name}.sha256"
  (CDPATH= cd -- "$remote_directory" && sha256sum -c "${archive_name}.sha256")
  tar -xzf "${remote_directory}/${archive_name}" -C "$remote_directory"
  remote_installer=${remote_directory}/smart-bill-manager-docker/install.sh
  [ -x "$remote_installer" ] || {
    printf '%s\n' "verified deployment bundle does not contain install.sh" >&2
    exit 1
  }

  set --
  [ -z "$runtime_directory" ] || set -- "$@" --runtime-directory "$runtime_directory"
  [ -z "$postgres_directory" ] || set -- "$@" --postgres-directory "$postgres_directory"
  [ -z "$objects_directory" ] || set -- "$@" --objects-directory "$objects_directory"
  [ -z "$backups_directory" ] || set -- "$@" --backups-directory "$backups_directory"
  [ -z "$http_port" ] || set -- "$@" --http-port "$http_port"
  "$remote_installer" "$@"
  trap - EXIT HUP INT TERM
  cleanup_remote
  exit 0
fi

# 前置检查放在提问之前：不满足条件时立即失败，不让用户先回答一轮问题、
# 也不在磁盘上留下任何目录。
release_environment=${bundle_root}/infra/compose/release.env
[ -f "$release_environment" ] || {
  printf '%s\n' "deployment bundle is incomplete: infra/compose/release.env is missing" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || {
  printf '%s\n' "docker 未安装或不在 PATH 中。请先安装 Docker Engine。" >&2
  exit 1
}
docker version >/dev/null 2>&1 || {
  printf '%s\n' "无法连接 Docker daemon。请确认它正在运行，且当前用户有权限访问。" >&2
  exit 1
}
compose_version=$(docker compose version --short 2>/dev/null) || {
  printf '%s\n' "docker compose 不可用。需要 Docker Compose 2.24.4 或更新版本。" >&2
  exit 1
}
compose_major=${compose_version%%.*}
compose_rest=${compose_version#*.}
compose_minor=${compose_rest%%.*}
compose_patch=${compose_rest#*.}
compose_patch=${compose_patch%%[!0-9]*}
[ -n "$compose_patch" ] || compose_patch=0
compose_supported=false
if [ "$compose_major" -gt 2 ] 2>/dev/null; then
  compose_supported=true
elif [ "$compose_major" -eq 2 ] 2>/dev/null; then
  if [ "$compose_minor" -gt 24 ] 2>/dev/null; then
    compose_supported=true
  elif [ "$compose_minor" -eq 24 ] && [ "$compose_patch" -ge 4 ] 2>/dev/null; then
    compose_supported=true
  fi
fi
[ "$compose_supported" = true ] || {
  printf '当前 Docker Compose 版本为 %s，需要 2.24.4 或更新版本。\n' "$compose_version" >&2
  exit 1
}

if [ -r /proc/meminfo ]; then
  available_kib=$(awk '/^MemAvailable:/ { print $2 }' /proc/meminfo)
  case "$available_kib" in
    ''|*[!0-9]*) ;;
    *)
      if [ "$available_kib" -lt 6291456 ]; then
        printf '可用内存约 %s MiB，低于建议的 6144 MiB。安装可能因内存不足失败。\n' \
          "$((available_kib / 1024))" >&2
        printf '%s' "仍要继续请按 Enter，或按 Ctrl+C 停止：" >&2
        if [ ! -t 0 ] && ( : </dev/tty ) 2>/dev/null; then
          IFS= read -r _ </dev/tty || exit 1
        else
          IFS= read -r _ || exit 1
        fi
      fi
      ;;
  esac
fi

default_runtime_directory=$(dirname -- "$bundle_root")/smart-bill-manager-runtime
use_controlling_terminal=false
if [ ! -t 0 ] && ( : </dev/tty ) 2>/dev/null; then
  use_controlling_terminal=true
fi

read_install_input() {
  if [ "$use_controlling_terminal" = true ]; then
    IFS= read -r prompt_value </dev/tty
  else
    IFS= read -r prompt_value
  fi
}

prompt_default() {
  prompt_label=$1
  prompt_default_value=$2
  printf '%s [%s]: ' "$prompt_label" "$prompt_default_value" >&2
  read_install_input || {
    printf '%s\n' "installation input ended before configuration was complete" >&2
    exit 1
  }
  if [ -n "$prompt_value" ]; then
    printf '%s\n' "$prompt_value"
  else
    printf '%s\n' "$prompt_default_value"
  fi
}

prompt_required() {
  prompt_label=$1
  printf '%s: ' "$prompt_label" >&2
  read_install_input || {
    printf '%s\n' "installation input ended before configuration was complete" >&2
    exit 1
  }
  [ -n "$prompt_value" ] || {
    printf '%s\n' "$prompt_label is required" >&2
    exit 2
  }
  printf '%s\n' "$prompt_value"
}

[ -n "$runtime_directory" ] || runtime_directory=$(prompt_default "运行目录" "$default_runtime_directory")

# 已存在的运行目录：配置齐全时视为上次未装完，沿用原配置继续，不再重复提问；
# 否则明确拒绝，并说明它不是本安装器创建的。
resume_installation=false
if [ -e "$runtime_directory" ] || [ -L "$runtime_directory" ]; then
  if [ -f "${runtime_directory}/deployment.env" ] && [ ! -L "${runtime_directory}/deployment.env" ]; then
    resume_installation=true
    http_port=$(awk -F= '/^SBM_HTTP_PORT=/ { print $2 }' "${runtime_directory}/deployment.env")
    [ -n "$http_port" ] || http_port=8080
    printf '%s\n' "检测到未完成的安装，沿用已有配置继续：${runtime_directory}" >&2
  else
    printf '%s\n' "运行目录已存在且不包含 deployment.env：${runtime_directory}" >&2
    printf '%s\n' "请换一个尚不存在的目录，或先移除该目录后重试。" >&2
    exit 1
  fi
fi

if [ "$resume_installation" = false ]; then
  [ -n "$postgres_directory" ] || postgres_directory=$(prompt_default "PostgreSQL 数据目录" "$runtime_directory/data/postgres")
  [ -n "$objects_directory" ] || objects_directory=$(prompt_default "附件对象目录" "$runtime_directory/data/objects")
  [ -n "$backups_directory" ] || backups_directory=$(prompt_default "备份目录" "$runtime_directory/backups")
  [ -n "$http_port" ] || http_port=$(prompt_default "本机 HTTP 端口" "8080")

  for required_value in "$runtime_directory" "$postgres_directory" "$objects_directory" \
    "$backups_directory" "$http_port"; do
    case "$required_value" in
      *'
'*) printf '%s\n' "installation values must not contain newlines" >&2; exit 2 ;;
    esac
  done
fi

# 先拉镜像。这是最常见的失败点（需要访问 ghcr.io），放在创建任何目录之前，
# 失败后磁盘上不留痕迹，重跑即可。
application_image=$(awk -F= '/^SBM_IMAGE=/ { print substr($0, index($0, "=") + 1) }' "$release_environment")
database_image=$(awk -F= '/^SBM_POSTGRES_IMAGE=/ { print substr($0, index($0, "=") + 1) }' "$release_environment")
for image in "$application_image" "$database_image"; do
  [ -n "$image" ] || {
    printf '%s\n' "deployment bundle is incomplete: release.env does not pin both images" >&2
    exit 1
  }
  docker image inspect "$image" >/dev/null 2>&1 && continue
  printf '正在拉取镜像：%s\n' "${image%%@*}" >&2
  docker pull --quiet "$image" >/dev/null || {
    printf '%s\n' "镜像拉取失败。请检查网络是否可以访问 ghcr.io 与 Docker Hub 后重试；" >&2
    printf '%s\n' "本次未创建任何目录，直接重新运行安装器即可。" >&2
    exit 1
  }
done

if [ "$resume_installation" = false ]; then
  set -- "$runtime_directory" --http-port "$http_port"
  [ "$postgres_directory" = "$runtime_directory/data/postgres" ] || \
    set -- "$@" --postgres-directory "$postgres_directory"
  [ "$objects_directory" = "$runtime_directory/data/objects" ] || \
    set -- "$@" --objects-directory "$objects_directory"
  [ "$backups_directory" = "$runtime_directory/backups" ] || \
    set -- "$@" --backups-directory "$backups_directory"
  "${tools_directory}/prepare-self-hosted-deployment.sh" "$@"
fi

deploy=${tools_directory}/sbm-deploy.sh
"$deploy" "$runtime_directory" pull
"$deploy" "$runtime_directory" bootstrap
"$deploy" "$runtime_directory" start
"$deploy" "$runtime_directory" status

printf '\nSmart Bill Manager 已启动：http://127.0.0.1:%s\n' "$http_port"
printf '%s\n' "在浏览器打开该地址创建 Owner 账号，完成一次性初始化。"
printf '运行目录：%s\n' "$runtime_directory"
printf '日常管理：%s %s status|logs|stop|start|down\n' "$deploy" "$runtime_directory"
