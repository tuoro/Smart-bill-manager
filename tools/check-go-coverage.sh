#!/usr/bin/env bash
set -euo pipefail

TOOL_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_DIR=$(cd -- "${TOOL_DIR}/.." && pwd)
API_DIR="${PROJECT_DIR}/apps/api"
GO_COMMAND="${GO_COMMAND:-go}"

if [[ ! -x "${GO_COMMAND}" ]] && ! command -v "${GO_COMMAND}" >/dev/null 2>&1; then
  echo "Go executable not found: ${GO_COMMAND}" >&2
  exit 2
fi

TEMP_DIR=$(mktemp -d)
trap 'rm -rf -- "${TEMP_DIR}"' EXIT

calculate_unique_statement_coverage() {
  local profile=$1
  awk '
    NR == 1 {
      if ($0 !~ /^mode: /) {
        print "invalid Go coverage profile" > "/dev/stderr"
        exit 2
      }
      next
    }
    {
      block = $1
      statements_in_block = $2 + 0
      execution_count = $3 + 0
      if (block in statement_count && statement_count[block] != statements_in_block) {
        print "inconsistent duplicate coverage block: " block > "/dev/stderr"
        exit 2
      }
      statement_count[block] = statements_in_block
      if (execution_count > 0) {
        covered_block[block] = 1
      }
    }
    END {
      total = 0
      covered = 0
      for (block in statement_count) {
        total += statement_count[block]
        if (covered_block[block]) {
          covered += statement_count[block]
        }
      }
      if (total == 0) {
        print "coverage profile contains no statements" > "/dev/stderr"
        exit 2
      }
      printf "%.2f %d %d\n", covered * 100 / total, covered, total
    }
  ' "${profile}"
}

check_layer() {
  local layer=$1
  local cover_packages=$2
  local threshold=$3
  local profile="${TEMP_DIR}/${layer}.cover"

  (
    cd -- "${API_DIR}"
    "${GO_COMMAND}" test -count=1 -covermode=atomic -coverpkg="${cover_packages}" -coverprofile="${profile}" ./...
  )

  local stats
  stats=$(calculate_unique_statement_coverage "${profile}")
  local percentage covered total
  read -r percentage covered total <<<"${stats}"
  printf '%s: %s%% (%s/%s statements), required >= %s%%\n' \
    "${layer}" "${percentage}" "${covered}" "${total}" "${threshold}"

  awk -v actual="${percentage}" -v required="${threshold}" \
    'BEGIN { if (actual + 0 < required + 0) exit 1 }'
}

echo "Coverage exclusions:"
sed -n '/^[[:space:]]*#/!{/^[[:space:]]*$/!p;}' "${TOOL_DIR}/coverage-exclusions.txt"

check_layer "domain-application" "./internal/domain/...,./internal/application/..." "85"
check_layer "infrastructure-transport" "./internal/adapters/...,./internal/transport/..." "70"
