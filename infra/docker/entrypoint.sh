#!/bin/sh
set -eu

fail() {
  printf '%s\n' "entrypoint: $1" >&2
  exit 1
}

source_file=/run/secrets/sbm_master_key
target_dir=/run/sbm-secrets
target_file=${target_dir}/master-key

[ "$#" -gt 0 ] || fail command_required

needs_master=false
needs_objects=false
case "$1" in
  /app/provision-postgresql|/app/migrate|/app/recover-account)
    ;;
  /app/server|/app/bootstrap-owner|/app/backup)
    needs_master=true
    needs_objects=true
    ;;
  *)
    needs_master=true
    needs_objects=true
    ;;
esac

[ ! -L "$target_dir" ] || fail secret_target_invalid
mkdir -p "$target_dir" || fail secret_target_unavailable
[ -d "$target_dir" ] || fail secret_target_invalid
chown root:sbm "$target_dir" || fail secret_target_permissions
chmod 0710 "$target_dir" || fail secret_target_permissions
[ "$(stat -c '%a:%u:%g' "$target_dir" 2>/dev/null || true)" = "710:0:10001" ] || fail secret_target_permissions

if [ "$needs_objects" = true ]; then
  data_dir=/var/lib/sbm/objects
  [ ! -L "$data_dir" ] || fail data_directory_invalid
  mkdir -p "$data_dir" || fail data_directory_unavailable
  [ -d "$data_dir" ] || fail data_directory_invalid
  chown root:root "$data_dir" || fail data_directory_permissions
  chmod 0700 "$data_dir" || fail data_directory_permissions
  chown sbm:sbm "$data_dir" || fail data_directory_permissions
fi

if [ "$needs_master" = true ]; then
  [ ! -L "$source_file" ] || fail master_key_source_invalid
  [ -f "$source_file" ] || fail master_key_source_invalid
  [ "$(stat -c '%h' "$source_file" 2>/dev/null || true)" = "1" ] || fail master_key_source_invalid

  source_mode=$(stat -c '%a' "$source_file" 2>/dev/null || true)
  case "$source_mode" in
    ''|*[!0-7]*) fail master_key_source_invalid ;;
  esac
  [ $((0$source_mode & 077)) -eq 0 ] || fail master_key_source_permissions

if [ -e "$target_file" ] || [ -L "$target_file" ]; then
  [ ! -L "$target_file" ] || fail master_key_target_invalid
  [ -f "$target_file" ] || fail master_key_target_invalid
  [ "$(stat -c '%h' "$target_file" 2>/dev/null || true)" = "1" ] || fail master_key_target_invalid
fi

candidate=$(mktemp "${target_dir}/master-key.tmp.XXXXXX") || fail master_key_target_unavailable
cleanup() {
  rm -f "$candidate"
}
trap cleanup EXIT HUP INT TERM
chmod 0600 "$candidate" || fail master_key_target_permissions
dd if="$source_file" of="$candidate" bs=129 count=1 2>/dev/null || fail master_key_source_unreadable

byte_count=$(wc -c < "$candidate" | tr -d ' ')
case "$byte_count" in
  ''|*[!0-9]*) fail master_key_format_invalid ;;
esac
[ "$byte_count" -gt 0 ] && [ "$byte_count" -le 128 ] || fail master_key_format_invalid

normalized_count=$byte_count
last_byte=$(od -An -tu1 -j $((byte_count - 1)) -N 1 "$candidate" | tr -d ' ')
# 与 Go 读取器一致：恰好 32 字节时每一字节均为原始密钥，不截断 CR/LF。
if [ "$byte_count" -ne 32 ] && [ "$last_byte" = "10" ]; then
  normalized_count=$((normalized_count - 1))
  if [ "$normalized_count" -gt 0 ]; then
    previous_byte=$(od -An -tu1 -j $((normalized_count - 1)) -N 1 "$candidate" | tr -d ' ')
    [ "$previous_byte" != "13" ] || normalized_count=$((normalized_count - 1))
  fi
fi

case "$normalized_count" in
  32)
    ;;
  64)
    head -c 64 "$candidate" | LC_ALL=C grep -Eq '^[0-9A-Fa-f]{64}$' || fail master_key_format_invalid
    ;;
  44)
    head -c 44 "$candidate" | LC_ALL=C grep -Eq '^[A-Za-z0-9+/]{43}=$' || fail master_key_format_invalid
    ;;
  *)
    fail master_key_format_invalid
    ;;
esac

chmod 0600 "$candidate" || fail master_key_target_permissions
chown sbm:sbm "$candidate" || fail master_key_target_permissions
mv -f "$candidate" "$target_file" || fail master_key_target_unavailable
candidate=

[ ! -L "$target_file" ] || fail master_key_target_invalid
[ -f "$target_file" ] || fail master_key_target_invalid
[ "$(stat -c '%h:%a:%u:%g' "$target_file" 2>/dev/null || true)" = "1:600:10001:10001" ] || fail master_key_target_permissions

trap - EXIT HUP INT TERM
fi

