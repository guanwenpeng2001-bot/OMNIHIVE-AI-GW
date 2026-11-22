# Changelog

This project follows the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format.

## Unreleased

## 0.3.2 - 2026-11-22

### Added

- **Full configured platform catalog & multimodal plaza**: Configured platforms now project their complete catalog inventory (including text, image, video, audio, and embedding modalities and actions) to the Model Plaza with explicit `availability_state` (`live` vs `catalog_only`) while keeping live routing and pricing gates unchanged.
- **First-class WorkBuddy platform (CN & Global)**: Native OAuth device login, usage snapshots, model discovery, and multi-protocol gateway support (`chat_completions`, `messages`, `responses`, `generate_content`), backed by migration `335_workbuddy_platform.sql`.
- **Keyless free model lanes (OpenCode Zen Free & Kilo)**: Added `zen_free` account mode on `opencode_go` and first-class `kilo` platform (`free` keyless lane and `apikey` lane, migration `336_kilo_platform.sql`), complete with strict free-lane admission filters, sanitized upstream errors, and full frontend onboarding/monitor/key-usage UI.

### Fixed

- **Complete upstream model discovery & multimodal metadata**: Fixed Kimi Coding model discovery failure (`upstream_model_sync_discovery_unconfirmed`) and enriched multimodal modality/action/context metadata across configured providers.
- **Default workspace membership onboarding & backfill**: Automatically provisions idempotent Default workspace memberships on user creation (registration, OAuth, admin creation) with strict `projects.read` + `models.read` RBAC isolation, and backfills existing active accounts via migration `334_backfill_default_workspace_members.sql`.
- **URL allowlist toggle & connection probe diagnostics**: Fixed `URLAllowlist.Enabled=false` still passing default upstream host lists while preserving HTTPS and private-IP SSRF protections; stopped `ProbeConnection` from swallowing build/network errors as `EVIDENCE_UNAVAILABLE`/`CHECK_UNSUPPORTED` and surfaced actionable, redacted failure details in the UI.

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
