# BE-27 catalog verification migration

Apply 316 with the normal PostgreSQL migration runner after BE-01, BE-04 and BE-05. The expansion creates target and scheduling tables, a bounded lease queue, append-only observations and publication history, and actor-scoped command idempotency. It adds the platform `catalog.publish` action to the existing action registry. Old binaries can keep writing their existing tables while the new tables are present.

When a target is registered, the newest matching BE-04 connection verification identified by the active connection can seed recent discovery evidence. It needs the same credential and proxy revision, a passed discovery check, explicit model membership and an unexpired observation. The imported evidence keeps its original window and expiry. Its provenance distinguishes it from a live probe with an adapter fixture. Existing registry and legacy catalog facts retain their current source and publication policy.

Register the control package on the v2 group with console session middleware. Add the verification store to the common operation readers, and call `Worker.Run(ctx)` from the durable-worker process lifecycle with its shutdown context. The source behind ModelDetail already reads the reviewed publication for visible model IDs. Compose OpenAPI and generate client types after merging the fragments.

Across worker processes, the queue admits 64 queued or running jobs and leases up to four concurrently. It coalesces pending work per verification target by rejecting additional keys with a conflict. A lease lasts 30 seconds, the probe has a 15-second budget, and the maximum queue age is five minutes. Expired work finalizes as unknown. New observations keep their original timestamp and use a 24-hour expiry. Access is rechecked before and after a network call. The probe resolves the encrypted credential only for the configured active platform connection, applies BE-04 egress and redirect policy, and saves the bounded exact-model result.

To roll back content, publish a previous compatibility revision or withdraw to revision zero. For binary rollback, drain the workers and leave 316 in place. Restarted workers expire pending work according to its saved timestamps. No data rewrite or removal is needed during the rollback window.

On the package worktree, run the catalog, worker, HTTP and migration checks against its temporary PostgreSQL container.

```sh
cd backend
go test -tags=integration -count=1 -v ./internal/modules/catalog/verification ./internal/control/catalog/verification
go build ./...
go vet ./...
go test -tags=unit ./...
```
