# OmniHive quality gate

Baseline: `codex/refactor` at `2637e9e59`, recorded on 2026-11-19.

## Run it

From the repository root:

```sh
scripts/quality-gate.sh                 # every stage
scripts/quality-gate.sh backend         # one stage
scripts/quality-gate.sh frontend contracts
scripts/quality-gate.sh --list
```

Each stage is independent and idempotent. Every run writes a separate log
directory under `.quality-gate/logs/<UTC run id>/`, updates
`.quality-gate/logs/latest`, and prints a `PASS` or `FAIL` line with the elapsed
seconds and log path. `.quality-gate/` is git-ignored.

`scripts/quality-gate.sh backend` accepts a disposable PostgreSQL URL through
`QUALITY_GATE_DATABASE_DSN` and its paired stats database through
`QUALITY_GATE_STATS_DSN`. `QUALITY_GATE_REDIS_ADDR` supplies an isolated Redis.
Without those variables the gate downloads a pinned PostgreSQL 16 and Redis 7
into `.quality-gate/tools/`, starts them on free loopback ports, creates
`job_test`/`stats_test`/`backup_data_test`, and stops them on exit. It never
connects to a shared database. PostgreSQL sessions are pinned to `TimeZone=UTC`
because migration tests compare timezone-sensitive historical values.

The frontend dependency tree is the git-excluded `frontend/node_modules`
symlink. The gate does not reinstall it when Vitest is already present.

## Stages

| Stage | Coverage |
| --- | --- |
| `frontend` | `check:i18n`, `typecheck`, `lint:check`, `test:run`, production `build` |
| `backend` | `go build ./...`, `go build -tags=embed ./cmd/server`, `go vet ./...`, `go test ./...`, `go test -tags=unit ./...`, then `go test -p 1 -tags=integration ./...` against disposable PostgreSQL 16 + Redis 7 |
| `contracts` | JSON validity for `contracts/protocol` and `contracts/api`; lifecycle, auth, journal and money fixture consuming tests |
| `generated-drift` | `go generate ./cmd/server` (Wire), `go run ./api/v2/compose`, pinned `openapi-typescript@7.10.1` for `v2.d.ts`; fails on any diff |
| `independence` | grep for case-insensitive `sub2api` in product source, deploy and CI; every line must match `docs/refactor/IDENTITY_ALLOWLIST.md` |
| `smoke` | starts the `embed` production binary on an isolated database/Redis with a loopback TLS fixture, then runs `contracts/migrations/*/smoke.sh` |

The independence scan excludes documentation, tests, historical migrations,
generated plugin descriptors, dependency locks and the allowlist itself. Tests,
historical migration text and documented compatibility evidence remain in the
repository but cannot introduce a new runtime identity.

## Baseline at 2637e9e59

Environment: WSL2 Ubuntu 26.04, 12 cores, Go 1.27.4, Node 24.19.0, pnpm 12.21.0.
The machine was shared with other refactor agents, so wall-clock times include
some scheduler pressure.

| Check | Result | Evidence |
| --- | --- | --- |
| `make test-shell` | PASS | All Linux-available shell/deploy tests pass. Apple container lifecycle is skipped because it requires macOS `stat`/`plutil`; recorded as a macOS-runner gap. |
| Frontend `check:i18n` | PASS | `<run>/frontend.log` |
| Frontend `typecheck` | PASS | `vue-tsc --noEmit` |
| Frontend `lint:check` | PASS | Complete ESLint tree |
| Frontend `test:run` | PASS | Full Vitest suite, six workers |
| Frontend `build` | PASS | Vite production build plus embedded assets |
| Backend `go build ./...` | PASS | Untagged build |
| Backend `go build -tags=embed ./cmd/server` | PASS | Release-style embedded build |
| Backend `go vet ./...` | PASS | Full tree |
| Backend `go test ./...` | PASS | Untagged tests |
| Backend `go test -tags=unit ./...` | PASS | Unit-tag matrix |
| Backend integration `-tags=integration ./...` | PASS | 432s against isolated PG16/Redis7 with `-p 1` |
| Contracts | PASS | 111s; protocol/API JSON plus consuming tests |
| Generated drift | PASS | 50s; Wire, composed OpenAPI and `v2.d.ts` reproduced byte-for-byte |
| Independence | PASS | 284 existing identifier lines covered; no new non-allowlisted occurrence |
| Smoke | PASS | 15s; BE-01, BE-04 and BE-06 pass; BE-05 and BE-09 skip for missing fixtures (below) |

Approximate sequential full-gate time: 35–40 minutes on this shared machine.
Backend unit time dominates; the integration run alone is 432s.

## Failures, ownership and disposition

No product-code failure remains at this baseline. The failures found while
establishing the gate were deterministic test-infrastructure defects.

### 1. Repository integration fixture cleanup — fixed (test harness)

