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
