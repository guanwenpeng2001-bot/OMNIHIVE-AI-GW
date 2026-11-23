#!/usr/bin/env bash
set -euo pipefail
# One artifact policy for both release configurations. This does not set DB policy.
version=${1:?release version required}
commit=${2:?release commit required}
output=${3:-.cache/omnihive-release/omnihive-release-manifest.json}
[[ "$version" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?$ ]] || exit 1
[[ "$commit" =~ ^[0-9a-fA-F]{7,64}$ ]] || exit 1
mkdir -p "$(dirname "$output")"
printf '{"product":"omnihive","version":"%s","minimum_rollback_version":"0.3.0","commit":"%s"}\n' "${version#v}" "$commit" > "$output"
