# Policies integration

Apply `backend/migrations/299_versioned_routing_policies.sql` with the standard migration runner. Each scope has one policy lineage. Draft edits and publications append immutable revisions with the current inherited definitions and versions. Rollback appends a publication based on the current active version, restores the selected definition and recomposes it under current parent constraints. The epoch, active assignment, impact, outbox row and idempotency receipt commit together.

## Control and API

Add `policies.ProviderSet` from `internal/control/policies` alongside the identity providers. Pass the handler to the protected v2 route group.

```go
policies.Register(v2, policiesHandler,
    gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(), panelRateLimiter.Heavy(), gin.HandlerFunc(auditLog))
```

The registration supplies tenant cache and request-ID headers. The handler checks the identity evaluator for each action against the policy's actual workspace and project. For a simulation it also checks the scenario project. An alternate key requires its own project scope and the explicit `keys.act_as` grant. Identity grants that project action to administrators through its standard role rules. Key binding, current credential status, action and model restrictions are checked from metadata.

At aggregate composition run `go generate ./cmd/server` and `go run ./api/openapi/compose` from `backend`, then regenerate the v2 transport types. The fragment imports the identity parameters and error envelope and the routing trace. Request examples live in `contracts/api/policies/workflow.json`.

- Use exact scope filters `scope_type,scope_id` on the workspace collection. For an `api_key` scope also provide `project_id`. The default collection scope is the workspace. Project-only grantees open their project filter.
- Creation, publication and rollback require `Idempotency-Key`. Draft edits, publication and rollback require a quoted decimal `If-Match`. A stale draft edit returns 412 and the current ETag. Competing publishers or rollbacks return 409. Repeating a successful command with the original key and input returns its original result.
- The initial draft has an empty `base_version`. A successful publication clears `draft_version`. Use the active version as the next draft's base. Empty `allow_list` values are invalid. Use `deny_all` to close a scope. Candidate pool IDs in different layers intersect. Hard funding, capability, region, training, retention and price restrictions also apply to fallback candidates. Rates on this policy boundary are exact USD per million input tokens.
- Use the same explicit unsigned decimal seed for draft and baseline. Seed zero is valid. Simulation receipts bind the scenario, evidence, draft, base, assignment hierarchy, scope generation, impact counts and expiry. Changed input requires a new simulation. A comparison swaps the edited layer under the same current ancestors and descendants. The candidate labels are opaque and stable within the frozen graph. No paid decision follows from a successful simulation.

## Snapshot production and gateway reads

Bind the authorized routing candidate producer at admission and in the control-plane evidence refresh worker. Use `policy.SnapshotWriter{DB: db}.Capture(ctx, caller, scenario, graph)` when the authorized facts are acquired. Supply the identity actor, workspace and scope version, plus the exact project, optional key, model, protocol, action, capabilities and input/output bounds. An unknown or expired graph returns 410. Capture is outside the simulation request path. The registered simulation executes a repeatable-read, read-only transaction and calls `planner.Plan` directly.

Use opaque unique snapshot IDs, freeze `AsOf`, catalog and price versions and provide bounded expiry of at most five minutes. Publish fresh evidence after eligibility, authorization, price or capacity changes. Persist one fact record per planner candidate. Include verified connections connections' account and verification IDs in `Evidence.Connection`. The reader rechecks project ownership, active verification, pool memberships, current credential version and proxy revision. These queries select metadata columns only. Candidates with different regional/retention evidence remain distinct even when they serve the same model.

Pricing tariff selection must happen in the already authorized price lane for that candidate. Pass the frozen `pricing.Version`, original instant, input bound and tier label to `policy.ExactInputRate` to obtain the candidate price. An unpriced, non-USD or unrepresentable result leaves price unknown. Supply server-observed funding, quota, verified attributes and capabilities from the corresponding authorities. An endpoint label or catalog reference by itself supplies no verified privacy or capacity evidence.

