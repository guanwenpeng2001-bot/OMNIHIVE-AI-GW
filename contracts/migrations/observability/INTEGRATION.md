# BE-29 workspace telemetry exports

Apply migration 318 after BE-01 and BE-21. The migration creates scoped collector, policy, idempotency, pending delivery and bounded deduplication storage. It classifies the existing unused `ops.export.configure` action as a workspace action so the identity evaluator grants it to workspace administrators. There is no historical export backfill. A binary rollback keeps the expansion. Reapply the production policy and validate any destination explicitly approved for import before enabling it.

## Composition

Include `observability.ProviderSet` and attach the resulting handler to v2 with `observability.Register(v2, handler, session, panelLimit, legacyAudit)`. The provider owns a PostgreSQL pool with four connections and uses BE-21 controlled egress. Register its cleanup after the supervisor drains. Compose the v2 fragment and regenerate the frontend API types through the aggregate branch.

Install the BE-21 `auditHandler.Capture` after session authentication on authorized project routes. In serving modes, run both `exportStore.RunIngest(ctx, onIngestFailure)` and the audit observer. Use `observer.RunWithExport(ctx, auditStore, onAuditFailure, exportStore.Offer)` in place of the existing observer call. Start `exportStore.RunDelivery(ctx, onWorkerFailure)` in worker modes. Every callback takes no arguments. Use fixed failure counters and expose `exportStore.Dropped()` as a process counter. Workspace and project visibility are checked through the live identity evaluator for every sampled scope. Delivery checks the current grant of the approval actor.

## Collector protocol

Register an immutable public HTTPS endpoint using `POST W/observability/destinations`. Create an export for its returned ID and explicitly approve the timing class, the fraction of eligible operations to sample and the retention period. Collector credentials, query and fragment are disallowed. The endpoint is private configuration and is erased upon collector revocation.

The worker validates the destination by posting `{}` with `X-OmniHive-Telemetry-Validation: 1`. A 2xx response activates that policy version. Once active, the POST body only contains `operation` (`scope.read` or `scope.write`), boolean `success` and capped `latency_ms`. The worker uses fresh background-job context, public DNS address validation and pinning, and a four-second send timeout. The collector returns a 2xx once it has accepted the sample and should honor the `Idempotency-Key` across timeouts and retries. Apply `X-OmniHive-Telemetry-Expires-At` at the collector to enforce its copy's lifetime. OmniHive enforces the expiry for its own spool and deduplication records independently.

`PATCH W/observability/exports/{id}` checks the current `If-Match` version, clears the previous policy's pending samples, and revalidates any enabled version. Explicitly use `enabled:true` to approve and revalidate a disabled or failed export. `DELETE W/observability/destinations/{id}` checks the collector version, disables its active and validating exports, and drops their queued samples. These mutations serialize with in-progress HTTPS sends. After a successful mutation the old policy cannot start a send. The remote collector can have accepted a send that began before revocation.

## Bounds and verification

Each workspace has eight active or validating exports. Each export has up to 256 pending samples and 512 completed delivery IDs within the policy's 1–30 day retention window. Data is erased immediately on completion, with only an opaque delivery ID, status bookkeeping and expiry kept for deduplication. Network errors, HTTP 408, 425, 429 and 5xx use up to four attempts. The worker schedules retries 15 seconds, one minute and five minutes after the prior claim. Validation uses the same limits. Expired records are excluded from sends and swept in bounded batches. Serving processes accept up to 256 observations through a nonblocking buffer. At saturation they drop samples and keep serving. Export workers and tables are independent of the settlement receipt and outbox.

Run from the backend directory with an isolated `UPSTREAM_JOB_TEST_DSN` and `AFFINITY_REDIS_TEST_ADDR`.

```sh
go build ./...
go vet ./...
go test ./...
go test -p 1 -tags=integration -count=1 ./internal/modules/ops/export ./internal/control/observability ./internal/control/audit ./internal/control/identity
```

The package tests exercise full migration, scoped grants, collector revocation, lease recovery, retry and queue caps, expiry, payload projection and the ability to deliver independently of locks on the financial outbox and receipt tables. The BE-01 integration test exercises the shared scope authority with its Redis-backed API key cache.
