# · Database restore

## Composition

Add `restorehttp.ProviderSet` from `internal/control/restore` to Wire. It builds `*be09.Runtime` from `internal/legacy/restore` using the existing application DB, config, backup service, identity evaluator, TOTP service and user service. The constructor attaches the v1 backup adapter. Include the runtime in the identity operation registry alongside connections.

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

From `backend`, regenerate Wire with `go generate ./cmd/server` and compose the fragment with `go run ./api/openapi/compose`. Regenerate the frontend transport declarations after composing the fragments.

## Storage and deployment

| Setting | Value |
| --- | --- |
| Application DB | Existing production target from the server config |
| `OMNIHIVE_RESTORE_JOURNAL_DSN` | PostgreSQL database dedicated to this installation. Give the journal role schema creation and journal DML privileges. Place its backups and retention outside the target recovery procedure. |
| `OMNIHIVE_RESTORE_ISOLATED_DSN` | Dedicated scratch database on a separate PostgreSQL cluster, as a PostgreSQL URL with explicit user, host, port, DB name and `sslmode`. Give the restore role ownership of the scratch schema. |
| Worker dependencies | Compatible `pg_dump` and `psql`, private temporary storage for the staged SQL and compressed snapshots, access to the configured backup object store |
| Recovery dependencies | Preserve backup-store access, configuration, the persistent encryption key, deployment artifacts and plugin versions independently of the target DB. The validation report covers PostgreSQL contents. |

All gateway, control and worker instances use the same DSNs and backup store. The service compares `pg_control_system().system_identifier` and `current_database()` on live sessions. Grant the connecting roles permission to execute `pg_control_system()`. Production and journal can use different databases of one cluster. The rehearsal uses its own cluster and restricted credentials and network access. PostgreSQL host, service and options environment overrides are rejected during setup. A previously bound journal also checks that target identities still match on restart. Both restore DSNs omitted leaves backup administration available and restore commands unavailable. Providing only one setting or binding colliding targets fails initialization.

Migration 298 is additive. The application migration runner records the file and retains the existing backup settings and records. At initialization the runtime applies the same embedded file in a transaction on the dedicated journal with `SET LOCAL omnihive.restore_journal='on'`. The application database uses the normal migration session. The `restore_journal` schema contains target bindings, legacy backup links, previews, operations, legacy v1 requests and append-only transition events. It has no foreign key into the target DB.

Drain older restore initiators before enabling the new write path. An old process can still execute its own unfenced goroutine. On startup old running restores are marked `review_required` and their backup links are imported idempotently into the journal. Historical success values remain legacy metadata. A dump taken by the asynchronous backup creator can contain its own running row. Startup consults the journal before cleaning stale objects and recovers the completed archive metadata for that row. New operations record independent evidence. Both single-object and ordered multipart archives go through digest verification.

## Execution and review

1. A preview reads the gzip archive, validates completion and the required user, key, billing and task tables, and freezes the source locator, SHA-256 digest, COPY row counts and content fingerprints. A supplied `expected_digest` must match. The preview belongs to its actor, workspace and target and expires in 30 minutes.
2. The worker claims a 45-second lease and renews it every 15 seconds. It stages the checked archive and a verification script on private temporary storage. New dumps and all replay sessions use UTC and ISO/YMD. For both rehearsal and production, one target transaction locks existing tables against writes, takes the pre-restore snapshot, applies the archive and checks its contents. It replays COPY rows into typed temporary tables and compares both multisets, including duplicates. A timestamp in an older +08 archive therefore compares equal to the same instant in UTC. The check also rejects unexpected user tables. Allow space for temporary SQL, the largest table's verification data, WAL and the transaction's replacement tables.
3. Make a fresh production preview after the matching rehearsal succeeds. Complete the existing session-scoped TOTP grant at `POST /api/v1/user/totp/step-up`. The worker rechecks authorization and step-up after the snapshot. Run the same release and configure the journal on every serving replica. On entry to the snapshot stage, HTTP admission closes and drains admitted handlers across those replicas. New requests (including gateway streams and GETs with side effects) receive 503 `RESTORE_MAINTENANCE`; liveness remains available. Monitor the journal directly during the cutover. Schedule external schema changes and out-of-band jobs outside this window.
4. The worker fences the commit against its journal generation after all comparisons pass. A SQL or verification failure before this point rolls back replacement data and releases locks. The journal records `failed/rolled_back` and the reason. Correct the source or deployment and start a fresh preview. A successful commit records `succeeded/verified` and reopens traffic. A restarted worker reclaims expired snapshot work, marks expired pre-commit transactions rolled back, and reconciles older unknown or unacknowledged commit outcomes into `failed/manual_recovery`. Production admission stays closed for that explicit review.

### Reconcile a lost commit acknowledgement

1. Fence the affected worker and its target psql session. Drain all replicas and independent writers at the load balancer or deployment layer before manual repair. Read the operation ID, generation, stage, source digest and pre-restore snapshot from the dedicated journal:

   ```sql
   SELECT o.id,o.generation,o.target,o.state,o.stage,o.snapshot,p.digest,o.report
   FROM restore_journal.operations o JOIN restore_journal.previews p ON p.id=o.preview_id
   WHERE o.stage='manual_recovery';
   ```

