-- A submission claims its identity before admission. A crash leaves a queryable
-- handle and cannot grant another sender a permit. Payloads never enter this table.
CREATE TABLE playground_runs (
    run_id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    workspace_id bigint NOT NULL,
    project_id bigint NOT NULL,
    actor_id bigint NOT NULL,
    api_key_id bigint NOT NULL,
    payer_user_id bigint NOT NULL,
    funding_acknowledged_at timestamptz NOT NULL DEFAULT now(),
    raw_events_requested boolean NOT NULL,
    preview_consent boolean NOT NULL,
    protocol text NOT NULL,
    model_id text NOT NULL,
    replay_of text,
    key_hash bytea NOT NULL CHECK (octet_length(key_hash) = 32),
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    state text NOT NULL CHECK (state IN ('running','cancel_requested','succeeded','failed','canceled','unknown')),
    last_seq bigint NOT NULL DEFAULT 0,
    deadline timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_id, actor_id, key_hash),
    FOREIGN KEY (workspace_id, project_id) REFERENCES projects(workspace_id,id)
);
ALTER TABLE gateway_executions DROP CONSTRAINT gateway_executions_kind_check;
ALTER TABLE gateway_executions ADD CONSTRAINT gateway_executions_kind_check
    CHECK (kind IN ('request','turn','job','playground'));
INSERT INTO identity_actions(action,category) VALUES ('playground.raw','project') ON CONFLICT DO NOTHING;

CREATE FUNCTION playground_dispatch_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT FROM gateway_executions WHERE request_id=NEW.request_id AND kind='playground') THEN
        PERFORM 1 FROM playground_runs WHERE request_id=NEW.request_id
            AND state='running' AND deadline>clock_timestamp() FOR SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION 'inactive playground run'; END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER playground_dispatch_fence BEFORE INSERT OR UPDATE OF dispatched_at ON gateway_execution_attempts
FOR EACH ROW WHEN (NEW.dispatched_at IS NOT NULL) EXECUTE FUNCTION playground_dispatch_fence();
