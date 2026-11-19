# OmniHive Refactor — Review Round 1

- Worktree: `codex/review-1` at `codex/refactor` `1e5012626`
- Date: 2026-11-19 (Asia/Shanghai)
- Scope: merged packages B0/A0/BE-01/BE-02/BE-03/BE-04/D0 + INT integration, against `PLAN_v1.0.md`, `CHARTER.md`, backend contracts, `INTEGRATION_LOG.md`, `LEDGER.md`.
- Method: source review plus throwaway verification (contrast math, OpenAPI regeneration, module/identity grep). No source files were modified; the regenerated OpenAPI matched byte-for-byte and the worktree is clean.

## Severity legend

- P0 — blocks release: user-visible brand/identity, security bypass, or financial correctness defect shipped in merged code.
- P1 — must fix this round: security/authorization or design-compliance defect with a concrete exploit or spec violation.
- P2 — should fix: robustness/SSRF/leak hardening, accessibility failure below AA.
- P3 — polish/documentation drift.

---

## 1. Independence (B0)

The Go module was renamed to `github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend` (`backend/go.mod:1`), the update source defaults to `guanwenpeng2001-bot/OMNIHIVE-AI-GW` with `legacyUpdateBinaryName = "sub2api"` for staged-binary migration (`backend/internal/service/update_service.go:35-36`), GoReleaser publishes `omnihive`/`omnihive_<v>_<os>_<arch>` archives, and the frontend package is `omnihive-frontend` with no sub2api dependency in the lockfile. LICENSE (LGPLv3) is retained. There is **no `NOTICE` file** in the repository; if inherited sub2api code requires an accompanying provenance/NOTICE statement, that attribution is missing (P3, owner B0/legal).

`rg -i "sub2api|wei-shaw"` counts at this tip: backend 484, frontend 274, deploy 114, docs 88, CI 1. Classification of the remaining runtime hits:

### P0 — user-visible identity still shipped (owner: FE-WP1, in progress; B0 follow-up "frontend identity (FE)")

- `frontend/index.html:7` — document `<title>OmniHive · Sub2API - AI API Gateway</title>`.
- `frontend/public/logo.svg:2` — logo `<title>OmniHive · Sub2API</title>`.
- `frontend/src/stores/app.ts:29` and `:297` — `siteName` defaults/falls back to `'Sub2API'`.
- `frontend/src/router/title.ts:11` — document-title fallback `'Sub2API'`.
- `frontend/src/views/auth/EmailVerifyView.vue:283,388` — `siteName` fallback `'Sub2API'`.
- `frontend/src/stores/adminCompliance.ts:7-8` — user-facing fallback compliance phrases name `Sub2API`.
- `frontend/src/components/keys/UseKeyModal.vue:1092-1318` — onboarding SDK/CLI snippets emit `SUB2API_API_KEY`, `model_provider = "sub2api"`, `name = "Sub2API Grok"` to end users.
- `frontend/src/components/intro/quickstart.ts:59-86` and `frontend/src/components/intro/helpExamples.ts:9-143` — onboarding examples emit `SUB2API_BASE_URL` / `SUB2API_API_KEY` / `model_providers.sub2api`.

Failure scenario: a fresh deployment shows the old brand in the browser tab, logo tooltip, and every generated SDK snippet, violating the charter's "no Sub2API identity left in user-visible UI" non-negotiable.

### P0 — live external sub2api domain in shipped UI (owner: FE-WP1)

- `frontend/src/components/admin/TLSFingerprintProfilesModal.vue:148` — link to `https://tls.sub2api.org`. This is both a brand leak and a runtime dependency on an upstream-owned domain.

### P1 — fresh installs persist a sub2api-named database (owners: FE-WP1 + BE-22)

- `frontend/src/views/setup/SetupWizardView.vue:560` — setup wizard default `dbname: 'sub2api'`.
- `backend/internal/config/config.go:2234` — `viper.SetDefault("database.dbname", "sub2api")`.

Failure scenario: every new deployment that accepts defaults creates a database named `sub2api`, so the rename is not complete for fresh installs even though existing deployments keep old names for compatibility. Fix: default to `omnihive`, keep `sub2api` only as an explicit legacy-read fallback.

### P1 — client-visible protocol/header identity (owners: BE-12 / BE-30 + FE-WP1)

