-- An upstream-observed OpenAI plan change invalidates verification, but is not
-- a legacy configuration takeover. The writer marker is transaction-local,
-- account-specific, and only used by the internal 429 observation path.
CREATE OR REPLACE FUNCTION connection_fence_legacy_edit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF current_setting('omnihive.connection_writer',true) IS DISTINCT FROM 'v2'
       AND (OLD.name,OLD.platform,OLD.type,OLD.proxy_id,OLD.proxy_group_id,OLD.concurrency,OLD.status,
            OLD.schedulable,OLD.credentials-'credential_secret_version')
        IS DISTINCT FROM
           (NEW.name,NEW.platform,NEW.type,NEW.proxy_id,NEW.proxy_group_id,NEW.concurrency,NEW.status,
            NEW.schedulable,NEW.credentials-'credential_secret_version') THEN
        IF current_setting('omnihive.connection_observation_account',true)=NEW.id::text
           AND NEW.platform='openai'
           AND jsonb_typeof(NEW.credentials->'plan_type')='string'
           AND length(trim(NEW.credentials->>'plan_type'))>0
           AND OLD.credentials->'plan_type' IS DISTINCT FROM NEW.credentials->'plan_type'
           AND OLD.credentials-'plan_type' IS NOT DISTINCT FROM NEW.credentials-'plan_type'
           AND to_jsonb(OLD)-'credentials'-'updated_at'
            IS NOT DISTINCT FROM to_jsonb(NEW)-'credentials'-'updated_at' THEN
            WITH invalidated AS (
                UPDATE provider_connections SET verified_id=NULL,version=version+1
                WHERE account_id=NEW.id AND NOT legacy_dirty
                RETURNING workspace_id,account_id,version
            )
            INSERT INTO connection_events(workspace_id,account_id,type,version)
            SELECT workspace_id,account_id,'connection.plan_observed',version FROM invalidated;
        ELSE
            UPDATE provider_connections SET legacy_dirty=true,verified_id=NULL,version=version+1
            WHERE account_id=NEW.id;
        END IF;
    END IF;
    RETURN NEW;
END $$;