Affected suites: `TestAccountRepoSuite`, `TestGroupRepoSuite`,
`TestUsageLogRepoSuite`, `TestUserRepoAPIKeyGroupFilterSuite`,
`TestUserRepoSuite`, `TestUserSubscriptionRepoSuite`,
`TestListSchedulableAccountLoadsMatchesListSchedulable`.

Minimal reproduction:

```sh
cd backend
UPSTREAM_JOB_TEST_DSN='postgres://…/job_test?...&TimeZone=UTC' \
SUB2API_TEST_EXTERNAL_SERVICES=1 AFFINITY_REDIS_TEST_ADDR=127.0.0.1:56379 \
go test -tags=integration -count=1 ./internal/repository
```

The clean-up order in `backend/internal/repository/integration_fixtures_test.go`
did not review the tables added by BE-01 identity and BE-04 connections:
`provider_connections`, `connection_credential_versions`,
`connection_migration_quarantine`, `project_grants` and `project_key_policies`.
The fixture correctly fails closed on an unreviewed foreign-key dependent, so
every suite using it failed. The test-only delete/review order now includes
those tables. Owner: repository integration harness / BE-01 + BE-04 schema.
Three consecutive runs of the package pass.

### 2. Migration upgrade timezone — fixed (gate environment)

Minimal reproduction without the gate:

```sh
cd backend
UPSTREAM_JOB_TEST_DSN='postgres://…/job_test?sslmode=disable' \
SUB2API_TEST_EXTERNAL_SERVICES=1 \
go test -tags=integration -count=1 -run '^TestUsageCoverageMigrationUpgrade$' ./migrations
```

`258_usage_dashboard_coverage.sql` resets the watermark with the date literal
`1970-01-01`; in an `Asia/Shanghai` session that is not the UTC epoch the test
compares against. The gate now appends `TimeZone=UTC` to every provided DSN.
Owner: test environment/migrations harness. Not a product failure. Three
consecutive runs pass with the gate DSN.

### 3. External Redis alias — fixed (gate environment)

Minimal reproduction:

```sh
cd backend
SUB2API_TEST_EXTERNAL_SERVICES=1 AFFINITY_REDIS_TEST_ADDR=127.0.0.1:56379 \
go test -tags=integration -count=1 -run '^TestCursorRealRedisSessionCache$' ./migrations
```

With external services enabled, the test validates `CURSOR_TEST_REDIS_ADDR`
before it checks whether the variable is empty. The gate now exports all Redis
aliases (`CURSOR`, `QUOTA`, `SECURITY`, `PROMPT_AUDIT`,
`SCHEDULER_RESOURCES`, `ACCOUNTING`) to the same isolated instance. Owner: test
environment. Three consecutive runs pass.

### 4. Smoke fixture gaps — recorded, not failed

| Script | Disposition | Reason |
| --- | --- | --- |
| `BE-01/smoke.sh` | PASS | Project key, replay, both gateway adapters, legacy scheduling |
| `BE-04/smoke.sh` | PASS | Connections, credential, verification, preview, activation, rotation, events |
| `BE-05/smoke.sh` | SKIP | Requires a catalog fixture (visible account/channel/group/model). A fresh isolated database has zero catalog items; the gate enables the plaza but does not fabricate product data. |
| `BE-06/smoke.sh` | PASS | The isolated server has no upstream, so the script runs with `SMOKE_EXPECT_STATUS=503`: original 503, repeat 204, conflict 409, execution ID retained |
| `BE-09/smoke.sh` | SKIP | Requires `SMOKE_BACKUP_ID` for an existing completed fixture backup; no backup job is available on a fresh server |

The gate also installs a pinned portable `jq` for the smoke scripts.

### 5. Platform-only gap

`make test-shell` includes Apple container lifecycle tests. On Linux the
Makefile target reports the macOS-only checks as skipped. A macOS runner remains
required for that portion of the shell job.

## Flaky classification

Every failure above reproduced deterministically before its harness fix (not a
timing or ordering flake). After the fixes:

- `TestUsageCoverageMigrationUpgrade` and
  `TestCursorRealRedisSessionCache`: 3/3 passes with `-count=3`.
- `./internal/repository` integration package, including all formerly failing
  suites: 3/3 passes with `-count=3` (51s).
- Full `-tags=integration ./...`: pass.

No test is currently marked flaky. An earlier `oh-remote` trial that pointed
every specialized `*_DSN` at `job_test` produced environment-only failures in
`TestCredentialDraftExistingAccountConfirmationIsBoundAndDoesNotCreate`,
`TestBillingBacklogPostgres` and scheduler/model-access suites; those variables
are outside the CI contract and are not part of this baseline.

## CI alignment

`.github/workflows/backend-ci.yml` now:

- verifies the two gate scripts with `bash -n`;
- runs backend CI on PostgreSQL 16 with a Redis 7 service and the same external
  service variables as the gate;
- performs `go build ./...` and `go vet ./...` before unit/integration tests;
- builds the frontend production assets after the regression/coverage run;
- adds a `quality-gate` job for contracts, generated drift and independence.

The `required-round` PostgreSQL/Redis matrices and the macOS shell runner remain
unchanged.
