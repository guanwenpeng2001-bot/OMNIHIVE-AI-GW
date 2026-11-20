-- Existing pilot rows keep their original auth class, owners and member instances.
ALTER TABLE byok_credentials
    ADD COLUMN auth_type text NOT NULL DEFAULT 'api_key',
    ADD COLUMN service_account_ids bigint[] NOT NULL DEFAULT '{}',
    ADD COLUMN self_service boolean NOT NULL DEFAULT false;

-- The pilot row retains the per-provider fee and legacy API-key enablement.
-- A reviewed class takes precedence over that legacy switch, including a denial.
CREATE TABLE byok_provider_classes (
    workspace_id bigint NOT NULL,
    provider text NOT NULL,
    auth_type text NOT NULL,
    enabled boolean NOT NULL DEFAULT false,
    PRIMARY KEY(workspace_id,provider,auth_type),
    FOREIGN KEY(workspace_id,provider) REFERENCES byok_pilot_providers(workspace_id,provider)
);

-- A machine principal has a dedicated project key and a sponsoring membership.
-- A retired identity is retained so its key never silently becomes a human grant.
CREATE TABLE byok_service_accounts (
    id bigserial PRIMARY KEY,
    workspace_id bigint NOT NULL,
    project_id bigint NOT NULL,
    membership_id bigint,
    api_key_id bigint NOT NULL UNIQUE,
    revoked_at timestamptz,
    FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
    FOREIGN KEY(workspace_id,api_key_id) REFERENCES api_keys(workspace_id,id),
    FOREIGN KEY(membership_id) REFERENCES workspace_members(id) ON DELETE SET NULL,
    FOREIGN KEY(workspace_id,membership_id) REFERENCES workspace_members(workspace_id,id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE byok_lifecycle_events (
    id bigserial PRIMARY KEY, workspace_id bigint NOT NULL, credential_id bigint NOT NULL,
    project_ids bigint[] NOT NULL, actor_id bigint, version bigint NOT NULL,
    event text NOT NULL, at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(workspace_id,credential_id) REFERENCES byok_credentials(workspace_id,id)
);
CREATE UNIQUE INDEX byok_event_revision ON byok_lifecycle_events(workspace_id,credential_id,version);
CREATE INDEX byok_events_seek ON byok_lifecycle_events(workspace_id,credential_id,id);

-- Serialize class cutover with permits that still read only the pilot row.
CREATE FUNCTION byok_lock_class_parent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        PERFORM 1 FROM byok_pilot_providers WHERE workspace_id=OLD.workspace_id AND provider=OLD.provider FOR UPDATE;
        RETURN OLD;
    END IF;
    IF TG_OP='UPDATE' AND (NEW.workspace_id,NEW.provider,NEW.auth_type) IS DISTINCT FROM (OLD.workspace_id,OLD.provider,OLD.auth_type) THEN
        RAISE EXCEPTION 'BYOK class identity is immutable' USING ERRCODE='23514';
    END IF;
    PERFORM 1 FROM byok_pilot_providers WHERE workspace_id=NEW.workspace_id AND provider=NEW.provider FOR UPDATE;
    RETURN NEW;
END $$;
CREATE TRIGGER byok_class_cutover BEFORE INSERT OR UPDATE OR DELETE ON byok_provider_classes
FOR EACH ROW EXECUTE FUNCTION byok_lock_class_parent();

-- Mixed-version nodes may finish already committed permits. They cannot mutate
-- self-service credentials or acquire a new permit requiring class/principal checks.
CREATE FUNCTION byok_require_current_writer() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE guarded boolean;
BEGIN
    IF current_setting('omnihive.byok_writer',true) = '28' THEN RETURN NEW; END IF;
    IF TG_TABLE_NAME = 'byok_credentials' THEN
        guarded := NEW.self_service OR EXISTS (SELECT FROM byok_provider_classes pc
            WHERE (pc.workspace_id,pc.provider,pc.auth_type)=(NEW.workspace_id,NEW.provider,NEW.auth_type));
    ELSE
        SELECT c.self_service OR EXISTS (SELECT FROM byok_provider_classes pc
            WHERE (pc.workspace_id,pc.provider,pc.auth_type)=(c.workspace_id,c.provider,c.auth_type))
            OR EXISTS (SELECT FROM gateway_execution_attempts a JOIN gateway_executions e USING(request_id)
                JOIN byok_service_accounts sa ON sa.api_key_id=e.api_key_id WHERE a.attempt_id=NEW.attempt_id)
        INTO guarded FROM byok_credentials c WHERE (c.workspace_id,c.id)=(NEW.workspace_id,NEW.credential_id);
    END IF;
    IF guarded THEN RAISE EXCEPTION 'BYOK writer upgrade required' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER byok_control_writer BEFORE INSERT OR UPDATE ON byok_credentials
FOR EACH ROW EXECUTE FUNCTION byok_require_current_writer();
CREATE TRIGGER byok_permit_writer BEFORE INSERT ON byok_attempt_funding
FOR EACH ROW EXECUTE FUNCTION byok_require_current_writer();