- `backend/internal/server/middleware/admin_auth.go:39` + `frontend/src/api/admin/ops.ts:460,506` — Ops WebSocket subprotocol `sub2api-admin`. Needs a coordinated dual-accept rename (server accepts `omnihive-admin`, keeps `sub2api-admin` for old bundles during rollout).
- `backend/internal/service/cursor_gateway_compat.go:363` and `cursor_gateway_service.go:451` — response header `X-Sub2API-Usage-Source`. Rename to `X-OmniHive-Usage-Source` with legacy emission only while old clients are supported.
- `backend/internal/service/upstream_billing_probe.go:653,804` — `/v1/sub2api/billing` path and `object:"sub2api.key_billing"` are **legitimate wire-compat** (must remain readable to interoperate with upstream relays); keep behind BE-30 retirement tracking.

### P1 — plugin identity and signatures (owner: BE-20, deferred per LEDGER)

- `backend/internal/service/plugin_package.go:26-27` — built-in plugin ID `local.sub2api.openai-transport` and publisher key `sub2api-openai-transport-v1`.
- `backend/internal/service/plugin_manager.go:32` — Redis prefix `sub2api:plugin-ui:v1:`.
- `backend/internal/service/plugin_compatibility.go:18-38` — manifest `Requires.Sub2API` / `TestedSub2APIVersions` fields.
- `frontend/src/api/admin/plugins.ts:10-37` — same manifest fields surfaced to the admin UI.

These are persisted/signature identities: cannot be mechanically renamed without breaking v1 signatures. BE-20 must version the manifest, rebuild/resign, and map old→new.

### Persisted-compat keys (P2/P3 — owner BE-30, with FE-WP1 for `D08` storage keys)

Correctly retained as data-compat, but each needs a versioned migration + old-key purge, not a blind rename:

- `backend/internal/securityaudit/prompt_types.go:12-13` and `prompt_snapshot.go:24` — Redis channels/keys and the payload priority separator.
- `backend/internal/service/openai_live_attestation.go:29` — HMAC domain `sub2api/live-attestation/v1`.
- `backend/internal/service/openai_codex_fingerprint.go:239-257` and `openai_codex_account_identity.go:100` — fingerprint/identity HMAC domains (must stay stable or deployed clients break).
- `backend/internal/service/openai_gateway_grok.go:654` — `sub2api:grok-safety-id:v1`.
- `backend/internal/service/channel_monitor_service.go:93` — `sub2api:duplicate_operation_id`.
- `backend/internal/service/auth_identity_tickets.go:15` — `urn:sub2api:identity-ticket`.
- `backend/internal/service/openai_codex_tool_names.go:15` — `python__sub2api` tool alias.
- `frontend/src/utils/ipGeoLookup.ts:24`, `frontend/src/api/admin/groups.ts:167`, `frontend/src/components/admin/subscription/bulkSubscriptionOperation.ts:53` — legacy storage/session keys.
- `backend/internal/service/gateway_service.go:912-914` — `SUB2API_DEBUG_*` env fallbacks (deprecated, value-not-logged per IDENTITY_MIGRATION.md).
- `deploy/docker-entrypoint.sh:18,27-28`, `deploy/install.sh`, `deploy/apple-container.sh`, `deploy/docker-compose*.yml` — legacy binary/dir/service/env fallbacks (legitimate migration; time-box to BE-24 cutover).

### Minor brand leakage to upstreams (P3 — owner BE-12)

- `backend/internal/service/qianfan_actions.go:322` — `cuid` default `"sub2api"` sent upstream.
- `backend/internal/service/batch_image_provider_vertex.go:491-493` — Vertex batch display/labels `sub2api-…`.

---

## 2. BE-02 Money

No P0/P1/P2 defects found in `backend/internal/core/money`.

Verified:
- `Exact` accumulates `*big.Rat`; `RoundCharge()` uses `decimal.NewFromBigRat(...,8)` which implements half-away-from-zero (`DivRound`), matching the plan's only charge rounding boundary. The golden fixture `0.000078125 → 0.00007813` and reservation ceiling `0.00007813` are covered (`money_test.go`).
- `ReserveCeil()` does exact `ceil(v·10⁸)/10⁸`; negative reservation is rejected.
- `Rate.Cost()` converts time units exactly via `big.NewRat(from,to)` and divides by `per` exactly; no float anywhere.
- JSON/SQL paths are strict: strings only, unknown/duplicate fields rejected, NaN/exponent/numeric JSON rejected (`wire.go` `stringFields`).

