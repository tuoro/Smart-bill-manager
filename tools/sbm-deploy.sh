#!/bin/sh
set -eu

usage() {
  cat >&2 <<'EOF'
usage: tools/sbm-deploy.sh DEPLOYMENT_DIRECTORY COMMAND [arguments]

commands:
  pull
  bootstrap EMAIL DISPLAY_NAME TENANT_NAME CURRENCY TIMEZONE
  start
  status
  logs
  stop
  down
  upgrade --backup-confirmed
  config
EOF
  exit 2
}

[ "$#" -ge 2 ] || usage
deployment_directory=$1
command_name=$2
shift 2

case "$deployment_directory" in
  /*) ;;
  *) usage ;;
esac
[ -d "$deployment_directory" ] && [ ! -L "$deployment_directory" ] || {
  printf '%s\n' "deployment directory is unavailable" >&2
  exit 1
}
[ "$(stat -c '%a' "$deployment_directory" 2>/dev/null || true)" = "700" ] || {
  printf '%s\n' "deployment directory must have mode 0700" >&2
  exit 1
}

environment_file=${deployment_directory}/deployment.env
[ -f "$environment_file" ] && [ ! -L "$environment_file" ] || {
  printf '%s\n' "deployment environment file is unavailable" >&2
  exit 1
}

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
repository_root=$(dirname -- "$script_directory")
release_environment_file=${repository_root}/infra/compose/release.env
[ -f "$release_environment_file" ] && [ ! -L "$release_environment_file" ] || {
  printf '%s\n' "release environment file is unavailable" >&2
  exit 1
}
[ "$(stat -c '%a' "$environment_file" 2>/dev/null || true)" = "600" ] || {
  printf '%s\n' "deployment environment file must have mode 0600" >&2
  exit 1
}

project_name=$(sed -n 's/^SBM_COMPOSE_PROJECT_NAME=//p' "$environment_file")
[ -n "$project_name" ] || project_name=smart-bill-manager
case "$project_name" in
  [a-z0-9]* ) ;;
  *) printf '%s\n' "Compose project name is invalid" >&2; exit 1 ;;
esac
case "$project_name" in
  *[!a-z0-9_-]* ) printf '%s\n' "Compose project name is invalid" >&2; exit 1 ;;
esac

compose_version=$(docker compose version --short 2>/dev/null || true)
compose_version=${compose_version#v}
compose_major=${compose_version%%.*}
compose_remainder=${compose_version#*.}
compose_minor=${compose_remainder%%.*}
compose_patch=${compose_remainder#*.}
compose_patch=${compose_patch%%[-+]*}
for compose_component in "$compose_major" "$compose_minor" "$compose_patch"; do
  case "$compose_component" in
    ''|*[!0-9]*)
      printf '%s\n' "Docker Compose 2.24.4 or newer is required" >&2
      exit 1
      ;;
  esac
done
if [ "$compose_major" -lt 2 ] || \
  { [ "$compose_major" -eq 2 ] && [ "$compose_minor" -lt 24 ]; } || \
  { [ "$compose_major" -eq 2 ] && [ "$compose_minor" -eq 24 ] && [ "$compose_patch" -lt 4 ]; }; then
  printf '%s\n' "Docker Compose 2.24.4 or newer is required" >&2
  exit 1
fi

base_compose=${repository_root}/infra/compose/compose.yaml
release_compose=${repository_root}/infra/compose/compose.release.yaml
bootstrap_compose=${repository_root}/infra/compose/compose.bootstrap.yaml

# Compose 会让调用方环境覆盖 --env-file；清除全部应用插值键，确保只有
# 已审查的发布文件与用户部署文件能够提供配置。
unset \
  SBM_IMAGE \
  SBM_POSTGRES_IMAGE \
  SBM_PULL_POLICY \
  SBM_RELEASE_ARTIFACTS_SOURCE \
  SBM_BUILD_SHA \
  SBM_RELEASE_INPUT_SHA256 \
  SBM_STORAGE_TYPE \
  SBM_POSTGRES_DATA_SOURCE \
  SBM_OBJECTS_SOURCE \
  SBM_BACKUPS_DIRECTORY \
  SBM_COMPOSE_PROJECT_NAME \
  SBM_DEPLOYMENT_MODE \
  SBM_COOKIE_SECURE \
  SBM_BIND_ADDRESS \
  SBM_HTTP_PORT \
  SBM_SESSION_TTL \
  SBM_AI_CONCURRENCY \
  SBM_MASTER_KEY_SOURCE \
  SBM_POSTGRES_ADMIN_PASSWORD_SOURCE \
  SBM_POSTGRES_MIGRATION_PASSWORD_SOURCE \
  SBM_POSTGRES_RUNTIME_PASSWORD_SOURCE \
  SBM_OWNER_PASSWORD_SOURCE

compose() {
  docker compose \
    --project-name "$project_name" \
    --env-file "$environment_file" \
    --env-file "$release_environment_file" \
    -f "$base_compose" \
    -f "$release_compose" \
    "$@"
}

compose_with_bootstrap() {
  docker compose \
    --project-name "$project_name" \
    --env-file "$environment_file" \
    --env-file "$release_environment_file" \
    -f "$base_compose" \
    -f "$release_compose" \
    -f "$bootstrap_compose" \
    "$@"
}

case "$command_name" in
  pull)
    [ "$#" -eq 0 ] || usage
    compose pull database provision migrate app
    ;;
  bootstrap)
    [ "$#" -eq 5 ] || usage
    owner_email=$1
    owner_display_name=$2
    tenant_name=$3
    currency=$4
    timezone=$5
    owner_password=${deployment_directory}/owner-password
    [ -f "$owner_password" ] && [ ! -L "$owner_password" ] || {
      printf '%s\n' "one-time Owner password is unavailable; bootstrap may already be complete" >&2
      exit 1
    }
    compose up -d --no-build --pull never --wait database
    compose run --rm --no-deps provision
    compose run --rm --no-deps migrate
    compose_with_bootstrap run --rm --no-deps app \
      /app/bootstrap-owner \
      -email "$owner_email" \
      -display-name "$owner_display_name" \
      -tenant-name "$tenant_name" \
      -currency "$currency" \
      -timezone "$timezone" \
      -password-file /run/sbm-secrets/owner-password
    rm -f -- "$owner_password"
    printf '%s\n' "Owner bootstrap completed; the one-time password file was removed"
    ;;
  start)
    [ "$#" -eq 0 ] || usage
    compose up -d --no-build --pull never --wait app
    ;;
  status)
    [ "$#" -eq 0 ] || usage
    compose ps
    ;;
  logs)
    [ "$#" -eq 0 ] || usage
    compose logs --tail 200 app database
    ;;
  stop)
    [ "$#" -eq 0 ] || usage
    compose stop app
    ;;
  down)
    [ "$#" -eq 0 ] || usage
    compose down --remove-orphans
    ;;
  upgrade)
    [ "$#" -eq 1 ] && [ "$1" = "--backup-confirmed" ] || usage
    compose stop app
    compose up -d --no-build --pull never --wait database
    compose run --rm --no-deps provision
    compose run --rm --no-deps migrate
    compose up -d --no-build --pull never --wait app
    ;;
  config)
    [ "$#" -eq 0 ] || usage
    compose config
    ;;
  *)
    usage
    ;;
esac
