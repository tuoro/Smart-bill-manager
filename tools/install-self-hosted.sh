#!/bin/sh
set -eu

usage() {
  cat >&2 <<'EOF'
usage: install-self-hosted.sh [options]

options:
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

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
if [ -x "${script_directory}/prepare-self-hosted-deployment.sh" ]; then
  tools_directory=$script_directory
  bundle_root=$(dirname -- "$script_directory")
elif [ -x "${script_directory}/tools/prepare-self-hosted-deployment.sh" ]; then
  tools_directory=${script_directory}/tools
  bundle_root=$script_directory
else
  printf '%s\n' "deployment tools are unavailable" >&2
  exit 1
fi

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

default_runtime_directory=$(dirname -- "$bundle_root")/smart-bill-manager-runtime

prompt_default() {
  prompt_label=$1
  prompt_default_value=$2
  printf '%s [%s]: ' "$prompt_label" "$prompt_default_value" >&2
  IFS= read -r prompt_value || {
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
  IFS= read -r prompt_value || {
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
IFS= read -r _confirmation || {
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