Findings:
- P3 · `backend/internal/core/money/amount.go:30-39` — `Currency.valid()` accepts any three uppercase letters; the plan says "checked three-letter code", which could be read as ISO-4217 membership. Current behavior is format-only (e.g. `ZZZ` passes). Confirm intent; if ISO membership is required, add a code set.
- P3 · `backend/internal/core/money/comparison.go:22-34` — `ParseV1Number` uses `strconv.ParseFloat` for exponent-range validation only; the stored value is parsed via `decimal.NewFromString`, so this is acceptable for historical replay (no value is computed through float64). Note it in the file's contract to prevent future "why float" flagging.
- Open (not a BE-02 defect): legacy billing code still contains `float64` price/amount fields (D04). The money type is delivered but the migration of pricing/settlement paths belongs to BE-07/BE-11.

---

## 3. BE-01 Identity / access

The scope model, restricted-key policy and idempotency are sound in the reviewed paths. No authorization bypass found.

Findings:
- P2 · `backend/internal/modules/identity/postgres/commands.go:96-118` — `identity_create_receipts` rows are never expired or cleaned. Idempotency receipts accumulate forever (one row per distinct key per actor/route). Add a bounded retention job.
- P3 · `backend/internal/modules/identity/postgres/store.go:169-175` (`Keys`) — `version` is hardcoded to `1` in the SELECT; `If-Match` on keys will therefore always compare against `1` until a real version is wired. Confirm this is intentional placeholder or add a version column.
- P3 · `backend/internal/modules/identity/postgres/commands.go:SetGrants` — the grant action allowlist is `category IN ('member','project','key')`, so `text.chat`/`image.generate` (key-category actions) can be attached to a member grant even though grants only drive console `Evaluate`/`RequireResource`, not gateway dispatch. Harmless today but over-broad; restrict member grants to `member`/`project` categories unless a key-category grant has a defined meaning.
- P3 · `backend/internal/control/identity/handler.go:97-117` (`fail`) and `backend/internal/control/connections/handler.go:145-166` (`fail`) — identity includes `"data":nil` in the error envelope, connections omits `data`. Align both to the API_CONTRACTS envelope `{code,message,reason,metadata,data}`.

Verified correct (no finding): cross-workspace membership filter, the `identity_resource_owners` one-owner-or-quarantine rule, `member_bound/issued_membership` key revocation on member exit, idempotency fingerprint conflict (409) on same-key/different-input, and the `oh_p_` credential fallthrough to legacy lookup.

---

## 4. BE-03 Attempt controller

No double-charge or unbounded-generation defect found. The bounded silent-first-output replay exception is implemented correctly:

- `backend/internal/service/attempt_budget.go:261-277` (`interruptGeneration`) sets `AbandonedUncertain` only when the response is still `PreOutput` and not replay-blocked (zero semantic/downstream bytes).
- `backend/internal/service/attempt_budget.go:296-305` (`DecideAttempt`) allows the single replacement only when `interrupted` matches the failed account, then pins `replayFrom`/`replayExcluded`.
- `backend/internal/gateway/execution/attempt/controller.go:97-102` switches to a *different* account (`FirstOutputTimeoutAbandoned`) and excludes the abandoned account.
- `backend/internal/handler/openai_gateway_handler.go:894-895` and `:3051` skip usage recording for an abandoned replacement, preventing double charge.

Findings:
- P3 · `backend/internal/service/attempt_budget.go:270` — `b.mu.Lock()` is taken after `beginGenerationAttempt` releases `admissionMu`; `Commit()` (`:251`) and `BeginAttemptStreamTurn` use the same `mu`, so ordering is consistent. No defect; the nested `checkSendLocked` double-check is correct.
- Observation: the exception relies on the caller passing `silentFirstOutputTimeout=true`; three WS/HTTP sites pass `false` unconditionally (`openai_ws_forwarder_ingress.go:1877`, `openai_gateway_response_handling.go:394,463`, `openai_ws_v2_passthrough_adapter.go:1222`). Confirm those paths are genuine terminal/client-disconnect cases where replay must remain forbidden (they are, per the zero-byte guard), but any future "first-output timeout" path must thread the flag explicitly.

