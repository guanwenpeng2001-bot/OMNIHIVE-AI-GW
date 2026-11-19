#!/usr/bin/env bash
set -euo pipefail

# The integration fixture creates and drops its own databases inside this
# disposable postgres:16-alpine container. Supply its loopback job_test URI.
: "${BE23_INTEGRATION_TEST_DSN:?Set the disposable PG16 job_test DSN}"

contracts_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT

cd "$contracts_dir/../../../backend"
export BE23_SMOKE_BINARY="$scratch/omnihive-refactor"
export BE23_SMOKE_REPORT_DIR="$scratch"
go build -o "$BE23_SMOKE_BINARY" ./cmd/omnihivectl/refactor/standalone
go test -tags=integration -count=1 ./cmd/omnihivectl/refactor

python3 - "$contracts_dir/report.schema.json" "$scratch" <<'PY'
import json
import sys
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker

schema = json.loads(Path(sys.argv[1]).read_text())
Draft202012Validator.check_schema(schema)
validator = Draft202012Validator(schema, format_checker=FormatChecker())
reports = sorted(Path(sys.argv[2]).glob("*.json"))
if len(reports) != 7:
    raise SystemExit(f"Expected seven CLI reports, got {len(reports)}")
for report in reports:
    validator.validate(json.loads(report.read_text()))
    print(f"Validated {report.name}")
PY
