# Backend integration log

## BE-01 · Project identity and gateway key policy

Integrated on `codex/refactor` on 2026-11-19. The production router registers the v2 identity routes with the existing JWT session, panel rate limits and audit middleware. It installs the project-scope resolver before registering native gateway routes and aliases. Wire uses the existing PostgreSQL connection for both identity bindings. The operation registry starts empty until operation-producing packages supply readers. Migration 290 runs through the existing migration runner.

The integration suite used `UPSTREAM_JOB_TEST_DSN` for the dedicated `job_test` database and `AFFINITY_REDIS_TEST_ADDR` for Redis.

The composed OpenAPI 3.1 document and generated frontend transport declarations are checked in. Regenerate from the repository root with these commands.

```sh
(cd backend && go run ./api/v2/compose)
npm exec --yes --package=openapi-typescript@7.10.1 -- \
  openapi-typescript backend/api/v2/openapi.yaml -o frontend/src/api/generated/v2.d.ts
```

| Verification from `backend` | Result |
| --- | --- |
| `go generate ./cmd/server` | Passed |
| `go build ./...` | Passed |
| `go build -tags=embed ./cmd/server` | Passed |
| `go vet ./cmd/... ./internal/server/...` | Passed |
| `go test -tags=unit ./internal/server/middleware ./migrations ./cmd/server` | Passed |
| `go test -tags=unit ./internal/server` | Passed |
| `go test ./internal/server/... ./internal/handler/... ./internal/service/...` | Passed |
| `go test -tags=integration -count=1 ./internal/control/identity ./internal/core/money/...` | Passed against dedicated local PostgreSQL 16 and Redis 7 |
| Repeat OpenAPI composition and TypeScript generation | Both outputs matched byte for byte |
| Frontend `vue-tsc --noEmit` | Passed |

[The production-route smoke](../../contracts/migrations/BE-01/smoke.sh) passed against both the standard-mode server binary and the release-style `embed` build, each using a separate PostgreSQL database and Redis namespace. To reproduce, start the binary with a disposable data directory, a fresh test database, an explicit nonempty database password, isolated Redis and a bootstrap admin. Run the script with `SMOKE_BASE_URL`, `SMOKE_ADMIN_EMAIL` and `SMOKE_ADMIN_PASSWORD` for that instance.

| Production request | Result |
| --- | --- |
| `GET /api/v2/me/scopes` | Anonymous request returned `401`. A session from `/api/v1/auth/login` returned `200` with the default workspace and project. Both responses sent `Cache-Control: private, no-store` and `X-Request-ID`. |
| `GET /api/v2/workspaces/{workspace}/projects/{project}/access` | `200` with the expected actor, project and delegated actions. |
| `POST /api/v2/workspaces/{workspace}/projects/{project}/keys` | First request returned `201` and the restricted credential. Repeating the payload and `Idempotency-Key` returned `201` with the same key ID and `credential: null`. Both requests were recorded in the PostgreSQL audit log. |
| Ordinary and Google gateway routes | Both accepted the project credential and returned the existing group-assignment error for allowed actions on the ungrouped key. The ordinary embeddings route and Google `countTokens` returned `403 Access denied` for actions absent from the key. |
| `POST /v1/chat/completions` | Invalid key returned `401`. A funded, group-bound key issued through v1 authenticated and reached scheduling, which returned `503` because the test server had no upstream accounts. |

Automatic setup with an empty PostgreSQL password bootstrapped the initial test admin in `postgres`. The successful run used an explicit database password and verified the login against the dedicated smoke database. This setup behavior predates identity composition.

## BE-02 · Exact money and historical fixtures

Verified on the composed branch on 2026-11-19. The money package has no production routes or migrations. Its integration suite used the same dedicated `job_test` database and Redis service as the BE-01 suite. PostgreSQL numeric round-trips and Redis serialization passed alongside the historical v1 replay, JSON wire and exact rounding tests. The half-tie fixture `0.000078125` produced `0.00007813` for both the charge and reservation ceiling.

## BE-03 · Attempt controller and stream lifecycle

Merged `codex/be-03-attempt` (`712c2b6c6`) into `codex/refactor` as `50e4f9cf9` on 2026-11-19. No composition, router, Wire, OpenAPI or migration changes are required. The controller is internal to the gateway loops. Responses, Messages and WebSocket share one `AttemptDecision`. A verified silent first-output timeout allows one bounded replay on a different account. The abandoned attempt is recorded as `abandoned_uncertain` and is never charged. All other uncertain-acceptance cases stop without a second generation. See [the lifecycle contract](../../contracts/protocol/lifecycle/README.md).

| Verification from `backend` on `50e4f9cf9` | Result |
| --- | --- |
| `go build ./...`, `go vet ./internal/gateway/...` | Passed |
| `go test ./internal/handler/... ./internal/service/... ./internal/gateway/... ./internal/server/...` | Passed |
| `go test -tags=unit ./internal/handler/... ./internal/service/... ./internal/gateway/...` | Passed |
| Lifecycle protocol fixtures (34) | Passed on the package branch |

## BE-04 · Provider connections, credentials and pools