---

## 5. BE-04 Connections / credentials

No secret was found to leak into responses, logs, audit rows, or drafts in the reviewed paths: credential bodies are omitted from audit (`audit_log.go:174-176`), `Project()` redacts `credential` without `credentials.read_metadata`, `projectReceipt` deletes `credential_version` for non-owners, and `ProjectKey` clears `PasswordHash`/`TotpSecretEncrypted` (`legacy/BE-01/key.go`).

Findings:
- P2 · `backend/internal/modules/connections/model.go:ValidEndpoint` — accepts any `https://…` host, including loopback/link-local/internal hostnames. For *new* BE-04 connections this is mitigated at verify time by `ProbeConnection` (`account_test_service.go:3291-3307`, `ValidateResolvedIP:true`, `AllowPrivateHosts` gated on `URLAllowlist`) and activation requires an eligible verification, so dispatch cannot be activated against a private host. Residual risk is legacy accounts that pass the backfill but are already `active` and dispatch through the legacy egress path. Fix: reject private/loopback/link-local hosts in `ValidEndpoint` (and backfill `ENDPOINT_REVIEW`) unless `URLAllowlist.AllowPrivateHosts` is explicitly enabled; BE-21 must close the legacy dispatch egress path.
- P2 · `backend/internal/legacy/BE-04/migrate/backfill.go:118-126` — inline-secret conversion calls `ChangeSecret` inside `SAVEPOINT before_conversion` and rolls back on `ErrCredentialSecretInvalid`, but only that one error triggers rollback+quarantine. A decryption failure surfaced as a different error type during `EncryptVerifiedCredential` could abort the batch instead of quarantining. Tighten the error classification so any "credential unreadable" shape quarantines.
- P3 · `backend/internal/modules/connections/workflow.go:Rotate` — `replacement.Operation` is forced to `"set"` after rejecting a caller-supplied non-empty value; the intent is fine but the error for a supplied value is `INVALID /replacement/operation`. Use a clearer reason (e.g. `ROTATION_SET_ONLY`).
- Verified correct: migration 293's `connection_snapshot_credential` fence (`IF UPDATE AND NEW.version <= OLD.version → RAISE`), the `provider_connections.version` bump on credential change, the legacy-edit `legacy_dirty` fence, and `ChangeSecret` CAS `WHERE … version=current`.

---

## 6. Integration (Wire / router / OpenAPI)

- `backend/internal/server/router.go:152-159` — v2 identity/connections routes are mounted with `TenantHeaders → jwtAuth → panelRateLimiter.Global() → auditLog`; the v1 OAuth bridge uses the same chain. No v2 route is registered without auth or audit.
- `backend/cmd/server/wire_gen.go:359-369` — identity `postgres.Store` is bound as `GatewayScopes`/`AccessEvaluator` and shared with the connections store; operations reader registration is correct.
- OpenAPI regeneration (`go run ./api/v2/compose`) reproduced `backend/api/v2/openapi.yaml` byte-for-byte; `frontend/src/api/generated/v2.d.ts` covers all v2 paths. No generated-type drift found at this tip.
- P3 · `backend/internal/server/router.go:159` — the v1 authorization bridge is registered under `/api/v1` but the audit body-omission key in `audit_log.go:176` is `PUT /api/v1/workspaces/:workspace_id/connections/:id/authorization`; this matches, so no change needed, but the route string is duplicated in two places — extract a constant to prevent drift.

---

## 7. Design System v1 (`frontend/src/components/ui`, tokens)

Component accessibility is strong: Modal/Drawer use `role="dialog"`, `aria-modal`, Teleport, focus trap and Escape (`utils.ts` `useOverlayDialog`/`getFocusableElements`); Button sets `disabled`/`aria-busy`; IconButton supplies an accessible name; Table/Switch/Tabs are covered by tests. No mock data was found in shipped UI primitives.

Contrast verification (computed WCAG ratios; DESIGN_SYSTEM.md `docs/refactor/design/DESIGN_SYSTEM.md:34-44`):

