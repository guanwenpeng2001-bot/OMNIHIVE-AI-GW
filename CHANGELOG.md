# Changelog

This project follows the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format.

## Unreleased

## 0.3.5 - Unreleased

### Fixed

- Personal console scope remains personal across Dashboard, Keys and Playground. Stale workspace/project selections no longer fall through to the first project. Default workspace membership continues to grant no project actions.
- Project Key creation, lifecycle filtering, model policies and action intersections use the selected project. Failed or unknown authorization does not reopen actions. Workspace administrators can edit action grants per project without creating an unintended project/action cross-product.
- Playground and task execution keep project-scoped credentials and validate Key capabilities and current authorization. Playground requires a usable Key-specific quote; price changes invalidate confirmation before reservation, and replay uses the source request's model and scope.
- Managed connections reject legacy account selection and bulk configuration writes. Runtime subscription observations preserve connection visibility while invalidating stale verification, through a narrowly scoped database fence.
- WorkBuddy account edits preserve saved model selections and aliases after reopening. The same hydration fix covers Gemini/Anthropic OAuth and existing Kilo/OpenCode modes.
- Public model names no longer select an arbitrary provider. Missing availability remains unknown; published state is independent of routing. Catalog, pricing layouts, details and command search use the same model IDs and price presentation.

### Changed

- Model facts retain field-level provenance for the reviewed registry, platform presets, pi-ai references and operator observations. Reference prices remain separate from sales tariffs, cost accounting and settlement.
- Existing operator group/channel prices remain the sole editable sale-price authority. Shared resolution produces frozen request price contexts; additive exact-price bindings unify consumers without rewriting historical charges.
- Key model rules use one canonical writer with compatible action/ownership projections. Legacy conflicts are evaluated as an intersection and reported for diagnosis.
- The model operations view connects authorized project facts, discovered accounts, groups, sale-price sources and publication. Configuration diagnostics expose redacted effective values and sources, with version-checked whole-provider OAuth resets.
- New installations explicitly default catalog network refresh and the public plaza to disabled, with JSON logging. Upgrade snapshots preserve existing effective behavior. Integration secret encryption supports a key separate from TOTP while retaining legacy ciphertext reads.

### Upgrade and rollback

- Migrations 339–341 are additive; existing migration files and billing records are unchanged. The schema floor and semantic rollback floor are separate; this release requires a safe rollback target of v0.3.5 after adopting its semantics. Do not point a v0.3.3 binary at an upgraded database as a normal rollback.
- Release packaging, online update and the native installer consume the versioned compatibility authority. Verified installed manifests cannot lower the floor, and existing release artifacts are not replaced.
- See `docs/plans/v0.3.5-unified-console-catalog.md` and the v0.3.5 delivery evidence for rollout gates and verification limits. This entry prepares the version; it does not publish a release or deploy production.

## 0.3.2 - 2026-11-22

### Added

- **Full configured platform catalog & multimodal plaza**: Configured platforms now project their complete catalog inventory (including text, image, video, audio, and embedding modalities and actions) to the Model Plaza with explicit `availability_state` (`live` vs `catalog_only`) while keeping live routing and pricing gates unchanged.
- **First-class WorkBuddy platform (CN & Global)**: Native OAuth device login, usage snapshots, model discovery, and multi-protocol gateway support (`chat_completions`, `messages`, `responses`, `generate_content`), backed by migration `335_workbuddy_platform.sql`.
- **Keyless free model lanes (OpenCode Zen Free & Kilo)**: Added `zen_free` account mode on `opencode_go` and first-class `kilo` platform (`free` keyless lane and `apikey` lane, migration `336_kilo_platform.sql`), complete with strict free-lane admission filters, sanitized upstream errors, and full frontend onboarding/monitor/key-usage UI.

### Fixed

- **Complete upstream model discovery & multimodal metadata**: Fixed Kimi Coding model discovery failure (`upstream_model_sync_discovery_unconfirmed`) and enriched multimodal modality/action/context metadata across configured providers.
- **Default workspace membership onboarding & backfill**: Automatically provisions idempotent Default workspace memberships on user creation (registration, OAuth, admin creation) with strict `projects.read` + `models.read` RBAC isolation, and backfills existing active accounts via migration `334_backfill_default_workspace_members.sql`. Migration `337_default_workspace_role_source.sql` adds `workspace_members.inherits_global_role`, repairs the role source for restored defaults whose id is not workspace `1`, and keeps automatic memberships inheriting the global role while manual role assignments stay independent. Companion migration `338` completes the backfill of automatic administrator role sources and adds the member-lookup composite index used by workspace membership queries.
- **URL allowlist toggle & connection probe diagnostics**: Fixed `URLAllowlist.Enabled=false` still passing default upstream host lists while preserving HTTPS and private-IP SSRF protections; stopped `ProbeConnection` from swallowing build/network errors as `EVIDENCE_UNAVAILABLE`/`CHECK_UNSUPPORTED` and surfaced actionable, redacted failure details in the UI.

### Security

- **Fail-closed outbound checks**: Disabling the URL allowlist no longer widens upstream reachability; HTTPS enforcement, private-host blocking and redirect refusal remain in force for connection probes, WorkBuddy login/poll and account tests.
- **WorkBuddy credential handling**: Native OAuth access and refresh tokens stay inside the encrypted credential store and are never echoed; only the non-sensitive `site`, `uid`, `domain`, `enterprise_id`, `expires_at` and `refresh_expires_at` metadata is projected back to callers, and upstream diagnostics are redacted.
- **Kilo free/paid lane isolation**: The keyless `free` lane omits `Authorization`, admits only `isFree: true` discovery, and sanitizes upstream errors; switching to `apikey` requires clearing the public credential so a free identity cannot silently become a paid one.
- **Metering and credential audit hardening**: Cache-hit receipts bind to the frozen pricing source, credential secret guard replays are rejected, and the encrypted credential backfill is replayed idempotently. Plaza `availability_state`, catalog-only entries and price projections never grant routing access; live authorization and pricing gates remain authoritative.

## 0.3.1 - 2026-11-22

### Added

- For stateless Responses requests to GPT-6.2 and later models on OpenAI API key accounts, the gateway moves top-level `instructions` into the first `developer` input message so they stay inside the cached prompt prefix (OpenAI bills top-level instructions on these models at the uncached rate). Disable with `GATEWAY_OPENAI_INSTRUCTIONS_CACHE_HOIST=false`.

### Fixed

- Update checks rank a prerelease below the release with the same version, so release candidates are offered the final release.

## 0.3.0 - 2026-11-22

### Added

- Project billing views connect quotes, request attempts, receipts and ledger movements for reconciliation.
- The console exposes model verification and routing decisions. Playground requests use project keys and verified connections.
- Restore drills use an independent journal and exercise recovery against PostgreSQL with live foreign keys.

### Changed

- Pricing lookups index normalized model names. Restore status checks reuse cached observations and distinguish journal outages.
- CI gates cover backend and frontend checks, contract validation, generated-file drift, dependency independence and deployment smoke checks.

### Fixed

- Streaming Chat usage counters retain observed values across frames. Missing usage or pricing evidence routes affected requests to review.
- Restore fencing accounts for each admitted request. Playground scheduling retains the connection selected at admission.

### Security

- Provider credentials use encrypted storage. Account scheduling isolates records with unreadable credentials.
- Outbound URL checks restrict server-side request forgery (SSRF). TOTP verification rejects replayed codes.
- Rollback rejects unauthenticated release manifests.
