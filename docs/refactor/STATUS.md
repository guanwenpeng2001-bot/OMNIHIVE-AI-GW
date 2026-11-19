# Refactor status snapshot

Baseline of this snapshot: `codex/refactor` @ `edb69d377` (pushed to origin).
Authoritative detail lives in `LEDGER.md`, `INTEGRATION_LOG.md`, `QUALITY_GATE.md` and `reviews/`.

## Merged and integrated (backend)

BE-01 identity · BE-02 money · BE-03 attempt lifecycle · BE-04 connections ·
BE-05 catalog · BE-06 execution journal · BE-07 pricing · BE-08 routing planner ·
BE-09 restore · BE-10 policy · BE-11 settlement · BE-12 protocol split (first slice) ·
BE-13 playground · BE-14 requests · BE-15 tasks · BE-16 cost workbench ·
BE-17 BYOK pilot · BE-18 status · BE-20 plugin v2 · BE-21 audit · BE-22 runtime modes ·
BE-23 prep · BE-24 verification scaffold · BE-25 media · BE-26 plugin actions ·
BE-27 catalog verification · BE-28 full BYOK · BE-29 telemetry · BE-30 retirement inventory.

Identity separation (module path, NOTICE, release/installer identity, dual-accept
WS subprotocol and usage-source header, `.ohplugin`) is merged.

## Merged and integrated (frontend)

Design system v1, WP1 shell/brand/auth, WP2 public site (home, models, detail,
compare, pricing, status) + visual QA, WP3 console (overview, keys, playground,
tasks, workspaces), WP4 connection wizard, WP5 pools/routing simulator,
WP6 requests/billing, WP7 members/settings/audit/backup/ops/profile.

## Verification status

- Frontend gate at 24eab6e33: PASS (i18n, typecheck, lint, 468 files / 5357 tests, build).
- Contracts + generated-drift + independence: PASS on codespace.
- Backend full suite at 24eab6e33: FAIL on environment-specific and residual tests
  (dedicated-DB tests rejecting the generic DSN, one Redis port test, credential
  draft integer `1.0`, media/embeddings assertions). Earlier full suite passed at
  958fdedb1, before BE-25..30 merges.

## Known gaps

- BE-19 live webhook still uses the legacy `HandlePaymentNotification`; the exact
  `payments.HandleVerified` path is wired in the v2 API but not the live callback
  until BE-22 cutover.
- WP5 routing UI and WP6 billing UI not started.
- FE-WP4 account export filename still uses the legacy `sub2api-account-*.json`.
- BE-25..BE-30 (P2) not started.
- Real provider/payment/live-cutover verification cannot be done without
  production credentials; all such items are marked pending in the reports.

## How to verify

```sh
bash scripts/quality-gate.sh            # full gate (see docs/refactor/QUALITY_GATE.md)
bash contracts/release/backend/verify.sh
```

Heavy builds run locally with `oh-remote local -- <cmd>`; integration tests use
`oh-remote run <worktree> -- bash -c '...'` (scheduler across laptop/codespace).
