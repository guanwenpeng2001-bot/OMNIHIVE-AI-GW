# BE-23 integration

## Independence CLI

Register `refactor` in the independence CLI and pass arguments after the subcommand to `refactor.Run` from `backend/cmd/omnihivectl/refactor`. Return its exit code from the parent command. Pass the parent command context and its stdout and stderr. The independence coordinator owns CLI manifest, packaging and distribution. The standalone main under `refactor/standalone` provides a direct entry point before that integration.

```sh
omnihivectl refactor inventory --format=json
omnihivectl refactor status --format=json
```

From `backend`, the standalone equivalent is `go run ./cmd/omnihivectl/refactor/standalone inventory --format=json`. Both commands use `OMNIHIVE_REFACTOR_DSN` for the application database and optionally `OMNIHIVE_RESTORE_JOURNAL_DSN` for the separate BE-09 journal. Supply read-only credentials through the deployment environment. Exit 0 emits one JSON report on stdout. Exit 2 indicates invalid usage or missing application configuration. Exit 1 indicates a connection configuration, read or output failure. Diagnostics contain fixed text.

Both commands use `internal/modules/migration.Reader`. A later administrative preview can reuse the reader behind a platform operator authorization check. The CLI outputs instance-wide evidence. Any future workspace preview needs an explicitly scoped query and matching authorization.

## Report and storage

[Report JSON Schema](report.schema.json) defines the current shape. The [runbook](../../../docs/migrations/backend-v2/README.md) specifies the measurement scope and source of each counter.

- `inventory` assigns an observation UUID and emits `checkpoint.state = observed`. The UUID identifies the output artifact and is ephemeral.
- `status` opens a fresh snapshot and reads the most recently updated persisted checkpoint with `module = instance` and `workspace_id = 0`. It exposes that checkpoint's UUID, revision and UTC update time. `unknown` indicates an unavailable checkpoint table. `not_started` indicates a visible table with no matching checkpoint. The aggregates in both commands reflect the current snapshots.
- The reader uses read-only repeatable-read snapshots on the application and restore databases. An unconfigured, inaccessible or missing journal leaves its counters unknown. The snapshots have independent observation times.
- Migration `312_refactor_checkpoints.sql` adds the checkpoint and mapping exception tables through the normal runner. The checkpoint key is `(run_id, module, workspace_id)`, with workspace 0 reserved for an instance-level run. An exception extends this key with `(source_kind, source_ref)` to make repeated observations safely upsertable. Resolution is recorded with `resolved_at`. Keep identities opaque and use fixed reason codes. The report exposes checkpoint metadata and aggregate exception counts.

When the migration writer is integrated, it should read the committed cursor and revision, compare that revision before advancing, and commit the batch mappings, exceptions, cursor and revision together. A mismatch means another writer advanced the checkpoint. Reload before retrying. Do not reuse a different run's cursor after changing the module or workspace scope.

## Pending activation

| Dependency | Evidence required before enabling the corresponding work |
| --- | --- |
| BE-10 | Reconcile BE-01 scope and BE-04 connection/pool assignments with legacy group and composite targets. Freeze an immutable policy snapshot, compare simulation with the production planner, and fence policy publication and revocation by epoch. |
| BE-11 | Establish exact wallet/account openings, receipts, holds and balanced postings by scope, funder, currency and cutoff. Verify v1/v2 receipt replay and effect deduplication, then enable reconciliation and a settlement writer switch when transaction and rollback-floor evidence is available. |
| BE-12 | Verify native and bridged protocol/account support and frozen fixture parity. Switch dispatch by supported tuple and prove execution identity, provider acceptance and stream drain across mixed versions. |
| BE-15 and BE-22 | Bind queue ownership to the runtime cohort and worker generation, rehearse claim/drain/recovery, and prove stale-worker fencing for tasks, outbox rows, restores and pending receipts. |
| Remaining schema-producing P1 work | Inventory plugins, financial writers, safe audit evidence and retained resources, complete coverage of source-to-target mappings, and obtain per-cohort release evidence before BE-24 and later BE-30 retirement. |

The planned `migrate --module --workspace --resume` uses the persisted checkpoint for a specific module and workspace. The planned `reconcile --currency --cutoff` reads both authorities at a fixed cutoff. The planned `cutover --module --workspace --expected-version` uses compare-and-swap to record a single authoritative writer for a cohort. Implement authority state and rollback-floor persistence with those writers and their epoch semantics. Until then `unexplained_differences` and `rollback_floor` are unknown and `eligible_for_switch` is false.

## Verification

From the repository root, set `MIGRATION_INTEGRATION_TEST_DSN` to the loopback `job_test` URI on a new `postgres:16-alpine` container. Run `contracts/migrations/migration/smoke.sh` with Go and Python 3 with `jsonschema` installed. The smoke creates isolated fixture databases, applies 312 twice to seeded legacy data, applies the normal migration chain, exercises both CLI entry points, and validates emitted reports against the JSON Schema. Tear down the test container afterward.
