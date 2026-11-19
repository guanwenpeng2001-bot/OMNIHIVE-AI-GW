# BE-15 image task center

## Composition

Apply migration 304 after identity (290), execution journal (295), wallet evidence (300) and conversion plans (301). Add `handler.NewImageTaskCenter` to the server injector. Its identity ports, catalog reader, gateway, batch services and persistent secret encryptor come from existing providers. Supply a `service.BackupObjectStore` targeting the **private image artifact bucket**. The existing S3 object-store factory implements this interface. Give the `task-artifacts/` prefix a lifecycle of at least 24 hours after upload and a short cleanup margin. Keep the binding available to readers across configuration changes. Creating with a missing object store or an ephemeral encryption key returns 503. The closed create path leaves legacy reads available.

The server resolves the artifact bucket from the current image storage settings at startup. Use a private bucket with no `public_base_url`. Keep that bucket and its objects reachable through the retention window when updating storage settings. Restart to apply the updated binding. Configured private storage with uploads disabled remains available for downloads. Missing, public, or unconfigured storage makes new submissions unavailable with HTTP 503. Legacy task metadata and legacy artifact adapters remain available.

Pass the returned `*internal/control/tasks.Handler` through the router provider. Register it under the same JWT session, CSRF, panel rate limiter and audit middleware as identity. Construct a fresh grant evaluation on each call. The additional grant `tasks.artifacts.revoke` controls removal. Access is always checked in the original workspace and project. A member with grants in multiple projects can switch back to the purchase's canonical scope. Guessing the same ID in a different project produces 404. Reading metadata or downloading an authorized result consumes no wallet, inference quota or generation attempt.

```go
taskhttp.Register(r.Group("/api/v2"), imageTaskCenter,
    gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(), gin.HandlerFunc(auditLog))
```

Add the task creation and retry paths to the existing body-omission table in the audit middleware. Preserve safe actor, scope, resource, transition and outcome. Strip the artifact ticket, provider URL, object locator, request body and decrypted retry input from logs and traces. Compose the BE-15 fragment, regenerate frontend types and regenerate Wire in the aggregate integration branch.

## Authority and rollout

| Source | Task ID | Current authority | Adoption |
| --- | --- | --- | --- |
| OpenAI-compatible image generation from v2 | `task_<uuid>` | PostgreSQL generation and worker identity, BE-06 single-use send permit, independent BE-11 receipts | The native image handler performs moderation, quota admission, connection selection and settlement. Task center captures a private object copy. Each dispatch rechecks the original grant and the selected endpoint's verified image capability. |
| Existing Gemini/Vertex image batches | `batch_<numeric job id>` | Existing batch worker, queue, provider job and legacy hold/capture/release | Same-transaction identity capture. Stage, items, parent, costs, deletion and output expiry read the original rows. Each indexed image is a separate downloadable artifact. |
| Existing upstream image jobs | `up_<numeric job id>` | Upstream repository and poller | The existing public request is the source identity. Successful indexed Qianwen result URLs stay server-side. |
| Redis OpenAI-compatible v1 image tasks | `redis_<imgtask id>` | Original Redis worker and terminal receipt | Import scans surviving records and stores their original key, owner, scope, version, pending deadline and result expiry. |

The SQL expansion captures currently provable legacy identities and listens for new SQL jobs. Missing owner/key/account relationships remain outside the tenant reader. Inspect unmatched jobs with the query below. After resolving a BE-01 quarantine, invoke `image_task_map_legacy('batch_image', id)` or `image_task_map_legacy('upstream_job', id)` on that source row. Existing identities remain frozen. A BE-06 execution join requires matching scope, key, owner, funding and legacy job identity. A historical batch capture has its own settlement identifier. Missing BE-06 requests, size observations and receipt amounts remain null.

```sql
SELECT b.id FROM batch_image_jobs b
WHERE NOT EXISTS (SELECT 1 FROM image_tasks t WHERE t.source='batch_image' AND t.source_id=b.batch_id);
SELECT j.id FROM upstream_jobs j WHERE j.family='image'
AND NOT EXISTS (SELECT 1 FROM image_tasks t WHERE t.source='upstream_job' AND t.source_id=j.public_request_id);
```

Run the importer from `backend/` with the installation's `TASK_IMPORT_POSTGRES_DSN` and `TASK_IMPORT_REDIS_URL`. It reads Redis without changing any queues, keys or TTLs. For a custom queue, pass `-lock-prefix` with its configured value. Each completed SCAN page prints a resume cursor. Repeat full passes while old producers and workers remain active, then perform a final pass after drain. Original Redis tasks that already expired have no recoverable output.

```sh
go run ./internal/modules/tasks/cmd/import-legacy -source images
go run ./internal/modules/tasks/cmd/import-legacy -source batch-leases
```

Call `imageTaskCenter.Store.Recover(ctx)` at startup and periodically from the worker lifecycle. It fences expired native executions, marks them unknown and clears expired encrypted retry inputs. The read projection also recognizes elapsed deadlines between sweeps. Accepted or ambiguous work is never dispatched by this recovery. Native inputs and outputs have a 24-hour window. A user can explicitly fund a new execution for a confirmed rejected or unsent failure with retained input. Legacy batch previews omit reconstruction evidence such as reference images and provider settings. Their items report `retryable:false`. Supply their original full inputs when creating new paid work.

Use route registration as the v2 cutover switch. Rolling back registration leaves accepted requests, settlement and old recovery paths available. Before retiring old writers, stop their submissions, wait for their Redis locks/active sets and nonterminal SQL jobs to drain or reconcile them, and compare scoped state, settled amount, item IDs and expiry across both readers. Imported legacy leases remain observational. They grant no new send permit.

## BE-21

Reauthorize the session and `tasks.artifacts.read` at ticket issue and content delivery. The one-minute ticket binds actor, grant epoch, workspace, project, task, artifact and expiry. The content handler rechecks authorization and retention after opening a potentially slow object. All outward results use the controller's scoped path and a fixed attachment filename. A revoke immediately disables outstanding tickets. The original v1 provider or public-storage links keep their previous expiry and access semantics.

Adapt `tasks.ObjectReader` to the controlled egress/object platform. Native objects use private keys, batch downloads reuse the pinned credential and provider route, and historical HTTPS delivery URLs use DNS-pinned public transport, bounded redirects, time and response size. Keep provider-signed URLs inside the adapter. Track orphaned uploads through the private bucket lifecycle. Use scope and stable task/receipt identities for audit and emit coarse outcome labels for metrics.

## BE-25

Add audio/video only with verified action, adapter and funding evidence. The image runner uses the original actor's project key and a BE-06 UUID allocated before dispatch. A worker must hold the live generation both to obtain a send permit and to atomically publish artifacts. Keep execution, provider acceptance, artifact availability, cancellation and settlement independent. A provider-reported percent can populate `progress`; an unmeasured job returns null. Extend item/replay input storage for families with reconstructible child inputs. Add capability-specific cancel and status reconciliation before offering those operations. Continue using the same scope-bound ticket and receipt association.

## Verification

Run `contracts/migrations/BE-15/smoke.sh` with a disposable `job_test` PostgreSQL database and Redis. It exercises concurrent matching and conflicting submissions, fresh paid retry, low balance, uncertain submission, stale worker writes, provider cancellation, historical item and lease migration, expired/revoked artifacts, request/settlement links, browser download revocation, cursor and ticket binding. Provider dispatch tests use local fixtures. Production provider generation is billable and was not part of the smoke.
