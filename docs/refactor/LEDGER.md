# Refactor Ledger

Status legend: todo / doing / review / merged / blocked

| ID | Work package | Owner model | Branch | Status | Notes |
|---|---|---|---|---|---|
| B0 | Baseline + Go module independence + identity inventory/renames | gpt-6.2-sol | codex/independence | merged | c3560b4d2; follow-ups: plugin v2 signing (BE-20), protocol/persisted keys (BE-30), frontend identity (FE), live cutover (BE-24) |
| A0 | Backend current state, target architecture, work packages, API contracts | gpt-6.2-astra | codex/architecture | merged | 874358e84 |
| BE-02 | Exact Money, rate and historical fixtures | gpt-6.2-astra | codex/be-02-money | merged | 555a92ab7 |
| BE-01 | Identity, projects and access | gpt-6.2-astra | codex/be-01-identity | merged | 43daedeb8; integrated 66510a70b (smoke pass) |
| INT | Backend integration coordinator (wire/router/openapi/types) | gpt-6.2-sol | codex/refactor | doing | per-package integration + smoke |
| BE-04 | Connections, credentials and pools | gpt-6.2-astra | codex/be-04-connections | doing | based on 66510a70b |
| BE-03 | Attempt controller and stream lifecycle | gpt-6.2-astra | codex/be-03-attempt | merged | 712c2b6c6 → 50e4f9cf9 (tests pass) |
| BE-06 | Durable execution journal and idempotency | gpt-6.2-astra | codex/be-06-journal | doing | based on 50e4f9cf9 |
| D0 | Reference review, UI audit, design system v1, ui component library | gemini-4-carbon | codex/design-system | merged | 66717d700 → 960fbb04e |
| FE-WP1 | Shell, brand, IA, ScopeSwitcher, D08 storage adapter, auth/setup views | gemini-4-carbon | codex/fe-wp1-shell | doing | foundation gate for WP2–WP7 |
| FE-WP2 | Public site, model plaza, detail/compare heatmap, pricing, status, docs | gemini | — | todo | after WP1 + BE-05 |
| FE-WP3 | Overview, keys, playground, task center, workspaces | gemini | — | todo | after WP1 (+BE-13/15) |
| FE-WP4 | 5-step connection wizard, egress | gemini | — | todo | after WP1 + BE-04 |
| FE-WP5 | Pools, mapping, routing simulator | gemini | — | todo | after WP1 + BE-08/10 |
| FE-WP6 | Request waterfall, cost & quota, billing | gemini | — | todo | after WP1 + BE-14/16 |
| FE-WP7 | Ops, members, audit, backup, settings, profile | gemini | — | todo | after WP1 |
