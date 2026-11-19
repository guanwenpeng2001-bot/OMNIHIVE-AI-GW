# BE-17 scoped BYOK pilot

## Enrollment and control

Apply migration 306 after BE-04, BE-06, BE-07, BE-10 and BE-11. It starts with an empty allowlist. Enroll a reviewed workspace and ordinary provider API-key class explicitly. Set the service fee in USD per billable BYOK attempt. Its default is zero.

```sql
INSERT INTO byok_pilot_providers(workspace_id,provider,enabled,service_fee_usd)
VALUES (:workspace_id,:provider,true,:reviewed_service_fee)
ON CONFLICT (workspace_id,provider) DO UPDATE SET
    enabled=excluded.enabled, service_fee_usd=excluded.service_fee_usd;
```

The pilot creates a disabled, unschedulable native account for journal identity. Keep it out of ordinary shared pools. The isolated vault uses BE-04's persistent authenticated encryption and verified encrypt/decrypt round trip, bound to the workspace, account, random BYOK reference and version. Legacy account rows contain no pilot key or resolvable pilot reference. Only the current version has ciphertext. Retiring a version erases its ciphertext from the live vault. Include the new tables and the persistent encryption key in the existing backup and restore procedures.

Add `internal/control/byok.ProviderSet` to the aggregate injector. Pass its handler to the protected v2 router and register the store with BE-01's existing operation readers. Compose the BE-17 fragment and regenerate FE transport types in the aggregate change.

```go
byokhttp.Register(v2, byokHandler, gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(), gin.HandlerFunc(auditLog))
```

Configure `auditBodyOmittedRoutes` before mounting. Register the collection POST and each credential's rotate POST for **both** route shapes below. Route-level omission covers malformed and unrecognized secret fields. Use metadata-only audit actions for verification, policy replacement and revocation. Exclude bodies from any upstream HTTP recorder.

```
POST /api/v2/workspaces/:workspace_id/byok
POST /api/v2/workspaces/:workspace_id/byok/:id/rotate
POST /api/v2/workspaces/:workspace_id/projects/:project_id/byok/credentials
POST /api/v2/workspaces/:workspace_id/projects/:project_id/byok/credentials/:id/rotate
```

Both collections require `credentials.byok.manage`. Workspace management can grant several projects within the workspace. Project-delegated management lists and changes credentials dedicated to that project. Each nonempty grant dimension is an allowlist. A key must belong to a granted member and project. Runtime eligibility also checks the current membership instance, live key, workspace authorization generation, actual model policy, verified model inventory and verification expiry. Removing and recreating a membership requires a new grant.

## Dispatch and accounting

