#!/usr/bin/env bash
set -euo pipefail

TOOL_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_DIR=$(cd -- "${TOOL_DIR}/.." && pwd)
API_DIR="${PROJECT_DIR}/apps/api"
MATRIX="${PROJECT_DIR}/tests/critical-invariants.tsv"
GO_COMMAND="${GO_COMMAND:-go}"

if [[ ! -x "${GO_COMMAND}" ]] && ! command -v "${GO_COMMAND}" >/dev/null 2>&1; then
  echo "Go executable not found: ${GO_COMMAND}" >&2
  exit 2
fi
if [[ ! -f "${MATRIX}" ]]; then
  echo "Critical invariant matrix not found: ${MATRIX}" >&2
  exit 2
fi

declare -A EXECUTED_TESTS=()
TOTAL_BRANCHES=0
PASSED_BRANCHES=0

while IFS=$'\t' read -r invariant branch_case package test_name; do
  [[ -z "${invariant}" || "${invariant}" == \#* ]] && continue
  if [[ -z "${branch_case}" || -z "${package}" || -z "${test_name}" ]]; then
    echo "Invalid critical-invariant row for ${invariant}" >&2
    exit 2
  fi
  TOTAL_BRANCHES=$((TOTAL_BRANCHES + 1))
  key="${package}:${test_name}"
  if [[ -z "${EXECUTED_TESTS[${key}]+present}" ]]; then
    test_list=$(cd -- "${API_DIR}" && "${GO_COMMAND}" test "${package}" -list "^${test_name}$")
    if ! awk -v expected="${test_name}" '$0 == expected { found = 1 } END { exit !found }' <<<"${test_list}"; then
      echo "Mapped test does not exist: ${package} ${test_name}" >&2
      exit 1
    fi
    (
      cd -- "${API_DIR}"
      "${GO_COMMAND}" test -count=1 "${package}" -run "^${test_name}$"
    )
    EXECUTED_TESTS[${key}]=passed
  fi
  PASSED_BRANCHES=$((PASSED_BRANCHES + 1))
  printf 'PASS %-32s %s\n' "${invariant}" "${branch_case}"
done <"${MATRIX}"

if [[ ${TOTAL_BRANCHES} -eq 0 || ${PASSED_BRANCHES} -ne ${TOTAL_BRANCHES} ]]; then
  echo "Critical invariant gate failed: ${PASSED_BRANCHES}/${TOTAL_BRANCHES}" >&2
  exit 1
fi
printf 'Critical invariant logical branches: %d/%d (100%%)\n' "${PASSED_BRANCHES}" "${TOTAL_BRANCHES}"
