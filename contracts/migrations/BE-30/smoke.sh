#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname -- "${BASH_SOURCE[0]}")/../../../backend"
export BE30_INTEGRATION_TEST_DSN="${BE30_INTEGRATION_TEST_DSN:-${OH_TEST_PG_DSN:?Set BE30_INTEGRATION_TEST_DSN to a disposable loopback job_test URI}}"
go build ./cmd/omnihive-retire ./internal/legacy/retirement
go vet ./cmd/omnihive-retire ./internal/legacy/retirement
go test -count=1 ./cmd/omnihive-retire ./internal/legacy/retirement ./migrations -run 'Test(CatalogAndReviewOrder|ProjectionRequires|MigrationFilesUseUniqueNumericPrefixes|RetireCommand)'
go test -tags=integration -run '^TestRetirementOnIsolatedPostgres$' -v -count=1 ./cmd/omnihive-retire
