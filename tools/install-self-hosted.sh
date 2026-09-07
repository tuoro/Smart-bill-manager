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
  --yes                       不进行任何交互，全部使用默认值
EOF
  exit 2
}

release_version=
runtime_directory=
postgres_directory=
objects_directory=
backups_directory=
http_port=
assume_yes=false

while [ "$#" -gt 0 ]; do
  if [ "$1" = --yes ]; then
    assume_yes=true
    shift
    continue
  fi
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

explicit_runtime_directory=$runtime_directory

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
  printf '%s\n' "正在校验安装包…" >&2
  (CDPATH= cd -- "$remote_directory" && sha256sum -c --status "${archive_name}.sha256") || {
    printf '%s\n' "安装包校验失败，请重新运行安装器。" >&2
    exit 1
  }
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
  [ "$assume_yes" = false ] || set -- "$@" --yes
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
# 应用镜像只发布 linux/amd64。在 ARM 主机上失败信息会出现在拉取或启动阶段，
# 且不易看出根因，因此在这里显式判断 Docker 实际运行的架构。
server_architecture=$(docker version --format '{{.Server.Arch}}' 2>/dev/null)
case "$server_architecture" in
  amd64|x86_64|'') ;;
  *)
    printf 'Docker 运行在 %s 架构上，而当前版本只发布 linux/amd64 镜像。\n' \
      "$server_architecture" >&2
    printf '%s\n' "本机无法运行该镜像。请改用 x86_64 主机。" >&2
    exit 1
    ;;
esac

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

# 端口不该由用户回答。占用时自动向后找一个空闲端口，只在结果里告知。
port_in_use() {
  probe_port=$1
  probe_hex=$(printf '%04X' "$probe_port")
  for table in /proc/net/tcp /proc/net/tcp6; do
    [ -r "$table" ] || continue
    # 第 2 列为本地地址，形如 0100007F:1F90；状态 0A 表示 LISTEN。
    awk -v hex="$probe_hex" '
      NR > 1 {
        split($2, address, ":")
        if (address[2] == hex && $4 == "0A") { found = 1 }
      }
      END { exit !found }
    ' "$table" && return 0
  done
  return 1
}

select_http_port() {
  candidate=$1
  attempt=0
  while [ "$attempt" -lt 40 ]; do
    port_in_use "$candidate" || {
      printf '%s\n' "$candidate"
      return 0
    }
    candidate=$((candidate + 1))
    attempt=$((attempt + 1))
  done
  printf '%s\n' "$1"
}

# 默认运行目录必须独立于部署包位置。流式安装时部署包解压在 /tmp 的临时目录里，
# 安装结束会被 rm -rf；把运行目录默认到它旁边会让整个部署连同数据一起被删除。
if [ -n "${HOME:-}" ] && [ -d "$HOME" ] && [ -w "$HOME" ]; then
  default_runtime_directory=${HOME}/smart-bill-manager
else
  default_runtime_directory=$(pwd -P)/smart-bill-manager
fi
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

[ -n "$runtime_directory" ] || runtime_directory=$default_runtime_directory
[ -n "$http_port" ] || http_port=$(select_http_port 8080)

if [ "$assume_yes" = false ] && [ -z "$explicit_runtime_directory" ]; then
  printf '\n' >&2
  printf '%s\n' "即将安装 Smart Bill Manager：" >&2
  printf '\n' >&2
  printf '  数据保存在  %s\n' "$runtime_directory" >&2
  printf '  安装后访问  http://127.0.0.1:%s\n' "$http_port" >&2
  printf '\n' >&2
  printf '%s\n' "数据库、上传的单据和备份都会放在上面这个目录里，请勿随意删除。" >&2
  if [ "$http_port" != 8080 ]; then
    printf '%s\n' "（8080 端口已被占用，已自动改用 ${http_port}。）" >&2
  fi
  printf '\n' >&2
  printf '%s' "按 Enter 开始安装；如需换个位置，请直接输入完整路径：" >&2
  read_install_input || {
    printf '\n%s\n' "已取消安装。" >&2
    exit 1
  }
  [ -z "$prompt_value" ] || runtime_directory=$prompt_value
  printf '\n' >&2
fi

