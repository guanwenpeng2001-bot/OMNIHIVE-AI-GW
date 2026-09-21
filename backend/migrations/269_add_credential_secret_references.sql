-- Additive only: existing inline credentials are never automatically converted.
CREATE TABLE IF NOT EXISTS credential_secrets (
    id BIGSERIAL PRIMARY KEY,
    reference TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL DEFAULT 1 REFERENCES workspaces(id),
    account_id BIGINT NOT NULL UNIQUE REFERENCES accounts(id),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    ciphertext TEXT NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS credential_secrets_workspace_idx ON credential_secrets(workspace_id);

-- Fail closed even for older/raw writers: a converted account cannot regain
-- inline secrets or bind another account/workspace's reference.
CREATE OR REPLACE FUNCTION guard_account_credential_reference() RETURNS trigger AS $$
DECLARE reference_value TEXT;
BEGIN
    reference_value := NEW.credentials ->> 'credential_secret_reference';
    IF TG_OP = 'UPDATE' AND OLD.credentials ? 'credential_secret_reference'
       AND (reference_value IS NULL OR reference_value IS DISTINCT FROM OLD.credentials ->> 'credential_secret_reference') THEN
        RAISE EXCEPTION 'credential reference cannot be replaced or removed' USING ERRCODE = '23514';
    END IF;
    IF NEW.credentials ? 'credential_secret_reference' THEN
        IF reference_value IS NULL OR NOT EXISTS (
            SELECT 1 FROM credential_secrets WHERE reference = reference_value
            AND account_id = NEW.id AND workspace_id = NEW.workspace_id
        ) THEN
            RAISE EXCEPTION 'invalid credential reference ownership' USING ERRCODE = '23514';
        END IF;
        IF NEW.credentials ?| ARRAY['access_token','refresh_token','id_token','agent_private_key',
           'api_key','session_key','cookie','password','sso_token','sso','sso-rw','clearTextPassword',
           'aws_secret_access_key','aws_session_token','service_account_json','service_account','private_key'] THEN
            RAISE EXCEPTION 'inline secret on referenced account' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS accounts_credential_reference_guard ON accounts;
CREATE TRIGGER accounts_credential_reference_guard BEFORE INSERT OR UPDATE OF credentials, workspace_id
ON accounts FOR EACH ROW EXECUTE FUNCTION guard_account_credential_reference();
