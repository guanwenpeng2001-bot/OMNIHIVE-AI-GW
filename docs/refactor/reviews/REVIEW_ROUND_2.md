# OmniHive Refactor — Review Round 2

- Baseline: `codex/refactor` `2637e9e59` (BE-05, BE-06, BE-09, ID-SWEEP, DS-A11Y + BE-06/BE-09 integration)
- Review worktree: `codex/review-1`, merged to `2637e9e59`
- Scope: BE-05 catalog, BE-06 execution journal, BE-09 restore, integration/wiring, ID-SWEEP compat renames; plus round-1 resolution check.
- Method: source review + throwaway verification (rate-math test, OpenAPI regeneration, contrast math, `go build ./...`). No source files changed; regeneration matched byte-for-byte.

## Severity legend

P0 blocks release; P1 must fix this round; P2 should fix; P3 polish/documentation.

---

## 1. BE-05 Catalog

### P1 — Catalog prices (display + compare) are inflated by the `per` denominator (owner: BE-05)

`backend/internal/legacy/BE-05/pricing.go:129` (`legacyRate`) builds a rate as `amount = value × per × multiplier` with denominator `per`. For the standard input price `$1.25 / 1M tokens`, the produced `input_rate` is `{amount:"1250000", per:"1000000"}` instead of `{amount:"1.25", per:"1000000"}`.

Verified with a throwaway test: `legacyRate(1.25, "input_token", "1000000", 1)` returned `amount=1250000 per=1000000` (expected `amount=1.25`). The failure propagates to:

- `Summary.Input/Output/AdditionalRates` via `priceFor` (`pricing.go:12`), so the public/scoped model list, detail and additional rates all render ~1,000,000× too high.
- `compare.go` `simpleEstimate` (`backend/internal/control/catalog/models/compare.go:135`) which calls `rates[i].Cost(q)`; because `Cost = amount×q/per`, the estimated charge for 1000 tokens at `$1.25/1M` becomes `$1250.00000000` instead of `$0.00125000`.

Failure scenario: the model plaza and compare estimator display nonsensical prices, and any future BE-07 consumer of these rates would overcharge by 1e6.

Fix: drop the quantity multiplication. The rate amount should be `value × multiplier`; e.g. `money.NewRate("USD", decimalText(value), unit, per)` then multiply the amount by `multiplier` and rebuild with the same `per`. Add a numeric unit test for `legacyRate` (currently only `projectEndpoint` is tested).

### P2 — Anonymous catalog reads perform DB writes (owner: BE-05)

`backend/internal/modules/catalog/reader.go:22` publishes on every current public snapshot, and `postgres/store.go:46` (`Publish`) runs an INSERT `catalog_publications` + evidence + outbox statement even when the version already exists (`ON CONFLICT DO NOTHING`). Every unauthenticated `GET /api/v2/public/models` therefore issues a write statement and outbox attempt on the publications tables.

Failure scenario: high public traffic (or a simple anonymous crawl) amplifies database writes and outbox churn with no rate limiting except the panel public limiter; a read endpoint becomes a write amplification vector.

Fix: short-circuit publish when the version already exists (cheap cached/conditional read before write), or move publication to a background writer keyed on catalog version change.

### P3 — Public `region` / `retention` filters can never match (owner: BE-05)

`backend/internal/control/catalog/models/list.go:98` filters region/retention against `d.Endpoints`, but the public projection redacts endpoints (`reader.go` `projectModel` sets `Redactions=["endpoints",…]` and leaves `Endpoints` empty). The API contract advertises `region`/`retention` on `GET U/models`, so those filters silently return an empty list. Fix: either document them as scoped-only on public, or expose region/retention as a redacted-level facet.

### Verified correct (no finding)

- Cross-scope redaction: `postgres/store.go` `validatePublic` rejects any publication with scoped evidence/scope/endpoints/private price source; `priceFor` only emits public rates from `public_plaza_reference` and scoped rates from `offer.Access` groups.
- Metrics scoping: `postgres/metrics.go` `metricQuery` binds `k.user_id=$1 AND k.workspace_id=$2` and project ownership — no cross-workspace metric leak.
- Cursor: HMAC-signed (`list.go:sealCursor/openCursor`), perspective+filter digest match, `SnapshotTTL` and `catalog_version` mismatch produce `410 SNAPSHOT_EXPIRED`.
- Unknown ≠ 0: `unavailableMetrics` returns nil request/ttft/speed with `withheld`/`unavailable`/`insufficient_samples`; zero requests is only emitted when it is an observation, with sample-state carried alongside.

---

## 2. BE-06 Execution journal

No P0/P1 found. The required invariants hold in code:

