-- v0.3.4 keeps the legacy encrypted settings payload untouched so older
-- binaries can still read it during the supported rollback window. The new
-- envelope is therefore additive and takes precedence only in v0.3.4+.
CREATE TABLE IF NOT EXISTS backup_secret_payloads (
    setting_key TEXT PRIMARY KEY,
    envelope JSONB NOT NULL,
    key_id TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (setting_key IN ('backup_s3_config')),
    CHECK (jsonb_typeof(envelope) = 'object')
);

CREATE TABLE IF NOT EXISTS backup_secret_migrations (
    setting_key TEXT PRIMARY KEY,
    state TEXT NOT NULL CHECK (state IN ('pending', 'migrated', 'error', 'read_only_legacy')),
    key_id TEXT,
    error_message TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (setting_key IN ('backup_s3_config'))
);

-- SQL never decrypts secrets. The application records pending work and performs
-- authenticated re-encryption after startup when the deployment key is present.
INSERT INTO backup_secret_migrations (setting_key, state, updated_at)
SELECT key, 'pending', now()
FROM settings
WHERE key = 'backup_s3_config'
  AND value <> ''
ON CONFLICT (setting_key) DO NOTHING;
