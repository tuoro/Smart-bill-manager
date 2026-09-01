#!/bin/sh
set -eu

usage() {
  cat >&2 <<'EOF'
usage: prepare-self-hosted-deployment.sh DEPLOYMENT_DIRECTORY [options]

options:
  --postgres-directory ABSOLUTE_NEW_DIRECTORY
  --objects-directory ABSOLUTE_NEW_DIRECTORY
  --backups-directory ABSOLUTE_NEW_DIRECTORY
  --http-port PORT
EOF
  exit 2
}

[ "$#" -ge 1 ] || usage
deployment_directory=$1
shift

postgres_directory_input=
objects_directory_input=
backups_directory_input=
http_port=8080

while [ "$#" -gt 0 ]; do
  [ "$#" -ge 2 ] || usage
  case "$1" in
    --postgres-directory) postgres_directory_input=$2 ;;
    --objects-directory) objects_directory_input=$2 ;;
    --backups-directory) backups_directory_input=$2 ;;
    --http-port) http_port=$2 ;;
    *) usage ;;
  esac
  shift 2
done

case "$http_port" in
  ''|*[!0-9]*)
    printf '%s\n' "HTTP port must be an integer from 1 to 65535" >&2
    exit 2
    ;;
esac
[ "$http_port" -ge 1 ] && [ "$http_port" -le 65535 ] || {
  printf '%s\n' "HTTP port must be an integer from 1 to 65535" >&2
  exit 2
}

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
repository_root=$(dirname -- "$script_directory")