- Idempotency hash: `journal/journal.go:Register` canonicalizes JSON (`UseNumber`, duplicate-key rejection) and stores `sha256(key)` + `sha256(canonical payload)`; migration `295_execution_journal.sql` has the unique index on `(workspace_id,project_id,actor_id,api_key_id,route,idempotency_hash)`.
- Same key / different payload → `postgres.go:Admit` compares `storedFingerprint` and returns `ErrConflict`, mapped to HTTP 409 in `gateway_request_plan.go:beginResponsesExecution`.
- Admission failure rollback: both call sites (`gateway_handler_responses.go:79`, `openai_gateway_handler.go:533`) `defer finishExecution()` and return on `!admitted`, so the plan/budget is finalized even for 204/409/403/503.
- Receipt link: trigger `gateway_execution_bind_receipt` rejects receipts whose dispatched attempt is `abandoned_uncertain` or `not_sent`, or whose key/funder mismatches the execution.
- Reconciliation read-only: `gateway_execution_reconciliation` is a `VIEW`.
- Abandoned never charged: `Decision` forces `acceptance='unknown'` for `abandoned_uncertain`; `Observe` cannot overwrite a decided/abandoned attempt; `journalReceiptAllowed` returns false for the abandoned account.

Findings (all P3):

- P3 · `backend/internal/gateway/execution/journal/postgres.go:Observe/Finish` do not check `RowsAffected`, so a silently-missed update is not surfaced to the caller. Intentional for idempotent observe/finish, but wrap with a debug assertion or return the count for telemetry.
- P3 · Hot path: `InstallExecutionJournal` (`backend/internal/handler/gateway_request_plan.go:18`) adds admit+dispatch+observe+decision+finish synchronous writes only on `/v1/responses`, `/responses`, `/backend-api/codex/responses`, and the async image generation routes. That bounds the blast radius correctly, but `INTEGRATION_LOG.md` records no per-request overhead baseline for these routes; measure and record it before BE-24.

---

## 3. BE-09 Restore

No P0/P1 found. Verified:

- Journal/target separation: `modules/ops/restore/journal.go:Initialize` requires distinct `pg_control_system()` cluster identities for journal, production and isolated DBs; the journal schema lives in its own DB.
- Lease generation/CAS: `worker.go:claim` uses `FOR UPDATE SKIP LOCKED` and increments `generation` on reclaim; every mutation is fenced by `WHERE id AND generation AND owner AND state='running' AND lease_until>now()` with a `RowsAffected==1` check (`changed()`).
- Stale worker cannot write success: an expired-owner claim bumps the generation, so the old worker's `stage`/`finish` affect 0 rows → `ErrLeaseLost`.
- Rollback safety: `apply` snapshots the target before `applying`; a failure at/after `applying` transitions to `unknown` (not `failed`), so a half-applied restore is never reported as clean failure.
- Secret hygiene: `Archives.Record` stores only the object locator; `Operation.Report` contains digest/counts/reason only — no DSNs, passwords or archive credentials reach responses or audit bodies.
- Step-up: `Execute` requires step-up for `production`, and `apply` re-checks `RequireStepUp` immediately before the production restore.
- Legacy single-node fallback: `legacy/BE-09/runtime.go:New` falls back to the legacy in-process restore control when both DSN env vars are absent; the v2 endpoints then return `503 RESTORE_UNAVAILABLE` rather than running unsafe.

Findings (all P3):

- P3 · `worker.go:apply` stages the full decompressed SQL dump with `os.CreateTemp("", "omnihive-restore-*.sql")` (0600). It is removed on the success path but lingers after a crash. Prefer an unlinked temp file or a post-startup sweep, and note the dump contains user data.
- P3 · `runtime.go:New` requires three separate PostgreSQL clusters; this is a heavy deployment prerequisite and the single-node `all` mode silently degrades to the legacy restore path. Document the operational requirement and the degraded-mode behavior in `INTEGRATION_LOG.md`/runbook.
- P3 · `journal.go:Import` upserts backups with a complex `WHERE`; a backup whose `record` is rewritten by a later run can leave stale review state. Confirm the `review_required` coalescing matches the legacy `RestoreStatus` mapping (low risk).

---

## 4. Integration / wiring

No drift found:

- OpenAPI regeneration (`go run ./api/v2/compose`) reproduced `backend/api/v2/openapi.yaml` byte-for-byte; `frontend/src/api/generated/v2.d.ts` covers `/api/v2/public/models*`, `/api/v2/workspaces/.../models*`, and `/api/v2/admin/restores*`.
- `go build ./...` passed; wire_gen binds the BE-09 restore runtime as an `identity.OperationReader` and the catalog handler into the router.
- Route auth/audit: `restorehttp.Register` is mounted with `jwtAuth → panelRateLimiter.Global() → auditLog` (`router.go:171`); restore actions are `platform`-category and therefore only grantable to global admins via `identity.AccessEvaluator`. Catalog public routes use `PublicIP` limiter + `BackendModeUserGuard`; scoped routes use JWT + heavy limiter + audit.
- Worker shutdown: `cmd/server/main.go:221` calls `stopRestores()` before the 20s `shutdownApplication`, so the restore worker is drained before HTTP shutdown.

