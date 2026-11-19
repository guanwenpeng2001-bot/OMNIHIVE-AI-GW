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
| BE-10 | Versioned policy editor, simulation, publish and rollback | gpt-6.2-astra | codex/be-10-policy | merged | 457123325; 8 control routes integrated; migration 299 |
| BE-14 | Request list, detail and route explanation | gpt-6.2-sol | codex/be-14-requests | merged | 25170900b; routes+operation reader+openapi integrated |
| BE-15 | Fenced task and artifact center | gpt-6.2-astra | codex/be-15-tasks | merged | 34efee1f8 + wiring a4fc19ea8 (routes/recover/private store; 503 fallback); production paid image generation unverified |
| BE-20 | Plugin v2 SDK and host capability enforcement | gpt-6.2-astra | codex/be-20-plugins | merged | 93d008c2d + wiring a4fc19ea8 (GET A/extensions, .ohplugin upload) |
| BE-23 | Migration inventory and cutover tooling (prep) | gpt-6.2-sol | codex/be-23-migration | merged | afd408412; inventory/status CLI + migration 312; eligible_for_switch=false until BE-10/11/12/15/22 land |
| BE-16 | Cost, quota and reconciliation workbench | gpt-6.2-astra | codex/be-16-cost | merged | dd4ffeb6b + routes/op-reader integrated be96361a0; budget Reserve hook pending in admission |
| BE-18 | Public status and incident lifecycle | gpt-6.2-sol | codex/be-18-status | merged | a8e1c29dd + routes/collector integrated 191613128 |
| BE-17 | Tenant BYOK pilot and funding fallback | gpt-6.2-astra | codex/be-17-byok | merged | e9689ecdd + routes/op-reader integrated baa10198a |
| BE-13 | Authenticated streaming Playground | gpt-6.2-astra | codex/be-13-playground | merged | ba83c16a6 merged; production wiring in progress |
| BE-19 | Payments wiring (HandleVerified + evidence routes) | gpt-6.2-sol | codex/refactor | merged | b6329106d; legacy webhook cutover remains with BE-22 |
| BE-21 | Scoped audit, retention, controlled egress and observability | gpt-6.2-astra | codex/be-21-audit | merged | c1851422c + routes/op-reader b33051c74; observer/retention lifecycle to BE-22 |
| BE-22 | Runtime modes and lifecycle supervisor | gpt-6.2-astra | codex/be-22-runtime | merged | 15aa736b1/e6105a8bb; merged a20e01ebd; all/control/gateway/worker smoke pass |
| BE-24 | Release verification | gpt-6.2-astra | codex/be-24-release | merged | 60s isolated rerun at edac3ce6c: J01–J06 and all executable checks pass; TYPESCRIPT/SDK_HTTP offline caches and PAIRED_2H/WIRE_PARITY/CUTOVER remain environment gates |
| WP1 | Shell, brand, IA, D08, auth/setup | gemini | codex/fe-wp1-shell | merged | c7a91b892; typecheck/lint/i18n/5295 tests/build pass |
| RV-2 | Review round 2 (BE-05/06/09, wiring, identity sweep) | deepseek-v4-pro | codex/review-1 | merged | b33cecb5a; 1×P1 (BE-05 pricing) fixed, FE-WP1 P1s in flight |
| BE-09 | Restore operation identity and fencing | gpt-6.2-astra | codex/be-09-restore | merged | d06cd4333 → integrated 988c4f942 (router/worker lifecycle/openapi/types) |
| RV-1 | Review round 1 (B0, BE-01..04, integration, design system) | deepseek-v4-pro | codex/review-1 | merged | 34fc1c6bd; 2×P0 + 5×P1 routed (FE-WP1, ID-SWEEP, DS-A11Y, BE-20) |
| ID-SWEEP | Residual identity: onboarding snippets, external links, dbname default, WS subprotocol/header dual-accept | gpt-6.2-sol | codex/identity-sweep | merged | cbcc3c23f → 259df733d; NOTICE + IDENTITY_ALLOWLIST (740 lines classified); handoffs to FE-WP1/FE-WP4; gate green (5296 tests) |
| DS-A11Y | Design-system contrast and IconButton a11y fixes | gemini-3.8-flash | codex/ds-a11y | merged | 20c2875c8; tokens AA-verified by tokens-contrast.spec.ts; IconButton ariaLabel now required |
| QG | One-command quality gate + baseline | deepseek-v4.1-flash | codex/test-gate | merged | 9bd91379a; all stages PASS at 2637e9e59; 3 fixture/env failures fixed; BE-05/09 smoke skipped (need fixtures) |
| D0 | Reference review, UI audit, design system v1, ui component library | gemini-4-carbon | codex/design-system | merged | 66717d700 → 960fbb04e |
| FE-WP1 | Shell, brand, IA, D08, auth/setup | gemini | codex/fe-wp1-shell | merged | c7a91b892; 451 files/5295 tests/build pass |
| FE-WP2 | Public site, model plaza, detail/compare, pricing, status | coordinator | codex/refactor | merged | 5f48a1e3f + 0916d617d (canonical `/model-plaza`, gateway `/models` collision resolved); 1560px responsive shell landed in c6cf8ca30 |
| FE-WP3 | Overview, keys, playground, task center, workspaces | gemini | codex/fe-wp3-console | merged | 47fc2bf23 + routes a9ef01149 |
| FE-WP4 | 5-step connection wizard, egress | gemini-4-hydrogen | codex/fe-wp4-connections | merged | 954146d71; router routes applied; typecheck + 97 tests pass |
| FE-WP5 | Pools, mapping, routing workbench and simulator | gemini | codex/fe-wp5-routing | merged | 01ff24314 + routes dff37714c; branch 5321 tests pass |
| FE-WP6 | Request waterfall, cost & quota, billing | coordinator | codex/fe-wp6-billing | merged | d4548a936 + routes e0b26ae56 |
| FE-WP7 | Ops, members, audit, backup, settings, profile | gemini | codex/fe-wp7-ops | merged | 9b26594fc + db9cbb37d; scoped audit/backup/ops/profile live-verified in c6cf8ca30 |
| FE-FIX | Responsive header, console IA, project-scope bootstrap, strict v2 reads, tour skip | gemini + coordinator | codex/refactor | merged | 9dfd9e0bd + c6cf8ca30; 104 targeted frontend tests, full frontend gate PASS on Codespace |
