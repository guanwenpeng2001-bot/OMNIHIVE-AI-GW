# Backend v2 inventory and cutover runbook draft

This draft covers BE-23 preparation. Inventory and status are available now. The migration, reconciliation and cohort authority procedures below require the corresponding writers and fences to ship.

## Capture the inventory

1. Record the release, maintenance window and approved deployment identity. Confirm the application database and the separately bound BE-09 journal against the running deployment. Record the physical application, PostgreSQL and Redis volume bindings, backup object-store access, persistent encryption-key continuity and retained plugin artifacts in the deployment's restricted record. Preserve the previous binary and recoverable backups. [Deployment identity migration](../../operations/identity-migration.md)
2. Configure `OMNIHIVE_REFACTOR_DSN` and, if the separate journal is deployed, `OMNIHIVE_RESTORE_JOURNAL_DSN` with approved read-only credentials. Export the JSON artifacts with protected file permissions.

   ```sh
   umask 077
   omnihivectl refactor inventory --format=json > inventory-before.json
   omnihivectl refactor status --format=json > status-before.json
   ```

3. Record collection time, run UUID, scope and any unknown sources in the restricted review record. Each database is read consistently within its own transaction. Drain writers before a cross-store comparison so the snapshots refer to the same operational boundary. Repeat with the same deployment and scope after a rehearsal or approved transition. [BE-23 integration](../../../contracts/migrations/BE-23/INTEGRATION.md)

## Compare the observations

`state: "unknown"` with `rows: null` marks an unavailable inventory source. A numeric zero means the reader observed zero matching rows. An unmapped counter uses `null` for unknown. Counts include retained or soft-deleted records unless the source column below specifies a filter. Sources can overlap. Compare each source to its counterpart across observations. Record expected changes from work completed between snapshots.

| Report counters | Measurement |
| --- | --- |
| `workspaces`, `projects` | Rows in `workspaces` and `projects`. Report bounds for `scope_version` and `version`, respectively. |
| `api_keys`, `api_key_credentials` | Separate logical key and credential rows. Legacy key and digest tables lack a resource-wide version column. Multiple credential rows can belong to one key. |
| `legacy_upstream_accounts`, `provider_connections` | Rows in `accounts` and `provider_connections`. The second source provides `version` bounds. Consult BE-04 coverage for live accounts that lack a clean mapping. |
| `legacy_credential_references`, `connection_credential_versions` | Rows and `version` bounds from `credential_secrets` and `connection_credential_versions`. The latter includes revoked history and overlaps with current references. Validate key decryptability in an isolated restoration rehearsal. |
| `legacy_wallet_accounts`, `billing_accounts` | The existing funding store carries one legacy wallet per `users` row, including zero-balance and retained users. Version evidence is unavailable for those rows. The BE-11 billing-account source remains unknown until integrated. |
| `upstream_jobs`, `batch_image_jobs` | Separate task table row counts and `version` bounds. The batch image revision starts at zero. Check active jobs, holds and provider acceptance independently before worker handoff. |
| `restore_operations`, `restore_operations_outstanding`, `restore_backups_requiring_review` | BE-09 journal totals, operations in `queued`, `running` or `unknown`, and backups flagged for review. Operation counters include `generation` bounds. Legacy backup settings and object payloads require separate review. |
| `unmapped` | Totals from BE-01 identity quarantine, BE-04 connection quarantine and unresolved BE-23 mapping exceptions across recorded runs. Pending backfill coverage and duplicate references across those sources require independent checks. |

Version bounds are decimal strings for lossless transport. A present version descriptor with null bounds indicates no observed values in that source. A null descriptor means the field is absent, unreadable or unversioned. The application inventory scans only the listed tables and returns aggregate rows and numeric version bounds.

Check confirmed logical resources against mapped, pending and quarantined records at the same cutoff. Use BE-01 ownership coverage and BE-04 connection coverage, along with the catalog provenance rules and BE-06 execution/receipt links. Record each exception's scope, stable identity, reason and approved disposition in the restricted journal. Unknown or unexplained evidence blocks the affected cohort. [BE-01](../../../contracts/migrations/BE-01/INTEGRATION.md), [BE-04](../../../contracts/migrations/BE-04/INTEGRATION.md), [BE-05](../../../contracts/migrations/BE-05/INTEGRATION.md), [BE-06](../../../contracts/migrations/BE-06/INTEGRATION.md), [BE-09](../../../contracts/migrations/BE-09/INTEGRATION.md)

## Rehearse a cohort switch after integration

1. Define the module, workspace, queue and supported provider/protocol tuple. Record the current authority and generation, desired policy release, fixed cutoff and the oldest release that can serve and recover the resulting state. Establish who can approve each authority change.
2. In an isolated copy, pause new work for the cohort. Drain active streams, lease holders, outbox workers and pending settlement work up to the chosen cutoff. Record unresolved provider acceptance and restore states. Stale lease holders must fail claims, completion and settlement after the new epoch is active.
3. Resume mapping in bounded batches using the checkpoint revision. Compare source and target counts, statuses and version bindings for the cohort. Compare exact openings, holds, pending receipts and unique effects by workspace, project, funder and currency. Reconcile opening plus posted effects to closing, with reserved and available balances accounted for separately. Balanced postings per currency and a zero unexplained difference are required. Verify dual-version readers can recover work committed around the cutoff and each effect has one writer.
4. Rehearse the atomic authority change using the expected current version. Read back the committed authority and verify that only the new generation can dispatch or settle. Replay captured queue and receipt fixtures through the designated recovery path and confirm there is no duplicate provider submission, reservation, debit or posting. Record evidence for the cohort before widening it.

## Roll back at or above the recorded floor

The **rollback floor** is the minimum compatible release and authority generation that can read and safely finish all durable effects already committed for a cohort. Record the release identity, active authority epoch, cutoff, pending receipt and task compatibility, trigger/lease compatibility, and the first durable effect that requires the newer behavior. Advance the floor whenever a durable effect needs a newer reader or writer. The current CLI reports a null floor until that evidence can be persisted.

For an operational rollback, fence current admissions, drain or isolate current lease owners, and choose a previously rehearsed release that meets the floor. Confirm it can recover outstanding holds and receipts, preserve task identities, handle the active policy and credential versions and read committed ledger entries. Change the authority with the same version guard, retaining the expanded schema, checkpoint journal, external restore journal and all newly posted transactions. An unreconciled provider acceptance or interrupted restore stays under review with its original identity.

If the prior binary is below the floor, keep a compatible authority to recover the cohort and plan a forward repair. A separately approved disaster restore uses the BE-09 journal and a verified rehearsal. Schema retirement belongs to BE-30 after the observation period and old consumers and leases have drained.