2. Preserve the source, journal and current target dump. Fetch the S3 manifest from `snapshot.key`, check every listed part's size and SHA-256, concatenate by ascending index, and compare the SHA-256 of the resulting gzip file with `snapshot.digest`. Decompress to a private file, checking gzip integrity and exit status. Restore it first into a disposable database. Compare balances, users, keys, billing entries and job counts with the pre-cutover evidence. Retain the manifests and operator decision outside the target.
3. If the target outcome cannot be verified, recover the checked pre-restore SQL with `psql -X --single-transaction -v ON_ERROR_STOP=1 -f snapshot.sql` against the explicitly confirmed production database. Verify the target against the checked disposable copy, including application-specific critical records, sequences and external state. Preserve the encryption key, configuration and protected credentials across this step.
4. Only after those checks pass, record the reviewed operation and generation and release the gate in the dedicated journal. The update must affect exactly one row. Restart the workers, confirm readiness and reopen the load balancer. A subsequent product restore starts from a fresh preview and rehearsal.

```sql
UPDATE restore_journal.operations
   SET state='failed', stage='reconciled', owner=NULL, lease_until=NULL,
       report=report || jsonb_build_object('reason','OPERATOR_RECONCILED'),
       updated_at=clock_timestamp()
 WHERE id=:'operation_id' AND generation=:'reviewed_generation'::bigint
   AND state='failed' AND stage='manual_recovery';
```

For a reviewed legacy candidate, clear only its reviewed backup ID's `review_required` flag after fencing old writers and recording the operator decision. A binary rollback retains migration 298 and the independent journal. Keep restore writes drained while any earlier-generation workers or uncertain operations remain.

## API and frontend integration

| Method and route | Authority and response |
| --- | --- |
| `POST /api/v2/admin/restores/previews` | `ops.restore.preview`, `Idempotency-Key`, HTTP 201. Input has `backup_id`, `target` and optional `expected_digest`. Output has `id`, `verified`, `impact` and `expires_at`. |
| `POST /api/v2/admin/restores` | `ops.restore.execute`, `Idempotency-Key`, HTTP 202. Input has `preview_id`, `target` and nullable `rehearsal_id`. Production requires a successful isolated operation for the same backup and digest. |
| `GET /api/v2/admin/restores/{id}` | `ops.restore.execute`, HTTP 200. Poll the accepted ID for `generation`, `state`, `stage`, `target` and `report`. |
| `GET /api/v2/workspaces/1/operations/{id}` | identity operation status. Current restore authority sees details. An owner whose platform authority was removed sees only their operation status when they retain workspace access. |

The operations console should retain the operation ID and reuse the create key on transport loss. A reused key with different input or an already consumed preview returns 409. Poll through `queued` and `running`. Display `succeeded` only with the final verified report. Display `unknown` as requiring operator review and keep the stage and opaque operation ID visible. `STEP_UP_REQUIRED` uses the existing TOTP prompt. `STEP_UP_TOTP_NOT_ENABLED` requires enrolling the actor. Archive credentials, object keys, SQL text and row contents stay outside v2 responses. Example payloads are in [the contract fixture](../../api/restore/restore.json).

The existing v1 restore endpoint retains its admin password check and returns HTTP 202 after persisting a request. A production request also requires the execution grant and session TOTP. The worker uses separate idempotent isolated and production operations and creates the fresh production preview after rehearsal. The accepted v1 request ID stays queryable on the admin and identity operation routes through the production child's terminal state. A failure before source verification has a reason-only report. The backup read and list endpoints project status from the journal, including when the backup predates its own inventory record. An explicit deletion hides the legacy backup link while retaining operation history. Runtime should run restore consumers in `all` and `worker`, and mount the authenticated endpoints in `all` and `control`. The v1 adapter is the only production restore initiator outside v2. Direct, unbound `BackupService.RestoreBackup` and `StartRestore` compatibility helpers have fixture callers and reject calls on the composed service.

## Verification

Run Go build and vet on the restore, compatibility, handler and service packages. The PostgreSQL/Redis integration runs below require `pg_dump`, `psql`, a dedicated `job_test` DSN in `UPSTREAM_JOB_TEST_DSN`, `AFFINITY_REDIS_TEST_ADDR` and `OMNIHIVE_TEST_EXTERNAL_SERVICES=1`. Set `BE09_TEST_JOURNAL_DSN` for a disposable database named `be09_journal_test` and `BE09_TEST_ISOLATED_DSN` for `be09_rehearsal_test` on its own disposable cluster. All three fixture DSNs must use explicit loopback addresses, ports and `sslmode=disable`. The integration suites create unique test databases and drop only those databases.

```sh
go test -tags=unit ./internal/modules/ops/restore ./internal/service -run 'Test(ArchiveEvidence|BackupService|StartRestore|RecoverStaleRecords)'
go test -p 1 -tags=integration -count=1 ./internal/modules/ops/restore ./internal/control/restore ./internal/control/identity
go test -p 1 ./internal/service/... ./internal/handler/... ./internal/repository/...
```

After aggregate wiring, run [the smoke script](smoke.sh) against a disposable standard-mode or embedded server with a completed fixture backup, isolated Redis, an independent journal and a separate disposable rehearsal cluster. Set `SMOKE_BASE_URL`, `SMOKE_ADMIN_EMAIL`, `SMOKE_ADMIN_PASSWORD` and `SMOKE_BACKUP_ID`. The script uses a loopback server with a password-login fixture admin and performs an isolated execution, idempotent replay, report verification and identity operation lookup.