normalize_new_directory() {
  candidate=$1
  label=$2
  case "$candidate" in
    /*) ;;
    *) printf '%s\n' "$label must be an absolute path" >&2; exit 2 ;;
  esac
  case "$candidate" in
    *[!A-Za-z0-9_./-]*)
      printf '%s\n' "$label contains unsupported characters" >&2
      exit 2
      ;;
  esac
  [ "$candidate" != "/" ] || {
    printf '%s\n' "$label must not be the filesystem root" >&2
    exit 2
  }

  candidate_parent=$(dirname -- "$candidate")
  candidate_name=$(basename -- "$candidate")
  case "$candidate_name" in
    ''|.|..) printf '%s\n' "$label name is invalid" >&2; exit 2 ;;
  esac
  [ -d "$candidate_parent" ] && [ ! -L "$candidate_parent" ] || {
    printf '%s\n' "$label parent must be an existing regular directory" >&2
    exit 1
  }
  candidate_parent=$(CDPATH= cd -- "$candidate_parent" && pwd -P)
  normalized=${candidate_parent}/${candidate_name}
  case "$normalized" in
    "$repository_root"|"$repository_root"/*)
      printf '%s\n' "$label must be outside the Git repository" >&2
      exit 2
      ;;
  esac
  [ ! -e "$normalized" ] && [ ! -L "$normalized" ] || {
    printf '%s\n' "$label must not already exist" >&2
    exit 1
  }
  printf '%s\n' "$normalized"
}

deployment_directory=$(normalize_new_directory "$deployment_directory" "deployment directory")

if [ -n "$postgres_directory_input" ]; then
  postgres_data_directory=$(normalize_new_directory "$postgres_directory_input" "PostgreSQL directory")
else
  postgres_data_directory=${deployment_directory}/data/postgres
fi
if [ -n "$objects_directory_input" ]; then
  objects_directory=$(normalize_new_directory "$objects_directory_input" "objects directory")
else
  objects_directory=${deployment_directory}/data/objects
fi
if [ -n "$backups_directory_input" ]; then
  backups_directory=$(normalize_new_directory "$backups_directory_input" "backups directory")
else
  backups_directory=${deployment_directory}/backups
fi

[ "$postgres_data_directory" != "$objects_directory" ] && \
  [ "$postgres_data_directory" != "$backups_directory" ] && \
  [ "$objects_directory" != "$backups_directory" ] || {
  printf '%s\n' "PostgreSQL, objects, and backups directories must be distinct" >&2
  exit 2
}

umask 077
mkdir -- "$deployment_directory"
chmod 0700 "$deployment_directory"

master_key=${deployment_directory}/master-key
postgres_admin_password=${deployment_directory}/postgres-admin-password
postgres_migration_password=${deployment_directory}/postgres-migration-password
postgres_runtime_password=${deployment_directory}/postgres-runtime-password
owner_password=${deployment_directory}/owner-password
environment_file=${deployment_directory}/deployment.env
default_data_directory=${deployment_directory}/data
created_default_data=false
created_postgres=false
created_objects=false
created_backups=false

cleanup() {
  rm -f -- \
    "$master_key" \
    "$postgres_admin_password" \
    "$postgres_migration_password" \
    "$postgres_runtime_password" \
    "$owner_password" \
    "$environment_file"
  [ "$created_postgres" = false ] || rmdir -- "$postgres_data_directory" 2>/dev/null || true
  [ "$created_objects" = false ] || rmdir -- "$objects_directory" 2>/dev/null || true
  [ "$created_backups" = false ] || rmdir -- "$backups_directory" 2>/dev/null || true
  [ "$created_default_data" = false ] || rmdir -- "$default_data_directory" 2>/dev/null || true
  rmdir -- "$deployment_directory" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

case "$postgres_data_directory:$objects_directory" in
  "$default_data_directory"/*|*:"$default_data_directory"/*)
    mkdir -- "$default_data_directory"
    created_default_data=true
    chmod 0700 "$default_data_directory"
    ;;
esac
mkdir -- "$postgres_data_directory"
created_postgres=true
mkdir -- "$objects_directory"
created_objects=true
mkdir -- "$backups_directory"
created_backups=true
chmod 0700 "$postgres_data_directory" "$objects_directory" "$backups_directory"

generate_hex_secret() {
  secret_path=$1
  od -An -N 32 -tx1 /dev/urandom | tr -d ' \n' >"$secret_path"
  [ "$(wc -c <"$secret_path" | tr -d ' ')" = "64" ] || {
    printf '%s\n' "failed to generate deployment secret" >&2
    exit 1
  }
  chmod 0600 "$secret_path"
}

generate_hex_secret "$master_key"
generate_hex_secret "$postgres_admin_password"
generate_hex_secret "$postgres_migration_password"
generate_hex_secret "$postgres_runtime_password"
generate_hex_secret "$owner_password"

{
  printf '%s\n' 'SBM_STORAGE_TYPE=bind'
  printf 'SBM_POSTGRES_DATA_SOURCE=%s\n' "$postgres_data_directory"
  printf 'SBM_OBJECTS_SOURCE=%s\n' "$objects_directory"
  printf 'SBM_BACKUPS_DIRECTORY=%s\n' "$backups_directory"
  printf '%s\n' 'SBM_COMPOSE_PROJECT_NAME=smart-bill-manager'
  printf '%s\n' 'SBM_DEPLOYMENT_MODE=local'
  printf '%s\n' 'SBM_COOKIE_SECURE=false'
  printf '%s\n' 'SBM_BIND_ADDRESS=127.0.0.1'
  printf 'SBM_HTTP_PORT=%s\n' "$http_port"
  printf '%s\n' 'SBM_SESSION_TTL=168h'
  printf '%s\n' 'SBM_AI_CONCURRENCY=2'
  printf 'SBM_MASTER_KEY_SOURCE=%s\n' "$master_key"
  printf 'SBM_POSTGRES_ADMIN_PASSWORD_SOURCE=%s\n' "$postgres_admin_password"
  printf 'SBM_POSTGRES_MIGRATION_PASSWORD_SOURCE=%s\n' "$postgres_migration_password"
  printf 'SBM_POSTGRES_RUNTIME_PASSWORD_SOURCE=%s\n' "$postgres_runtime_password"
  printf 'SBM_OWNER_PASSWORD_SOURCE=%s\n' "$owner_password"
} >"$environment_file"
chmod 0600 "$environment_file"

trap - EXIT HUP INT TERM
printf '%s\n' "deployment files created with owner-only permissions"
printf '%s\n' "PostgreSQL data: ${postgres_data_directory}"
printf '%s\n' "objects: ${objects_directory}"
printf '%s\n' "backups: ${backups_directory}"
printf '%s\n' "record the Owner password from ${owner_password} before bootstrap; bootstrap deletes that file after success"
