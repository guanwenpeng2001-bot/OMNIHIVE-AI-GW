#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../../../backend"
: "${QUOTES_POSTGRES_DSN:?Set a disposable BE-07 PostgreSQL URI}"
: "${QUOTES_REDIS_ADDR:?Set an isolated Redis address}"
go test -tags=integration -count=1 -run '^TestBE07' ./internal/control/catalog/compare
go test -tags=unit -count=1 -run '^TestBE07' ./internal/service
