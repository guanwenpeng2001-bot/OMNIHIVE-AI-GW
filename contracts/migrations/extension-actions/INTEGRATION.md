# Migration 315 and activation

Apply `315_extension_action_grants.sql` after 309, 293 and 290 and before exposing the new inventory and private catalog projections. It creates an empty grant table, two guard triggers and the effective-action view. BE-20 installations, publisher grants and task records create zero action approvals during migration.

After an operator verifies an action against the real host adapter, record its evidence UUID, verified time, expiry, exact archive SHA-256, package version, enabled binding ID, model, project, account, current credential version and reviewer ID. Pass the reviewed values as SQL parameters.

```sql
INSERT INTO omnihive_extension_action_grants
    (binding_id,action,package_version,archive_sha256,workspace_id,project_id,
     account_id,credential_version,model,approved_by,verification_id,verified_at,expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
RETURNING id;
```

The insert verifies the signed declaration against the live enabled binding, archive digest and version. The database also checks project ownership and the credential version. The unique scope/account/model/action key rejects competing grants. Revoke a grant with `DELETE FROM omnihive_extension_action_grants WHERE id=$1`. To renew, delete the reviewed grant by ID and insert its replacement atomically in one transaction. A worker takes admission from the effective view and checks that the same grant still exists after the guest returns.

The existing BE-20 binding index allows one active installation per typed capability, platform and account type. Replacing a binding, disabling an installation or reinstalling an artifact retires its dependent grants. An in-place archive, manifest or version update expires grants as well. Rolling back to the same bytes requires a new approval tied to the new activation. Older hosts reject the new capability IDs while continuing to serve their own supported operations. Disable the action cohort before rolling back serving binaries. Keep schema 315 through the rollback window.

| Reader or writer | Contract |
| --- | --- |
| BE-20 installation lifecycle | Continues to own artifacts, state, publisher trust, rollout and bindings |
| Operator release process | Writes an explicit verified grant to 315 for each reviewed tuple |
| Admin inventory | Combines the SQL grant projection with local runtime and trust checks |
| BE-05 private catalog | Projects reviewed action labels on accessible, verified endpoint capabilities |
| Native Responses and BE-15 image task sends | Use host-selected typed plans after scope, credential and budget admission and before the single-use attempt permit |
| BE-21 egress and existing settlement | Preserve the host-selected network path, usage parser and financial writer |

Compose the BE-26 schema fragment alongside the updated BE-05 and BE-20 fragments when regenerating the aggregate API. The existing admin and gateway registrations are the entry points.