materialize_secret() {
  database_source=$1
  database_target=$2
  database_label=$3
  database_maximum=${4:-1025}

  [ ! -L "$database_source" ] || fail "${database_label}_source_invalid"
  [ -f "$database_source" ] || fail "${database_label}_source_invalid"
  [ "$(stat -c '%h' "$database_source" 2>/dev/null || true)" = "1" ] || fail "${database_label}_source_invalid"
  database_mode=$(stat -c '%a' "$database_source" 2>/dev/null || true)
  case "$database_mode" in
    ''|*[!0-7]*) fail "${database_label}_source_invalid" ;;
  esac
  [ $((0$database_mode & 077)) -eq 0 ] || fail "${database_label}_source_permissions"

  database_candidate=$(mktemp "${target_dir}/${database_label}.tmp.XXXXXX") || fail "${database_label}_target_unavailable"
  chmod 0600 "$database_candidate" || fail "${database_label}_target_permissions"
  dd if="$database_source" of="$database_candidate" bs=$((database_maximum + 1)) count=1 2>/dev/null || fail "${database_label}_source_unreadable"
  database_byte_count=$(wc -c < "$database_candidate" | tr -d ' ')
  case "$database_byte_count" in
    ''|*[!0-9]*) fail "${database_label}_format_invalid" ;;
  esac
  [ "$database_byte_count" -gt 0 ] && [ "$database_byte_count" -le "$database_maximum" ] || fail "${database_label}_format_invalid"
  chmod 0600 "$database_candidate" || fail "${database_label}_target_permissions"
  chown sbm:sbm "$database_candidate" || fail "${database_label}_target_permissions"
  mv -f "$database_candidate" "$database_target" || fail "${database_label}_target_unavailable"
  [ "$(stat -c '%h:%a:%u:%g' "$database_target" 2>/dev/null || true)" = "1:600:10001:10001" ] || fail "${database_label}_target_permissions"
}

case "$1" in
  /app/provision-postgresql)
    materialize_secret /run/secrets/sbm_postgres_admin_password "${target_dir}/postgres-admin-password" postgres-admin-password
    materialize_secret /run/secrets/sbm_postgres_migration_password "${target_dir}/postgres-migration-password" postgres-migration-password
    materialize_secret /run/secrets/sbm_postgres_runtime_password "${target_dir}/postgres-runtime-password" postgres-runtime-password
    ;;
  /app/migrate)
    materialize_secret /run/secrets/sbm_postgres_migration_password "${target_dir}/postgres-migration-password" postgres-migration-password
    ;;
  /app/server|/app/bootstrap-owner|/app/recover-account)
    materialize_secret /run/secrets/sbm_postgres_runtime_password "${target_dir}/postgres-runtime-password" postgres-runtime-password
    ;;
  /app/backup)
    materialize_secret /run/secrets/sbm_postgres_runtime_password "${target_dir}/postgres-runtime-password" postgres-runtime-password
    materialize_secret /run/secrets/sbm_postgres_migration_password "${target_dir}/postgres-migration-password" postgres-migration-password
    ;;
  *)
    materialize_secret /run/secrets/sbm_postgres_runtime_password "${target_dir}/postgres-runtime-password" postgres-runtime-password
    ;;
esac

if [ "$1" = "/app/recover-account" ] && { [ -e /run/secrets/sbm_account_recovery_input ] || [ -L /run/secrets/sbm_account_recovery_input ]; }; then
  materialize_secret /run/secrets/sbm_account_recovery_input "${target_dir}/account-recovery-input" account-recovery-input 8192
fi

if [ "$1" = "/app/bootstrap-owner" ]; then
  owner_source=/run/secrets/sbm_owner_password
  owner_target=${target_dir}/owner-password

  [ ! -L "$owner_source" ] || fail owner_password_source_invalid
  [ -f "$owner_source" ] || fail owner_password_source_invalid
  [ "$(stat -c '%h' "$owner_source" 2>/dev/null || true)" = "1" ] || fail owner_password_source_invalid

  owner_mode=$(stat -c '%a' "$owner_source" 2>/dev/null || true)
  case "$owner_mode" in
    ''|*[!0-7]*) fail owner_password_source_invalid ;;
  esac
  [ $((0$owner_mode & 077)) -eq 0 ] || fail owner_password_source_permissions

  if [ -e "$owner_target" ] || [ -L "$owner_target" ]; then
    [ ! -L "$owner_target" ] || fail owner_password_target_invalid
    [ -f "$owner_target" ] || fail owner_password_target_invalid
    [ "$(stat -c '%h' "$owner_target" 2>/dev/null || true)" = "1" ] || fail owner_password_target_invalid
  fi

  owner_candidate=$(mktemp "${target_dir}/owner-password.tmp.XXXXXX") || fail owner_password_target_unavailable
  cleanup_owner() {
    rm -f "$owner_candidate"
  }
  trap cleanup_owner EXIT HUP INT TERM
  chmod 0600 "$owner_candidate" || fail owner_password_target_permissions
  dd if="$owner_source" of="$owner_candidate" bs=1026 count=1 2>/dev/null || fail owner_password_source_unreadable

  owner_byte_count=$(wc -c < "$owner_candidate" | tr -d ' ')
  case "$owner_byte_count" in
    ''|*[!0-9]*) fail owner_password_format_invalid ;;
  esac
  [ "$owner_byte_count" -gt 0 ] && [ "$owner_byte_count" -le 1025 ] || fail owner_password_format_invalid

  chmod 0600 "$owner_candidate" || fail owner_password_target_permissions
  chown sbm:sbm "$owner_candidate" || fail owner_password_target_permissions
  mv -f "$owner_candidate" "$owner_target" || fail owner_password_target_unavailable
  owner_candidate=

  [ ! -L "$owner_target" ] || fail owner_password_target_invalid
  [ -f "$owner_target" ] || fail owner_password_target_invalid
  [ "$(stat -c '%h:%a:%u:%g' "$owner_target" 2>/dev/null || true)" = "1:600:10001:10001" ] || fail owner_password_target_permissions

  trap - EXIT HUP INT TERM
fi

exec /app/run-as-sbm "$@"
