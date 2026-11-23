#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
: "${BE8_RELEASE_CHECKER:?build omnihivectl and set BE8_RELEASE_CHECKER}"
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
manifest="$scratch/omnihive-release-manifest.json"
bash "$root/scripts/release-manifest.sh" 0.3.4 0123456789abcdef "$manifest"
grep -q '"minimum_rollback_version":"0.3.0"' "$manifest"
grep -q '"version":"0.3.4"' "$manifest"
grep -q '"commit":"0123456789abcdef"' "$manifest"
(cd "$scratch" && sha256sum omnihive-release-manifest.json > checksums.txt)
"$BE8_RELEASE_CHECKER" release check --target v0.3.4 --manifest "$manifest" --checksums "$scratch/checksums.txt" --fresh --json > "$scratch/result"
grep -q '"eligible":true' "$scratch/result"
grep -q '"effective_floor":"v0.3.0"' "$scratch/result"
for config in .goreleaser.yaml .goreleaser.simple.yaml; do
    grep -q 'bash scripts/release-manifest.sh' "$root/$config"
    if grep -q minimum_rollback_version "$root/$config"; then exit 1; fi
done
if bash "$root/scripts/release-manifest.sh" 'bad"version' abcdef0 "$manifest"; then exit 1; fi
if bash "$root/scripts/release-manifest.sh" 0.3.4 'bad"commit' "$manifest"; then exit 1; fi
echo 'PASS: shared manifest generation and CLI verification'
