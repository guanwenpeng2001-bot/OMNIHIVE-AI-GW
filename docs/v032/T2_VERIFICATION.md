# T2 verification

All final checks passed. Worktree: `w4-be-onboarding-probe`; branch: `codex/v032-be-onboarding-probe`. No T1-owned files were modified. The acceptance contract is also copied to `/home/administrator/Documents/Codex/2026-11-19/omnihive-refactor-workspace/acceptance/v032/CONTRACT_T2.md` as explicitly requested in the resumed task.

## Commands and results

Every test/vet invocation used the resource gate and environment below, from `backend/`. Packages were run sequentially, one package per invocation.

```bash
export GOCACHE=/home/administrator/Documents/Codex/2026-11-19/omnihive-refactor-workspace/acceptance/wave3/gocache
export GOTMPDIR=/home/administrator/Documents/Codex/2026-11-19/omnihive-refactor-workspace/acceptance/wave3/gotmp
export GOTOOLCHAIN=local GOFLAGS="-mod=readonly -buildvcs=false" GOMAXPROCS=3
heavy=/home/administrator/Documents/Codex/2026-11-19/omnihive-refactor-workspace/acceptance/wave3/bin/heavy
eval "$(/home/administrator/Documents/Codex/2026-11-19/omnihive-refactor-workspace/acceptance/wave3/bin/localdb env | sed s/55480/55494/g)"
export WORKSPACE_TEST_DSN="$UPSTREAM_JOB_TEST_DSN"
export REPOSITORY_INTEGRATION_TEST_DSN="$UPSTREAM_JOB_TEST_DSN"
```

| Exact command (`$heavy` expands to the absolute wrapper above) | Final result |
| --- | --- |
| `$heavy go test -tags=unit ./internal/config -count=1` | PASS |
| `$heavy go test -tags=unit ./internal/util/urlvalidator -count=1` | PASS |
| `$heavy go test -tags=unit ./internal/modules/connections -count=1` | PASS |
| `$heavy go test -tags=unit ./internal/modules/identity -count=1` | PASS |
| `$heavy go test -tags=unit ./internal/modules/identity/postgres/ -count=1` | PASS |
| `$heavy go test -tags=unit ./internal/control/connections -count=1` | Compiled; this package's tests require integration tag |
| `$heavy go test -tags=unit ./internal/control/identity -count=1` | PASS |
| `$heavy go test -tags=unit ./internal/setup -count=1` | PASS |
| `$heavy go test -tags=unit ./internal/repository -run TestUserRepository -count=1` | PASS |
| `$heavy go test -tags=unit ./internal/securityaudit -count=1` | PASS |
| `$heavy go test -tags=unit,integration ./internal/control/identity -count=1` | PASS, including new onboarding/RBAC/backfill tests |
| `$heavy go test -tags=unit,integration ./internal/control/connections -count=1` | PASS |
| `$heavy go test -tags=unit,integration ./internal/control/policies -count=1` | PASS |
| `$heavy go test -tags=unit,integration ./internal/control/billing -count=1` | PASS |
| `$heavy go test -tags=unit,integration ./internal/control/requests -count=1` | PASS |
| `$heavy go test -tags=unit,integration ./internal/setup -count=1` | PASS, including fresh database initial-admin provisioning |
| `$heavy go test -tags=unit,integration ./migrations -count=1` | PASS |
| `$heavy go test -tags=unit,integration ./internal/repository -run 'TestWorkspace\|TestMigrationsRunner\|TestUserRepository' -count=1` | PASS |
| `$heavy go test -tags=unit ./internal/service -run 'ProbeConnection\|ConnectionProbe\|URLAllowlist\|AuthService\|Workspace\|Grok.*URL\|BuildGrok\|AdminService_CreateUser' -count=1` | PASS |

The backslashes before `|` in the Markdown table escape table delimiters only; the actual Go regular expressions contain plain `|` characters.

`$heavy go vet -tags=unit,integration PACKAGE` passed separately for every package below:

```text
./internal/config
./internal/util/urlvalidator
./internal/modules/connections
./internal/modules/identity
./internal/modules/identity/postgres/
./internal/control/connections
./internal/control/identity
./internal/control/policies
./internal/control/billing
./internal/control/requests
./internal/repository
./internal/service
./internal/setup
./internal/securityaudit
./migrations
```

`git diff --check` passed. The final change also passes the preserved existing reason-code expectations and compatible-probe struct-equality tests.

## Fixture corrections and coverage limits

The assigned PostgreSQL 55494 cluster initially lacked `omni_fixture`, so the first integration attempt failed before tests could connect. Its default timezone also caused the existing migration-258 epoch assertion to fail. On that disposable cluster only, created the fixture login and configured its timezone to UTC; rerunning the affected suites passed. No source changes to existing migration 258 or its test were needed. Early compile/test failures while implementing the new regression fixtures were corrected before the final successful runs.

Tests use the assigned local PostgreSQL, local HTTP fixtures and existing Redis fixture. They do not contact the production database or live custom endpoint. Optional unrelated integration fixtures retain their existing environment-based skip behavior. `config.Load()` defaults and the configured custom public HTTPS URL are tested without network access.

## Exact changed files

- `backend/internal/config/config.go`
- `backend/internal/config/url_allowlist_options_test.go`
- `backend/internal/control/identity/onboarding_integration_test.go`
- `backend/internal/modules/connections/compatible_probe.go`
- `backend/internal/modules/connections/model.go`
- `backend/internal/modules/connections/probe_diagnostics_test.go`
- `backend/internal/modules/connections/workflow.go`
- `backend/internal/repository/user_repo.go`
- `backend/internal/repository/user_workspace_onboarding_test.go`
- `backend/internal/securityaudit/prompt_outbound_security.go`
- `backend/internal/service/account_test_service.go`
- `backend/internal/service/cn_provider_probe_url.go`
- `backend/internal/service/connection_probe_diagnostics.go`
- `backend/internal/service/connection_probe_diagnostics_test.go`
- `backend/internal/service/crs_sync_service.go`
- `backend/internal/service/gateway_upstream_request.go`
- `backend/internal/service/gemini_messages_compat_service.go`
- `backend/internal/service/grok_upstream_url.go`
- `backend/internal/service/openai_gateway_request_body.go`
- `backend/internal/service/pricing_service.go`
- `backend/internal/service/url_allowlist_test.go`
- `backend/internal/setup/setup.go`
- `backend/internal/setup/workspace_onboarding_integration_test.go`
- `backend/migrations/334_backfill_default_workspace_members.sql`
- `docs/v032/CONTRACT_T2.md`
- `docs/v032/T2_VERIFICATION.md`
