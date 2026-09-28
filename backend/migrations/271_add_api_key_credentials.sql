-- Owner: key-credential-lifecycle. Rollback floor: a build that resolves API key
-- auth by credential digest; never restore plaintext credentials from a backup.
--
-- Expand -> digest index -> verify -> clear plaintext, all idempotent and safe to
-- re-run. The digest is hex(sha256(secret)); it matches the application credential
-- digest and the API key auth cache identifier exactly.
CREATE TABLE IF NOT EXISTS api_key_credentials (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL DEFAULT 1 REFERENCES workspaces(id),
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    credential_digest VARCHAR(64) NOT NULL,
    display_suffix VARCHAR(16) NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'current',
    valid_until TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS api_key_credentials_digest_unique ON api_key_credentials(credential_digest);
CREATE INDEX IF NOT EXISTS api_key_credentials_api_key_id ON api_key_credentials(api_key_id);
CREATE INDEX IF NOT EXISTS api_key_credentials_status ON api_key_credentials(status);

-- Backfill: each live plaintext key becomes one 'current' digest credential that
-- keeps the original api_keys.id. Tombstones and already-migrated rows are skipped.
INSERT INTO api_key_credentials (workspace_id, api_key_id, credential_digest, display_suffix, status, created_at, updated_at)
SELECT a.workspace_id, a.id, encode(sha256(a.key::bytea), 'hex'), right(a.key, 4), 'current', NOW(), NOW()
FROM api_keys a
WHERE a.deleted_at IS NULL AND a.key <> ''
ON CONFLICT (credential_digest) DO NOTHING;

-- Verify before clearing: no live non-empty plaintext key may lack a matching digest.
DO $api_key_credential_verify$
BEGIN
    IF EXISTS (
        SELECT 1 FROM api_keys a
        WHERE a.deleted_at IS NULL AND a.key <> ''
          AND NOT EXISTS (
              SELECT 1 FROM api_key_credentials c
              WHERE c.api_key_id = a.id AND c.credential_digest = encode(sha256(a.key::bytea), 'hex')
          )
    ) THEN
        RAISE EXCEPTION 'api key credential backfill incomplete; refusing to clear plaintext';
    END IF;
END;
$api_key_credential_verify$;

-- Withdraw plaintext: the column loses its uniqueness (now on credential_digest)
-- and defaults to empty so new keys never store a secret.
ALTER TABLE api_keys ALTER COLUMN key SET DEFAULT '';
ALTER TABLE api_keys ALTER COLUMN key DROP NOT NULL;
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_key_key;
DROP INDEX IF EXISTS api_keys_key_key;

-- Batch the statements, not the transaction: row locks last until this migration
-- commits. Only deleted_at identifies tombstones; custom secrets may use any
-- allowed prefix, including __deleted__.
DO $api_key_credential_clear$
DECLARE
    cleared INTEGER;
BEGIN
    LOOP
        UPDATE api_keys SET key = ''
        WHERE id IN (SELECT id FROM api_keys WHERE key <> '' LIMIT 5000);
        GET DIAGNOSTICS cleared = ROW_COUNT;
        EXIT WHEN cleared = 0;
    END LOOP;
END;
$api_key_credential_clear$;