Consume `Store.Published(ctx, target)` for the current committed layers, publication time, version, expiry and epoch. The direct reader on each admission provides immediate cross-process visibility. A cached integration must poll `Store.Changes` within five seconds, advance its durable cursor after invalidation, refresh before the returned snapshot expires, and reject stale admission after sixty seconds. The policy reader has no Redis dependency.

`Store.PlanExecution(ctx, authorizedScope, scenario, requestLayer, seed)` loads the authorized graph, requires an evidence age of at most sixty seconds and calls the routing planner using execution mode. Its eligible order is the execution boundary for primary and retry dispatch. Keep existing native credential rechecks, funding reservations, slot acquisition and settlement attempt journaling at dispatch. Restrict every retry to the remaining eligible candidates. Trusted deployment bootstrap can create/edit a platform lineage with workspace zero and publish it through `BootstrapPlatform`. Tenant handlers accept workspace, project and key lineages.

## Legacy rollout and rollback drill

1. Finish the identity ownership backfill for groups and composite routes. Run `psql -X -v ON_ERROR_STOP=1 -f contracts/migrations/policies/backfill.sql` against that database. Repeating the transaction updates changed source fields and clears deleted, moved or quarantined shadow entries.
2. Read each shadow through `service.LegacyRoutingIntent`. Compare group model patterns and ordered account IDs, both fallback group IDs, action and privacy gates, dispatch mappings, manifest settings and quota/profit gates. Use the original `Group.ModelAllowlist.Allows` semantics for wildcard aliases. For composite routes compare public model/match type, account type, platform, upstream model, endpoint, priority, enabled state and every ordered target. `EffectiveTargets` preserves legacy single-target defaults, target order, region/provider constraints, weight, latency and affinity TTL.
3. The old mixed MCP handler retains per-user preferences in process memory. Export preferences while the source instance and user session are still available. Record the reviewed user/workspace/project relation in `routing_account_preference_owners`. Use `service.ImportRoutingPreferences` with the same user and scope. It retains the original strategy, ZDR, fallback, prompt cache, region, provider whitelist, exact budget value, threshold and update time. Review funding and provider identities before converting this advisory record into a draft. Keep missing or disputed mappings out of publication.
4. Shadow-plan representative primary, fallback, unknown-evidence and BYOK cases against matched snapshots and seeds. Publish a scoped test draft, verify a second gateway reader sees the epoch, change its region, then roll back and verify it restores the original choice under a new version. Keep legacy readers and publishers serving their rollout cohort through parity and the rollback drill. Runtime owns old-handler retirement. Binary rollback leaves additive schema and history in place.

## and byok

Playground should submit the downstream key's identity authorized scope and the same sanitized scenario used in the approved graph. Plan through `Store.PlanExecution`, admit the chosen eligible candidate through journaling and wallet reservation and attach the execution trace to the journal. A playground comparison uses `Store.Simulate` and its explicit seed. A paid run uses a separate request and reservation.

Byok should mark a funding switch in `Evidence.BYOKToPlatform`, preserve the candidate's specific funding source and set `FundingAuthorized` only after key/member consent, project entitlement and the applicable funding authorization. Endpoint/model fallback, parent policy and funding authorization are independent gates. Resolve the confirmed connections secret version immediately before dispatch. Native paid rollback or retry still needs its own reservation and credential checks.

## Verification

From `backend`, run `go test ./internal/modules/policy`. With disposable PostgreSQL `job_test` and Redis at `UPSTREAM_JOB_TEST_DSN` and `AFFINITY_REDIS_TEST_ADDR`, run `go test -tags=integration -count=1 ./internal/control/policies`. The suite applies the migration chain, exercises Gin, races publications, checks impact, expiry, rollback, a separate gateway reader, credential revocation and a simulator role without ciphertext or commercial-write privileges. It also checks idempotent legacy backfill and explicit preference ownership.
