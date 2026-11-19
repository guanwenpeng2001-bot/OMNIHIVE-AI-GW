# BE-09 · Database restore

## Composition

Add `restorehttp.ProviderSet` from `internal/control/restore` to Wire. It builds `*be09.Runtime` from `internal/legacy/BE-09` using the existing application DB, config, backup service, identity evaluator, TOTP service and user service. The constructor attaches the v1 backup adapter. Include the runtime in the BE-01 operation registry alongside BE-04.

```go
identity.Operations{Readers: []identity.OperationReader{connectionStore, restoreRuntime}}
```

Pass the handler through the composition root and register it on the existing authenticated control plane. Keep JWT sessions, the panel rate limiter, audit and configured CSRF protection. The handler binds platform restore operations to the default workspace. Register the restored operation actions in the aggregate audit route map as `ops.restore.preview` and `ops.restore.execute`.

```go
restorehttp.Register(r.Group("/api/v2"), restoreHandler,
    gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(), gin.HandlerFunc(auditLog))
```

Start the worker once per worker-capable process. During shutdown, cancel and join it before closing the runtime or the shared application DB.

```go
stopRestores := restoreRuntime.StartWorker(applicationContext)
// In the application cleanup function:
stopRestores()
restoreRuntime.Close()
```

From `backend`, regenerate Wire with `go generate ./cmd/server` and compose the fragment with `go run ./api/v2/compose`. Regenerate v2 frontend transport declarations using the integration log command. The coordinator owns the aggregate edits and these outputs.

## Storage and deployment

| Setting | Value |
| --- | --- |
| Application DB | Existing production target from the server config |
| `OMNIHIVE_RESTORE_JOURNAL_DSN` | PostgreSQL database dedicated to this installation. Give the journal role schema creation and journal DML privileges. Place its backups and retention outside the target recovery procedure. |
| `OMNIHIVE_RESTORE_ISOLATED_DSN` | Dedicated scratch database on a separate PostgreSQL cluster, as a PostgreSQL URL with explicit user, host, port, DB name and `sslmode`. Give the restore role ownership of the scratch schema. |
| Worker dependencies | Compatible `pg_dump` and `psql`, private temporary storage for the staged SQL and compressed snapshots, access to the configured backup object store |
| Recovery dependencies | Preserve backup-store access, configuration, the persistent encryption key, deployment artifacts and plugin versions independently of the target DB. The validation report covers PostgreSQL contents. |

All control and worker instances use the same DSNs and backup store. The service compares `pg_control_system().system_identifier` and `current_database()` on live sessions. Grant the connecting roles permission to execute `pg_control_system()`. Production and journal can use different databases of one cluster. The rehearsal uses its own cluster and restricted credentials and network access. PostgreSQL host, service and options environment overrides are rejected during setup. A previously bound journal also checks that target identities still match on restart. Both restore DSNs omitted leaves backup administration available and restore commands unavailable. Providing only one setting or binding colliding targets fails initialization.

Migration 298 is additive. The application migration runner records the file and retains the existing backup settings and records. At initialization the runtime applies the same embedded file in a transaction on the dedicated journal with `SET LOCAL omnihive.restore_journal='on'`. The application database uses the normal migration session. The `restore_journal` schema contains target bindings, legacy backup links, previews, operations, legacy v1 requests and append-only transition events. It has no foreign key into the target DB.

Drain older restore initiators before enabling the new write path. An old process can still execute its own unfenced goroutine. On startup old running restores are marked `review_required` and their backup links are imported idempotently into the journal. Historical success values remain legacy metadata. New operations record independent evidence. Both single-object and ordered multipart archives go through digest verification.

## Execution and review

1. A preview reads the gzip archive, validates completion and the required user, key, billing and task tables, and freezes the source locator, SHA-256 digest, COPY row counts and content fingerprints. A supplied `expected_digest` must match. The preview belongs to its actor, workspace and target and expires in 30 minutes.
2. An isolated execution claims the persisted operation with a 45-second DB-clock lease. Workers renew every 15 seconds. They re-read and check the exact source, save a compressed pre-restore dump to versioned object parts and persist its manifest locator and digest before applying SQL. `psql` uses a single transaction and stops on SQL errors. A fresh target dump must match the source COPY sections, column lists, row counts and order-independent row fingerprints. User, API key, billing entry and upstream job counts are exposed in the report. Drain application writers for a consistent production cutover and verification window.
3. Create a production preview after the matching isolated operation succeeds. Execution requires the platform action and the existing session-scoped TOTP grant from `POST /api/v1/user/totp/step-up`. The worker checks the actor's current grant and step-up again immediately before applying to production. Keep the operator session available through the pre-restore snapshot.
4. A peer can reclaim expired pre-apply work with an incremented generation. Progress, renewal and terminal updates compare operation ID, owner, generation, state, stage where applicable, and live lease. An interrupted apply or post-apply verification becomes `unknown` and blocks new work on that target. A failed isolated content check records `failed`. A failed production content check requires review. A late worker cannot publish success.

The journal records the source digest and four counts for verified restores. Sequence state, external object payloads, deployed config, encryption keys and plugin binaries require their own recovery evidence. A rejected archive or incomplete backup remains available to the legacy backup inventory.