# 防御性检查：运行目录落在部署包内时，流式安装结束的清理会把数据一并删除。
case "$runtime_directory" in
  "$bundle_root"|"$bundle_root"/*|"$(dirname -- "$bundle_root")"/smart-bill-manager-runtime)
    printf '%s\n' "数据保存位置不能放在部署包目录内：${runtime_directory}" >&2
    printf '%s\n' "该目录在安装结束后会被清理，数据会一并丢失。请改用其他位置。" >&2
    exit 2
    ;;
esac

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
  # 数据库、附件与备份默认都放在运行目录下，普通安装无需过问。需要分盘或 NAS 时
  # 用 --postgres-directory / --objects-directory / --backups-directory 指定。
  [ -n "$postgres_directory" ] || postgres_directory=${runtime_directory}/data/postgres
  [ -n "$objects_directory" ] || objects_directory=${runtime_directory}/data/objects
  [ -n "$backups_directory" ] || backups_directory=${runtime_directory}/backups

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
step() {
  printf '[%s/4] %s\n' "$1" "$2" >&2
}

step 1 "下载程序镜像（约 600 MiB，首次安装耗时较长）"
for image in "$application_image" "$database_image"; do
  [ -n "$image" ] || {
    printf '%s\n' "deployment bundle is incomplete: release.env does not pin both images" >&2
    exit 1
  }
  docker image inspect "$image" >/dev/null 2>&1 && continue
  docker pull --quiet "$image" >/dev/null || {
    printf '%s\n' "镜像拉取失败。请检查网络是否可以访问 ghcr.io 与 Docker Hub 后重试；" >&2
    printf '%s\n' "本次未创建任何目录，直接重新运行安装器即可。" >&2
    exit 1
  }
done

step 2 "创建数据目录"
if [ "$resume_installation" = false ]; then
  set -- "$runtime_directory" --http-port "$http_port"
  [ "$postgres_directory" = "$runtime_directory/data/postgres" ] || \
    set -- "$@" --postgres-directory "$postgres_directory"
  [ "$objects_directory" = "$runtime_directory/data/objects" ] || \
    set -- "$@" --objects-directory "$objects_directory"
  [ "$backups_directory" = "$runtime_directory/backups" ] || \
    set -- "$@" --backups-directory "$backups_directory"
  "${tools_directory}/prepare-self-hosted-deployment.sh" "$@" >/dev/null
fi

# 流式安装时部署包解压在临时目录，安装结束即被清理。把它复制进运行目录，
# 使 sbm-deploy.sh 与它管理的数据同处一地，日常管理命令在清理后依然可用。
installed_bundle=${runtime_directory}/bundle
if [ "$(CDPATH= cd -- "$bundle_root" && pwd -P)" != "$(CDPATH= cd -- "$runtime_directory" 2>/dev/null && pwd -P || printf '%s' "$runtime_directory")/bundle" ]; then
  rm -rf -- "$installed_bundle"
  mkdir -p -- "$installed_bundle"
  chmod 0700 -- "$installed_bundle"
  (cd -- "$bundle_root" && tar -cf - .) | (cd -- "$installed_bundle" && tar -xf -)
fi

deploy=${installed_bundle}/tools/sbm-deploy.sh
[ -x "$deploy" ] || deploy=${tools_directory}/sbm-deploy.sh

# 运行目录内放一个包装脚本，使日常命令不必重复冗长的绝对路径。
manage=${runtime_directory}/sbm
cat >"$manage" <<'WRAPPER'
#!/bin/sh
# 由安装器生成：把运行目录固定为脚本自身所在目录。
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
exec "${here}/bundle/tools/sbm-deploy.sh" "$here" "$@"
WRAPPER
chmod 0755 "$manage"
"$deploy" "$runtime_directory" pull >/dev/null

step 3 "初始化数据库"
"$deploy" "$runtime_directory" bootstrap >/dev/null

step 4 "启动服务"
"$deploy" "$runtime_directory" start >/dev/null
"$deploy" "$runtime_directory" status >/dev/null

display_directory=$runtime_directory
case "$runtime_directory" in
  "${HOME:-/nonexistent}"/*) display_directory="~${runtime_directory#${HOME}}" ;;
esac

printf '\n'
printf '%s\n' "安装完成。"
printf '\n'
printf '  现在用浏览器打开   http://127.0.0.1:%s\n' "$http_port"
printf '%s\n' "  页面会引导你创建管理员账号，之后就可以开始使用。"
printf '\n'
printf '  数据保存在        %s\n' "$display_directory"
printf '  查看运行状态      %s/sbm status\n' "$display_directory"
printf '  停止 / 启动       %s/sbm stop   %s/sbm start\n' "$display_directory" "$display_directory"
printf '\n' 
