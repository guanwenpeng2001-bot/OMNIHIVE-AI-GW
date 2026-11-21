# Catalog integration

## Composition

Add `modelshttp.ProviderSet` from `internal/control/catalog/models` to the composition root. Pass `*modelshttp.Handler` through `server.SetupRouter` to `registerRoutes`. After the existing v2 registrations, add the following using the existing session, panel limiter, audit and backend mode guard.

```go
catalogGuard := middleware2.BackendModeUserGuard(settingService)
modelshttp.Register(v2, modelsHandler,
    []gin.HandlerFunc{panelRateLimiter.PublicIP(), catalogGuard},
    []gin.HandlerFunc{gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(), panelRateLimiter.Heavy(), catalogGuard, gin.HandlerFunc(auditLog)},
)
```

Wire reuses the database, existing identity AccessEvaluator, v1 plaza, pricing, settings, API key and ops services, and configuration. Regenerate from `backend` with `go generate ./cmd/server` and `go run ./api/openapi/compose`. Generate `frontend/src/api/generated/v2.d.ts` with the pinned generator from the integration log.

| Method | Paths under /api/v2 | Authority |
| --- | --- | --- |
| GET | /public/models, /public/models/:id | Anonymous plaza on, backend mode off, anonymous access enabled |
| GET | /workspaces/:workspace_id/projects/:project_id/models, /workspaces/:workspace_id/projects/:project_id/models/:id | Session, visible project, models.read and existing group visibility |
| POST | /public/models/compare, /workspaces/:workspace_id/projects/:project_id/models/compare | Same respective catalog authority |

The published public list is credential-free. Use the scoped route for an authenticated plaza, including when the existing plaza setting requires authentication. Effective scope comes from identity. Endpoint inventory, region and privacy metadata require `connections.read`. Existing model discovery, channel/group visibility and v1 pricing decide membership and reference rates. The catalog retains independent access, route and price states. A route candidate in the catalog can still fail live scheduling or funding checks.

## Migration and retention

Apply `294_catalog_evidence.sql` with the ordinary runner. The migration imports account discovery snapshots and successful connections verifications into private immutable evidence. Existing and new legacy writers continue to own the discovery source and connection verification. Triggers record changed successful evidence alongside the existing write. Failed refreshes retain the historical success. Active connection evidence is fenced by workspace, verification identity, current credential and proxy revision. Observed and expiry times survive projection. An expired observation remains historical.

The first enabled catalog read imports the current approved registry version and typed facts. A successful public read inserts its safe snapshot, visibility provenance and outbox event in one statement. Repeating the same publication version creates a single event. Consumers can order by `catalog_publications.revocation_epoch`. Read routes recheck live visibility and authorization on every request. Stored snapshot rows supply provenance. Their expiry is one hour. An opaque signed cursor preserves its original `as_of` for up to 15 minutes and returns 410 if authorization, evidence or published content changes. All instances must share the deployment JWT secret for cursor continuity.

Leave the additive tables and triggers in place during binary rollback. Deploy migration 294 before enabling these routes. The public projection deliberately excludes private endpoint IDs, discovery values and actor or project metrics. Only the registry and explicitly public plaza rates are independently publishable. Deployment retention for historical capability and publication rows belongs to the data retention rollout.

## Frontend integration

- **Plaza** should list with repeated facets and an opaque `next_cursor`. Preserve the exact scope and filters on continuation. Restart from the first page on 410. Use `model` as the inference name and `id` for detail and compare. Access filters are scoped only. Unknown values sort after known values.
- **Detail** accepts the list's `catalog_version`. Source, maturity, observation time and freshness accompany individual model and endpoint facts. Scoped access describes whether the user can bind a visible group in the project. A covered connection verification confirms authorization and discovery; its action binding carries the gateway eligibility evidence and states that live invocation is unmeasured. Configured endpoint region and confirmed training preference are separate facts. A training preference does not establish storage retention. Treat null values, explicit false and zero distinctly.
- **Compare and heatmap** can send one to three IDs to `models/compare`. The response preserves input order and one catalog version. Actor-owned project samples cover the last completed 24-hour window, with five samples required per metric. Success rate is a fraction from 0 to 1. Public metrics are withheld. The benchmark array stays empty until a verified benchmark source is available. Reference estimates support plain USD token/request rates and cached input, use decimal arithmetic, and can return `unpriced` for ambiguous, tiered or time-banded rates. A production quote and settlement remain pricing and wallet responsibilities.

## Backend integration

- **pricing** can start with `catalog.CatalogReader` and opaque `StableID(provider, model)`. The v2 projection's `ref_` price versions identify display rates inherited from the v1 plaza float boundary and encoded as exact Money rates. Replace that adapter with pricing's published price versions and quote calculator when available. Capability writes never alter prices.
- **routing** should use authorized catalog membership and action identity as input to its policy planner. Consume endpoint evidence with the current workspace/project and enforce expiry, adapter eligibility, credentials, health and runtime constraints independently. Public registry tuples are reference facts with an unconfirmed invocation mode. The scoped display of a route candidate grants no scheduling rights.
- **catalog-verification** can append time-bounded action probes and explicit publishable declarations to the evidence contract. Preserve previous successes when subsequent probes fail and distinguish last success from active eligibility.

## Verification

Use a disposable database and isolated Redis. Run the module and route tests from `backend`, and after composition run the smoke script against an isolated test installation. Enable the plaza and use an account, attached channel and group with a visible model to exercise detail and compare.

```sh
go test ./internal/modules/catalog/... ./internal/control/catalog/... ./internal/legacy/catalog/...
go test -tags=integration -count=1 ./internal/control/catalog/models
```
