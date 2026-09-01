#!/bin/sh
set -eu

attempt=0
while [ "$attempt" -lt 120 ]; do
  if wget -q -O /dev/null http://127.0.0.1:19086/healthz; then
    exec /app/server
  fi
  attempt=$((attempt + 1))
  sleep 0.25
done

printf '%s\n' 'acceptance-start: synthetic_provider_not_ready' >&2
exit 1
