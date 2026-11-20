#!/usr/bin/env bash
# Run `go test -json` and fail on failures, unexpected skips and -run no-ops.
#
#   scripts/go-test-strict.sh -tags=unit -count=1 ./...
#   GO_TEST_STRICT_PROFILE=integration scripts/go-test-strict.sh -tags=integration ./...
#
# Every `Test*` skip must either be impossible in the current environment (the
# CI jobs provide the full PostgreSQL/Redis DSN matrix) or be a documented
# OS/structural skip listed in scripts/test_go_test_strict.py. Database, Redis,
# DSN and integration skips are never allowlisted.
#
# When `-run` is supplied the gate additionally requires at least one executed
# test and rejects `[no test files]` / `testing: warning: no tests to run`, so a
# missing build tag can no longer turn a targeted contract check into a no-op.
#
# Profiles (GO_TEST_STRICT_PROFILE): unit (default), integration, e2e, local,
# contracts. Extra exact test names can be added with
# GO_TEST_STRICT_EXTRA_ALLOW="TestA TestB".
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECKER="${SCRIPT_DIR}/test_go_test_strict.py"

command -v go >/dev/null 2>&1 || {
  echo "go-test-strict: 'go' is required in PATH" >&2
  exit 1
}
command -v python3 >/dev/null 2>&1 || {
  echo "go-test-strict: 'python3' is required in PATH" >&2
  exit 1
}
[[ -f "$CHECKER" ]] || {
  echo "go-test-strict: missing checker ${CHECKER}" >&2
  exit 1
}

RUN_PRESENT=0
for arg in "$@"; do
  case "$arg" in
    -run|--run|-run=*|--run=*) RUN_PRESENT=1 ;;
  esac
done

# Collect the stream without `-v`: -json already carries test-level output.
set +e
go test -json "$@" | python3 "$CHECKER" \
  --check \
  --profile "${GO_TEST_STRICT_PROFILE:-unit}" \
  --run-present "$RUN_PRESENT" \
  --extra-allow "${GO_TEST_STRICT_EXTRA_ALLOW:-}"
statuses=("${PIPESTATUS[@]}")
set -e

go_status="${statuses[0]:-1}"
checker_status="${statuses[1]:-1}"

if (( go_status != 0 )); then
  echo "go-test-strict: go test exited ${go_status}" >&2
fi
if (( checker_status != 0 )); then
  echo "go-test-strict: skip/coverage gate rejected the run" >&2
fi
if (( go_status != 0 || checker_status != 0 )); then
  exit 1
fi

exit 0
