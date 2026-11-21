# Batch image recovery

- Redis lock renewal now reports lost ownership, cancels the processor, and
  atomically checks the owner token before acknowledging or requeuing work.
  Processor persistence, indexing, capture/release, usage and terminal settlement
  boundaries check the worker lease.
- A keyset scan reconstructs work from durable submitted/running/indexing/settling
  jobs and terminal jobs with unresolved holds. Scan position survives transient
  Redis/database errors and cycles after the last page.
- Indexing and settlement failures retain the provider receipt and recoverable
  state. Exhausting local retries no longer refunds successful upstream work.
- Submission claims are persisted before contacting the provider. An uploading
  job without a provider receipt is ambiguous, so stale recovery and cancellation
  cannot infer that a refund is safe. Receipt persistence has a bounded context
  independent of client cancellation.
- Capture retains the existing idempotency/terminal identity and lock
  protocol. The acknowledged `UsageLogRepository.Create` precedes completed
  status; usage errors (including missing configuration) propagate for recovery.

Ambiguous submissions require provider reconciliation before retrying.

## Real process-exit fault injection

`batch_image_recovery_postgres_test.go` uses real production repositories and Redis
queue scripts. The child test executable reserves the job and obtains a Redis
lease, then calls settlement. Decorators call `os.Exit(71)` directly after each
selected durable commit, so no defer, Redis acknowledgement, or in-memory recovery
survives. The parent checks persisted intermediate state, waits for lease expiry,
recovers the active reservation, acquires a new lock and retries settlement three
times before fenced queue completion.

| Fault point | State before recovery | Result after recovery |
| --- | --- | --- |
| Capture committed, before usage | settling, capture count 1, usage count 0 | completed, capture count 1, usage count 1 |
| Usage committed, before terminal update | settling, capture count 1, usage count 1 | completed, capture count 1, usage count 1 |
| Terminal committed, before queue acknowledgement | completed, capture count 1, usage count 1 | unchanged; stale reservation safely acknowledged |
| Usage writer returns error | settling, capture count 1, usage count 0 | completed, capture count 1, usage count 1 |
| Terminal writer returns error | settling, capture count 1, usage count 1 | completed, capture count 1, usage count 1 |

All five cases start with balance 100 and hold 1, bill two successful images at
0.25, and assert balance 99.5, frozen balance 0, actual usage cost 0.5, and removal
of the inflight marker. A sixth real-Redis test expires an old lock, acquires a new
one, and proves the old token cannot renew, acknowledge, requeue, or delete the
new lock. Unit tests additionally exercise queue-scan disconnects/pagination,
unknown uploads, lease loss, missing usage persistence, and priority determinism.

## Reproduction

Only disposable instances are used. The PostgreSQL test refuses databases other
than `job_test`; fixtures clean their own rows and unique Redis keys.
Run the following against disposable local services.

```powershell
docker run -d --name absorb-recovery-postgres -e POSTGRES_PASSWORD=accounting-test -e POSTGRES_DB=job_test -p 127.0.0.1:15435:5432 pgvector/pgvector:pg16
docker run -d --name absorb-recovery-redis -p 127.0.0.1:16381:6379 redis:7-alpine
$env:UPSTREAM_JOB_TEST_DSN='postgres://postgres:accounting-test@127.0.0.1:15435/job_test?sslmode=disable'
$env:ACCOUNTING_TEST_REDIS_ADDR='127.0.0.1:16381'
go test -tags=unit ./internal/repository -run '^TestBatchImageRecoveryPostgresRedis$' -count=1 -v
```
