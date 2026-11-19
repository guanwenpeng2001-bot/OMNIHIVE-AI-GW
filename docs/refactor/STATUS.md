# Refactor status snapshot

Baseline of this snapshot: `codex/refactor` @ `b80fe3170` (pushed to origin).
Authoritative detail lives in `LEDGER.md`, `INTEGRATION_LOG.md`, `QUALITY_GATE.md` and `reviews/`.

## Merged and integrated (backend)

BE-01 identity · BE-02 money · BE-03 attempt lifecycle · BE-04 connections ·
BE-05 catalog · BE-06 execution journal · BE-07 pricing · BE-08 routing planner ·
BE-09 restore · BE-10 policy · BE-11 settlement · BE-12 protocol split (first slice) ·
BE-13 playground · BE-14 requests · BE-15 tasks · BE-16 cost workbench ·
BE-17 BYOK pilot · BE-18 status · BE-20 plugin v2 · BE-21 audit · BE-23 prep.

Identity separation (module path, NOTICE, release/installer identity, dual-accept
WS subprotocol and usage-source header, `.ohplugin`) is merged.

## Merged and integrated (frontend)

Design system v1 (tokens, fonts, 22 primitives, `/__design`), design-system
contrast fixes, WP4 connection wizard + egress (routes applied).

## In flight

WP1 shell/brand/auth (branch `codex/fe-wp1-shell`, finishing failing tests),
WP2 public site, WP3 console, WP7 ops/settings, BE-22 runtime modes, BE-24 release
verification, BE-19 payment wiring (main checkout, uncommitted).

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
