# Refactor status snapshot

Baseline of this snapshot: current `codex/refactor` delivery tip (pushed to origin; see PR #6).
Authoritative detail lives in `LEDGER.md`, `INTEGRATION_LOG.md`, `QUALITY_GATE.md`,
`reviews/` and the BE-24 report referenced below.

## Merged and integrated (backend)

BE-01 identity · BE-02 money · BE-03 attempt lifecycle · BE-04 connections ·
BE-05 catalog · BE-06 execution journal · BE-07 pricing · BE-08 routing planner ·
BE-09 restore · BE-10 policy · BE-11 settlement · BE-12 protocol split ·
BE-13 playground · BE-14 requests · BE-15 tasks · BE-16 cost workbench ·
BE-17 BYOK pilot · BE-18 status · BE-19 payments · BE-20 plugin v2 · BE-21 audit ·
BE-22 runtime modes · BE-23 migration prep · BE-24 verification scaffold ·
BE-25 media · BE-26 plugin actions · BE-27 catalog verification ·
BE-28 full BYOK · BE-29 telemetry · BE-30 retirement inventory.

Identity separation (module path, NOTICE, release/installer identity, dual-accept
WS subprotocol and usage-source header, `.ohplugin`) is merged. The legacy default
site subtitle was removed, and the user MCP/onboarding/audit/routing/rankings
route regressions introduced by the runtime-mode split were restored.

## Merged and integrated (frontend)

Design system v1, WP1 shell/brand/auth, WP2 public site (home, model plaza,
detail, compare, pricing, status), WP3 console (overview, keys, playground,
tasks, workspaces), WP4 connection wizard, WP5 pools/routing simulator,
WP6 requests/billing, WP7 members/settings/audit/backup/ops/profile.

The public shell now uses a 1560px wide-screen grid with aligned header/footer,
responsive navigation below `xl`, and a compact accessible menu. The console
sidebar exposes Playground, request/task, billing, pools, routing, reconciliation
and project-audit surfaces. Strict v2 reads no longer receive an implicit browser
`timezone`; project-scoped pages select the default workspace/project on first
use. The first-run tour only auto-starts on the dashboard and always exposes an
enabled skip/exit action.

## Verification status

- Frontend full gate at `edac3ce6c` (Codespace): PASS — i18n, typecheck, lint,
  468 files / 5,357 tests / 1 skipped, production build.
- Backend full suite at ancestor `f44eeec65`: PASS (92 packages default,
  97 packages `-tags=unit`) on isolated PostgreSQL 16 and Redis 7.
- Backend targeted suites at `edac3ce6c`: PASS for routes, handler, tasks, BYOK,
  connections, playground, identity, tasks controller and server lifecycle.
- `golangci-lint` v2.15.0 at `edac3ce6c`: PASS (0 issues).
- Quality gate `contracts`, `generated-drift`, `independence`: PASS.
- BE-24 isolated candidate `edac3ce6c`, 60-second load: J01–J06 PASS and all
  executable OPENAPI/GROUPED_PLAYGROUND/SURFACES_HTTP/PROTOCOL_HTTP/PUBLIC_NATIVE/
  CALLBACK_HTTP/ACCESS/LOAD/MATRIX/REGISTRY/STATUS_MODULE/FINANCE/MIGRATION/
  RESTORE/CUTOVER_FIXTURE checks PASS. Report:
  `contracts/release/backend/reports/20261119T182146Z-edac3ce6c/summary.md`.
- Live local deployment at `http://127.0.0.1:18080` built with `-tags embed`;
  public home/model plaza, login, dashboard, requests, tasks, playground,
  billing, routing and project audit were exercised in a real browser at
  360/768/1920px.

## Remaining environment gates

These are not code-completion items and require external resources:

- `TYPESCRIPT` and `SDK_HTTP` in the release verifier need offline
  `openapi-typescript@7.10.1` and `openai@4.104.0` tool caches. The normal
  generated-drift gate and frontend build already verify the checked-in types.
- `PAIRED_2H`, `WIRE_PARITY` and `CUTOVER` require the final deployment window,
  real provider credentials/traffic and release-owner sign-off.
- Real provider protocol matrices, BYOK approvals, media provider spend and
  payment webhook cutover require production credentials; no fake success is
  reported.

## How to verify

```sh
bash scripts/quality-gate.sh
contracts/release/backend/verify.sh
BE24_TOOLS=.quality-gate/tools \
  contracts/release/backend/verify.sh --full 60
```

Heavy builds and full frontend/integration suites run on the Codespace or
laptop via `oh-remote run`; local commands are limited to edits, git, typecheck,
targeted tests and the live embedded deployment.

## Delivery

- Integration branch: `codex/refactor` (pushed).
- Pull request: https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/pull/6 → `dev`.
- Final visual evidence: `docs/refactor/design/screens/final2/` (102 captures,
  17 routes × light/dark × 360/768/1440).
