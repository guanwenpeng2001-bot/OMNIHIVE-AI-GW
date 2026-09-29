# Batch image recovery absorption — batch 4

Validated 2026-09-18 (Asia/Shanghai) on baseline `f48cd7409`, which includes
accounting A (`ec7014fa5`). Branch: `guanwenpeng2001-bot/absorb-accounting-recovery`.

## Scope and guarantees

- Applied only batch-image changes from `aeb1e1502` (entry `eeeef9cce`), the
  settlement fragment of `2232831f5`, and priority/eligibility tests from
  `577d145e5` (entry `b4e3f1954`). No ops cleanup fragments were imported.
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
- Capture retains accounting A's existing idempotency/terminal identity and lock
  protocol. The acknowledged `UsageLogRepository.Create` precedes completed
  status; usage errors (including missing configuration) propagate for recovery.
- Current pricing formulas, snapshots, amount normalization, priority direction,
  and eligibility implementation are unchanged. No migration was added.

Ambiguous submissions still require reconciliation with the provider; this patch
does not automatically resubmit them. Historical jobs already marked completed
without usage are not backfilled. Neither limitation is treated as evidence for
refunding or charging again.

## Executed validation

Commands below ran from `backend` on the candidate implementation:

| Command | Result |
| --- | --- |
| `go test -tags=unit ./internal/repository ./internal/service -run 'BatchImage\|Settlement\|Recovery' -count=1` | PASS with isolated PostgreSQL/Redis enabled; repository 2.823s, service 0.142s |
| `go test -tags=unit ./internal/repository ./internal/service -count=1` | Repository PASS (11.693s); service has exactly the seven known baseline session-hash failures (178.609s), no additional failures |
| `go build ./...` | PASS |
| `go test -tags=integration ./internal/repository -run 'TestBatchImageRepository_' -count=1` | PASS (4.489s); real testcontainers PostgreSQL/Redis harness, including recovery scan and stale-claim tests |
| `go test -tags=unit ./internal/repository -run '^TestBatchImageRecoveryPostgresRedis$' -count=1 -v` | PASS (1.279s); all six subtests actually ran |
| `git diff --check` | PASS |

The full-suite baseline exceptions are:

1. `TestGenerateSessionHash_ContinuousConversation_HashChangesWithMessages`
2. `TestGenerateSessionHash_MessageRollback`
3. `TestGenerateSessionHash_SameUserGrowingConversation`
4. `TestGenerateSessionHash_MultipleUserMessages`
5. `TestGenerateSessionHash_LongConversation`
6. `TestGenerateSessionHash_GeminiMultiTurnHashChanges`
7. `TestGenerateSessionHash_GeminiMultiTurnHashNotSticky`

Raw local output is retained in `.pi/validation/recovery-{targeted,full,build,integration,faults}.log`
(ignored worktree artifacts, not required by the tests).

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
The two named test containers were stopped after validation and retained for
inspection; use `docker start absorb-recovery-postgres absorb-recovery-redis` to
reuse them, or the following commands on a host without those container names.

```powershell
docker run -d --name absorb-recovery-postgres -e POSTGRES_PASSWORD=accounting-test -e POSTGRES_DB=job_test -p 127.0.0.1:15435:5432 pgvector/pgvector:pg16
docker run -d --name absorb-recovery-redis -p 127.0.0.1:16381:6379 redis:7-alpine
$env:UPSTREAM_JOB_TEST_DSN='postgres://postgres:accounting-test@127.0.0.1:15435/job_test?sslmode=disable'
$env:ACCOUNTING_TEST_REDIS_ADDR='127.0.0.1:16381'
go test -tags=unit ./internal/repository -run '^TestBatchImageRecoveryPostgresRedis$' -count=1 -v
```

The password above belongs only to the disposable test container. The separate
integration harness uses `DOCKER_HOST=npipe:////./pipe/dockerDesktopLinuxEngine`
on Windows and creates its own PostgreSQL/Redis containers. No production database
or provider was contacted and no upstream image charge was incurred.
