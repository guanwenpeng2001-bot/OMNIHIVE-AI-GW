# BE-06 execution journal integration

## Composition and rollout

Apply `backend/migrations/295_execution_journal.sql` through the existing PostgreSQL migration runner after BE-01 migration 290. The migration expands the schema. Existing key-budget and receipt repositories remain the financial writers.

Pass the existing `*sql.DB` to `server.SetupRouter`. Install the middleware immediately after BE-01 project scopes, before the first registered gateway group. Regenerate Wire after changing the coordinator-owned constructor.

```go
r.Use(middleware2.InstallProjectScopes(gatewayScopes, cfg))
r.Use(handler.InstallExecutionJournal(db, gatewayScopes))
```

This enables GET and POST on `/v1/responses`, `/responses`, and `/backend-api/codex/responses`, plus the async image generation submission on `/v1/images/generations/async` and `/images/generations/async`. Native handlers retain their own error encoders, output and websocket terminal frames. HTTP callers receive the admitted UUID in `X-Execution-ID`. An explicitly keyed matching Responses resubmission acknowledges the original UUID using 204 with an empty body. Raw inference output stays ephemeral. Different input under the same key yields the native 409 error. BE-13 can return a full RunSummary through its own page endpoint. Job deduplication in this slice uses the existing persisted job/task ID.

Install the journal on every gateway serving these paths before advertising key-based duplicate suppression. Legacy gateway instances accept requests through their original paths. For a binary rollback, drain the admitted requests and workers, route fresh traffic to the previous binary, and retain migration 295. Historical mappings and outstanding holds remain readable. The coordinator owns registration and post-merge live smoke.

Compose `backend/api/v2/fragments/execution.yaml` with the other fragments during aggregate integration. Its internal component schemas describe admission and attempt metadata. The package adds zero v2 routes. The fixtures in `contracts/api/execution/` are checked against actual Go serialization in `TestExecutionAPIContracts`.

## Internal contracts

Both gateway services use one factory for scope, existing attempt allowance, immutable catalog/pricing evidence, settlement dependency and admission. A WS turn gets a fresh request UUID and its own BE-03 response state. An async image task detaches from the submission context with its own deadline and identity. Later turns and job retries must retain their original allowance within each logical execution.

`AdmittedExecution` carries `RequestID`, `Scope`, `AttemptBudget`, `Deadline`, `Versions` and `ReservationRef`. Capture the caller's authenticated `scope.From(ctx)` and existing `AttemptBudget` before calling the factory. `Postgres.Admit` returns the original admission and a replay bit for the same scoped registration. A fresh server-generated request UUID also allows readback after an ambiguous commit. The credential adapter carries the digest and verifies current access before each send.

`Attempt` carries `ID`, `RequestID`, `Number`, `AccountID`, `Acceptance` and `Outcome`. Admission creates the first unsent intent in the same statement. Dispatch claims a unique ordinal before transport entry. A failed or lost dispatch acknowledgement stops forwarding. BE-03 supplies retry decisions and terminal state. A silent abandoned attempt remains unknown and visible after a successful replacement.

## Data and recovery

| Relation | Consumer contract |
| --- | --- |
| `gateway_executions` | A request, WS turn or job, its scope, frozen versions, deadline and original reservation reference. Use workspace and project restrictions on every control-plane query. |
| `gateway_execution_attempts` | One pre-dispatch intent per ordinal. A dispatch timestamp records the permit boundary. Provider acceptance may remain unknown after process loss. |
| `gateway_execution_receipts` | The receipt insert trigger links the dispatched account, execution and payer in the existing receipt transaction. A rejected or abandoned attempt is ineligible. |
| `gateway_execution_reconciliation` | Abandoned attempts and expired dispatched requests lacking a receipt. BE-11 investigates durable evidence. These rows confer no charge or resend permission. |
| `gateway_execution_release_obligations` | Outstanding existing budget reservations for a finished or expired execution. Use the existing conditional release API keyed by API key and reservation reference. Live cleanup runs detached after cancellation. Existing lease owners retain their own release/expiry protocols. |
| `gateway_execution_usage` | Partial token evidence. Explicit known-zero is 0. Missing receipts and absent counters produce SQL NULL. Historical positive counters remain observable. |
| `gateway_execution_legacy_ids` | Maps the original local request, turn or task ID and linked receipt IDs. BE-01-scoped historical receipts and upstream jobs retain their existing IDs. A NULL journal UUID identifies an unobserved historical execution. |

Journal registration stores SHA-256 hashes of the idempotency key and canonical payload. It stores no wire body, bearer secret or IP. PostgreSQL serializes duplicate registrations across gateway instances. A process that dies after admission leaves a recoverable intent. The release view covers a reservation committed before its in-memory acknowledgement. No historical dispatch timestamp or token observation is synthesized.

## Cross-package file inventory

All paths below are relative to `backend/`. Owned factory and protocol files are described above. The following existing files required changes at their current call sites.

