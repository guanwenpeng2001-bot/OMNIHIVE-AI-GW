# Accounting recovery A: locking and receipt durability

Implemented on candidate `73da91077`, selectively adapting `e3cce574d`,
`f4534f6d7`, and `bb0aeef69`. This is a function-level absorption, not a replacement
of endpoint files or a migration to a different pricing/normalization contract.
No schema, route, UI, pricing, or usage normalization changes are included.

## Transaction order

| Path | Order |
| --- | --- |
| Receipt settlement / reconciliation | Receipt identity → dedup identity → canonical usage → user (balance effect) → subscription → API key → account → platform quota / new derived event |
| Existing direct usage billing | Dedup identity → same financial effects order |
| Existing batch image reserve | Operation dedup → user |
| Existing batch image capture / release | Shared terminal dedup → historical opposite operation check → operation dedup → user |
| Recharge / balance redemption / refund | Existing order or redemption identity → user, using atomic balance updates |
| Subscription extension | Transaction → latest subscription row lock → expiry and optional status update → commit → cache invalidation |
| Subscription expiry / group deletion | Subscription IDs ascending, then update/delete the locked set in chunks within the same transaction |
| Platform quota replacement | Existing platform rows ascending before deletion/update, new records in platform order |
| Platform quota initialization / snapshots | User ID then platform; snapshot SQL explicitly locks the ordered target rows before updating |

Financial row locks use `FOR NO KEY UPDATE`: concurrent monetary updates still
exclude each other, while usage insertion's foreign-key `KEY SHARE` locks remain
compatible. Only required financial rows are acquired, always in the same order.
Subscription batches retain Ent soft-delete behavior and caller-owned transactions.

The shared `batch_image_terminal:<batchID>` identity prevents capture and release
from both consuming a payer's pooled frozen funds. Historical operation identities
and fingerprints remain intact; both live and archived opposite terminal claims
are checked. A failed capture rolls back its terminal claim with its money effects.
This covers the existing refund/hold paths and introduces no generic precharge flow.

## Admission and recovery

Admission is a process-local pressure bound, not a financial authority. It runs
before opening a SQL transaction for receipt settle/reconcile, direct Apply, and
batch reserve/capture/release. Existing callers that already own a transaction
(such as recharge/refund) do not wait on this queue while holding database locks.

- At most 32 active payers, one active operation per payer.
- At most 64 admitted operations per payer and 1024 overall, including active work.
- Recovery has an extra slot per payer and 32 globally; it joins the same FIFO.
- Payers rotate when an operation completes; waiting uses no database connection.
- Admission plus transaction has a ten-second budget, respecting shorter deadlines.
- Only PostgreSQL transaction-aborted SQLSTATEs `40P01` and `40001` are retried,
  at most three attempts. Ambiguous commits return to durable identity recovery.
- Rejected durable receipts remain pending. The existing recovery loop replays them.

Settlement queues capture immutable identity/fingerprint values, then use repository
evidence for settlement and cache invalidation. Missing, mismatched, or invalid-state
persistence acknowledgements are rejected. Worker shutdown closes admission and
waits for both queued tasks and admitted synchronous overflow submissions.

Subscription cache invalidation uses the identity read under the row lock. It runs
after a successful local commit even if the subsequent response read fails. An
outer Ent transaction installs a commit hook; rollback retains the cache. Invalidation
also publishes the existing cross-instance subscription invalidation event. Cache
errors are logged after commit rather than reported as a failed monetary mutation.

The optional settlement status `contention` field reports bounded process-local
latency samples and counts. Lock timings include SQL round trips, not just server
lock wait time. The last 1024 samples are retained; sample counts are cumulative.

## Verification on 2026-09-18

An isolated PostgreSQL 16 container and isolated Redis 7 container were created for
this task. Shared `sub2api-*` and `cumora-*` container data were not modified.

