# Catalog verification and compatibility

Use an existing platform-managed connection in workspace 1. Register its model and matrix tuple with `POST /api/v2/admin/catalog/verification-targets`. The response supplies the catalog entry ID. An interval of zero selects manual verification. A periodic schedule accepts 300–604800 seconds and runs under the approving actor. Update it with `PUT /api/v2/admin/catalog/verification-targets/{id}/schedule` and the schedule version in `If-Match`.

Queue `POST /api/v2/admin/catalog/verifications` with an `Idempotency-Key`. The request may omit `protocol` when the other dimensions select one target. Poll the ordinary workspace operation URL from the 202 response. `GET /api/v2/admin/catalog/verification-targets/{id}` shows the last attempt and the last successful observation independently. Both command and status reads require `catalog.verify`.

A single sample represents a conclusive credential rejection or exact model discovery with BE-04 authorization evidence. A timeout, public upstream listing, missing model, unsupported discovery or interrupted attempt reports unknown with zero conclusive samples and a null `verified_at`. UTC `window_start` and `observed_at` describe the observation window, and freshness becomes stale at `expires_at`. A previous success can remain visible alongside a later failure or unknown result. These observations describe catalog discovery and are independent of service health and production request metrics.

Publish explicitly with `POST /api/v2/admin/catalog/verification-targets/{id}/publications`, `catalog.publish`, `Idempotency-Key` and the current publication version as `If-Match` (initially `"0"`). A candidate advances to experimental, stable, deprecated and closed. Stable requires the newest, fresh successful probe with the release's compatibility fixture and current connection revision. The included fixture covers a non-streaming OpenAI Chat client using the Responses bridge. The checked-in request uses a synthetic model. A real client substitutes its authorized catalog model.

Deprecating an entry requires a reason and a future sunset. Closing is available at sunset. A rollback to an earlier revision appends a new version pointing to the prior evidence and notice. A rollback to `"0"` withdraws the compatibility entry. The public and scoped ModelDetail projections include published matrix tuples for already visible models. Read `maturity`, `state` and `freshness` together. Evidence for an experimental tuple stays with that tuple. Access, route eligibility and prices come from the existing catalog sources.

- [Verification request, accepted operation, and terminal results](verification.json)
- [Chat and Responses bridge fixture](compatibility-example.json)
- [ModelDetail compatibility with a deprecation notice and stale evidence](deprecated-compatibility.json)