1. For each authorized BE-01 scope, call `Store.Eligible(ctx, scope, model)`. It returns verified credentials in ascending priority and ID order. Build each graph entry through `BindCandidate`, supplying native capability, funding entitlement, health, quota and region evidence. Supply the request's affirmative platform consent for a possible wallet lane. Preserve the returned candidate ID and fact when calling `policy.SnapshotWriter.Capture`. Wallet candidates require a verified shared BE-04 connection, endpoint fallback permission, policy funding-switch permission, current credential consent and request consent. Tenant candidates use the isolated pilot verification and their disabled account ID.
2. Publish exact BE-07 cards with `pricing.VersionStore.Save` under `byok.PriceScope(workspace, project, account, source, model)`, using the tenant credential ID for `byok` and the selected platform account ID for `wallet`. The pilot supports `text.chat` and `text.embeddings` with bounded input and output tokens. Freeze the approved wallet card ID and pricing instant in BE-06 admission when consenting to a possible fallback. Currency, exact tiered rates, scenario bounds, the pricing instant and amount are independently preserved. The first release supports one wallet price version per admitted request. A different price requires new admission.
3. At the existing physical send boundary call `byok.Gateway{Store: vault, Planner: policyStore, Wallet: settlement, Budget: externalBudget}.Send(ctx, dispatch, transport)` with the BE-01 authorized context and BE-06 reauthorizer. Use the scoped scenario and request policy from admission. The first attempt must match the planner's selection. Its immutable record freezes request consent and the eligible shared candidates for this credential. A retry can only narrow that set. A later attempt needs an eligible matching candidate and a prior BE-06 retry/switch decision. Hold native concurrency/capacity slots before invoking this method. The method owns the BE-06 dispatch permit for the pilot. Run the existing provider transport exactly once inside the callback, with the callback context so force revocation can cancel the call. Tenant material is ephemeral and valid only for the selected native account and endpoint. Resolve wallet credentials normally with the BE-04 version fence.
4. Every wallet attempt and each positive tenant service fee needs a distinct BE-11 wallet-backed key reservation. Configure an active key period budget for those pilot keys. BE-11 can return successful admission with an empty hold on an unbudgeted key. This is rejected. A unique reservation reference isolates competing dispatches. A second planner/credential check and a locked database check of the actual reservation, payer, key, amount and expiry follow reservation. A paid quote must round trip exactly through the current BE-11 float input. Amounts outside that compatibility range fail admission. The final transaction records the single-use journal permit and immutable funding evidence together. A failed pre-commit check releases its own hold. BE-11 expiry recovery handles an interrupted or uncertain commit.
5. Feed the callback's attempt to BE-06 for acceptance, decisions and terminal evidence. For a billable attempt, call `Store.SettlementAmounts(ctx, frozenScope, attemptID, observedUsage)`. Pass its platform charge and reservation ID to the ordinary deduplicated BE-11 receipt. Persist or project the external estimate and platform service fee separately. A BYOK platform charge consists of its service fee alone. A wallet charge is recalculated from the exact stored card and observed dimensions. Missing wallet usage/pricing requires the existing unpriced or reconciliation path. Settle or release unused holds according to acceptance and receipt state. The scanner can join orphaned attempt reservations through `byok_attempt_funding.reservation_id`, while unbound holds expire through the existing BE-11 worker.
6. Opted-in external budget admission requires `ExternalBudget.AdmitExternal` supplied by the budget owner. It receives the exact external estimate, scoped principal, attempt ID and the execution transaction. It must apply its own budget constraints atomically. A missing adapter or unknown external cost denies an opted-in attempt. Ordinary external estimates never enter a wallet reservation or debit.

## BE-28, request details and frontend

- Implement further provider classes, service-account grants, event reads and self-service grant patches on the dedicated BYOK store. The existing verify probe requires demonstrated network, API-key authorization and a discovered model. Verification expires after 24 hours. Rotation verifies the replacement before switching and retires the old ciphertext in the same transaction. A failed replacement preserves the live credential. Old-version send permits fail CAS. Drain revocation closes new dispatch and lets existing transports finish. Force also cancels active pilot transport contexts, checked against PostgreSQL every 250 ms. Already accepted upstream work retains its ordinary acceptance and settlement evidence. Disable the provider's API key at the provider for provider-side revocation.
- The project collection supports list/create, `PUT /{id}/policy`, lifecycle POSTs and `GET /{id}/estimate`. Workspace self-service uses the equivalent `W/byok` routes for administration. Use fresh idempotency keys on POST and the quoted current resource version on policy, verify, rotate and revoke. A successful retry with the same POST key replays the original safe result. Verification and lifecycle results can already be terminal in the returned 202 Operation. A failed verification makes the credential unavailable. Render `verified_until`, priority, grant dimensions and fallback consent explicitly. Keep input secrets out of browser storage, query keys and telemetry.
- Estimate requires the project, a granted key/member/model, and explicit input/output token bounds. Render `external_state=unknown` and a null value as unknown. A known zero has a Money object. Display the exact platform service fee separately with currency and no floating-point conversion.
- BE-14/16 can join `byok_request_funding` by attempt and full authorized workspace/project/request scope. Gate financial columns behind `billing.ledger.read` and credential identity behind its metadata permission. The per-attempt source and frozen estimate remain available even after rotation or revocation. Use the settled receipt for actual platform charges. Show mixed funding if attempts switched. EffectiveQuota consumes `include_external_in_budget` for external exposure. Ledger sums must avoid adding a component fee to a platform charge which already contains it.

## Verification and rollback

Run ordinary Go build/vet/tests from `backend`. With an isolated PostgreSQL `job_test` and Redis, run `go test -tags=integration -count=1 ./internal/control/byok`. The integration suite uses the real policy planner, grant checks, encryption, wallet holds, execution log, exact pricing and metadata-only Gin routes. Provider verification uses a deterministic probe fixture.

Disable `byok_pilot_providers.enabled` to close admission and enrollment. Revocation remains available for disabled enrollments. Drain existing transports and settle or expire outstanding reservations. Keep migration 306 and its evidence for receipt reads across rollback. Existing platform OAuth and API-key credentials require no data conversion.
