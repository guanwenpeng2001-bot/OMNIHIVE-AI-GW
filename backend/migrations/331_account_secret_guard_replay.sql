CREATE OR REPLACE FUNCTION guard_account_inline_secrets() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.credentials ?| ARRAY['access_token','refresh_token','id_token','agent_private_key','api_key','session_key','cookie',
        'password','sso_token','sso','sso-rw','clearTextPassword','aws_secret_access_key','aws_session_token','service_account_json',
        'service_account','private_key','volcengine_access_key_id','volcengine_secret_access_key','byteplus_access_key_id',
        'byteplus_secret_access_key','tencent_query_secret_id','tencent_query_secret_key','usage_query_token']
       AND current_setting('omnihive.account_secret_backfill',true) IS DISTINCT FROM NEW.id::text THEN
        RAISE EXCEPTION 'account secrets require encrypted storage' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS accounts_no_inline_secrets ON accounts;
CREATE TRIGGER accounts_no_inline_secrets BEFORE INSERT OR UPDATE OF credentials ON accounts
    FOR EACH ROW EXECUTE FUNCTION guard_account_inline_secrets();
CREATE TABLE IF NOT EXISTS account_secret_backfill (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS account_secret_backfill_done (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    completed_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO account_secret_backfill SELECT id FROM accounts
WHERE credentials IS NOT NULL AND credentials <> '{}'::jsonb
ON CONFLICT DO NOTHING;
