#!/usr/bin/env bash
set -euo pipefail
: "${UPSTREAM_JOB_TEST_DSN:?Set the disposable local job_test PostgreSQL URI}"
: "${AFFINITY_REDIS_TEST_ADDR:?Set the disposable Redis address}"
cd "$(dirname "$0")/../../../backend"
go test -tags=integration -p=1 -count=1 -run '^TestImageTaskCenter' ./internal/modules/tasks ./internal/control/tasks ./internal/handler
