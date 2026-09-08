#!/usr/bin/env bash
# 生成 apps/api 的 PostgreSQL 集成测试所需配置。
# 密码文件必须是仅属主可读的普通文件，否则适配器会拒绝加载，所以先设 umask。
set -euo pipefail

: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${CI_POSTGRES_PASSWORD:?CI_POSTGRES_PASSWORD is required}"

umask 077
password_file="${RUNNER_TEMP}/postgres-password"
printf '%s' "${CI_POSTGRES_PASSWORD}" >"${password_file}"
printf '{"host":"127.0.0.1","port":5432,"admin_user":"postgres","password_file":"%s"}\n' \
  "${password_file}" >"${RUNNER_TEMP}/postgres-test.json"