---

## 5. ID-SWEEP compat renames (round-1 follow-up)

The dual-accept renames do not break old clients:

- WS subprotocol: `admin_auth.go:17-18` accepts both `omnihive-admin` and `sub2api-admin`; `sanitizeAdminWebSocketProtocol` negotiates the new protocol first and falls back to legacy for old clients; the frontend offers `['omnihive-admin','sub2api-admin']`. Verified correct.
- Usage-source header: `cursor_gateway_compat.go:422-423` emits both `X-OmniHive-Usage-Source` and `X-Sub2API-Usage-Source`. Old consumers keep working.
- Plugin identity: `plugin_manager.go:32-33` dual Redis prefixes, `plugin_package.go:27-29` legacy built-in IDs retained, and `plugin_compatibility.go:842` requires the `OmniHive` manifest field (empty `Sub2API`) for new manifests. Correct BE-20 handoff.
- NOTICE added at repository root crediting Wei-Shaw/sub2api, with `IDENTITY_ALLOWLIST.md` classifying the remaining 740 lines (13 attribution / 495 persisted-compat / 218 historical docs / 14 defects).

Remaining round-1 identity defects (routed but still open, in-progress owners):

- P1 · `frontend/public/logo.svg:2` still reads `OmniHive · Sub2API` in the SVG accessible title. → FE-WP1
- P1 · `frontend/src/views/setup/SetupWizardView.vue:560` still defaults `dbname: 'sub2api'` and `:144` still uses placeholder `sub2api`. The backend default was fixed (`config.go:2242` → `omnihive`, with install-lock legacy fallback at `config.go:1868`), but a fresh install through the wizard still submits `sub2api`. → FE-WP1
- P2 · `frontend/src/views/admin/AccountsView.vue:2369` exports `sub2api-account-<ts>.json`; `BatchImageGuideView.vue:811` IndexedDB name `sub2api-batch-image-preview-cache`. → FE-WP4 / FE-WP1 (D08)

Round-1 P0/P1 resolved in this window: HTML title, `stores/app.ts`, `router/title.ts`, `EmailVerifyView`, compliance phrases, onboarding SDK/CLI snippets, `tls.sub2api.org` link, and the DS-A11Y contrast + `IconButton.ariaLabel` fixes (recomputed: muted light `#666c79`/dark `#828ba0` now ≥4.74:1; light accent-active `#c56207`+`#141008` = 4.63:1; dark = 5.95:1).

---

## Plan requirements still uncovered

Merged/in-progress at this tip: B0, A0, BE-01..06, BE-09, D0, DS-A11Y, ID-SWEEP, INT, FE-WP1, FE-WP4.

Still uncovered by any merged or in-progress package:

- F01 authenticated Playground — BE-13
- F02/F03/F04 routing explain / simulator / policy templates — BE-08, BE-10
- F05 model-plaza frontend (backend catalog now merged) — FE-WP2
- F06 unified task center — BE-15
- F07 full protocol-conversion quality — BE-12
- F08 remaining granular permissions (exports/cache/artifacts/async) — BE-14, BE-16, BE-21
- F10 request detail / drilldown — BE-14
- F13 plugin v2 host — BE-20
- F14 catalog verification lifecycle — BE-27
- F15 BYOK self-service — BE-17, BE-28
- F16 cost & quota workbench — BE-16
- F17 public status / incidents — BE-18
- B04 exact pricing authority — BE-07
- B05 exact settlement / wallet / evidence — BE-11
- B07 task ownership / artifact authorization — BE-15
- D04 float64 migration in settlement/billing call sites (money type + catalog rates done) — BE-07, BE-11
- D05 reconciliation operations (BE-06 supplies the read-only VIEW) — BE-11
- FE-WP2..FE-WP7 page work (plaza, overview/keys/playground, wizard, pools, waterfall/billing, ops/settings)

---

## Prioritized P0/P1 summary

P0
- None newly introduced this round.

P1
- `BE-05` — `legacyRate` off-by-`per` inflates every catalog price and the compare quote by ~1,000,000× (`backend/internal/legacy/BE-05/pricing.go:129`).
- `FE-WP1` — `frontend/public/logo.svg:2` still exposes `Sub2API` in the accessible title.
- `FE-WP1` — setup wizard still defaults `dbname`/placeholder to `sub2api` (`SetupWizardView.vue:560,144`).
