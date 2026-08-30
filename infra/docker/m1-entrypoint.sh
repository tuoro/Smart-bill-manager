#!/bin/sh
set -eu

source_file="${SBM_MASTER_KEY_SOURCE_FILE:-/run/secrets/sbm_master_key}"
target_file="${SBM_MASTER_KEY_FILE:-/run/sbm-secrets/master-key}"
target_dir="$(dirname "$target_file")"

if [ ! -f "$source_file" ]; then
  echo "master key source file is required" >&2
  exit 1
fi

mkdir -p /var/lib/sbm/database /var/lib/sbm/objects "$target_dir"
cp "$source_file" "$target_file"
chown -R sbm:sbm /var/lib/sbm/database /var/lib/sbm/objects
chown sbm:sbm "$target_dir" "$target_file"
chmod 0700 /var/lib/sbm/database /var/lib/sbm/objects "$target_dir"
chmod 0600 "$target_file"

exec su-exec sbm:sbm "$@"
