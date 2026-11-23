# OmniHive v0.3.4 upgrade and rollback runbook

This runbook covers an in-place upgrade from v0.3.3 to v0.3.4, the new backup
secret encryption key, and the rollback floor for release artifacts. It does
not replace a database backup or the production release smoke checks.

## Before you begin

1. Back up PostgreSQL, the application data directory, and the persistent
   deployment secrets (`.env` or systemd environment, `config.yaml`, JWT and
   TOTP keys, and any data encryption keys).
2. Remove the instance from the load balancer and drain active connections
   before stopping the application, following the deployment guide.
3. Record the current `backend/cmd/server/VERSION` and the active database
   migration state.

## Migrations

v0.3.4 applies five additive migrations:

| Migration | Purpose |
| --- | --- |
| `339_default_workspace_resolution.sql` | Resolves Default workspace ownership and adds the stable default workspace resolver used by resource ownership paths |
| `340_project_access_requests.sql` | Adds membership-bound project access requests, decisions, and tenant-qualified indexes |
| `341_setting_authority_and_defaults.sql` | Adds settings provenance and seeds missing registry defaults without changing existing values |
| `342_backup_secret_envelopes.sql` | Adds additive backup secret envelopes and migration state while retaining legacy ciphertext |
| `343_backup_scheduler_jobs.sql` | Adds backup job and scheduler-state tables and imports existing backup record IDs once |

The migrations do not raise the database rollback floor. Do not edit already
applied SQL files. After startup, verify the applied set in `schema_migrations`
includes `339` through `343`.

## Backup secret encryption key

Backup S3 credentials are protected by a dedicated key rather than the TOTP
encryption key:

| Variable | Required | Format | Purpose |
| --- | --- | --- | --- |
| `BACKUP_SECRET_ENCRYPTION_KEY` | Required for new or rotated backup S3 credentials | 32 bytes as 64 lowercase hex characters (`openssl rand -hex 32`) | AES-256-GCM envelope key for backup S3 secret payloads |
| `BACKUP_SECRET_ENCRYPTION_KEY_PREVIOUS` | Optional | 64 hex characters | Read-only fallback used while rotating the primary key |

Store the primary key persistently in the protected deployment secret/config
location with mode `0600`, separately from `TOTP_ENCRYPTION_KEY`. Never commit it
to the repository or write its plaintext to logs.

Upgrade behavior:

- An installation with an existing legacy backup credential and no new key still
  starts and can read its TOTP-encrypted credential.
- The backup page and effective API show `migration_required` until the
  deployment key is configured and the legacy value is re-encrypted.
- New or rotated backup S3 credentials fail until
  `BACKUP_SECRET_ENCRYPTION_KEY` is available.
- Keep `TOTP_ENCRYPTION_KEY` through the rollback window; v0.3.3 reads the
  retained legacy ciphertext.

After adding the key, restart, open the backup administration page, confirm the
key source no longer shows `migration_required`, run an S3 connection test, and
create a manual backup that completes with `verified_at` set before changing
either key.

## Deployment changes to review

- `MODEL_CATALOG_REFRESH_ENABLED` defaults to `false` in v0.3.4. An existing
  deployment that previously relied on the old code default of `true` must set
  the variable explicitly to continue periodic catalog refresh.
- `LOG_FORMAT` defaults to `json`.
- `TURNSTILE_REQUIRED` defaults to `false` and is enforced only in release mode.
  Effective login gating also requires the database Turnstile soft switch and
  secret.
- New-user defaults are database-authoritative: balance `0`, concurrency `5`.
  They are seeded on fresh installs and do not follow later environment changes.

## Upgrade procedure

### Docker Compose

1. Put the new image tag in the Compose configuration or deployment `.env`.
2. Run `docker compose pull` and `docker compose up -d`.
3. Check `/health` and `/ready`, then run the post-upgrade checks below.

### Native/systemd or direct binary

1. Stop the service and keep the previous binary for recovery.
2. Replace the binary and preserve the systemd environment and `/etc/omnihive`
   files.
3. Start the service and run the post-upgrade checks below.

Existing-install CLI upgrade and rollback checks need database access through
`OMNIHIVE_RELEASE_DSN`. Do not print this DSN.

## Post-upgrade checks

- `/health` returns `200` and `/ready` returns `200` when dependencies are
  available.
- `schema_migrations` contains migrations `339` through `343`.
- Configuration diagnostics show no unexpected `CONFIG_INVALID` or
  `CONFIG_UNAVAILABLE` entries. Verify the derived gates
  `passkey.effective_enabled`, `turnstile.effective_enabled`, and
  `ops.effective_enabled` reflect the intended deployment.
- Backup administration shows scheduler `loaded` and `health` as expected. Run a
  manual remote backup and confirm it is `completed` and `verified_at` is set.
- Rollback eligibility shows the `database_floor`, eligible versions, and
  exclusion reasons.
- Reconcile any shared Default workspace grants reported by migration `339`
  before treating the upgrade as complete.

## Rollback

The effective rollback floor is:

```text
max(oldest_supported_release, database floor, artifact floor)
```

For v0.3.4, the oldest supported release is `v0.2.5` and the artifact floor is
`0.3.0`. The database floor is read from
`release_schema_compatibility.minimum_rollback_version`; the v0.3.4 migrations do
not raise it.

Before rolling back a Docker image by changing the tag, run a release preflight
against the deployed database:

```bash
OMNIHIVE_RELEASE_DSN=... omnihivectl release check \
  --target v0.3.3 \
  --manifest release-manifest.json \
  --checksums checksums.txt \
  --json
```

Use the manifest and checksum files from the selected release. The check
verifies the manifest identity, checksum, and rollback floor before download or
replacement. If the target archive does not contain a capable CLI, install a
current trusted `omnihivectl` first. The application image/binary must not be
replaced when the check cannot establish eligibility.

Application rollback does not reverse migrations. Restoring the database is a
separate disaster-recovery operation. v0.3.3 can continue reading the retained
legacy backup secret fields and backup record IDs during the rollback window.
