# Refactor Ledger

Status legend: todo / doing / review / merged / blocked

| ID | Work package | Owner model | Branch | Status | Notes |
|---|---|---|---|---|---|
| B0 | Baseline + Go module independence + identity inventory/renames | gpt-6.2-sol | codex/independence | merged | c3560b4d2; follow-ups: plugin v2 signing (BE-20), protocol/persisted keys (BE-30), frontend identity (FE), live cutover (BE-24) |
| A0 | Backend current state, target architecture, work packages, API contracts | gpt-6.2-astra | codex/architecture | merged | 874358e84 |
| BE-02 | Exact Money, rate and historical fixtures | gpt-6.2-astra | codex/be-02-money | merged | 555a92ab7 |
| BE-01 | Identity, projects and access | gpt-6.2-astra | codex/be-01-identity | merged | 43daedeb8; integrated 66510a70b (smoke pass) |
| INT | Backend integration coordinator (wire/router/openapi/types) | gpt-6.2-sol | codex/refactor | idle | BE-01, BE-04, BE-05, BE-06, BE-09 integrated |
| BE-04 | Connections, credentials and pools | gpt-6.2-astra | codex/be-04-connections | merged | b58c783e7 → fd601fcb6; integrated c7c7d0961 (smoke pass) |
| BE-05 | Capability evidence and public catalog projection | gpt-6.2-sol | codex/be-05-catalog | merged | c6270bee6 + 660e876e6; RV-2 P1 legacyRate 百万倍 → fixed a345b79d0 + regression tests |
| BE-03 | Attempt controller and stream lifecycle | gpt-6.2-astra | codex/be-03-attempt | merged | 712c2b6c6 → 50e4f9cf9 (tests pass) |
| BE-06 | Durable execution journal and idempotency | gpt-6.2-astra | codex/be-06-journal | merged | 39b054030 → integrated 146fc5cdb (journal middleware in router; build/vet/unit pass) |
| BE-07 | Exact pricing authority, quotes and comparisons | cloud C1 | codex/cloud-c1-be07 | merged | d58d645b7 merged; build + pricing/compare/quote tests pass; wiring pending |
| BE-08 | Shared routing planner and production trace | cloud C2 | codex/cloud-c2-be08 | merged | bc6c91707; migration 297 moved to backend/migrations; planner/scheduler tests pass; authorization snapshot wiring pending |
| BE-24A | Release verification scaffold (OpenAPI freeze + J01–J06 fixtures + runbooks) | cloud (C5) | codex/cloud-c5-be24a | merged | 8bb24d441 → merged; verify.sh exit 0 at tip (OpenAPI pass, 6 journeys pass; HTTP/MATRIX/FINANCE/ACCESS/MIGRATION/LOAD pending) |
| BE-11 | Exact settlement, wallet and evidence assembly | cloud C3 | codex/cloud-c3-be11 | merged | 3076832be merged; service settlement tests pass; mixed-version acceptance pending |
| BE-12 | Gateway protocol and provider split | cloud C4 | codex/cloud-c4-be12 | merged | 6aae24a8f (first slice); golden matrix/remaining providers pending |
| RV-2 | Review round 2 (BE-05/06/09, wiring, identity sweep) | deepseek-v4-pro | codex/review-1 | merged | b33cecb5a; 1×P1 (BE-05 pricing) fixed, FE-WP1 P1s in flight |
| BE-09 | Restore operation identity and fencing | gpt-6.2-astra | codex/be-09-restore | merged | d06cd4333 → integrated 988c4f942 (router/worker lifecycle/openapi/types) |
| RV-1 | Review round 1 (B0, BE-01..04, integration, design system) | deepseek-v4-pro | codex/review-1 | merged | 34fc1c6bd; 2×P0 + 5×P1 routed (FE-WP1, ID-SWEEP, DS-A11Y, BE-20) |
| ID-SWEEP | Residual identity: onboarding snippets, external links, dbname default, WS subprotocol/header dual-accept | gpt-6.2-sol | codex/identity-sweep | merged | cbcc3c23f → 259df733d; NOTICE + IDENTITY_ALLOWLIST (740 lines classified); handoffs to FE-WP1/FE-WP4; gate green (5296 tests) |
| DS-A11Y | Design-system contrast and IconButton a11y fixes | gemini-3.8-flash | codex/ds-a11y | merged | 20c2875c8; tokens AA-verified by tokens-contrast.spec.ts; IconButton ariaLabel now required |
| QG | One-command quality gate + baseline | deepseek-v4.1-flash | codex/test-gate | doing | scripts/quality-gate.sh, QUALITY_GATE.md |
| D0 | Reference review, UI audit, design system v1, ui component library | gemini-4-carbon | codex/design-system | merged | 66717d700 → 960fbb04e |
| FE-WP1 | Shell, brand, IA, ScopeSwitcher, D08 storage adapter, auth/setup views | gemini-4-carbon | codex/fe-wp1-shell | doing | foundation gate for WP2–WP7 |
| FE-WP2 | Public site, model plaza, detail/compare heatmap, pricing, status, docs | gemini | — | todo | after WP1 + BE-05 |
| FE-WP3 | Overview, keys, playground, task center, workspaces | gemini | — | todo | after WP1 (+BE-13/15) |
| FE-WP4 | 5-step connection wizard, egress | gemini-4-hydrogen | codex/fe-wp4-connections | merged | 954146d71; router routes applied; typecheck + 97 tests pass |
| FE-WP5 | Pools, mapping, routing simulator | gemini | — | todo | after WP1 + BE-08/10 |
| FE-WP6 | Request waterfall, cost & quota, billing | gemini | — | todo | after WP1 + BE-14/16 |
| FE-WP7 | Ops, members, audit, backup, settings, profile | gemini-4-hydrogen | codex/fe-wp7-ops | doing | parallel with WP1; routes applied after WP1 merge |
