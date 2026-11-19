#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
backend="$root/backend"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/be24a-openapi.XXXXXXXX")
trap 'rm -rf -- "$scratch"' EXIT

mkdir -p "$scratch/backend/api/v2"
cp -- "$backend/go.mod" "$backend/go.sum" "$scratch/backend/"
cp -R -- "$backend/api/v2/compose" "$backend/api/v2/fragments" "$scratch/backend/api/v2/"
(cd "$scratch/backend" && GOWORK=off go run ./api/v2/compose)
cmp -- "$backend/api/v2/openapi.yaml" "$scratch/backend/api/v2/openapi.yaml"
