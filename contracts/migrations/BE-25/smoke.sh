#!/usr/bin/env bash
set -euo pipefail
: "${UPSTREAM_JOB_TEST_DSN:?Set the disposable job_test PostgreSQL URI}"
: "${AFFINITY_REDIS_TEST_ADDR:?Set the disposable Redis address}"
cd "$(dirname "$0")/../../../backend"
go build ./internal/control/tasks ./internal/modules/tasks/... ./internal/providers/mediajobs ./internal/handler
go vet ./internal/repository ./internal/control/tasks ./internal/modules/tasks/... ./internal/providers/mediajobs ./internal/handler ./internal/service
go test -count=1 ./internal/modules/tasks/media ./internal/providers/mediajobs
go test -tags=unit -p=1 -count=1 -run '^Test(MediaTask|Qianwen(RealtimeDurablePerTurnIdempotencyAndRestart|AudioSpeechRequest|AudioCompatForward|VideoOfficialUsageAndTariff|VideoCompatCancel|VideoTransportSubmitPoll))' ./internal/handler ./internal/service
go test -tags=integration -p=1 -count=1 -run '^Test(ImageTaskCenter|MediaTask)' ./internal/modules/tasks ./internal/control/tasks ./internal/handler
