# BE-16 cost, quota and reconciliation workbench

## Composition

Apply migration 305 after the BE-01 mappings, BE-06 journal, BE-11 wallet evidence and BE-14 request views. Compose `backend/api/v2/fragments/BE-16.yaml` with BE-01. Construct `handler.ProvideCostWorkbench(db, identityStore, totp, cfg)`, register `billing.Register(v2, workbench, sessionMiddleware...)`, and append the same handler to `identity.Operations.Readers`. Use the control session, audit and rate-limit middleware. The active gateway and aggregate registration are owned by the composition package.

Project activity, charges and quota require `billing.read`. The workspace activity endpoint and unrestricted workspace reconciliation list require the corresponding workspace-wide authorization. A delegated reconciler can pass `project_id` to the list. Resolution binds the case to its recorded project. A requested quota key requires `keys.read`. Viewing another member's personal funding requires `billing.payers.read`, a workspace membership and access to the selected project. Wallet balances and promotional credits belong to the payer across projects.

## Source evidence and migration

Complete the BE-01 historical receipt and usage mappings. BE-14 request anchors exclude quarantined and unproven mappings. Logical requests, dispatch permits and receipt identities have separate counts. Admission time or the mapped historical observation defines the half-open activity window. Per-currency groups use exact SQL numerics and return decimal strings. A legacy usage record can report a recorded positive cost estimate. Legacy default zeros without evidence stay unknown. A free settled receipt is a known zero. Receipt detail links use the BE-14 request identity. Journaled price versions are frozen at admission. Missing historical price versions stay null.

The same SQL projection supplies activity, charges and CSV. Cursor signatures bind route, actor, grants epoch, scope, filters and admission cutoff. Timestamped dispatch, settlement and hold evidence respects that cutoff. Preexisting mutable records without a transition timestamp have only their latest observation. Export registration captures selected rows in one transaction and returns an expiring 15-minute download operation. Poll and download reauthorize the creating actor, workspace, project, `billing.read` and `billing.export`. Purge expired `billing_export_rows` through a retention job using the parent command's `expires_at`. Retain command identities for the retry window and retain all commands referenced by financial evidence.

The migration seeds up to 1,000 historical outbox entries and records a receipt-ID checkpoint. Run the following statement, committing each batch, until the returned count reaches zero. Overlapping workers serialize on the checkpoint row. New settlement transactions capture their links using a deferred trigger even when they commit below a previous high-water mark.

```sql
SELECT billing_backfill_movements(1000);
```

The live fallback reads eligible outbox evidence during backfill. Compare the event IDs in `billing_proven_movements` and `billing_movement_links` after catch-up. One outbox receipt and the frozen payer must prove the posting. Wallet transactions shared by multiple receipts have ambiguous attribution and stay outside this projection. Pre-outbox history has no inferred wallet movement. The historical mappings and these immutable links should survive rollback. Retain the BE-11 wallet triggers while financial writers are active.

## Admission and quota boundaries

Project budgets use `project-{currency}` IDs. Create the first rule with `If-Match: "0"`. Subsequent updates require its current version and all writes use a scoped idempotency key. The budget is shared across models and keys. Rule revisions retain their effective time, timezone and day, Monday-week or month reset boundary. IANA local-calendar arithmetic accounts for daylight saving changes.

The gateway admission owner must call `readmodel.Reserve(ctx, tx, scope, requestID, upperBound)` in the journal admission transaction, at read-committed isolation and before dispatch. Use the journal request ID and frozen currency. A row lock serializes concurrent admissions for that project and currency. Hard mode returns `BUDGET_EXHAUSTED` above available headroom. Soft mode records the hold and exposes overrun. Matching retries reuse the hold. Call `readmodel.Release` in the transaction proving a hold was never dispatched or otherwise safely released. Settled receipt evidence supersedes a hold, with charge attribution to its admission window. Existing requests still use their established wallet/key admission path until this hook is registered. Reconcile uncertain dispatches before releasing a hold.

Rules expose each shared subscription/group window once. The named migration adapter `readmodel/legacy_entitlement.go` calls the existing subscription reset calculation for `Store.Quotas`. BE-19 can retire it when entitlement evidence supplies the reset window. Per-platform quotas have daily/weekly calendar resets and a 30-day rolling month. Wallet availability reflects existing paid holds. Recorded promotional awards reference the same wallet allowance and have unknown separate remaining credit. Historical entitlement rows whose counters have changed after the query time return unknown counters. Current key-to-group bindings determine the set of candidate entitlements for legacy sources. Without a selected key, project rules determine the global block and key-specific alternatives remain individually visible. BE-17 should supply BYOK exposure observations and the frozen group/funding choice where available.

## Reconciliation and BE-19 handoff

A wallet difference equals the receipt command charge plus signed settlement-transaction movements minus a previously captured wallet hold. A positive difference denotes an under-debit. Missing or abandoned attempts carry null differences. `attach_evidence` records the operator's external evidence reference, original discrepancy, command actor and note. It returns an operation and a durable audit reference. Store sensitive statement contents with the evidence owner. Resolving a case leaves all paid evidence intact.

`POST B/adjustments` requires a current session step-up and `billing.adjust`. A signed correction updates the proven payer's USD wallet and appends a BE-11 balanced event in the same transaction. `billing_adjustment_effects` binds the new event ID, original wallet event ID, receipt, request, scope and command ID. The database rejects edits to these references. Ledger and request deep links preserve the original charge. Payment rail refunds continue through the payment workflow.

BE-19 should consume the original event, adjustment event and command IDs when linking provider refunds, chargebacks, redemption, affiliate and subscription fulfillment. Preserve provider currency and minor units. The legacy receipt producer establishes USD only. Additional currency, entitlement postings, catalog-reference amounts, BYOK exposure and service fees require explicit frozen producer evidence. Keep these values separate from wallet movement. Introduce independent proof for a payer-funded promotion before reporting a spendable gift balance or adding it to available funds.

## Verification

From `backend/`, run `go build ./...`, `go vet ./...` and `go test ./...`. Set `UPSTREAM_JOB_TEST_DSN` to an isolated PostgreSQL 16 database named `job_test` for the HTTP, accounting and parallel admission regression.

```sh
go test -tags=integration -count=1 -run '^TestCostWorkbenchPostgres$' -v ./internal/control/billing
```
