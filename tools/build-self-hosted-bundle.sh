#!/bin/sh
set -eu

usage() {
  printf '%s\n' "usage: $0 /absolute/new/smart-bill-manager-docker.tar.gz" >&2
  exit 2
}

[ "$#" -eq 1 ] || usage
output=$1
case "$output" in
  /*.tar.gz) ;;
  *) usage ;;
esac

output_parent=$(dirname -- "$output")
[ -d "$output_parent" ] && [ ! -L "$output_parent" ] || {
  printf '%s\n' "bundle output parent must be an existing regular directory" >&2
  exit 1
}
output_parent=$(CDPATH= cd -- "$output_parent" && pwd -P)
output=${output_parent}/$(basename -- "$output")
[ ! -e "$output" ] && [ ! -L "$output" ] || {
  printf '%s\n' "bundle output must not already exist" >&2
  exit 1
}
checksum=${output}.sha256
[ ! -e "$checksum" ] && [ ! -L "$checksum" ] || {
  printf '%s\n' "bundle checksum output must not already exist" >&2
  exit 1
}

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
repository_root=$(dirname -- "$script_directory")
staging=$(mktemp -d "${output_parent}/.sbm-bundle.XXXXXX")
bundle_root=${staging}/smart-bill-manager-docker

cleanup() {
  case "$staging" in
    "${output_parent}"/.sbm-bundle.*) rm -rf -- "$staging" ;;
  esac
}
trap cleanup EXIT HUP INT TERM

mkdir -p -- "$bundle_root/docs" "$bundle_root/infra/compose" "$bundle_root/tools"

for file in README.md README_EN.md LICENSE; do
  cp -- "${repository_root}/${file}" "${bundle_root}/${file}"
done
for file in deployment.md backup-restore.md local-operations.md; do
  cp -- "${repository_root}/docs/${file}" "${bundle_root}/docs/${file}"
done
for file in compose.yaml compose.release.yaml compose.bootstrap.yaml release.env; do
  cp -- "${repository_root}/infra/compose/${file}" "${bundle_root}/infra/compose/${file}"
done
for file in prepare-self-hosted-deployment.sh sbm-deploy.sh; do
  cp -- "${repository_root}/tools/${file}" "${bundle_root}/tools/${file}"
  chmod 0755 "${bundle_root}/tools/${file}"
done

tar \
  --sort=name \
  --mtime='UTC 1970-01-01' \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  -C "$staging" \
  -czf "$output" \
  smart-bill-manager-docker
chmod 0644 "$output"
digest=$(sha256sum "$output" | awk '{print $1}')
printf '%s  %s\n' "$digest" "$(basename -- "$output")" >"$checksum"
chmod 0644 "$checksum"

trap - EXIT HUP INT TERM
cleanup
printf '%s\n' "self-hosted Docker bundle and checksum created"
