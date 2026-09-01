#!/bin/sh
set -eu

usage() {
  printf '%s\n' "usage: $0 /absolute/new/deployment-directory" >&2
  exit 2
}

[ "$#" -eq 1 ] || usage
deployment_directory=$1

case "$deployment_directory" in
  /*) ;;
  *) usage ;;
esac

case "$deployment_directory" in
  *[!A-Za-z0-9_./-]*)
    printf '%s\n' "deployment directory contains unsupported characters" >&2
    exit 2
    ;;
esac

[ "$deployment_directory" != "/" ] || {
  printf '%s\n' "deployment directory must not be the filesystem root" >&2
  exit 2
}

parent_directory=$(dirname -- "$deployment_directory")
deployment_name=$(basename -- "$deployment_directory")
case "$deployment_name" in
  ''|.|..)
    printf '%s\n' "deployment directory name is invalid" >&2
    exit 2
    ;;
esac
[ -d "$parent_directory" ] && [ ! -L "$parent_directory" ] || {
  printf '%s\n' "deployment directory parent must be an existing regular directory" >&2
  exit 1
}
parent_directory=$(CDPATH= cd -- "$parent_directory" && pwd -P)
deployment_directory=${parent_directory}/${deployment_name}

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
repository_root=$(dirname -- "$script_directory")
case "$deployment_directory" in
  "$repository_root"|"$repository_root"/*)
    printf '%s\n' "deployment directory must be outside the Git repository" >&2
    exit 2
    ;;
esac
[ ! -e "$deployment_directory" ] && [ ! -L "$deployment_directory" ] || {
  printf '%s\n' "deployment directory must not already exist" >&2
  exit 1
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
data_directory=${deployment_directory}/data
postgres_data_directory=${data_directory}/postgres
objects_directory=${data_directory}/objects
backups_directory=${deployment_directory}/backups

cleanup() {
  rm -f -- \
    "$master_key" \
    "$postgres_admin_password" \
    "$postgres_migration_password" \
    "$postgres_runtime_password" \
    "$owner_password" \
    "$environment_file"
  rmdir -- "$postgres_data_directory" 2>/dev/null || true
  rmdir -- "$objects_directory" 2>/dev/null || true
  rmdir -- "$data_directory" 2>/dev/null || true
  rmdir -- "$backups_directory" 2>/dev/null || true
  rmdir -- "$deployment_directory" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

mkdir -- "$data_directory" "$postgres_data_directory" "$objects_directory" "$backups_directory"
chmod 0700 "$data_directory" "$postgres_data_directory" "$objects_directory" "$backups_directory"

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
  printf '%s\n' 'SBM_COMPOSE_PROJECT_NAME=smart-bill-manager'
  printf '%s\n' 'SBM_DEPLOYMENT_MODE=local'
  printf '%s\n' 'SBM_COOKIE_SECURE=false'
  printf '%s\n' 'SBM_BIND_ADDRESS=127.0.0.1'
  printf '%s\n' 'SBM_HTTP_PORT=8080'
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
printf '%s\n' "persistent database, object, and backup directories are under ${deployment_directory}"
printf '%s\n' "record the Owner password from ${owner_password} before bootstrap; bootstrap deletes that file after success"
