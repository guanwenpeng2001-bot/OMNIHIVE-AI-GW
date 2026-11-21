# Quotes integration

Apply `backend/migrations/296_exact_price_versions.sql` through the existing migration runner before enabling persisted publication lookup. The store appends immutable exact rate cards. Repeat writes of identical content are idempotent. To schedule a replacement in the same price lane, publish successive cards with disjoint half-open effective windows. Overlapping active cards in one lane produce a resolution error. Binary rollback can leave the additive table in place.

## Catalog composition

The catalog registration already owns both comparison paths. In `internal/control/catalog/models/handler.go`, use the existing `h.serve` wrapper for the following handlers. Retain the catalog middleware and the post-read authorization check in that wrapper.

```go
store := pricing.VersionStore{DB: db}
compareService := compare.Service{Catalog: reader, Published: store}
quoteService := quotes.Service{Catalog: reader, Published: store, Keys: authorizedKeyPrices}
```

- Replace the catalog compare handler in the public and scoped model groups with `h.serve(compareService.Handle)`. The response keeps a free estimate as an explicit zero charge and preserves the ordering of the requested IDs.
- Add `POST /workspaces/:workspace_id/projects/:project_id/quotes` as a sibling of the scoped models group using `h.serve(quoteService.Handle)` and the same tenant headers, session and rate-limiting middleware. The action is `models.read`.
- Supply `quotes.KeyPrices` from the project/key policy and route binding layer. Authorize the supplied key, model and action together in the current workspace and project, then load the key's effective group/channel tariff. The adapter receives an ID and needs no key credential. A missing adapter returns 404 for keyed quotes.
- Register these dependencies in the composition root. The comparison operations remain defined by catalog and the quotes operation is defined by pricing. Regenerate the aggregate OpenAPI and frontend types as part of the composition change. To publish the extended scenarios on the compare routes, update catalog's compare request schema to reference `ExactPriceScenario`.

Use `quotes.PublicationScope` for cards the publication layer has already approved for the specified audience. Public prices and actor-scoped prices have separate keys. Scoped keys include the authorization version. Each request still requires current catalog visibility. The price store accepts server-derived scopes only. A historic `price_at` is evaluated when an immutable version covers it. Where publication is absent, current unkeyed estimates use the catalog reference projection. Some provider-specific legacy price cards may have no complete projection and yield an unpriced estimate.

## call sites

| File | Integration |
| --- | --- |
| `internal/service/model_pricing_resolver.go` and `pricing_service.go` | Source authorized group, channel, catalog and fallback cards for `pricing.Resolve`. Freeze effective cards at the original pricing instant. |
| `internal/service/billing_service.go` | `FreezeExactCost` runs the existing selection and exports a replayable exact leaf for shadow comparison. It is subject to legacy input, float and storage limits. New versions should load exact strings directly. |
| `internal/service/billing_pricing_snapshot.go` | `ReplayExactPricing` compares supported frozen v1 leaves with the exact calculator using no live pricing reads. Preserve legacy replay for historical composite and staged-rounding receipts. |
| `internal/service/gateway_usage_billing.go` and `openai_gateway_usage.go` | Assemble typed observed dimensions for native and OpenAI charges, with cached input, media, search and reasoning represented once. Freeze the selected version and usage before enqueueing. For a multi-part charge, combine the applicable rate rules and quantities before applying the eight-place charge rounding. |
| `internal/service/usage_billing.go`, `usage_settlement_receipt.go`, `usage_settlement_service.go` | Carry the frozen exact inputs into the receipt. Recalculate from `pricing.Frozen.Calculate` on recovery and make the settled eight-place amount authoritative for customer effects. Keep historical fingerprint verification on its versioned path. |
| `internal/service/api_key_budget.go`, `internal/repository/usage_billing_repo.go` and `usage_settlement_receipt_repo.go` | Use the same frozen amount for billing effects and enforce the narrower legacy storage bounds at the write boundary. |

The migration provides storage for exact versions. Publication/backfill of existing rates and the keyed production policy adapter require composition work. Quote or comparison reads do not invoke an upstream model or mutate settlement.

## Verification

Run `contracts/migrations/quotes/smoke.sh` with `QUOTES_POSTGRES_DSN` and `QUOTES_REDIS_ADDR` pointed at dedicated disposable instances. It probes Redis, applies migration 296 twice, tests immutable storage and exercises the Gin handlers with a scoped fixture catalog. It also checks exact replay against the money historic fixtures. The full application needs the composition above before its live routes exercise pricing.
