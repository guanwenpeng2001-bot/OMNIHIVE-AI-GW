#!/usr/bin/env bash
set -euo pipefail

if [[ -z ${SMOKE_BASE_URL:-} ]]; then
  : "${UPSTREAM_JOB_TEST_DSN:?Set the dedicated job_test PostgreSQL URI}"
  : "${AFFINITY_REDIS_TEST_ADDR:?Set the isolated Redis host and port}"
  backend=$(cd "$(dirname "$0")/../../../backend" && pwd)
  if [[ -n ${EXECUTION_TEST_BIN_DIR:-} ]]; then
    bins=$(cd "$EXECUTION_TEST_BIN_DIR" && pwd)
    (cd "$backend/internal/gateway/execution/journal" && "$bins/journal" -test.count=1)
    cd "$backend/internal/handler"
    exec "$bins/handler" -test.count=1 -test.run='^TestJournal'
  fi
  cd "$backend"
  go test -tags=integration -count=1 ./internal/gateway/execution/journal
  exec go test -tags='integration unit' -count=1 -run '^TestJournal' ./internal/handler
fi

: "${SMOKE_API_KEY:?Set a gateway key for the smoke instance}"
: "${SMOKE_MODEL:?Set the model available on that instance}"
export SMOKE_BASE_URL SMOKE_API_KEY SMOKE_MODEL
exec python3 - <<'PY'
import json
import os
from urllib.error import HTTPError
from urllib.request import Request, urlopen
from uuid import UUID, uuid4

url = os.environ["SMOKE_BASE_URL"].rstrip("/") + "/v1/responses"
headers = {
    "Authorization": "Bearer " + os.environ["SMOKE_API_KEY"],
    "Content-Type": "application/json",
    "Idempotency-Key": str(uuid4()),
}

def post(prompt):
    payload = {"model": os.environ["SMOKE_MODEL"], "input": prompt, "stream": False}
    request = Request(url, json.dumps(payload).encode(), headers, method="POST")
    try:
        response = urlopen(request, timeout=60)
    except HTTPError as error:
        response = error
    with response:
        return response.status, response.headers, response.read()

status, first, body = post("Reply OK.")
expected = int(os.environ.get("SMOKE_EXPECT_STATUS", "200"))
if status != expected:
    raise SystemExit(f"Expected original native status {expected}, got {status}")
native = json.loads(body)
if (status == 200 and native.get("object") != "response") or (status >= 400 and "error" not in native):
    raise SystemExit("Unexpected native Responses body")
execution = first.get("X-Execution-ID")
UUID(execution or "")
status, replay, body = post("Reply OK.")
if status != 204 or body or replay.get("X-Execution-ID") != execution or replay.get("X-Idempotency-Replayed") != "true":
    raise SystemExit("The repeat did not acknowledge the original execution")
status, _, body = post("Reply YES.")
if status != 409 or json.loads(body).get("error", {}).get("type") != "invalid_request_error":
    raise SystemExit("Expected a native idempotency conflict")
print(f"Original {expected}, repeat 204, conflict 409. Execution {execution}")
PY
