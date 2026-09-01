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
  --owner-email EMAIL
  --owner-display-name NAME
  --tenant-name NAME
  --currency CODE
  --timezone IANA_TIMEZONE
  --http-port PORT
EOF
  exit 2
}

release_version=
runtime_directory=
postgres_directory=
objects_directory=
backups_directory=
owner_email=
owner_display_name=
tenant_name=
currency=
timezone=
http_port=

while [ "$#" -gt 0 ]; do
  [ "$#" -ge 2 ] || usage
  case "$1" in
    --release-version) release_version=$2 ;;
    --runtime-directory) runtime_directory=$2 ;;
    --postgres-directory) postgres_directory=$2 ;;
    --objects-directory) objects_directory=$2 ;;
    --backups-directory) backups_directory=$2 ;;
    --owner-email) owner_email=$2 ;;
    --owner-display-name) owner_display_name=$2 ;;
    --tenant-name) tenant_name=$2 ;;
    --currency) currency=$2 ;;
    --timezone) timezone=$2 ;;
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
  [ -z "$owner_email" ] || set -- "$@" --owner-email "$owner_email"
  [ -z "$owner_display_name" ] || set -- "$@" --owner-display-name "$owner_display_name"
  [ -z "$tenant_name" ] || set -- "$@" --tenant-name "$tenant_name"
  [ -z "$currency" ] || set -- "$@" --currency "$currency"
  [ -z "$timezone" ] || set -- "$@" --timezone "$timezone"
  [ -z "$http_port" ] || set -- "$@" --http-port "$http_port"
  "$remote_installer" "$@"
  trap - EXIT HUP INT TERM
  cleanup_remote
  exit 0
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
[ -n "$postgres_directory" ] || postgres_directory=$(prompt_default "PostgreSQL 数据目录" "$runtime_directory/data/postgres")
[ -n "$objects_directory" ] || objects_directory=$(prompt_default "附件对象目录" "$runtime_directory/data/objects")
[ -n "$backups_directory" ] || backups_directory=$(prompt_default "备份目录" "$runtime_directory/backups")
[ -n "$owner_email" ] || owner_email=$(prompt_required "Owner 登录邮箱")
[ -n "$owner_display_name" ] || owner_display_name=$(prompt_default "Owner 显示名称" "Owner")
[ -n "$tenant_name" ] || tenant_name=$(prompt_default "工作区名称" "My Workspace")
[ -n "$currency" ] || currency=$(prompt_default "默认币种" "CNY")
[ -n "$timezone" ] || timezone=$(prompt_default "IANA 时区" "Asia/Shanghai")
[ -n "$http_port" ] || http_port=$(prompt_default "本机 HTTP 端口" "8080")

for required_value in "$runtime_directory" "$postgres_directory" "$objects_directory" \
  "$backups_directory" "$owner_email" "$owner_display_name" "$tenant_name" \
  "$currency" "$timezone" "$http_port"; do
  case "$required_value" in
    *'
'*) printf '%s\n' "installation values must not contain newlines" >&2; exit 2 ;;
  esac
done

set -- "$runtime_directory" --http-port "$http_port"
[ "$postgres_directory" = "$runtime_directory/data/postgres" ] || \
  set -- "$@" --postgres-directory "$postgres_directory"
[ "$objects_directory" = "$runtime_directory/data/objects" ] || \
  set -- "$@" --objects-directory "$objects_directory"
[ "$backups_directory" = "$runtime_directory/backups" ] || \
  set -- "$@" --backups-directory "$backups_directory"
"${tools_directory}/prepare-self-hosted-deployment.sh" "$@"

owner_password_file=${runtime_directory}/owner-password
printf '\n一次性 Owner 密码已写入：%s\n' "$owner_password_file"
printf '%s\n' "请现在将其保存到密码管理器；初始化成功后该文件会被删除。"
printf '%s' "保存完成后按 Enter 继续，或按 Ctrl+C 停止安装：" >&2
read_install_input || {
  printf '%s\n' "installation stopped before Owner bootstrap; prepared files were retained" >&2
  exit 1
}

deploy=${tools_directory}/sbm-deploy.sh
"$deploy" "$runtime_directory" pull
"$deploy" "$runtime_directory" bootstrap \
  "$owner_email" "$owner_display_name" "$tenant_name" "$currency" "$timezone"
"$deploy" "$runtime_directory" start
"$deploy" "$runtime_directory" status

printf '\nSmart Bill Manager 已启动：http://127.0.0.1:%s\n' "$http_port"
printf '运行目录：%s\n' "$runtime_directory"
printf '日常管理：%s %s status|logs|stop|start|down\n' "$deploy" "$runtime_directory"
