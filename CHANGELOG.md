# Changelog

This project follows the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format.

## Unreleased

### Added

- For stateless Responses requests to GPT-6.2 and later models on OpenAI API key accounts, the gateway moves top-level `instructions` into the first `developer` input message so they stay inside the cached prompt prefix (OpenAI bills top-level instructions on these models at the uncached rate). Disable with `GATEWAY_OPENAI_INSTRUCTIONS_CACHE_HOIST=false`.

### Fixed

- Update checks rank a prerelease below the release with the same version, so release candidates are offered the final release.

## 0.3.0 (pending release)

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
