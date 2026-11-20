# BE-01 integration

## Composition

Import `internal/control/identity` as `identityhttp` and `internal/modules/identity` as `identity`. Add `identityhttp.ProviderSet` to the composition root. This set constructs the PostgreSQL store with the existing `*sql.DB`, binds the control backend, `identity.AccessEvaluator`, `identity.ScopeResolver` and `identity.GatewayScopes`, and constructs the HTTP handler.

Provide `identity.Operations` at composition. Initially the constructor returns `identity.Operations{}`. As operation-producing packages land, supply their `identity.OperationReader` implementations in the `Readers` slice. Each reader owns its operation IDs and constrains its SQL lookup by workspace. It returns `fault.NotFound` for an unrecognized ID. For a match it supplies the actor, project, delegated action, and the result projected for the caller. The identity handler rechecks the workspace and project before returning the result. BE-09, BE-14, BE-15, BE-18 and BE-21 register their own readers.

Pass `*identityhttp.Handler` and `identity.GatewayScopes` from Wire through `server.SetupRouter` to `registerRoutes`. Inside `registerRoutes`, before the first call registering gateway routes, add the following with the existing config, JWT, request logger, audit middleware, and panel limiter in scope.

```go
r.Use(middleware2.InstallProjectScopes(gatewayScopes, cfg))
identityhttp.Register(r.Group("/api/v2"), identityHandler,
    gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(), gin.HandlerFunc(auditLog))
```

Register the resolver before **every** group using `NewAPIKeyAuthMiddleware` or `APIKeyAuthWithSubscriptionGoogle`. Gin captures middleware at route registration. Installing it in `SetupRouter` before the existing `registerRoutes` call also covers earlier groups and aliases. `Register` installs private/no-store and request-ID headers before session validation. Reuse the existing session binding, JWT validation, CORS and configured CSRF protections. Passkey RP and credential IDs, JWT/refresh session families, and TOTP records continue through their existing authentication handlers.

From `backend/`, regenerate Wire with `go generate ./cmd/server`. The persistence implementation uses database/sql and migration 290. The Ent schema and generated API client require no identity-specific generation. Compose `backend/api/v2/fragments/identity.yaml` into v2 OpenAPI and regenerate the frontend transport types there.

## Routes

| Method | Path below /api/v2 | Authority |
| --- | --- | --- |
| GET | /me/scopes | Authenticated workspace visibility |
| GET, POST | /workspaces/:workspace_id/projects | projects.read, projects.create |
| GET, PATCH, DELETE | /workspaces/:workspace_id/projects/:project_id | projects.read, projects.edit, projects.delete |
| GET | /workspaces/:workspace_id/projects/:project_id/access | Project visibility |
| GET, POST | /workspaces/:workspace_id/projects/:project_id/keys | keys.read, keys.create and all delegated native actions on create |
| PUT | /workspaces/:workspace_id/members/:id/grants | members.grants.write, target membership, expected grant version |
| GET | /workspaces/:workspace_id/operations/:id | Current visibility plus operation owner or delegated operation action |

Project and key creation require Idempotency-Key. Project update/archive and grant replacement require a quoted decimal version in If-Match. Existing memberships start at grant version 1. The PUT response returns its successor. The member management reader should expose `workspace_members.grant_version` alongside each existing user ID when the coordinator wires the member editor. DELETE archives a project, retaining financial and historical associations. All numeric identities on v2 are opaque decimal strings.

## Rollout and backfill

1. Apply `290_project_identity_and_grants.sql` with the standard migration runner. It follows 289. PostgreSQL seeds the default project for each existing workspace and on subsequent workspace creation. A unique partial index protects concurrent lazy creation.
2. Use the deployment's libpq environment and run `contracts/migrations/identity/backfill.sh`. Each invocation of `identity_backfill_batch(kind,500)` commits its mappings, quarantine entries and high-water mark together. Workers serialize per kind. Re-running resumes after the last committed ID and checks for lower sequence IDs that committed late.
3. Inspect `coverage.sql` per resource kind. The view covers workspace-owned accounts, groups, proxies, proxy groups, channels, monitors/templates, model access groups and keys, plus account/group-linked scheduled plans and composite routes. Jobs, usage, receipts and prompt audit evidence require matching key/user ownership and compatible workspace evidence. Batch image items and artifacts inherit their job's authorization. Missing or conflicting evidence enters quarantine. A verified manual repair corrects the underlying evidence and removes the reviewed quarantine entry, after which the next sweep assigns the row. Unresolved rows remain inaccessible.
4. Ship both gateway protocol adapters before enabling v2 key issuance. The new digest shares the existing unique credential index with status `project`. Legacy gateways reject that status. A database trigger prevents legacy rotation from issuing an unrestricted current/grace credential on a project key. Old custom secrets with the new prefix still use digest fallback. New gateways intersect project-key actions with current owner grants on every admission. Member deletion invalidates membership-bound keys even after re-enrollment. Legacy key IDs, credential digests, users, wallet payers and member IDs retain their original values. Legacy gateway keys preserve their existing owner-level admission policy.
5. Open the v2 console routes after the request-chain smoke test. The exposed native actions are `text.chat`, `text.embeddings`, `image.generate`, `usage.read` and `tasks.read`. A restricted credential needs an explicitly supported method/route and, for model-restricted inference, an authorized public model. Bearer, x-api-key and Google query clients continue through the native extractors. Production group assignment and billing eligibility remain effective. Where ungrouped scheduling is disabled, assign an eligible existing group through the existing group management flow before inference.

For a binary rollback, retain the expanded schema. Legacy clients can keep using their existing credentials. Requests using project credentials need project-aware gateway instances. BE-30 owns removal of the default-project lookup fallback after coverage, quarantine review and retirement of old writers.

## Verification and dependent packages

From `backend`, run `go test -tags=unit ./internal/server/middleware ./migrations`. With a dedicated `job_test` database and Redis configured through `UPSTREAM_JOB_TEST_DSN` and `AFFINITY_REDIS_TEST_ADDR`, run `go test -tags=integration -count=1 ./internal/control/identity`. The integration suite applies the real migration chain, binds Register to HTTP, exercises both protocol adapters and checks stale Redis authentication after a legacy credential revoke.

After aggregate wiring, make an authenticated GET to `/api/v2/me/scopes`, follow a real project to `/access`, create a restricted key with an idempotency key, replay that creation and check for a null credential. Verify both native gateways see the same authorized scope. Use an isolated test installation for the create smoke.

BE-04/05/08/10 and console handlers consume `AccessEvaluator.Evaluate`. BE-06 reads `scope.From(request.Context())` to freeze the actor, owner, user-wallet payer and authorization version. Before dispatch it must reauthorize a credential after any queue, retry or lease delay. BE-14/15/21 call `ScopeResolver.RequireResource` before direct lookup, export and artifact access, and recheck authorization before delivery. New resource families extend the ownership view and the checkpoint inventory with their own migration. BE-23 tracks counts and quarantine throughout rollout. BE-22/30 retire the bounded compatibility adapter once the corresponding legacy key, model, group, subscription and invalidation callers have moved.
