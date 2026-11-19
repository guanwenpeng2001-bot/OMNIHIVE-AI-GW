# BE-14 request records

Apply `backend/migrations/303_request_readmodel.sql` through the existing migration runner after 290, 293, 295, 297, 300 and 301. It adds live read views, scoped seek and trace indexes, and a table containing export handles and their selected sections. Historical receipt and usage projection follows the existing BE-01 backfill checkpoints. Complete the `usage_logs` and `usage_settlement_receipts` checkpoints with the BE-01 scripts before checking historical coverage.

## Registration

Construct `handler.ProvideRequestsHandler(db, identityStore, cfg)` during startup. Pass the resulting handler to `internal/control/requests.Register(v2, requestsHandler, ...)` with the same session, rate limit and audit middleware as BE-01. Append that instance to `identity.Operations.Readers` before constructing the identity handler. It serves request export statuses through `GET W/operations/{id}`.

Compose `backend/api/v2/fragments/BE-14.yaml` after BE-01, BE-05 and BE-08. The API fixtures in `contracts/api/BE-14` exercise the management responses. The usual v1 Ops and Usage readers retain their existing endpoints during migration.

## Request evidence

- `request_anchors` supplies one scope-qualified identity per gateway request, turn or job. It includes historical receipt and usage anchors only when BE-01 has proven the resource mapping. Historical receipt/usage correlation requires the same request ID, API key, funding user and account. Existing project assignments take precedence. An Ops log can refine a journaled or historical request when the key, user and server-controlled correlation match. An Ops-only row without project evidence remains outside this projection.
- `internal/modules/requests.Store.List` takes an authorized workspace/project and a seek page. Order uses `(started_at, id) DESC`. The half-open window uses admission time for journaled requests and observation time for historical ones. The signed cursor binds actor, workspace, project, grant version, filters and a 15-minute admission cutoff. Timestamped receipt and completion evidence use the same cutoff. The producer has no revision history for mutable attempt fields.
- Journal requests use the journal UUID. Historical IDs use `legacy-receipt-{receipt_id}` or `legacy-usage-{usage_log_id}`. The earlier server/receipt aliases in `gateway_execution_legacy_ids` remain available for proven internal correlations.
- `attempt_count` measures dispatch permits. A failed retry adds to that count and the logical request remains one item. Unknown attempts abandoned under BE-03 appear under `abandoned_attempts` with unconfirmed receipt evidence. The eligible replacement carries its own linked receipt and usage. An expired dispatch without a receipt remains unconfirmed.
- The timeline distinguishes journal admission, gateway finalization and the receipt settlement timestamp. Where the receipt supplies upstream TTFT, the estimated first-output instant is dispatch time plus TTFT. The milestone identifies the estimate as `upstream_ttft_estimate`. The journal currently has no dedicated client-first-output timestamp. Where observable, a normal client completion comes from gateway finalization.
- Route explanation selects the latest stored BE-08 execution decision. It carries trace-local candidate IDs and the actual exclusion reasons. BE-08 currently has no persistent candidate-to-attempt identity mapping. Attempt connection labels require `connections.read` and authoritative same-scope inventory. BE-12 conversion evidence is shown when the attempt has a persisted `conversion_plan`. The BE-12 baseline exposes only the schema and context-level conversion plan, so producer coverage can be partial.
- Receipt and settlement state remain visible as operational metadata under `requests.read`. Amounts, funding account, funding source, price version, reservation and ledger references require `billing.ledger.read`. The funding-source filter requires the same grant. Connection names and IDs are protected by `connections.read`. The reader projects allowlisted metadata fields from legacy Ops and receives no result artifacts, provider response bodies or credentials.

## Export operations

`POST P/requests/{id}/exports` uses `requests.read`, `requests.export` and a scoped idempotency key hash. Identical ordered or reordered sections return the original operation and expiration. A changed section set returns 409. The operation is immediately ready and the 202 response points to the BE-01 status route. Its 15-minute result URL is scoped to the originating actor, request, project and workspace. Poll and download reauthorize both grants. The download applies the current finance and connection grants, then streams the selected JSON sections from the same detail projection. The database retains the registration for idempotent retry and stores no serialized export. Expired downloads return 410.

## Consumers

- BE-16 can link a settlement to its journal UUID when `gateway_execution_receipts` proves the association. Historical settlements link using `legacy-receipt-{receipt_id}` when scoped by BE-01. Read immutable receipt command and BE-11 wallet evidence to reproduce amounts. Preserve the distinction between absent, pending, review, and settled values. The detail reader reports the settled command amount, which can differ from actual wallet movement.
- BE-18 can use scoped journal UUIDs to count logical requests and count dispatch permits separately. Use the outcome and safe reason fields for upstream rate limits, client cancellation, and stream interruption. Public status should only consume aggregated samples with its own measurement rules.
- BE-21 can use the BE-01 route audit middleware and the operation actor, request and project identifiers for export audit. Registration, status and download all check current access. Use `request_metadata_exports.expires_at` when defining retention, preserving idempotency for the agreed retry window. Download regenerates the projection and retains no serialized content.

## Verification

Set `UPSTREAM_JOB_TEST_DSN` to a disposable PostgreSQL 16 database named `job_test`. The test uses the real migration runner and identity evaluator.

```sh
cd backend
go test -tags=integration -count=1 -v ./internal/control/requests
go build ./...
go vet ./...
go test ./...
```