Integrated on `codex/refactor` on 2026-11-19 after merge `fd601fcb6`. The production router mounts the v2 connections and pools endpoints and the v1 OAuth authorization bridge with the existing JWT, panel rate limit and audit middleware. Wire shares the PostgreSQL connection, identity evaluator, persistent secret encryptor and account test service. The connection store supplies BE-04 operation status through the BE-01 registry.

The migration runner recorded `293_connection_workflows.sql` after migration 290 on isolated PostgreSQL 16.16. The backfill command converted one fixture account to an encrypted versioned reference and quarantined one with `MODEL_MAPPING_REVIEW`. Coverage reported one mapped, one quarantined and zero pending. An immediate repeat reported zero new conversions or quarantines. The native scheduler consumed the conversion event and its Redis cache held no inline API key for the converted account. The quarantined fixture retained its original credential.

Storage tests used `UPSTREAM_JOB_TEST_DSN` with a dedicated `job_test` database and `AFFINITY_REDIS_TEST_ADDR` with Redis 7.4.7. Regenerate Wire, the composed OpenAPI document and the frontend transport types from the repository root with these commands.

```sh
(cd backend && go generate ./cmd/server && go run ./api/v2/compose)
npm exec --yes --package=openapi-typescript@7.10.1 -- \
  openapi-typescript backend/api/v2/openapi.yaml -o frontend/src/api/generated/v2.d.ts
```

| Verification from `backend` | Result |
| --- | --- |
| `go build ./...`, `go build -tags=embed ./cmd/server` | Passed |
| `go vet ./cmd/... ./internal/server/...` | Passed |
| `go test -tags=unit ./internal/server/... ./migrations ./cmd/server` | Passed |
| `go test -p 1 ./internal/server/... ./internal/handler/... ./internal/service/... ./internal/repository/...` | Passed with PostgreSQL and Redis after updating the credential corruption fixtures |
| `go test -tags=unit ./internal/modules/connections ./internal/modules/credentials` | Passed |
| BE-04 account probe and credential tests | Passed |
| `go test -p 1 -tags=integration -count=1` for identity, connections, credentials, the BE-04 backfill and money | Passed |
| Repository credential storage tests with `CREDENTIAL_SECRET_TEST_DSN` | Passed |
| Repeat Wire, OpenAPI and TypeScript generation | All outputs matched byte for byte |
| Frontend `pnpm run typecheck` (`vue-tsc --noEmit`) | Passed |

Migration 293's version fence rejected legacy test fixture writes that changed ciphertext or revocation metadata without advancing the version. The affected repository tests now seed deliberately inconsistent rows with the trigger disabled only within the fixture transaction and enabled again before commit. Normal rotation, revocation and refresh tests continue through the active trigger.

The BE-01 and BE-04 production smokes passed against the standard-mode binary and the release-style `embed` build. Each used a fresh database, an explicit database password, isolated Redis, a persistent TOTP encryption key and a trusted loopback HTTPS fixture. The BE-04 smoke covered authenticated discovery, idempotent draft replay, pools, verification and operation lookup, preview, activation, rotation, revocation and history. See the [BE-01 smoke](../../contracts/migrations/BE-01/smoke.sh) and [BE-04 smoke](../../contracts/migrations/BE-04/smoke.sh).

| Production check | Result |
| --- | --- |
| `PUT /api/v2/workspaces/{workspace}/connections/{id}/credential` | Successful and invalid requests retained the actor, action, status and request ID in PostgreSQL audit rows. The request bodies were omitted. |
| `POST /api/v2/workspaces/{workspace}/connections/{id}/rotations` | The successful rotation and malformed request both omitted the request body. |
| `PUT /api/v1/workspaces/{workspace}/connections/{id}/authorization` | The legacy OAuth start route issued an HttpOnly cookie scoped to the workspace path. The bridge consumed a synthetic draft with that cookie and rejected an attempt without the cookie. Consumption removed the access token from the pending draft. Successful, rejected and malformed requests omitted their audit bodies. |
| Member without `connections.read` | Connection inventory, detail and event reads returned `403`. A member reading their own existing operation received `result: null` and `resource_id: null`. Disposable secret values were absent from the checked responses, audit rows and server logs. |

## Baseline gate at 5ed388cbd (coordinator)

After merging BE-06 (146fc5cdb), BE-09 (988c4f942) and the C5 release scaffold (c732ba3ee), the merged integration tip was checked:

| Check | Result |
| --- | --- |
| `cd backend && go test ./...` | Pass (EXIT=0, 71 packages ok) |
| `go run ./api/v2/compose` then `git diff` | No drift |
| `contracts/release/backend/verify.sh` | exit 0 — OpenAPI pass, J01–J06 pass; HTTP/MATRIX/FINANCE/ACCESS/MIGRATION/LOAD marked pending |

## Frontend gate at 5ed388cbd

| Check | Result |
| --- | --- |
| `pnpm run typecheck` | Pass (TC=0) |
| `pnpm run test:run` | Pass (TEST=0) |
| `pnpm run build` | Pass (BUILD=0) |