```powershell
docker run -d --name absorb-locks-postgres -e POSTGRES_PASSWORD=accounting-test -e POSTGRES_DB=job_test -p 127.0.0.1:15434:5432 pgvector/pgvector:pg16
docker run -d --name absorb-locks-redis -p 127.0.0.1:16381:6379 redis:7-alpine
cd backend
$env:UPSTREAM_JOB_TEST_DSN='postgres://postgres:accounting-test@127.0.0.1:15434/job_test?sslmode=disable'
$env:ACCOUNTING_TEST_REDIS_ADDR='127.0.0.1:16381'
go test -tags=unit ./internal/repository -run 'TestBillingMixedContentionPostgres|TestUsageSettlementReceiptPostgres|TestSubscriptionRenewalPostgresRedis' -count=1 -v
```

Result: PASS, repository 7.487 s. This runs real migrations on the disposable
database and real Redis cache invalidation. The fixtures refuse any database name
other than `job_test`. The DSN above contains only this disposable test password.

| Scenario | Verified result |
| --- | --- |
| 24 concurrent one-day extensions | Exactly 24 days accumulated |
| Expired and negative adjustments | Expired renewal starts from now; shortening an expired row or expiring an active row is rejected |
| Status update failure | Expiry and status rollback; Redis cache retained |
| Outer transaction commit / rollback | Cache retained before commit and after rollback, removed after commit |
| Response read failure after commit | Cache still removed |
| One payer, database-only locking | 24 receipts each replayed twice, 8 recharge/refund transactions, capture/release race, 8 expiry batches: 0.315 s, balances and effect counts exact |
| Four payers, database-only locking | 96 receipts each replayed twice, 32 recharge/refund transactions, 8 terminal operations, 8 expiry batches: 0.945 s, exact effects |
| Four payers, admission enabled | Same workload: 0.945 s, exact effects |
| Reversed quota initialization / replacement / snapshots | 32 concurrent workers, no lost snapshot or deadlock |
| Receipt replay and fault boundaries | 20/32 concurrent same-identity settlements; lost persistence/settlement ACKs; rollback on log, balance, commit and derived event failure; stopped/full worker recovery; archived identity replay: all pass |

The database-only cases bypass local admission and retry to model independent
processes. All three mixed cases recorded zero deadlock and serialization retries.
The cross-payer unit test additionally proves an admitted hot payer does not block
another payer. These timings are developer-machine observations, not capacity or
throughput guarantees. Fault injection/reconstruction is not an operating-system
process-kill test.

Required commands, run from `backend` without integration environment variables:

```text
go test -tags=unit ./internal/repository ./internal/service -run 'Subscription|Renewal|Receipt|Settlement|Billing|Lock' -count=1
go test -tags=unit ./internal/repository ./internal/service -count=1
go build ./...
```

Final targeted result: PASS (repository 0.737 s, service 10.083 s).
Build result: PASS.
Full unit result: repository PASS (3.741 s); service FAIL (178.626 s), with exactly
the seven pre-existing session-hash exceptions authorized in the dispatch:

- `TestGenerateSessionHash_ContinuousConversation_HashChangesWithMessages`
- `TestGenerateSessionHash_MessageRollback`
- `TestGenerateSessionHash_SameUserGrowingConversation`
- `TestGenerateSessionHash_MultipleUserMessages`
- `TestGenerateSessionHash_LongConversation`
- `TestGenerateSessionHash_GeminiMultiTurnHashChanges`
- `TestGenerateSessionHash_GeminiMultiTurnHashNotSticky`

No other test failed. Session-hash source and tests were not changed.

After verification, the two task-owned containers and their anonymous volumes
were removed with `docker rm -f -v absorb-locks-postgres absorb-locks-redis`.

Raw local logs: workspace-root `accounting-targeted.log`, `accounting-unit.log`,
and `accounting-integration.log` (ignored test artifacts).

The recovery boundary is unchanged: upstream usage can still be lost before the
first durable receipt commit. There is no new pricing authority, batch subscription
API, or guarantee of recovery before that commit. Existing session-hash failures
remain outside this task's scope.