BE-23 can inventory outstanding work from `restore_journal.operations`, historical review candidates from `restore_journal.backups WHERE review_required` and stage changes from `restore_journal.events`, ordered by `sequence`. For an `unknown` operation, isolate and drain the old worker and its target-side `psql` session first. Check transaction outcome, re-dump the target in maintenance mode, compare evidence and retain the source, snapshot manifest and operator review record externally. The snapshot manifest lists each part's object key, index, compressed size and SHA-256. Concatenate parts in index order and verify the persisted full gzip digest before recovery.

After completing and recording that review, a journal administrator may release the target by recording a failed terminal outcome with a conditional update scoped to the reviewed ID and generation. A fresh request then takes a new snapshot and performs verification. The update must affect exactly one row.

```sql
UPDATE restore_journal.operations
   SET state = 'failed', owner = NULL, lease_until = NULL,
       report = report || jsonb_build_object('reason','OPERATOR_RECONCILED'),
       updated_at = clock_timestamp()
 WHERE id = :'operation_id' AND generation = :'reviewed_generation'::bigint
   AND state = 'unknown';
```

For a reviewed legacy candidate, clear only its reviewed backup ID's `review_required` flag after fencing old writers and recording the operator decision. A binary rollback retains migration 298 and the independent journal. Keep restore writes drained while any earlier-generation workers or uncertain operations remain.

## API and frontend handoff

| Method and route | Authority and response |
| --- | --- |
| `POST /api/v2/admin/restores/previews` | `ops.restore.preview`, `Idempotency-Key`, HTTP 201. Input has `backup_id`, `target` and optional `expected_digest`. Output has `id`, `verified`, `impact` and `expires_at`. |
| `POST /api/v2/admin/restores` | `ops.restore.execute`, `Idempotency-Key`, HTTP 202. Input has `preview_id`, `target` and nullable `rehearsal_id`. Production requires a successful isolated operation for the same backup and digest. |
| `GET /api/v2/admin/restores/{id}` | `ops.restore.execute`, HTTP 200. Poll the accepted ID for `generation`, `state`, `stage`, `target` and `report`. |
| `GET /api/v2/workspaces/1/operations/{id}` | BE-01 operation status. Current restore authority sees details. An owner whose platform authority was removed sees only their operation status when they retain workspace access. |

FE-WP7 should retain the operation ID and reuse the create key on transport loss. A reused key with different input or an already consumed preview returns 409. Poll through `queued` and `running`. Display `succeeded` only with the final verified report. Display `unknown` as requiring operator review and keep the stage and opaque operation ID visible. `STEP_UP_REQUIRED` uses the existing TOTP prompt. `STEP_UP_TOTP_NOT_ENABLED` requires enrolling the actor. Archive credentials, object keys, SQL text and row contents stay outside v2 responses. Example payloads are in [the contract fixture](../../api/BE-09/restore.json).

The existing v1 restore endpoint retains its admin password check and returns HTTP 202 after persisting a request. A production request also requires the execution grant and session TOTP. The worker uses separate idempotent isolated and production operations and creates the fresh production preview after rehearsal. The accepted v1 request ID stays queryable on the admin and BE-01 operation routes through the production child's terminal state. A failure before source verification has a reason-only report. The backup read and list endpoints project status from the journal, including when the backup predates its own inventory record. An explicit deletion hides the legacy backup link while retaining operation history. BE-22 should run restore consumers in `all` and `worker`, and mount the authenticated endpoints in `all` and `control`. The v1 adapter is the only production restore initiator outside v2. Direct, unbound `BackupService.RestoreBackup` and `StartRestore` compatibility helpers have fixture callers and reject calls on the composed service.

## Verification

Run Go build and vet on the restore, compatibility, handler and service packages. The PostgreSQL/Redis integration runs below require `pg_dump`, `psql`, a dedicated `job_test` DSN in `UPSTREAM_JOB_TEST_DSN`, `AFFINITY_REDIS_TEST_ADDR` and `OMNIHIVE_TEST_EXTERNAL_SERVICES=1`. Set `BE09_TEST_JOURNAL_DSN` for a disposable database named `be09_journal_test` and `BE09_TEST_ISOLATED_DSN` for `be09_rehearsal_test` on its own disposable cluster. All three fixture DSNs must use explicit loopback addresses, ports and `sslmode=disable`. The integration suites create unique test databases and drop only those databases.

```sh
go test -tags=unit ./internal/modules/ops/restore ./internal/service -run 'Test(ArchiveEvidence|BackupService|StartRestore|RecoverStaleRecords)'
go test -p 1 -tags=integration -count=1 ./internal/modules/ops/restore ./internal/control/restore ./internal/control/identity
go test -p 1 ./internal/service/... ./internal/handler/... ./internal/repository/...
```

After aggregate wiring, run [the smoke script](smoke.sh) against a disposable standard-mode or embedded server with a completed fixture backup, isolated Redis, an independent journal and a separate disposable rehearsal cluster. Set `SMOKE_BASE_URL`, `SMOKE_ADMIN_EMAIL`, `SMOKE_ADMIN_PASSWORD` and `SMOKE_BACKUP_ID`. The script uses a loopback server with a password-login fixture admin and performs an isolated execution, idempotent replay, report verification and BE-01 operation lookup.