- P2 · `frontend/src/styles/tokens.css:26-27` — light `--oh-accent-active` `#B45309` with `--oh-accent-ink` `#141008` = **3.78:1**, below WCAG AA 4.5:1. The design system's contrast rule ("primary amber fills always use --oh-accent-ink … 9.8:1") does not hold for the pressed/active primary-button state. Fix: use `#FBBF24` (or `#FFFFFF`-safe ink) for the active fill, or darken the active fill to keep `≥4.5:1` with `#141008`.
- P2 · `frontend/src/styles/tokens.css:15,121` — `--oh-fg-muted` fails AA on the surfaces where it is actually used: light `#6E7482` on canvas `#F7F8FA` = 4.41:1 and on subtle `#F1F3F6` = 4.21:1; dark `#7A8394` on elevated `#1C202B` = 4.26:1. The doc claims `>= 4.6:1`. Fix: darken light muted to `#666C79` and lighten dark muted to `#828BA0`, or restrict muted to `bg-surface`/`bg-canvas` where it passes.
- P3 · `docs/refactor/design/DESIGN_SYSTEM.md:44` — two contrast claims are overstated: `--oh-accent-ink` is **8.83:1** not `9.8:1`, and dark `--oh-accent-text` `#FBBF24` on surface is **10.52:1** not `11.6:1` (light `#B45309` = 5.02:1 vs claimed `5.1:1`). All still pass AA/AAA, so correct the documentation.
- P3 · `frontend/src/components/ui/IconButton.vue:18-32` — the spec says `ariaLabel` is **required**; the implementation makes it optional and falls back to `'Button'`. Enforce required `ariaLabel` or derive from `label`.

---

## Plan requirements not yet covered (merged + in-progress only)

Merged/in-progress: B0, A0, BE-01, BE-02, BE-03, BE-04, D0, INT, BE-05, BE-06, BE-09, FE-WP1, FE-WP4.

Still uncovered by any merged or in-progress package:

- F01 (authenticated Playground) — BE-13
- F02, F03, F04 routing explain / simulator / cross-provider policy templates — BE-08, BE-10
- F05 model-plaza frontend (backend BE-05 in progress) — FE-WP2
- F06 unified task center — BE-15
- F07 full protocol conversion quality (BE-03 covers only attempt/lifecycle slice) — BE-12
- F08 remaining fine-grained permissions (exports, cache, artifacts, async) — BE-14, BE-16, BE-21
- F10 request detail / drilldown — BE-14
- F13 plugin v2 host — BE-20
- F14 catalog verification lifecycle — BE-27
- F15 BYOK self-service — BE-17, BE-28
- F16 cost & quota workbench — BE-16
- F17 public status / incidents — BE-18
- B04 exact pricing authority — BE-07
- B05 exact settlement / wallet / evidence — BE-11
- B07 task ownership/artifact authorization — BE-15
- D04 float64 migration in billing/settlement paths (money type exists, call sites not migrated) — BE-07, BE-11
- D05 receipt-recovery reconciliation surface — BE-06, BE-11
- FE-WP2..FE-WP7 page work (plaza, overview/keys/playground, wizard, pools, waterfall/billing, ops/settings) — FE workstream

---

## Prioritized P0/P1 summary

P0
1. FE-WP1 — shipped user-visible `Sub2API` identity: HTML/logo titles, `siteName` fallbacks (`app.ts`, `title.ts`, `EmailVerifyView`), compliance phrases, and onboarding SDK/CLI snippets (`UseKeyModal`, `quickstart.ts`, `helpExamples.ts`).
2. FE-WP1 — live external link `https://tls.sub2api.org` in `TLSFingerprintProfilesModal.vue`.

P1
3. FE-WP1 + BE-22 — fresh installs default `database.dbname` to `sub2api` (`SetupWizardView.vue:560`, `config.go:2234`).
4. BE-12/BE-30 + FE-WP1 — client-visible protocol identity: `sub2api-admin` WS subprotocol and `X-Sub2API-Usage-Source` header need coordinated rename.
5. BE-20 — plugin identity/signatures (`local.sub2api.openai-transport`, manifest `Requires.Sub2API`, `sub2api:plugin-ui:v1`) need versioned migration/resign.
6. D0 (design system) — light primary-button pressed state fails AA (3.78:1) and `--oh-fg-muted` fails AA on canvas/subtle/elevated surfaces; contrast claims in DESIGN_SYSTEM.md are overstated.
7. BE-30 — persisted-compat data/coordination keys (`prompt_audit`, codex/attestation/grok HMAC domains, storage keys) remain on old namespaces pending versioned migration.
