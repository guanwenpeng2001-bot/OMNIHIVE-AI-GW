CREATE TABLE provider_connections (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    workspace_id bigint NOT NULL REFERENCES workspaces(id),
    provider text NOT NULL, auth_type text NOT NULL, name text NOT NULL, endpoint text NOT NULL DEFAULT '',
    proxy_id bigint REFERENCES proxies(id), model_ids text[] NOT NULL DEFAULT '{}',
    pool_ids bigint[] NOT NULL DEFAULT '{}', concurrency integer NOT NULL CHECK (concurrency > 0),
    state text NOT NULL CHECK (state IN ('draft','active','disabled')),
    version bigint NOT NULL DEFAULT 1, verified_id uuid, legacy_dirty boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(workspace_id,account_id)
);
CREATE INDEX connection_inventory ON provider_connections(workspace_id,account_id);

CREATE TABLE connection_credential_versions (
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    workspace_id bigint NOT NULL REFERENCES workspaces(id), reference text NOT NULL,
    version bigint NOT NULL, ciphertext text, last_four text, revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(account_id,version)
);
INSERT INTO connection_credential_versions(account_id,workspace_id,reference,version,ciphertext,revoked_at)
SELECT account_id,workspace_id,reference,version,CASE WHEN revoked_at IS NULL THEN ciphertext END,revoked_at FROM credential_secrets;
CREATE FUNCTION connection_snapshot_credential() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.version <= OLD.version THEN
        RAISE EXCEPTION 'credential fence must advance' USING ERRCODE='23514';
    END IF;
    UPDATE connection_credential_versions SET revoked_at=coalesce(revoked_at,now())
        WHERE account_id=NEW.account_id AND revoked_at IS NULL;
    INSERT INTO connection_credential_versions(account_id,workspace_id,reference,version,ciphertext,revoked_at)
        VALUES(NEW.account_id,NEW.workspace_id,NEW.reference,NEW.version,
            CASE WHEN NEW.revoked_at IS NULL THEN NEW.ciphertext END,NEW.revoked_at);
    UPDATE provider_connections SET verified_id=NULL,version=version+1,
        state=CASE WHEN NEW.revoked_at IS NOT NULL THEN 'disabled' ELSE state END
        WHERE account_id=NEW.account_id AND workspace_id=NEW.workspace_id;
    IF current_setting('omnihive.connection_writer',true) IS DISTINCT FROM 'v2' THEN
        INSERT INTO connection_events(workspace_id,account_id,type,version)
        SELECT workspace_id,account_id,CASE WHEN NEW.revoked_at IS NULL THEN 'credential.updated' ELSE 'credential.revoked' END,NEW.version
        FROM provider_connections WHERE account_id=NEW.account_id AND workspace_id=NEW.workspace_id;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER connection_credential_version AFTER INSERT OR UPDATE OF version,ciphertext,revoked_at
    ON credential_secrets FOR EACH ROW EXECUTE FUNCTION connection_snapshot_credential();

CREATE TABLE connection_pools (
    id bigserial PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id),
    name text NOT NULL, max_concurrency integer NOT NULL CHECK(max_concurrency>0),
    version bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(workspace_id,id)
);
CREATE TABLE connection_pool_members (
    workspace_id bigint NOT NULL, pool_id bigint NOT NULL, account_id bigint NOT NULL,
    PRIMARY KEY(pool_id,account_id),
    FOREIGN KEY(workspace_id,pool_id) REFERENCES connection_pools(workspace_id,id) ON DELETE CASCADE,
    FOREIGN KEY(workspace_id,account_id) REFERENCES provider_connections(workspace_id,account_id) ON DELETE CASCADE
);
CREATE TABLE connection_operations (
    id uuid PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), actor_id bigint NOT NULL,
    account_id bigint, action text NOT NULL, state text NOT NULL, result jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE connection_verifications (
    id uuid PRIMARY KEY, workspace_id bigint NOT NULL, account_id bigint NOT NULL,
    base_version bigint NOT NULL, credential_version bigint NOT NULL, proxy_revision text NOT NULL,
    eligible boolean NOT NULL, snapshot jsonb NOT NULL, expires_at timestamptz NOT NULL,
    FOREIGN KEY(workspace_id,account_id) REFERENCES provider_connections(workspace_id,account_id) ON DELETE CASCADE
);
CREATE TABLE connection_previews (
    id uuid PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), actor_id bigint NOT NULL,
    account_id bigint, patch jsonb NOT NULL, items jsonb NOT NULL, expires_at timestamptz NOT NULL
);
CREATE TABLE connection_events (
    id bigserial PRIMARY KEY, workspace_id bigint NOT NULL, account_id bigint NOT NULL,
    actor_id bigint, type text NOT NULL, version bigint NOT NULL, at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,account_id) REFERENCES provider_connections(workspace_id,account_id) ON DELETE CASCADE
);
CREATE INDEX connection_events_page ON connection_events(workspace_id,account_id,id);
CREATE TABLE connection_migration_quarantine (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE, reason text NOT NULL,
    reviewed_revision timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO identity_actions(action,category) VALUES ('pools.read','project'),('pools.edit','project') ON CONFLICT DO NOTHING;


CREATE FUNCTION connection_fence_legacy_edit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF current_setting('omnihive.connection_writer',true) IS DISTINCT FROM 'v2'
       AND (OLD.name,OLD.platform,OLD.type,OLD.proxy_id,OLD.proxy_group_id,OLD.concurrency,OLD.status,
            OLD.schedulable,OLD.credentials-'credential_secret_version')
        IS DISTINCT FROM
           (NEW.name,NEW.platform,NEW.type,NEW.proxy_id,NEW.proxy_group_id,NEW.concurrency,NEW.status,
            NEW.schedulable,NEW.credentials-'credential_secret_version') THEN
        UPDATE provider_connections SET legacy_dirty=true,verified_id=NULL,version=version+1
        WHERE account_id=NEW.id;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER connection_fence_legacy_edit AFTER UPDATE OF credentials,name,platform,type,proxy_id,proxy_group_id,concurrency,status,schedulable
ON accounts FOR EACH ROW EXECUTE FUNCTION connection_fence_legacy_edit();
