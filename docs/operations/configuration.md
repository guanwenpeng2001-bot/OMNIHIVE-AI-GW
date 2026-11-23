# OmniHive configuration authority and diagnostics

This page describes how v0.3.4 resolves a setting, how administrators can read the
effective value and source, and how database overrides are reset to a deployment
baseline. It is the operator reference for the configuration diagnostics API and
the corresponding admin console view.

## Authority layers

Every registered setting has exactly one authority. The authority determines the
fallback chain; OmniHive does not apply a global "last writer wins" policy.

| Authority | Fallback chain | Typical use |
| --- | --- | --- |
| `deployment` | environment > YAML > registry default | Static deployment inputs such as log format, catalog refresh, Turnstile requirement, WebAuthn, and Ops hard gate |
| `database_override` | database > environment > YAML > registry default | Runtime operator settings that can temporarily override deployment values, such as OAuth provider groups and image storage |
| `database_only` | database > registry default | Tenant/operations values whose runtime truth is stored in the database, such as Model Plaza switches and new-user defaults |
| `versioned_artifact` | artifact | Signed/reviewed release artifacts and reference data; not editable in the settings page |

For `deployment` settings, the database does not participate. For
`database_override` settings, an explicitly stored database value wins; deleting
that override causes the deployment baseline to take effect again. For
`database_only` settings, a missing key uses the registry default and a database
read or parse failure is reported as unavailable or invalid rather than silently
replaced for security- and money-sensitive keys.

Existing explicit values, including `false`, `0`, and empty strings, are
preserved. Upgrades add only missing keys and provenance metadata.

## Effective value and source API

Administrators can read the effective configuration with:

```http
GET /api/v1/admin/config/effective
```

Query parameters:

| Parameter | Values | Default |
| --- | --- | --- |
| `group` | Registry group, for example `security`, `ops`, `plaza`, `image_storage` | all groups |
| `q` | Case-insensitive substring match against key or group | none |
| `cursor` | Lexical key cursor returned as `next_cursor` | first page |
| `limit` | `1`–`200` | `50` |
| `source` | `database`, `environment`, `yaml`, `default`, `derived`, `artifact` | all sources |
| `state` | `effective`, `invalid`, `unavailable`, `blocked_by_hard_gate` | all states |

The response data contains `config_file`, `generated_at`, `items`, and
`next_cursor`. Each item includes:

- `key`, `group`, and `value_type` (`bool`, `int`, `float`, `string`,
  `string_list`, `json`, or `object`).
- `value`: the effective non-secret value; `null` for sensitive or unavailable
  settings.
- `source` and `authority`.
- `fallback_chain`: the applicable resolution order.
- `restart_required` and `resettable`.
- `sensitive` and `configured`: secrets are always redacted. `configured` is a
  presence boolean for sensitive values and `null` for non-sensitive values.
- `state` and optional `error_code`, `blocked_by`, `install_seed_source`, and
  `baseline`.

Derived security gates are reported as `source=derived`:

- `passkey.effective_enabled`
- `turnstile.effective_enabled`
- `ops.effective_enabled`

The admin console presents the same data at `/admin/settings/diagnostics`
once the v0.3.4 frontend package is deployed.

## Reset semantics

Database overrides are reset with:

```http
POST /api/v1/admin/config/reset
{"keys":["linuxdo_connect_enabled"]}
```

Reset is admin-only and is mounted behind the existing step-up policy for
credential-changing operations. The service:

1. Validates every requested key before deleting anything.
2. Expands provider and image-storage keys to their atomic reset group.
3. Deletes the group in one database transaction.
4. Reloads affected runtime caches and invalidates the image-storage uploader.
5. Returns the shared admin settings projection and the post-reset effective
   entries.
6. Records only key names in the audit trail; values and secrets are not logged.

Only registry entries with `resettable=true` and `database_override` authority
can be reset. `CONFIG_KEY_NOT_RESETTABLE` is returned for anything else. Reset
does not edit environment variables or YAML; it removes the database override so
the current deployment baseline becomes authoritative.

## Defaults reference

The table lists the most operator-visible defaults. It is not the complete
registry; the effective API is authoritative for a running instance.

| Key | Group | Authority | Default | Environment / YAML |
| --- | --- | --- | --- | --- |
| `model_catalog_refresh_enabled` | `catalog` | `deployment` | `false` | `MODEL_CATALOG_REFRESH_ENABLED` / `model_catalog_refresh_enabled` |
| `log.format` | `logging` | `deployment` | `json` | `LOG_FORMAT` / `log.format` |
| `turnstile.required` | `security` | `deployment` | `false` | `TURNSTILE_REQUIRED` / `turnstile.required`; enforced only in release mode |
| `webauthn.enabled` | `security` | `deployment` | `false` | `WEBAUTHN_ENABLED` / `webauthn.enabled` |
| `webauthn.rp_id` | `security` | `deployment` | empty | `WEBAUTHN_RP_ID` / `webauthn.rp_id` |
| `webauthn.rp_origins` | `security` | `deployment` | `[]` | `WEBAUTHN_RP_ORIGINS` / `webauthn.rp_origins` |
| `ops.enabled` | `ops` | `deployment` | `true` | `OPS_ENABLED` / `ops.enabled` |
| `model_plaza_enabled` | `plaza` | `database_only` | `false`; new installs seed `true` | DB setting |
| `model_plaza_require_auth` | `plaza` | `database_only` | `false` | DB setting |
| `model_plaza_description` | `plaza` | `database_only` | empty | DB setting |
| `default_balance` | `onboarding` | `database_only` | `0` | DB setting; install seed source is tracked separately |
| `default_concurrency` | `onboarding` | `database_only` | `5` | DB setting; install seed source is tracked separately |
| `image_storage.enabled` | `image_storage` | `database_override` | `false` | `IMAGE_STORAGE_ENABLED` / `image_storage.enabled` |
| `image_storage.presign_expiry_hours` | `image_storage` | `database_override` | `1` | `IMAGE_STORAGE_PRESIGN_EXPIRY_HOURS` |
| `image_storage.max_download_bytes` | `image_storage` | `database_override` | `0` | `IMAGE_STORAGE_MAX_DOWNLOAD_BYTES` |
| `image_storage.secret_access_key` | `image_storage` | `database_override` | empty, sensitive | `IMAGE_STORAGE_SECRET_ACCESS_KEY` |
| `image_storage.access_key_id` | `image_storage` | `database_override` | empty | `IMAGE_STORAGE_ACCESS_KEY_ID` |
| backup S3 configuration | `backup` | `database_only` | empty object, sensitive | managed by the backup service |
| `backup_schedule` | `backup` | `database_only` | `enabled=false`, `retain_days=14` | managed by the backup service |

Provider groups such as `linuxdo_connect`, `dingtalk_connect`, `wechat_connect`,
`oidc_connect`, `github_oauth`, and `google_oauth` are `database_override`
groups. Resetting a provider key removes the whole provider override group and
re-resolves the group from the current environment/YAML baseline.

## Backup secret key

Backup S3 credentials are not editable through the generic settings API. They
use the backup administration endpoint and an independent envelope key. See
[release-v034.md](release-v034.md) for the `BACKUP_SECRET_ENCRYPTION_KEY`
upgrade procedure and legacy fallback.
