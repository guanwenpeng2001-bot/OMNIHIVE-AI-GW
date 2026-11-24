# Changelog

This project follows the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format.

## Unreleased

## 0.3.6 - 2026-11-25

### Fixed

- **Free lanes and Antigravity routing**: OpenCode Zen free, OpenRouter `:free` and Kilo free models resolve to an explicit zero-price free-lane tariff, and Antigravity tier variants (for example `-high`, `-low`, `-thinking`) use their base model's rate card, so these published models are routable again instead of appearing in the plaza without a usable route.
- **Discovery after token refresh**: Routine Antigravity token renewal keeps the account's catalog identity when the project is unchanged, and post-refresh privacy writes compare against the refreshed account, so discovered models no longer go stale every refresh. Changing the project still invalidates discovery.
- **Upgrade baseline freeze**: The v0.3.4 selection-freeze marker is no longer treated as an administrator deny-all restriction on native discovery.
- **Catalog billing state**: Model billing is aggregated only from routable offers, so unroutable historical records no longer turn a consistently priced model into "priced by group" (conflict). Genuine price differences between groups are still reported.
- **In-site update checks**: Release discovery uses ETag caching and falls back to the release redirect when GitHub's anonymous API limit is reached; downloads keep checksum and signature verification. `UPDATE_GITHUB_TOKEN` is still used when set.
- **Configuration diagnostics**: Decimal money and rate settings (balances, thresholds, rates, multipliers, caps) are recognised as numbers instead of being flagged invalid.

## 0.3.5 - 2026-11-24

### Security

- Updated `github.com/gorilla/websocket` to the upstream fix for GO-2026-6278 (weak PRNG for client mask keys).

### Fixed

- **OAuth re-authorization**: Re-authorizing an existing OAuth account whose credentials are stored encrypted no longer fails with "Credential version changed". The confirmation writes against the credential revision captured at review, so a genuine concurrent rotation still returns a conflict and stale drafts must be reviewed again.
- **Scheduler catalog updates**: Busy bucket rebuilds after model catalog or group changes are retried with backoff instead of logging an error every poll; events are acknowledged only after the rebuild is applied.
- **Admin settings**: Numeric settings (balance, concurrency, SMTP port, retention and others) save as numbers instead of failing with HTTP 400; sensitive security changes use TOTP step-up; the deployment-managed Passkey RP ID is read-only.
- **Admin catalog and connections**: Model catalog quick-filter counts come from the full snapshot; the connection wizard keeps only discovered models and no longer shows "verified" before a probe runs; channel plaza publication uses the group-scoped flow; "Clear all" clears model mappings too.
- **Console**: Archived projects show their archived state; duplicate scope banners, empty-date usage requests and empty project-key action submissions are removed; MCP settings show inline validation.
- **Public pages and layout**: No horizontal overflow on mobile pricing, plaza and model detail pages; key usage and legal pages use the public shell and shared theme; Cmd+K lists docs, compatibility and compare; disabled features are not reachable by direct URL.
- **Localization**: Remaining hard-coded Chinese strings in admin, profile and settings pages are translated in both languages.

## 0.3.4 - 2026-11-23

### Added

- **Personal and project key onboarding**: Self-service personal keys, project-scoped key issuance, fail-closed scope/access checks, and a first-run checklist for starting with a personal key or joining a workspace project.
- **Model catalog provenance and 3-axis status**: Public and project catalog views expose `source`, `availability`, and `billing` independently, with per-field provenance and common facts on model details.
- **Admin model catalog**: Platform admins can inspect catalog identity, provenance, account/route offers, pricing waterfalls, and diagnostic quick filters from the admin console.
- **Configuration diagnostics**: Admin-only effective configuration API reports each setting's value, authority, source, fallback chain, restart/reset semantics, and hard/soft gate state without exposing secrets.
- **Verified scheduled backups**: Backup jobs persist scheduler state and object manifests, verify uploaded remote objects before completion, and expose scheduler health plus the last verified backup.

### Changed

- Settings now resolve through declared authority layers: `deployment` uses env > YAML > default, `database_override` uses DB > env > YAML > default, `database_only` uses DB > default, and versioned artifacts remain artifact-only.
- Catalog refresh is opt-in with `MODEL_CATALOG_REFRESH_ENABLED=false`; the default log format is JSON.
- New-user balance and concurrency are database-authoritative defaults (`0` and `5`) and are only seeded during installation.
- Backup S3 credentials use a dedicated AES-256-GCM envelope key when `BACKUP_SECRET_ENCRYPTION_KEY` is configured, while legacy TOTP-encrypted credentials remain readable during the rollback window.
- Update and rollback eligibility is resolved from the highest of the oldest supported release, the persisted database floor, and the verified artifact floor before download or replacement.

### Fixed

- General account edits preserve model mapping and allowlist selections when `preserve_model_selection` is used, instead of silently overwriting them.
- Project key listing and issuance expose the credential only on first issuance; replays return refreshed metadata without repeating the secret.

### Security

- Effective configuration responses, logs, audits, and cache snapshots never return secret values; sensitive settings report only `configured`, source, and key metadata.
- Turnstile, WebAuthn/passkey, and Ops expose derived hard/soft gate state and fail closed when a required gate is unavailable or incomplete.
- Backup secret envelope decryption failures are never reinterpreted as plaintext.

### Upgrade notes

- Migrations `339` through `343` are additive. Back up PostgreSQL, application data, and persistent keys before upgrading.
- Configure `BACKUP_SECRET_ENCRYPTION_KEY` (64 hex characters) before storing or rotating backup S3 credentials. Existing legacy TOTP-encrypted backup credentials remain readable and are marked `migration_required` until re-encrypted.
- The v0.3.4 artifact rollback floor is `0.3.0`; the oldest supported release remains `v0.2.5`, and the database floor is not raised. v0.3.3 can still read its legacy backup fields.
- Installations that previously relied on the old code default for `model_catalog_refresh_enabled=true` now default to `false`; set the variable explicitly to opt in.

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
