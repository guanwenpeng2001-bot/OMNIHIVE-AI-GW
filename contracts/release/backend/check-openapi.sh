#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
root=${BE24_PRODUCT_ROOT:-$root}
backend="$root/backend"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/be24a-openapi.XXXXXXXX")
trap 'rm -rf -- "$scratch"' EXIT

mkdir -p "$scratch/backend/api/openapi"
cp -- "$backend/go.mod" "$backend/go.sum" "$scratch/backend/"
cp -R -- "$backend/api/openapi/compose" "$backend/api/openapi/fragments" "$scratch/backend/api/openapi/"
(cd "$scratch/backend" && GOWORK=off go run ./api/openapi/compose)
cmp -- "$backend/api/openapi/openapi.yaml" "$scratch/backend/api/openapi/openapi.yaml"
