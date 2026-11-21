# Scoped audit

## Composition for runtime

Apply 310 after 290, 300, 303, 304 and 305. In control mode construct the store, observer and handler with the same identity evaluator and JWT configuration as identity.

```go
auditStore := audit.New(db, identityStore)
observer := audit.NewObserver()
auditHandler := audithttp.New(auditStore, observer, cfg)
audithttp.Register(v2, auditHandler, session, panelLimit, legacyAudit)
```

Register `auditStore` in `identity.Operations.Readers`. Install `auditHandler.Capture` after session validation on authenticated routes whose handlers validate both the workspace and project. Include the request and task routes to observe export and artifact access. Apply it once per route. Start `observer.Run(ctx, auditStore, onWriteFailure)` on each serving process. Start `auditStore.RunRetention(ctx, onSweepFailure)` in a worker-capable process. The callbacks take no arguments. Increment a fixed failure counter in each. Stop workers before closing SQL. The API fragment is `backend/api/openapi/fragments/audit.yaml`. Recompose OpenAPI and regenerate the frontend types in the aggregate branch.

Use `GET P/audit/events` for the seek page and `GET P/audit/events/{id}` for direct reads. The page accepts an exact action, resource kind, resource ID, inclusive `from`, exclusive `to`, cursor and limit. Scope comes from the authenticated route. The 15-minute cursor binds the actor, workspace, project, identity epoch and filters. Reads use the live evaluator and private/no-store headers. Workspace v2 access logs use the matched route and a server-issued correlation ID. The response header carries that ID. The finance grant alone gives no access. Audit readers, including administrators, see the same metadata. No role supplies a content projection.

`POST P/audit/events/{id}/exports` takes an Idempotency-Key and both audit.read and audit.export. The ready handle belongs to its actor, workspace, project and event. The identity operation reader rechecks current grants. Download regenerates the metadata, rechecks grants after materialization and checks the 15-minute deadline before returning the attachment. Expired rows remain available for a short diagnostic window and then return 404. The source event survives while a handle is live.

## Migration and retention

The migration installs capture triggers before backfilling historical request metadata exports, artifact revocations, billing adjustments and reconciliation resolutions from their committed source rows. Resource ownership comes from composite source keys. An opaque digest identifies a task/artifact pair independently of its locator. Existing unscoped logs without authoritative ownership stay outside the project view. The migration performs one transactional backfill. Measure source counts and lock duration on a production-sized snapshot before scheduling this migration.

`GET/PUT P/audit/retention` reads or replaces the three retention periods. A write requires audit.retention.write. Defaults are 90 days for committed events, seven days for observations and 24 hours for encrypted task retry input. Zero content hours disables retained retry input. Lowering the content TTL shortens existing input deadlines. The worker erases the ciphertext in batches. Receipt, ledger, task history and artifact lifecycle retain their own custody. The financial effects captured here have an indefinite hold, and a row-level guard also enforces any finite future evidence deadline. There is no public hold-release endpoint.

The worker visits up to 100 project IDs per query, including archived projects, and processes up to 500 rows of each class per project per minute. The transaction locks the project when applying or purging its policy. Purged rows and their count evidence commit together. A retry with no eligible rows creates no additional purge evidence. Identical policy writes create no additional change evidence. A binary rollback keeps the expanded schema, capture triggers and evidence holds.

## Egress and observability

tasks's historical task object reader uses the shared controlled egress policy. Native objects still use the private bucket and batch images use their owner-authorized batch downloader. The task HTTP adapter checks each DNS answer and pins its public IP at the transport, including through a selected CONNECT proxy. It accepts HTTPS and up to three GET redirects, clears headers on redirects, and caps receipt at 32 MiB and 30 seconds.

The plugins WASI guest approves the host capability. The provider callback uses the host's account-bound proxy, TLS profile, generation permit and existing connection limits. It forces pinned public transport and disables redirect replay. The models-discovery path also pins through the selected proxy. The response cap is 64 MiB with the generation deadline as the outer time limit. extension-actions should route new host provider calls through the same callback. The native v1 compatibility plugin executes its original transport in an external process. Its network custody remains with the trusted binary and deployment controls until retirement.

The observation queue holds 256 fixed-size records per serving process. The producer returns immediately on saturation and increments `Observer.Dropped()`. Export that counter under a fixed series. The consumer emits the `omnihive.audit` tracer's `audit.request` root span and the meter's `audit.request.duration` histogram. Attributes are the two operation values and a boolean success. The consumer starts from a fresh context and receives numeric scope facts separately for private storage. Preserve that separation in observability and apply workspace authorization before destination selection. The public status publisher continues to own its aggregate dimensions. Financial outbox and settlement operations have independent queues.

## Verification

Run the full build, vet and ordinary Go tests from backend. Point UPSTREAM_JOB_TEST_DSN at an isolated job_test PostgreSQL database and AFFINITY_REDIS_TEST_ADDR at its disposable Redis instance, then run the following command from backend.

```sh
go test -p 1 -tags=integration -count=1 ./internal/control/audit ./internal/control/requests ./internal/control/tasks ./internal/control/identity
```

The audit suite covers scoped direct access, cursors, role separation, repeated exports, download after revocation, retention replay, financial holds and ciphertext erasure. The existing request, task and identity suites cover the requests download, tasks object ticket and gateway revocation against Redis. The controlled egress unit fixtures exercise TLS, a CONNECT proxy, per-hop pinning, forbidden destinations, deadlines, oversized chunked responses and trace-header removal.
