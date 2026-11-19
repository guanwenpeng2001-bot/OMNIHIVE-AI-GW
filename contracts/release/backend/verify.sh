#!/usr/bin/env bash
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd -- "$here/../../.." && pwd)

if [[ ${1-} == --finance && $# == 1 ]]; then
  exec "$here/full.sh" --finance
fi
if [[ ${1-} == --full ]]; then
  shift
  if (( $# > 1 )); then echo 'Usage: verify.sh --full [load-seconds]' >&2; exit 2; fi
  exec "$here/full.sh" "$@"
fi
if (( $# != 0 )); then
  echo 'Usage: verify.sh [--full [load-seconds] | --finance]' >&2
  exit 2
fi

printf 'revision %s\n' "$(git -C "$root" rev-parse HEAD)"
echo '[read-only checkout]'
failed=0
if "$here/check-openapi.sh"; then
  echo 'pass OPENAPI — composed bytes match'
else
  echo 'fail OPENAPI — compose or byte comparison failed (see output above)'
  failed=1
fi
if ! python3 "$here/check-journey.py"; then
  failed=1
fi

echo '[isolated product verification — disposable PostgreSQL and Redis]'
echo 'pending HTTP — run with the integrated candidate and local deterministic upstream'
echo 'pending MATRIX — enumerate supported and rejected candidate tuples'
echo 'pending FINANCE — inject faults around the durable receipt boundary'
echo 'pending ACCESS — exercise scopes, projections, cache and revocation'
echo 'pending MIGRATION — run mixed-version cutover and isolated restore'
echo 'pending LOAD — measure paired baseline and candidate for two hours'
exit "$failed"