| File | Change and handoff |
| --- | --- |
| `internal/handler/gateway_handler_responses.go` | Validate admission before the common Responses forwarding loop and emit native errors. |
| `internal/handler/openai_gateway_handler.go` | Validate OpenAI HTTP admission and each WS turn before forwarding. |
| `internal/handler/image_task_handler.go` | Fail the existing task if durable admission fails before queue handoff. Expose its execution identity on successful admission. |
| `internal/handler/openai_gateway_handler_test.go` | Allow the existing two-turn WS fixture to run against each normal route and middleware. |
| `internal/server/middleware/api_key_auth.go` | Bind BE-01's accepted credential digest and action to the journal-enabled request. |
| `internal/service/attempt_budget.go` | Claim the durable permit at the shared BE-03 send boundary and persist transport outcomes, including delayed WS acceptance. |
| `internal/service/gateway_tpm_wire.go` | Reuse the unsent TPM release on authorization/permit rejection after acquisition. |
| `internal/service/gateway_usage_billing.go` | Use the journal request identity for ordinary usage. Existing independent financial event IDs retain their established identities. |
| `internal/service/openai_gateway_usage.go` | Preserve each journaled WS turn identity and propagate observed token-counter presence into its receipt. |
| `internal/service/usage_log.go` | Carry the optional presence evidence through legacy receipt JSON. |
| `internal/service/usage_settlement_service.go` | Block an abandoned account's receipt at the producer boundary before persistence or settlement. |

## Dependent work packages

- **BE-07** supplies authoritative catalog/price versions to the factory and uses the frozen pricing instant. A missing or legacy price version remains identifiable.
- **BE-08** attaches scoped routing evidence to the existing request UUID and attempt UUID/ordinal. Actual sends claim the shared permit. Simulations omit admission and dispatch.
- **BE-11** consumes the frozen user-wallet payer, reservation reference, receipt link, nullable evidence, reconciliation and release views. The existing receipt transaction remains the sole billing authority.
- **BE-13** supplies an authenticated actor and credential, canonical payload registration and a new UUID per submitted run. Return the existing run on replay. A paid replay needs a new key and the user's funding acknowledgement.
- **BE-14** joins scoped requests, attempts, receipt links, legacy mappings and release/reconciliation state. Expose abandoned unknown attempts separately from charged replacement usage. Project authorization and redaction apply before detail lookup.
- **BE-15** can adopt the same job admission before fenced worker dispatch for further task families. Job aliases remain available through the legacy mapping.

## Verification

From the worktree root, provide a disposable `job_test` PostgreSQL URI in `UPSTREAM_JOB_TEST_DSN` and local Redis address in `AFFINITY_REDIS_TEST_ADDR`, then run `contracts/migrations/execution/smoke.sh`. It exercises real SQL, child-process kills and Redis queue revocation, followed by native HTTP and WS handlers on all three Responses paths. The test upstream is local. On a memory-constrained database host, set `EXECUTION_TEST_BIN_DIR` to a directory containing the `journal` and `handler` Go test binaries built from this commit with tags `integration` and `integration unit`, respectively. The smoke executes the same test selection against that host’s PostgreSQL and Redis.

After aggregate wiring, run the same script with `SMOKE_BASE_URL`, `SMOKE_API_KEY` and `SMOKE_MODEL` set for an isolated funded test installation. It submits one generation and verifies the original ID, matching replay and conflicting input. Set `SMOKE_EXPECT_STATUS=503` when deliberately exercising admission on an installation without an eligible upstream.

From `backend/`, run the hot-path comparison on the dedicated database.

```sh
go test -tags=integration -run '^$' -bench 'Benchmark(GatewayJournal|Registration)$' \
  -cpu=1 -count=3 -benchtime=1s -benchmem ./internal/gateway/execution/journal
```

`BenchmarkGatewayJournal` includes plan creation, existing TPM, SQL credential reauthorization, dispatch, outcome persistence and finalization. The comparison uses the same constructed gateway with journal installation omitted.

The table reports medians of three one-second samples on Linux/amd64 with Go 1.27.4 and one Go CPU. The isolated Docker harness ran PostgreSQL 16 and Redis 7. PostgreSQL used tmpfs with `fsync=off` and `synchronous_commit=off`.

| Operation | Median time | Median bytes | Median allocations |
| --- | ---: | ---: | ---: |
| Unkeyed registration | 2.818 ns | 0 | 0 |
| Keyed JSON registration | 7.605 µs | 1,840 | 44 |
| Existing plan and send | 3.896 ms | 8,122 | 144 |
| Journaled plan and send | 15.947 ms | 22,998 | 358 |

The journaled samples ranged from 15.396 to 16.395 ms. The benchmark measures SQL execution under these settings. Separate process-kill tests exercise gateway crash recovery. Power-loss durability and production disk latency require separate measurements.
